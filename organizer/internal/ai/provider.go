package ai

import (
	"context"
	"time"
)

// DefaultTemperature is the default sampling temperature for structured outputs (0.1 for high determinism).
const DefaultTemperature = 0.1

// DefaultTimeout is the default timeout for AI provider requests.
const DefaultTimeout = 60 * time.Second

// Provider is the unified abstraction for AI structured generation backends.
type Provider interface {
	Name() string
	GenerateStructured(ctx context.Context, prompt string, schema any, result any) error
}

// SearchProvider is an optional interface implemented by providers that support built-in web search (grounding).
type SearchProvider interface {
	Provider
	GenerateStructuredWithSearch(ctx context.Context, prompt string, schema any, result any) error
}

// ToolFunc executes a tool invocation requested by the model; argsJSON is the
// raw JSON arguments object, the return value is the JSON-serializable result
// fed back to the model.
type ToolFunc func(ctx context.Context, argsJSON string) (resultJSON string, err error)

// Tool declares a function the model may call during generation.
type Tool struct {
	Name        string
	Description string
	Parameters  any // schema struct, converted per provider (strict / OpenAPI)
	Handler     ToolFunc
}

// ToolProvider is an optional interface implemented by providers that support
// function calling. The provider drives the multi-round tool loop internally
// (model requests tool -> handler executes -> result fed back) until the final
// structured output matching schema is produced.
type ToolProvider interface {
	Provider
	GenerateStructuredWithTools(ctx context.Context, prompt string, tools []Tool, schema any, result any) error
}

// Option represents a functional option for configuring a Provider.
type Option func(*ProviderOptions)

// ProviderOptions holds configuration options for an AI provider call or client.
type ProviderOptions struct {
	BaseURL     string
	Temperature float64
	Timeout     time.Duration
	Model       string
}

// WithBaseURL overrides the default provider API base URL (essential for httptest mocks).
func WithBaseURL(baseURL string) Option {
	return func(o *ProviderOptions) {
		o.BaseURL = baseURL
	}
}

// WithTemperature sets the model sampling temperature.
func WithTemperature(temp float64) Option {
	return func(o *ProviderOptions) {
		o.Temperature = temp
	}
}

// WithTimeout sets the request timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(o *ProviderOptions) {
		o.Timeout = timeout
	}
}

// WithModel sets the model name.
func WithModel(model string) Option {
	return func(o *ProviderOptions) {
		o.Model = model
	}
}

// DefaultOptions returns the default options for a provider call.
func DefaultOptions() ProviderOptions {
	return ProviderOptions{
		Temperature: DefaultTemperature,
		Timeout:     DefaultTimeout,
	}
}
