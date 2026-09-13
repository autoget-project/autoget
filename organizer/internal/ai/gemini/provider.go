// Package gemini implements the ai.Provider interface on top of the official
// Google GenAI SDK (Gemini API backend).
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/ptr"
)

const DefaultModel = "gemini-1.5-pro"

// maxToolRounds is a conservative upper bound on tool-call rounds in a single
// GenerateStructuredWithTools call, guarding against runaway loops.
const maxToolRounds = 10

// Provider implements the ai.Provider interface using Google Gemini.
type Provider struct {
	client  *genai.Client
	model   string
	options ai.ProviderOptions
}

// NewProvider creates a new Gemini Provider.
func NewProvider(apiKey string, opts ...ai.Option) (*Provider, error) {
	options := ai.DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	model := DefaultModel
	if options.Model != "" {
		model = strings.TrimPrefix(options.Model, "gemini:")
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: &http.Client{Timeout: options.Timeout},
		HTTPOptions: genai.HTTPOptions{
			BaseURL: strings.TrimRight(options.BaseURL, "/"),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	return &Provider{
		client:  client,
		model:   model,
		options: options,
	}, nil
}

func (p *Provider) Name() string {
	return "gemini"
}

func (p *Provider) GenerateStructured(ctx context.Context, prompt string, schema any, result any) error {
	return p.generateWithOptionalSearch(ctx, prompt, schema, result, false)
}

func (p *Provider) GenerateStructuredWithSearch(ctx context.Context, prompt string, schema any, result any) error {
	return p.generateWithOptionalSearch(ctx, prompt, schema, result, true)
}

func (p *Provider) generateWithOptionalSearch(ctx context.Context, prompt string, schema any, result any, enableSearch bool) error {
	responseSchema, err := resolveOpenAPISchema(schema)
	if err != nil {
		return err
	}

	cfg := p.baseContentConfig(responseSchema)
	if enableSearch {
		cfg.Tools = []*genai.Tool{
			{GoogleSearch: &genai.GoogleSearch{}},
		}
	}

	resp, err := p.client.Models.GenerateContent(ctx, p.model, genai.Text(prompt), cfg)
	if err != nil {
		return fmt.Errorf("gemini API request failed: %w", err)
	}

	content := resp.Text()
	if content == "" {
		fullResp, _ := json.Marshal(resp)
		return fmt.Errorf("gemini API returned no text parts in candidate: resp=%s", string(fullResp))
	}

	if err := json.Unmarshal([]byte(content), result); err != nil {
		return fmt.Errorf("failed to unmarshal structured result: %w (content: %s)", err, content)
	}

	return nil
}

// resolveOpenAPISchema normalizes the schema argument and converts it to the
// SDK's Schema type: ai.JSONSchema values pass through, anything else goes
// through the OpenAPI schema pipeline (shared by result and tool schemas).
func resolveOpenAPISchema(schema any) (*genai.Schema, error) {
	var jsonSchema ai.JSONSchema
	var err error

	if s, ok := schema.(ai.JSONSchema); ok {
		jsonSchema = s
	} else {
		jsonSchema, err = ai.GenerateOpenAPISchema(schema)
		if err != nil {
			return nil, fmt.Errorf("failed to generate openapi schema: %w", err)
		}
	}

	responseSchema, err := toGenaiSchema(jsonSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to convert response schema: %w", err)
	}
	return responseSchema, nil
}

// baseContentConfig builds the GenerateContentConfig skeleton shared by every
// generation path: temperature, strict JSON response schema and disabled
// safety blocking. Tool lists are appended by the callers.
func (p *Provider) baseContentConfig(responseSchema *genai.Schema) *genai.GenerateContentConfig {
	return &genai.GenerateContentConfig{
		Temperature:      ptr.Float32(float32(p.options.Temperature)),
		ResponseMIMEType: "application/json",
		ResponseSchema:   responseSchema,
		SafetySettings: []*genai.SafetySetting{
			{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdBlockNone},
		},
	}
}

// toGenaiSchema converts the project's JSON Schema representation into the
// SDK's Schema type via a JSON round-trip (identical wire field names).
func toGenaiSchema(s ai.JSONSchema) (*genai.Schema, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var out genai.Schema
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GenerateStructuredWithTools implements ai.ToolProvider using Gemini native
// function calling. Unlike the Grok provider (which must use the split
// protocol), the Gemini API treats responseSchema and function calling tools
// as independent GenerateContentConfig fields and officially supports
// combining them, so the combined mode keeps the strict ResponseSchema active
// on every round (the split-mode fallback is only a live e2e contingency, see
// the step-8 checkpoint):
//
//   - every round carries the responseSchema plus FunctionDeclaration tools;
//     tool parameters go through the same OpenAPI schema pipeline as the
//     result schema to avoid strict-only fields like additionalProperties;
//   - model functionCall parts are echoed back verbatim, followed by one
//     user-role content per call carrying the FunctionResponse (payload
//     wrapped as {"result": ...} per the official convention);
//   - handler errors are fed back as {"error": ...} so the model can
//     self-correct or abandon the tool instead of aborting the loop;
//   - the loop ends when the model stops requesting tools; the terminal text
//     is strict JSON matching responseSchema and is decoded into result.
func (p *Provider) GenerateStructuredWithTools(ctx context.Context, prompt string, tools []ai.Tool, schema any, result any) error {
	// Result schema normalization: same pipeline as generateWithOptionalSearch.
	responseSchema, err := resolveOpenAPISchema(schema)
	if err != nil {
		return err
	}

	// Tool declarations via the same OpenAPI schema pipeline.
	handlers := make(map[string]ai.ToolFunc, len(tools))
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		params, err := ai.GenerateOpenAPISchema(t.Parameters)
		if err != nil {
			return fmt.Errorf("failed to generate openapi schema for tool %s: %w", t.Name, err)
		}
		paramsSchema, err := toGenaiSchema(params)
		if err != nil {
			return fmt.Errorf("failed to convert parameters schema for tool %s: %w", t.Name, err)
		}
		decls = append(decls, &genai.FunctionDeclaration{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  paramsSchema,
		})
		handlers[t.Name] = t.Handler
	}

	cfg := p.baseContentConfig(responseSchema)
	cfg.Tools = []*genai.Tool{
		{FunctionDeclarations: decls},
	}

	contents := []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}

	for round := 1; ; round++ {
		if round > maxToolRounds {
			return fmt.Errorf("gemini: exceeded max tool rounds (%d) without final answer", maxToolRounds)
		}

		resp, err := p.client.Models.GenerateContent(ctx, p.model, contents, cfg)
		if err != nil {
			return fmt.Errorf("gemini API request failed: %w", err)
		}

		calls := resp.FunctionCalls()
		if len(calls) == 0 {
			// Terminal round: the text is strict JSON per responseSchema.
			content := resp.Text()
			if content == "" {
				fullResp, _ := json.Marshal(resp)
				return fmt.Errorf("gemini API returned no text parts in candidate: resp=%s", string(fullResp))
			}

			if err := json.Unmarshal([]byte(content), result); err != nil {
				return fmt.Errorf("failed to unmarshal structured result: %w (content: %s)", err, content)
			}

			return nil
		}

		// Echo the model turn (functionCall parts) back verbatim.
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
			return fmt.Errorf("gemini API returned function calls without candidate content")
		}
		contents = append(contents, resp.Candidates[0].Content)

		for _, call := range calls {
			handler, ok := handlers[call.Name]
			if !ok {
				return fmt.Errorf("gemini: model requested undeclared tool %q", call.Name)
			}

			argsJSON, err := json.Marshal(call.Args)
			if err != nil {
				return fmt.Errorf("gemini: failed to marshal arguments for tool %s: %w", call.Name, err)
			}

			// Feed handler errors back as the function response so the model
			// can self-correct or abandon the tool; never abort the loop.
			var payload map[string]any
			resultJSON, handlerErr := handler(ctx, string(argsJSON))
			if handlerErr != nil {
				payload = map[string]any{"error": handlerErr.Error()}
			} else {
				var parsed any
				if parseErr := json.Unmarshal([]byte(resultJSON), &parsed); parseErr != nil {
					// Not valid JSON: embed the raw string so nothing is lost.
					payload = map[string]any{"result": resultJSON}
				} else {
					payload = map[string]any{"result": parsed}
				}
			}

			contents = append(contents, genai.NewContentFromFunctionResponse(call.Name, payload, genai.RoleUser))
		}
	}
}
