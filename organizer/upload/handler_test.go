package upload_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/protocol"

	"github.com/autoget-project/autoget/organizer/upload"
)

func setupTestServer(t *testing.T) (*httptest.Server, *upload.Store, string) {
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
	t.Cleanup(ts.Close)

	return ts, store, completedDir
}

func TestHandler_EndToEndLifecycle(t *testing.T) {
	ts, _, completedDir := setupTestServer(t)
	client := ts.Client()

	torrentID := "torrent-lifecycle-1"
	relPath := "season1/ep01.mp4"
	totalSize := int64(30)

	// 1. POST /v1/upload/init -> 201 Created
	initReq := protocol.UploadInitRequest{
		TorrentID:    torrentID,
		RelativePath: relPath,
		TotalSize:    totalSize,
	}
	initBody, _ := json.Marshal(initReq)
	resp, err := client.Post(ts.URL+"/v1/upload/init", "application/json", bytes.NewReader(initBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var initResp protocol.UploadStatusResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&initResp))
	assert.Equal(t, int64(0), initResp.Offset)
	assert.Equal(t, protocol.StatusUploading, initResp.Status)
	uploadID := initResp.UploadID

	// 2. GET /v1/upload/{id} -> 200 OK
	getResp, err := client.Get(ts.URL + "/v1/upload/" + uploadID)
	require.NoError(t, err)
	defer func() { _ = getResp.Body.Close() }()
	assert.Equal(t, http.StatusOK, getResp.StatusCode)
	assert.Equal(t, "0", getResp.Header.Get(protocol.UploadOffsetHeader))
	assert.Equal(t, strconv.FormatInt(totalSize, 10), getResp.Header.Get(protocol.UploadLengthHeader))

	// 3. PATCH /v1/upload/{id} chunk 1 (10 bytes)
	chunk1 := []byte("0123456789")
	patchReq, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/upload/"+uploadID, bytes.NewReader(chunk1))
	require.NoError(t, err)
	patchReq.Header.Set(protocol.UploadOffsetHeader, "0")
	patchReq.Header.Set("Content-Type", protocol.UploadContentType)
	patchReq.ContentLength = int64(len(chunk1))

	patchResp, err := client.Do(patchReq)
	require.NoError(t, err)
	defer func() { _ = patchResp.Body.Close() }()
	assert.Equal(t, http.StatusNoContent, patchResp.StatusCode)
	assert.Equal(t, "10", patchResp.Header.Get(protocol.UploadOffsetHeader))

	// 4. PATCH with wrong offset -> 409 Conflict with Upload-Offset header
	patchReqConflict, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/upload/"+uploadID, bytes.NewReader(chunk1))
	require.NoError(t, err)
	patchReqConflict.Header.Set(protocol.UploadOffsetHeader, "0") // Stale offset
	patchReqConflict.Header.Set("Content-Type", protocol.UploadContentType)
	patchReqConflict.ContentLength = int64(len(chunk1))

	patchRespConflict, err := client.Do(patchReqConflict)
	require.NoError(t, err)
	defer func() { _ = patchRespConflict.Body.Close() }()
	assert.Equal(t, http.StatusConflict, patchRespConflict.StatusCode)
	assert.Equal(t, "10", patchRespConflict.Header.Get(protocol.UploadOffsetHeader))

	// 5. PATCH chunk 2 (20 bytes) with SHA256 checksum
	chunk2 := []byte("abcdefghijklmnopqrst")
	h := sha256.Sum256(chunk2)
	chunk2Sha := hex.EncodeToString(h[:])

	patchReq2, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/upload/"+uploadID, bytes.NewReader(chunk2))
	require.NoError(t, err)
	patchReq2.Header.Set(protocol.UploadOffsetHeader, "10")
	patchReq2.Header.Set(protocol.UploadChecksumHeader, chunk2Sha)
	patchReq2.Header.Set("Content-Type", protocol.UploadContentType)
	patchReq2.ContentLength = int64(len(chunk2))

	patchResp2, err := client.Do(patchReq2)
	require.NoError(t, err)
	defer func() { _ = patchResp2.Body.Close() }()
	assert.Equal(t, http.StatusNoContent, patchResp2.StatusCode)
	assert.Equal(t, "30", patchResp2.Header.Get(protocol.UploadOffsetHeader))

	// 6. POST /v1/upload/{id}/finish -> 200 OK
	finishReq := protocol.UploadFinishRequest{
		TorrentID:    torrentID,
		RelativePath: relPath,
	}
	finishBody, _ := json.Marshal(finishReq)
	finishResp, err := client.Post(ts.URL+"/v1/upload/"+uploadID+"/finish", "application/json", bytes.NewReader(finishBody))
	require.NoError(t, err)
	defer func() { _ = finishResp.Body.Close() }()
	assert.Equal(t, http.StatusOK, finishResp.StatusCode)

	var finishRespBody protocol.UploadFinishResponse
	require.NoError(t, json.NewDecoder(finishResp.Body).Decode(&finishRespBody))
	assert.Equal(t, protocol.StatusCompleted, finishRespBody.Status)

	// Verify target file content
	targetFile := filepath.Join(completedDir, torrentID, relPath)
	data, err := os.ReadFile(targetFile)
	require.NoError(t, err)
	assert.Equal(t, append(chunk1, chunk2...), data)

	// 7. Idempotent finish call with TorrentID and RelativePath -> 200 OK
	finishResp2, err := client.Post(ts.URL+"/v1/upload/"+uploadID+"/finish", "application/json", bytes.NewReader(finishBody))
	require.NoError(t, err)
	defer func() { _ = finishResp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, finishResp2.StatusCode)
}

func TestHandler_ExceedMaxChunkSize(t *testing.T) {
	_, store, _ := setupTestServer(t)
	h := upload.NewHandler(store)

	req := httptest.NewRequest(http.MethodPatch, "/v1/upload/some-id", bytes.NewReader([]byte{}))
	req.SetPathValue("id", "some-id")
	req.Header.Set(protocol.UploadOffsetHeader, "0")
	req.ContentLength = protocol.MaxChunkSize + 1

	w := httptest.NewRecorder()
	h.HandlePatch(w, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestHandler_DeleteCancelsUpload(t *testing.T) {
	ts, _, _ := setupTestServer(t)
	client := ts.Client()

	initReq := protocol.UploadInitRequest{
		TorrentID:    "torrent-del",
		RelativePath: "file.mkv",
		TotalSize:    100,
	}
	initBody, _ := json.Marshal(initReq)
	initResp, err := client.Post(ts.URL+"/v1/upload/init", "application/json", bytes.NewReader(initBody))
	require.NoError(t, err)
	defer func() { _ = initResp.Body.Close() }()

	var res protocol.UploadStatusResponse
	_ = json.NewDecoder(initResp.Body).Decode(&res)

	// DELETE /v1/upload/{id} -> 204
	delReq, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/v1/upload/%s", ts.URL, res.UploadID), nil)
	require.NoError(t, err)
	delResp, err := client.Do(delReq)
	require.NoError(t, err)
	defer func() { _ = delResp.Body.Close() }()
	assert.Equal(t, http.StatusNoContent, delResp.StatusCode)

	// GET now should return 404
	getResp, err := client.Get(fmt.Sprintf("%s/v1/upload/%s", ts.URL, res.UploadID))
	require.NoError(t, err)
	defer func() { _ = getResp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, getResp.StatusCode)
}
