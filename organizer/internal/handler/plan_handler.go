// Package handler implements the REST route layer (/v1/plan, /v1/execute,
// /v1/replan-with-hint) of the organizer service.
package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/pipeline"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

// PlanHandlerOption configures optional settings on PlanHandler.
type PlanHandlerOption func(*PlanHandler)

// WithPlanSummaryFn sets a custom summary logger function (used for testing without stdout pollution).
func WithPlanSummaryFn(fn func(format string, args ...any)) PlanHandlerOption {
	return func(h *PlanHandler) {
		h.summaryFn = fn
	}
}

// PlanHandler serves POST /v1/plan.
type PlanHandler struct {
	pipeline  *pipeline.Pipeline
	tracer    trace.Tracer
	summaryFn func(format string, args ...any)
}

// NewPlanHandler creates a new PlanHandler.
func NewPlanHandler(p *pipeline.Pipeline, tracer trace.Tracer, opts ...PlanHandlerOption) *PlanHandler {
	if tracer == nil {
		tracer = telemetry.Tracer()
	}
	h := &PlanHandler{
		pipeline:  p,
		tracer:    tracer,
		summaryFn: log.Printf,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Handle processes a plan creation request through the full 4-stage pipeline.
// Fatal unrecoverable internal errors map to HTTP 500; normal planning (including
// the unknown category) keeps the response contract with error set to null.
func (h *PlanHandler) Handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	tr := h.tracer
	if tr == nil {
		tr = telemetry.Tracer()
	}
	ctx, span := tr.Start(r.Context(), telemetry.SpanHTTPPlan)
	defer span.End()

	traceID := telemetry.TraceIDFromContext(ctx)
	if traceID != "" {
		w.Header().Set("X-Trace-Id", traceID)
	}

	var req model.APIPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if h.summaryFn != nil {
			h.summaryFn("[PLAN] trace_id=%s dir=%q files=%d status=ERROR err=%q duration_ms=%d",
				traceID, req.Dir, len(req.Files), err.Error(), time.Since(start).Milliseconds())
		}
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	span.SetAttributes(
		attribute.String(telemetry.AttrOrganizerDir, req.Dir),
		attribute.Int(telemetry.AttrOrganizerFilesCount, len(req.Files)),
	)

	resp, err := h.pipeline.CreatePlan(ctx, req.Dir, req.Files, req.Metadata)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if h.summaryFn != nil {
			h.summaryFn("[PLAN] trace_id=%s dir=%q files=%d status=ERROR err=%q duration_ms=%d",
				traceID, req.Dir, len(req.Files), err.Error(), time.Since(start).Milliseconds())
		}
		// Fatal internal failure -> 500 while preserving the response shape.
		msg := err.Error()
		writeJSON(w, http.StatusInternalServerError, model.PlanResponse{Plan: []model.PlanAction{}, Error: &msg})
		return
	}

	if h.summaryFn != nil {
		h.summaryFn("[PLAN] trace_id=%s dir=%q files=%d actions=%d status=OK duration_ms=%d",
			traceID, req.Dir, len(req.Files), len(resp.Plan), time.Since(start).Milliseconds())
	}
	// Normal planning: error stays null (contract compatibility).
	writeJSON(w, http.StatusOK, resp)
}

// writeJSON serializes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers are already sent; nothing more can be recovered here.
		return
	}
}
