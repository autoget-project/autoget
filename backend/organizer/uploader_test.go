package organizer_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/upload"
	"github.com/autoget-project/autoget/protocol"

	"github.com/autoget-project/autoget/backend/organizer"
)

func setupUploaderTestEnv(t *testing.T) (*httptest.Server, *upload.Store, string) {
	tempDir := t.TempDir()
	completedDir := filepath.Join(tempDir, "completed")
	uploadDir := filepath.Join(completedDir, ".uploads")
	require.NoError(t, os.MkdirAll(completedDir, 0o755))

	store, err := upload.NewStore(uploadDir, completedDir, 1024*1024)
	require.NoError(t, err)

	mux := http.NewServeMux()
	upload.RegisterRoutes(mux, upload.NewHandler(store))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts, store, completedDir
}

func generateTestFile(t *testing.T, dir string, name string, size int64) (string, string) {
	filePath := filepath.Join(dir, name)
	buf := make([]byte, size)
	_, err := rand.Read(buf)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filePath, buf, 0o644))

	h := sha256.Sum256(buf)
	return filePath, hex.EncodeToString(h[:])
}

func fileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func TestUploader_NormalFullUpload(t *testing.T) {
	ts, _, completedDir := setupUploaderTestEnv(t)
	client, err := organizer.NewClient(ts.URL, ts.Client())
	require.NoError(t, err)

	uploader := organizer.NewUploader(client,
		organizer.WithChunkSize(64*1024), // 64KB chunks
		organizer.WithBackoffBase(10*time.Millisecond),
	)

	srcDir := t.TempDir()
	fileSize := int64(250 * 1024) // 250KB
	srcPath, expectedSha := generateTestFile(t, srcDir, "movie.mkv", fileSize)

	var lastUploaded int64
	onProgress := func(bytesUploaded, totalBytes int64) {
		lastUploaded = bytesUploaded
	}

	req := &protocol.UploadInitRequest{
		TorrentID:    "torrent-full-1",
		RelativePath: "nested/movie.mkv",
		TotalSize:    fileSize,
	}

	err = uploader.UploadFile(context.Background(), srcPath, req, onProgress)
	require.NoError(t, err)
	assert.Equal(t, fileSize, lastUploaded)

	finalTarget := filepath.Join(completedDir, "torrent-full-1", "nested", "movie.mkv")
	assert.FileExists(t, finalTarget)

	actualSha, err := fileSHA256(finalTarget)
	require.NoError(t, err)
	assert.Equal(t, expectedSha, actualSha)

	// Second run of UploadFile on the same completed file should be instant and succeed
	err = uploader.UploadFile(context.Background(), srcPath, req, nil)
	require.NoError(t, err)
}

func TestUploader_ZeroByteFile(t *testing.T) {
	ts, _, completedDir := setupUploaderTestEnv(t)
	client, err := organizer.NewClient(ts.URL, ts.Client())
	require.NoError(t, err)

	uploader := organizer.NewUploader(client)

	srcDir := t.TempDir()
	emptyPath := filepath.Join(srcDir, "empty.nfo")
	require.NoError(t, os.WriteFile(emptyPath, []byte{}, 0o644))

	req := &protocol.UploadInitRequest{
		TorrentID:    "torrent-empty",
		RelativePath: "empty.nfo",
		TotalSize:    0,
	}

	err = uploader.UploadFile(context.Background(), emptyPath, req, nil)
	require.NoError(t, err)

	finalTarget := filepath.Join(completedDir, "torrent-empty", "empty.nfo")
	fi, err := os.Stat(finalTarget)
	require.NoError(t, err)
	assert.Equal(t, int64(0), fi.Size())
}

// faultTransport intercepts RoundTrip to inject simulated connection failures or drops.
type faultTransport struct {
	base          http.RoundTripper
	dropNextPatch atomic.Bool
	dropNextResp  atomic.Bool
	injectTimeout atomic.Bool
}

func (f *faultTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.injectTimeout.Load() {
		return nil, errors.New("simulated network timeout")
	}

	if r.Method == http.MethodPatch && f.dropNextPatch.CompareAndSwap(true, false) {
		// Read part of body and drop connection
		lr := io.LimitReader(r.Body, 1024)
		_, _ = io.ReadAll(lr)
		return nil, errors.New("simulated connection reset by peer")
	}

	resp, err := f.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	if r.Method == http.MethodPatch && f.dropNextResp.CompareAndSwap(true, false) {
		// Response was generated (data landed on server), but client drops response
		_ = resp.Body.Close()
		return nil, errors.New("simulated connection drop after server write")
	}

	return resp, nil
}

