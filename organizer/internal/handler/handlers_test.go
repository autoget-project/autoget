package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/mock"
	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	"github.com/autoget-project/autoget/organizer/internal/ptr"
	"github.com/autoget-project/autoget/organizer/internal/service"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

// env is an offline test server wiring every REST endpoint around a mock
// provider and isolated temporary directories.
type env struct {
	mux         http.Handler
	downloadDir string
	targetDir   string
	provider    *mock.Provider
	tp          *sdktrace.TracerProvider
	exp         *tracetest.InMemoryExporter
}

func newTestEnv(t *testing.T, prov *mock.Provider) *env {
	t.Helper()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()

	tp, exp := telemetry.NewTestTracerProvider()
	tracer := tp.Tracer("test-handler")

	pipe := pipeline.NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, targetDir, nil, tracer)
	exec := service.NewExecutor(downloadDir, targetDir)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/plan", NewPlanHandler(pipe, tracer).Handle)
	mux.HandleFunc("POST /v1/execute", NewExecuteHandler(exec, tracer).Handle)
	mux.HandleFunc("POST /v1/replan-with-hint", NewReplanHandler(prov, tracer).Handle)

	return &env{
		mux:         mux,
		downloadDir: downloadDir,
		targetDir:   targetDir,
		provider:    prov,
		tp:          tp,
		exp:         exp,
	}
}

func postJSON(t *testing.T, e *env, path string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v interface{}) {
	t.Helper()

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), v), "decode response body %q", rec.Body.String())
}

func TestPlanHandler_OKContract(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t, mock.NewProvider())

	// A pure-extension book hits the offline matcher: no LLM is involved at
	// all, so the handler is tested in complete isolation.
	rec := postJSON(t, e, "/v1/plan", model.APIPlanRequest{
		Dir:      "hash1",
		Files:    []string{"mybook.epub"},
		Metadata: map[string]interface{}{"title": "My Book"},
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var raw map[string]interface{}
	decodeBody(t, rec, &raw)

	// Contract: "error" must be present and null.
	errField, ok := raw["error"]
	require.True(t, ok, "error field must be present")
	assert.Nil(t, errField)

	plan, ok := raw["plan"].([]interface{})
	require.True(t, ok, "plan must be a list, got %v", raw["plan"])
	require.Len(t, plan, 1)

	action, ok := plan[0].(map[string]interface{})
	require.True(t, ok, "plan entry must be an object")
	assert.Equal(t, "mybook.epub", action["file"])
	assert.Equal(t, "move", action["action"])
	assert.Equal(t, "book/mybook.epub", action["target"])
}

func TestPlanHandler_FatalError500(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.SetDefaultResponse(nil, errors.New("categorizer offline"))
	e := newTestEnv(t, prov)

	// Unknown extension forces the Stage 1 LLM fallback, which fails fatally.
	rec := postJSON(t, e, "/v1/plan", model.APIPlanRequest{
		Dir:   "hash2",
		Files: []string{"mystery.bin"},
	})
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Error)
	assert.Contains(t, *resp.Error, "categorizer offline")
}

