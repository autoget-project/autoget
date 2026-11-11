package transmission

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/organizer"
)

func TestSafely_RecoversPanic(t *testing.T) {
	c := &Client{name: "t"}
	assert.NotPanics(t, func() { c.safely("x", func() { panic("boom") }) })
}

func TestToTorrentsByHash_SkipsNilHash(t *testing.T) {
	h := "abc"
	m := toTorrentsByHash([]transmissionrpc.Torrent{{}, {HashString: &h}})
	assert.Len(t, m, 1)
	assert.Contains(t, m, "abc")
}

func TestStopTorrents_NilFields(t *testing.T) {
	testDB, err := db.SqliteForTest()
	require.NoError(t, err)
	c := &Client{name: "t", db: testDB}
	status := transmissionrpc.TorrentStatusSeed
	assert.NotPanics(t, func() {
		c.stopTorrents([]transmissionrpc.Torrent{{}, {Status: &status}})
	})
}

func TestUpdateDownloadProgress_NilFields(t *testing.T) {
	testDB, err := db.SqliteForTest()
	require.NoError(t, err)
	require.NoError(t, db.SaveDownloadStatus(testDB, &db.DownloadStatus{ID: "a", Downloader: "t", State: db.DownloadStarted}))
	c := &Client{name: "t", db: testDB}
	assert.NotPanics(t, func() {
		c.updateDownloadProgress(map[string]*transmissionrpc.Torrent{"a": {}})
	})

	got, err := db.GetDownloadStatus(testDB, "a")
	require.NoError(t, err)
	assert.Equal(t, db.DownloadStarted, got.State)
	assert.Equal(t, uint16(0), got.DownloadProgress)
	assert.Equal(t, uint64(0), got.Size)
}

func TestRegisterCronjobs_StopsOnContextCancel(t *testing.T) {
	old := progressInterval
	progressInterval = 5 * time.Millisecond
	defer func() { progressInterval = old }()

	c := &Client{name: "t", cfg: &config.DownloaderConfig{}}
	ctx, cancel := context.WithCancel(context.Background())
	c.RegisterCronjobs(ctx, cron.New())
	cancel()

	sctx, scancel := context.WithTimeout(context.Background(), time.Second)
	defer scancel()
	require.NoError(t, c.Shutdown(sctx))
}

func newFakeClient(t *testing.T, fake *fakeTransmission, d *gorm.DB, conf *config.DownloaderConfig) *Client {
	t.Helper()
	serv := httptest.NewServer(http.HandlerFunc(fake.ServeHTTP))
	httpClient = &http.Client{}
	t.Cleanup(func() {
		httpClient = http.DefaultClient
		serv.Close()
	})
	conf.Transmission.URL = serv.URL
	c, err := New("test", conf, d, nil)
	require.NoError(t, err)
	return c
}

func TestProgressChecker_SessionStatsError(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)

	tmpDir := t.TempDir()
	downloadDir := filepath.Join(tmpDir, "download")
	finishedDir := filepath.Join(tmpDir, "finished")
	require.NoError(t, os.Mkdir(downloadDir, 0755))
	require.NoError(t, os.Mkdir(finishedDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, "f.txt"), []byte("x"), 0644))

	// unmoved seeding download: would be copied if the checker fell through.
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "2", Downloader: "test", State: db.DownloadSeeding, MoveState: db.UnMoved}).Error)

	fake := &fakeTransmission{
		failMethods: map[string]bool{"session-stats": true},
		resp: []any{&torrentGetResults{Torrents: []transmissionrpc.Torrent{
			newTorrentWithProgress(2, "2", transmissionrpc.TorrentStatusSeed, 1.0, downloadDir, []transmissionrpc.TorrentFile{{Name: "f.txt", Length: 1}}),
		}}},
	}
	conf := &config.DownloaderConfig{Transmission: &config.TransmissionConfig{DownloadDir: downloadDir, FinishedDir: finishedDir}}
	c := newFakeClient(t, fake, d, conf)

	// organizer must never be consulted.
	organizerCalled := false
	organizerServ := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		organizerCalled = true
	}))
	t.Cleanup(organizerServ.Close)
	c.organizerClient, err = organizer.NewClient(organizerServ.URL, nil)
	require.NoError(t, err)

	c.ProgressChecker()

	require.Len(t, fake.reqs, 2)
	assert.Equal(t, "torrent-get", fake.reqs[0].Method)
	assert.Equal(t, "session-stats", fake.reqs[1].Method)
	assert.False(t, organizerCalled)

	got, err := db.GetDownloadStatus(d, "2")
	require.NoError(t, err)
	assert.Equal(t, db.UnMoved, got.MoveState)
	_, statErr := os.Stat(filepath.Join(finishedDir, "2", "f.txt"))
	assert.True(t, os.IsNotExist(statErr), "files must not be copied")
}

func TestStopTorrents_DBError(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)

	fake := &fakeTransmission{}
	conf := &config.DownloaderConfig{
		Transmission:  &config.TransmissionConfig{},
		SeedingPolicy: &config.SeedingPolicy{IntervalInDays: 1, UploadAtLeastInMB: 1},
	}
	c := newFakeClient(t, fake, d, conf)

	// Fail every query with a non-NotFound error.
	failing := true
	require.NoError(t, d.Callback().Query().Before("gorm:query").Register("test:fail", func(tx *gorm.DB) {
		if failing {
			_ = tx.AddError(errors.New("db unavailable"))
		}
	}))

	st := transmissionrpc.TorrentStatusSeed
	hash := "h1"
	id := int64(1)
	uploaded := int64(10)
	c.stopTorrents([]transmissionrpc.Torrent{{ID: &id, HashString: &hash, Status: &st, UploadedEver: &uploaded}})

	failing = false
	var n int64
	require.NoError(t, d.Model(&db.DownloadStatus{}).Count(&n).Error)
	assert.Zero(t, n, "no row may be written when the lookup fails")
	assert.Empty(t, fake.reqs, "no torrent-stop request may be sent")
}
