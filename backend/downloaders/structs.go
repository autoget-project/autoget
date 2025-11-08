package downloaders

import (
	"fmt"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/downloaders/transmission"
	"github.com/autoget-project/autoget/backend/organizer"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type IDownloader interface {
	RegisterCronjobs(cron *cron.Cron)
	RegisterDailySeedingChecker(cron *cron.Cron)
	ProgressChecker()
	TorrentsDir() string
	DownloadDir() string
	DeleteTorrent(hash string) error
}

func New(name string, cfg *config.DownloaderConfig, db *gorm.DB, organizerClient *organizer.Client) (IDownloader, error) {
	if cfg.Transmission == nil {
		return nil, fmt.Errorf("Unknown downloader %s", name)
	}

	return transmission.New(name, cfg, db, organizerClient)
}
