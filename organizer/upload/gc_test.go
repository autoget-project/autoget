package upload_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/protocol"

	"github.com/autoget-project/autoget/organizer/upload"
)

func TestGC_ExpiredCleanup(t *testing.T) {
	store, _, uploadDir := setupTestStore(t)
	gc := upload.NewGC(store, 24*time.Hour)

	// Create an upload session
	req := &protocol.UploadInitRequest{
		TorrentID:    "gc-torrent-1",
		RelativePath: "file1.mkv",
		TotalSize:    100,
	}
	resp, _, err := store.Init(req)
	require.NoError(t, err)

	partPath := filepath.Join(uploadDir, resp.UploadID+".part")
	metaPath := filepath.Join(uploadDir, resp.UploadID+".json")
	assert.FileExists(t, partPath)
	assert.FileExists(t, metaPath)

	// CleanNow when not expired (12h later) -> should not clean
	gc.CleanOnce(time.Now().Add(12 * time.Hour))
	assert.FileExists(t, partPath)
	assert.FileExists(t, metaPath)

	// CleanNow when expired (25h later) -> should clean
	gc.CleanOnce(time.Now().Add(25 * time.Hour))
	assert.NoFileExists(t, partPath)
	assert.NoFileExists(t, metaPath)
}

func TestGC_OrphanedPartFileCleanup(t *testing.T) {
	store, _, uploadDir := setupTestStore(t)
	gc := upload.NewGC(store, 24*time.Hour)

	// Create an orphaned .part file (no .json)
	orphanPart := filepath.Join(uploadDir, "orphan123.part")
	require.NoError(t, os.WriteFile(orphanPart, []byte("some orphaned bytes"), 0o644))

	// Set mod time to 30h ago
	oldTime := time.Now().Add(-30 * time.Hour)
	require.NoError(t, os.Chtimes(orphanPart, oldTime, oldTime))

	// GC run should remove orphaned part
	gc.CleanOnce(time.Now())
	assert.NoFileExists(t, orphanPart)
}

func TestGC_UpdatedAtRefreshPreservesActiveUpload(t *testing.T) {
	store, _, uploadDir := setupTestStore(t)
	gc := upload.NewGC(store, 24*time.Hour)

	req := &protocol.UploadInitRequest{
		TorrentID:    "gc-torrent-active",
		RelativePath: "active.bin",
		TotalSize:    200,
	}
	resp, _, err := store.Init(req)
	require.NoError(t, err)

	// Append chunk to refresh UpdatedAt
	chunk := bytes.Repeat([]byte("A"), 10)
	_, err = store.AppendChunk(resp.UploadID, 0, bytes.NewReader(chunk), 10, "")
	require.NoError(t, err)

	partPath := filepath.Join(uploadDir, resp.UploadID+".part")
	metaPath := filepath.Join(uploadDir, resp.UploadID+".json")

	// Even if 23 hours pass, it's still alive
	gc.CleanOnce(time.Now().Add(23 * time.Hour))
	assert.FileExists(t, partPath)
	assert.FileExists(t, metaPath)
}
