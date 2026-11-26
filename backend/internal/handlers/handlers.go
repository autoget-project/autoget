package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/downloaders"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/organizer"
)

type Service struct {
	config *config.Config
	db     *gorm.DB

	indexers        map[string]indexers.IIndexer
	downloaders     map[string]downloaders.IDownloader
	organizerClient *organizer.Client

	// acceptMu serialises accept_plan so a plan is executed at most once.
	acceptMu sync.Mutex
}

func NewService(config *config.Config, db *gorm.DB, indexers map[string]indexers.IIndexer, downloaders map[string]downloaders.IDownloader, organizerClient *organizer.Client) *Service {
	s := &Service{
		config:          config,
		db:              db,
		indexers:        indexers,
		downloaders:     downloaders,
		organizerClient: organizerClient,
	}

	return s
}

func (s *Service) SetupRouter(router *gin.RouterGroup) {
	router.GET("/indexers", s.listIndexers)
	router.GET("/indexers/:indexer/categories", s.indexerCategories)
	router.GET("/indexers/:indexer/resources", s.indexerListResources)
	router.GET("/indexers/:indexer/resources/:resource", s.indexerResourceDetail)
	router.POST("/indexers/:indexer/resources/:resource/download", s.indexerDownload)
	router.POST("/indexers/:indexer/registerSearch", s.indexerRegisterSearch)

	router.GET("/downloaders", s.listDownloaders)
	router.GET("/downloaders/:downloader", s.getDownloaderStatuses)
	router.POST("/download/:id/organize", s.organizeDownload)
	router.DELETE("/download/:id", s.deleteDownload)

	router.GET("/image", s.image)
}

func (s *Service) listIndexers(c *gin.Context) {
	resp := []string{}
	for k := range s.indexers {
		resp = append(resp, k)
	}
	slices.Sort(resp)
	c.JSON(200, resp)
}

func (s *Service) indexerCategories(c *gin.Context) {
	indexerName := c.Param("indexer")
	indexer, ok := s.indexers[indexerName]
	if !ok {
		c.JSON(404, gin.H{"error": "Indexer not found"})
		return
	}

	categories, err := indexer.Categories()
	if err != nil {
		respondIndexerError(c, err)
		return
	}

	c.JSON(200, categories)
}

type ListRequest struct {
	Category  string   `form:"category"`
	Keyword   string   `form:"keyword"`
	Page      uint32   `form:"page"`
	PageSize  uint32   `form:"pageSize"`
	Free      bool     `form:"free"`
	Standards []string `form:"standards"`
}

func (s *Service) indexerListResources(c *gin.Context) {
	indexerName := c.Param("indexer")
	indexer, ok := s.indexers[indexerName]
	if !ok {
		c.JSON(404, gin.H{"error": "Indexer not found"})
		return
	}

	req := &ListRequest{}
	if err := c.ShouldBindQuery(req); err != nil {
		respondError(c, 400, "invalid request", err)
		return
	}

	lreq := &indexers.ListRequest{
		Category:  req.Category,
		Keyword:   req.Keyword,
		Page:      req.Page,
		PageSize:  req.PageSize,
		Free:      req.Free,
		Standards: req.Standards,
	}

	listResult, err := indexer.List(lreq)
	if err != nil {
		respondIndexerError(c, err)
		return
	}

	c.JSON(200, listResult)
}

func (s *Service) indexerResourceDetail(c *gin.Context) {
	indexerName := c.Param("indexer")
	indexer, ok := s.indexers[indexerName]
	if !ok {
		c.JSON(404, gin.H{"error": "Indexer not found"})
		return
	}

	resourceID := c.Param("resource")
	detail, err := indexer.Detail(resourceID, true)
	if err != nil {
		respondIndexerError(c, err)
		return
	}

	c.JSON(200, detail)
}

func (s *Service) indexerDownload(c *gin.Context) {
	indexerName := c.Param("indexer")
	indexer, ok := s.indexers[indexerName]
	if !ok {
		c.JSON(404, gin.H{"error": "Indexer not found"})
		return
	}

	if err := indexers.DownloadTracked(s.db, indexer, c.Param("resource")); err != nil {
		respondIndexerError(c, err)
		return
	}

	c.JSON(200, gin.H{"status": "started"})
}

type indexerRegisterSearchReq struct {
	Text   string `json:"text" binding:"required"`
	Action string `json:"action" binding:"required"`
}

