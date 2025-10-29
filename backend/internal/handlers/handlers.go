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
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Service struct {
	config *config.Config
	db     *gorm.DB

	indexers    map[string]indexers.IIndexer
	downloaders map[string]downloaders.IDownloader
}

func NewService(config *config.Config, db *gorm.DB, indexers map[string]indexers.IIndexer, downloaders map[string]downloaders.IDownloader) *Service {
	s := &Service{
		config:      config,
		db:          db,
		indexers:    indexers,
		downloaders: downloaders,
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
		c.JSON(500, gin.H{"error": err.Error()})
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

type listDownloadersRespItem struct {
	TorrentsDir string `json:"torrents_dir"`
	DownloadDir string `json:"download_dir"`
}

type listDownloadersResp struct {
	Map map[string]listDownloadersRespItem `json:"downloaders"`
}

func (s *Service) listDownloaders(c *gin.Context) {
	m := map[string]listDownloadersRespItem{}
	for name, dl := range s.downloaders {
		m[name] = listDownloadersRespItem{
			TorrentsDir: dl.TorrentsDir(),
			DownloadDir: dl.DownloadDir(),
		}
	}
	c.JSON(200, listDownloadersResp{Map: m})
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
