package organizer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
			{File: "/path/to/file1.txt", Action: ActionMove, Target: "/new/path/file1.txt"},
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
			json.NewEncoder(w).Encode(PlanResponse{Plan: expectedPlan})
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
		assert.Empty(t, resp.Error)
		assert.Equal(t, expectedPlan, resp.Plan)
	})

	t.Run("api error in response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(PlanResponse{Error: "internal organizer error"})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		resp, err := client.Plan(&PlanRequest{})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "internal organizer error", resp.Error)
	})

	t.Run("http error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server failure"))
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
			w.Write([]byte("not a json"))
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
			Plan: []PlanAction{{File: "file.txt", Action: ActionMove, Target: "new/file.txt"}},
		}

		success, failedResp, err := client.Execute(req)
		require.NoError(t, err)
		assert.True(t, success)
		assert.Nil(t, failedResp)
	})

	t.Run("partial failure", func(t *testing.T) {
		expectedFailures := []PlanFailed{
			{
				PlanAction: PlanAction{File: "file2.txt", Action: ActionMove, Target: "new/file2.txt"},
				Reason:     "permission denied",
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ExecuteResponse{FailedMoves: expectedFailures})
		}))
		defer server.Close()

		client, err := NewClient(server.URL, nil)
		require.NoError(t, err)

		req := &ExecuteRequest{
			Dir: "test-dir-id",
			Plan: []PlanAction{
				{File: "file1.txt", Action: ActionMove, Target: "new/file1.txt"},
				{File: "file2.txt", Action: ActionMove, Target: "new/file2.txt"},
			},
		}

		success, failedResp, err := client.Execute(req)
		require.NoError(t, err)
		assert.False(t, success)
		require.NotNil(t, failedResp)
		assert.Equal(t, expectedFailures, failedResp.FailedMoves)
	})

	t.Run("response decoding error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte("not a json"))
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
