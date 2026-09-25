package stage3planner

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
			name:       "bango hands the canonical actress dir to the LLM",
			cat:        model.CategoryBangoPorn,
			enriched:   model.EnrichedMetadata{Bango: "SSIS-001", Actors: []string{"悠香", "YUUKA"}},
			wantPrompt: "revises a bango (JAV) file organization plan",
			wantContains: []string{
				`"root_path":"jav"`,
				`"actor_dir":"悠香"`,
			},
		},
		{
			name:       "bango without a resolved actress falls back to amateur",
			cat:        model.CategoryBangoPorn,
			wantPrompt: "revises a bango (JAV) file organization plan",
			wantContains: []string{
				`"actor_dir":"素人"`,
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
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
			if tt.cat == model.CategoryPhotobook {
				assert.NotContains(t, got, `"root_path":"`, "the generic prompt payload has no library root")
			}
		})
	}
}
