package transmission

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/protocol"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/db/dbtest"
	"github.com/autoget-project/autoget/backend/organizer"
)

func deleteTestClient(t *testing.T, fake *fakeTransmission, status *db.DownloadStatus) *Client {
	t.Helper()
	d, err := dbtest.SqliteForTest()
	require.NoError(t, err)
	if status != nil {
		require.NoError(t, d.Create(status).Error)
	}
	return newFakeClient(t, fake, d, &config.DownloaderConfig{Transmission: &config.TransmissionConfig{
		TorrentsDir: "/torrents",
		DownloadDir: "/downloads",
	}})
}

func stateOf(t *testing.T, c *Client, id string) db.DownloadStatus {
	t.Helper()
	var st db.DownloadStatus
	require.NoError(t, c.db.First(&st, "id = ?", id).Error)
	return st
}

func TestGetters(t *testing.T) {
	c := deleteTestClient(t, &fakeTransmission{}, nil)
	assert.Equal(t, "/torrents", c.TorrentsDir())
	assert.Equal(t, "/downloads", c.DownloadDir())
}

func TestDeleteTorrent_NotFound(t *testing.T) {
	tor := newTorrent(1, "other", transmissionrpc.TorrentStatusSeed, 0)
	fake := &fakeTransmission{resp: []any{map[string]any{"torrents": []transmissionrpc.Torrent{tor}}}}
	c := deleteTestClient(t, fake, nil)

	err := c.DeleteTorrent("missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "torrent not found")
	assert.Len(t, fake.reqs, 1, "no remove may be sent")
}

func TestDeleteTorrent_GetAllError(t *testing.T) {
	fake := &fakeTransmission{failMethods: map[string]bool{"torrent-get": true}}
	c := deleteTestClient(t, fake, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadSeeding})

	require.Error(t, c.DeleteTorrent("h"))
	assert.Equal(t, db.DownloadSeeding, stateOf(t, c, "h").State)
}

func TestDeleteTorrent_Success(t *testing.T) {
	tor := newTorrent(7, "h", transmissionrpc.TorrentStatusSeed, 0)
	fake := &fakeTransmission{resp: []any{
		map[string]any{"torrents": []transmissionrpc.Torrent{tor}},
		map[string]any{},
	}}
	c := deleteTestClient(t, fake, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadSeeding, MoveState: db.UnMoved})

	require.NoError(t, c.DeleteTorrent("h"))

	require.Len(t, fake.reqs, 2)
	assert.Equal(t, "torrent-remove", fake.reqs[1].Method)
	args, ok := fake.reqs[1].Arguments.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, args["delete-local-data"])
	assert.Equal(t, []any{float64(7)}, args["ids"])
	assert.Equal(t, db.DownloadDeleted, stateOf(t, c, "h").State)
}

func TestDeleteTorrent_RemoveErrorLeavesDBUnchanged(t *testing.T) {
	tor := newTorrent(7, "h", transmissionrpc.TorrentStatusSeed, 0)
	fake := &fakeTransmission{
		resp:        []any{map[string]any{"torrents": []transmissionrpc.Torrent{tor}}},
		failMethods: map[string]bool{"torrent-remove": true},
	}
	c := deleteTestClient(t, fake, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadSeeding})

	require.Error(t, c.DeleteTorrent("h"))
	assert.Equal(t, db.DownloadSeeding, stateOf(t, c, "h").State)
}

func TestDeleteTorrent_MovingCancelsUploads(t *testing.T) {
	var mu sync.Mutex
	var cancelled []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			cancelled = append(cancelled, filepath.Base(r.URL.Path))
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	org, err := organizer.NewClient(srv.URL, srv.Client())
	require.NoError(t, err)

	tor := newTorrent(7, "h", transmissionrpc.TorrentStatusSeed, 0)
	tor.Files = []transmissionrpc.TorrentFile{{Name: "a.mkv"}, {Name: "sub/b.mkv"}}
	fake := &fakeTransmission{resp: []any{
		map[string]any{"torrents": []transmissionrpc.Torrent{tor}},
		map[string]any{},
	}}
	c := deleteTestClient(t, fake, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadSeeding, MoveState: db.Moving})
	c.organizerClient = org

	require.NoError(t, c.DeleteTorrent("h"))

	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{
		protocol.DeriveUploadID("h", "a.mkv"),
		protocol.DeriveUploadID("h", "sub/b.mkv"),
	}, cancelled)
	assert.Equal(t, db.DownloadDeleted, stateOf(t, c, "h").State)
}

func TestRemoveTorrents_RPCErrorKeepsState(t *testing.T) {
	tor := newTorrent(3, "h", transmissionrpc.TorrentStatusStopped, 0)
	fake := &fakeTransmission{failMethods: map[string]bool{"torrent-remove": true}}
	c := deleteTestClient(t, fake, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadStopped, MoveState: db.Moved})

	c.removeTorrents(map[string]*transmissionrpc.Torrent{"h": &tor})

	require.Len(t, fake.reqs, 1)
	assert.Equal(t, db.DownloadStopped, stateOf(t, c, "h").State)
}

func TestRemoveTorrents_SkipsNilIDAndMissing(t *testing.T) {
	hash := "h"
	fake := &fakeTransmission{}
	c := deleteTestClient(t, fake, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadStopped, MoveState: db.Moved})
	require.NoError(t, c.db.Create(&db.DownloadStatus{ID: "gone", Downloader: "test", State: db.DownloadStopped, MoveState: db.Moved}).Error)

	c.removeTorrents(map[string]*transmissionrpc.Torrent{hash: {HashString: &hash}})

	assert.Empty(t, fake.reqs, "nothing to remove -> no RPC")
	assert.Equal(t, db.DownloadStopped, stateOf(t, c, "h").State)
}

func TestUploadTorrentFiles_Failures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	org, err := organizer.NewClient(srv.URL, srv.Client())
	require.NoError(t, err)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.mkv"), []byte("data"), 0o644))

	c := deleteTestClient(t, &fakeTransmission{}, &db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadSeeding, MoveState: db.Moving})
	c.organizerClient = org
	c.uploader = organizer.NewUploader(org)

	t.Run("organizer 500 keeps Moving", func(t *testing.T) {
		tor := &transmissionrpc.Torrent{DownloadDir: &dir, Files: []transmissionrpc.TorrentFile{{Name: "a.mkv"}}}
		s := stateOf(t, c, "h")
		require.Error(t, c.uploadTorrentFiles(tor, &s))
		assert.Equal(t, db.Moving, stateOf(t, c, "h").MoveState)
	})

	t.Run("missing source file keeps Moving", func(t *testing.T) {
		tor := &transmissionrpc.Torrent{DownloadDir: &dir, Files: []transmissionrpc.TorrentFile{{Name: "nope.mkv"}}}
		s := stateOf(t, c, "h")
		require.Error(t, c.uploadTorrentFiles(tor, &s))
		assert.Equal(t, db.Moving, stateOf(t, c, "h").MoveState)
	})
}
