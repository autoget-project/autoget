package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/autoget-project/organizer/internal/ai"
)

// CallRecord records details of a call to GenerateStructured.
type CallRecord struct {
	Prompt string
	Schema any
}

// Rule defines a mock rule that returns a specific structured object or JSON string when Prompt matches.
type Rule struct {
	// PromptPattern is a regex (IsRegex) or substring to match against the prompt.
	// An empty pattern with IsRegex:false matches every prompt (catch-all).
	PromptPattern string
	IsRegex       bool
	Response      any // Struct or map or JSON string
	Error         error
}

// ToolStep is one scripted turn: either the mock LLM requests a tool call
// (Final=false, handler from the tools slice is executed and the recorded
// result appended) or produces the terminal response/error.
type ToolStep struct {
	ToolName string // requested tool call (terminal when empty)
	ArgsJSON string
	Response any   // terminal structured response (JSON string / struct / map)
	Error    error // terminal error
}

// ToolRule matches GenerateStructuredWithTools calls by prompt substring
// (same matching semantics as Rule) and replays Steps in order.
type ToolRule struct {
	PromptPattern string
	Steps         []ToolStep
}

// ToolCallRecord captures one executed tool invocation for assertions.
type ToolCallRecord struct {
	Name       string
	ArgsJSON   string
	ResultJSON string
}

// Provider implements a thread-safe mock ai.Provider for unit and offline integration tests.
type Provider struct {
	mu          sync.RWMutex
	name        string
	calls       []CallRecord
	rules       []Rule
	toolRules   []ToolRule
	toolCalls   []ToolCallRecord
	defaultResp any
	defaultErr  error
}

// NewProvider creates a new Mock Provider.
func NewProvider() *Provider {
	return &Provider{
		name:  "mock",
		calls: make([]CallRecord, 0),
		rules: make([]Rule, 0),
	}
}

func (p *Provider) Name() string {
	if p.name == "" {
		return "mock"
	}
	return p.name
}

// SetName sets the provider name (e.g. to mimic "grok" or "gemini" if needed).
func (p *Provider) SetName(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.name = name
}

// AddRule registers a matching rule for mock responses.
func (p *Provider) AddRule(rule Rule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = append(p.rules, rule)
}

// AddToolRule registers a scripted tool-session rule for GenerateStructuredWithTools.
func (p *Provider) AddToolRule(rule ToolRule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.toolRules = append(p.toolRules, rule)
}

// SetDefaultResponse sets the fallback response when no rule matches.
func (p *Provider) SetDefaultResponse(resp any, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultResp = resp
	p.defaultErr = err
}

// Calls returns all recorded calls.
func (p *Provider) Calls() []CallRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	copied := make([]CallRecord, len(p.calls))
	copy(copied, p.calls)
	return copied
}

// ToolCalls returns copies of all executed tool invocations.
func (p *Provider) ToolCalls() []ToolCallRecord {
	p.mu.RLock()
	defer p.mu.RUnlock()
	copied := make([]ToolCallRecord, len(p.toolCalls))
	copy(copied, p.toolCalls)
	return copied
}

// Reset clears recorded calls, rules and tool session state.
func (p *Provider) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = nil
	p.rules = nil
	p.toolRules = nil
	p.toolCalls = nil
	p.defaultResp = nil
	p.defaultErr = nil
}

func (p *Provider) GenerateStructuredWithSearch(ctx context.Context, prompt string, schema any, result any) error {
	return p.GenerateStructured(ctx, prompt, schema, result)
}

