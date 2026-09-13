package gemini_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/ai/gemini"
)

type TestOutput struct {
	Title string `json:"title"`
	Score int    `json:"score"`
}

func TestGeminiProvider_Success(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assertions inside the server goroutine must stay non-fatal.
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.Path, "generateContent")
		assert.Equal(t, "test-gemini-key", r.Header.Get("x-goog-api-key"))

		var reqBody map[string]interface{}
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		genConfig, ok := reqBody["generationConfig"].(map[string]interface{})
		assert.True(t, ok, "generationConfig must be an object")
		assert.Equal(t, "application/json", genConfig["responseMimeType"])

		resp := map[string]interface{}{
			"candidates": []map[string]interface{}{
				{
					"content": map[string]interface{}{
						"parts": []map[string]interface{}{
							{
								"text": `{"title":"gemini-test","score":99}`,
							},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(ts.Close)

	provider, err := gemini.NewProvider("test-gemini-key",
		ai.WithBaseURL(ts.URL),
		ai.WithModel("gemini:gemini-1.5-flash"),
		ai.WithTimeout(5*time.Second),
	)
	require.NoError(t, err)
	assert.Equal(t, "gemini", provider.Name())

	var out TestOutput
	require.NoError(t, provider.GenerateStructured(context.Background(), "test prompt", TestOutput{}, &out))
	assert.Equal(t, TestOutput{Title: "gemini-test", Score: 99}, out)
}

func TestGeminiProvider_APIError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Invalid request","status":"INVALID_ARGUMENT"}}`))
	}))
	t.Cleanup(ts.Close)

	provider, err := gemini.NewProvider("bad-key", ai.WithBaseURL(ts.URL))
	require.NoError(t, err)

	var out TestOutput
	err = provider.GenerateStructured(context.Background(), "test prompt", TestOutput{}, &out)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "400"), "error must mention the status code, got %v", err)
}

// Compile-time proof that the Gemini provider supports the optional tool protocol.
var _ ai.ToolProvider = (*gemini.Provider)(nil)

// ToolDecision is the structured output target used by tool-loop tests.
type ToolDecision struct {
	Hit bool `json:"hit"`
}

// searchPornArgs mirrors the planner's search tool arguments.
type searchPornArgs struct {
	Query string `json:"query"`
}

// toolLoopPart mirrors one wire part for request shape assertions.
type toolLoopPart struct {
	Text         string `json:"text"`
	FunctionCall *struct {
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	} `json:"functionCall"`
	FunctionResponse *struct {
		Name     string         `json:"name"`
		Response map[string]any `json:"response"`
	} `json:"functionResponse"`
}

// toolLoopContent mirrors one wire content for request shape assertions.
type toolLoopContent struct {
	Role  string         `json:"role"`
	Parts []toolLoopPart `json:"parts"`
}

