package transmission

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/hekmon/transmissionrpc/v3"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/protocol"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/organizer"
)

var (
	logger = log.With().Str("component", "transmission").Logger()

	httpClient = &http.Client{Timeout: 30 * time.Second}
)

// progressFields lists every torrent field read on the ProgressChecker path
// (toTorrentsByHash, updateDownloadProgress, copyFinishedDownloads,
// copyTorrentFiles, uploadTorrentFiles). Keep in sync when reading more fields.
var progressFields = []string{"hashString", "percentDone", "status", "totalSize", "files", "downloadDir"}

type Client struct {
	client          *transmissionrpc.Client
	name            string
	db              *gorm.DB
	organizerClient *organizer.Client
	cfg             *config.DownloaderConfig
	uploader        *organizer.Uploader
	inFlightMu      sync.Mutex
	inFlight        map[string]bool
	uploadSem       chan struct{}

	// ctx is cancelled at shutdown; set by RegisterCronjobs before any
	// background goroutine starts. nil means c.context().
	ctx context.Context
	wg  sync.WaitGroup
}

// progressInterval is how often the progress checker runs.
var progressInterval = time.Minute

func (c *Client) context() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

// safely runs f and logs, rather than propagates, any panic so a bad response
// cannot crash the whole process from a background goroutine.
func (c *Client) safely(name string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error().Str("name", c.name).Str("task", name).Interface("panic", r).
				Bytes("stack", debug.Stack()).Msg("recovered from panic")
		}
	}()
	f()
}

// Shutdown waits for the progress loop and in-flight uploads to finish, or for
// ctx to expire. Cancel the context passed to RegisterCronjobs first.
func (c *Client) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func New(name string, cfg *config.DownloaderConfig, db *gorm.DB, organizerClient *organizer.Client) (*Client, error) {
	u, err := url.Parse(cfg.Transmission.URL)
	if err != nil {
		return nil, err
	}

	if cfg.Transmission.Username != "" && cfg.Transmission.Password != "" {
		u.User = url.UserPassword(cfg.Transmission.Username, cfg.Transmission.Password)
	}

	client, err := transmissionrpc.New(u, &transmissionrpc.Config{
		CustomClient: httpClient,
	})
	if err != nil {
		return nil, err
	}

	var uploader *organizer.Uploader
	concurrency := 2
	if cfg.Transfer != nil {
		if err := cfg.Transfer.Validate(); err != nil {
			return nil, err
		}
		concurrency = cfg.Transfer.Concurrency
		chunkSize := cfg.Transfer.ChunkSizeMB * 1024 * 1024
		uploader = organizer.NewUploader(organizerClient,
			organizer.WithChunkSize(chunkSize),
			organizer.WithMaxRetries(cfg.Transfer.MaxRetries),
		)
	} else {
		uploader = organizer.NewUploader(organizerClient)
	}

	return &Client{
		client:          client,
		name:            name,
		db:              db,
		organizerClient: organizerClient,
		cfg:             cfg,
		uploader:        uploader,
		inFlight:        make(map[string]bool),
		uploadSem:       make(chan struct{}, concurrency),
	}, nil
}

func (c *Client) RegisterCronjobs(ctx context.Context, cron *cron.Cron) {
	c.ctx = ctx
	c.RegisterDailySeedingChecker(cron)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(progressInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				c.safely("progress checker", c.ProgressChecker)
			}
		}
	}()
}

func toTorrentsByHash(torrents []transmissionrpc.Torrent) map[string]*transmissionrpc.Torrent {
	torrentsByHash := make(map[string]*transmissionrpc.Torrent)
	for _, t := range torrents {
		if t.HashString == nil {
			logger.Warn().Msg("skipping torrent without hash")
			continue
		}
		torrentsByHash[*t.HashString] = &t
	}
	return torrentsByHash
}

func (c *Client) ProgressChecker() {
	torrents, err := c.client.TorrentGet(c.context(), progressFields, nil)
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get torrents")
		return
	}

	torrentsByHash := toTorrentsByHash(torrents)

	c.updateDownloadProgress(torrentsByHash)

	// check if transmission is actively downloading.
	stats, err := c.client.SessionStats(c.context())
	if err != nil {
		logger.Err(err).Str("name", c.name).Msg("failed to get session stats")
		return
	}

	// if downloadSpeed > 2M/s, consider transimission is still busy
	if stats.DownloadSpeed > 2*1000*1000 {
		return
	}

	// start copys
	c.copyFinishedDownloads(torrentsByHash)

	// create organizer plan
	c.createOrganizerPlan()
}

