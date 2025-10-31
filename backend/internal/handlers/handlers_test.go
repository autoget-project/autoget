package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/autoget-project/autoget/backend/downloaders"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/errors"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type indexerMock struct {
	mockName           string
	mockCategories     []indexers.Category
	mockCategoriesErr  *errors.HTTPStatusError
	mockListResult     *indexers.ListResult
	mockListErr        *errors.HTTPStatusError
	mockDetailResult   *indexers.ResourceDetail
	mockDetailErr      *errors.HTTPStatusError
	mockDownloadResult *indexers.DownloadResult
	mockDownloadErr    *errors.HTTPStatusError
}

func (i *indexerMock) Name() string {
	return i.mockName
}

func (i *indexerMock) Categories() ([]indexers.Category, *errors.HTTPStatusError) {
	return i.mockCategories, i.mockCategoriesErr
}

func (i *indexerMock) List(req *indexers.ListRequest) (*indexers.ListResult, *errors.HTTPStatusError) {
	return i.mockListResult, i.mockListErr
}

func (i *indexerMock) Detail(id string, fileList bool) (*indexers.ResourceDetail, *errors.HTTPStatusError) {
	return i.mockDetailResult, i.mockDetailErr
}

func (i *indexerMock) Download(id string) (*indexers.DownloadResult, *errors.HTTPStatusError) {
	return i.mockDownloadResult, i.mockDownloadErr
}

func (i *indexerMock) RegisterRSSCronjob(cron *cron.Cron) {}

func (i *indexerMock) DownloaderName() string {
	return "mock-downloader"
}

type downloadersMock struct {
	mockTorrentsDir string
	mockDownloadDir string
}

func (d *downloadersMock) TorrentsDir() string {
	return d.mockTorrentsDir
}

func (d *downloadersMock) DownloadDir() string {
	return d.mockDownloadDir
}

func (d *downloadersMock) RegisterCronjobs(cron *cron.Cron)            {}
func (d *downloadersMock) RegisterDailySeedingChecker(cron *cron.Cron) {}
func (d *downloadersMock) ProgressChecker()                            {}

func testSetup(t *testing.T) (*Service, *gin.Engine, *indexerMock, *gorm.DB) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	testDB, err := db.SqliteForTest()
	require.NoError(t, err)

	m := &indexerMock{
		mockName: "mock",
	}

	serv := &Service{
		db: testDB,
		indexers: map[string]indexers.IIndexer{
			"mock": m,
		},
		downloaders: map[string]downloaders.IDownloader{
			"mock": &downloadersMock{
				mockTorrentsDir: "/torrents",
				mockDownloadDir: "/downloads",
			},
		},
	}

	router := gin.Default()
	serv.SetupRouter(router.Group("/"))

	return serv, router, m, testDB
}

func TestService_indexerCategories(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		_, router, m, _ := testSetup(t)

		m.mockCategories = []indexers.Category{
			{ID: "1", Name: "Category 1"},
			{ID: "2", Name: "Category 2"},
		}

		w := httptest.NewRecorder()

		req := httptest.NewRequest("GET", "/indexers/mock/categories", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var categories []indexers.Category
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &categories))

		assert.Len(t, categories, 2)
		assert.Equal(t, "1", categories[0].ID)
		assert.Equal(t, "2", categories[1].ID)
	})

	t.Run("error", func(t *testing.T) {
		tests := []struct {
			name         string
			indexerName  string
			mockErr      *errors.HTTPStatusError
			expectedCode int
			expectedMsg  string
		}{
			{
				name:         "indexer not found",
				indexerName:  "nonexistent",
				mockErr:      nil,
				expectedCode: http.StatusNotFound,
				expectedMsg:  "Indexer not found",
			},
			{
				name:         "mock indexer returns error",
				indexerName:  "mock",
				mockErr:      errors.NewHTTPStatusError(http.StatusInternalServerError, "mock error"),
				expectedCode: http.StatusInternalServerError,
				expectedMsg:  "mock error",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, router, m, _ := testSetup(t)

				m.mockCategoriesErr = tt.mockErr

				w := httptest.NewRecorder()

				req := httptest.NewRequest("GET", "/indexers/"+tt.indexerName+"/categories", nil)
				router.ServeHTTP(w, req)

				assert.Equal(t, tt.expectedCode, w.Code)

				var resp map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.expectedMsg, resp["error"])
			})
		}
	})
}