func (s *Service) indexerRegisterSearch(c *gin.Context) {
	indexerName := c.Param("indexer")
	if _, ok := s.indexers[indexerName]; !ok {
		c.JSON(404, gin.H{"error": "Indexer not found"})
		return
	}

	req := &indexerRegisterSearchReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		respondError(c, 400, "invalid request", err)
		return
	}

	if req.Action != indexers.ActionDownload &&
		req.Action != indexers.ActionNotification {
		c.JSON(400, gin.H{"error": "Invalid action"})
		return
	}

	if err := db.AddSearch(s.db, &db.RSSSearch{
		Indexer: indexerName,
		Text:    req.Text,
		Action:  req.Action,
	}); err != nil {
		respondError(c, 500, "failed to register search", err)
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

type DownloaderInfoResponse struct {
	Name               string `json:"name"`
	CountOfDownloading int64  `json:"count_of_downloading"`
	CountOfPlanned     int64  `json:"count_of_planned"`
	CountOfFailed      int64  `json:"count_of_failed"`
}

func (s *Service) listDownloaders(c *gin.Context) {
	// Get all downloader names from the service configuration
	var downloaderNames []string
	for name := range s.downloaders {
		downloaderNames = append(downloaderNames, name)
	}

	// Get state counts for all configured downloaders
	downloadersState, err := db.GetAllDownloadersStateCountsWithNames(s.db, downloaderNames)
	if err != nil {
		respondError(c, 500, "failed to list downloaders", err)
		return
	}

	// Convert to proper response struct
	var response []DownloaderInfoResponse
	for _, item := range downloadersState {
		name, _ := item["name"].(string)
		countOfDownloading, _ := item["count_of_downloading"].(int64)
		countOfPlanned, _ := item["count_of_planned"].(int64)
		countOfFailed, _ := item["count_of_failed"].(int64)

		response = append(response, DownloaderInfoResponse{
			Name:               name,
			CountOfDownloading: countOfDownloading,
			CountOfPlanned:     countOfPlanned,
			CountOfFailed:      countOfFailed,
		})
	}

	// Sort by name
	slices.SortFunc(response, func(a, b DownloaderInfoResponse) int {
		return strings.Compare(a.Name, b.Name)
	})

	c.JSON(200, response)
}

type DownloaderStatusResponse struct {
	State     db.DownloaderStateCounts `json:"state"`
	Resources []db.DownloadStatus      `json:"resources"`
	Total     int64                    `json:"total"`     // rows in the whole list, across pages
	Page      int                      `json:"page"`      // 1-based
	PageSize  int                      `json:"page_size"` // requested (or default) page size
}

const (
	defaultPageSize = 50
	maxPageSize     = 200
)

// parsePaging reads the optional page (default 1) and page_size (default
// defaultPageSize, at most maxPageSize) query parameters. It answers 400 and
// returns false on invalid values.
func parsePaging(c *gin.Context) (page, pageSize int, ok bool) {
	page, pageSize = 1, defaultPageSize
	if v := c.Query("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			c.JSON(400, gin.H{"error": "page must be a positive integer"})
			return 0, 0, false
		}
		page = n
	}
	if v := c.Query("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			c.JSON(400, gin.H{"error": fmt.Sprintf("page_size must be between 1 and %d", maxPageSize)})
			return 0, 0, false
		}
		pageSize = n
	}
	return page, pageSize, true
}

func (s *Service) getDownloaderStatuses(c *gin.Context) {
	downloaderName := c.Param("downloader")

	// Check if downloader exists
	_, ok := s.downloaders[downloaderName]
	if !ok {
		c.JSON(404, gin.H{"error": "Downloader not found"})
		return
	}

	state := c.Query("state")
	if state == "" {
		c.JSON(400, gin.H{"error": "State parameter is required. Valid states: downloading, seeding, stopped, planned, failed"})
		return
	}

	page, pageSize, ok := parsePaging(c)
	if !ok {
		return
	}

	statuses, total, valid, err := db.ListDownloadStatuses(s.db, downloaderName, db.DownloadList(state), pageSize, (page-1)*pageSize)
	if !valid {
		c.JSON(400, gin.H{"error": "Invalid state. Valid states: downloading, seeding, stopped, planned, failed"})
		return
	}
	if err != nil {
		respondError(c, 500, "failed to list downloads", err)
		return
	}

	// Get state counts for this downloader
	stateCounts, err := db.GetDownloaderStateCounts(s.db, downloaderName)
	if err != nil {
		respondError(c, 500, "failed to list downloads", err)
		return
	}

	response := DownloaderStatusResponse{
		State:     *stateCounts,
		Resources: statuses,
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
	}

	c.JSON(200, response)
}