func (c *Client) updateDownloadProgress(torrentsByHash map[string]*transmissionrpc.Torrent) {
	statuses, err := db.GetUnfinishedDownloadStatusByDownloader(c.db, c.name)
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get download status")
		return
	}

	for _, s := range statuses {
		t, ok := torrentsByHash[s.ID]
		if !ok {
			continue
		}

		if t.PercentDone == nil || t.Status == nil || t.TotalSize == nil {
			logger.Warn().Str("name", c.name).Str("id", s.ID).Msg("torrent is missing fields; skipping progress update")
			continue
		}

		s.DownloadProgress = uint16(*t.PercentDone * 1000)
		s.Size = uint64(t.TotalSize.Byte())
		if *t.Status == transmissionrpc.TorrentStatusSeed {
			s.State = db.DownloadSeeding
		}
		if err := db.SaveDownloadStatus(c.db, &s); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("failed to save download status")
		}
	}
}

func (c *Client) copyFinishedDownloads(torrentsByHash map[string]*transmissionrpc.Torrent) {
	statuses, err := db.GetFinishedUnmovedDownloadStatusByDownloader(c.db, c.name)
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get seeding download status")
		return
	}

	mode := "local"
	if c.cfg.Transfer != nil && c.cfg.Transfer.Mode != "" {
		mode = c.cfg.Transfer.Mode
	}

	for _, s := range statuses {
		t, ok := torrentsByHash[s.ID]
		if !ok {
			continue
		}

		if t.DownloadDir == nil || t.Files == nil {
			logger.Warn().Str("name", c.name).Str("id", s.ID).Msg("torrent is missing files or download dir; skipping copy")
			continue
		}

		if err := validateTorrentPaths(t); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("torrent has unsafe file path; skipping copy")
			continue
		}

		if mode == "http" {
			c.dispatch(t, s, c.uploadTorrentFiles)
		} else {
			c.dispatch(t, s, c.copyTorrentFiles)
		}
	}
}

// dispatch runs work for one torrent on a background worker, bounded by
// uploadSem and deduplicated by torrent ID via inFlight. The status is marked
// Moving first so an interrupted transfer is picked up again after a restart.
func (c *Client) dispatch(t *transmissionrpc.Torrent, s db.DownloadStatus, work func(*transmissionrpc.Torrent, *db.DownloadStatus)) {
	c.inFlightMu.Lock()
	if c.inFlight[s.ID] {
		c.inFlightMu.Unlock()
		return
	}
	c.inFlight[s.ID] = true
	c.inFlightMu.Unlock()

	// Ensure MoveState is set to Moving in database
	if s.MoveState != db.Moving {
		s.MoveState = db.Moving
		if err := db.SaveDownloadStatus(c.db, &s); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("failed to update status to Moving")
		}
	}

	c.wg.Add(1)
	go func(torrent *transmissionrpc.Torrent, status db.DownloadStatus) {
		defer c.wg.Done()
		defer func() {
			c.inFlightMu.Lock()
			delete(c.inFlight, status.ID)
			c.inFlightMu.Unlock()
		}()

		select {
		case c.uploadSem <- struct{}{}:
			defer func() { <-c.uploadSem }()
		case <-c.context().Done():
			return
		}

		c.safely("transfer", func() { work(torrent, &status) })
	}(t, s)
}

// safeRel rejects torrent file names that would escape their base directory.
func safeRel(name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("unsafe file path %q", name)
	}
	return nil
}

func validateTorrentPaths(t *transmissionrpc.Torrent) error {
	for _, f := range t.Files {
		if err := safeRel(f.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) uploadTorrentFiles(t *transmissionrpc.Torrent, s *db.DownloadStatus) {
	files := make([]string, 0, len(t.Files))
	ctx := c.context()

	if err := validateTorrentPaths(t); err != nil {
		logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("rejecting torrent upload")
		return
	}

	for _, f := range t.Files {
		srcPath := filepath.Join(*t.DownloadDir, f.Name)
		fi, err := os.Stat(srcPath)
		if err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("file", f.Name).Msg("failed to stat source file for upload; parking")
			return
		}

		initReq := &protocol.UploadInitRequest{
			TorrentID:    s.ID,
			RelativePath: f.Name,
			TotalSize:    fi.Size(),
			ModifyTime:   fi.ModTime().Unix(),
		}

		if err := c.uploader.UploadFile(ctx, srcPath, initReq, nil); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("file", f.Name).Msg("failed to upload torrent file; parking for next tick")
			return
		}

		files = append(files, f.Name)
	}

	// All files uploaded and finalized successfully
	s.FileList = files
	s.MoveState = db.Moved
	if err := db.SaveDownloadStatus(c.db, s); err != nil {
		logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("failed to save download status after upload")
		return
	}

	logger.Info().Str("name", c.name).Str("id", s.ID).Int("files", len(files)).Msg("successfully uploaded torrent files via HTTP")
}

