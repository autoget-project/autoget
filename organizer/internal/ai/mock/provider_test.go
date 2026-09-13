package mock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/ai/mock"
)

// Compile-time assertion: the mock implements the optional ToolProvider capability.
var _ ai.ToolProvider = (*mock.Provider)(nil)

type MockTarget struct {
	Result string `json:"result"`
	Code   int    `json:"code"`
}

func TestMockProvider_ExactAndRegexRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prompt    string
		want      MockTarget
		wantCalls int
	}{
		{"exact match wins", "exact match", MockTarget{Result: "exact_success", Code: 200}, 1},
		{"regex rule", "classify file: sample.mkv", MockTarget{Result: "regex_matched", Code: 100}, 1},
		{"fallback response", "something else", MockTarget{Result: "fallback", Code: 0}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := mock.NewProvider()
			p.AddRule(mock.Rule{
				PromptPattern: "exact match",
				Response:      MockTarget{Result: "exact_success", Code: 200},
			})
			p.AddRule(mock.Rule{
				PromptPattern: `classify\s+file:\s+(\w+\.mkv)`,
				IsRegex:       true,
				Response:      `{"result":"regex_matched","code":100}`,
			})
			p.SetDefaultResponse(MockTarget{Result: "fallback", Code: 0}, nil)

			var res MockTarget
			require.NoError(t, p.GenerateStructured(context.Background(), tt.prompt, MockTarget{}, &res))
			assert.Equal(t, tt.want, res)
			assert.Len(t, p.Calls(), tt.wantCalls, "call tracking must record every GenerateStructured call")
		})
	}
}

func TestMockProvider_ErrorRule(t *testing.T) {
	t.Parallel()

	p := mock.NewProvider()
	p.AddRule(mock.Rule{
		PromptPattern: "error prompt",
		Error:         errors.New("simulated failure"),
	})

	var res MockTarget
	err := p.GenerateStructured(context.Background(), "error prompt", MockTarget{}, &res)
	require.Error(t, err)
	assert.EqualError(t, err, "simulated failure")
}

func TestMockProvider_GenerateStructuredWithTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		prompt           string
		tools            []ai.Tool
		steps            []mock.ToolStep
		skipToolRule     bool // simulate no ToolRule matching the prompt
		wantResult       MockTarget
		wantToolCalls    []mock.ToolCallRecord
		wantErr          string
		wantPromptLogged bool // GenerateStructuredWithTools must record the prompt in Calls()
	}{
		{
			name:             "no tool call, direct terminal response",
			prompt:           "plan: direct",
			steps:            []mock.ToolStep{{Response: MockTarget{Result: "direct", Code: 1}}},
			wantResult:       MockTarget{Result: "direct", Code: 1},
			wantToolCalls:    []mock.ToolCallRecord{},
			wantPromptLogged: true,
		},
		{
			name:   "single tool round then terminal",
			prompt: "plan: single",
			tools: []ai.Tool{{
				Name: "search_porn",
				Handler: func(_ context.Context, argsJSON string) (string, error) {
					return `{"videos":["v1"]}`, nil
				},
			}},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"query":"agatha"}`},
				{Response: `{"result":"single","code":7}`},
			},
			wantResult: MockTarget{Result: "single", Code: 7},
			wantToolCalls: []mock.ToolCallRecord{
				{Name: "search_porn", ArgsJSON: `{"query":"agatha"}`, ResultJSON: `{"videos":["v1"]}`},
			},
			wantPromptLogged: true,
		},
		{
			name:   "multi tool rounds executed in scripted order",
			prompt: "plan: multi",
			tools: []ai.Tool{
				{
					Name: "search_porn",
					Handler: func(_ context.Context, _ string) (string, error) {
						return `{"round":1}`, nil
					},
				},
				{
					Name: "pick_site",
					Handler: func(_ context.Context, argsJSON string) (string, error) {
						return `{"round":2,"echo":` + argsJSON + `}`, nil
					},
				},
			},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{"q":"a"}`},
				{ToolName: "pick_site", ArgsJSON: `{"site":"tushyraw"}`},
				{Response: MockTarget{Result: "multi", Code: 9}},
			},
			wantResult: MockTarget{Result: "multi", Code: 9},
			wantToolCalls: []mock.ToolCallRecord{
				{Name: "search_porn", ArgsJSON: `{"q":"a"}`, ResultJSON: `{"round":1}`},
				{Name: "pick_site", ArgsJSON: `{"site":"tushyraw"}`, ResultJSON: `{"round":2,"echo":{"site":"tushyraw"}}`},
			},
		},
		{
			name:   "handler error aborts the whole call",
			prompt: "plan: handler error",
			tools: []ai.Tool{{
				Name: "search_porn",
				Handler: func(_ context.Context, _ string) (string, error) {
					return "", errors.New("tpdb down")
				},
			}},
			steps: []mock.ToolStep{
				{ToolName: "search_porn", ArgsJSON: `{}`},
				{Response: MockTarget{Result: "never", Code: 0}},
			},
			wantErr: "tpdb down",
		},
		{
			name:   "script exhausted without terminal step",
			prompt: "plan: exhausted",
			tools: []ai.Tool{{
				Name: "search_porn",
				Handler: func(_ context.Context, _ string) (string, error) {
					return `{}`, nil
				},
			}},
			steps:   []mock.ToolStep{{ToolName: "search_porn", ArgsJSON: `{"q":"x"}`}},
			wantErr: "exhausted without a terminal response",
		},
		{
			name:          "no matching tool rule falls back to default response",
			prompt:        "plan: unmatched",
			skipToolRule:  true,
			wantResult:    MockTarget{Result: "fallback", Code: 3},
			wantToolCalls: []mock.ToolCallRecord{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := mock.NewProvider()
			if !tt.skipToolRule {
				p.AddToolRule(mock.ToolRule{PromptPattern: tt.prompt, Steps: tt.steps})
			} else {
				p.SetDefaultResponse(MockTarget{Result: "fallback", Code: 3}, nil)
			}

			var res MockTarget
			err := p.GenerateStructuredWithTools(context.Background(), tt.prompt, tt.tools, MockTarget{}, &res)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResult, res)
			assert.Equal(t, tt.wantToolCalls, p.ToolCalls())

			if tt.wantPromptLogged {
				calls := p.Calls()
				require.Len(t, calls, 1, "GenerateStructuredWithTools must record the prompt like GenerateStructured")
				assert.Equal(t, tt.prompt, calls[0].Prompt)
			}
		})
	}
}

func TestMockProvider_ToolReset(t *testing.T) {
	t.Parallel()

	p := mock.NewProvider()
	p.AddToolRule(mock.ToolRule{
		PromptPattern: "reset me",
		Steps:         []mock.ToolStep{{Response: MockTarget{Result: "stale"}}},
	})
	p.Reset()

	assert.Empty(t, p.ToolCalls(), "Reset must clear recorded tool calls")

	// The tool rule is gone and no default response is set, so the call must fail
	// instead of silently replaying the stale scripted session.
	var res MockTarget
	err := p.GenerateStructuredWithTools(context.Background(), "reset me", nil, MockTarget{}, &res)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no matching tool rule")
}