func TestService_listIndexers(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		_, router, _, _ := testSetup(t)

		w := httptest.NewRecorder()

		req := httptest.NewRequest("GET", "/indexers", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var indexers []string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &indexers))

		assert.Len(t, indexers, 1)
		assert.Contains(t, indexers, "mock")
	})
}

func TestService_indexerResourceDetail(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		_, router, m, _ := testSetup(t)

		m.mockDetailResult = &indexers.ResourceDetail{
			ListResourceItem: indexers.ListResourceItem{
				ID:    "res-detail-1",
				Title: "Detailed Resource 1",
			},
			Description: "This is a detailed description.",
		}

		w := httptest.NewRecorder()

		req := httptest.NewRequest("GET", "/indexers/mock/resources/res-detail-1", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var detailResult indexers.ResourceDetail
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detailResult))

		assert.Equal(t, "res-detail-1", detailResult.ID)
		assert.Equal(t, "Detailed Resource 1", detailResult.Title)
	})

	t.Run("error", func(t *testing.T) {
		tests := []struct {
			name         string
			indexerName  string
			resourceID   string
			mockErr      *errors.HTTPStatusError
			expectedCode int
			expectedMsg  string
		}{
			{
				name:         "indexer not found",
				indexerName:  "nonexistent",
				resourceID:   "any",
				mockErr:      nil,
				expectedCode: http.StatusNotFound,
				expectedMsg:  "Indexer not found",
			},
			{
				name:         "mock indexer returns error",
				indexerName:  "mock",
				resourceID:   "some-id",
				mockErr:      errors.NewHTTPStatusError(http.StatusInternalServerError, "mock detail error"),
				expectedCode: http.StatusInternalServerError,
				expectedMsg:  "mock detail error",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, router, m, _ := testSetup(t)

				m.mockDetailErr = tt.mockErr

				w := httptest.NewRecorder()

				req := httptest.NewRequest("GET", "/indexers/"+tt.indexerName+"/resources/"+tt.resourceID, nil)
				router.ServeHTTP(w, req)

				assert.Equal(t, tt.expectedCode, w.Code)

				var resp map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.expectedMsg, resp["error"])
			})
		}
	})
}

func TestService_indexerListResources(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		_, router, m, _ := testSetup(t)

		m.mockListResult = &indexers.ListResult{
			Pagination: indexers.Pagination{
				Page:       1,
				TotalPages: 1,
				PageSize:   10,
				Total:      1,
			},
			Resources: []indexers.ListResourceItem{
				{ID: "res1", Title: "Resource 1"},
			},
		}

		w := httptest.NewRecorder()

		req := httptest.NewRequest("GET", "/indexers/mock/resources?category=test&keyword=foo", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var listResult indexers.ListResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResult))

		assert.Equal(t, uint32(1), listResult.Pagination.Total)
		assert.Len(t, listResult.Resources, 1)
		assert.Equal(t, "res1", listResult.Resources[0].ID)
	})

	t.Run("error", func(t *testing.T) {
		tests := []struct {
			name         string
			indexerName  string
			queryParams  string
			mockErr      *errors.HTTPStatusError
			expectedCode int
			expectedMsg  string
		}{
			{
				name:         "indexer not found",
				indexerName:  "nonexistent",
				queryParams:  "",
				mockErr:      nil,
				expectedCode: http.StatusNotFound,
				expectedMsg:  "Indexer not found",
			},
			{
				name:         "invalid query params",
				indexerName:  "mock",
				queryParams:  "page=abc", // Invalid page parameter
				mockErr:      nil,
				expectedCode: http.StatusBadRequest,
				expectedMsg:  "strconv.ParseUint: parsing \"abc\": invalid syntax", // Gin's default error message for invalid uint
			},
			{
				name:         "mock indexer returns error",
				indexerName:  "mock",
				queryParams:  "",
				mockErr:      errors.NewHTTPStatusError(http.StatusInternalServerError, "mock list error"),
				expectedCode: http.StatusInternalServerError,
				expectedMsg:  "mock list error",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, router, m, _ := testSetup(t)

				m.mockListErr = tt.mockErr

				w := httptest.NewRecorder()

				req := httptest.NewRequest("GET", "/indexers/"+tt.indexerName+"/resources?"+tt.queryParams, nil)
				router.ServeHTTP(w, req)

				assert.Equal(t, tt.expectedCode, w.Code)

				var resp map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.expectedMsg, resp["error"])
			})
		}
	})
}

