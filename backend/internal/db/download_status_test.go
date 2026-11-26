package db

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/organizer"
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

	// Planned items (Moved + Planned)
	plannedStatus1 := &DownloadStatus{
		ID:            "planned-1",
		Downloader:    testDownloader,
		MoveState:     Moved,
		OrganizeState: Planned,
	}
	plannedStatus2 := &DownloadStatus{
		ID:            "planned-2",
		Downloader:    testDownloader,
		MoveState:     Moved,
		OrganizeState: Planned,
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

	// Failed but deleted: must not be counted
	deletedFailedStatus := &DownloadStatus{
		ID:            "failed-deleted",
		Downloader:    testDownloader,
		State:         DownloadDeleted,
		MoveState:     Moved,
		OrganizeState: ExecutePlanFailed,
	}
	require.NoError(t, db.Create(deletedFailedStatus).Error)

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

	failedList, err := GetMovedAndOrganizeStatesDownloadStatusByDownloader(db, testDownloader, []OrganizeState{CreatePlanFailed, ExecutePlanFailed})
	require.NoError(t, err)
	assert.Len(t, failedList, int(counts.CountOfFailed))

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
		OrganizeState: Planned,
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

func TestDownloadStatus_DuplicateCreateTranslatesError(t *testing.T) {
	d, err := SqliteForTest()
	require.NoError(t, err)
	require.NoError(t, d.Create(&DownloadStatus{ID: "dup"}).Error)
	err = d.Create(&DownloadStatus{ID: "dup"}).Error
	assert.ErrorIs(t, err, gorm.ErrDuplicatedKey)
}

func TestGetDownloadStatuses_OrderAndInQuery(t *testing.T) {
	d, err := SqliteForTest()
	require.NoError(t, err)
	base := time.Now().Add(-time.Hour)
	rows := []*DownloadStatus{
		{ID: "a-old-create", Downloader: "dl", MoveState: Moved, OrganizeState: CreatePlanFailed, CreatedAt: base},
		{ID: "b-new-exec", Downloader: "dl", MoveState: Moved, OrganizeState: ExecutePlanFailed, CreatedAt: base.Add(30 * time.Minute)},
		{ID: "c-mid-create", Downloader: "dl", MoveState: Moved, OrganizeState: CreatePlanFailed, CreatedAt: base.Add(10 * time.Minute)},
		{ID: "d-planned", Downloader: "dl", MoveState: Moved, OrganizeState: Planned, CreatedAt: base.Add(40 * time.Minute)},
		{ID: "f-tie-b", Downloader: "dl", MoveState: Moved, OrganizeState: CreatePlanFailed, CreatedAt: base.Add(20 * time.Minute)},
		{ID: "f-tie-a", Downloader: "dl", MoveState: Moved, OrganizeState: ExecutePlanFailed, CreatedAt: base.Add(20 * time.Minute)},
		{ID: "e-other", Downloader: "other", MoveState: Moved, OrganizeState: CreatePlanFailed, CreatedAt: base.Add(50 * time.Minute)},
	}
	for _, r := range rows {
		require.NoError(t, d.Create(r).Error)
	}

	got, err := GetMovedAndOrganizeStatesDownloadStatusByDownloader(d, "dl", []OrganizeState{CreatePlanFailed, ExecutePlanFailed})
	require.NoError(t, err)
	ids := []string{}
	for _, g := range got {
		ids = append(ids, g.ID)
	}
	assert.Equal(t, []string{"b-new-exec", "f-tie-a", "f-tie-b", "c-mid-create", "a-old-create"}, ids)

	single, err := GetMovedAndOrganizeStateDownloadStatusByDownloader(d, "dl", Planned)
	require.NoError(t, err)
	require.Len(t, single, 1)
	assert.Equal(t, "d-planned", single[0].ID)
}

func TestGetStoppedMovedDownloadStatusByDownloader_ExcludesMoving(t *testing.T) {
	d, err := SqliteForTest()
	require.NoError(t, err)
	rows := []*DownloadStatus{
		{ID: "moved", Downloader: "dl", State: DownloadStopped, MoveState: Moved},
		{ID: "moving", Downloader: "dl", State: DownloadStopped, MoveState: Moving},
		{ID: "unmoved", Downloader: "dl", State: DownloadStopped, MoveState: UnMoved},
		{ID: "seeding", Downloader: "dl", State: DownloadSeeding, MoveState: Moved},
		{ID: "other", Downloader: "other", State: DownloadStopped, MoveState: Moved},
	}
	for _, r := range rows {
		require.NoError(t, d.Create(r).Error)
	}

	got, err := GetStoppedMovedDownloadStatusByDownloader(d, "dl")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "moved", got[0].ID)
}

