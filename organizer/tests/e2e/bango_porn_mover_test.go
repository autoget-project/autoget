package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
	stage2enricher "github.com/autoget-project/organizer/internal/pipeline/stage2_enricher"
)

// TestE2E_BangoMoverWithActorStore drives the bango mover with an actor store.
// When an actress alias is resolved or provided, the plan creates the actress directory.
func TestE2E_BangoMoverWithActorStore(t *testing.T) {
	// Prepare a real actor store fixture with Yua Mikami (三上悠亚)
	actorData := `{
  "三上悠亚": ["三上悠亜", "Yua Mikami", "Mikami Yua", "三上悠亚"]
}`
	actorFile := filepath.Join(t.TempDir(), "actor.json")
	require.NoError(t, os.WriteFile(actorFile, []byte(actorData), 0o644))

	runWithLiveProvidersAndActorStore(t, actorFile, func(t *testing.T, s *sandbox, store *stage2enricher.ActorStore) {
		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir:   "downloads",
			Files: []string{"SSIS-698-C.mp4"},
			// Metadata contains alias Yua Mikami, which should resolve to primary name 三上悠亚
			Metadata: map[string]interface{}{
				"actors": []string{"Yua Mikami"},
			},
		})

		require.Equal(t, 200, code, body)
		var resp model.PlanResponse
		decodeBody(t, code, body, &resp)
		assert.Nil(t, resp.Error)
		require.Len(t, resp.Plan, 1)

		act := resp.Plan[0]
		assert.Equal(t, "move", act.Action)
		require.NotNil(t, act.Target)

		// Invariant: jav/ primary actress name / uppercase bango with -C
		assert.True(t, strings.HasPrefix(*act.Target, "jav/"))
		assert.Contains(t, *act.Target, "三上悠亚")
		assert.Contains(t, *act.Target, "SSIS-698-C.mp4")
	})
}

// TestE2E_BangoPartMatrixAndCPriority covers the bango derivation matrix and
// priority (M3): ssis-698-a -> part.1, ssis-698-b -> part.2, and ssis-698-C
// keeps its name because the -C Chinese-subtitle rule outranks multi-part
// renumbering.
func TestE2E_BangoPartMatrixAndCPriority(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		files := []string{"ssis-698-a.mp4", "ssis-698-b.mp4", "ssis-698-C.mp4"}
		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir:   "javdl",
			Files: files,
		})

		require.Equal(t, 200, code, body)
		var resp model.PlanResponse
		decodeBody(t, code, body, &resp)
		assert.Nil(t, resp.Error)
		require.Len(t, resp.Plan, 3)

		planMap := make(map[string]model.PlanAction)
		for _, act := range resp.Plan {
			planMap[act.File] = act
		}

		// Invariant: all 3 should land under jav/ (with 素人 or actress name)
		for _, f := range files {
			act, ok := planMap[f]
			require.True(t, ok)
			assert.Equal(t, "move", act.Action)
			require.NotNil(t, act.Target)
			assert.True(t, strings.HasPrefix(*act.Target, "jav/"))
		}

		// Check multi-part part.1 / part.2 and -C preservation (case-insensitively on bango)
		actA := planMap["ssis-698-a.mp4"]
		actB := planMap["ssis-698-b.mp4"]
		actC := planMap["ssis-698-C.mp4"]

		assert.Contains(t, strings.ToUpper(*actA.Target), "SSIS-698.PART.1.MP4")
		assert.Contains(t, strings.ToUpper(*actB.Target), "SSIS-698.PART.2.MP4")
		assert.Contains(t, strings.ToUpper(*actC.Target), "SSIS-698-C.MP4")
	})
}
