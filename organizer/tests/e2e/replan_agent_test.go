package e2e

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

// TestE2E_ReplanWithHintLifecycle drives /v1/plan -> /v1/replan-with-hint ->
// /v1/execute: the user hint is applied on top of the previous plan without
// re-running Stage 1/2, and executing the replanned result delivers the file
// into TARGET_DIR and archives the source directory.
func TestE2E_ReplanWithHintLifecycle(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		s.seedDownloadFile(t, "replandl", "movie.mkv", "mkvdata")

		// Initial plan.
		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir:      "replandl",
			Files:    []string{"movie.mkv"},
			Metadata: map[string]interface{}{"organizer_category": "movie", "title": "Wrong Name", "year": 2020},
		})
		require.Equal(t, http.StatusOK, code, body)
		var initial model.PlanResponse
		decodeBody(t, code, body, &initial)
		require.Nil(t, initial.Error)
		require.Len(t, initial.Plan, 1)

		// Replan with a user hint: Stage 1/2 are never re-run.
		code, body = s.postJSON(t, "/v1/replan-with-hint", model.APIReplanRequest{
			Files:            []string{"movie.mkv"},
			Metadata:         map[string]interface{}{"organizer_category": "movie", "title": "Wrong Name", "year": 2020},
			PreviousResponse: initial,
			UserHint:         "the movie name is wrong, it should be The Correct Name",
		})
		require.Equal(t, http.StatusOK, code, body)
		var replanned model.PlanResponse
		decodeBody(t, code, body, &replanned)
		require.Nil(t, replanned.Error)
		require.NotEmpty(t, replanned.Plan)
		require.NotNil(t, replanned.Plan[0].Target, "user hint must be applied")

		// Invariant: Target must remain a valid movie target and update away from "Wrong Name"
		target := *replanned.Plan[0].Target
		assert.True(t, strings.HasPrefix(target, "movie/"))
		assert.False(t, strings.Contains(target, "Wrong Name"), "target should not keep Wrong Name")
		assert.True(t, strings.Contains(strings.ToLower(target), "correct") || strings.Contains(target, "The Correct Name"),
			"target should reflect user correction: %s", target)

		// Execute the replanned plan: file lands and source dir is archived.
		code, body = s.postJSON(t, "/v1/execute", model.APIExecuteRequest{
			Dir:  "replandl",
			Plan: replanned.Plan,
		})
		require.Equal(t, http.StatusOK, code, body)

		// Verification: The destination file exists under targetDir
		assert.FileExists(t, filepath.Join(s.targetDir, target))
		assert.DirExists(t, filepath.Join(s.downloadDir, "archive", "replandl"))
	})
}
