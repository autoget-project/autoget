package transmission

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autoget-project/autoget/backend/downloaders/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/organizer"
	"github.com/hekmon/transmissionrpc/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestPayload struct {
	Method    string      `json:"method"`
	Arguments interface{} `json:"arguments,omitempty"`
	Tag       int         `json:"tag,omitempty"`
}

type torrentGetResults struct {
	Torrents []transmissionrpc.Torrent `json:"torrents"`
}

type answerPayload struct {
	Arguments interface{} `json:"arguments"`
	Result    string      `json:"result"`
	Tag       int         `json:"tag"`
}

// fake transmission rpc server
type fakeTransmission struct {
	reqs []*requestPayload
	resp []any
}

func (f *fakeTransmission) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	req := &requestPayload{}
	json.NewDecoder(r.Body).Decode(req)
	f.reqs = append(f.reqs, req)

	w.Header().Set("Content-Type", "application/json")

	body := f.resp[0]
	f.resp = f.resp[1:]

	resp := &answerPayload{
		Arguments: body,
		Result:    "success",
		Tag:       req.Tag,
	}

	json.NewEncoder(w).Encode(resp)
}

func newTorrent(id int64, hash string, status transmissionrpc.TorrentStatus, uploaded int64) transmissionrpc.Torrent {
	name := fmt.Sprintf("Torrent %d", id)
	return transmissionrpc.Torrent{
		ID:           &id,
		HashString:   &hash,
		Status:       &status,
		UploadedEver: &uploaded,
		Name:         &name,
	}
}

func TestCheckDailySeeding(t *testing.T) {
	fake := &fakeTransmission{}

	serv := httptest.NewServer(http.HandlerFunc(fake.ServeHTTP))

	httpClient = &http.Client{}
	t.Cleanup(func() {
		httpClient = http.DefaultClient
		serv.Close()
	})

	d, err := db.SqliteForTest()
	require.NoError(t, err)

	conf := &config.DownloaderConfig{
		Transmission: &config.TransmissionConfig{
			URL: serv.URL,
		},
		SeedingPolicy: &config.SeedingPolicy{
			IntervalInDays:    3,
			UploadAtLeastInMB: 1,
		},
	}

	client, err := New("test", conf, d, nil)
	require.NoError(t, err)

	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	threeDaysAgo := time.Now().AddDate(0, 0, -3).Format("2006-01-02")
	twoMonthsAgo := time.Now().AddDate(0, -2, 0).Format("2006-01-02")

	// r1 is current seeding, less then 3 day
	r1 := &db.DownloadStatus{
		ID:         "1",
		Downloader: "test",
		UploadHistories: map[string]int64{
			yesterday: 0,
		},
		State: db.DownloadSeeding,
	}
	require.NoError(t, d.Create(r1).Error)

	// r2 is current seeding, more then 3 day, latest upload - 3 day ago > 1MB, it will continue seeding
	r2 := &db.DownloadStatus{
		ID:         "2",
		Downloader: "test",
		UploadHistories: map[string]int64{
			threeDaysAgo: 0,
			twoMonthsAgo: 0,
		},
		State: db.DownloadSeeding,
	}
	require.NoError(t, d.Create(r2).Error)

	// r3 is current seeding, more then 3 day, latest upload - 3 day ago < 1MB, it will stop seeding
	r3 := &db.DownloadStatus{
		ID:         "3",
		Downloader: "test",
		UploadHistories: map[string]int64{
			threeDaysAgo: 0,
		},
		State: db.DownloadSeeding,
	}
	require.NoError(t, d.Create(r3).Error)

	// r4 is not current seeding, more then 30 day, record should be deleted
	r4 := &db.DownloadStatus{
		ID:         "4",
		Downloader: "test",
		UpdatedAt:  time.Now().AddDate(0, 0, -1-db.StoreMaxDays),
		UploadHistories: map[string]int64{
			threeDaysAgo: 0,
		},
		State:     db.DownloadStopped,
		MoveState: db.Moved,
	}
	require.NoError(t, d.Create(r4).Error)

	fake.resp = []any{
		&torrentGetResults{
			Torrents: []transmissionrpc.Torrent{
				newTorrent(1, "1", transmissionrpc.TorrentStatusSeed, 100),
				newTorrent(2, "2", transmissionrpc.TorrentStatusSeed, 1025*1024),
				newTorrent(3, "3", transmissionrpc.TorrentStatusSeed, 1000*1024),
				newTorrent(4, "4", transmissionrpc.TorrentStatusStopped, 1000*1024),
				// r5 is brand new, insert to db
				newTorrent(5, "5", transmissionrpc.TorrentStatusSeed, 1000*1024),
			},
		},
		&struct{}{},
		&struct{}{},
	}

	client.checkDailySeeding()

	assert.Len(t, fake.reqs, 3)
	assert.Equal(t, "torrent-get", fake.reqs[0].Method)
	assert.Equal(t, "torrent-stop", fake.reqs[1].Method)
	assert.Equal(t, "torrent-remove", fake.reqs[2].Method)
	assert.Equal(t, map[string]interface{}{"ids": []interface{}{float64(3)}}, fake.reqs[1].Arguments)
	assert.Equal(t, map[string]interface{}{"ids": []interface{}{float64(4)}, "delete-local-data": true}, fake.reqs[2].Arguments)

	{
		// r1 new history item added
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "1").Error)
		assert.Equal(t, map[string]int64{
			yesterday: 0,
			today:     100,
		}, r.UploadHistories)
	}

	{
		// r2 new history item added and cleanup old item.
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "2").Error)
		assert.Equal(t, map[string]int64{
			threeDaysAgo: 0,
			today:        1025 * 1024,
		}, r.UploadHistories)
	}

	{
		// r3 new history item added.
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "3").Error)
		assert.Equal(t, map[string]int64{
			threeDaysAgo: 0,
			today:        1000 * 1024,
		}, r.UploadHistories)
	}

	{
		// r4 should be deleted
		r := &db.DownloadStatus{}
		err := d.First(r, "id = ?", "4").Error
		require.NoError(t, err)
		assert.Equal(t, r.State, db.DownloadDeleted)
	}

	{
		// r5 should be inserted
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "5").Error)
		assert.Equal(t, map[string]int64{
			today: 1000 * 1024,
		}, r.UploadHistories)
	}
}