func TestDownloadStatusJSONWire(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	s := DownloadStatus{
		ID: "h", CreatedAt: ts, UpdatedAt: ts, Downloader: "tr", DownloadProgress: 500,
		State: DownloadSeeding, Size: 7, ResIndexer: "idx", ResTitle: "t", ResTitle2: "t2",
		Category: "c", DetailsURL: "u", MoveState: Moved, OrganizeState: Planned,
	}
	b, err := json.Marshal(s)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"h","created_at":"2025-01-02T03:04:05Z","updated_at":"2025-01-02T03:04:05Z",
"downloader":"tr","download_progress":500,"state":1,"upload_histories":null,"size":7,"res_indexer":"idx",
"res_title":"t","res_title2":"t2","category":"c","details_url":"u","file_list":null,"metadata":null,
"move_state":1,"organize_state":1,"organize_plans":null}`, string(b))

	var got DownloadStatus
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, s, got)
}

func TestUpdateDownloadStatusColumns(t *testing.T) {
	gdb, err := SqliteForTest()
	require.NoError(t, err)

	orig := &DownloadStatus{
		ID: "1", State: DownloadSeeding, MoveState: Moving, OrganizeState: Planned,
		UploadHistories: map[string]int64{"2025-06-04": 1},
		FileList:        []string{"old"},
		OrganizePlans:   &organizer.PlanResponse{Plan: []organizer.PlanAction{{File: "old"}}},
	}
	require.NoError(t, gdb.Create(orig).Error)
	require.NoError(t, gdb.Model(&DownloadStatus{}).Where("id = ?", "1").Update("updated_at", time.Unix(1000, 0)).Error)

	t.Run("only selected columns, zero values and serializers", func(t *testing.T) {
		// Stale in-memory row: State differs from the DB's Seeding, and must not be written.
		stale := &DownloadStatus{ID: "1", State: DownloadStarted, MoveState: UnMoved, FileList: []string{"a", "b"},
			OrganizePlans: &organizer.PlanResponse{Plan: []organizer.PlanAction{{File: "new"}}}}
		ok, err := UpdateDownloadStatusColumns(gdb, stale, []string{"move_state", "file_list", "organize_plans"})
		require.NoError(t, err)
		assert.True(t, ok)

		got, err := GetDownloadStatus(gdb, "1")
		require.NoError(t, err)
		assert.Equal(t, DownloadSeeding, got.State)
		assert.Equal(t, UnMoved, got.MoveState) // zero value written
		assert.Equal(t, Planned, got.OrganizeState)
		assert.Equal(t, []string{"a", "b"}, got.FileList)
		require.NotNil(t, got.OrganizePlans)
		assert.Equal(t, "new", got.OrganizePlans.Plan[0].File)
		assert.Equal(t, map[string]int64{"2025-06-04": 1}, got.UploadHistories)
		assert.True(t, got.UpdatedAt.After(time.Unix(1000, 0)), "updated_at must be bumped")
	})

	t.Run("non-matching condition changes nothing", func(t *testing.T) {
		s := &DownloadStatus{ID: "1", OrganizeState: Organized}
		ok, err := UpdateDownloadStatusColumns(gdb, s, []string{"organize_state"}, "organize_state = ?", Unplanned)
		require.NoError(t, err)
		assert.False(t, ok)
		got, err := GetDownloadStatus(gdb, "1")
		require.NoError(t, err)
		assert.Equal(t, Planned, got.OrganizeState)
	})

	t.Run("matching condition updates", func(t *testing.T) {
		s := &DownloadStatus{ID: "1", OrganizeState: Organized}
		ok, err := UpdateDownloadStatusColumns(gdb, s, []string{"organize_state"}, "organize_state = ?", Planned)
		require.NoError(t, err)
		assert.True(t, ok)
		got, err := GetDownloadStatus(gdb, "1")
		require.NoError(t, err)
		assert.Equal(t, Organized, got.OrganizeState)
	})
}

func TestPurgeDeletedDownloadStatuses(t *testing.T) {
	d, err := SqliteForTest()
	require.NoError(t, err)
	for _, r := range []*DownloadStatus{
		{ID: "old-deleted", State: DownloadDeleted},
		{ID: "new-deleted", State: DownloadDeleted},
		{ID: "old-stopped", State: DownloadStopped},
	} {
		require.NoError(t, d.Create(r).Error)
	}
	old := time.Now().AddDate(0, 0, -40)
	require.NoError(t, d.Model(&DownloadStatus{}).Where("id IN ?", []string{"old-deleted", "old-stopped"}).UpdateColumn("updated_at", old).Error)

	n, err := PurgeDeletedDownloadStatuses(d, time.Now().AddDate(0, 0, -30))
	require.NoError(t, err)
	assert.Equal(t, int64(1), n)

	var ids []string
	require.NoError(t, d.Unscoped().Model(&DownloadStatus{}).Order("id").Pluck("id", &ids).Error)
	assert.Equal(t, []string{"new-deleted", "old-stopped"}, ids)
}
