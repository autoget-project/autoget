package handlers

import (
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/autoget-project/autoget/backend/downloaders"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/config"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/organizer"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Service struct {
	config *config.Config
	db     *gorm.DB

	indexers        map[string]indexers.IIndexer
	downloaders     map[string]downloaders.IDownloader
	organizerClient *organizer.Client
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
	router.GET("/indexers/:indexer/resources/:resource/download", s.indexerDownload)
	router.GET("/indexers/:indexer/registerSearch", s.indexerRegisterSearch)

	router.GET("/downloaders", s.listDownloaders)
	router.GET("/downloaders/:downloader", s.getDownloaderStatuses)
	router.POST("/download/:id/organize", s.organizeDownload)

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
		c.JSON(err.Code, gin.H{"error": err.Message})
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
		c.JSON(400, gin.H{"error": err.Error()})
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
		c.JSON(err.Code, gin.H{"error": err.Message})
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
		c.JSON(err.Code, gin.H{"error": err.Message})
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

	resourceID := c.Param("resource")

	detail, err := indexer.Detail(resourceID, true)
	if err != nil {
		c.JSON(err.Code, gin.H{"error": err.Message})
		return
	}

	res, err := indexer.Download(resourceID)
	if err != nil {
		c.JSON(err.Code, gin.H{"error": err.Message})
		return
	}

	files := []string{}
	for _, file := range detail.Files {
		files = append(files, file.Name)
	}

	downloadStatus := &db.DownloadStatus{
		ID:         res.TorrentHash,
		Downloader: indexer.DownloaderName(),
		State:      db.DownloadStarted,
		ResTitle:   detail.Title,
		ResTitle2:  detail.Title2,
		ResIndexer: indexerName,
		Category:   detail.Category,
		FileList:   files,
		Metadata:   detail.Metadata,
	}
	if err := s.db.Create(downloadStatus).Error; err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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
		c.JSON(400, gin.H{"error": err.Error()})
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
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
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
		c.JSON(500, gin.H{"error": err.Error()})
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

	var statuses []db.DownloadStatus
	var err error

	switch state {
	case "downloading":
		statuses, err = db.GetUnfinishedDownloadStatusByDownloader(s.db, downloaderName)
	case "seeding":
		// For seeding, we want downloads that are in seeding state
		statuses, err = db.GetDownloadStatusByDownloaderAndState(s.db, downloaderName, db.DownloadSeeding)
	case "stopped":
		// For stopped, we want downloads that are stopped
		statuses, err = db.GetDownloadStatusByDownloaderAndState(s.db, downloaderName, db.DownloadStopped)
	case "planned":
		// For planned, we want downloads that are moved and have been planned for organization
		statuses, err = db.GetMovedAndOrganizeStateDownloadStatusByDownloader(s.db, downloaderName, db.Planed)
	case "failed":
		// For failed, we want downloads that have failed during either plan creation or execution
		// Get both create_plan_failed and execute_plan_failed statuses
		var createFailedStatuses, executeFailedStatuses []db.DownloadStatus

		createFailedStatuses, err = db.GetMovedAndOrganizeStateDownloadStatusByDownloader(s.db, downloaderName, db.CreatePlanFailed)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		executeFailedStatuses, err = db.GetMovedAndOrganizeStateDownloadStatusByDownloader(s.db, downloaderName, db.ExecutePlanFailed)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		// Combine both lists
		statuses = append(createFailedStatuses, executeFailedStatuses...)
	default:
		c.JSON(400, gin.H{"error": "Invalid state. Valid states: downloading, seeding, stopped, planned, failed"})
		return
	}

	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Get state counts for this downloader
	stateCounts, err := db.GetDownloaderStateCounts(s.db, downloaderName)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	response := DownloaderStatusResponse{
		State:     *stateCounts,
		Resources: statuses,
	}

	c.JSON(200, response)
}

func (s *Service) image(c *gin.Context) {
	// m-team image require "referer" to request
	u, ok := c.GetQuery("url")
	if !ok {
		c.JSON(400, gin.H{"error": "missing url query"})
		return
	}

	u, _ = url.QueryUnescape(u)
	if !strings.HasPrefix(u, "https://img.m-team.cc/images/") {
		c.JSON(400, gin.H{"error": "invalid url"})
		return
	}

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	req.Header.Set("referer", "https://kp.m-team.cc/")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	defer resp.Body.Close()
	c.DataFromReader(resp.StatusCode, resp.ContentLength, resp.Header.Get("Content-Type"), resp.Body, nil)
}

type organizeDownloadReq struct {
	Action string `form:"action" binding:"required"`
}

func (s *Service) organizeDownload(c *gin.Context) {
	downloadID := c.Param("id")

	// Get the download status
	downloadStatus, err := db.GetDownloadStatusByID(s.db, downloadID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(404, gin.H{"error": "Download not found"})
		} else {
			c.JSON(500, gin.H{"error": err.Error()})
		}
		return
	}

	// Parse the action parameter
	req := &organizeDownloadReq{}
	if err := c.ShouldBindQuery(req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
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
	if downloadStatus.OrganizePlans == nil {
		c.JSON(400, gin.H{"error": "No organize plan available"})
		return
	}

	// Execute the plan
	executeReq := &organizer.ExecuteRequest{
		Dir:  downloadStatus.ID,
		Plan: downloadStatus.OrganizePlans.Plan,
	}

	success, failedResp, err := s.organizerClient.Execute(executeReq)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Update the organize plan action based on execution result
	if success {
		downloadStatus.OrganizeState = db.Organized
	} else {
		downloadStatus.OrganizeState = db.ExecutePlanFailed
	}

	// Update the download status
	if err := db.SaveDownloadStatus(s.db, downloadStatus); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
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

	// Update the download status
	if err := db.SaveDownloadStatus(s.db, downloadStatus); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"status": "marked as manually organized"})
}

func (s *Service) handleRePlan(c *gin.Context, downloadStatus *db.DownloadStatus) {
	// Get user_hint from query parameter (optional)
	userHint := c.Query("user_hint")

	var resp *organizer.PlanResponse
	var err error

	if userHint != "" {
		// Use ReplanWithHint when user_hint is provided
		resp, err = s.organizerClient.ReplanWithHint(&organizer.ReplanRequest{
			Files:            downloadStatus.FileList,
			Metadata:         downloadStatus.Metadata,
			PreviousResponse: downloadStatus.OrganizePlans,
			UserHint:         userHint,
		})
	} else {
		// Use regular Plan when user_hint is empty (keep current logic)
		resp, err = s.organizerClient.Plan(&organizer.PlanRequest{
			Dir:      downloadStatus.ID,
			Files:    downloadStatus.FileList,
			Metadata: downloadStatus.Metadata,
		})
	}

	if err != nil {
		// Update the state to CreatePlanFailed when re-planning fails
		downloadStatus.OrganizeState = db.CreatePlanFailed
		if saveErr := db.SaveDownloadStatus(s.db, downloadStatus); saveErr != nil {
			c.JSON(500, gin.H{"error": saveErr.Error()})
			return
		}
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Update the organize plan and state
	downloadStatus.OrganizePlans = resp
	downloadStatus.OrganizeState = db.Planed

	// Update the download status
	if err := db.SaveDownloadStatus(s.db, downloadStatus); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"status": "re_plan completed successfully",
		"plan":   resp,
	})
}
