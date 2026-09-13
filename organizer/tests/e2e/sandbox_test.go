// Package e2e hosts the full-scenario end-to-end test suite. It drives the
// real HTTP surface (/v1/plan, /v1/execute, /v1/replan-with-hint) inside an
// isolated sandbox (t.TempDir) using live LLM providers (configured via
// .env.e2e) whenever E2E_TEST=1 is set, verifying the real AI reasoning and
// physical execution of every pipeline stage over the wire.
// Tests gracefully skip when E2E_TEST=1 is not set.
package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/gemini"
	"github.com/autoget-project/autoget/organizer/internal/ai/grok"
	"github.com/autoget-project/autoget/organizer/internal/handler"
	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	stage3planner "github.com/autoget-project/autoget/organizer/internal/pipeline/stage3_planner"
	"github.com/autoget-project/autoget/organizer/internal/ptr"
	"github.com/autoget-project/autoget/organizer/internal/service"
	"github.com/autoget-project/autoget/organizer/internal/testutil"
)

// liveTarget defines a configured real LLM provider target.
type liveTarget struct {
	name    string
	newProv func(t *testing.T) ai.Provider
}

// getLiveTargets loads .env.e2e and discovers configured live AI providers.
// Skips the test if E2E_TEST != "1" or no live provider is configured.
func getLiveTargets(t *testing.T) []liveTarget {
	t.Helper()

	if os.Getenv("E2E_TEST") != "1" {
		t.Skip("Skipping E2E test: E2E_TEST=1 is not set")
	}

	testutil.LoadEnvFile(t, filepath.Join("..", "..", ".env.e2e"))

	var targets []liveTarget

	xaiModel := strings.TrimSpace(os.Getenv("XAI_MODEL"))
	xaiKey := strings.TrimSpace(os.Getenv("XAI_API_KEY"))
	if xaiModel != "" && xaiKey != "" {
		targets = append(targets, liveTarget{
			name: "grok_" + xaiModel,
			newProv: func(t *testing.T) ai.Provider {
				t.Helper()
				return grok.NewProvider(xaiKey, ai.WithModel(xaiModel))
			},
		})
	}

	geminiModel := strings.TrimSpace(os.Getenv("GEMINI_MODEL"))
	geminiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if geminiModel != "" && geminiKey != "" {
		targets = append(targets, liveTarget{
			name: "gemini_" + geminiModel,
			newProv: func(t *testing.T) ai.Provider {
				t.Helper()
				prov, err := gemini.NewProvider(geminiKey, ai.WithModel(geminiModel))
				require.NoError(t, err)
				return prov
			},
		})
	}

	if len(targets) == 0 {
		t.Skip("Skipping E2E test: neither XAI (XAI_MODEL+XAI_API_KEY) nor Gemini (GEMINI_MODEL+GEMINI_API_KEY) configured")
	}

	return targets
}

// runWithLiveProviders runs testFn against all configured live AI providers.
func runWithLiveProviders(t *testing.T, testFn func(t *testing.T, s *sandbox)) {
	t.Helper()

	targets := getLiveTargets(t)
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()
			prov := target.newProv(t)
			s := newSandbox(t, prov)
			testFn(t, s)
		})
	}
}

// runWithLiveAIProviders passes the live ai.Provider directly to testFn without spinning up HTTP server.
func runWithLiveAIProviders(t *testing.T, testFn func(t *testing.T, prov ai.Provider)) {
	t.Helper()

	targets := getLiveTargets(t)
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()
			prov := target.newProv(t)
			testFn(t, prov)
		})
	}
}

// runWithLiveProvidersAndActorStore runs testFn against all configured live AI providers with a populated actor store.
func runWithLiveProvidersAndActorStore(t *testing.T, actorFile string, testFn func(t *testing.T, s *sandbox, store *stage2enricher.ActorStore)) {
	t.Helper()

	targets := getLiveTargets(t)
	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()
			prov := target.newProv(t)
			store := stage2enricher.NewActorStore(actorFile, "", prov)
			s := newSandboxWithActorStore(t, prov, store)
			testFn(t, s, store)
		})
	}
}

// sandbox is an isolated E2E environment: a live HTTP server exposing the
// REST routes, backed by temporary DOWNLOAD_COMPLETED_DIR / TARGET_DIR roots
// with every TargetDir enum subdirectory pre-created.
type sandbox struct {
	server      *httptest.Server
	downloadDir string
	targetDir   string
}