func TestService_indexerRegisterSearch(t *testing.T) {
	t.Run("success - download action", func(t *testing.T) {
		_, router, _, testDB := testSetup(t)

		w := httptest.NewRecorder()
		reqBody := `{"text": "test search", "action": "download"}`
		req := httptest.NewRequest("GET", "/indexers/mock/registerSearch", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var searches []db.RSSSearch
		err := testDB.Find(&searches).Error
		require.NoError(t, err)
		assert.Len(t, searches, 1)
		assert.Equal(t, "mock", searches[0].Indexer)
		assert.Equal(t, "test search", searches[0].Text)
		assert.Equal(t, "download", searches[0].Action)
	})

	t.Run("success - notification action", func(t *testing.T) {
		_, router, _, testDB := testSetup(t)

		w := httptest.NewRecorder()
		reqBody := `{"text": "another search", "action": "notification"}`
		req := httptest.NewRequest("GET", "/indexers/mock/registerSearch", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var searches []db.RSSSearch
		err := testDB.Find(&searches).Error
		require.NoError(t, err)
		assert.Len(t, searches, 1)
		assert.Equal(t, "mock", searches[0].Indexer)
		assert.Equal(t, "another search", searches[0].Text)
		assert.Equal(t, "notification", searches[0].Action)
	})

	t.Run("error - indexer not found", func(t *testing.T) {
		_, router, _, _ := testSetup(t)

		w := httptest.NewRecorder()
		reqBody := `{"text": "test", "action": "download"}`
		req := httptest.NewRequest("GET", "/indexers/nonexistent/registerSearch", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Indexer not found", resp["error"])
	})

	t.Run("error - invalid request body", func(t *testing.T) {
		tests := []struct {
			name        string
			reqBody     string
			expectedMsg string
		}{
			{
				name:        "missing text",
				reqBody:     `{"action": "download"}`,
				expectedMsg: "Key: 'indexerRegisterSearchReq.Text' Error:Field validation for 'Text' failed on the 'required' tag",
			},
			{
				name:        "missing action",
				reqBody:     `{"text": "test"}`,
				expectedMsg: "Key: 'indexerRegisterSearchReq.Action' Error:Field validation for 'Action' failed on the 'required' tag",
			},
			{
				name:        "empty body",
				reqBody:     `{}`,
				expectedMsg: "Key: 'indexerRegisterSearchReq.Text' Error:Field validation for 'Text' failed on the 'required' tag",
			},
			{
				name:        "invalid json",
				reqBody:     `invalid json`,
				expectedMsg: "invalid character 'i' looking for beginning of value",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, router, _, _ := testSetup(t)

				w := httptest.NewRecorder()
				req := httptest.NewRequest("GET", "/indexers/mock/registerSearch", strings.NewReader(tt.reqBody))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(w, req)

				assert.Equal(t, http.StatusBadRequest, w.Code)
				var resp map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Contains(t, resp["error"], tt.expectedMsg)
			})
		}
	})

	t.Run("error - invalid action value", func(t *testing.T) {
		_, router, _, _ := testSetup(t)

		w := httptest.NewRecorder()
		reqBody := `{"text": "test", "action": "invalid_action"}`
		req := httptest.NewRequest("GET", "/indexers/mock/registerSearch", strings.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "Invalid action", resp["error"])
	})
}

func TestListDownloaders(t *testing.T) {
	_, router, _, _ := testSetup(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/downloaders", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var downloaders []string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &downloaders))

	assert.Len(t, downloaders, 1)
	assert.Contains(t, downloaders, "mock")
}
