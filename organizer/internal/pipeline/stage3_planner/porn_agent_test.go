package stage3planner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai/mock"
	"github.com/autoget-project/organizer/internal/metadata"
	"github.com/autoget-project/organizer/internal/model"
)

// testTPDBCandidates mirrors the field-tested step-1 samples (Agatha / GirlsWay
// / Long Con) so slugs and titles match the real API shapes.
func testTPDBCandidates() []metadata.TPDBVideo {
	return []metadata.TPDBVideo{
		{
			Slug:       "tushyraw-stunning-agatha-likes-it-in-the-ass",
			Title:      "Stunning Agatha Likes It In The Ass",
			Type:       "scene",
			Date:       "2026-08-30",
			Site:       "Tushy Raw",
			Performers: []string{"Agatha Vega"},
		},
		{
			Slug:       "girlsway-runaway-brides-regret",
			Title:      "Runaway Bride's Regret",
			Type:       "scene",
			Date:       "2026-09-06",
			Site:       "GirlsWay",
			Performers: []string{"Serene Siren", "Aubree Valentine"},
		},
		{
			Slug:       "vixen-long-con-part-1",
			Title:      "Long Con Part 1",
			Type:       "movie",
			Date:       "2025-11-14",
			Site:       "Vixen",
			Performers: []string{"Agatha Vega"},
		},
	}
}

// agathaTwoPerformers is the Agatha scene with a second performer appended,
// used to exercise ordered performer-directory matching.
func agathaTwoPerformers() []metadata.TPDBVideo {
	return []metadata.TPDBVideo{
		{
			Slug:       "tushyraw-stunning-agatha-likes-it-in-the-ass",
			Title:      "Stunning Agatha Likes It In The Ass",
			Type:       "scene",
			Date:       "2026-08-30",
			Site:       "Tushy Raw",
			Performers: []string{"Agatha Vega", "Second Actress"},
		},
	}
}

// fakePornSource returns preset candidates whose match key appears in the
// query (empty key matches every query) and records every query it receives.
type fakePornSource struct {
	match      string
	candidates []metadata.TPDBVideo
	err        error

	queries []string
}

func (f *fakePornSource) SearchVideos(_ context.Context, query string) ([]metadata.TPDBVideo, error) {
	f.queries = append(f.queries, query)
	if f.err != nil {
		return nil, f.err
	}
	var out []metadata.TPDBVideo
	for _, c := range f.candidates {
		if f.match == "" || strings.Contains(query, f.match) {
			out = append(out, c)
		}
	}
	return out, nil
}

// plainProviderStub implements only ai.Provider (no tool capability); it
// covers the non-ToolProvider degradation branch.
type plainProviderStub struct{}

func (plainProviderStub) Name() string { return "plain" }

func (plainProviderStub) GenerateStructured(_ context.Context, _ string, _ any, result any) error {
	return json.Unmarshal([]byte(`{"decisions":[]}`), result)
}

