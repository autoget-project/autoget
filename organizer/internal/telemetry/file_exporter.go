package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SpanRecord represents a light-weight structured record of an exported span in JSON Lines format.
type SpanRecord struct {
	TraceID      string                 `json:"trace_id"`
	SpanID       string                 `json:"span_id"`
	ParentSpanID string                 `json:"parent_span_id,omitempty"`
	Name         string                 `json:"name"`
	StartTime    time.Time              `json:"start_time"`
	EndTime      time.Time              `json:"end_time"`
	DurationMs   float64                `json:"duration_ms"`
	Attributes   map[string]interface{} `json:"attributes,omitempty"`
	Events       []SpanEventRecord      `json:"events,omitempty"`
	Status       SpanStatusRecord       `json:"status"`
}

// SpanEventRecord represents an event within a span.
type SpanEventRecord struct {
	Name       string                 `json:"name"`
	Time       time.Time              `json:"time"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// SpanStatusRecord represents the status of a span.
type SpanStatusRecord struct {
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
}

// FileSpanExporter implements sdktrace.SpanExporter by appending JSON Lines to a local file.
type FileSpanExporter struct {
	filePath string
	mu       sync.Mutex
	file     *os.File
}

// NewFileSpanExporter creates a FileSpanExporter writing to filePath.
func NewFileSpanExporter(filePath string) (*FileSpanExporter, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory for trace file %s: %w", filePath, err)
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open trace file %s: %w", filePath, err)
	}

	return &FileSpanExporter{
		filePath: filePath,
		file:     f,
	}, nil
}

// ExportSpans converts spans to SpanRecord and appends each as a JSON line to the file.
func (e *FileSpanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if len(spans) == 0 {
		return nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, span := range spans {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		record := e.convertSpan(span)
		line, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal span record: %w", err)
		}

		line = append(line, '\n')
		if _, err := e.file.Write(line); err != nil {
			return fmt.Errorf("failed to write span record to %s: %w", e.filePath, err)
		}
	}

	return nil
}

// Shutdown flushes and closes the file.
func (e *FileSpanExporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.file != nil {
		err := e.file.Close()
		e.file = nil
		return err
	}
	return nil
}

func (e *FileSpanExporter) convertSpan(span sdktrace.ReadOnlySpan) SpanRecord {
	sCtx := span.SpanContext()

	var parentSpanID string
	if span.Parent().IsValid() {
		parentSpanID = span.Parent().SpanID().String()
	}

	duration := span.EndTime().Sub(span.StartTime())
	durationMs := float64(duration.Microseconds()) / 1000.0

	var attrs map[string]interface{}
	if len(span.Attributes()) > 0 {
		attrs = make(map[string]interface{}, len(span.Attributes()))
		for _, attr := range span.Attributes() {
			attrs[string(attr.Key)] = attr.Value.AsInterface()
		}
	}

	var events []SpanEventRecord
	if len(span.Events()) > 0 {
		events = make([]SpanEventRecord, 0, len(span.Events()))
		for _, ev := range span.Events() {
			var evAttrs map[string]interface{}
			if len(ev.Attributes) > 0 {
				evAttrs = make(map[string]interface{}, len(ev.Attributes))
				for _, a := range ev.Attributes {
					evAttrs[string(a.Key)] = a.Value.AsInterface()
				}
			}
			events = append(events, SpanEventRecord{
				Name:       ev.Name,
				Time:       ev.Time,
				Attributes: evAttrs,
			})
		}
	}

	status := SpanStatusRecord{
		Code:        span.Status().Code.String(),
		Description: span.Status().Description,
	}

	return SpanRecord{
		TraceID:      sCtx.TraceID().String(),
		SpanID:       sCtx.SpanID().String(),
		ParentSpanID: parentSpanID,
		Name:         span.Name(),
		StartTime:    span.StartTime(),
		EndTime:      span.EndTime(),
		DurationMs:   durationMs,
		Attributes:   attrs,
		Events:       events,
		Status:       status,
	}
}
