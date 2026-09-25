package telemetry_test

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

func TestTelemetryInit_None(t *testing.T) {
	// Tests with Init touch global state, so avoid t.Parallel() for global Init tests
	cfg := telemetry.TelemetryConfig{
		Exporter: "none",
	}

	state, err := telemetry.Init(cfg)
	require.NoError(t, err)
	assert.Nil(t, state.Provider)
	assert.Nil(t, state.Exporter)

	tr := telemetry.Tracer("test")
	ctx, span := tr.Start(context.Background(), "test-span")
	defer span.End()

	// Span should not be recording and SpanContext should not be valid
	assert.False(t, span.IsRecording())
	assert.False(t, span.SpanContext().IsValid())

	assert.Empty(t, telemetry.TraceIDFromContext(ctx))

	require.NoError(t, telemetry.Flush(context.Background()))
	require.NoError(t, telemetry.Shutdown(context.Background()))
}

func TestTelemetryInit_InMemory(t *testing.T) {
	tp, exp := telemetry.NewTestTracerProvider()
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), telemetry.SpanStage1Classify)
	span.SetAttributes(
		attribute.String(telemetry.AttrStage1Category, "movie"),
		attribute.Bool(telemetry.AttrStage1RuleMatched, true),
	)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, telemetry.SpanStage1Classify, spans[0].Name)
	assert.NotEmpty(t, telemetry.TraceIDFromContext(ctx))

	attrMap := make(map[string]interface{})
	for _, a := range spans[0].Attributes {
		attrMap[string(a.Key)] = a.Value.AsInterface()
	}
	assert.Equal(t, "movie", attrMap[telemetry.AttrStage1Category])
	assert.Equal(t, true, attrMap[telemetry.AttrStage1RuleMatched])
}

func TestTelemetryInit_File(t *testing.T) {
	tempDir := t.TempDir()
	traceFile := filepath.Join(tempDir, "sub", "traces.jsonl")

	cfg := telemetry.TelemetryConfig{
		Exporter:    "file",
		FilePath:    traceFile,
		SampleRatio: 1.0,
	}

	state, err := telemetry.Init(cfg)
	require.NoError(t, err)
	require.NotNil(t, state.Provider)

	tr := telemetry.Tracer("test-file")
	_, span := tr.Start(context.Background(), telemetry.SpanHTTPPlan)
	span.SetAttributes(attribute.String(telemetry.AttrOrganizerDir, "/test/path"))
	span.AddEvent("start_processing", trace.WithAttributes(attribute.Int("step", 1)))
	span.End()

	// Flush and shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, telemetry.Flush(ctx))
	require.NoError(t, telemetry.Shutdown(ctx))

	// Verify file contents
	f, err := os.Open(traceFile)
	require.NoError(t, err)
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	var records []telemetry.SpanRecord
	for scanner.Scan() {
		line := scanner.Text()
		var rec telemetry.SpanRecord
		err := json.Unmarshal([]byte(line), &rec)
		require.NoError(t, err)
		records = append(records, rec)
	}
	require.NoError(t, scanner.Err())

	require.Len(t, records, 1)
	assert.Equal(t, "organizer", records[0].ServiceName)
	assert.Equal(t, telemetry.SpanHTTPPlan, records[0].Name)
	assert.NotEmpty(t, records[0].TraceID)
	assert.NotEmpty(t, records[0].SpanID)
	assert.Equal(t, "/test/path", records[0].Attributes[telemetry.AttrOrganizerDir])
	require.Len(t, records[0].Events, 1)
	assert.Equal(t, "start_processing", records[0].Events[0].Name)
}

func TestTelemetryConfig_CustomServiceName(t *testing.T) {
	tempDir := t.TempDir()
	traceFile := filepath.Join(tempDir, "custom.jsonl")

	cfg := telemetry.TelemetryConfig{
		Exporter:    "file",
		ServiceName: "my-custom-service",
		FilePath:    traceFile,
		SampleRatio: 1.0,
	}

	state, err := telemetry.Init(cfg)
	require.NoError(t, err)
	require.NotNil(t, state.Provider)

	tr := telemetry.Tracer("custom")
	_, span := tr.Start(context.Background(), "test-span")
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, telemetry.Flush(ctx))
	require.NoError(t, telemetry.Shutdown(ctx))

	spans, err := telemetry.ReadTraceSpans(traceFile, "")
	require.NoError(t, err)
	require.Len(t, spans, 1)
	assert.Equal(t, "my-custom-service", spans[0].ServiceName)
}

func TestTelemetryInit_GCP_MissingProjectID(t *testing.T) {
	cfg := telemetry.TelemetryConfig{
		Exporter:     "gcp",
		GCPProjectID: "",
	}

	_, err := telemetry.Init(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gcp exporter requires GCP_PROJECT_ID or GOOGLE_CLOUD_PROJECT to be set")
}

// blockingExporter blocks on ExportSpans until context cancellation.
type blockingExporter struct{}

func (b *blockingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	<-ctx.Done()
	return ctx.Err()
}

func (b *blockingExporter) Shutdown(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestTelemetryShutdown_Timeout(t *testing.T) {
	exp := &blockingExporter{}
	sp := sdktrace.NewBatchSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sp))

	tr := tp.Tracer("test")
	_, span := tr.Start(context.Background(), "test")
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := tp.Shutdown(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 1*time.Second, "shutdown must respect context timeout and not deadlock")
}

func TestTraceIDFromContext(t *testing.T) {
	t.Parallel()

	// With empty context
	assert.Empty(t, telemetry.TraceIDFromContext(context.Background()))

	// With active test span
	tp, _ := telemetry.NewTestTracerProvider()
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), "active-span")
	defer span.End()

	traceID := telemetry.TraceIDFromContext(ctx)
	assert.Len(t, traceID, 32)
	assert.Equal(t, span.SpanContext().TraceID().String(), traceID)
}

func TestTelemetrySampling(t *testing.T) {
	t.Parallel()

	// Ratio = 0.0 -> no spans sampled
	tpZero, expZero := telemetry.NewTestTracerProvider(0.0)
	t.Cleanup(func() {
		_ = tpZero.Shutdown(context.Background())
	})
	trZero := tpZero.Tracer("test")
	_, spanZero := trZero.Start(context.Background(), "unsampled")
	spanZero.End()
	assert.Empty(t, expZero.GetSpans())

	// Ratio = 1.0 -> all spans sampled
	tpOne, expOne := telemetry.NewTestTracerProvider(1.0)
	t.Cleanup(func() {
		_ = tpOne.Shutdown(context.Background())
	})
	trOne := tpOne.Tracer("test")
	_, spanOne := trOne.Start(context.Background(), "sampled")
	spanOne.End()
	assert.Len(t, expOne.GetSpans(), 1)
}

func TestTelemetry_ConcurrentSpans_Shutdown(t *testing.T) {
	t.Parallel()

	tp, _ := telemetry.NewTestTracerProvider(1.0)
	tr := tp.Tracer("concurrent-test")

	const goroutines = 20
	const iterations = 50

	stop := make(chan struct{})
	done := make(chan struct{}, goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < iterations; i++ {
				select {
				case <-stop:
					return
				default:
				}
				_, span := tr.Start(context.Background(), "concurrent-span")
				span.SetAttributes(attribute.Int("worker", id), attribute.Int("iter", i))
				span.End()
			}
		}(g)
	}

	time.Sleep(10 * time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := tp.Shutdown(shutdownCtx)
	require.NoError(t, err)

	close(stop)
	for g := 0; g < goroutines; g++ {
		<-done
	}
}
