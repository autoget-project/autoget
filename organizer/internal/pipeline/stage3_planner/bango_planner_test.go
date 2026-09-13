package stage3planner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai/mock"
	"github.com/autoget-project/organizer/internal/model"
)

func TestResolveBangoTargetDir_DecisionMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta model.EnrichedMetadata
		want string
	}{
		{
			name: "default jav with amateur dir",
			meta: model.EnrichedMetadata{Bango: "SSIS-698"},
			want: "jav/素人",
		},
		{
			name: "jav_vr root with bango subdir",
			meta: model.EnrichedMetadata{Bango: "SSIS-698", IsVR: true},
			want: "jav_vr/素人/SSIS-698",
		},
		{
			name: "madou root has top priority",
			meta: model.EnrichedMetadata{Bango: "MD-0123", FromMadou: true},
			want: "madou/素人",
		},
		{
			name: "resolved actor directory",
			meta: model.EnrichedMetadata{Bango: "SSIS-698", Actors: []string{"Yui Hatano", "波多野結衣"}},
			want: "jav/Yui Hatano",
		},
		{
			name: "vr with actor appends bango subdir",
			meta: model.EnrichedMetadata{Bango: "SSIS-698", IsVR: true, Actors: []string{"Yui Hatano"}},
			want: "jav_vr/Yui Hatano/SSIS-698",
		},
		{
			name: "madou overrides vr root but keeps bango subdir",
			meta: model.EnrichedMetadata{Bango: "MD-0123", IsVR: true, FromMadou: true},
			want: "madou/素人/MD-0123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ResolveBangoTargetDir(tt.meta))
		})
	}
}

func TestBangoPlanner_CPriorityOverMultiPart(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "bango (番号) identifiers",
		Response: `{"filenames":[
			{"file":"/downloads/SSIS-698-C.mp4","new_filename":"SSIS-698-C.mp4"},
			{"file":"/downloads/SSIS-698-A.mp4","new_filename":"SSIS-698.part.1.mp4"},
			{"file":"/downloads/SSIS-698-B.mp4","new_filename":"SSIS-698.part.2.mp4"}
		]}`,
	})

	planner := NewBangoPlanner(prov)
	pc := &PlannerContext{
		Files: []string{
			"/downloads/SSIS-698-C.mp4",
			"/downloads/SSIS-698-A.mp4",
			"/downloads/SSIS-698-B.mp4",
		},
		Metadata: model.EnrichedMetadata{
			Bango:  "SSIS-698",
			Actors: []string{"Yua Mikami"},
		},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	// The -C Chinese-subtitle suffix outranks multi-part renumbering.
	want := map[string]string{
		"/downloads/SSIS-698-C.mp4": "jav/Yua Mikami/SSIS-698-C.mp4",
		"/downloads/SSIS-698-A.mp4": "jav/Yua Mikami/SSIS-698.part.1.mp4",
		"/downloads/SSIS-698-B.mp4": "jav/Yua Mikami/SSIS-698.part.2.mp4",
	}
	for file, target := range want {
		action := findAction(t, actions, file)
		require.Equal(t, "move", action.Action, "file %s", file)
		require.NotNil(t, action.Target, "file %s", file)
		assert.Equal(t, target, *action.Target, "file %s", file)
	}
}

func TestBangoPlanner_ActorDirectoryFromEnrichedMetadata(t *testing.T) {
	t.Parallel()

	// A single -C file whose actor directory was already resolved by Stage 2.
	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "bango (番号) identifiers",
		Response:      `{"filenames":[{"file":"/downloads/SSIS-698-C.mp4","new_filename":"SSIS-698-C.mp4"}]}`,
	})

	planner := NewBangoPlanner(prov)
	pc := &PlannerContext{
		Files:    []string{"/downloads/SSIS-698-C.mp4"},
		Metadata: model.EnrichedMetadata{Bango: "SSIS-698", Actors: []string{"Yua Mikami"}},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	action := findAction(t, actions, "/downloads/SSIS-698-C.mp4")
	require.Equal(t, "move", action.Action)
	require.NotNil(t, action.Target)
	assert.Equal(t, "jav/Yua Mikami/SSIS-698-C.mp4", *action.Target)
}

func TestBangoPlanner_MadouAndNonVideoSkip(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "bango (番号) identifiers",
		Response:      `{"filenames":[{"file":"/downloads/MD-0123.mp4","new_filename":"MD-0123.mp4"}]}`,
	})

	planner := NewBangoPlanner(prov)
	pc := &PlannerContext{
		Files:    []string{"/downloads/MD-0123.mp4", "/downloads/cover.jpg"},
		Metadata: model.EnrichedMetadata{Bango: "MD-0123", FromMadou: true},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	move := findAction(t, actions, "/downloads/MD-0123.mp4")
	require.Equal(t, "move", move.Action)
	require.NotNil(t, move.Target)
	assert.Equal(t, "madou/素人/MD-0123.mp4", *move.Target)

	skip := findAction(t, actions, "/downloads/cover.jpg")
	assert.Equal(t, "skip", skip.Action)
	assert.Nil(t, skip.Target)
}