// toolLoopRequest mirrors the full generateContent request wire shape.
type toolLoopRequest struct {
	Contents []toolLoopContent `json:"contents"`
	Tools    []struct {
		FunctionDeclarations []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"functionDeclarations"`
	} `json:"tools"`
	GenerationConfig struct {
		ResponseMIMEType string         `json:"responseMimeType"`
		ResponseSchema   map[string]any `json:"responseSchema"`
		Temperature      float64        `json:"temperature"`
	} `json:"generationConfig"`
}

// textResponse builds a generateContent response with a single text part.
func textResponse(text string) map[string]any {
	return map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"role": "model",
					"parts": []map[string]any{
						{"text": text},
					},
				},
			},
		},
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestGeminiProvider_GenerateStructuredWithTools_MultiRound(t *testing.T) {
	t.Parallel()

	const (
		prompt         = "decide which scene matches"
		toolQuery      = "Agatha Vega 2026-08-30"
		toolResultJSON = `[{"slug":"agatha-vega-abc","title":"Agatha"}]`
	)

	var mu sync.Mutex
	var requests []toolLoopRequest
	var handlerArgs []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Assertions inside the server goroutine must stay non-fatal.
		assert.Contains(t, r.URL.Path, "generateContent")

		var reqBody toolLoopRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		idx := len(requests)
		requests = append(requests, reqBody)
		mu.Unlock()

		switch idx {
		case 0:
			// Round 1: tools + combined-mode strict response schema, plain
			// user prompt.
			assert.InDelta(t, 0.1, reqBody.GenerationConfig.Temperature, 0.0001)
			assert.Equal(t, "application/json", reqBody.GenerationConfig.ResponseMIMEType)
			assert.Equal(t, "object", reqBody.GenerationConfig.ResponseSchema["type"])

			assert.Len(t, reqBody.Tools, 1)
			assert.Len(t, reqBody.Tools[0].FunctionDeclarations, 1)
			decl := reqBody.Tools[0].FunctionDeclarations[0]
			assert.Equal(t, "search_porn", decl.Name)
			assert.Equal(t, "Search ThePornDB for scenes and movies", decl.Description)
			assert.Equal(t, "object", decl.Parameters["type"])
			props, ok := decl.Parameters["properties"].(map[string]any)
			assert.True(t, ok)
			query, ok := props["query"].(map[string]any)
			assert.True(t, ok)
			assert.Equal(t, "string", query["type"])
			assert.NotContains(t, decl.Parameters, "additionalProperties",
				"tool parameters use the OpenAPI variant, no strict-only fields")

			assert.Len(t, reqBody.Contents, 1)
			assert.Equal(t, "user", reqBody.Contents[0].Role)
			assert.Len(t, reqBody.Contents[0].Parts, 1)
			assert.Equal(t, prompt, reqBody.Contents[0].Parts[0].Text)

			writeJSON(t, w, map[string]any{
				"candidates": []map[string]any{
					{
						"content": map[string]any{
							"role": "model",
							"parts": []map[string]any{
								{
									"functionCall": map[string]any{
										"name": "search_porn",
										"args": map[string]any{"query": toolQuery},
									},
								},
							},
						},
					},
				},
			})
		case 1:
			// Round 2: contents must be user text -> model functionCall echo ->
			// user FunctionResponse carrying the handler result; the combined
			// mode keeps tools + response schema on every round.
			assert.Len(t, reqBody.Contents, 3)
			assert.Len(t, reqBody.Tools, 1)
			assert.Equal(t, "application/json", reqBody.GenerationConfig.ResponseMIMEType)

			user, model, fnResp := reqBody.Contents[0], reqBody.Contents[1], reqBody.Contents[2]

			assert.Equal(t, "user", user.Role)
			assert.Len(t, user.Parts, 1)
			assert.Equal(t, prompt, user.Parts[0].Text)

			assert.Equal(t, "model", model.Role)
			assert.Len(t, model.Parts, 1)
			assert.NotNil(t, model.Parts[0].FunctionCall, "model turn must keep the functionCall part")
			assert.Nil(t, model.Parts[0].FunctionResponse)
			assert.Equal(t, "search_porn", model.Parts[0].FunctionCall.Name)
			assert.Equal(t, map[string]any{"query": toolQuery}, model.Parts[0].FunctionCall.Args)

			assert.Equal(t, "user", fnResp.Role, "FunctionResponse contents are sent with role user")
			assert.Len(t, fnResp.Parts, 1)
			assert.NotNil(t, fnResp.Parts[0].FunctionResponse, "result turn must carry the functionResponse part")
			assert.Nil(t, fnResp.Parts[0].FunctionCall)
			assert.Equal(t, "search_porn", fnResp.Parts[0].FunctionResponse.Name)
			assert.Equal(t,
				map[string]any{"result": []any{
					map[string]any{"slug": "agatha-vega-abc", "title": "Agatha"},
				}},
				fnResp.Parts[0].FunctionResponse.Response,
				"response payload must wrap the handler output JSON under result")

			writeJSON(t, w, textResponse(`{"hit":true}`))
		default:
			t.Errorf("unexpected request #%d", idx+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(ts.Close)

	provider, err := gemini.NewProvider("test-gemini-key",
		ai.WithBaseURL(ts.URL),
		ai.WithModel("gemini:gemini-1.5-flash"),
		ai.WithTimeout(5*time.Second),
	)
	require.NoError(t, err)

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
	assert.Len(t, requests, 2, "expected 1 tool round + 1 terminal round with the final JSON")
	assert.Equal(t, []string{`{"query":"` + toolQuery + `"}`}, handlerArgs,
		"handler must run with the model-provided arguments")
}

func TestGeminiProvider_GenerateStructuredWithTools_NoToolCall(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requestCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody toolLoopRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))

		mu.Lock()
		requestCount++
		mu.Unlock()

		assert.Len(t, reqBody.Contents, 1, "first request must carry only the user prompt")
		writeJSON(t, w, textResponse(`{"hit":false}`))
	}))
	t.Cleanup(ts.Close)

	provider, err := gemini.NewProvider("test-gemini-key", ai.WithBaseURL(ts.URL))
	require.NoError(t, err)

	tool := ai.Tool{
		Name:       "search_porn",
		Parameters: searchPornArgs{},
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
	assert.Equal(t, 1, requestCount,
		"combined mode: a first round without function calls is already the terminal answer")
}

func TestGeminiProvider_GenerateStructuredWithTools_UnknownTool(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requestCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"role": "model",
						"parts": []map[string]any{
							{
								"functionCall": map[string]any{
									"name": "undeclared_tool",
									"args": map[string]any{"query": "x"},
								},
							},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(ts.Close)

	provider, err := gemini.NewProvider("test-gemini-key", ai.WithBaseURL(ts.URL))
	require.NoError(t, err)

	tool := ai.Tool{
		Name:       "search_porn",
		Parameters: searchPornArgs{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			return "[]", nil
		},
	}

	var out ToolDecision
	err = provider.GenerateStructuredWithTools(context.Background(), "decide", []ai.Tool{tool}, ToolDecision{}, &out)
	require.Error(t, err)
	assert.ErrorContains(t, err, "undeclared_tool")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 1, requestCount, "provider must fail fast on undeclared tool names")
}
