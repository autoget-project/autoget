package stage3planner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/ai/mock"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

func TestRouter_UnknownCategoryReturnsEmptyPlan(t *testing.T) {
	t.Parallel()

	// No rules and no default response: any LLM call would fail loudly.
	router := NewRouter(mock.NewProvider(), "", nil)

	actions, err := router.Plan(context.Background(), model.CategoryUnknown, &PlannerContext{
		Files: []string{"hash/junk.bin"},
	})
	require.NoError(t, err, "unknown category must not produce an error")
	assert.Empty(t, actions, "unknown category must return an empty plan")
}

func TestRouter_SimpleCategoriesRouteToLocalPlanner(t *testing.T) {
	t.Parallel()

	// No rules: if the router wrongly delegated to an LLM planner it would error.
	router := NewRouter(mock.NewProvider(), "", nil)

	for _, cat := range model.SimpleMoveCategories {
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()

			actions, err := router.Plan(context.Background(), cat, &PlannerContext{
				Files: []string{"hash/" + string(cat) + ".dat"},
			})
			require.NoError(t, err)
			require.Len(t, actions, 1)
			assert.Equal(t, "move", actions[0].Action)

			wantPrefix := string(cat) + "/"
			require.NotNil(t, actions[0].Target)
			assert.True(t, strings.HasPrefix(*actions[0].Target, wantPrefix),
				"expected target prefix %s, got %s", wantPrefix, *actions[0].Target)
		})
	}
}

func TestRouter_PornRoutesToLocalPlanner(t *testing.T) {
	t.Parallel()

	router := NewRouter(mock.NewProvider(), "", nil)

	actions, err := router.Plan(context.Background(), model.CategoryPorn, &PlannerContext{
		Files: []string{"Long-Con-1.mp4"},
		Entities: map[string]interface{}{
			"id": "vixen-long-con-part-1",
		},
	})
	require.NoError(t, err)

	action := findAction(t, actions, "Long-Con-1.mp4")
	require.NotNil(t, action.Target)
	assert.Equal(t, "porn/vixen-long-con-part-1/vixen-long-con-part-1.mp4", *action.Target)
}

func TestRouter_LLMPlannersRouteToProvider(t *testing.T) {
	t.Parallel()

	// A failing default response proves which planner each category reaches.
	prov := mock.NewProvider()
	prov.SetDefaultResponse(nil, errors.New("no rule"))
	router := NewRouter(prov, "", nil)

	tests := []struct {
		cat       model.Category
		errSubstr string
	}{
		{model.CategoryTVSeries, "tv planner llm"},
		{model.CategoryMovie, "movie planner llm"},
		{model.CategoryBangoPorn, "bango planner llm"},
	}

	for _, tt := range tests {
		t.Run(string(tt.cat), func(t *testing.T) {
			t.Parallel()

			_, err := router.Plan(context.Background(), tt.cat, &PlannerContext{
				Files:    []string{"X/E01.mkv"},
				Metadata: model.EnrichedMetadata{Title: "X"},
			})
			require.Error(t, err, "provider error must propagate")
			assert.Contains(t, err.Error(), tt.errSubstr)
		})
	}
}

func TestRouter_PlanWithDetail(t *testing.T) {
	t.Parallel()

	router := NewRouter(mock.NewProvider(), "", nil)
	actions, detail, err := router.PlanWithDetail(context.Background(), model.CategoryBook, &PlannerContext{
		Files: []string{"book/book.epub"},
	})
	require.NoError(t, err)
	require.Len(t, actions, 1)
	assert.Equal(t, "book", detail.Planner)
	assert.Empty(t, detail.ActionReasons, "deterministic category should have empty action reasons")

	// Test TV LLM planner populated ActionReasons
	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response: `{"plan":[{"file":"ep1.mkv","action":"move",
			"target":"tv_series/Others/Show (2020)/Season 01/Show (2020) S01E01.mkv",
			"reason":"first episode of season 1"}]}`,
	})
	tvRouter := NewRouter(prov, "", nil)
	tvActions, tvDetail, tvErr := tvRouter.PlanWithDetail(context.Background(), model.CategoryTVSeries, &PlannerContext{
		Files:    []string{"ep1.mkv"},
		Metadata: model.EnrichedMetadata{Title: "Show", Year: 2020},
	})
	require.NoError(t, tvErr)
	require.Len(t, tvActions, 1)
	assert.Equal(t, "tv_series", tvDetail.Planner)
	require.Len(t, tvDetail.ActionReasons, 1)
	assert.Equal(t, "ep1.mkv", tvDetail.ActionReasons[0].File)
	assert.Equal(t, "first episode of season 1", tvDetail.ActionReasons[0].Reason)
}
