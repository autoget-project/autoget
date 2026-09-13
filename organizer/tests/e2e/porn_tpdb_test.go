package e2e

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

// tpdbTokenFromEnv returns the ThePornDB API token from the environment and
// skips the test when it is not configured (same convention as
// .local/metadata-mcp/mcptools/theporndb_test.go). It must run inside
// runWithLiveProviders so getLiveTargets has already loaded .env.e2e.
func tpdbTokenFromEnv(t *testing.T) string {
	t.Helper()

	token := strings.TrimSpace(os.Getenv("TPDB_API_TOKEN"))
	if token == "" {
		t.Skip("TPDB_API_TOKEN environment variable not set")
	}
	return token
}

// TestE2E_PornTPDB drives the porn ThePornDB agent chain through POST /v1/plan
// against live providers and the real tpdb API: the performer-directory layer,
// the no-directory layer, title-word queries, the porn_vr root and the miss
// fallback. Assertions stay at shape level (prefix / structure / consistency)
// and never hardcode tpdb-derived scene titles.
func TestE2E_PornTPDB(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		tpdbTokenFromEnv(t)

		// Case 1: the scene hits tpdb and the performer already owns a
		// directory in the library -> porn/Agatha Vega/{title}/{title}.mp4.
		// A wrong shape (fallback, missing layer) fails loudly right here.
		t.Run("actor_dir_hit", func(t *testing.T) {
			const file = "TushyRaw.26.08.30.Agatha.Vega.XXX.1080p.mp4"

			actorDir := filepath.Join(s.targetDir, "porn", "Agatha Vega")
			require.NoError(t, os.MkdirAll(actorDir, 0o755))
			t.Cleanup(func() { _ = os.RemoveAll(actorDir) })

			target := planSingleFileTarget(t, s, "tpdb_actor_dir_hit", file, nil)

			require.True(t, strings.HasPrefix(target, "porn/Agatha Vega/"), "target %q", target)
			parts := strings.Split(target, "/")
			require.Len(t, parts, 4, "scene must be layered under the performer directory: %q", target)
			title := parts[2]
			assert.Equal(t, title+".mp4", parts[3], "file base must match the scene title directory: %q", target)
			assert.NotEqual(t, strings.TrimSuffix(file, filepath.Ext(file)), title, "scene title must not be the raw file stem: %q", target)
			// Intent-only soft check (not a shape invariant): the scene title
			// should reference the lead performer derived from the fixed input
			// filename. If ThePornDB ever renames the scene this may flake;
			// treat such a failure as a data change signal, not a regression.
			assert.Contains(t, strings.ToLower(title), "agatha", "scene title should reference the lead performer: %q", target)
		})

		// Case 2: an ambiguous dotted date YY.MM.DD (26.07.19, where either
		// group could be the year: 2026-07-19 or 2019-07-26) must not trap the
		// agent in one reading. The agent has to self-correct to the real scene
		// date; a wrong day-first reading matches nothing on tpdb and silently
		// degrades to the local naming chain (observed on grok when the prompt
		// pinned a single date order). With the performer directory present the
		// layered shape fails loudly whenever the agent gives up too early.
		t.Run("ambiguous_yy_mm_dd_date", func(t *testing.T) {
			const file = "Tushy.26.07.19.Azul.Hermosa.XXX.1080p.mp4"

			actorDir := filepath.Join(s.targetDir, "porn", "Azul Hermosa")
			require.NoError(t, os.MkdirAll(actorDir, 0o755))
			t.Cleanup(func() { _ = os.RemoveAll(actorDir) })

			target := planSingleFileTarget(t, s, "tpdb_ambiguous_date", file, nil)

			require.True(t, strings.HasPrefix(target, "porn/Azul Hermosa/"), "target %q", target)
			parts := strings.Split(target, "/")
			require.Len(t, parts, 4, "scene must be layered under the performer directory: %q", target)
			title := parts[2]
			assert.Equal(t, title+".mp4", parts[3], "file base must match the scene title directory: %q", target)
			// The tpdb scene title is a real descriptive sentence; the raw
			// dotted-date stem or the site+date fallback names must never leak
			// into the directory name (this is the regression being guarded).
			assert.NotContains(t, strings.ToLower(title), "26.07.19", "fallback dotted-date name must not leak: %q", target)
		})

		// Case 3: the scene hits tpdb but no performer directory exists ->
		// the performer layer must stay absent: porn/{title}/{title}.mp4.
		t.Run("no_actor_dir", func(t *testing.T) {
			const file = "TushyRaw.26.08.30.Agatha.Vega.XXX.1080p.mp4"

			target := planSingleFileTarget(t, s, "tpdb_no_actor_dir", file, nil)

			require.True(t, strings.HasPrefix(target, "porn/"), "target %q", target)
			parts := strings.Split(target, "/")
			require.Len(t, parts, 3, "performer layer must be absent without a pre-created directory: %q", target)
			assert.Equal(t, parts[1]+".mp4", parts[2], "directory and file base must stay consistent: %q", target)
		})

		// Case 4: a filename carrying scene title words hits the girlsway
		// scene via a title-word query: porn/{title}/{title}.mp4.
		t.Run("title_words", func(t *testing.T) {
			const file = "GirlsWay.26.09.06.Khloe.Kapri.Megan.Mistakes.Runaway.Brides.Regret.XXX.2160p.mp4"

			target := planSingleFileTarget(t, s, "tpdb_title_words", file, nil)

			require.True(t, strings.HasPrefix(target, "porn/"), "target %q", target)
			parts := strings.Split(target, "/")
			require.Len(t, parts, 3, "no performer directory exists: %q", target)
			assert.Equal(t, parts[1]+".mp4", parts[2], "directory and file base must stay consistent: %q", target)
			// Intent-only soft check (not a shape invariant): the scene title
			// words from the fixed input filename must survive into the target.
			// If ThePornDB ever renames the scene this may flake; treat such a
			// failure as a data change signal, not a regression.
			assert.Contains(t, strings.ToLower(parts[1]), "runaway", "name must keep the scene title words: %q", target)
		})

		// Case 5: a VR-marked western release must land under porn_vr/ with the
		// same shape rules: porn_vr/{name}/{name}.mp4. VR is a semantic
		// property declared at the API boundary via metadata.is_vr (a
		// downloader attaching a known VR release, mirroring how Stage 1
		// web-search grounding declares it through entities); the filename is
		// never pattern-matched for a VR token. The name itself carries a plain
		// VR marker but NO JAV VR label like DSVR: JAV labels in a western
		// filename are inherently ambiguous and let the Stage 1 bango checker
		// route the file to bango_porn (observed on grok with a DSVR-marked
		// sample).
		t.Run("vr_root", func(t *testing.T) {
			const file = "SlrOriginals.26.12.25.UnknownVrStar.Christmas.Special.VR.XXX.1080p.mp4"

			target := planSingleFileTarget(t, s, "tpdb_vr_root", file, map[string]interface{}{"is_vr": true})

			require.True(t, strings.HasPrefix(target, "porn_vr/"), "target %q", target)
			parts := strings.Split(target, "/")
			require.Len(t, parts, 3, "no performer directory exists under porn_vr: %q", target)
			assert.Equal(t, parts[1]+".mp4", parts[2], "directory and file base must stay consistent: %q", target)
		})

		// Case 6: an unfindable scene must degrade to the local naming chain:
		// porn/{name}/{name}.mp4. The sample keeps an unambiguous Western adult
		// studio (Tushy) plus gibberish scene words, so Stage 1 reliably
		// classifies porn while no such scene can exist on tpdb (an earlier
		// fully-synthetic token like CompletelyUnknownSceneXYZ made the
		// search-grounded classifier doubt the content type and flaked to
		// "unknown" on gemini).
		t.Run("miss_fallback", func(t *testing.T) {
			const file = "Tushy.26.01.01.Xyzzy.Scene.Zeta.XXX.1080p.mp4"

			target := planSingleFileTarget(t, s, "tpdb_miss_fallback", file, nil)

			require.True(t, strings.HasPrefix(target, "porn/"), "target %q", target)
			parts := strings.Split(target, "/")
			require.Len(t, parts, 3, "target %q", target)
			assert.Equal(t, parts[1]+".mp4", parts[2], "fallback must keep directory and file base consistent: %q", target)
		})
	})
}

// planSingleFileTarget posts a single-file /v1/plan request (metadata may be
// nil for cases without declared facts), checks the response contract and
// returns the move target. The generic assertPlanContract is not applicable
// here: tpdb-derived targets are only known as shapes (LLM output varies
// legally), so its exact-target equality is replaced by the per-case shape
// assertions on the returned target.
func planSingleFileTarget(t *testing.T, s *sandbox, dir, file string, metadata map[string]interface{}) string {
	t.Helper()

	code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
		Dir:      dir,
		Files:    []string{file},
		Metadata: metadata,
	})
	require.Equal(t, http.StatusOK, code, "plan must succeed: %s", body)
	var resp model.PlanResponse
	decodeBody(t, code, body, &resp)
	assert.Nil(t, resp.Error, "error must stay null in normal planning")
	require.Len(t, resp.Plan, 1, "single file must yield exactly one action: %+v", resp.Plan)

	act := resp.Plan[0]
	assert.Equal(t, file, act.File, "plan must address the requested file")
	require.Equal(t, "move", act.Action, "action: %+v", act)
	require.NotNil(t, act.Target, "move action must carry a target")
	return *act.Target
}
