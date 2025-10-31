package organizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	// ActionMove indicates that the file should be moved.
	ActionMove = "move"
	// ActionSkip indicates that the file should be skipped.
	ActionSkip = "skip"
)

// PlanRequest is the request body for the plan endpoint.
type PlanRequest struct {
	Files    []string               `json:"files"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PlanAction defines a single action to be taken on a file.
type PlanAction struct {
	File   string `json:"file"`             // Exact original path
	Action string `json:"action"`           // "move" or "skip"
	Target string `json:"target,omitempty"` // Target path for "move" action
}

// PlanResponse is the response from the plan endpoint.
type PlanResponse struct {
	Plan  []PlanAction `json:"plan,omitempty"`
	Error string       `json:"error,omitempty"`
}

// ExecuteRequest is the request body for the execute endpoint.
type ExecuteRequest struct {
	Plan []PlanAction `json:"plan"`
}

// PlanFailed represents a PlanAction that failed during execution.
type PlanFailed struct {
	PlanAction
	Reason string `json:"reason"`
}

// ExecuteResponse is the response from the execute endpoint on failure.
type ExecuteResponse struct {
	FailedMoves []PlanFailed `json:"failed_move"`
}

// Client is a client for the organizer service.
type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

// NewClient creates a new organizer service client.
func NewClient(baseURL string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    u,
		httpClient: httpClient,
	}, nil
}

// Plan sends a request to the /v1/plan endpoint to get an organization plan.
// 200 OK may still hint to an error and retry would not help.
// Other error, we may retry.
func (c *Client) Plan(ctx context.Context, req *PlanRequest) (*PlanResponse, error) {
	planURL := c.baseURL.JoinPath("/v1/plan")

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plan request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, planURL.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create plan request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send plan request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("plan request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var planResp PlanResponse
	if err := json.NewDecoder(resp.Body).Decode(&planResp); err != nil {
		return nil, fmt.Errorf("failed to decode plan response: %w", err)
	}

	return &planResp, nil
}

// Execute sends a request to the /v1/execute endpoint to execute a plan.
func (c *Client) Execute(ctx context.Context, req *ExecuteRequest) (bool, *ExecuteResponse, error) {
	executeURL := c.baseURL.JoinPath("/v1/execute")

	reqBody, err := json.Marshal(req)
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal execute request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, executeURL.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return false, nil, fmt.Errorf("failed to create execute request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, nil, fmt.Errorf("failed to send execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, nil, nil
	}

	var execResp ExecuteResponse
	// It's possible for the body to be empty on full success.
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, nil, fmt.Errorf("failed to read execute response body: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, &execResp); err != nil {
		return false, nil, fmt.Errorf("failed to decode execute response: %w", err)
	}
	return false, &execResp, nil
}
