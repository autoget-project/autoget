package db

import (
	"time"

	"github.com/autoget-project/autoget/backend/organizer"
	"gorm.io/gorm"
)

const (
	StoreMaxDays = 30
)

type DownloadState uint16

const (
	DownloadStarted DownloadState = iota
	DownloadSeeding
	DownloadStopped
	DownloadDeleted
)

type MoveState uint16

const (
	UnMoved MoveState = iota
	Moved
)

type OrganizeState uint16

const (
	Unplaned OrganizeState = iota
	Planed
	Organized
	CreatePlanFailed
	ExecutePlanFailed
)

type OrganizePlan struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type DownloadStatus struct {
	ID        string `gorm:"primarykey"` // hash
	CreatedAt time.Time
	UpdatedAt time.Time

	Downloader       string        `gorm:"index:idx_downloader_state;index:idx_downloader_movestate_organizestate"`
	DownloadProgress uint16        // in x/1000
	State            DownloadState `gorm:"index:idx_downloader_state;index:idx_downloader_state_movestate"`

	UploadHistories map[string]int64 `gorm:"serializer:json"`
	Size            uint64

	ResIndexer string
	ResTitle   string
	ResTitle2  string
	Category   string
	FileList   []string               `gorm:"serializer:json"`
	Metadata   map[string]interface{} `gorm:"serializer:json"`

	MoveState MoveState `gorm:"index:idx_downloader_state_movestate;index:idx_downloader_movestate_organizestate"`

	OrganizeState OrganizeState `gorm:"index:idx_downloader_movestate_organizestate"`

	OrganizePlans *organizer.PlanResponse `gorm:"serializer:json"`
}

func (s *DownloadStatus) AddToday(b int64) {
	t := time.Now().Format("2006-01-02")
	s.UploadHistories[t] = b
}

func (s *DownloadStatus) GetXDayBefore(x int) (int64, bool) {
	t := time.Now().AddDate(0, 0, -x).Format("2006-01-02")
	n, ok := s.UploadHistories[t]
	return n, ok
}

func (s *DownloadStatus) CleanupHistory() {
	now := time.Now()

	for k := range s.UploadHistories {
		t, _ := time.Parse("2006-01-02", k)
		if now.Sub(t).Hours() > StoreMaxDays*24 {
			delete(s.UploadHistories, k)
		}
	}
}

func GetDownloadStatusByID(db *gorm.DB, id string) (*DownloadStatus, error) {
	s := &DownloadStatus{}
	err := db.First(s, "id = ?", id).Error
	return s, err
}

func GetUnfinishedDownloadStatusByDownloader(db *gorm.DB, downloader string) ([]DownloadStatus, error) {
	var ss []DownloadStatus
	err := db.Where("downloader = ?", downloader).Where("state = ?", DownloadStarted).Find(&ss).Error
	return ss, err
}

func GetDownloadStatusByDownloaderAndState(db *gorm.DB, downloader string, state DownloadState) ([]DownloadStatus, error) {
	var ss []DownloadStatus
	err := db.Where("downloader = ?", downloader).Where("state = ?", state).Find(&ss).Error
	return ss, err
}

func GetFinishedUnmoveedDownloadStatusByDownloader(db *gorm.DB, downloader string) ([]DownloadStatus, error) {
	var ss []DownloadStatus
	err := db.Where("downloader = ?", downloader).Where("state >= ?", DownloadSeeding).Where("move_state = ?", UnMoved).Find(&ss).Error
	return ss, err
}

func GetStoppedMovedDownloadStatusByDownloader(db *gorm.DB, downloader string) ([]DownloadStatus, error) {
	var ss []DownloadStatus
	err := db.Where("downloader = ?", downloader).Where("state = ?", DownloadStopped).Where("move_state >= ?", Moved).Find(&ss).Error
	return ss, err
}

func GetMovedAndOrganizeStateDownloadStatusByDownloader(db *gorm.DB, downloader string, organizeState OrganizeState) ([]DownloadStatus, error) {
	var ss []DownloadStatus
	err := db.Where("downloader = ?", downloader).Where("state != ?", DownloadDeleted).Where("move_state = ?", Moved).Where("organize_state = ?", organizeState).Find(&ss).Error
	return ss, err
}

