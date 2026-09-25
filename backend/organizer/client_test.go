package organizer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/autoget-project/autoget/organizer/upload"
	"github.com/autoget-project/autoget/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	t.Run("valid base URL", func(t *testing.T) {
		client, err := NewClient("http://localhost:8080", nil)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, "http://localhost:8080", client.baseURL.String())
		assert.Equal(t, http.DefaultClient, client.httpClient)
	})

	t.Run("invalid base URL", func(t *testing.T) {
		client, err := NewClient(":", nil)
		require.Error(t, err)
		assert.Nil(t, client)
		assert.Contains(t, err.Error(), "invalid base URL")
	})

	t.Run("custom http client", func(t *testing.T) {
		customClient := &http.Client{}
		client, err := NewClient("http://localhost:8080", customClient)
		require.NoError(t, err)
		assert.NotNil(t, client)
		assert.Equal(t, customClient, client.httpClient)
	})
}

func TestClient_Plan(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedPlan := []PlanAction{
			{File: "/path/to/file1.txt", Action: ActionMove, Target: protocol.StringPtr("/new/path/file1.txt")},
			{File: "/path/to/file2.txt", Action: ActionSkip},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/plan", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var req PlanRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, "test-dir-id", req.Dir)
			assert.Equal(t, []string{"file1.txt"}, req.Files)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(PlanResponse{Plan: expectedPlan})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		req := &PlanRequest{
			Dir:      "test-dir-id",
			Files:    []string{"file1.txt"},
			Metadata: map[string]interface{}{"key": "value"},
		}

		resp, err := client.Plan(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Nil(t, resp.Error)
		assert.Equal(t, expectedPlan, resp.Plan)
	})

	t.Run("api error in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(PlanResponse{Error: protocol.StringPtr("internal organizer error")})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Plan(&PlanRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Error)
		assert.Equal(t, "internal organizer error", *resp.Error)
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server failure"))
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Plan(&PlanRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "plan request failed with status 500: server failure")
	})

	t.Run("http client error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close() // Close server to simulate network error

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Plan(&PlanRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "failed to send plan request")
	})

	t.Run("response decoding error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("not a json"))
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Plan(&PlanRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "failed to decode plan response")
	})
}

func TestClient_Execute(t *testing.T) {
	t.Run("full success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/execute", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var req ExecuteRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, "test-dir-id", req.Dir)
			assert.Len(t, req.Plan, 1)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		req := &ExecuteRequest{
			Dir:  "test-dir-id",
			Plan: []PlanAction{{File: "file.txt", Action: ActionMove, Target: protocol.StringPtr("new/file.txt")}},
		}

		success, failedResp, err := client.Execute(req)
		require.NoError(t, err)
		assert.True(t, success)
		assert.Nil(t, failedResp)
	})

	t.Run("partial failure", func(t *testing.T) {
		expectedFailures := []PlanFailed{
			{
				File:   "file2.txt",
				Action: ActionMove,
				Target: protocol.StringPtr("new/file2.txt"),
				Reason: "permission denied",
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ExecuteResponse{FailedMove: expectedFailures})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		req := &ExecuteRequest{
			Dir: "test-dir-id",
			Plan: []PlanAction{
				{File: "file1.txt", Action: ActionMove, Target: protocol.StringPtr("new/file1.txt")},
				{File: "file2.txt", Action: ActionMove, Target: protocol.StringPtr("new/file2.txt")},
			},
		}

		success, failedResp, err := client.Execute(req)
		require.NoError(t, err)
		assert.False(t, success)
		require.NotNil(t, failedResp)
		assert.Equal(t, expectedFailures, failedResp.FailedMove)
	})

	t.Run("response decoding error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("not a json"))
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		success, failedResp, err := client.Execute(&ExecuteRequest{})
		require.Error(t, err)
		assert.False(t, success)
		assert.Nil(t, failedResp)
		assert.Contains(t, err.Error(), "failed to decode execute response")
	})
}

