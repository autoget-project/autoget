package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSeedingStatus(t *testing.T) {
	db, err := SqliteForTest()
	require.NoError(t, err)

	want := &DownloadStatus{
		ID: "1",
		UploadHistories: map[string]int64{
			"2025-06-04": 100000,
			"2025-06-05": 100001,
		},
	}

	db.Create(want)

	got := &DownloadStatus{}
	db.First(got, "id = ?", want.ID)

	want.CreatedAt = got.CreatedAt
	want.UpdatedAt = got.UpdatedAt

	assert.Equal(t, want, got)
}

func TestSeedingStatus_AddToday(t *testing.T) {
	s := &DownloadStatus{
		UploadHistories: make(map[string]int64),
	}
	today := time.Now().Format("2006-01-02")
	amount := int64(12345)

	s.AddToday(amount)

	assert.Contains(t, s.UploadHistories, today)
	assert.Equal(t, amount, s.UploadHistories[today])

	// Test adding again for the same day
	s.AddToday(amount + 100)
	assert.Equal(t, amount+100, s.UploadHistories[today])
}

func TestSeedingStatus_GetXDayBefore(t *testing.T) {
	s := &DownloadStatus{
		UploadHistories: make(map[string]int64),
	}

	// Add some historical data
	dayMinus1 := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	dayMinus5 := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	s.UploadHistories[dayMinus1] = 100
	s.UploadHistories[dayMinus5] = 500

	// Test existing history
	n, ok := s.GetXDayBefore(1)
	assert.True(t, ok)
	assert.Equal(t, int64(100), n)

	n, ok = s.GetXDayBefore(5)
	assert.True(t, ok)
	assert.Equal(t, int64(500), n)

	// Test non-existing history
	n, ok = s.GetXDayBefore(2)
	assert.False(t, ok)
	assert.Equal(t, int64(0), n)
}

func TestSeedingStatus_CleanupHistory(t *testing.T) {
	s := &DownloadStatus{
		UploadHistories: make(map[string]int64),
	}

	now := time.Now()

	// Add entries older than storeMaxDays
	oldDate1 := now.AddDate(0, 0, -(StoreMaxDays + 1)).Format("2006-01-02")
	oldDate2 := now.AddDate(0, 0, -(StoreMaxDays + 5)).Format("2006-01-02")
	s.UploadHistories[oldDate1] = 100
	s.UploadHistories[oldDate2] = 200

	// Add entries within storeMaxDays
	recentDate1 := now.AddDate(0, 0, -5).Format("2006-01-02")
	recentDate2 := now.AddDate(0, 0, -(StoreMaxDays - 2)).Format("2006-01-02")
	s.UploadHistories[recentDate1] = 300
	s.UploadHistories[recentDate2] = 400

	s.CleanupHistory()

	// Verify old entries are removed
	assert.NotContains(t, s.UploadHistories, oldDate1)
	assert.NotContains(t, s.UploadHistories, oldDate2)

	// Verify recent entries are kept
	assert.Contains(t, s.UploadHistories, recentDate1)
	assert.Equal(t, int64(300), s.UploadHistories[recentDate1])
	assert.Contains(t, s.UploadHistories, recentDate2)
	assert.Equal(t, int64(400), s.UploadHistories[recentDate2])
}