// copyTorrentFiles copies every file of t into FinishedDir/<id>/ and, once all
// succeeded, marks the status Moved. On failure the status stays Moving and is
// retried on a later tick.
func (c *Client) copyTorrentFiles(t *transmissionrpc.Torrent, s *db.DownloadStatus) {
	if err := validateTorrentPaths(t); err != nil {
		logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("rejecting torrent copy")
		return
	}

	ctx := c.context()
	files := []string{}
	for _, f := range t.Files {
		from := filepath.Join(*t.DownloadDir, f.Name)
		target := filepath.Join(c.cfg.Transmission.FinishedDir, s.ID, f.Name)

		if err := copyFileAtomic(ctx, from, target); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Str("file", f.Name).Msg("failed to copy torrent file; will retry")
			return
		}

		files = append(files, f.Name)
	}

	// add files based on path from transmission.
	s.FileList = files
	s.MoveState = db.Moved
	if err := db.SaveDownloadStatus(c.db, s); err != nil {
		logger.Error().Err(err).Str("name", c.name).Str("id", s.ID).Msg("failed to save download status after copy")
	}
}

// ctxReader aborts reads once ctx is cancelled so shutdown is not held up by
// a large copy.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// copyFileAtomic copies from to target via target+".part", fsyncs, then renames,
// so target is either absent or complete. The .part file is removed on failure.
func copyFileAtomic(ctx context.Context, from, target string) (err error) {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	src, err := os.Open(from)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = src.Close() }()

	part := target + ".part"
	dst, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = dst.Close()
			_ = os.Remove(part)
		}
	}()

	if _, err = io.Copy(dst, ctxReader{ctx: ctx, r: src}); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	if err = dst.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}
	if err = dst.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}
	if err = os.Rename(part, target); err != nil {
		return fmt.Errorf("failed to rename file: %w", err)
	}
	return nil
}

func (c *Client) createOrganizerPlan() {
	statuses, err := db.GetMovedAndOrganizeStateDownloadStatusByDownloader(c.db, c.name, db.Unplanned)
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get moved & unplanned download status")
		return
	}

	for _, st := range statuses {
		resp, err := c.organizerClient.Plan(&organizer.PlanRequest{
			Dir:      st.ID,
			Files:    st.FileList,
			Metadata: st.Metadata,
		})
		if err != nil {
			logger.Error().Err(err).Str("name", c.name).Msg("failed to create organizer plan")
			st.OrganizeState = db.CreatePlanFailed
			if err := db.SaveDownloadStatus(c.db, &st); err != nil {
				logger.Error().Err(err).Str("name", c.name).Str("id", st.ID).Msg("failed to save download status")
			}
			continue
		}
		st.OrganizePlans = resp
		st.OrganizeState = db.Planned
		if err := db.SaveDownloadStatus(c.db, &st); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("id", st.ID).Msg("failed to save download status")
		}
	}
}

func (c *Client) RegisterDailySeedingChecker(cron *cron.Cron) {
	if c.cfg.SeedingPolicy == nil {
		logger.Info().Str("name", c.name).Msg("seeding policy is not configured")
		return
	}

	jobID, err := cron.AddFunc("0 8 * * *", func() {
		c.safely("daily seeding", c.checkDailySeeding)
	})
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to add cron job")
		return
	}
	logger.Info().Str("name", c.name).Int64("jobID", int64(jobID)).Msg("added cron job")
}

func (c *Client) checkDailySeeding() {
	logger.Info().Str("name", c.name).Msg("checking daily seeding")

	torrents, err := c.client.TorrentGetAll(c.context())
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get all torrents")
		return
	}

	torrentsByHash := toTorrentsByHash(torrents)

	c.stopTorrents(torrents)
	c.removeTorrents(torrentsByHash)
}