func (p *Provider) GenerateStructured(ctx context.Context, prompt string, schema any, result any) error {
	p.mu.Lock()
	p.calls = append(p.calls, CallRecord{
		Prompt: prompt,
		Schema: schema,
	})

	var matchedResp any
	var matchedErr error
	found := false

	for _, rule := range p.rules {
		matched := false
		if rule.IsRegex {
			if re, err := regexp.Compile(rule.PromptPattern); err == nil {
				matched = re.MatchString(prompt)
			}
		} else {
			matched = rule.PromptPattern == "" || rule.PromptPattern == prompt || strings.Contains(prompt, rule.PromptPattern)
		}

		if matched {
			matchedResp = rule.Response
			matchedErr = rule.Error
			found = true
			break
		}
	}

	if !found {
		matchedResp = p.defaultResp
		matchedErr = p.defaultErr
	}
	p.mu.Unlock()

	if matchedErr != nil {
		return matchedErr
	}

	if matchedResp == nil {
		return fmt.Errorf("mock provider: no matching rule or default response for prompt: %s", prompt)
	}

	return decodeResponse(matchedResp, result)
}

// GenerateStructuredWithTools replays a scripted tool session chosen by the
// first matching ToolRule (substring semantics, same as Rule). Each non-terminal
// ToolStep executes the handler of the same-named tool from the tools argument
// and records the invocation; the first terminal ToolStep (empty ToolName)
// ends the call via the regular marshal/unmarshal pipeline.
//
// Semantic difference from production providers (deliberate, plan v3 review
// 3.1): a handler error aborts the whole call immediately, while real providers
// feed {"error": "..."} back to the model for self-correction. The mock exists
// to drive deterministic business-logic tests; provider-side error recovery is
// covered by each production provider's own httptest suite.
func (p *Provider) GenerateStructuredWithTools(ctx context.Context, prompt string, tools []ai.Tool, schema any, result any) error {
	p.mu.Lock()
	p.calls = append(p.calls, CallRecord{
		Prompt: prompt,
		Schema: schema,
	})

	var steps []ToolStep
	found := false
	for _, rule := range p.toolRules {
		if rule.PromptPattern == "" || rule.PromptPattern == prompt || strings.Contains(prompt, rule.PromptPattern) {
			steps = rule.Steps
			found = true
			break
		}
	}

	var defaultResp any
	var defaultErr error
	if !found {
		defaultResp, defaultErr = p.defaultResp, p.defaultErr
	}
	p.mu.Unlock()

	if !found {
		if defaultErr != nil {
			return defaultErr
		}
		if defaultResp == nil {
			return fmt.Errorf("mock provider: no matching tool rule or default response for prompt: %s", prompt)
		}
		return decodeResponse(defaultResp, result)
	}

	for _, step := range steps {
		if step.ToolName == "" {
			if step.Error != nil {
				return step.Error
			}
			if step.Response == nil {
				return fmt.Errorf("mock provider: terminal tool step has neither response nor error for prompt: %s", prompt)
			}
			return decodeResponse(step.Response, result)
		}

		var handler ai.ToolFunc
		for _, tool := range tools {
			if tool.Name == step.ToolName {
				handler = tool.Handler
				break
			}
		}
		if handler == nil {
			return fmt.Errorf("mock provider: scripted step requests unknown tool %q", step.ToolName)
		}

		resultJSON, err := handler(ctx, step.ArgsJSON)
		if err != nil {
			return err
		}

		p.mu.Lock()
		p.toolCalls = append(p.toolCalls, ToolCallRecord{
			Name:       step.ToolName,
			ArgsJSON:   step.ArgsJSON,
			ResultJSON: resultJSON,
		})
		p.mu.Unlock()
	}

	return fmt.Errorf("mock provider: scripted tool steps exhausted without a terminal response for prompt: %s", prompt)
}

// decodeResponse writes resp into result following the same marshal/unmarshal
// pipeline as GenerateStructured.
func decodeResponse(resp any, result any) error {
	switch val := resp.(type) {
	case string:
		return json.Unmarshal([]byte(val), result)
	case []byte:
		return json.Unmarshal(val, result)
	default:
		data, err := json.Marshal(val)
		if err != nil {
			return fmt.Errorf("mock provider: failed to marshal response: %w", err)
		}
		return json.Unmarshal(data, result)
	}
}