const (
	maxImageSize     = 10 << 20
	maxImageRedirect = 3
)

// imageHost is the only host the image proxy may fetch from. It is a variable so tests can point it at a local server.
var imageHost = "img.m-team.cc"

var errInvalidImageURL = errors.New("invalid image url")

// imageClient has a timeout and refuses to follow redirects off imageHost.
var imageClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || req.URL.Host != imageHost || len(via) >= maxImageRedirect {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

// validateImageURL strictly checks that raw points to an image on imageHost.
func validateImageURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errInvalidImageURL
	}
	if u.Scheme != "https" || u.Host != imageHost || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, errInvalidImageURL
	}
	if !strings.HasPrefix(u.Path, "/images/") || path.Clean(u.Path) != u.Path {
		return nil, errInvalidImageURL
	}
	return u, nil
}

func (s *Service) image(c *gin.Context) {
	// m-team image require "referer" to request
	raw, ok := c.GetQuery("url")
	if !ok {
		c.JSON(400, gin.H{"error": "missing url query"})
		return
	}

	u, err := validateImageURL(raw)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid url"})
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		c.JSON(400, gin.H{"error": "invalid url"})
		return
	}

	req.Header.Set("referer", "https://kp.m-team.cc/")
	resp, err := imageClient.Do(req)
	if err != nil {
		log.Error().Err(err).Str("url", u.String()).Msg("image proxy upstream request failed")
		c.JSON(502, gin.H{"error": "upstream error"})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		log.Warn().Int("status", resp.StatusCode).Str("url", u.String()).Msg("image proxy upstream non-200")
		if resp.StatusCode == http.StatusNotFound {
			c.JSON(404, gin.H{"error": "image not found"})
			return
		}
		c.JSON(502, gin.H{"error": "upstream error"})
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		log.Warn().Str("content_type", contentType).Str("url", u.String()).Msg("image proxy upstream non-image")
		c.JSON(502, gin.H{"error": "upstream error"})
		return
	}
	if resp.ContentLength > maxImageSize {
		c.JSON(502, gin.H{"error": "upstream image too large"})
		return
	}

	c.Header("Cache-Control", "public, max-age=86400")
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, resp.ContentLength, contentType, io.LimitReader(resp.Body, maxImageSize), nil)
}

type organizeDownloadReq struct {
	Action string `form:"action" binding:"required"`
}

func (s *Service) organizeDownload(c *gin.Context) {
	downloadID := c.Param("id")

	// Get the download status
	downloadStatus, err := db.GetDownloadStatusByID(s.db, downloadID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "Download not found"})
		} else {
			respondError(c, 500, "failed to load download", err)
		}
		return
	}

	// Parse the action parameter
	req := &organizeDownloadReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		respondError(c, 400, "invalid request", err)
		return
	}

	switch req.Action {
	case "accept_plan":
		s.handleAcceptPlan(c, downloadStatus)
	case "manual_organized":
		s.handleManualOrganized(c, downloadStatus)
	case "re_plan":
		s.handleRePlan(c, downloadStatus)
	default:
		c.JSON(400, gin.H{"error": "Invalid action. Valid actions: accept_plan, manual_organized, re_plan"})
	}
}

