package indexers

import (
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/errors"
)

// DownloadTracked fetches resID's details and downloads it, inserting the
// DownloadStatus row right before the torrent is placed (rolled back if placing fails).
func DownloadTracked(d *gorm.DB, idx IIndexer, resID string) *errors.HTTPStatusError {
	detail, err := idx.Detail(resID, true)
	if err != nil {
		return err
	}

	files := []string{}
	for _, file := range detail.Files {
		files = append(files, file.Name)
	}

	downloadStatus := &db.DownloadStatus{
		Downloader: idx.DownloaderName(),
		State:      db.DownloadStarted,
		ResTitle:   detail.Title,
		ResTitle2:  detail.Title2,
		ResIndexer: idx.Name(),
		Category:   detail.Category,
		DetailsURL: detail.DetailsURL,
		FileList:   files,
		Metadata:   detail.Metadata,
	}

	// The row is inserted right before the torrent file is placed, so a DB failure
	// never leaves an untracked torrent in the watch dir.
	created := false
	_, err = idx.Download(resID, func(hash string) error {
		downloadStatus.ID = hash
		if err := d.Create(downloadStatus).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		if created {
			// Placing the file failed after the row was inserted: roll back.
			if delErr := d.Delete(&db.DownloadStatus{}, "id = ?", downloadStatus.ID).Error; delErr != nil {
				log.Error().Err(delErr).Str("id", downloadStatus.ID).Msg("failed to roll back download status")
			}
		}
		return err
	}
	return nil
}
