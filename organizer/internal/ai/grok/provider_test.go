package grok_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/grok"
)

// Compile-time proof that the Grok provider supports the optional tool protocol.
var _ ai.ToolProvider = (*grok.Provider)(nil)

type TestOutput struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGrokProvider_Success(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assertions inside the server goroutine must stay non-fatal.
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

		var reqBody map[string]interface{}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		assert.Equal(t, "grok-test", reqBody["model"])
		assert.InDelta(t, 0.1, reqBody["temperature"], 0.0001)

		respFormat, ok := reqBody["response_format"].(map[string]interface{})
		assert.True(t, ok, "response_format must be an object")
		assert.Equal(t, "json_schema", respFormat["type"])
		jsonSchema, ok := respFormat["json_schema"].(map[string]interface{})
		assert.True(t, ok, "response_format.json_schema must be an object")
		assert.Equal(t, true, jsonSchema["strict"])
		assert.NotEmpty(t, jsonSchema["name"], "response_format.json_schema.name must be set")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"name":"test-item","count":42}`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key",
		ai.WithBaseURL(ts.URL),
		ai.WithModel("xai:grok-test"),
		ai.WithTimeout(5*time.Second),
	)
	assert.Equal(t, "grok", provider.Name())

	var out TestOutput
	require.NoError(t, provider.GenerateStructured(context.Background(), "test prompt", TestOutput{}, &out))
	assert.Equal(t, TestOutput{Name: "test-item", Count: 42}, out)
}

func TestGrokProvider_SearchUsesResponsesWebSearch(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Search grounding goes through the Responses API with the server-side
		// web_search tool (the old chat search_parameters endpoint is 410).
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/responses", r.URL.Path)
		assert.Equal(t, "Bearer test-api-key", r.Header.Get("Authorization"))

		var reqBody map[string]interface{}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		assert.Equal(t, "grok-test", reqBody["model"])
		assert.NotContains(t, reqBody, "search_parameters", "deprecated live search must not be sent")

		tools, ok := reqBody["tools"].([]interface{})
		assert.True(t, ok, "tools must be an array")
		assert.Len(t, tools, 1)
		tool, ok := tools[0].(map[string]interface{})
		assert.True(t, ok)
		assert.Equal(t, "web_search", tool["type"], "the web_search server-side tool must be declared")

		text, ok := reqBody["text"].(map[string]interface{})
		assert.True(t, ok, "text must be an object")
		format, ok := text["format"].(map[string]interface{})
		assert.True(t, ok, "text.format must be an object")
		assert.Equal(t, "json_schema", format["type"])
		assert.Equal(t, true, format["strict"])
		assert.NotEmpty(t, format["name"])
		assert.NotEmpty(t, format["schema"], "text.format.schema must carry the strict schema")

		resp := map[string]interface{}{
			"id":          "resp_test",
			"output_text": `{"name":"search-item","count":7}`,
			"citations":   []interface{}{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key",
		ai.WithBaseURL(ts.URL),
		ai.WithModel("xai:grok-test"),
	)

	var out TestOutput
	require.NoError(t, provider.GenerateStructuredWithSearch(context.Background(), "test prompt", TestOutput{}, &out))
	assert.Equal(t, TestOutput{Name: "search-item", Count: 7}, out)
}

func TestGrokProvider_PlainGenerationStaysOnChatCompletions(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Plain structured generation keeps the chat completions path untouched
		// and must never declare a web_search tool.
		assert.Equal(t, "/chat/completions", r.URL.Path)

		var reqBody map[string]interface{}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		assert.NotContains(t, reqBody, "search_parameters")
		assert.NotContains(t, reqBody, "tools")

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": `{"name":"plain-item","count":3}`,
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key", ai.WithBaseURL(ts.URL))

	var out TestOutput
	require.NoError(t, provider.GenerateStructured(context.Background(), "test prompt", TestOutput{}, &out))
	assert.Equal(t, TestOutput{Name: "plain-item", Count: 3}, out)
}

func TestGrokProvider_SearchResponseFallbackToOutputParts(t *testing.T) {
	t.Parallel()

	// Some Responses API implementations omit the top-level output_text
	// convenience field; the provider must fall back to output message parts.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/responses", r.URL.Path)
		resp := map[string]interface{}{
			"id": "resp_test",
			"output": []map[string]interface{}{
				{
					"type": "message",
					"role": "assistant",
					"content": []map[string]interface{}{
						{"type": "output_text", "text": `{"name":"parts-item","count":9}`},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key", ai.WithBaseURL(ts.URL))

	var out TestOutput
	require.NoError(t, provider.GenerateStructuredWithSearch(context.Background(), "test prompt", TestOutput{}, &out))
	assert.Equal(t, TestOutput{Name: "parts-item", Count: 9}, out)
}

func TestGrokProvider_SearchAPIError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad search request"}}`))
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("bad-key", ai.WithBaseURL(ts.URL))

	var out TestOutput
	err := provider.GenerateStructuredWithSearch(context.Background(), "test prompt", TestOutput{}, &out)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

