package stage3planner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai/mock"
	"github.com/autoget-project/organizer/internal/model"
)

// findAction returns the action for the given file.
func findAction(t *testing.T, actions []model.PlanAction, file string) model.PlanAction {
	t.Helper()
	for _, a := range actions {
		if a.File == file {
			return a
		}
	}
	t.Fatalf("action for file %q not found in plan", file)
	return model.PlanAction{}
}

func TestTVPlanner_AnimRouting_H4(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		isAnim        bool
		wantPromptTip string
		wantTargetTip string
	}{
		{
			name:          "anim goes to anim_tv_series",
			isAnim:        true,
			wantPromptTip: "anim_tv_series/Chinese",
			wantTargetTip: "anim_tv_series/",
		},
		{
			name:          "non-anim goes to tv_series",
			isAnim:        false,
			wantPromptTip: "tv_series/Chinese",
			wantTargetTip: "tv_series/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prov := mock.NewProvider()
			prov.AddRule(mock.Rule{
				PromptPattern: "organizes TV series downloads",
				Response: `{"plan":[{"file":"Show/Show S01E01.mkv","action":"move",
					"target":"` + tt.wantTargetTip + `Chinese/我和僵尸有个约会 (1998)/Season 02/我和僵尸有个约会 (1998) S02E01.mkv"}]}`,
			})

			planner := NewTVPlanner(prov)
			pc := &PlannerContext{
				Files: []string{"Show/Show S01E01.mkv"},
				Metadata: model.EnrichedMetadata{
					Title:    "我和僵尸有个约会",
					Year:     1998,
					IsAnim:   tt.isAnim,
					Language: model.LanguageChinese,
				},
			}

			actions, err := planner.Plan(context.Background(), pc)
			require.NoError(t, err)

			// The planner must inject the H4 root into the LLM prompt.
			calls := prov.Calls()
			require.Len(t, calls, 1)
			assert.Contains(t, calls[0].Prompt, tt.wantPromptTip, "prompt root_path mismatch")

			action := findAction(t, actions, "Show/Show S01E01.mkv")
			assert.Equal(t, "move", action.Action)
			require.NotNil(t, action.Target)
			assert.True(t, strings.HasPrefix(*action.Target, tt.wantTargetTip),
				"expected target starting with %s, got %s", tt.wantTargetTip, *action.Target)
		})
	}
}

func TestTVPlanner_MessyEpisodeMapping(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response: `{"plan":[
			{"file":"Show/第03话.mkv","action":"move","target":"tv_series/Chinese/X (2020)/Season 01/X (2020) S01E03.mkv"},
			{"file":"Show/1x05.mkv","action":"move","target":"tv_series/Chinese/X (2020)/Season 01/X (2020) S01E05.mkv"},
			{"file":"Show/[01-02合集] E01.mkv","action":"move","target":"tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.mkv"},
			{"file":"Show/OVA1.mkv","action":"move","target":"tv_series/Chinese/X (2020)/Season 00/X (2020) S00E01.mkv"},
			{"file":"Show/SP02.mkv","action":"move","target":"tv_series/Chinese/X (2020)/Season 00/X (2020) S00E02.mkv"},
			{"file":"Show/E01v2.mkv","action":"move","target":"tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.mkv"},
			{"file":"Show/sample.mkv","action":"skip"}
		]}`,
	})

	planner := NewTVPlanner(prov)
	pc := &PlannerContext{
		Files: []string{
			"Show/第03话.mkv",
			"Show/1x05.mkv",
			"Show/[01-02合集] E01.mkv",
			"Show/OVA1.mkv",
			"Show/SP02.mkv",
			"Show/E01v2.mkv",
			"Show/sample.mkv",
			"Show/cover.jpg",
		},
		Metadata: model.EnrichedMetadata{Title: "X", Year: 2020, Language: model.LanguageChinese},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	// Every input file must be accounted for exactly once.
	require.Len(t, actions, len(pc.Files))

	wantTargets := map[string]string{
		"Show/第03话.mkv":          "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E03.mkv",
		"Show/1x05.mkv":          "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E05.mkv",
		"Show/[01-02合集] E01.mkv": "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.mkv",
		"Show/OVA1.mkv":          "tv_series/Chinese/X (2020)/Season 00/X (2020) S00E01.mkv",
		"Show/SP02.mkv":          "tv_series/Chinese/X (2020)/Season 00/X (2020) S00E02.mkv",
		"Show/E01v2.mkv":         "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.mkv",
	}
	for file, want := range wantTargets {
		action := findAction(t, actions, file)
		require.Equal(t, "move", action.Action, "file %s", file)
		require.NotNil(t, action.Target, "file %s", file)
		assert.Equal(t, want, *action.Target, "file %s", file)
	}

	// Sample skipped by the LLM, non-media skipped locally.
	for _, file := range []string{"Show/sample.mkv", "Show/cover.jpg"} {
		action := findAction(t, actions, file)
		assert.Equal(t, "skip", action.Action, "file %s", file)
		assert.Nil(t, action.Target, "file %s", file)
	}
}

func TestTVPlanner_UncoveredVideoMarkedSkip(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response:      `{"plan":[{"file":"Show/E01.mkv","action":"move","target":"tv_series/Others/X (2020)/Season 01/X (2020) S01E01.mkv"}]}`,
	})

	planner := NewTVPlanner(prov)
	pc := &PlannerContext{
		Files:    []string{"Show/E01.mkv", "Show/E02.mkv"},
		Metadata: model.EnrichedMetadata{Title: "X", Year: 2020},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	action := findAction(t, actions, "Show/E02.mkv")
	assert.Equal(t, "skip", action.Action, "uncovered video must be skipped")
}

func TestTVPlanner_LLMErrorPropagates(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Error:         fmt.Errorf("provider outage"),
	})

	planner := NewTVPlanner(prov)
	_, err := planner.Plan(context.Background(), &PlannerContext{
		Files:    []string{"Show/E01.mkv"},
		Metadata: model.EnrichedMetadata{Title: "X"},
	})
	require.Error(t, err, "LLM failure must propagate as a fatal error")
	assert.ErrorContains(t, err, "provider outage")
}
