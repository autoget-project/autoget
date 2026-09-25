package organizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

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

	UploadInitRequest    = protocol.UploadInitRequest
	UploadStatusResponse = protocol.UploadStatusResponse
	UploadFinishRequest  = protocol.UploadFinishRequest
	UploadFinishResponse = protocol.UploadFinishResponse
)

// ErrUploadOffsetConflict indicates that the uploaded chunk offset conflicted with the server's authoritative offset.
type ErrUploadOffsetConflict struct {
	ServerOffset int64
}

func (e ErrUploadOffsetConflict) Error() string {
	return fmt.Sprintf("upload offset conflict: server expects offset %d", e.ServerOffset)
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

// Replan sends a request to the /v1/replan endpoint to get a revised
// organization plan. The previous result is carried in a dedicated field and
// treated as flawed, so the organizer re-runs Stage 1 classification instead
// of blindly preserving it.
func (c *Client) Replan(req *ReplanRequest) (*PlanResponse, error) {
	replanURL := c.baseURL.JoinPath("/v1/replan")

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

// InitUpload sends POST /v1/upload/init.
func (c *Client) InitUpload(ctx context.Context, req *UploadInitRequest) (*UploadStatusResponse, error) {
	initURL := c.baseURL.JoinPath("/v1/upload/init")

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal init request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, initURL.String(), bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create init request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send init request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("init upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var statusResp UploadStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode init response: %w", err)
	}

	return &statusResp, nil
}

// GetUpload sends GET /v1/upload/{upload_id} to query current upload offset and status.
func (c *Client) GetUpload(ctx context.Context, uploadID string) (*UploadStatusResponse, error) {
	getURL := c.baseURL.JoinPath("/v1/upload", uploadID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, getURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create get upload request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send get upload request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var statusResp UploadStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode get upload response: %w", err)
	}

	return &statusResp, nil
}

// UploadChunk sends PATCH /v1/upload/{upload_id} to append a chunk.
// If the server returns 409 Conflict, ErrUploadOffsetConflict is returned containing the server's last_offset.
func (c *Client) UploadChunk(ctx context.Context, uploadID string, offset int64, body io.Reader, length int64, checksum string) (int64, error) {
	patchURL := c.baseURL.JoinPath("/v1/upload", uploadID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPatch, patchURL.String(), body)
	if err != nil {
		return 0, fmt.Errorf("failed to create patch request: %w", err)
	}

	httpReq.Header.Set(protocol.UploadOffsetHeader, strconv.FormatInt(offset, 10))
	httpReq.Header.Set("Content-Type", protocol.UploadContentType)
	if checksum != "" {
		httpReq.Header.Set(protocol.UploadChecksumHeader, checksum)
	}
	httpReq.ContentLength = length

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("failed to send patch request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		serverOffsetStr := resp.Header.Get(protocol.UploadOffsetHeader)
		if serverOffset, parseErr := strconv.ParseInt(serverOffsetStr, 10, 64); parseErr == nil {
			return serverOffset, ErrUploadOffsetConflict{ServerOffset: serverOffset}
		}
		return 0, fmt.Errorf("upload conflict with unparseable offset header %q", serverOffsetStr)
	}

	if resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("upload chunk failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	newOffsetStr := resp.Header.Get(protocol.UploadOffsetHeader)
	newOffset, err := strconv.ParseInt(newOffsetStr, 10, 64)
	if err != nil {
		return offset + length, nil
	}
	return newOffset, nil
}

// FinishUpload sends POST /v1/upload/{upload_id}/finish.
func (c *Client) FinishUpload(ctx context.Context, uploadID string, req *UploadFinishRequest) error {
	finishURL := c.baseURL.JoinPath("/v1/upload", uploadID, "finish")

	var bodyReader io.Reader
	if req != nil {
		reqBytes, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("failed to marshal finish request: %w", err)
		}
		bodyReader = bytes.NewReader(reqBytes)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, finishURL.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create finish request: %w", err)
	}
	if req != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send finish request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("finish upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// CancelUpload sends DELETE /v1/upload/{upload_id}.
func (c *Client) CancelUpload(ctx context.Context, uploadID string) error {
	cancelURL := c.baseURL.JoinPath("/v1/upload", uploadID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, cancelURL.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create cancel request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send cancel request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cancel upload failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}