func newTorrentWithProgress(id int64, hash string, status transmissionrpc.TorrentStatus, percentDone float64, downloadDir string, files []transmissionrpc.TorrentFile) transmissionrpc.Torrent {
	name := fmt.Sprintf("Torrent %d", id)
	uploaded := int64(0)
	return transmissionrpc.Torrent{
		ID:           &id,
		HashString:   &hash,
		Status:       &status,
		PercentDone:  &percentDone,
		Name:         &name,
		DownloadDir:  &downloadDir,
		Files:        files,
		UploadedEver: &uploaded,
	}
}

func TestProgressChecker(t *testing.T) {
	fake := &fakeTransmission{}

	serv := httptest.NewServer(http.HandlerFunc(fake.ServeHTTP))

	httpClient = &http.Client{}
	t.Cleanup(func() {
		httpClient = http.DefaultClient
		serv.Close()
	})

	d, err := db.SqliteForTest()
	require.NoError(t, err)

	tmpDir, err := os.MkdirTemp("", "autoget-test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	downloadDir := filepath.Join(tmpDir, "download")
	finishedDir := filepath.Join(tmpDir, "finished")
	require.NoError(t, os.Mkdir(downloadDir, 0755))
	require.NoError(t, os.Mkdir(finishedDir, 0755))

	conf := &config.DownloaderConfig{
		Transmission: &config.TransmissionConfig{
			URL:         serv.URL,
			DownloadDir: downloadDir,
			FinishedDir: finishedDir,
		},
	}

	// Create fake organizer server
	organizerServ := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/plan", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req organizer.PlanRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.NotEmpty(t, req.Dir)

		// Return plan for torrent with ID "3"
		if len(req.Files) > 0 && req.Files[0] == "file1.mkv" {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(organizer.PlanResponse{
				Plan: []organizer.PlanAction{
					{File: "/finished/3/file1.mkv", Action: organizer.ActionMove, Target: "/movies/movie1.mkv"},
					{File: "/finished/3/file2.srt", Action: organizer.ActionMove, Target: "/movies/movie1.srt"},
				},
			})
		} else {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(organizer.PlanResponse{Plan: []organizer.PlanAction{}})
		}
	}))
	t.Cleanup(func() { organizerServ.Close() })

	organizerClient, err := organizer.NewClient(organizerServ.URL, nil)
	require.NoError(t, err)

	client, err := New("test", conf, d, organizerClient)
	require.NoError(t, err)

	// r1 is downloading
	r1 := &db.DownloadStatus{
		ID:               "1",
		Downloader:       "test",
		State:            db.DownloadStarted,
		DownloadProgress: 0,
	}
	require.NoError(t, d.Create(r1).Error)

	// r2 is seeding and unmoved
	r2 := &db.DownloadStatus{
		ID:         "2",
		Downloader: "test",
		State:      db.DownloadSeeding,
		MoveState:  db.UnMoved,
	}
	require.NoError(t, d.Create(r2).Error)

	// create a fake file for r2
	r2FileContent := "hello world"
	r2FileName := "r2.txt"
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, r2FileName), []byte(r2FileContent), 0644))

	// create another fake file in a subdirectory for r2
	r2SubFileContent := "hello sub world"
	r2SubFileName := filepath.Join("sub", "r2.txt")
	require.NoError(t, os.MkdirAll(filepath.Join(downloadDir, "sub"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(downloadDir, r2SubFileName), []byte(r2SubFileContent), 0644))

	// r3 is moved but needs organizer planning
	r3 := &db.DownloadStatus{
		ID:            "3",
		Downloader:    "test",
		State:         db.DownloadSeeding,
		MoveState:     db.Moved,
		OrganizeState: db.Unplaned,
		FileList:      []string{"file1.mkv", "file2.srt"},
		Metadata:      map[string]interface{}{"title": "Test Movie"},
	}
	require.NoError(t, d.Create(r3).Error)

	percentDone1 := 0.5
	percentDone2 := 1.0
	// Set download speed less than 2MB/s to trigger organizer planning
	downloadSpeed := int64(1000 * 1000) // 1MB/s
	activeTorrentCount := int64(1)

	fake.resp = []any{
		&torrentGetResults{
			Torrents: []transmissionrpc.Torrent{
				newTorrentWithProgress(1, "1", transmissionrpc.TorrentStatusDownload, percentDone1, downloadDir, nil),
				newTorrentWithProgress(2, "2", transmissionrpc.TorrentStatusSeed, percentDone2, downloadDir, []transmissionrpc.TorrentFile{
					{Name: r2FileName, Length: int64(len(r2FileContent))},
					{Name: r2SubFileName, Length: int64(len(r2SubFileContent))},
				}),
				newTorrentWithProgress(3, "3", transmissionrpc.TorrentStatusSeed, 1.0, downloadDir, []transmissionrpc.TorrentFile{
					{Name: "file1.mkv", Length: 1000},
					{Name: "file2.srt", Length: 100},
				}),
			},
		},
		&transmissionrpc.SessionStats{
			DownloadSpeed:      downloadSpeed,
			ActiveTorrentCount: activeTorrentCount,
		},
	}

	client.ProgressChecker()

	assert.Len(t, fake.reqs, 2)
	assert.Equal(t, "torrent-get", fake.reqs[0].Method)
	assert.Equal(t, "session-stats", fake.reqs[1].Method)

	{
		// r1 progress updated
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "1").Error)
		assert.Equal(t, uint16(percentDone1*1000), r.DownloadProgress)
	}

	{
		// r2 moved
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "2").Error)
		assert.Equal(t, db.Moved, r.MoveState)

		// check file copied
		copiedContent, err := os.ReadFile(filepath.Join(finishedDir, "2", r2FileName))
		require.NoError(t, err)
		assert.Equal(t, r2FileContent, string(copiedContent))

		// check sub file copied
		copiedSubContent, err := os.ReadFile(filepath.Join(finishedDir, "2", r2SubFileName))
		require.NoError(t, err)
		assert.Equal(t, r2SubFileContent, string(copiedSubContent))
	}

	{
		// r3 should have organizer plan created
		r := &db.DownloadStatus{}
		require.NoError(t, d.First(r, "id = ?", "3").Error)
		assert.Equal(t, db.Planed, r.OrganizeState)
		assert.NotEmpty(t, r.OrganizePlans)
		assert.Len(t, r.OrganizePlans.Plan, 2)

		// Verify the plan actions
		actions := r.OrganizePlans.Plan
		assert.Equal(t, "/finished/3/file1.mkv", actions[0].File)
		assert.Equal(t, organizer.ActionMove, actions[0].Action)
		assert.Equal(t, "/movies/movie1.mkv", actions[0].Target)

		assert.Equal(t, "/finished/3/file2.srt", actions[1].File)
		assert.Equal(t, organizer.ActionMove, actions[1].Action)
		assert.Equal(t, "/movies/movie1.srt", actions[1].Target)
	}
}

