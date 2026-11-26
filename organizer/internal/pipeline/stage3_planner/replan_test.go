package stage3planner

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/ptr"
)

func TestReplanPrompt_DomainRoutingAndPayload(t *testing.T) {
	t.Parallel()

	prevTarget := "porn/OLD/OLD.mp4"
	base := ReplanInput{
		Files:        []string{"a.mp4"},
		Metadata:     map[string]interface{}{"title": "Raw Title"},
		PreviousPlan: []model.PlanAction{{File: "a.mp4", Action: model.ActionMove, Target: ptr.Str(prevTarget)}},
		UserHint:     "fix the title",
	}

	tests := []struct {
		name         string
		cat          model.Category
		enriched     model.EnrichedMetadata
		wantPrompt   string
		wantContains []string
	}{
		{
			name:         "tv series",
			cat:          model.CategoryTVSeries,
			wantPrompt:   "revises a TV series file organization plan",
			wantContains: []string{`"root_path":"tv_series"`},
		},
		{
			name:         "movie",
			cat:          model.CategoryMovie,
			wantPrompt:   "revises a movie file organization plan",
			wantContains: []string{`"root_path":"movie"`},
		},
		{
			name:       "bango hands the full target dir to the LLM",
			cat:        model.CategoryBangoPorn,
			enriched:   model.EnrichedMetadata{Bango: "SSIS-001", Actors: []string{"悠香", "YUUKA"}},
			wantPrompt: "revises a bango (JAV) file organization plan",
			wantContains: []string{
				`"root_path":"jav/悠香"`,
			},
		},
		{
			name:         "bango madou root",
			cat:          model.CategoryBangoPorn,
			enriched:     model.EnrichedMetadata{Bango: "MD-001", FromMadou: true, Actors: []string{"某人"}},
			wantPrompt:   "revises a bango (JAV) file organization plan",
			wantContains: []string{`"root_path":"madou/某人"`},
		},
		{
			name:         "bango vr adds the bango subdir",
			cat:          model.CategoryBangoPorn,
			enriched:     model.EnrichedMetadata{Bango: "SSIS-001", IsVR: true, Actors: []string{"悠香"}},
			wantPrompt:   "revises a bango (JAV) file organization plan",
			wantContains: []string{`"root_path":"jav_vr/悠香/SSIS-001"`},
		},
		{
			name:         "anim tv series",
			cat:          model.CategoryTVSeries,
			enriched:     model.EnrichedMetadata{IsAnim: true},
			wantPrompt:   "revises a TV series file organization plan",
			wantContains: []string{`"root_path":"anim_tv_series"`},
		},
		{
			name:         "anim movie",
			cat:          model.CategoryMovie,
			enriched:     model.EnrichedMetadata{IsAnim: true},
			wantPrompt:   "revises a movie file organization plan",
			wantContains: []string{`"root_path":"anim_movie"`},
		},
		{
			name:         "porn vr",
			cat:          model.CategoryPorn,
			enriched:     model.EnrichedMetadata{IsVR: true},
			wantPrompt:   "revises a western/general adult video (porn) file organization plan",
			wantContains: []string{`"root_path":"porn_vr"`},
		},
		{
			name:       "bango without a resolved actress falls back to amateur",
			cat:        model.CategoryBangoPorn,
			wantPrompt: "revises a bango (JAV) file organization plan",
			wantContains: []string{
				`"root_path":"jav/素人"`,
			},
		},
		{
			name:         "porn",
			cat:          model.CategoryPorn,
			wantPrompt:   "revises a western/general adult video (porn) file organization plan",
			wantContains: []string{`"root_path":"porn"`},
		},
		{
			name:         "unknown category uses the generic prompt",
			cat:          model.CategoryPhotobook,
			wantPrompt:   "revises file organization plans based on user feedback",
			wantContains: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := base
			in.Enriched = tt.enriched
			got := ReplanPrompt(tt.cat, in)

			assert.Contains(t, got, tt.wantPrompt)
			assert.Contains(t, got, `"user_hint":"fix the title"`)
			assert.Contains(t, got, prevTarget, "the flawed previous plan must be supplied")
			assert.Contains(t, got, `"enriched"`, "Stage 2 facts must be supplied")
			assert.NotContains(t, got, "including the category/library root")
			assert.NotContains(t, got, `"actor_dir"`)
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
			if tt.cat == model.CategoryPhotobook {
				assert.NotContains(t, got, `"root_path":"`, "the generic prompt payload has no library root")
			}
		})
	}
}

type failingProvider struct{ calls atomic.Int32 }

func (f *failingProvider) Name() string { return "failing" }

func (f *failingProvider) GenerateStructured(_ context.Context, _ string, _ any, _ any) error {
	f.calls.Add(1)
	return errors.New("unexpected llm call")
}

func TestPlanReplan_SimpleCategoryUsesSimplePlan(t *testing.T) {
	t.Parallel()

	files := []string{"a.epub", "b.txt"}
	p := &failingProvider{}
	got, err := PlanReplan(context.Background(), p, model.CategoryBook, ReplanInput{Files: files})
	require.NoError(t, err)
	assert.Equal(t, SimplePlan(model.CategoryBook, files), got)
	assert.Zero(t, p.calls.Load())
}
