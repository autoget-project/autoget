package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/service"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

// ExecuteHandler serves POST /v1/execute.
type ExecuteHandler struct {
	executor *service.Executor
	tracer   trace.Tracer
}

// NewExecuteHandler creates a new ExecuteHandler.
func NewExecuteHandler(e *service.Executor, tracer trace.Tracer) *ExecuteHandler {
	if tracer == nil {
		tracer = telemetry.Tracer()
	}
	return &ExecuteHandler{executor: e, tracer: tracer}
}

// Handle physically executes the plan. Any aggregated failed_move entry
// results in HTTP 400 carrying the full failure list; a fully successful run
// (including source directory archiving) returns HTTP 200 with an empty list.
func (h *ExecuteHandler) Handle(w http.ResponseWriter, r *http.Request) {
	tr := h.tracer
	if tr == nil {
		tr = telemetry.Tracer()
	}
	ctx, span := tr.Start(r.Context(), telemetry.SpanHTTPExecute)
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

	writeJSON(w, http.StatusOK, resp)
}