func TestClient_Replan(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		expectedPlan := []PlanAction{
			{File: "/path/to/file1.txt", Action: ActionMove, Target: protocol.StringPtr("/new/path/file1.txt")},
			{File: "/path/to/file2.txt", Action: ActionSkip},
		}

		previousResponse := &PlanResponse{
			Plan: []PlanAction{
				{File: "/old/path/file1.txt", Action: ActionMove, Target: protocol.StringPtr("/old/target/file1.txt")},
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/v1/replan", r.URL.Path)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var req ReplanRequest
			err := json.NewDecoder(r.Body).Decode(&req)
			require.NoError(t, err)
			assert.Equal(t, []string{"file1.txt", "file2.txt"}, req.Files)
			assert.Equal(t, "move files to documents folder", req.UserHint)
			assert.Equal(t, previousResponse, req.PreviousResult)
			assert.Equal(t, map[string]interface{}{"key": "value"}, req.Metadata)

			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(PlanResponse{Plan: expectedPlan})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		req := &ReplanRequest{
			Files:          []string{"file1.txt", "file2.txt"},
			Metadata:       map[string]interface{}{"key": "value"},
			PreviousResult: previousResponse,
			UserHint:       "move files to documents folder",
		}

		resp, err := client.Replan(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Nil(t, resp.Error)
		assert.Equal(t, expectedPlan, resp.Plan)
	})

	t.Run("api error in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(PlanResponse{Error: protocol.StringPtr("organizer service unavailable")})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		req := &ReplanRequest{
			Files:    []string{"file1.txt"},
			UserHint: "test hint",
		}

		resp, err := client.Replan(req)
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.NotNil(t, resp.Error)
		assert.Equal(t, "organizer service unavailable", *resp.Error)
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Replan(&ReplanRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "replan request failed with status 500: internal server error")
	})

	t.Run("network error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		server.Close() // Close server to simulate network error

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Replan(&ReplanRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "failed to send replan request")
	})

	t.Run("response decoding error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("invalid json"))
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Replan(&ReplanRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "failed to decode replan response")
	})
}

func TestClient_UploadEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	completedDir := filepath.Join(tempDir, "completed")
	uploadDir := filepath.Join(completedDir, ".uploads")
	require.NoError(t, os.MkdirAll(completedDir, 0o755))

	store, err := upload.NewStore(uploadDir, completedDir, 1024*1024)
	require.NoError(t, err)

	handler := upload.NewHandler(store)
	mux := http.NewServeMux()
	upload.RegisterRoutes(mux, handler)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	client, err := NewClient(ts.URL, ts.Client())
	require.NoError(t, err)

	ctx := context.Background()
	torrentID := "torrent-client-test"
	relPath := "dir/sample.bin"
	totalSize := int64(40)

	// 1. InitUpload
	initResp, err := client.InitUpload(ctx, &UploadInitRequest{
		TorrentID:    torrentID,
		RelativePath: relPath,
		TotalSize:    totalSize,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(0), initResp.Offset)
	assert.Equal(t, protocol.StatusUploading, initResp.Status)

	// 2. GetUpload
	getResp, err := client.GetUpload(ctx, initResp.UploadID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), getResp.Offset)
	assert.Equal(t, totalSize, getResp.TotalSize)

	// 3. UploadChunk: 20 bytes
	chunk1 := bytes.Repeat([]byte("1"), 20)
	newOffset, err := client.UploadChunk(ctx, initResp.UploadID, 0, bytes.NewReader(chunk1), 20, "")
	require.NoError(t, err)
	assert.Equal(t, int64(20), newOffset)

	// 4. UploadChunk conflict: sending at offset 0 again
	_, err = client.UploadChunk(ctx, initResp.UploadID, 0, bytes.NewReader(chunk1), 20, "")
	require.Error(t, err)
	var conflictErr ErrUploadOffsetConflict
	require.ErrorAs(t, err, &conflictErr)
	assert.Equal(t, int64(20), conflictErr.ServerOffset)

	// 5. UploadChunk: remaining 20 bytes
	chunk2 := bytes.Repeat([]byte("2"), 20)
	newOffset, err = client.UploadChunk(ctx, initResp.UploadID, 20, bytes.NewReader(chunk2), 20, "")
	require.NoError(t, err)
	assert.Equal(t, int64(40), newOffset)

	// 6. FinishUpload
	err = client.FinishUpload(ctx, initResp.UploadID, &UploadFinishRequest{
		TorrentID:    torrentID,
		RelativePath: relPath,
	})
	require.NoError(t, err)

	// Verify target file on disk
	finalTarget := filepath.Join(completedDir, torrentID, relPath)
	data, err := os.ReadFile(finalTarget)
	require.NoError(t, err)
	assert.Equal(t, append(chunk1, chunk2...), data)

	// 7. CancelUpload on a new upload
	initResp2, err := client.InitUpload(ctx, &UploadInitRequest{
		TorrentID:    torrentID,
		RelativePath: "to_cancel.bin",
		TotalSize:    10,
	})
	require.NoError(t, err)
	err = client.CancelUpload(ctx, initResp2.UploadID)
	require.NoError(t, err)
}
