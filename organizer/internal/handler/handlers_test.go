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
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer("test-handler")

	pipe := pipeline.NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, targetDir, nil, tracer)
	exec := service.NewExecutor(downloadDir, targetDir)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/plan", NewPlanHandler(pipe, tracer).Handle)
	mux.HandleFunc("POST /v1/execute", NewExecuteHandler(exec, tracer).Handle)
	mux.HandleFunc("POST /v1/replan", NewReplanHandler(pipe, tracer).Handle)
	mux.HandleFunc("POST /v1/replan-with-hint", NewReplanWithHintHandler(pipe, tracer).Handle)

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

	// Verify handler span hierarchy: http.plan is parent, pipeline.CreatePlan is child
	spans := e.exp.GetSpans()
	require.NotEmpty(t, spans)
	var httpPlanSpan, pipeSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == telemetry.SpanHTTPPlan {
			httpPlanSpan = &spans[i]
		}
		if spans[i].Name == telemetry.SpanPipelineCreatePlan {
			pipeSpan = &spans[i]
		}
	}
	require.NotNil(t, httpPlanSpan, "organizer.http.plan span should exist")
	require.NotNil(t, pipeSpan, "organizer.pipeline.CreatePlan span should exist")
	assert.Equal(t, httpPlanSpan.SpanContext.SpanID(), pipeSpan.Parent.SpanID(), "pipeline span must be child of http span")
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
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic naming","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a TV series file organization plan",
		Response: `{"plan":[
			{"file":"Show S01E01.mkv","action":"move","target":"tv_series/Others/Show (2020)/Season 02/Show (2020) S02E01.mkv"},
			{"file":"Show S01E02.mkv","action":"move","target":"../escape.mkv"}]}`,
	})
	e := newTestEnv(t, prov)

	prevTarget := "tv_series/Others/Show (2020)/Season 01/Show (2020) S01E01.mkv"
	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Files:    []string{"Show S01E01.mkv", "Show S01E02.mkv"},
		Metadata: map[string]interface{}{"title": "Show"},
		PreviousResult: &model.PlanResponse{Plan: []model.PlanAction{
			{File: "Show S01E01.mkv", Action: "move", Target: &prevTarget},
		}},
		UserHint: "these are actually season 2 episodes",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	assert.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 2, "every file must appear exactly once")

	// Stage 1 re-classifies, then the domain (TV) replan prompt is used with the
	// user hint and the flawed previous plan attached as suspect context.
	calls := prov.Calls()
	require.Len(t, calls, 2, "expect a Stage 1 reclassification call and a replan planning call")
	assert.Contains(t, calls[0].Prompt, "media categorization assistant")
	assert.Contains(t, calls[1].Prompt, "revises a TV series file organization plan")
	assert.Contains(t, calls[1].Prompt, "these are actually season 2 episodes")
	assert.Contains(t, calls[1].Prompt, prevTarget)

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

func TestReplanHandler_NonDomainCategoryUsesGenericPrompt(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"photobook","reason":"image set","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "revises file organization plans based on user feedback",
		Response:      `{"plan":[{"file":"a.mkv","action":"skip"}]}`,
	})
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Files:    []string{"a.mkv"},
		Metadata: map[string]interface{}{"title": "Album"},
		UserHint: "unknown domain",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	calls := prov.Calls()
	require.Len(t, calls, 2)
	assert.Contains(t, calls[1].Prompt, "revises file organization plans based on user feedback")

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Len(t, resp.Plan, 1)
	assert.Equal(t, "skip", resp.Plan[0].Action)
	assert.Nil(t, resp.Plan[0].Target)
}