func TestUploader_FaultInjectionAndResume(t *testing.T) {
	ts, _, completedDir := setupUploaderTestEnv(t)

	faultyTransport := &faultTransport{base: http.DefaultTransport}
	faultyClient := &http.Client{Transport: faultyTransport}

	client, err := organizer.NewClient(ts.URL, faultyClient)
	require.NoError(t, err)

	uploader := organizer.NewUploader(client,
		organizer.WithChunkSize(32*1024), // 32KB
		organizer.WithBackoffBase(5*time.Millisecond),
		organizer.WithMaxRetries(3),
	)

	srcDir := t.TempDir()
	fileSize := int64(128 * 1024) // 128KB (4 chunks of 32KB)
	srcPath, expectedSha := generateTestFile(t, srcDir, "faulty.bin", fileSize)

	// Inject a drop on the first PATCH request
	faultyTransport.dropNextPatch.Store(true)

	req := &protocol.UploadInitRequest{
		TorrentID:    "torrent-fault",
		RelativePath: "faulty.bin",
		TotalSize:    fileSize,
	}

	err = uploader.UploadFile(context.Background(), srcPath, req, nil)
	require.NoError(t, err)

	finalTarget := filepath.Join(completedDir, "torrent-fault", "faulty.bin")
	actualSha, err := fileSHA256(finalTarget)
	require.NoError(t, err)
	assert.Equal(t, expectedSha, actualSha)
}

func TestUploader_DropResponse409SelfHealing(t *testing.T) {
	ts, _, completedDir := setupUploaderTestEnv(t)

	faultyTransport := &faultTransport{base: http.DefaultTransport}
	faultyClient := &http.Client{Transport: faultyTransport}

	client, err := organizer.NewClient(ts.URL, faultyClient)
	require.NoError(t, err)

	uploader := organizer.NewUploader(client,
		organizer.WithChunkSize(32*1024), // 32KB
		organizer.WithBackoffBase(5*time.Millisecond),
		organizer.WithMaxRetries(3),
	)

	srcDir := t.TempDir()
	fileSize := int64(128 * 1024) // 128KB
	srcPath, expectedSha := generateTestFile(t, srcDir, "drop204.bin", fileSize)

	// Simulate server writing chunk 1 successfully, but client loses the HTTP 204 response
	faultyTransport.dropNextResp.Store(true)

	req := &protocol.UploadInitRequest{
		TorrentID:    "torrent-drop204",
		RelativePath: "drop204.bin",
		TotalSize:    fileSize,
	}

	err = uploader.UploadFile(context.Background(), srcPath, req, nil)
	require.NoError(t, err)

	finalTarget := filepath.Join(completedDir, "torrent-drop204", "drop204.bin")
	actualSha, err := fileSHA256(finalTarget)
	require.NoError(t, err)
	assert.Equal(t, expectedSha, actualSha)
}

func TestUploader_RetryExhaustionReturnsParked(t *testing.T) {
	ts, _, _ := setupUploaderTestEnv(t)

	faultyTransport := &faultTransport{base: http.DefaultTransport}
	faultyClient := &http.Client{Transport: faultyTransport}

	client, err := organizer.NewClient(ts.URL, faultyClient)
	require.NoError(t, err)

	uploader := organizer.NewUploader(client,
		organizer.WithChunkSize(32*1024),
		organizer.WithBackoffBase(1*time.Millisecond),
		organizer.WithMaxRetries(2),
	)

	srcDir := t.TempDir()
	srcPath, _ := generateTestFile(t, srcDir, "fail.bin", 64*1024)

	// Make server permanently unreachable for this test
	faultyTransport.injectTimeout.Store(true)

	req := &protocol.UploadInitRequest{
		TorrentID:    "torrent-timeout",
		RelativePath: "fail.bin",
		TotalSize:    64 * 1024,
	}

	err = uploader.UploadFile(context.Background(), srcPath, req, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, organizer.ErrParked), "expected ErrParked on failure")
}
