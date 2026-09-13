package grok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/autoget-project/organizer/internal/ai"
)

const (
	DefaultBaseURL = "https://api.x.ai/v1"
	DefaultModel   = "grok-beta"
)

// maxToolRounds is a conservative upper bound on tool-call rounds in a single
// GenerateStructuredWithTools call, guarding against runaway loops. The strict
// final round (response_format only) happens after the loop and is not counted.
const maxToolRounds = 10

// Provider implements the ai.Provider interface using xAI's Grok (OpenAI compatible).
type Provider struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	options    ai.ProviderOptions
}

// NewProvider creates a new Grok Provider.
func NewProvider(apiKey string, opts ...ai.Option) *Provider {
	options := ai.DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	baseURL := DefaultBaseURL
	if options.BaseURL != "" {
		baseURL = strings.TrimRight(options.BaseURL, "/")
	}

	model := DefaultModel
	if options.Model != "" {
		model = strings.TrimPrefix(options.Model, "xai:")
	}

	return &Provider{
		apiKey:     apiKey,
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{Timeout: options.Timeout},
		options:    options,
	}
}

func (p *Provider) Name() string {
	return "grok"
}

type chatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	Tools          []chatTool          `json:"tools,omitempty"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// chatTool declares a function the model may call (OpenAI-compatible shape).
type chatTool struct {
	Type     string       `json:"type"` // always "function"
	Function chatToolDecl `json:"function"`
}

type chatToolDecl struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Parameters  ai.JSONSchema `json:"parameters"`
}

// chatToolCall is one tool invocation requested by the model.
type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // always "function"
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // raw JSON string
}

type chatResponseFormat struct {
	Type       string                 `json:"type"`
	JSONSchema *chatJSONSchemaWrapper `json:"json_schema,omitempty"`
}

type chatJSONSchemaWrapper struct {
	Name   string        `json:"name"`
	Strict bool          `json:"strict"`
	Schema ai.JSONSchema `json:"schema"`
}

// responsesRequest is the xAI Responses API payload (OpenAI Responses shape):
// server-side web_search tool plus a strict json_schema text format.
type responsesRequest struct {
	Model       string             `json:"model"`
	Input       []responsesMessage `json:"input"`
	Tools       []responsesTool    `json:"tools,omitempty"`
	Text        *responsesText     `json:"text,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type responsesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responsesTool struct {
	Type string `json:"type"` // "web_search"
}

type responsesText struct {
	Format *responsesTextFormat `json:"format,omitempty"`
}

type responsesTextFormat struct {
	Type   string        `json:"type"`
	Name   string        `json:"name"`
	Strict bool          `json:"strict"`
	Schema ai.JSONSchema `json:"schema"`
}

type responsesResponse struct {
	OutputText string                `json:"output_text"` // aggregated assistant text
	Output     []responsesOutputItem `json:"output"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type responsesOutputItem struct {
	Type    string                `json:"type"` // "message"
	Content []responsesOutputPart `json:"content"`
}

type responsesOutputPart struct {
	Type string `json:"type"` // "output_text"
	Text string `json:"text"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

func (p *Provider) GenerateStructured(ctx context.Context, prompt string, schema any, result any) error {
	jsonSchema, err := resolveJSONSchema(schema)
	if err != nil {
		return err
	}

	respMsg, err := p.chat(ctx, chatCompletionRequest{
		Messages: []chatMessage{
			{
				Role:    "user",
				Content: prompt,
			},
		},
		Temperature:    p.options.Temperature,
		ResponseFormat: strictResponseFormat(jsonSchema),
	})
	if err != nil {
		return err
	}

	content := respMsg.Content
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return fmt.Errorf("failed to unmarshal structured result: %w (content: %s)", err, content)
	}

	return nil
}

// GenerateStructuredWithSearch implements ai.SearchProvider via xAI's Responses
// API web_search tool. The old chat-completions server-side live search
// (search_parameters.mode) is deprecated (HTTP 410) and must not be used; the
// Responses API runs the search server-side and returns one final answer.
func (p *Provider) GenerateStructuredWithSearch(ctx context.Context, prompt string, schema any, result any) error {
	jsonSchema, err := resolveJSONSchema(schema)
	if err != nil {
		return err
	}

	content, err := p.responses(ctx, responsesRequest{
		Input: []responsesMessage{
			{Role: "user", Content: prompt},
		},
		Tools: []responsesTool{
			{Type: "web_search"},
		},
		Text: &responsesText{
			Format: &responsesTextFormat{
				Type:   "json_schema",
				Name:   "structured_output",
				Strict: true,
				Schema: jsonSchema,
			},
		},
		Temperature: p.options.Temperature,
	})
	if err != nil {
		return err
	}

	if err := json.Unmarshal([]byte(content), result); err != nil {
		return fmt.Errorf("failed to unmarshal structured result: %w (content: %s)", err, content)
	}

	return nil
}

// responses posts one Responses API request and returns the combined assistant
// text (web search runs server-side).
func (p *Provider) responses(ctx context.Context, reqBody responsesRequest) (string, error) {
	reqBody.Model = p.model

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/responses", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("grok responses API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var respObj responsesResponse
	if err := json.Unmarshal(respBytes, &respObj); err != nil {
		return "", fmt.Errorf("failed to unmarshal responses response: %w", err)
	}

	if respObj.Error != nil && respObj.Error.Message != "" {
		return "", fmt.Errorf("grok responses API error: %s", respObj.Error.Message)
	}

	if text := extractResponsesText(respObj); text != "" {
		return text, nil
	}
	return "", fmt.Errorf("grok responses API returned no text content: %s", string(respBytes))
}