func TestCreateOrganizerPlan(t *testing.T) {
	d, err := db.SqliteForTest()
	require.NoError(t, err)

	// Create fake organizer server
	organizerServ := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/plan", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req organizer.PlanRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.NotEmpty(t, req.Dir)

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(organizer.PlanResponse{
			Plan: []organizer.PlanAction{
				{File: "movie.mkv", Action: organizer.ActionMove, Target: "/movies/Test Movie.mkv"},
				{File: "subtitle.srt", Action: organizer.ActionSkip},
			},
		})
	}))
	t.Cleanup(func() { organizerServ.Close() })

	organizerClient, err := organizer.NewClient(organizerServ.URL, nil)
	require.NoError(t, err)

	conf := &config.DownloaderConfig{
		Transmission: &config.TransmissionConfig{},
	}
	client, err := New("test", conf, d, organizerClient)
	require.NoError(t, err)

	t.Run("successful plan creation", func(t *testing.T) {
		// Create a download status that needs planning
		status := &db.DownloadStatus{
			ID:            "test1",
			Downloader:    "test",
			State:         db.DownloadSeeding,
			MoveState:     db.Moved,
			OrganizeState: db.Unplaned,
			FileList:      []string{"movie.mkv", "subtitle.srt"},
			Metadata:      map[string]interface{}{"title": "Test Movie"},
		}
		require.NoError(t, d.Create(status).Error)

		client.createOrganizerPlan()

		// Verify the plan was created
		updated := &db.DownloadStatus{}
		require.NoError(t, d.First(updated, "id = ?", "test1").Error)
		assert.Equal(t, db.Planed, updated.OrganizeState)
		assert.NotNil(t, updated.OrganizePlans)
		assert.Len(t, updated.OrganizePlans.Plan, 2)

		// Verify plan content
		actions := updated.OrganizePlans.Plan
		assert.Equal(t, "movie.mkv", actions[0].File)
		assert.Equal(t, organizer.ActionMove, actions[0].Action)
		assert.Equal(t, "/movies/Test Movie.mkv", actions[0].Target)

		assert.Equal(t, "subtitle.srt", actions[1].File)
		assert.Equal(t, organizer.ActionSkip, actions[1].Action)
	})

	t.Run("organizer service error", func(t *testing.T) {
		// Create a failing organizer server
		failingServ := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("organizer service error"))
		}))
		t.Cleanup(func() { failingServ.Close() })

		failingClient, err := organizer.NewClient(failingServ.URL, nil)
		require.NoError(t, err)

		clientWithFailingOrganizer, err := New("test", conf, d, failingClient)
		require.NoError(t, err)

		// Create a download status that needs planning
		status := &db.DownloadStatus{
			ID:            "test2",
			Downloader:    "test",
			State:         db.DownloadSeeding,
			MoveState:     db.Moved,
			OrganizeState: db.Unplaned,
			FileList:      []string{"movie.mkv"},
			Metadata:      map[string]interface{}{"title": "Test Movie"},
		}
		require.NoError(t, d.Create(status).Error)

		clientWithFailingOrganizer.createOrganizerPlan()

		// Verify the status changed to error
		updated := &db.DownloadStatus{}
		require.NoError(t, d.First(updated, "id = ?", "test2").Error)
		assert.Equal(t, db.CreatePlanFailed, updated.OrganizeState)
		assert.Nil(t, updated.OrganizePlans)
	})

	t.Run("already planned torrents are skipped", func(t *testing.T) {
		// Create a download status that's already planned
		status := &db.DownloadStatus{
			ID:            "test3",
			Downloader:    "test",
			State:         db.DownloadSeeding,
			MoveState:     db.Moved,
			OrganizeState: db.Planed,
			FileList:      []string{"movie.mkv"},
			Metadata:      map[string]interface{}{"title": "Test Movie"},
		}
		require.NoError(t, d.Create(status).Error)

		client.createOrganizerPlan()

		// Verify no new requests were made to organizer service
		updated := &db.DownloadStatus{}
		require.NoError(t, d.First(updated, "id = ?", "test3").Error)
		assert.Equal(t, db.Planed, updated.OrganizeState)
	})
}