func TestGrokProvider_ImplementsSearchProvider(t *testing.T) {
	t.Parallel()

	provider := grok.NewProvider("test-api-key")
	var _ ai.SearchProvider = provider
}

func TestGrokProvider_ErrorStatus(t *testing.T) {
	t.Parallel()

	statuses := []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError}

	for _, status := range statuses {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"api_error"}}`))
			}))
			t.Cleanup(ts.Close)

			provider := grok.NewProvider("bad-key", ai.WithBaseURL(ts.URL))

			var out TestOutput
			err := provider.GenerateStructured(context.Background(), "test prompt", TestOutput{}, &out)
			require.Error(t, err)
			assert.Contains(t, err.Error(), strconv.Itoa(status))
		})
	}
}

// ToolDecision is the structured output target used by tool-loop tests.
type ToolDecision struct {
	Hit bool `json:"hit"`
}

// searchPornArgs mirrors the planner's search tool arguments.
type searchPornArgs struct {
	Query string `json:"query"`
}

// toolLoopMessage mirrors one wire message for request shape assertions.
type toolLoopMessage struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

// toolLoopRequest mirrors the full chat completion request wire shape.
type toolLoopRequest struct {
	Model       string            `json:"model"`
	Messages    []toolLoopMessage `json:"messages"`
	Temperature float64           `json:"temperature"`
	Tools       []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
	ResponseFormat map[string]any `json:"response_format"`
}

// toolCallResponse builds a chat completion response carrying one function tool call.
func toolCallResponse(id, name, args string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": "",
					"tool_calls": []map[string]any{
						{
							"id":   id,
							"type": "function",
							"function": map[string]any{
								"name":      name,
								"arguments": args,
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}
}

// contentResponse builds a plain chat completion response without tool calls.
func contentResponse(content string) map[string]any {
	return map[string]any{
		"choices": []map[string]any{
			{
				"message":       map[string]any{"content": content},
				"finish_reason": "stop",
			},
		},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// assertStrictResponseFormat verifies the response_format payload of a strict
// final round request (json_schema + strict, named, non-empty schema).
func assertStrictResponseFormat(t *testing.T, rf map[string]any) {
	t.Helper()

	assert.NotNil(t, rf, "strict final round must carry response_format")
	assert.Equal(t, "json_schema", rf["type"])
	wrapper, ok := rf["json_schema"].(map[string]any)
	assert.True(t, ok, "response_format.json_schema must be an object")
	assert.Equal(t, true, wrapper["strict"])
	assert.NotEmpty(t, wrapper["name"])
	schema, ok := wrapper["schema"].(map[string]any)
	assert.True(t, ok, "response_format.json_schema.schema must be an object")
	assert.Equal(t, "object", schema["type"])
}

func TestGrokProvider_GenerateStructuredWithTools_MultiRound(t *testing.T) {
	t.Parallel()

	const (
		prompt         = "decide which scene matches"
		toolArgs       = `{"query":"Agatha Vega 2026-08-30"}`
		toolResultJSON = `[{"slug":"agatha-vega-abc","title":"Agatha"}]`
	)

	var mu sync.Mutex
	var requests []toolLoopRequest
	var handlerArgs []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody toolLoopRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		idx := len(requests)
		requests = append(requests, reqBody)
		mu.Unlock()

		switch idx {
		case 0:
			// Tool round 1: tools declared, no response_format, plain user prompt.
			assert.Equal(t, "grok-test", reqBody.Model)
			assert.Nil(t, reqBody.ResponseFormat, "tool rounds must not carry response_format")
			assert.Len(t, reqBody.Tools, 1)
			assert.Equal(t, "function", reqBody.Tools[0].Type)
			assert.Equal(t, "search_porn", reqBody.Tools[0].Function.Name)
			assert.Equal(t, "Search ThePornDB for scenes and movies", reqBody.Tools[0].Function.Description)

			params := reqBody.Tools[0].Function.Parameters
			assert.Equal(t, "object", params["type"])
			props, ok := params["properties"].(map[string]any)
			assert.True(t, ok)
			query, ok := props["query"].(map[string]any)
			assert.True(t, ok)
			assert.Equal(t, "string", query["type"])
			assert.Equal(t, false, params["additionalProperties"])
			assert.Equal(t, []any{"query"}, params["required"])

			assert.Len(t, reqBody.Messages, 1)
			assert.Equal(t, "user", reqBody.Messages[0].Role)
			assert.Equal(t, prompt, reqBody.Messages[0].Content)

			writeJSON(t, w, toolCallResponse("call-1", "search_porn", toolArgs))
		case 1:
			// Tool round 2: history carries the echoed assistant tool_calls and
			// the role=tool result; still tools-only, no response_format.
			assert.Nil(t, reqBody.ResponseFormat, "tool rounds must not carry response_format")
			assert.Len(t, reqBody.Tools, 1)
			assert.Len(t, reqBody.Messages, 3)

			user, assistant, toolMsg := reqBody.Messages[0], reqBody.Messages[1], reqBody.Messages[2]
			assert.Equal(t, "user", user.Role)
			assert.Equal(t, prompt, user.Content)

			assert.Equal(t, "assistant", assistant.Role)
			assert.Len(t, assistant.ToolCalls, 1)
			assert.Equal(t, "call-1", assistant.ToolCalls[0].ID)
			assert.Equal(t, "function", assistant.ToolCalls[0].Type)
			assert.Equal(t, "search_porn", assistant.ToolCalls[0].Function.Name)
			assert.Equal(t, toolArgs, assistant.ToolCalls[0].Function.Arguments)

			assert.Equal(t, "tool", toolMsg.Role)
			assert.Equal(t, "call-1", toolMsg.ToolCallID)
			assert.Equal(t, toolResultJSON, toolMsg.Content)

			// Intermediate content without tool_calls is discarded by the provider.
			writeJSON(t, w, contentResponse("intermediate chatter, to be discarded"))
		case 2:
			// Strict final round: response_format only, no tools, full history
			// (the discarded intermediate assistant message stays out).
			assertStrictResponseFormat(t, reqBody.ResponseFormat)
			assert.Empty(t, reqBody.Tools, "final round must not carry tools")
			assert.Len(t, reqBody.Messages, 3)
			assert.Equal(t, "user", reqBody.Messages[0].Role)
			assert.Equal(t, "assistant", reqBody.Messages[1].Role)
			assert.Equal(t, "tool", reqBody.Messages[2].Role)

			writeJSON(t, w, contentResponse(`{"hit":true}`))
		default:
			t.Errorf("unexpected request #%d", idx+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key",
		ai.WithBaseURL(ts.URL),
		ai.WithModel("xai:grok-test"),
		ai.WithTimeout(5*time.Second),
	)

	tool := ai.Tool{
		Name:        "search_porn",
		Description: "Search ThePornDB for scenes and movies",
		Parameters:  searchPornArgs{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			mu.Lock()
			defer mu.Unlock()
			handlerArgs = append(handlerArgs, argsJSON)
			return toolResultJSON, nil
		},
	}

	var out ToolDecision
	require.NoError(t, provider.GenerateStructuredWithTools(context.Background(), prompt, []ai.Tool{tool}, ToolDecision{}, &out))
	assert.True(t, out.Hit)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, requests, 3, "expected exactly 2 tool rounds + 1 strict final round")
	assert.Equal(t, []string{toolArgs}, handlerArgs, "handler must run with the model-provided arguments")
}

func TestGrokProvider_GenerateStructuredWithTools_NoToolCall(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []toolLoopRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody toolLoopRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		idx := len(requests)
		requests = append(requests, reqBody)
		mu.Unlock()

		switch idx {
		case 0:
			// First round: tools-only request, and the model answers directly.
			assert.Nil(t, reqBody.ResponseFormat)
			assert.Len(t, reqBody.Tools, 1)
			assert.Len(t, reqBody.Messages, 1)
			writeJSON(t, w, contentResponse("no tool needed"))
		case 1:
			// The very next request is already the strict final round.
			assertStrictResponseFormat(t, reqBody.ResponseFormat)
			assert.Empty(t, reqBody.Tools)
			assert.Len(t, reqBody.Messages, 1)
			assert.Equal(t, "user", reqBody.Messages[0].Role)
			writeJSON(t, w, contentResponse(`{"hit":false}`))
		default:
			t.Errorf("unexpected request #%d", idx+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key", ai.WithBaseURL(ts.URL))

	tool := ai.Tool{
		Name:        "search_porn",
		Description: "Search ThePornDB for scenes and movies",
		Parameters:  searchPornArgs{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			t.Error("handler must not run when the model never requests a tool")
			return "", nil
		},
	}

	var out ToolDecision
	require.NoError(t, provider.GenerateStructuredWithTools(context.Background(), "decide", []ai.Tool{tool}, ToolDecision{}, &out))
	assert.False(t, out.Hit)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, requests, 2, "expected 1 tool round + 1 strict final round")
}

func TestGrokProvider_GenerateStructuredWithTools_UnknownTool(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requestCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		writeJSON(t, w, toolCallResponse("call-1", "undeclared_tool", `{"query":"x"}`))
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key", ai.WithBaseURL(ts.URL))

	tool := ai.Tool{
		Name:       "search_porn",
		Parameters: searchPornArgs{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			return "[]", nil
		},
	}

	var out ToolDecision
	err := provider.GenerateStructuredWithTools(context.Background(), "decide", []ai.Tool{tool}, ToolDecision{}, &out)
	require.Error(t, err)
	assert.ErrorContains(t, err, "undeclared_tool")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, requestCount, "provider must fail fast on undeclared tool names")
}

func TestGrokProvider_GenerateStructuredWithTools_RoundLimit(t *testing.T) {
	t.Parallel()

	// Mirrors the package-level const grok.maxToolRounds (unexported, value 10).
	const maxToolRounds = 10

	var mu sync.Mutex
	var requestCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()
		writeJSON(t, w, toolCallResponse(fmt.Sprintf("call-%d", count), "search_porn", `{"query":"x"}`))
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key", ai.WithBaseURL(ts.URL))

	tool := ai.Tool{
		Name:       "search_porn",
		Parameters: searchPornArgs{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			return "[]", nil
		},
	}

	var out ToolDecision
	err := provider.GenerateStructuredWithTools(context.Background(), "decide", []ai.Tool{tool}, ToolDecision{}, &out)
	require.Error(t, err)
	assert.ErrorContains(t, err, "max tool rounds")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, maxToolRounds, requestCount, "loop must stop at the package-level maxToolRounds bound")
}

func TestGrokProvider_GenerateStructuredWithTools_HandlerErrorFedBack(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requests []toolLoopRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody toolLoopRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		idx := len(requests)
		requests = append(requests, reqBody)
		mu.Unlock()

		switch idx {
		case 0:
			assert.Len(t, reqBody.Tools, 1)
			writeJSON(t, w, toolCallResponse("call-1", "search_porn", `{"query":"x"}`))
		case 1:
			// Handler failed: the error must be fed back as a role=tool result
			// so the model can self-correct or abandon the tool.
			assert.Len(t, reqBody.Messages, 3)
			toolMsg := reqBody.Messages[2]
			assert.Equal(t, "tool", toolMsg.Role)
			assert.Equal(t, "call-1", toolMsg.ToolCallID)
			assert.Equal(t, `{"error":"tpdb unavailable"}`, toolMsg.Content)

			// Model gives up on tools; provider proceeds to the strict round.
			writeJSON(t, w, contentResponse("giving up"))
		case 2:
			assertStrictResponseFormat(t, reqBody.ResponseFormat)
			assert.Empty(t, reqBody.Tools)
			writeJSON(t, w, contentResponse(`{"hit":false}`))
		default:
			t.Errorf("unexpected request #%d", idx+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(ts.Close)

	provider := grok.NewProvider("test-api-key", ai.WithBaseURL(ts.URL))

	tool := ai.Tool{
		Name:       "search_porn",
		Parameters: searchPornArgs{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			return "", errors.New("tpdb unavailable")
		},
	}

	var out ToolDecision
	require.NoError(t, provider.GenerateStructuredWithTools(context.Background(), "decide", []ai.Tool{tool}, ToolDecision{}, &out))
	assert.False(t, out.Hit)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, requests, 3, "handler errors must not abort the tool loop")
}