// newSandbox wires the full server around the given provider inside a fresh
// sandbox. All model.TargetDirs subdirectories are pre-created so a
// StartupCheck-equivalent validation would never report missing dirs.
func newSandbox(t *testing.T, prov ai.Provider) *sandbox {
	t.Helper()
	return newSandboxWithActorStore(t, prov, nil)
}

// newSandboxWithActorStore wires the full server around the given provider and actor store.
func newSandboxWithActorStore(t *testing.T, prov ai.Provider, store *stage2enricher.ActorStore) *sandbox {
	t.Helper()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()

	for _, d := range model.AllTargetDirs {
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, string(d)), 0o755),
			"pre-create target subdirectory %s", d)
	}

	// Stage 2 runs with optional actorStore and nil external network sources.
	// The tpdb source is wired whenever TPDB_API_TOKEN is configured (loaded
	// from .env.e2e by getLiveTargets before this constructor runs); a missing
	// token keeps the explicit nil interface so the porn category silently
	// degrades to the local naming chain and every other e2e case is
	// unaffected.
	var tpdb stage3planner.PornSource
	if tok := strings.TrimSpace(os.Getenv("TPDB_API_TOKEN")); tok != "" {
		tpdb = metadata.NewThePornDB(tok)
	}
	pipe := pipeline.NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, store, prov), downloadDir, targetDir, tpdb)
	exec := service.NewExecutor(downloadDir, targetDir)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/plan", handler.NewPlanHandler(pipe).Handle)
	mux.HandleFunc("POST /v1/execute", handler.NewExecuteHandler(exec).Handle)
	mux.HandleFunc("POST /v1/replan-with-hint", handler.NewReplanHandler(prov).Handle)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &sandbox{
		server:      server,
		downloadDir: downloadDir,
		targetDir:   targetDir,
	}
}

// seedDownloadFile materializes a file inside DOWNLOAD_COMPLETED_DIR/{dir}.
func (s *sandbox) seedDownloadFile(t *testing.T, dir, file, content string) {
	t.Helper()

	full := filepath.Join(s.downloadDir, dir, file)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644), "seed file %s", full)
}

// postJSON performs a real HTTP round trip against the sandbox server and
// returns the status code plus raw body.
func (s *sandbox) postJSON(t *testing.T, path string, payload interface{}) (int, string) {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := http.Post(s.server.URL+path, "application/json", bytes.NewReader(body))
	require.NoError(t, err, "POST %s", path)
	t.Cleanup(func() { _ = resp.Body.Close() })

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(resp.Body)
	require.NoError(t, err, "read response body")
	return resp.StatusCode, buf.String()
}

func decodeBody(t *testing.T, code int, body string, v interface{}) {
	t.Helper()

	require.NoError(t, json.Unmarshal([]byte(body), v), "decode response (status %d) %q", code, body)
}

// assertPlanContract checks the API response contract: HTTP 200, error null,
// every input file appears exactly once, and each action matches the
// expected mapping (file -> action/target). Skip actions must serialize a
// null target.
func assertPlanContract(t *testing.T, code int, body string, want map[string]model.PlanAction) {
	t.Helper()

	require.Equal(t, http.StatusOK, code, "plan must succeed: %s", body)
	var resp model.PlanResponse
	decodeBody(t, code, body, &resp)
	assert.Nil(t, resp.Error, "error must stay null in normal planning")
	require.Len(t, resp.Plan, len(want), "plan must cover every file exactly once: %+v", resp.Plan)

	got := make(map[string]model.PlanAction, len(resp.Plan))
	for _, a := range resp.Plan {
		got[a.File] = a
	}

	files := make([]string, 0, len(want))
	for f := range want {
		files = append(files, f)
	}
	sort.Strings(files)

	for _, f := range files {
		w := want[f]
		g, ok := got[f]
		require.True(t, ok, "file %q missing from plan: %+v", f, resp.Plan)
		assert.Equal(t, w.Action, g.Action, "file %q action", f)
		if w.Action == "move" {
			require.NotNil(t, g.Target, "file %q: move action must carry a target", f)
			assert.Equal(t, *w.Target, *g.Target, "file %q target", f)
		} else {
			assert.Nil(t, g.Target, "file %q: skip action must serialize target null", f)
		}
	}
}

// wantMove builds the expected move action entry.
func wantMove(target string) model.PlanAction {
	return model.PlanAction{Action: "move", Target: ptr.Str(target)}
}
