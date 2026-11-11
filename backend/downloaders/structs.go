package downloaders

import (
	"context"
	"fmt"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/downloaders/transmission"
	"github.com/autoget-project/autoget/backend/internal/notify"
	"github.com/autoget-project/autoget/backend/organizer"
)

type IDownloader interface {
	RegisterCronjobs(ctx context.Context, cron *cron.Cron)
	Shutdown(ctx context.Context) error
	RegisterDailySeedingChecker(cron *cron.Cron)
	ProgressChecker()
	TorrentsDir() string
	DownloadDir() string
	DeleteTorrent(hash string) error
}

func New(name string, cfg *config.DownloaderConfig, db *gorm.DB, organizerClient *organizer.Client, notifier notify.INotifier) (IDownloader, error) {
	if cfg.Transmission == nil {
		return nil, fmt.Errorf("unknown downloader %s", name)
	}

	return transmission.New(name, cfg, db, organizerClient, notifier)
}