// extractResponsesText prefers the top-level output_text convenience field and
// falls back to walking output message parts (both shapes appear in the wild).
func extractResponsesText(resp responsesResponse) string {
	if strings.TrimSpace(resp.OutputText) != "" {
		return resp.OutputText
	}
	for _, item := range resp.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
				return part.Text
			}
		}
	}
	return ""
}

// resolveJSONSchema normalizes the schema argument to ai.JSONSchema, passing
// ai.JSONSchema values through and converting anything else via
// ai.GenerateStrictJSONSchema.
func resolveJSONSchema(schema any) (ai.JSONSchema, error) {
	if s, ok := schema.(ai.JSONSchema); ok {
		return s, nil
	}

	jsonSchema, err := ai.GenerateStrictJSONSchema(schema)
	if err != nil {
		return ai.JSONSchema{}, fmt.Errorf("failed to generate strict json schema: %w", err)
	}

	return jsonSchema, nil
}

// strictResponseFormat builds the strict json_schema response_format payload.
func strictResponseFormat(jsonSchema ai.JSONSchema) *chatResponseFormat {
	return &chatResponseFormat{
		Type: "json_schema",
		JSONSchema: &chatJSONSchemaWrapper{
			Name:   "structured_output",
			Strict: true,
			Schema: jsonSchema,
		},
	}
}

// chat posts one chat completion request and returns the first choice message.
func (p *Provider) chat(ctx context.Context, reqBody chatCompletionRequest) (chatMessage, error) {
	reqBody.Model = p.model

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return chatMessage{}, fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return chatMessage{}, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.apiKey))

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return chatMessage{}, fmt.Errorf("http request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatMessage{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return chatMessage{}, fmt.Errorf("grok API returned status %d: %s", resp.StatusCode, string(respBytes))
	}

	var chatResp chatCompletionResponse
	if err := json.Unmarshal(respBytes, &chatResp); err != nil {
		return chatMessage{}, fmt.Errorf("failed to unmarshal chat response: %w", err)
	}

	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return chatMessage{}, fmt.Errorf("grok API error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return chatMessage{}, fmt.Errorf("grok API returned no choices")
	}

	return chatResp.Choices[0].Message, nil
}

// GenerateStructuredWithTools implements ai.ToolProvider using xAI's official
// split protocol for combining function calling with structured outputs
// (verified against docs.x.ai, which guarantees "structured outputs with
// tools" only for supported Grok 4 family models; the split protocol works
// regardless of model version):
//
//   - tool rounds carry `tools` (never response_format); assistant tool_calls
//     messages are echoed back verbatim and every handler result is appended
//     as a role=tool message;
//   - once the model stops requesting tools, one final round sends the full
//     message history with ONLY the strict response_format (no tools), so the
//     terminal answer is strictly schema-constrained. Free-form text from the
//     loop is never parsed; only the final round's JSON content is decoded.
func (p *Provider) GenerateStructuredWithTools(ctx context.Context, prompt string, tools []ai.Tool, schema any, result any) error {
	jsonSchema, err := resolveJSONSchema(schema)
	if err != nil {
		return err
	}

	chatTools := make([]chatTool, 0, len(tools))
	handlers := make(map[string]ai.ToolFunc, len(tools))
	for _, t := range tools {
		params, err := ai.GenerateStrictJSONSchema(t.Parameters)
		if err != nil {
			return fmt.Errorf("failed to generate strict json schema for tool %s: %w", t.Name, err)
		}
		chatTools = append(chatTools, chatTool{
			Type: "function",
			Function: chatToolDecl{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
		handlers[t.Name] = t.Handler
	}

	messages := []chatMessage{{Role: "user", Content: prompt}}

	rounds := 0
	for {
		rounds++
		if rounds > maxToolRounds {
			return fmt.Errorf("grok: exceeded max tool rounds (%d) without final answer", maxToolRounds)
		}

		respMsg, err := p.chat(ctx, chatCompletionRequest{
			Messages:    messages,
			Temperature: p.options.Temperature,
			Tools:       chatTools,
		})
		if err != nil {
			return err
		}

		if len(respMsg.ToolCalls) == 0 {
			// Model stopped requesting tools; move on to the strict final round.
			break
		}

		// Echo the assistant tool_calls message back verbatim before the results.
		messages = append(messages, chatMessage{
			Role:      "assistant",
			Content:   respMsg.Content,
			ToolCalls: respMsg.ToolCalls,
		})

		for _, call := range respMsg.ToolCalls {
			handler, ok := handlers[call.Function.Name]
			if !ok {
				return fmt.Errorf("grok: model requested undeclared tool %q", call.Function.Name)
			}

			resultJSON, err := handler(ctx, call.Function.Arguments)
			if err != nil {
				// Feed handler errors back as a tool result so the model can
				// self-correct or abandon the tool instead of aborting the loop.
				errPayload, mErr := json.Marshal(map[string]string{"error": err.Error()})
				if mErr != nil {
					errPayload = []byte(`{"error":"tool handler failed"}`)
				}
				resultJSON = string(errPayload)
			}

			messages = append(messages, chatMessage{
				Role:       "tool",
				Content:    resultJSON,
				ToolCallID: call.ID,
			})
		}
	}

	// Strict final round: response_format only, no tools, full message history.
	respMsg, err := p.chat(ctx, chatCompletionRequest{
		Messages:       messages,
		Temperature:    p.options.Temperature,
		ResponseFormat: strictResponseFormat(jsonSchema),
	})
	if err != nil {
		return err
	}
	if len(respMsg.ToolCalls) > 0 {
		return fmt.Errorf("grok: unexpected tool_calls in strict final round")
	}

	content := respMsg.Content
	if err := json.Unmarshal([]byte(content), result); err != nil {
		return fmt.Errorf("failed to unmarshal structured result: %w (content: %s)", err, content)
	}

	return nil
}