func TestReplanHandler_RunsStage4SubtitlePairing(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"movie","reason":"single feature","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a movie file organization plan",
		Response: `{"plan":[{"file":"movie.mkv","action":"move",
			"target":"movie/Others/Movie (2000)/Movie (2000).mkv"}]}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Response: `{"plan":[{"file":"movie.chs.srt","action":"move",
			"matched_video":"movie.mkv","language":"Chinese"}]}`,
	})
	e := newTestEnv(t, prov)

	// Seed the subtitle on disk so Stage 4 can read its preview.
	subDir := filepath.Join(e.downloadDir, "replan_sub")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "movie.chs.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\n你好\n"), 0o644))

	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Dir:      "replan_sub",
		Files:    []string{"movie.mkv", "movie.chs.srt"},
		Metadata: map[string]interface{}{"title": "Movie"},
		UserHint: "fix the title",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 2)

	byFile := map[string]model.PlanAction{}
	for _, a := range resp.Plan {
		byFile[a.File] = a
	}
	sub := byFile["movie.chs.srt"]
	require.NotNil(t, sub.Target, "Stage 4 must pair the companion subtitle during a replan")
	assert.Equal(t,
		"movie/Others/Movie (2000)/Movie (2000).简体中文.chi.srt", *sub.Target)
}

func TestReplanHandler_LLMFailure500(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.SetDefaultResponse(nil, errors.New("replanner offline"))
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Files:          []string{"a.mkv"},
		PreviousResult: &model.PlanResponse{Plan: []model.PlanAction{}},
		UserHint:       "fix it",
	})
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.NotNil(t, resp.Error)
	assert.Contains(t, *resp.Error, "replanner offline")
}

func TestReplanWithHintHandler_LegacyWireShape(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a bango (JAV) file organization plan",
		Response:      `{"plan":[{"file":"SSIS-001.mp4","action":"move","target":"jav/Actress/SSIS-001.mp4"}]}`,
	})
	e := newTestEnv(t, prov)

	prevTarget := "porn/SSIS-001/SSIS-001.mp4"
	rec := postJSON(t, e, "/v1/replan-with-hint", model.APIReplanWithHintRequest{
		Files: []string{"SSIS-001.mp4"},
		PreviousResponse: &model.PlanResponse{Plan: []model.PlanAction{
			{File: "SSIS-001.mp4", Action: "move", Target: &prevTarget},
		}},
		UserHint: "this is a JAV",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)
	require.NotNil(t, resp.Plan[0].Target)
	assert.Equal(t, "jav/Actress/SSIS-001.mp4", *resp.Plan[0].Target)

	// The legacy endpoint delegates to the same pipeline path: a bango filename
	// rule-matches Stage 1, and the previous plan reaches the replan prompt.
	calls := prov.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "revises a bango (JAV) file organization plan")
	assert.Contains(t, calls[0].Prompt, prevTarget)
}

func TestReplanHandler_InvalidBody400(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/v1/replan", "/v1/replan-with-hint"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			e := newTestEnv(t, mock.NewProvider())

			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{invalid"))
			rec := httptest.NewRecorder()
			e.mux.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
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
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"photobook","reason":"image set","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "file organization plans",
		Response:      `{"plan":[{"file":"a.mkv","action":"skip"}]}`,
	})
	eReplan := newTestEnv(t, prov)
	recReplan := postJSON(t, eReplan, "/v1/replan", model.APIReplanRequest{
		Files:          []string{"a.mkv"},
		PreviousResult: &model.PlanResponse{Plan: []model.PlanAction{}},
		UserHint:       "hint",
	})
	assert.Equal(t, http.StatusOK, recReplan.Code)
	traceIDReplan := recReplan.Header().Get("X-Trace-Id")
	assert.NotEmpty(t, traceIDReplan, "X-Trace-Id header should be set on /v1/replan")
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

	require.Len(t, logLines, 2, "request log and summary log lines should be emitted")
	reqLine := logLines[0]
	assert.Contains(t, reqLine, "[PLAN_REQ]")
	assert.Contains(t, reqLine, `{"dir":"dir1","files":["book.epub"]}`)

	planLine := logLines[1]
	assert.Contains(t, planLine, "[PLAN]")
	assert.Contains(t, planLine, "trace_id="+traceID[:8])
	assert.Contains(t, planLine, `dir="dir1"`)
	assert.Contains(t, planLine, "files=1")
	assert.Contains(t, planLine, "actions=1")
	assert.Contains(t, planLine, "status=OK")
	assert.Contains(t, planLine, "duration_ms=")
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

	require.Len(t, logLines, 2, "request log and summary log line should be emitted on error")
	assert.Contains(t, logLines[0], "[PLAN_REQ]")
	assert.Contains(t, logLines[0], `{"dir":"err_dir","files":["mystery.bin"]}`)

	line := logLines[1]
	assert.Contains(t, line, "[PLAN]")
	assert.Contains(t, line, "trace_id="+traceID[:8])
	assert.Contains(t, line, `dir="err_dir"`)
	assert.Contains(t, line, "files=1")
	assert.Contains(t, line, "status=ERROR")
	assert.Contains(t, line, "backend failed")
	assert.Contains(t, line, "duration_ms=")
}

// Compile-time interface guards.
var _ ai.Provider = (*mock.Provider)(nil)

func TestReplanHandler_ReclassifiesPornToBango(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a bango (JAV) file organization plan",
		Response:      `{"plan":[{"file":"NAAC-076.mp4","action":"move","target":"jav/YUUKA/NAAC-076.mp4"}]}`,
	})
	e := newTestEnv(t, prov)

	prevTarget := "porn/NAAC-076/NAAC-076.mp4"
	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Files: []string{"NAAC-076.mp4"},
		Metadata: map[string]interface{}{
			"organizer_category": []string{"porn"},
			"dmm_id":             "n_1541naac076tk",
			"actors":             []string{"YUUKA"},
			"title":              "NAAC-076 【数量限定】Best naked/YUUKA チェキ付き",
		},
		PreviousResult: &model.PlanResponse{Plan: []model.PlanAction{
			{File: "NAAC-076.mp4", Action: "move", Target: &prevTarget},
		}},
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)
	require.NotNil(t, resp.Plan[0].Target)
	assert.Equal(t, "jav/YUUKA/NAAC-076.mp4", *resp.Plan[0].Target)

	calls := prov.Calls()
	require.Len(t, calls, 1, "dmm_id reclassification must short-circuit the Stage 1 LLM")
	assert.Contains(t, calls[0].Prompt, "revises a bango (JAV) file organization plan")
	assert.Contains(t, calls[0].Prompt, "porn/NAAC-076/NAAC-076.mp4",
		"the flawed previous plan must be supplied as suspect context")
	assert.Contains(t, calls[0].Prompt, "n_1541naac076tk")
	assert.NotContains(t, calls[0].Prompt, "organizer_category",
		"the stale upstream classification must not be forwarded")
}

func TestReplanHandler_ReclassifiesViaLLMThenPlans(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"movie","reason":"single feature","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a movie file organization plan",
		Response:      `{"plan":[{"file":"movie.mkv","action":"move","target":"movie/Chinese/正确的电影 (2022)/正确的电影 (2022).mkv"}]}`,
	})
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Files:          []string{"movie.mkv"},
		Metadata:       map[string]interface{}{"title": "错误的名字", "year": 2020.0},
		PreviousResult: nil,
		UserHint:       "the title is wrong, it should be 正确的电影 and the year is 2022",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)
	require.NotNil(t, resp.Plan[0].Target)
	assert.Equal(t, "movie/Chinese/正确的电影 (2022)/正确的电影 (2022).mkv", *resp.Plan[0].Target)

	calls := prov.Calls()
	require.Len(t, calls, 2, "expect a Stage 1 reclassification call and a replan planning call")
	assert.Contains(t, calls[0].Prompt, "media categorization assistant")
	assert.Contains(t, calls[0].Prompt, "the year is 2022", "user hint must reach the reclassification")
	assert.Contains(t, calls[1].Prompt, "revises a movie file organization plan")
	assert.Contains(t, calls[1].Prompt, "the year is 2022", "user hint must reach the planner")
}

func TestReplanHandler_MissingPreviousResultIsSafe(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a bango (JAV) file organization plan",
		Response:      `{"plan":[{"file":"SSIS-001.mp4","action":"move","target":"jav/Actress/SSIS-001.mp4"}]}`,
	})
	e := newTestEnv(t, prov)

	rec := postJSON(t, e, "/v1/replan", model.APIReplanRequest{
		Files:          []string{"SSIS-001.mp4"},
		PreviousResult: nil,
		UserHint:       "fix it",
	})
	require.Equal(t, http.StatusOK, rec.Code)

	var resp model.PlanResponse
	decodeBody(t, rec, &resp)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)

	calls := prov.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "revises a bango (JAV) file organization plan")
}