func GetDownloadStatus(db *gorm.DB, hash string) (*DownloadStatus, error) {
	s := &DownloadStatus{}
	err := db.First(s, "id = ?", hash).Error
	return s, err
}

func SaveDownloadStatus(db *gorm.DB, s *DownloadStatus) error {
	return db.Save(s).Error
}

func RemoveDownloadStatus(db *gorm.DB, id string) error {
	return db.Where("id = ?", id).Delete(&DownloadStatus{}).Error
}

func UpdateDownloadStateForStatuses(db *gorm.DB, ids []string, state DownloadState) error {
	return db.Model(&DownloadStatus{}).Where("id IN ?", ids).Update("state", state).Error
}

type DownloaderStateCounts struct {
	CountOfDownloading int64 `json:"count_of_downloading"`
	CountOfPlanned     int64 `json:"count_of_planned"`
	CountOfFailed      int64 `json:"count_of_failed"`
}

func GetDownloaderStateCounts(db *gorm.DB, downloader string) (*DownloaderStateCounts, error) {
	counts := &DownloaderStateCounts{}

	// Count downloading (DownloadStarted AND not moved to organized states yet)
	err := db.Model(&DownloadStatus{}).Where("downloader = ?", downloader).Where("state = ?", DownloadStarted).Where("move_state != ?", Moved).Count(&counts.CountOfDownloading).Error
	if err != nil {
		return nil, err
	}

	// Count planned (moved and organized state = Planed)
	err = db.Model(&DownloadStatus{}).Where("downloader = ?", downloader).Where("move_state = ?", Moved).Where("organize_state = ?", Planed).Count(&counts.CountOfPlanned).Error
	if err != nil {
		return nil, err
	}

	// Count failed (both CreatePlanFailed and ExecutePlanFailed)
	var createFailedCount, executeFailedCount int64

	err = db.Model(&DownloadStatus{}).Where("downloader = ?", downloader).Where("move_state = ?", Moved).Where("organize_state = ?", CreatePlanFailed).Count(&createFailedCount).Error
	if err != nil {
		return nil, err
	}

	err = db.Model(&DownloadStatus{}).Where("downloader = ?", downloader).Where("move_state = ?", Moved).Where("organize_state = ?", ExecutePlanFailed).Count(&executeFailedCount).Error
	if err != nil {
		return nil, err
	}

	counts.CountOfFailed = createFailedCount + executeFailedCount

	return counts, nil
}

func GetAllDownloadersStateCounts(db *gorm.DB) ([]map[string]interface{}, error) {
	var downloaders []string

	// Get all unique downloaders
	err := db.Model(&DownloadStatus{}).Distinct("downloader").Pluck("downloader", &downloaders).Error
	if err != nil {
		return nil, err
	}

	var result []map[string]interface{}

	for _, downloader := range downloaders {
		counts, err := GetDownloaderStateCounts(db, downloader)
		if err != nil {
			return nil, err
		}

		item := map[string]interface{}{
			"name":                 downloader,
			"count_of_downloading": counts.CountOfDownloading,
			"count_of_planned":     counts.CountOfPlanned,
			"count_of_failed":      counts.CountOfFailed,
		}
		result = append(result, item)
	}

	return result, nil
}

// GetAllDownloadersStateCountsWithNames takes a list of downloader names and returns state counts for all of them
func GetAllDownloadersStateCountsWithNames(db *gorm.DB, downloaderNames []string) ([]map[string]interface{}, error) {
	var result []map[string]interface{}

	for _, downloader := range downloaderNames {
		counts, err := GetDownloaderStateCounts(db, downloader)
		if err != nil {
			return nil, err
		}

		item := map[string]interface{}{
			"name":                 downloader,
			"count_of_downloading": counts.CountOfDownloading,
			"count_of_planned":     counts.CountOfPlanned,
			"count_of_failed":      counts.CountOfFailed,
		}
		result = append(result, item)
	}

	return result, nil
}