func TestPornPlanner_AgentPlans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		pc          *PlannerContext
		preDirs     []string // directories pre-created under targetDir
		candidates  []metadata.TPDBVideo
		steps       []mock.ToolStep
		wantTargets map[string]string
	}{
		{
			name: "hit without performer dir",
			pc: &PlannerContext{
				Files:    []string{"Agatha Vega 2026.08.30 2160p.mp4"},
				Metadata: model.EnrichedMetadata{Title: "Stunning Agatha"},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Agatha Vega 2026-08-30"}`},
				{Response: `{"decisions":[{"file":"Agatha Vega 2026.08.30 2160p.mp4","hit":true,"slug":"tushyraw-stunning-agatha-likes-it-in-the-ass"}]}`},
			},
			wantTargets: map[string]string{
				"Agatha Vega 2026.08.30 2160p.mp4": "porn/Stunning Agatha Likes It In The Ass/Stunning Agatha Likes It In The Ass.mp4",
			},
		},
		{
			name: "hit layered under second performer dir",
			pc: &PlannerContext{
				Files: []string{"Agatha Vega 2026.08.30 2160p.mp4"},
			},
			preDirs:    []string{"porn/Second Actress"},
			candidates: agathaTwoPerformers(),
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Agatha Vega 2026-08-30"}`},
				{Response: `{"decisions":[{"file":"Agatha Vega 2026.08.30 2160p.mp4","hit":true,"slug":"tushyraw-stunning-agatha-likes-it-in-the-ass"}]}`},
			},
			wantTargets: map[string]string{
				"Agatha Vega 2026.08.30 2160p.mp4": "porn/Second Actress/Stunning Agatha Likes It In The Ass/Stunning Agatha Likes It In The Ass.mp4",
			},
		},
		{
			name: "vr root scans only porn_vr",
			pc: &PlannerContext{
				Files:    []string{"Agatha Vega VR 2026.08.30.mp4"},
				Metadata: model.EnrichedMetadata{IsVR: true},
			},
			preDirs:    []string{"porn/Second Actress", "porn_vr/Agatha Vega"},
			candidates: agathaTwoPerformers(),
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Agatha Vega 2026-08-30"}`},
				{Response: `{"decisions":[{"file":"Agatha Vega VR 2026.08.30.mp4","hit":true,"slug":"tushyraw-stunning-agatha-likes-it-in-the-ass"}]}`},
			},
			wantTargets: map[string]string{
				"Agatha Vega VR 2026.08.30.mp4": "porn_vr/Agatha Vega/Stunning Agatha Likes It In The Ass/Stunning Agatha Likes It In The Ass.mp4",
			},
		},
		{
			name: "same slug parts and distinct slugs",
			pc: &PlannerContext{
				Files: []string{"Long Con 1.mp4", "Long Con 2.mp4", "Runaway Bride.mp4"},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Long Con"}`},
				{ToolName: "search_porn", ArgsJSON: `{"query":"Runaway Bride 2026-09-06"}`},
				{Response: `{"decisions":[` +
					`{"file":"Long Con 1.mp4","hit":true,"slug":"vixen-long-con-part-1"},` +
					`{"file":"Long Con 2.mp4","hit":true,"slug":"vixen-long-con-part-1"},` +
					`{"file":"Runaway Bride.mp4","hit":true,"slug":"girlsway-runaway-brides-regret"}]}`},
			},
			wantTargets: map[string]string{
				"Long Con 1.mp4":    "porn/Long Con Part 1/Long Con Part 1.part.1.mp4",
				"Long Con 2.mp4":    "porn/Long Con Part 1/Long Con Part 1.part.2.mp4",
				"Runaway Bride.mp4": "porn/Runaway Bride's Regret/Runaway Bride's Regret.mp4",
			},
		},
		{
			name: "hit tagged virtual reality routes to porn_vr even without stage2 flag",
			pc: &PlannerContext{
				Files: []string{"SlrOriginals.26.12.25.Hot.Scene.mp4"},
				// Stage 2 did not declare VR (no search grounding, no metadata):
				// the tpdb hit tag is the authoritative VR evidence.
				Metadata: model.EnrichedMetadata{IsVR: false},
			},
			candidates: []metadata.TPDBVideo{
				{
					Slug:       "slroriginals-hot-scene",
					Title:      "Hot Scene",
					Type:       "scene",
					Date:       "2026-12-25",
					Site:       "SLR Originals",
					Performers: []string{"VR Star"},
					Tags:       []string{"Virtual Reality", "Cowgirl"},
				},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"SLR Originals Hot Scene 2026-12-25"}`},
				{Response: `{"decisions":[{"file":"SlrOriginals.26.12.25.Hot.Scene.mp4","hit":true,"slug":"slroriginals-hot-scene"}]}`},
			},
			wantTargets: map[string]string{
				"SlrOriginals.26.12.25.Hot.Scene.mp4": "porn_vr/Hot Scene/Hot Scene.mp4",
			},
		},
		{
			name: "fabricated slug falls back",
			pc: &PlannerContext{
				Files: []string{"Mystery Scene.mp4"},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Mystery Scene"}`},
				{Response: `{"decisions":[{"file":"Mystery Scene.mp4","hit":true,"slug":"totally-made-up-slug"}]}`},
			},
			wantTargets: map[string]string{
				"Mystery Scene.mp4": "porn/Mystery Scene/Mystery Scene.mp4",
			},
		},
		{
			name: "miss and unmentioned files fall back",
			pc: &PlannerContext{
				Files: []string{"Miss One.mp4", "Runaway Bride.mp4", "Miss Two.mp4"},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Runaway Bride 2026-09-06"}`},
				{Response: `{"decisions":[` +
					`{"file":"Miss One.mp4","hit":false,"slug":""},` +
					`{"file":"Runaway Bride.mp4","hit":true,"slug":"girlsway-runaway-brides-regret"}]}`},
			},
			wantTargets: map[string]string{
				"Miss One.mp4":      "porn/Miss One/Miss One.mp4",
				"Miss Two.mp4":      "porn/Miss Two/Miss Two.mp4",
				"Runaway Bride.mp4": "porn/Runaway Bride's Regret/Runaway Bride's Regret.mp4",
			},
		},
		{
			name: "title sanitization",
			pc: &PlannerContext{
				Files: []string{"Dirty Title.mp4"},
			},
			candidates: []metadata.TPDBVideo{
				{
					Slug:       "dirty-title-scene",
					Title:      "  \"Why\": <So>? *Wild* | Scene \\1/  ",
					Type:       "scene",
					Date:       "2026-08-30",
					Site:       "Tushy Raw",
					Performers: []string{"Agatha Vega"},
				},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"Dirty Title"}`},
				{Response: `{"decisions":[{"file":"Dirty Title.mp4","hit":true,"slug":"dirty-title-scene"}]}`},
			},
			wantTargets: map[string]string{
				"Dirty Title.mp4": "porn/Why So Wild Scene 1/Why So Wild Scene 1.mp4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			targetDir := t.TempDir()
			for _, d := range tt.preDirs {
				require.NoError(t, os.MkdirAll(filepath.Join(targetDir, d), 0o755))
			}

			candidates := tt.candidates
			if candidates == nil {
				candidates = testTPDBCandidates()
			}
			src := &fakePornSource{candidates: candidates}

			mp := mock.NewProvider()
			mp.AddToolRule(mock.ToolRule{Steps: tt.steps})

			planner := NewPornPlanner(mp, targetDir, src)
			actions, err := planner.Plan(context.Background(), tt.pc)
			require.NoError(t, err)

			for file, want := range tt.wantTargets {
				action := findAction(t, actions, file)
				require.Equal(t, "move", action.Action, "file %s", file)
				require.NotNil(t, action.Target, "file %s", file)
				assert.Equal(t, want, *action.Target, "file %s", file)
			}
		})
	}
}

func TestPornPlanner_AgentPromptCarriesPayloadAndToolResult(t *testing.T) {
	t.Parallel()

	pc := &PlannerContext{
		Files:    []string{"Agatha Vega 2026.08.30 2160p.mp4"},
		Metadata: model.EnrichedMetadata{Title: "Stunning Agatha"},
		Entities: map[string]interface{}{"actors": []interface{}{"Agatha Vega"}},
	}
	src := &fakePornSource{candidates: testTPDBCandidates()}
	mp := mock.NewProvider()
	mp.AddToolRule(mock.ToolRule{Steps: []mock.ToolStep{
		{ToolName: "search_porn", ArgsJSON: `{"query":"Agatha Vega 2026-08-30"}`},
		{Response: `{"decisions":[{"file":"Agatha Vega 2026.08.30 2160p.mp4","hit":true,"slug":"tushyraw-stunning-agatha-likes-it-in-the-ass"}]}`},
	}})

	planner := NewPornPlanner(mp, t.TempDir(), src)
	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)
	require.NotEmpty(t, actions)

	// The prompt payload carries the enriched title anchor, file list and VR flag.
	calls := mp.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "Stunning Agatha")
	assert.Contains(t, calls[0].Prompt, "Agatha Vega 2026.08.30 2160p.mp4")
	assert.Contains(t, calls[0].Prompt, `"is_vr":false`)

	// The search_porn tool ran and its JSON result was fed back to the model.
	toolCalls := mp.ToolCalls()
	require.Len(t, toolCalls, 1)
	assert.Equal(t, "search_porn", toolCalls[0].Name)
	assert.Contains(t, toolCalls[0].ResultJSON, "tushyraw-stunning-agatha-likes-it-in-the-ass")
}

func TestPornPlanner_AgentErrorDegradesToFallback(t *testing.T) {
	t.Parallel()

	pc := &PlannerContext{Files: []string{"Agatha Vega 2026.08.30.mp4", "Long Con.mp4"}}
	src := &fakePornSource{candidates: testTPDBCandidates()}
	mp := mock.NewProvider()
	mp.AddToolRule(mock.ToolRule{Steps: []mock.ToolStep{
		{ToolName: "search_porn", ArgsJSON: `{"query":"Agatha Vega 2026-08-30"}`},
		{Error: errors.New("llm exploded")},
	}})

	planner := NewPornPlanner(mp, t.TempDir(), src)
	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err, "agent failure must degrade, not surface")

	for _, f := range pc.Files {
		action := findAction(t, actions, f)
		require.Equal(t, "move", action.Action, "file %s", f)
		require.NotNil(t, action.Target, "file %s", f)
		stem := strings.TrimSuffix(f, filepath.Ext(f))
		assert.Equal(t, "porn/"+stem+"/"+f, *action.Target, "file %s", f)
	}
}

func TestPornPlanner_NonToolProviderSkipsAgent(t *testing.T) {
	t.Parallel()

	pc := &PlannerContext{Files: []string{"Agatha Vega 2026.08.30.mp4"}}
	src := &fakePornSource{candidates: testTPDBCandidates()}

	planner := NewPornPlanner(plainProviderStub{}, t.TempDir(), src)
	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	action := findAction(t, actions, pc.Files[0])
	require.Equal(t, "move", action.Action)
	require.NotNil(t, action.Target)
	assert.Equal(t, "porn/Agatha Vega 2026.08.30/Agatha Vega 2026.08.30.mp4", *action.Target)
}

func TestPornPlanner_NilTPDBSkipsAgent(t *testing.T) {
	t.Parallel()

	pc := &PlannerContext{Files: []string{"Agatha Vega 2026.08.30.mp4"}}
	mp := mock.NewProvider()

	planner := NewPornPlanner(mp, t.TempDir(), nil)
	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	action := findAction(t, actions, pc.Files[0])
	require.Equal(t, "move", action.Action)
	require.NotNil(t, action.Target)
	assert.Equal(t, "porn/Agatha Vega 2026.08.30/Agatha Vega 2026.08.30.mp4", *action.Target)

	// The agent path is never entered: no LLM call, no tool session.
	assert.Empty(t, mp.Calls())
	assert.Empty(t, mp.ToolCalls())
}

func TestPornPlanner_AgentLeavesSubtitlesToStage4AndSkipsOthers(t *testing.T) {
	t.Parallel()

	pc := &PlannerContext{
		Files: []string{"Runaway Bride.mp4", "Runaway Bride.srt", "cover.jpg"},
	}
	src := &fakePornSource{candidates: testTPDBCandidates()}
	mp := mock.NewProvider()
	mp.AddToolRule(mock.ToolRule{Steps: []mock.ToolStep{
		{ToolName: "search_porn", ArgsJSON: `{"query":"Runaway Bride 2026-09-06"}`},
		{Response: `{"decisions":[{"file":"Runaway Bride.mp4","hit":true,"slug":"girlsway-runaway-brides-regret"}]}`},
	}})

	planner := NewPornPlanner(mp, t.TempDir(), src)
	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	move := findAction(t, actions, "Runaway Bride.mp4")
	require.Equal(t, "move", move.Action)
	require.NotNil(t, move.Target)
	assert.Equal(t, "porn/Runaway Bride's Regret/Runaway Bride's Regret.mp4", *move.Target)

	// The subtitle stays unplanned for Stage 4 semantic pairing.
	for _, a := range actions {
		assert.NotEqual(t, "Runaway Bride.srt", a.File, "subtitle must be left to stage 4")
	}

	other := findAction(t, actions, "cover.jpg")
	assert.Equal(t, "skip", other.Action)
	assert.Nil(t, other.Target)
}