func (s *Service) handleAcceptPlan(c *gin.Context, downloadStatus *db.DownloadStatus) {
	s.acceptMu.Lock()
	defer s.acceptMu.Unlock()

	// Reload under the lock: the row loaded by the caller may be stale.
	downloadStatus, err := db.GetDownloadStatusByID(s.db, downloadStatus.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "Download not found"})
		} else {
			respondError(c, 500, "failed to load download", err)
		}
		return
	}

	if downloadStatus.OrganizePlans == nil {
		c.JSON(400, gin.H{"error": "No organize plan available"})
		return
	}

	if downloadStatus.OrganizeState != db.Planned {
		c.JSON(409, gin.H{"error": "plan is not in planned state"})
		return
	}

	// Execute the plan
	executeReq := &organizer.ExecuteRequest{
		Dir:  downloadStatus.ID,
		Plan: downloadStatus.OrganizePlans.Plan,
	}

	success, failedResp, err := s.organizerClient.Execute(context.WithoutCancel(c.Request.Context()), executeReq)
	if err != nil {
		respondError(c, 500, "failed to execute organize plan", err)
		return
	}

	// Update the organize plan action based on execution result
	if success {
		downloadStatus.OrganizeState = db.Organized
	} else {
		downloadStatus.OrganizeState = db.ExecutePlanFailed
	}

	// Only transition a plan that is still planned.
	updated, err := db.UpdateDownloadStatusColumns(s.db, downloadStatus, []string{"organize_state"}, "organize_state = ?", db.Planned)
	if err != nil {
		respondError(c, 500, "failed to save download status", err)
		return
	}
	if !updated {
		c.JSON(409, gin.H{"error": "plan is not in planned state"})
		return
	}

	if success {
		c.JSON(200, gin.H{"status": "organization completed successfully"})
	} else {
		c.JSON(200, gin.H{
			"status": "organization partially completed",
			"failed": failedResp,
		})
	}
}

func (s *Service) handleManualOrganized(c *gin.Context, downloadStatus *db.DownloadStatus) {
	// Set the organize plan action to manually organized
	downloadStatus.OrganizeState = db.Organized

	if _, err := db.UpdateDownloadStatusColumns(s.db, downloadStatus, []string{"organize_state"}); err != nil {
		respondError(c, 500, "failed to save download status", err)
		return
	}

	c.JSON(200, gin.H{"status": "marked as manually organized"})
}

func (s *Service) handleRePlan(c *gin.Context, downloadStatus *db.DownloadStatus) {
	if downloadStatus.OrganizeState == db.Organized {
		c.JSON(409, gin.H{"error": "download is already organized"})
		return
	}

	// Get user_hint from query parameter (optional)
	userHint := c.Query("user_hint")

	// Both hint and no-hint replans go through the unified replan endpoint: the
	// previous result is passed in a dedicated field and treated as flawed, so
	// the organizer re-classifies instead of reproducing the same wrong plan.
	resp, err := s.organizerClient.Replan(c.Request.Context(), &organizer.ReplanRequest{
		Dir:            downloadStatus.ID,
		Files:          downloadStatus.FileList,
		Metadata:       downloadStatus.Metadata,
		PreviousResult: downloadStatus.OrganizePlans,
		UserHint:       userHint,
	})

	if err != nil {
		// Update the state to CreatePlanFailed when re-planning fails
		downloadStatus.OrganizeState = db.CreatePlanFailed
		// Best effort: if the download was organized meanwhile, keep that state.
		if _, saveErr := db.UpdateDownloadStatusColumns(s.db, downloadStatus, []string{"organize_state"}, "organize_state <> ?", db.Organized); saveErr != nil {
			respondError(c, 500, "failed to save download status", saveErr)
			return
		}
		respondError(c, 500, "failed to re-plan download", err)
		return
	}

	// Update the organize plan and state
	downloadStatus.OrganizePlans = resp
	downloadStatus.OrganizeState = db.Planned

	updated, err := db.UpdateDownloadStatusColumns(s.db, downloadStatus, []string{"organize_plans", "organize_state"}, "organize_state <> ?", db.Organized)
	if err != nil {
		respondError(c, 500, "failed to save download status", err)
		return
	}
	if !updated {
		c.JSON(409, gin.H{"error": "download is already organized"})
		return
	}

	c.JSON(200, gin.H{
		"status": "re_plan completed successfully",
		"plan":   resp,
	})
}

func (s *Service) deleteDownload(c *gin.Context) {
	downloadID := c.Param("id")

	// Get the download status to find which downloader to use
	downloadStatus, err := db.GetDownloadStatusByID(s.db, downloadID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "Download not found"})
		} else {
			respondError(c, 500, "failed to load download", err)
		}
		return
	}

	// Get the downloader
	downloader, ok := s.downloaders[downloadStatus.Downloader]
	if !ok {
		c.JSON(404, gin.H{"error": "Downloader not found"})
		return
	}

	// Delete the torrent
	if err := downloader.DeleteTorrent(downloadID); err != nil {
		respondError(c, 500, "failed to delete download", err)
		return
	}

	c.JSON(200, gin.H{"status": "deleted"})
}
