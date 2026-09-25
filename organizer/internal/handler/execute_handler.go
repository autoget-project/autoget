package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/service"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

// ExecuteHandlerOption configures optional settings on ExecuteHandler.
type ExecuteHandlerOption func(*ExecuteHandler)

// WithExecuteSummaryFn sets a custom summary logger function (used for testing without stdout pollution).
func WithExecuteSummaryFn(fn func(format string, args ...any)) ExecuteHandlerOption {
	return func(h *ExecuteHandler) {
		h.summaryFn = fn
	}
}

// ExecuteHandler serves POST /v1/execute.
type ExecuteHandler struct {
	executor  *service.Executor
	tracer    trace.Tracer
	summaryFn func(format string, args ...any)
}

// NewExecuteHandler creates a new ExecuteHandler.
func NewExecuteHandler(e *service.Executor, tracer trace.Tracer, opts ...ExecuteHandlerOption) *ExecuteHandler {
	if tracer == nil {
		tracer = telemetry.Tracer()
	}
	h := &ExecuteHandler{
		executor:  e,
		tracer:    tracer,
		summaryFn: log.Printf,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Handle physically executes the plan. Any aggregated failed_move entry
// results in HTTP 400 carrying the full failure list; a fully successful run
// (including source directory archiving) returns HTTP 200 with an empty list.
func (h *ExecuteHandler) Handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, span := h.tracer.Start(r.Context(), telemetry.SpanHTTPExecute)
	defer span.End()

	traceID := telemetry.TraceIDFromContext(ctx)
	if traceID != "" {
		w.Header().Set("X-Trace-Id", traceID)
	}

	var req model.APIExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	resp, err := h.executor.ExecutePlan(ctx, req.Dir, req.Plan)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		// Fatal internal failure (e.g. archive rename error) -> 500.
		http.Error(w, fmt.Sprintf("execute failed: %v", err), http.StatusInternalServerError)
		return
	}

	if len(resp.FailedMove) > 0 {
		span.SetStatus(codes.Error, "failed moves detected")
		writeJSON(w, http.StatusBadRequest, resp)
		return
	}

	if h.summaryFn != nil {
		h.summaryFn("[EXECUTE] trace_id=%s actions=%d status=OK duration_ms=%d",
			traceID, len(req.Plan), time.Since(start).Milliseconds())
	}
	writeJSON(w, http.StatusOK, resp)
}
