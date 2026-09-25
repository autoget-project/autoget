package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/oauth"
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

// buildResource constructs an OTel Resource with service.name and any additional attributes configured.
func buildResource(serviceName string, extraAttrs ...attribute.KeyValue) (*resource.Resource, error) {
	if serviceName == "" {
		serviceName = defaultTracerName
	}
	attrs := append([]attribute.KeyValue{semconv.ServiceNameKey.String(serviceName)}, extraAttrs...)
	return resource.Merge(
		resource.Default(),
		resource.NewSchemaless(attrs...),
	)
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
		res, err := buildResource(cfg.ServiceName)
		if err != nil {
			return nil, fmt.Errorf("failed to build resource: %w", err)
		}
		exp := tracetest.NewInMemoryExporter()
		sp := sdktrace.NewSimpleSpanProcessor(exp)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSpanProcessor(sp),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		)
		return &ProviderState{
			Provider: tp,
			Exporter: exp,
		}, nil

	case "file":
		res, err := buildResource(cfg.ServiceName)
		if err != nil {
			return nil, fmt.Errorf("failed to build resource: %w", err)
		}
		exp, err := NewFileSpanExporter(cfg.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create file span exporter: %w", err)
		}
		bsp := sdktrace.NewBatchSpanProcessor(exp)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
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
		res, err := buildResource(
			cfg.ServiceName,
			attribute.String("gcp.project_id", cfg.GCPProjectID),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to build resource: %w", err)
		}

		creds, err := oauth.NewApplicationDefault(
			context.Background(),
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/trace.append",
		)
		if err != nil {
			return nil, fmt.Errorf("failed to acquire Google ADC credentials for OTLP gRPC: %w", err)
		}

		exp, err := otlptracegrpc.New(
			context.Background(),
			otlptracegrpc.WithEndpoint("telemetry.googleapis.com:443"),
			otlptracegrpc.WithDialOption(grpc.WithPerRPCCredentials(creds)),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP OTLP trace exporter: %w", err)
		}
		bsp := sdktrace.NewBatchSpanProcessor(exp)
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
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

	res, _ := buildResource("test-organizer")
	exp := tracetest.NewInMemoryExporter()
	sp := sdktrace.NewSimpleSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
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
