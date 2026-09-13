package stage3planner

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai/mock"
	"github.com/autoget-project/organizer/internal/model"
)

func TestMoviePlanner_AnimRouting_H4(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		isAnim        bool
		wantPromptTip string
		wantTargetTip string
	}{
		{
			name:          "anim goes to anim_movie",
			isAnim:        true,
			wantPromptTip: "anim_movie/Chinese",
			wantTargetTip: "anim_movie/",
		},
		{
			name:          "non-anim goes to movie",
			isAnim:        false,
			wantPromptTip: "movie/Chinese",
			wantTargetTip: "movie/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prov := mock.NewProvider()
			prov.AddRule(mock.Rule{
				PromptPattern: "organizes movie downloads",
				Response: `{"plan":[{"file":"Movie/Movie.2001.mkv","action":"move",
					"target":"` + tt.wantTargetTip + `Chinese/千与千寻 (2001)/千与千寻 (2001).mkv"}]}`,
			})

			planner := NewMoviePlanner(prov)
			pc := &PlannerContext{
				Files: []string{"Movie/Movie.2001.mkv"},
				Metadata: model.EnrichedMetadata{
					Title:    "千与千寻",
					Year:     2001,
					IsAnim:   tt.isAnim,
					Language: model.LanguageChinese,
				},
			}

			actions, err := planner.Plan(context.Background(), pc)
			require.NoError(t, err)

			calls := prov.Calls()
			require.Len(t, calls, 1)
			assert.Contains(t, calls[0].Prompt, tt.wantPromptTip, "prompt root_path mismatch")

			action := findAction(t, actions, "Movie/Movie.2001.mkv")
			assert.Equal(t, "move", action.Action)
			require.NotNil(t, action.Target)
			assert.True(t, strings.HasPrefix(*action.Target, tt.wantTargetTip),
				"expected target starting with %s, got %s", tt.wantTargetTip, *action.Target)
		})
	}
}

func TestMoviePlanner_SampleAndTrailerFiltering(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes movie downloads",
		Response: `{"plan":[
			{"file":"Movie/The.Matrix.1999.mkv","action":"move","target":"movie/English/黑客帝国 (1999)/黑客帝国 (1999).mkv"},
			{"file":"Movie/sample.mkv","action":"skip"},
			{"file":"Movie/trailer.mkv","action":"skip"}
		]}`,
	})

	planner := NewMoviePlanner(prov)
	pc := &PlannerContext{
		Files: []string{
			"Movie/The.Matrix.1999.mkv",
			"Movie/sample.mkv",
			"Movie/trailer.mkv",
			"Movie/cover.nfo",
		},
		Metadata: model.EnrichedMetadata{
			Title:    "黑客帝国",
			Year:     1999,
			Language: model.LanguageEnglish,
		},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)
	require.Len(t, actions, len(pc.Files))

	main := findAction(t, actions, "Movie/The.Matrix.1999.mkv")
	require.Equal(t, "move", main.Action)
	require.NotNil(t, main.Target)
	assert.Equal(t, "movie/English/黑客帝国 (1999)/黑客帝国 (1999).mkv", *main.Target)

	for _, file := range []string{"Movie/sample.mkv", "Movie/trailer.mkv", "Movie/cover.nfo"} {
		action := findAction(t, actions, file)
		assert.Equal(t, "skip", action.Action, "file %s", file)
		assert.Nil(t, action.Target, "file %s", file)
	}
}
