package organizer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/autoget-project/autoget/protocol"
)

// Re-export protocol constants for convenience
const (
	ActionMove = protocol.ActionMove
	ActionSkip = protocol.ActionSkip
)

// Type aliases to shared protocol DTOs
type (
	PlanAction      = protocol.PlanAction
	PlanRequest     = protocol.APIPlanRequest
	PlanResponse    = protocol.PlanResponse
	ExecuteRequest  = protocol.APIExecuteRequest
	PlanFailed      = protocol.PlanFailed
	ExecuteResponse = protocol.ExecuteResponse
	ReplanRequest   = protocol.APIReplanRequest
)

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
func (c *Client) Plan(req *PlanRequest) (*PlanResponse, error) {
	planURL := c.baseURL.JoinPath("/v1/plan")

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal plan request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, planURL.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create plan request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send plan request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
func (c *Client) Execute(req *ExecuteRequest) (bool, *ExecuteResponse, error) {
	executeURL := c.baseURL.JoinPath("/v1/execute")

	reqBody, err := json.Marshal(req)
	if err != nil {
		return false, nil, fmt.Errorf("failed to marshal execute request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, executeURL.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return false, nil, fmt.Errorf("failed to create execute request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return false, nil, fmt.Errorf("failed to send execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

// ReplanWithHint sends a request to the /v1/replan-with-hint endpoint to get a revised organization plan.
func (c *Client) ReplanWithHint(req *ReplanRequest) (*PlanResponse, error) {
	replanURL := c.baseURL.JoinPath("/v1/replan-with-hint")

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal replan request: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, replanURL.String(), bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create replan request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send replan request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("replan request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var planResp PlanResponse
	if err := json.NewDecoder(resp.Body).Decode(&planResp); err != nil {
		return nil, fmt.Errorf("failed to decode replan response: %w", err)
	}

	return &planResp, nil
}
