package telemetry

import (
	"os"
	"strconv"
)

// TelemetryConfig holds configuration for OpenTelemetry tracing.
type TelemetryConfig struct {
	// Exporter specifies the exporter: "none"(default) | "inmemory" | "file" | "gcp"
	Exporter     string
	FilePath     string  // default: ".local/traces.jsonl"
	GCPProjectID string  // preferred GCP_PROJECT_ID, fallback GOOGLE_CLOUD_PROJECT
	SampleRatio  float64 // sampling ratio, default 1.0 (sample all)
}

// LoadTelemetryConfig loads telemetry configuration from environment variables.
func LoadTelemetryConfig() TelemetryConfig {
	exporter := os.Getenv("OTEL_TRACES_EXPORTER")
	if exporter == "" {
		exporter = "none"
	}

	filePath := os.Getenv("OTEL_TRACE_FILE")
	if filePath == "" {
		filePath = ".local/traces.jsonl"
	}

	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	if gcpProjectID == "" {
		gcpProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	sampleRatio := 1.0
	if ratioStr := os.Getenv("OTEL_SAMPLE_RATIO"); ratioStr != "" {
		if val, err := strconv.ParseFloat(ratioStr, 64); err == nil && val >= 0.0 && val <= 1.0 {
			sampleRatio = val
		}
	}

	return TelemetryConfig{
		Exporter:     exporter,
		FilePath:     filePath,
		GCPProjectID: gcpProjectID,
		SampleRatio:  sampleRatio,
	}
}
