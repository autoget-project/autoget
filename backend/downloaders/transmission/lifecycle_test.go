package transmission

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/notify"
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
	c, err := New("test", conf, d, nil, nil)
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

func TestSafeRel(t *testing.T) {
	for _, ok := range []string{"a.mkv", "sub/a.mkv", "a/../b"} {
		assert.NoError(t, safeRel(ok), ok)
	}
	for _, bad := range []string{"../x", "/abs", "a/../../x", "", ".."} {
		assert.Error(t, safeRel(bad), bad)
	}
}

func TestCopyFileAtomic(t *testing.T) {
	dir := t.TempDir()
	from := filepath.Join(dir, "src.bin")
	require.NoError(t, os.WriteFile(from, []byte("payload"), 0o644))

	target := filepath.Join(dir, "out", "nested", "dst.bin")
	require.NoError(t, copyFileAtomic(context.Background(), from, target))
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "payload", string(got))
	assert.NoFileExists(t, target+".part")
}

func TestCopyFileAtomic_FailureLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out", "dst.bin")

	// missing source
	require.Error(t, copyFileAtomic(context.Background(), filepath.Join(dir, "missing"), target))
	assert.NoFileExists(t, target)
	assert.NoFileExists(t, target+".part")

	// cancelled context aborts mid-copy and cleans the .part file
	from := filepath.Join(dir, "src.bin")
	require.NoError(t, os.WriteFile(from, []byte("payload"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, copyFileAtomic(ctx, from, target), context.Canceled)
	assert.NoFileExists(t, target)
	assert.NoFileExists(t, target+".part")
}

func TestCopyFinishedDownloads_RejectsUnsafePath(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	tmp := t.TempDir()
	downloadDir := filepath.Join(tmp, "download")
	finishedDir := filepath.Join(tmp, "finished")
	require.NoError(t, os.MkdirAll(downloadDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "secret"), []byte("s"), 0o644))
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "h", Downloader: "t", State: db.DownloadSeeding, MoveState: db.UnMoved}).Error)

	c := &Client{name: "t", db: d, cfg: &config.DownloaderConfig{Transmission: &config.TransmissionConfig{FinishedDir: finishedDir}},
		inFlight: map[string]bool{}, uploadSem: make(chan struct{}, 1)}
	hash := "h"
	c.copyFinishedDownloads(map[string]*transmissionrpc.Torrent{"h": {
		HashString: &hash, DownloadDir: &downloadDir,
		Files: []transmissionrpc.TorrentFile{{Name: "../secret"}},
	}})
	c.wg.Wait()

	got, err := db.GetDownloadStatus(d, "h")
	require.NoError(t, err)
	assert.Equal(t, db.UnMoved, got.MoveState)
	assert.NoDirExists(t, finishedDir)
}

func TestCopyFinishedDownloads_LocalIsAsyncAndDeduped(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	tmp := t.TempDir()
	downloadDir := filepath.Join(tmp, "download")
	finishedDir := filepath.Join(tmp, "finished")
	require.NoError(t, os.MkdirAll(downloadDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, "f.txt"), []byte("data"), 0o644))
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "h", Downloader: "t", State: db.DownloadSeeding, MoveState: db.UnMoved}).Error)

	c := &Client{name: "t", db: d, cfg: &config.DownloaderConfig{Transmission: &config.TransmissionConfig{FinishedDir: finishedDir}},
		inFlight: map[string]bool{}, uploadSem: make(chan struct{}, 1)}

	// Occupy the only worker slot so the copy cannot start: the call must
	// still return immediately, and a second tick must not dispatch again.
	c.uploadSem <- struct{}{}
	hash := "h"
	torrents := map[string]*transmissionrpc.Torrent{"h": {
		HashString: &hash, DownloadDir: &downloadDir,
		Files: []transmissionrpc.TorrentFile{{Name: "f.txt"}},
	}}
	c.copyFinishedDownloads(torrents)
	c.copyFinishedDownloads(torrents)

	got, err := db.GetDownloadStatus(d, "h")
	require.NoError(t, err)
	assert.Equal(t, db.Moving, got.MoveState)
	assert.NoFileExists(t, filepath.Join(finishedDir, "h", "f.txt"))

	<-c.uploadSem
	c.wg.Wait()

	got, err = db.GetDownloadStatus(d, "h")
	require.NoError(t, err)
	assert.Equal(t, db.Moved, got.MoveState)
	assert.Equal(t, []string{"f.txt"}, []string(got.FileList))
	assert.FileExists(t, filepath.Join(finishedDir, "h", "f.txt"))
}

func TestShutdown_CancelsQueuedCopy(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	tmp := t.TempDir()
	downloadDir := filepath.Join(tmp, "download")
	finishedDir := filepath.Join(tmp, "finished")
	require.NoError(t, os.MkdirAll(downloadDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, "f.txt"), []byte("data"), 0o644))
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "h", Downloader: "t", State: db.DownloadSeeding, MoveState: db.UnMoved}).Error)

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{name: "t", db: d, ctx: ctx, cfg: &config.DownloaderConfig{Transmission: &config.TransmissionConfig{FinishedDir: finishedDir}},
		inFlight: map[string]bool{}, uploadSem: make(chan struct{}, 1)}
	c.uploadSem <- struct{}{} // never released: the worker stays queued
	hash := "h"
	c.copyFinishedDownloads(map[string]*transmissionrpc.Torrent{"h": {
		HashString: &hash, DownloadDir: &downloadDir,
		Files: []transmissionrpc.TorrentFile{{Name: "f.txt"}},
	}})
	cancel()

	sctx, scancel := context.WithTimeout(context.Background(), time.Second)
	defer scancel()
	require.NoError(t, c.Shutdown(sctx))

	// state stays resumable
	got, err := db.GetDownloadStatus(d, "h")
	require.NoError(t, err)
	assert.Equal(t, db.Moving, got.MoveState)
	assert.NoFileExists(t, filepath.Join(finishedDir, "h", "f.txt"))
}

