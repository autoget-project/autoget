package upload_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/protocol"

	"github.com/autoget-project/autoget/organizer/upload"
)

func setupTestStore(t *testing.T) (*upload.Store, string, string) {
	tempDir := t.TempDir()
	completedDir := filepath.Join(tempDir, "completed")
	uploadDir := filepath.Join(completedDir, ".uploads")
	require.NoError(t, os.MkdirAll(completedDir, 0o755))

	store, err := upload.NewStore(uploadDir, completedDir, 1024*1024) // 1MB reserve
	require.NoError(t, err)
	return store, completedDir, uploadDir
}

func TestStore_InitAndAppendFlow(t *testing.T) {
	store, completedDir, _ := setupTestStore(t)

	req := &protocol.UploadInitRequest{
		TorrentID:    "test-torrent-1",
		RelativePath: "folder/test.bin",
		TotalSize:    100,
	}

	// 1. First init -> created (isNew = true, offset = 0)
	resp, isNew, err := store.Init(req)
	require.NoError(t, err)
	assert.True(t, isNew)
	assert.Equal(t, int64(0), resp.Offset)
	assert.Equal(t, int64(100), resp.TotalSize)
	assert.Equal(t, protocol.StatusUploading, resp.Status)

	// 2. Append chunk 1: 40 bytes at offset 0
	chunk1 := bytes.Repeat([]byte("A"), 40)
	newOffset, err := store.AppendChunk(resp.UploadID, 0, bytes.NewReader(chunk1), 40, "")
	require.NoError(t, err)
	assert.Equal(t, int64(40), newOffset)

	// 3. Idempotent init -> returns existing session (isNew = false, offset = 40)
	resp2, isNew2, err := store.Init(req)
	require.NoError(t, err)
	assert.False(t, isNew2)
	assert.Equal(t, int64(40), resp2.Offset)

	// 4. Conflicting offset on append
	_, err = store.AppendChunk(resp.UploadID, 0, bytes.NewReader(chunk1), 40, "")
	var conflictErr upload.ErrOffsetConflict
	require.True(t, errors.As(err, &conflictErr))
	assert.Equal(t, int64(40), conflictErr.LastOffset)

	// 5. Append chunk 2: 60 bytes at offset 40 with checksum
	chunk2 := bytes.Repeat([]byte("B"), 60)
	h := sha256.Sum256(chunk2)
	chunk2Sha := hex.EncodeToString(h[:])
	newOffset, err = store.AppendChunk(resp.UploadID, 40, bytes.NewReader(chunk2), 60, chunk2Sha)
	require.NoError(t, err)
	assert.Equal(t, int64(100), newOffset)

	// 6. Finish upload
	err = store.Finish(resp.UploadID, nil)
	require.NoError(t, err)

	// Final destination check
	finalTarget := filepath.Join(completedDir, "test-torrent-1", "folder", "test.bin")
	data, err := os.ReadFile(finalTarget)
	require.NoError(t, err)
	expectedData := append(bytes.Repeat([]byte("A"), 40), bytes.Repeat([]byte("B"), 60)...)
	assert.Equal(t, expectedData, data)

	// Once finished, init should report completed
	respCompleted, isNewComp, err := store.Init(req)
	require.NoError(t, err)
	assert.False(t, isNewComp)
	assert.Equal(t, protocol.StatusCompleted, respCompleted.Status)
}

func TestStore_ZeroByteFile(t *testing.T) {
	store, completedDir, _ := setupTestStore(t)

	req := &protocol.UploadInitRequest{
		TorrentID:    "test-torrent-zero",
		RelativePath: "empty.txt",
		TotalSize:    0,
	}

	resp, isNew, err := store.Init(req)
	require.NoError(t, err)
	assert.False(t, isNew)
	assert.Equal(t, protocol.StatusCompleted, resp.Status)

	finalTarget := filepath.Join(completedDir, "test-torrent-zero", "empty.txt")
	fi, err := os.Stat(finalTarget)
	require.NoError(t, err)
	assert.Equal(t, int64(0), fi.Size())
}

