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
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

// ReplanHandler serves POST /v1/replan. It is thin transport: the pipeline
// re-runs Stage 1 classification (rules + LLM, cheap), re-plans with the
// previous (flawed) plan and the user hint as deductively suspect context, and
// finalizes through Stage 4 (subtitle pairing + sanitization). Stage 2 external
// metadata lookups are skipped.
type ReplanHandler struct {
	pipeline  *pipeline.Pipeline
	tracer    trace.Tracer
	summaryFn func(format string, args ...any)
}

// NewReplanHandler creates a new ReplanHandler.
func NewReplanHandler(p *pipeline.Pipeline, tracer trace.Tracer) *ReplanHandler {
	if tracer == nil {
		tracer = telemetry.Tracer()
	}
	return &ReplanHandler{
		pipeline:  p,
		tracer:    tracer,
		summaryFn: log.Printf,
	}
}

// Handle decodes the replan request and delegates to the pipeline.
func (h *ReplanHandler) Handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, span := h.tracer.Start(r.Context(), telemetry.SpanHTTPReplan)
	defer span.End()

	traceID := telemetry.TraceIDFromContext(ctx)
	if traceID != "" {
		w.Header().Set("X-Trace-Id", traceID)
	}

	var req model.APIReplanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	resp, err := h.pipeline.Replan(ctx, req.Dir, req.Files, req.Metadata, model.ReplanContext{
		PreviousPlan: previousPlanActions(req.PreviousResult),
		UserHint:     req.UserHint,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := err.Error()
		writeJSON(w, http.StatusInternalServerError, model.PlanResponse{Plan: []model.PlanAction{}, Error: &msg})
		return
	}

	if h.summaryFn != nil {
		h.summaryFn("[REPLAN] trace_id=%s dir=%q files=%d actions=%d status=OK duration_ms=%d",
			shortID(traceID, 8), shortID(req.Dir, 8), len(req.Files), len(resp.Plan), time.Since(start).Milliseconds())
	}
	writeJSON(w, http.StatusOK, resp)
}

// previousPlanActions extracts the actions of the previous (flawed) response.
func previousPlanActions(resp *model.PlanResponse) []model.PlanAction {
	if resp == nil {
		return nil
	}
	return resp.Plan
}

// ReplanWithHintHandler serves the legacy POST /v1/replan-with-hint endpoint.
// It accepts the historical request shape and delegates to the same pipeline
// replan path, so an old client gets the corrected behavior (Stage 1 is re-run
// instead of trusting the stale classification).
type ReplanWithHintHandler struct {
	pipeline  *pipeline.Pipeline
	tracer    trace.Tracer
	summaryFn func(format string, args ...any)
}

// NewReplanWithHintHandler creates a new ReplanWithHintHandler.
func NewReplanWithHintHandler(p *pipeline.Pipeline, tracer trace.Tracer) *ReplanWithHintHandler {
	if tracer == nil {
		tracer = telemetry.Tracer()
	}
	return &ReplanWithHintHandler{
		pipeline:  p,
		tracer:    tracer,
		summaryFn: log.Printf,
	}
}

// Handle adapts the legacy request to the unified replan path.
func (h *ReplanWithHintHandler) Handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, span := h.tracer.Start(r.Context(), telemetry.SpanHTTPReplan)
	defer span.End()

	traceID := telemetry.TraceIDFromContext(ctx)
	if traceID != "" {
		w.Header().Set("X-Trace-Id", traceID)
	}

	var req model.APIReplanWithHintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	resp, err := h.pipeline.Replan(ctx, req.Dir, req.Files, req.Metadata, model.ReplanContext{
		PreviousPlan: previousPlanActions(req.PreviousResponse),
		UserHint:     req.UserHint,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		msg := err.Error()
		writeJSON(w, http.StatusInternalServerError, model.PlanResponse{Plan: []model.PlanAction{}, Error: &msg})
		return
	}

	if h.summaryFn != nil {
		h.summaryFn("[REPLAN] trace_id=%s dir=%q files=%d actions=%d status=OK duration_ms=%d",
			shortID(traceID, 8), shortID(req.Dir, 8), len(req.Files), len(resp.Plan), time.Since(start).Milliseconds())
	}
	writeJSON(w, http.StatusOK, resp)
}