type fakeNotifier struct {
	mu   sync.Mutex
	msgs []string
}

func (f *fakeNotifier) SendMessage(m string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, m)
	return nil
}

func (f *fakeNotifier) SendMarkdownMessage(m string) error { return f.SendMessage(m) }

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.msgs)
}

func TestTransferBackoff(t *testing.T) {
	assert.Equal(t, time.Duration(0), transferBackoff(0))
	assert.Equal(t, time.Minute, transferBackoff(1))
	assert.Equal(t, 2*time.Minute, transferBackoff(2))
	assert.Equal(t, 16*time.Minute, transferBackoff(5))
	assert.Equal(t, time.Hour, transferBackoff(7))
	assert.Equal(t, time.Hour, transferBackoff(1000))
}

func newDispatchClient(t *testing.T, n notify.INotifier, clock *time.Time) *Client {
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "h", Downloader: "t", State: db.DownloadSeeding, MoveState: db.UnMoved}).Error)
	return &Client{name: "t", db: d, notifier: n,
		inFlight: map[string]bool{}, uploadSem: make(chan struct{}, 1),
		now: func() time.Time { return *clock }}
}

func TestDispatch_BackoffResetAndAlert(t *testing.T) {
	clock := time.Unix(1_000_000, 0)
	n := &fakeNotifier{}
	c := newDispatchClient(t, n, &clock)

	calls := 0
	var fail atomic.Bool
	fail.Store(true)
	work := func(*transmissionrpc.Torrent, *db.DownloadStatus) error {
		calls++
		if fail.Load() {
			return errors.New("boom")
		}
		return nil
	}
	run := func() {
		c.dispatch(&transmissionrpc.Torrent{}, db.DownloadStatus{ID: "h"}, work)
		c.wg.Wait()
	}

	run()
	assert.Equal(t, 1, calls)

	// within the 1m backoff: skipped
	clock = clock.Add(30 * time.Second)
	run()
	assert.Equal(t, 1, calls)

	// after backoff: retried, next delay doubles to 2m
	clock = clock.Add(31 * time.Second)
	run()
	assert.Equal(t, 2, calls)
	clock = clock.Add(90 * time.Second)
	run()
	assert.Equal(t, 2, calls)

	// failures 3..4: no alert yet
	for i := 0; i < 2; i++ {
		clock = clock.Add(time.Hour)
		run()
	}
	assert.Equal(t, 4, calls)
	assert.Equal(t, 0, n.count())

	// 5th failure alerts; 6th does not alert again
	clock = clock.Add(time.Hour)
	run()
	assert.Equal(t, 5, calls)
	assert.Equal(t, 1, n.count())
	clock = clock.Add(time.Hour)
	run()
	assert.Equal(t, 6, calls)
	assert.Equal(t, 1, n.count())

	// success clears state; a later failure starts again from 1m
	fail.Store(false)
	clock = clock.Add(time.Hour)
	run()
	assert.Equal(t, 7, calls)
	c.inFlightMu.Lock()
	assert.Empty(t, c.failures)
	c.inFlightMu.Unlock()

	fail.Store(true)
	run()
	assert.Equal(t, 8, calls)
	clock = clock.Add(61 * time.Second)
	run()
	assert.Equal(t, 9, calls)
}

func TestDispatch_NilNotifier(t *testing.T) {
	clock := time.Unix(1_000_000, 0)
	c := newDispatchClient(t, nil, &clock)
	for i := 0; i < transferAlertThreshold+1; i++ {
		c.dispatch(&transmissionrpc.Torrent{}, db.DownloadStatus{ID: "h"},
			func(*transmissionrpc.Torrent, *db.DownloadStatus) error { return errors.New("boom") })
		c.wg.Wait()
		clock = clock.Add(2 * time.Hour)
	}
}

func TestDeleteTorrent_ClearsFailureState(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "h", Downloader: "test", State: db.DownloadSeeding, MoveState: db.UnMoved}).Error)

	tor := newTorrent(1, "h", transmissionrpc.TorrentStatusSeed, 0)
	fake := &fakeTransmission{resp: []any{
		map[string]any{"torrents": []transmissionrpc.Torrent{tor}}, // torrent-get
		map[string]any{}, // torrent-remove
	}}
	c := newFakeClient(t, fake, d, &config.DownloaderConfig{Transmission: &config.TransmissionConfig{}})

	c.recordTransferResult("h", errors.New("boom"))
	c.recordTransferResult("other", errors.New("boom"))
	require.Len(t, c.failures, 2)

	require.NoError(t, c.DeleteTorrent("h"))

	c.inFlightMu.Lock()
	defer c.inFlightMu.Unlock()
	assert.NotContains(t, c.failures, "h")
	assert.Contains(t, c.failures, "other")
}
