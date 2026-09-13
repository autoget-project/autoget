package e2e

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

// TestE2E_MovieMover drives the movie mover through the full pipeline: the
// main feature is renamed into the movie library, the companion subtitle is
// renamed next to it, and cover art / partial files are skipped.
func TestE2E_MovieMover(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		s.seedDownloadFile(t, "dl", "The.Mad.Phoenix.1997/The.Mad.Phoenix.en.ass",
			"[Script Info]\nTitle: The Mad Phoenix\n")

		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir: "dl",
			Files: []string{
				"The.Mad.Phoenix.1997/The.Mad.Phoenix.1997.mkv",
				"The.Mad.Phoenix.1997/The.Mad.Phoenix.en.ass",
				"The.Mad.Phoenix.1997/cover.jpg",
				"The.Mad.Phoenix.1997/behind the scenes.mp4.part",
			},
			Metadata: map[string]interface{}{"organizer_category": "movie", "title": "南海十三郎", "year": 1997},
		})

		require.Equal(t, 200, code, body)
		var resp model.PlanResponse
		decodeBody(t, code, body, &resp)
		assert.Nil(t, resp.Error)
		require.NotEmpty(t, resp.Plan)

		planMap := make(map[string]model.PlanAction)
		for _, act := range resp.Plan {
			planMap[act.File] = act
		}

		// Invariant checks: main video must move to movie library
		mkv, hasMkv := planMap["The.Mad.Phoenix.1997/The.Mad.Phoenix.1997.mkv"]
		require.True(t, hasMkv, "main video should be planned")
		assert.Equal(t, "move", mkv.Action)
		require.NotNil(t, mkv.Target)
		assert.True(t, strings.HasPrefix(*mkv.Target, "movie/"))
		assert.Contains(t, *mkv.Target, "南海十三郎")
		assert.Contains(t, *mkv.Target, "1997")
		assert.True(t, strings.HasSuffix(*mkv.Target, ".mkv"))

		// Companion subtitle should be moved next to video with language code
		ass, hasAss := planMap["The.Mad.Phoenix.1997/The.Mad.Phoenix.en.ass"]
		require.True(t, hasAss, "companion subtitle should be planned")
		assert.Equal(t, "move", ass.Action)
		require.NotNil(t, ass.Target)
		assert.True(t, strings.HasPrefix(*ass.Target, "movie/"))
		assert.Contains(t, *ass.Target, "南海十三郎")
		assert.True(t, strings.HasSuffix(*ass.Target, ".ass"))
		assert.Contains(t, *ass.Target, "eng")

		// Non-media/noise files should be skipped (if present in plan)
		if cover, ok := planMap["The.Mad.Phoenix.1997/cover.jpg"]; ok {
			assert.Equal(t, "skip", cover.Action)
			assert.Nil(t, cover.Target)
		}
		if part, ok := planMap["The.Mad.Phoenix.1997/behind the scenes.mp4.part"]; ok {
			assert.Equal(t, "skip", part.Action)
			assert.Nil(t, part.Target)
		}
	})
}

// TestE2E_ReleaseGroupNoiseMovieWithSample covers a complex release with
// release-group noise: the main feature is renamed into the library while the
// sample file is skipped.
func TestE2E_ReleaseGroupNoiseMovieWithSample(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		video := "[HDSky] The.Matrix.1999.1080p.BluRay.x264.DTS-WiKi/The.Matrix.1999.mkv"
		sample := "[HDSky] The.Matrix.1999.1080p.BluRay.x264.DTS-WiKi/The.Matrix.1999.sample.mkv"

		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir:   "matrixdl",
			Files: []string{video, sample},
			// No explicit organizer_category: LLM classifies it
			Metadata: map[string]interface{}{"title": "The Matrix", "year": 1999},
		})

		require.Equal(t, 200, code, body)
		var resp model.PlanResponse
		decodeBody(t, code, body, &resp)
		assert.Nil(t, resp.Error)
		require.Len(t, resp.Plan, 2)

		planMap := make(map[string]model.PlanAction)
		for _, act := range resp.Plan {
			planMap[act.File] = act
		}

		// Invariant: main video moves to movie library with clean title
		mkv := planMap[video]
		assert.Equal(t, "move", mkv.Action)
		require.NotNil(t, mkv.Target)
		assert.True(t, strings.HasPrefix(*mkv.Target, "movie/"))
		assert.Contains(t, *mkv.Target, "The Matrix")
		assert.Contains(t, *mkv.Target, "1999")
		assert.True(t, strings.HasSuffix(*mkv.Target, ".mkv"))

		// Invariant: sample file must be skipped
		sampleAct := planMap[sample]
		assert.Equal(t, "skip", sampleAct.Action)
		assert.Nil(t, sampleAct.Target)
	})
}