func TestPlanHandler_UnknownCategoryEmptyPlan200(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"unknown","reason":"junk","entities":{}}`,
	})
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/plan", model.APIPlanRequest{
		Dir:   "hash3",
		Files: []string{"mystery.bin"},
	})
	require.Equal(t, http.StatusOK, rec.Code, "unknown category is normal planning")

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	assert.Nil(t, resp.Error)
	assert.Empty(t, resp.Plan, "unknown category must return an empty plan")

	// Pin the wire contract: "plan" must serialize as [], not null.
	var raw map[string]json.RawMessage
	decodeBody(t, rec, &raw)
	planRaw, ok := raw["plan"]
	require.True(t, ok, "plan field must be present")
	assert.Equal(t, "[]", strings.TrimSpace(string(planRaw)))
}

func TestExecuteHandler_Success200AndArchive(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t, mock.NewProvider())

	require.NoError(t, os.MkdirAll(filepath.Join(e.downloadDir, "d1"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(e.downloadDir, "d1", "movie.mkv"), []byte("data"), 0o644))

	rec := postJSON(t, e, "/v1/execute", model.APIExecuteRequest{
		Dir: "d1",
		Plan: []model.PlanAction{
			{File: "movie.mkv", Action: "move", Target: ptr.Str("movie/Others/M (2000)/M (2000).mkv")},
		},
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.ExecuteResponse
	decodeBody(t, rec, &resp)
	assert.Empty(t, resp.FailedMove)

	assert.FileExists(t, filepath.Join(e.targetDir, "movie", "Others", "M (2000)", "M (2000).mkv"))
	assert.DirExists(t, filepath.Join(e.downloadDir, "archive", "d1"))
}

func TestExecuteHandler_PartialFailure400(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t, mock.NewProvider())

	require.NoError(t, os.MkdirAll(filepath.Join(e.downloadDir, "d2"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(e.downloadDir, "d2", "good.mkv"), []byte("data"), 0o644))

	rec := postJSON(t, e, "/v1/execute", model.APIExecuteRequest{
		Dir: "d2",
		Plan: []model.PlanAction{
			{File: "missing.mkv", Action: "move", Target: ptr.Str("movie/Others/M (2000)/M (2000).mkv")},
			{File: "good.mkv", Action: "move", Target: ptr.Str("movie/Others/M (2000)/M (2000) part.2.mkv")},
		},
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var resp model.ExecuteResponse
	decodeBody(t, rec, &resp)
	require.Len(t, resp.FailedMove, 1)
	assert.Equal(t, "missing.mkv", resp.FailedMove[0].File)
	assert.Equal(t, "file not found", resp.FailedMove[0].Reason)

	// The legal move must still have been executed, and a failed execution
	// must never archive the source directory.
	assert.FileExists(t, filepath.Join(e.targetDir, "movie", "Others", "M (2000)", "M (2000) part.2.mkv"))
	assert.NoDirExists(t, filepath.Join(e.downloadDir, "archive", "d2"))
}

func TestExecuteHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t, mock.NewProvider())

	req := httptest.NewRequest(http.MethodGet, "/v1/execute", nil)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestReplanHandler_TVDomainRouting(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a TV series file organization plan",
		Response: `{"plan":[
			{"file":"Show S01E01.mkv","action":"move","target":"tv_series/Others/Show (2020)/Season 02/Show (2020) S02E01.mkv"},
			{"file":"Show S01E02.mkv","action":"move","target":"../escape.mkv"}]}`,
	})
	e := newTestEnv(t, prov)

	prevTarget := "tv_series/Others/Show (2020)/Season 01/Show (2020) S01E01.mkv"
	rec := postJSON(t, e, "/v1/replan-with-hint", model.APIReplanRequest{
		Files:    []string{"Show S01E01.mkv", "Show S01E02.mkv"},
		Metadata: map[string]interface{}{"title": "Show"},
		PreviousResponse: &model.PlanResponse{Plan: []model.PlanAction{
			{File: "Show S01E01.mkv", Action: "move", Target: &prevTarget},
		}},
		UserHint: "these are actually season 2 episodes",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	assert.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 2, "every file must appear exactly once")

	// The domain (TV) replan prompt must be used, with the user hint injected.
	calls := prov.Calls()
	require.Len(t, calls, 1, "single replan must issue exactly one LLM call")
	assert.Contains(t, calls[0].Prompt, "revises a TV series file organization plan")
	assert.Contains(t, calls[0].Prompt, "these are actually season 2 episodes")

	byFile := map[string]model.PlanAction{}
	for _, a := range resp.Plan {
		byFile[a.File] = a
	}
	move := byFile["Show S01E01.mkv"]
	require.NotNil(t, move.Target)
	assert.Equal(t, "move", move.Action)
	assert.Equal(t, "tv_series/Others/Show (2020)/Season 02/Show (2020) S02E01.mkv", *move.Target)

	// Stage 4 security still applies to replans: traversal forced skip.
	skip := byFile["Show S01E02.mkv"]
	assert.Equal(t, "skip", skip.Action)
	assert.Nil(t, skip.Target)
}

func TestReplanHandler_EmptyPlanFallsBackToGenericPrompt(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises file organization plans based on user feedback",
		Response: `{"plan":[{"file":"movie.mkv","action":"move",
			"target":"movie/Others/Movie (2000)/Movie (2000).mkv"}]}`,
	})
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan-with-hint", model.APIReplanRequest{
		Files:            []string{"movie.mkv"},
		Metadata:         map[string]interface{}{"title": "Movie"},
		PreviousResponse: &model.PlanResponse{Plan: []model.PlanAction{}},
		UserHint:         "the year should be 2000, not 2024",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	calls := prov.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "revises file organization plans based on user feedback",
		"empty previous plan must fall back to the generic replan prompt")

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	assert.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)
	assert.Equal(t, "move", resp.Plan[0].Action)
}

func TestReplanHandler_NilPreviousResponseFallsBackToGenericPrompt(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises file organization plans based on user feedback",
		Response: `{"plan":[{"file":"movie.mkv","action":"move",
			"target":"movie/Others/Movie (2000)/Movie (2000).mkv"}]}`,
	})
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan-with-hint", model.APIReplanRequest{
		Files:            []string{"movie.mkv"},
		Metadata:         map[string]interface{}{"title": "Movie"},
		PreviousResponse: nil,
		UserHint:         "the year should be 2000, not 2024",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	calls := prov.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "revises file organization plans based on user feedback",
		"nil previous response must fall back to the generic replan prompt without panic")

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	assert.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)
	assert.Equal(t, "move", resp.Plan[0].Action)
}

func TestReplanHandler_UnknownRootFallsBackToGenericPrompt(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises file organization plans based on user feedback",
		Response:      `{"plan":[{"file":"a.mkv","action":"skip"}]}`,
	})
	e := newTestEnv(t, prov)

	unknownRoot := "weird_root/a.mkv"
	rec := postJSON(t, e, "/v1/replan-with-hint", model.APIReplanRequest{
		Files: []string{"a.mkv"},
		PreviousResponse: &model.PlanResponse{Plan: []model.PlanAction{
			{File: "a.mkv", Action: "move", Target: &unknownRoot},
		}},
		UserHint: "unknown domain",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	calls := prov.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "revises file organization plans based on user feedback",
		"uninferable root must fall back to the generic replan prompt")

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Plan, 1)
	assert.Equal(t, "skip", resp.Plan[0].Action)
	assert.Nil(t, resp.Plan[0].Target)
}

func TestReplanHandler_LLMFailure500(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.SetDefaultResponse(nil, errors.New("replanner offline"))
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan-with-hint", model.APIReplanRequest{
		Files:            []string{"a.mkv"},
		PreviousResponse: &model.PlanResponse{Plan: []model.PlanAction{}},
		UserHint:         "fix it",
	})
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Error)
	assert.Contains(t, *resp.Error, "replanner offline")
}

func TestReplanHandler_InvalidBody400(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t, mock.NewProvider())

	req := httptest.NewRequest(http.MethodPost, "/v1/replan-with-hint", strings.NewReader("{invalid"))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandlers_TraceHeaders(t *testing.T) {
	t.Parallel()

	e := newTestEnv(t, mock.NewProvider())

	// Test Plan handler returns X-Trace-Id
	recPlan := postJSON(t, e, "/v1/plan", model.APIPlanRequest{
		Dir:   "trace_test",
		Files: []string{"test.epub"},
	})
	assert.Equal(t, http.StatusOK, recPlan.Code)
	traceIDPlan := recPlan.Header().Get("X-Trace-Id")
	assert.NotEmpty(t, traceIDPlan, "X-Trace-Id header should be set on /v1/plan")
	assert.Len(t, traceIDPlan, 32, "trace ID should be 32 hex chars")

	// Test Execute handler returns X-Trace-Id
	recExec := postJSON(t, e, "/v1/execute", model.APIExecuteRequest{
		Dir:  "trace_test",
		Plan: []model.PlanAction{},
	})
	assert.Equal(t, http.StatusOK, recExec.Code)
	traceIDExec := recExec.Header().Get("X-Trace-Id")
	assert.NotEmpty(t, traceIDExec, "X-Trace-Id header should be set on /v1/execute")
	assert.Len(t, traceIDExec, 32, "trace ID should be 32 hex chars")

	// Test Replan handler returns X-Trace-Id
	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "file organization plans",
		Response:      `{"plan":[{"file":"a.mkv","action":"skip"}]}`,
	})
	eReplan := newTestEnv(t, prov)
	recReplan := postJSON(t, eReplan, "/v1/replan-with-hint", model.APIReplanRequest{
		Files:            []string{"a.mkv"},
		PreviousResponse: &model.PlanResponse{Plan: []model.PlanAction{}},
		UserHint:         "hint",
	})
	assert.Equal(t, http.StatusOK, recReplan.Code)
	traceIDReplan := recReplan.Header().Get("X-Trace-Id")
	assert.NotEmpty(t, traceIDReplan, "X-Trace-Id header should be set on /v1/replan-with-hint")
	assert.Len(t, traceIDReplan, 32, "trace ID should be 32 hex chars")
}

func TestPlanHandler_SummaryLog_And_TraceHeader(t *testing.T) {
	t.Parallel()

	var logLines []string
	summaryFn := func(format string, args ...any) {
		logLines = append(logLines, fmt.Sprintf(format, args...))
	}

	downloadDir := t.TempDir()
	targetDir := t.TempDir()
	tp, _ := telemetry.NewTestTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test-handler-summary")

	pipe := pipeline.NewPipeline(mock.NewProvider(), stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, targetDir, nil, tracer)
	planHandler := NewPlanHandler(pipe, tracer, WithPlanSummaryFn(summaryFn))

	reqBody := `{"dir":"dir1","files":["book.epub"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	planHandler.Handle(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	traceID := rec.Header().Get("X-Trace-Id")
	require.NotEmpty(t, traceID)
	require.Len(t, traceID, 32)

	require.Len(t, logLines, 1, "exactly one summary log line should be emitted")
	line := logLines[0]
	assert.Contains(t, line, "[PLAN]")
	assert.Contains(t, line, "trace_id="+traceID)
	assert.Contains(t, line, `dir="dir1"`)
	assert.Contains(t, line, "files=1")
	assert.Contains(t, line, "actions=1")
	assert.Contains(t, line, "status=OK")
	assert.Contains(t, line, "duration_ms=")
	assert.NotContains(t, line, `{"dir"`, "raw request JSON should not be dumped into summary log")
}

func TestPlanHandler_ErrorSummaryLog(t *testing.T) {
	t.Parallel()

	var logLines []string
	summaryFn := func(format string, args ...any) {
		logLines = append(logLines, fmt.Sprintf(format, args...))
	}

	downloadDir := t.TempDir()
	targetDir := t.TempDir()
	tp, _ := telemetry.NewTestTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test-handler-err-summary")

	prov := mock.NewProvider()
	prov.SetDefaultResponse(nil, errors.New("backend failed"))
	pipe := pipeline.NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, targetDir, nil, tracer)
	planHandler := NewPlanHandler(pipe, tracer, WithPlanSummaryFn(summaryFn))

	// Unknown extension forces LLM which fails
	reqBody := `{"dir":"err_dir","files":["mystery.bin"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/plan", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	planHandler.Handle(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	traceID := rec.Header().Get("X-Trace-Id")
	require.NotEmpty(t, traceID)

	require.Len(t, logLines, 1, "exactly one summary log line should be emitted on error")
	line := logLines[0]
	assert.Contains(t, line, "[PLAN]")
	assert.Contains(t, line, "trace_id="+traceID)
	assert.Contains(t, line, `dir="err_dir"`)
	assert.Contains(t, line, "files=1")
	assert.Contains(t, line, "status=ERROR")
	assert.Contains(t, line, "backend failed")
	assert.Contains(t, line, "duration_ms=")
}

// Compile-time interface guards.
var _ ai.Provider = (*mock.Provider)(nil)
