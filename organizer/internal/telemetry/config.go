package telemetry

import (
	"encoding/json"
	"os"
	"strconv"
)

// TelemetryConfig holds configuration for OpenTelemetry tracing.
type TelemetryConfig struct {
	// Exporter specifies the exporter: "none"(default) | "inmemory" | "file" | "gcp"
	Exporter     string
	ServiceName  string  // default: "organizer" (or via OTEL_SERVICE_NAME)
	FilePath     string  // default: ".local/traces.jsonl"
	GCPProjectID string  // preferred GCP_PROJECT_ID, fallback GOOGLE_CLOUD_PROJECT
	SampleRatio  float64 // sampling ratio, default 1.0 (sample all)
}

// LoadTelemetryConfig loads telemetry configuration from environment variables.
func LoadTelemetryConfig() TelemetryConfig {
	exporter := os.Getenv("TRACE_EXPORTER")
	if exporter == "" {
		exporter = os.Getenv("OTEL_TRACES_EXPORTER")
	}
	if exporter == "" {
		exporter = "none"
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "organizer"
	}

	filePath := os.Getenv("OTEL_TRACE_FILE")
	if filePath == "" {
		filePath = ".local/traces.jsonl"
	}

	gcpProjectID := os.Getenv("GCP_PROJECT_ID")
	if gcpProjectID == "" {
		gcpProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if gcpProjectID == "" {
		// Try reading project_id from GOOGLE_APPLICATION_CREDENTIALS json file if available
		gcpProjectID = extractProjectIDFromCreds(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	}

	sampleRatio := 1.0
	if ratioStr := os.Getenv("OTEL_SAMPLE_RATIO"); ratioStr != "" {
		if val, err := strconv.ParseFloat(ratioStr, 64); err == nil && val >= 0.0 && val <= 1.0 {
			sampleRatio = val
		}
	}

	return TelemetryConfig{
		Exporter:     exporter,
		ServiceName:  serviceName,
		FilePath:     filePath,
		GCPProjectID: gcpProjectID,
		SampleRatio:  sampleRatio,
	}
}

// extractProjectIDFromCreds attempts to parse project_id from a Google service account JSON key file.
func extractProjectIDFromCreds(credsPath string) string {
	if credsPath == "" {
		return ""
	}
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return ""
	}
	var sa struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &sa); err == nil && sa.ProjectID != "" {
		return sa.ProjectID
	}
	return ""
}