func (c *Client) stopTorrents(torrents []transmissionrpc.Torrent) {
	stopIDs := []string{}
	stopTorIDs := []int64{}

	for _, t := range torrents {
		// only check seeding torrents
		if t.Status == nil || *t.Status != transmissionrpc.TorrentStatusSeed {
			continue
		}
		if t.HashString == nil || t.UploadedEver == nil || t.ID == nil {
			logger.Warn().Str("name", c.name).Msg("seeding torrent is missing fields; skipping")
			continue
		}

		hash := (*t.HashString)
		uploaded := *t.UploadedEver

		ss, err := db.GetDownloadStatus(c.db, hash)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			logger.Error().Err(err).Str("name", c.name).Str("hash", hash).Msg("failed to get download status")
			continue
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ss.ID = hash
			ss.Downloader = c.name
			ss.State = db.DownloadSeeding
			ss.UploadHistories = make(map[string]int64)
			if t.Name != nil {
				ss.ResTitle = *t.Name
			}
			ss.AddToday(uploaded)
			if err := db.SaveDownloadStatus(c.db, ss); err != nil {
				logger.Error().Err(err).Str("name", c.name).Str("id", ss.ID).Msg("failed to save download status")
			}

			continue
		}
		if ss.UploadHistories == nil {
			ss.UploadHistories = make(map[string]int64)
		}
		ss.CleanupHistory()
		ss.AddToday(uploaded)

		if err := db.SaveDownloadStatus(c.db, ss); err != nil {
			logger.Error().Err(err).Str("name", c.name).Str("id", ss.ID).Msg("failed to save download status")
		}

		before, ok := ss.GetXDayBefore(int(c.cfg.SeedingPolicy.IntervalInDays))
		if !ok {
			continue
		}

		if (uploaded - before) > c.cfg.SeedingPolicy.UploadAtLeastInMB*1024*1024 {
			continue
		}

		// stop this torrent
		stopTorIDs = append(stopTorIDs, *t.ID)
		stopIDs = append(stopIDs, hash)
	}

	// nothing to stop
	if len(stopTorIDs) == 0 {
		return
	}

	// stop torrents
	if err := c.client.TorrentStopIDs(c.context(), stopTorIDs); err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to stop torrents")
		return
	}

	// update state in db
	if err := db.UpdateDownloadStateForStatuses(c.db, stopIDs, db.DownloadStopped); err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to update download status")
		return
	}
}

func (c *Client) removeTorrents(torrentsByHash map[string]*transmissionrpc.Torrent) {
	statuses, err := db.GetStoppedMovedDownloadStatusByDownloader(c.db, c.name)
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get stopped download status")
		return
	}

	deleteStatusIDs := []string{}
	deleteTorIDs := []int64{}
	for _, s := range statuses {
		t, ok := torrentsByHash[s.ID]
		if !ok {
			continue
		}

		if t.ID == nil {
			continue
		}
		deleteTorIDs = append(deleteTorIDs, *t.ID)
		deleteStatusIDs = append(deleteStatusIDs, s.ID)
	}

	// nothing to delete
	if len(deleteTorIDs) == 0 {
		return
	}

	// delete torrents
	if err := c.client.TorrentRemove(c.context(), transmissionrpc.TorrentRemovePayload{IDs: deleteTorIDs, DeleteLocalData: true}); err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to delete torrents")
		return
	}

	if err := db.UpdateDownloadStateForStatuses(c.db, deleteStatusIDs, db.DownloadDeleted); err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to update download status")
	}
}

func (c *Client) TorrentsDir() string {
	return c.cfg.Transmission.TorrentsDir
}

func (c *Client) DeleteTorrent(hash string) error {
	torrents, err := c.client.TorrentGetAll(c.context())
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get all torrents")
		return err
	}

	// Find the torrent by hash
	var torrentID int64
	for _, t := range torrents {
		if t.HashString != nil && t.ID != nil && *t.HashString == hash {
			torrentID = *t.ID
			break
		}
	}

	if torrentID == 0 {
		return errors.New("torrent not found")
	}

	// If torrent is Moving, attempt to cancel upload sessions with organizer
	if st, err := db.GetDownloadStatus(c.db, hash); err == nil && st != nil && st.MoveState == db.Moving {
		for _, t := range torrents {
			if t.HashString != nil && *t.HashString == hash {
				for _, f := range t.Files {
					upID := protocol.DeriveUploadID(hash, f.Name)
					_ = c.organizerClient.CancelUpload(c.context(), upID)
				}
				break
			}
		}
	}

	// Delete the torrent from Transmission
	if err := c.client.TorrentRemove(c.context(), transmissionrpc.TorrentRemovePayload{
		IDs:             []int64{torrentID},
		DeleteLocalData: true,
	}); err != nil {
		logger.Error().Err(err).Str("name", c.name).Str("hash", hash).Msg("failed to delete torrent")
		return err
	}

	// Update the download status in database
	if err := db.UpdateDownloadStateForStatuses(c.db, []string{hash}, db.DownloadDeleted); err != nil {
		logger.Error().Err(err).Str("name", c.name).Str("hash", hash).Msg("failed to update download status")
		return err
	}

	logger.Info().Str("name", c.name).Str("hash", hash).Msg("successfully deleted torrent")
	return nil
}

func (c *Client) DownloadDir() string {
	return c.cfg.Transmission.DownloadDir
}