func TestGetDownloaderStateCounts(t *testing.T) {
	db, err := SqliteForTest()
	require.NoError(t, err)

	// Create test data for different states
	testDownloader := "test-downloader"

	// Downloading items (DownloadStarted)
	downloadingStatus1 := &DownloadStatus{
		ID:         "downloading-1",
		Downloader: testDownloader,
		State:      DownloadStarted,
	}
	downloadingStatus2 := &DownloadStatus{
		ID:         "downloading-2",
		Downloader: testDownloader,
		State:      DownloadStarted,
	}

	// Planned items (Moved + Planed)
	plannedStatus1 := &DownloadStatus{
		ID:            "planned-1",
		Downloader:    testDownloader,
		MoveState:     Moved,
		OrganizeState: Planed,
	}
	plannedStatus2 := &DownloadStatus{
		ID:            "planned-2",
		Downloader:    testDownloader,
		MoveState:     Moved,
		OrganizeState: Planed,
	}

	// Failed items (Moved + CreatePlanFailed)
	failedStatus1 := &DownloadStatus{
		ID:            "failed-1",
		Downloader:    testDownloader,
		MoveState:     Moved,
		OrganizeState: CreatePlanFailed,
	}

	// Failed items (Moved + ExecutePlanFailed)
	failedStatus2 := &DownloadStatus{
		ID:            "failed-2",
		Downloader:    testDownloader,
		MoveState:     Moved,
		OrganizeState: ExecutePlanFailed,
	}

	// Items that shouldn't be counted (different downloader)
	otherDownloaderStatus := &DownloadStatus{
		ID:         "other-1",
		Downloader: "other-downloader",
		State:      DownloadStarted,
	}

	// Create all test records
	err = db.Create(downloadingStatus1).Error
	require.NoError(t, err)
	err = db.Create(downloadingStatus2).Error
	require.NoError(t, err)
	err = db.Create(plannedStatus1).Error
	require.NoError(t, err)
	err = db.Create(plannedStatus2).Error
	require.NoError(t, err)
	err = db.Create(failedStatus1).Error
	require.NoError(t, err)
	err = db.Create(failedStatus2).Error
	require.NoError(t, err)
	err = db.Create(otherDownloaderStatus).Error
	require.NoError(t, err)

	// Test the function
	counts, err := GetDownloaderStateCounts(db, testDownloader)
	require.NoError(t, err)

	// Verify counts
	assert.Equal(t, int64(2), counts.CountOfDownloading)
	assert.Equal(t, int64(2), counts.CountOfPlanned)
	assert.Equal(t, int64(2), counts.CountOfFailed)

	// Test with non-existent downloader
	emptyCounts, err := GetDownloaderStateCounts(db, "non-existent")
	require.NoError(t, err)
	assert.Equal(t, int64(0), emptyCounts.CountOfDownloading)
	assert.Equal(t, int64(0), emptyCounts.CountOfPlanned)
	assert.Equal(t, int64(0), emptyCounts.CountOfFailed)
}

func TestGetAllDownloadersStateCounts(t *testing.T) {
	db, err := SqliteForTest()
	require.NoError(t, err)

	// Create test data for multiple downloaders
	downloader1 := "downloader-1"
	downloader2 := "downloader-2"

	// Items for downloader-1
	status1_1 := &DownloadStatus{
		ID:         "1-1",
		Downloader: downloader1,
		State:      DownloadStarted,
	}
	status1_2 := &DownloadStatus{
		ID:            "1-2",
		Downloader:    downloader1,
		MoveState:     Moved,
		OrganizeState: Planed,
	}

	// Items for downloader-2
	status2_1 := &DownloadStatus{
		ID:         "2-1",
		Downloader: downloader2,
		State:      DownloadStarted,
	}
	status2_2 := &DownloadStatus{
		ID:            "2-2",
		Downloader:    downloader2,
		MoveState:     Moved,
		OrganizeState: CreatePlanFailed,
	}

	// Create all test records
	err = db.Create(status1_1).Error
	require.NoError(t, err)
	err = db.Create(status1_2).Error
	require.NoError(t, err)
	err = db.Create(status2_1).Error
	require.NoError(t, err)
	err = db.Create(status2_2).Error
	require.NoError(t, err)

	// Test the function
	result, err := GetAllDownloadersStateCounts(db)
	require.NoError(t, err)

	// Should have entries for both downloaders
	assert.Len(t, result, 2)

	// Find downloader-1 entry
	var downloader1Result map[string]interface{}
	for _, item := range result {
		if item["name"] == downloader1 {
			downloader1Result = item
			break
		}
	}
	require.NotNil(t, downloader1Result)
	assert.Equal(t, downloader1, downloader1Result["name"])
	assert.Equal(t, int64(1), downloader1Result["count_of_downloading"])
	assert.Equal(t, int64(1), downloader1Result["count_of_planned"])
	assert.Equal(t, int64(0), downloader1Result["count_of_failed"])

	// Find downloader-2 entry
	var downloader2Result map[string]interface{}
	for _, item := range result {
		if item["name"] == downloader2 {
			downloader2Result = item
			break
		}
	}
	require.NotNil(t, downloader2Result)
	assert.Equal(t, downloader2, downloader2Result["name"])
	assert.Equal(t, int64(1), downloader2Result["count_of_downloading"])
	assert.Equal(t, int64(0), downloader2Result["count_of_planned"])
	assert.Equal(t, int64(1), downloader2Result["count_of_failed"])

	// Test with empty database
	emptyDB, err := SqliteForTest()
	require.NoError(t, err)
	emptyResult, err := GetAllDownloadersStateCounts(emptyDB)
	require.NoError(t, err)
	assert.Len(t, emptyResult, 0)
}
