package helpers

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/internal/db"
)

func TestDownloadTorrentFileFromURL_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxTorrentSize+10)))
	}))
	defer srv.Close()

	_, _, err := DownloadTorrentFileFromURL(srv.Client(), srv.URL, t.TempDir()+"/x.torrent", nil, nil)
	assert.ErrorContains(t, err, "too large")
}

func TestDownloadTorrentFileFromURL_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := DownloadTorrentFileFromURL(srv.Client(), srv.URL, t.TempDir()+"/x.torrent", nil, nil)
	assert.ErrorContains(t, err, "HTTP status error")
}

func torrentBytes(t *testing.T) []byte {
	t.Helper()
	info := metainfo.Info{Name: "x", Length: 4, PieceLength: 16384, Pieces: make([]byte, 20)}
	ib, err := bencode.Marshal(info)
	require.NoError(t, err)
	mb, err := bencode.Marshal(metainfo.MetaInfo{InfoBytes: ib})
	require.NoError(t, err)
	return mb
}

func torrentServer(t *testing.T) *httptest.Server {
	data := torrentBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	t.Cleanup(srv.Close)
	return srv
}

func dirEntries(t *testing.T, dir string) []string {
	es, err := os.ReadDir(dir)
	require.NoError(t, err)
	var n []string
	for _, e := range es {
		n = append(n, e.Name())
	}
	return n
}

func TestDownloadTorrentFileFromURL_Success(t *testing.T) {
	srv := torrentServer(t)
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	dir := t.TempDir()
	dest := filepath.Join(dir, "a.torrent")
	called := false
	_, _, err = DownloadTorrentFileFromURL(srv.Client(), srv.URL, dest, d, func(hash string) error {
		called = true
		assert.NotEmpty(t, hash)
		_, statErr := os.Stat(dest)
		assert.True(t, os.IsNotExist(statErr), "dest must not exist before hook")
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, []string{"a.torrent"}, dirEntries(t, dir))
}

func TestDownloadTorrentFileFromURL_HookError(t *testing.T) {
	srv := torrentServer(t)
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	dir := t.TempDir()
	_, _, err = DownloadTorrentFileFromURL(srv.Client(), srv.URL, filepath.Join(dir, "a.torrent"), d, func(string) error {
		return gorm.ErrDuplicatedKey
	})
	require.Error(t, err)
	assert.Equal(t, 409, DownloadHTTPError(err).Code)
	assert.Equal(t, "duplicate download", DownloadHTTPError(err).Message)
	assert.Empty(t, dirEntries(t, dir))
}

func TestDownloadTorrentFileFromURL_Duplicate(t *testing.T) {
	srv := torrentServer(t)
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	m, err := metainfo.Load(bytes.NewReader(torrentBytes(t)))
	require.NoError(t, err)
	require.NoError(t, d.Create(&db.DownloadStatus{ID: m.HashInfoBytes().HexString()}).Error)
	dir := t.TempDir()
	_, _, err = DownloadTorrentFileFromURL(srv.Client(), srv.URL, filepath.Join(dir, "a.torrent"), d, nil)
	assert.ErrorIs(t, err, ErrDuplicateDownload)
	assert.Empty(t, dirEntries(t, dir))
}

func TestDownloadTorrentFileFromURL_RenameFailure(t *testing.T) {
	srv := torrentServer(t)
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	dir := t.TempDir()
	// dest is an existing non-empty directory, so rename fails.
	dest := filepath.Join(dir, "dest")
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "sub"), 0o755))
	hooked := false
	_, _, err = DownloadTorrentFileFromURL(srv.Client(), srv.URL, dest, d, func(string) error { hooked = true; return nil })
	require.Error(t, err)
	assert.True(t, hooked)
	assert.Equal(t, []string{"dest"}, dirEntries(t, dir))
}

func TestDownloadHTTPError_HidesInternalError(t *testing.T) {
	e := DownloadHTTPError(errors.New("open /secret/path: permission denied"))
	assert.Equal(t, 500, e.Code)
	assert.NotContains(t, e.Message, "/secret/path")
}
