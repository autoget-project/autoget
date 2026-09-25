package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	gcpexporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	defaultTracerName = "organizer"
	shutdownTimeout   = 5 * time.Second
)

// ProviderState holds the initialized TracerProvider and its optional exporter/processor for management.
type ProviderState struct {
	Provider *sdktrace.TracerProvider
	Exporter sdktrace.SpanExporter
}

var (
	globalMu    sync.Mutex
	globalState *ProviderState
)

// Init initializes the package-level OpenTelemetry TracerProvider according to cfg.
// Init is reentrant: if a previous provider was initialized, it shuts it down first.
func Init(cfg TelemetryConfig) (*ProviderState, error) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalState != nil && globalState.Provider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		_ = globalState.Provider.Shutdown(ctx)
		cancel()
		globalState = nil
	}

	state, err := newProviderState(cfg)
	if err != nil {
		return nil, err
	}

	globalState = state
	if state.Provider != nil {
		otel.SetTracerProvider(state.Provider)
	} else {
		otel.SetTracerProvider(noop.NewTracerProvider())
	}

	return state, nil
}

// newProviderState builds a ProviderState from TelemetryConfig.
func newProviderState(cfg TelemetryConfig) (*ProviderState, error) {
	switch cfg.Exporter {
	case "none", "":
		// No-op provider
		return &ProviderState{
			Provider: nil,
			Exporter: nil,
		}, nil

	case "inmemory":
		exp := tracetest.NewInMemoryExporter()
		sp := sdktrace.NewSimpleSpanProcessor(exp)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(sp),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		)
		return &ProviderState{
			Provider: tp,
			Exporter: exp,
		}, nil

	case "file":
		exp, err := NewFileSpanExporter(cfg.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create file span exporter: %w", err)
		}
		bsp := sdktrace.NewBatchSpanProcessor(exp)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(bsp),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		)
		return &ProviderState{
			Provider: tp,
			Exporter: exp,
		}, nil

	case "gcp":
		if cfg.GCPProjectID == "" {
			return nil, fmt.Errorf("gcp exporter requires GCP_PROJECT_ID or GOOGLE_CLOUD_PROJECT to be set")
		}
		exp, err := gcpexporter.New(gcpexporter.WithProjectID(cfg.GCPProjectID))
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP trace exporter: %w", err)
		}
		bsp := sdktrace.NewBatchSpanProcessor(exp)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSpanProcessor(bsp),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		)
		return &ProviderState{
			Provider: tp,
			Exporter: exp,
		}, nil

	default:
		return nil, fmt.Errorf("unknown telemetry exporter: %q", cfg.Exporter)
	}
}

// NewTestTracerProvider creates an isolated TracerProvider and InMemoryExporter for testing.
// Never calls global state, fully supporting t.Parallel().
func NewTestTracerProvider(sampleRatio ...float64) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	ratio := 1.0
	if len(sampleRatio) > 0 {
		ratio = sampleRatio[0]
	}

	exp := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sp),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	return tp, exp
}

// Shutdown shuts down the global TracerProvider with a timeout.
func Shutdown(ctx context.Context) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalState != nil && globalState.Provider != nil {
		err := globalState.Provider.Shutdown(ctx)
		globalState = nil
		otel.SetTracerProvider(noop.NewTracerProvider())
		return err
	}
	return nil
}

// Flush forces any buffered spans in the global TracerProvider to be exported.
func Flush(ctx context.Context) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalState != nil && globalState.Provider != nil {
		return globalState.Provider.ForceFlush(ctx)
	}
	return nil
}

// Tracer returns a named tracer from the global TracerProvider.
func Tracer(name ...string) trace.Tracer {
	tracerName := defaultTracerName
	if len(name) > 0 && name[0] != "" {
		tracerName = name[0]
	}
	return otel.GetTracerProvider().Tracer(tracerName)
}

// TraceIDFromContext returns the 32-character hex trace ID from ctx, or an empty string if none exists or is invalid.
func TraceIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return ""
	}
	return spanCtx.TraceID().String()
}
