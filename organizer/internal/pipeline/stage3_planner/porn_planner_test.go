package stage3planner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

func TestPornPlanner_NamingFallbackChain_L9(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pc   *PlannerContext
		want string
	}{
		{
			name: "entities id first",
			pc: &PlannerContext{
				Files:    []string{"Long-Con-1.mp4"},
				Metadata: model.EnrichedMetadata{Title: "Long Con Part 1"},
				Entities: map[string]interface{}{
					"id":   "vixen-long-con-part-1",
					"name": "Long Con Part 1",
				},
			},
			want: "porn/vixen-long-con-part-1/vixen-long-con-part-1.mp4",
		},
		{
			name: "entities name second",
			pc: &PlannerContext{
				Files:    []string{"Long-Con-1.mp4"},
				Metadata: model.EnrichedMetadata{Title: "Should Not Be Used"},
				Entities: map[string]interface{}{
					"name": "Long Con Part 1",
				},
			},
			want: "porn/Long Con Part 1/Long Con Part 1.mp4",
		},
		{
			name: "enriched title third",
			pc: &PlannerContext{
				Files:    []string{"Long-Con-1.mp4"},
				Metadata: model.EnrichedMetadata{Title: "Long Con Part 1"},
			},
			want: "porn/Long Con Part 1/Long Con Part 1.mp4",
		},
		{
			name: "file stem last",
			pc: &PlannerContext{
				Files: []string{"Long-Con-1.mp4"},
			},
			want: "porn/Long-Con-1/Long-Con-1.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			planner := NewPornPlanner(nil, "", nil)
			actions, err := planner.Plan(context.Background(), tt.pc)
			require.NoError(t, err)

			action := findAction(t, actions, tt.pc.Files[0])
			require.Equal(t, "move", action.Action)
			require.NotNil(t, action.Target)
			assert.Equal(t, tt.want, *action.Target)
		})
	}
}

func TestPornPlanner_VRRootAndCompanionFiles(t *testing.T) {
	t.Parallel()

	planner := NewPornPlanner(nil, "", nil)
	pc := &PlannerContext{
		Files: []string{
			"VR Video.mp4",
			"VR Video.srt",
			"cover.jpg",
			"meta.nfo",
		},
		Metadata: model.EnrichedMetadata{IsVR: true},
	}

	actions, err := planner.Plan(context.Background(), pc)
	require.NoError(t, err)

	move := findAction(t, actions, "VR Video.mp4")
	require.Equal(t, "move", move.Action)
	require.NotNil(t, move.Target)
	assert.Equal(t, "porn_vr/VR Video/VR Video.mp4", *move.Target)

	// Subtitle files stay unplanned: Stage 4 pairs them semantically.
	for _, a := range actions {
		assert.NotEqual(t, "VR Video.srt", a.File, "subtitle must be left to stage 4")
	}

	// Non-media files are skipped.
	for _, file := range []string{"cover.jpg", "meta.nfo"} {
		action := findAction(t, actions, file)
		assert.Equal(t, "skip", action.Action, "file %s", file)
		assert.Nil(t, action.Target, "file %s", file)
	}
}