func TestStore_TruncationSelfHealing(t *testing.T) {
	store, completedDir, uploadDir := setupTestStore(t)

	req := &protocol.UploadInitRequest{
		TorrentID:    "test-torrent-heal",
		RelativePath: "video.mkv",
		TotalSize:    200,
	}

	resp, _, err := store.Init(req)
	require.NoError(t, err)

	// Append 50 bytes successfully
	chunk := bytes.Repeat([]byte("X"), 50)
	_, err = store.AppendChunk(resp.UploadID, 0, bytes.NewReader(chunk), 50, "")
	require.NoError(t, err)

	// Simulate crash: manually append garbage 30 bytes to .part file without updating .json
	partPath := filepath.Join(uploadDir, resp.UploadID+".part")
	f, err := os.OpenFile(partPath, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	_, err = f.Write(bytes.Repeat([]byte("Z"), 30))
	require.NoError(t, err)
	_ = f.Close()

	fi, err := os.Stat(partPath)
	require.NoError(t, err)
	assert.Equal(t, int64(80), fi.Size()) // physical size is 80

	// Reload via NewStore to simulate service restart
	store2, err := upload.NewStore(uploadDir, completedDir, 1024*1024)
	require.NoError(t, err)

	// Calling Get should trigger truncation self-healing back to 50
	status, err := store2.Get(resp.UploadID)
	require.NoError(t, err)
	assert.Equal(t, int64(50), status.Offset)

	fiAfter, err := os.Stat(partPath)
	require.NoError(t, err)
	assert.Equal(t, int64(50), fiAfter.Size())
}

func TestStore_PathSecurityChecks(t *testing.T) {
	store, _, _ := setupTestStore(t)

	testCases := []struct {
		torrentID string
		relPath   string
		valid     bool
	}{
		{"valid-id_123", "sub/file.mkv", true},
		{"valid-id_123", "/absolute/path", false},
		{"valid-id_123", "../escape.mkv", false},
		{"valid-id_123", "sub/../../escape.mkv", false},
		{"invalid/slash", "file.mkv", false},
		{"", "file.mkv", false},
		{"valid-id", "", false},
	}

	for _, tc := range testCases {
		err := store.ValidateParams(tc.torrentID, tc.relPath)
		if tc.valid {
			assert.NoError(t, err, "expected valid: %s / %s", tc.torrentID, tc.relPath)
		} else {
			assert.Error(t, err, "expected error: %s / %s", tc.torrentID, tc.relPath)
		}
	}
}

func TestStore_ConcurrentAppendsSerialized(t *testing.T) {
	store, _, _ := setupTestStore(t)

	req := &protocol.UploadInitRequest{
		TorrentID:    "test-concurrent",
		RelativePath: "data.bin",
		TotalSize:    1000,
	}

	resp, _, err := store.Init(req)
	require.NoError(t, err)

	// Concurrently attempt to append at offset 0
	// Only one must succeed; the others must get ErrOffsetConflict
	const routines = 10
	var wg sync.WaitGroup
	successCount := 0
	conflictCount := 0
	var mu sync.Mutex

	for i := 0; i < routines; i++ {
		wg.Add(1)
		go func(val byte) {
			defer wg.Done()
			chunk := bytes.Repeat([]byte{val}, 50)
			_, err := store.AppendChunk(resp.UploadID, 0, bytes.NewReader(chunk), 50, "")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				successCount++
			} else {
				var conflict upload.ErrOffsetConflict
				if errors.As(err, &conflict) {
					conflictCount++
				}
			}
		}(byte(i))
	}

	wg.Wait()
	assert.Equal(t, 1, successCount)
	assert.Equal(t, routines-1, conflictCount)
}

func TestStore_CancelIdempotent(t *testing.T) {
	store, _, uploadDir := setupTestStore(t)

	req := &protocol.UploadInitRequest{
		TorrentID:    "test-cancel",
		RelativePath: "cancelled.bin",
		TotalSize:    100,
	}

	resp, _, err := store.Init(req)
	require.NoError(t, err)

	// Cancel once
	require.NoError(t, store.Cancel(resp.UploadID))

	// Verify part and json are gone
	assert.NoFileExists(t, filepath.Join(uploadDir, resp.UploadID+".part"))
	assert.NoFileExists(t, filepath.Join(uploadDir, resp.UploadID+".json"))

	// Cancel again (idempotent)
	require.NoError(t, store.Cancel(resp.UploadID))
}
