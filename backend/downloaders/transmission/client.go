package transmission

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/hekmon/transmissionrpc/v3"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var (
	logger = log.With().Str("component", "transmission").Logger()

	httpClient = http.DefaultClient
)

type Client struct {
	client *transmissionrpc.Client
	name   string
	db     *gorm.DB
	cfg    *config.DownloaderConfig
}

func New(name string, cfg *config.DownloaderConfig, db *gorm.DB) (*Client, error) {
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

	return &Client{
		client: client,
		name:   name,
		db:     db,
		cfg:    cfg,
	}, nil
}

func (c *Client) RegisterCronjobs(cron *cron.Cron) {
	c.RegisterDailySeedingChecker(cron)

	go func() {
		time.Sleep(time.Minute)
		c.ProgressChecker()
	}()
}

func toTorrentsByHash(torrents []transmissionrpc.Torrent) map[string]*transmissionrpc.Torrent {
	torrentsByHash := make(map[string]*transmissionrpc.Torrent)
	for _, t := range torrents {
		torrentsByHash[*t.HashString] = &t
	}
	return torrentsByHash
}

func (c *Client) ProgressChecker() {
	torrents, err := c.client.TorrentGetAll(context.Background())
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get all torrents")
		return
	}

	torrentsByHash := toTorrentsByHash(torrents)

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

		s.DownloadProgress = int32(*t.PercentDone * 1000)
		if *t.Status == transmissionrpc.TorrentStatusSeed {
			s.State = db.DownloadSeeding
		}
		db.SaveDownloadStatus(c.db, &s)
	}

	// check if transmission is actively downloading.
	stats, err := c.client.SessionStats(context.Background())
	if err != nil {
		logger.Err(err).Str("name", c.name).Msg("failed to get session stats")
	}

	// if downloadSpeed > 2M/s, consider transimission is still busy
	if stats.DownloadSpeed > 2*1000*1000 {
		return
	}

	// start copys
	statuses, err = db.GetFinishedUnmoveedDownloadStatusByDownloader(c.db, c.name)
	if err != nil {
		logger.Error().Err(err).Str("name", c.name).Msg("failed to get seeding download status")
		return
	}

	for _, s := range statuses {
		t, ok := torrentsByHash[s.ID]
		if !ok {
			continue
		}

		success := true
		for _, f := range t.Files {
			from := filepath.Join(*t.DownloadDir, f.Name)
			target := filepath.Join(c.cfg.Transmission.FinishedDir, s.ID, f.Name)

			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				success = false
				logger.Error().Err(err).Str("name", c.name).Msg("failed to create parent directory for copied file")
				break
			}

			fromFile, err := os.Open(from)
			if err != nil {
				success = false
				logger.Error().Err(err).Str("name", c.name).Msg("failed to open file")
				break
			}
			defer fromFile.Close()

			targetFile, err := os.Create(target)
			if err != nil {
				success = false
				logger.Error().Err(err).Str("name", c.name).Msg("failed to create file")
				break
			}
			defer targetFile.Close()

			_, err = io.Copy(targetFile, fromFile)
			if err != nil {
				success = false
				logger.Error().Err(err).Str("name", c.name).Msg("failed to copy file")
				break
			}
		}

		if success {
			s.MoveState = db.Moved
			db.SaveDownloadStatus(c.db, &s)
		}
	}
}

func (c *Client) RegisterDailySeedingChecker(cron *cron.Cron) {
	if c.cfg.SeedingPolicy == nil {
		return
	}

	cron.AddFunc("0 0 8 * * *", func() {
		c.checkDailySeeding()
	})
}

func (c *Client) checkDailySeeding() {
	torrents, err := c.client.TorrentGetAll(context.Background())
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
		if *t.Status != transmissionrpc.TorrentStatusSeed {
			continue
		}

		hash := (*t.HashString)
		uploaded := *t.UploadedEver

		ss, err := db.GetDownloadStatus(c.db, hash)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			ss.ID = hash
			ss.Downloader = c.name
			ss.State = db.DownloadSeeding
			ss.UploadHistories = make(map[string]int64)
			ss.ResTitle = *t.Name
			ss.AddToday(uploaded)
			db.SaveDownloadStatus(c.db, ss)

			continue
		}
		ss.CleanupHistory()
		ss.AddToday(uploaded)

		db.SaveDownloadStatus(c.db, ss)

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
	if err := c.client.TorrentStopIDs(context.Background(), stopTorIDs); err != nil {
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

		deleteTorIDs = append(deleteTorIDs, *t.ID)
		deleteStatusIDs = append(deleteStatusIDs, s.ID)
	}

	// nothing to delete
	if len(deleteTorIDs) == 0 {
		return
	}

	// delete torrents
	if err := c.client.TorrentRemove(context.Background(), transmissionrpc.TorrentRemovePayload{IDs: deleteTorIDs, DeleteLocalData: true}); err != nil {
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

func (c *Client) DownloadDir() string {
	return c.cfg.Transmission.DownloadDir
}
