package nyaa

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/nyaa/prefetcheddata"
	"github.com/autoget-project/autoget/backend/internal/errors"
	"github.com/autoget-project/autoget/backend/internal/helpers"
	"github.com/autoget-project/autoget/backend/internal/notify"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

var (
	_ indexers.IIndexer = (*Client)(nil)

	logger = log.With().Str("indexer", "nyaa").Logger()
)

const (
	defaultBaseURL  = "https://nyaa.si/"
	defaultPageSize = 75
)

type Config struct {
	BaseURL    string `yaml:"base_url"`
	UseProxy   bool   `yaml:"use_proxy"`
	Downloader string `yaml:"downloader"`

	proxyURL string
}

func (c *Config) SetProxyURL(proxyURL string) {
	c.proxyURL = proxyURL
}

func (c *Config) getProxyURL() string {
	if c.proxyURL != "" {
		return c.proxyURL
	}
	return os.Getenv("HTTP_PROXY")
}

type Client struct {
	indexers.IndexerBasicInfo

	config      *Config
	torrentsDir string
	db          *gorm.DB
	notify      notify.INotifier

	httpClient *http.Client

	DefaultBaseURL string
	CategoriesMap  map[string]indexers.Category
	CategoriesList []indexers.Category
}

func (c *Client) getBaseURL() string {
	if c.config.BaseURL == "" {
		return c.DefaultBaseURL
	}
	return c.config.BaseURL
}

func NewClient(config *Config, torrentsDir string, db *gorm.DB, notify notify.INotifier) *Client {
	c := &Client{
		IndexerBasicInfo: *indexers.NewIndexerBasicInfo("nyaa", config.Downloader, false),
		config:           config,
		torrentsDir:      torrentsDir,
		db:               db,
		notify:           notify,
		httpClient:       http.DefaultClient,
		DefaultBaseURL:   defaultBaseURL,
		CategoriesMap:    prefetcheddata.Categories,
		CategoriesList:   prefetcheddata.CategoriesList,
	}

	if config.UseProxy {
		proxyURL := config.getProxyURL()
		if proxyURL != "" {
			proxy, err := url.Parse(proxyURL)
			if err != nil {
				logger.Fatal().Err(err).Msg("failed to parse proxy URL")
			}
			c.httpClient.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxy),
			}
		}
	}

	return c
}

// Name of the indexer.
func (c *Client) Name() string {
	return c.Name_
}

// Categories returns indexer's resource categories.
func (c *Client) Categories() ([]indexers.Category, *errors.HTTPStatusError) {
	return c.CategoriesList, nil
}

// List resources in given category and keyword (optional).
func (c *Client) List(req *indexers.ListRequest) (*indexers.ListResult, *errors.HTTPStatusError) {
	// Nyaa only support following query params
	q := url.Values{}
	if req.Category != "" {
		q.Set("c", req.Category)
	}
	if req.Keyword != "" {
		q.Set("q", req.Keyword)
	}
	if req.Page > 0 {
		q.Set("p", strconv.Itoa(int(req.Page)))
	}

	u, err := url.Parse(c.getBaseURL())
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to join path: %v", err))
	}

	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to fetch list page: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to read response body: %v", err))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to parse HTML: %v", err))
	}

	var resources []indexers.ListResourceItem
	doc.Find("table.torrent-list tbody tr").Each(func(i int, s *goquery.Selection) {
		item := indexers.ListResourceItem{
			Free: true,
		}

		// Column 1: Category
		categoryLink := s.Find("td:nth-child(1) a").AttrOr("href", "")
		if categoryLink != "" {
			item.Category = c.CategoriesMap[strings.TrimPrefix(categoryLink, "/?c=")].Name
		}

		// Column 2: Title and ID
		titleLink := s.Find("td:nth-child(2) a").Last()
		item.Title = strings.TrimSpace(titleLink.Text())
		idLink, exists := titleLink.Attr("href")
		if exists {
			item.ID = strings.TrimPrefix(idLink, "/view/")
		}

		// Column 4: Size
		item.Size, _ = humanSizeToBytes(s.Find("td:nth-child(4)").Text())

		// Column 5: CreatedDate
		timestampStr, exists := s.Find("td:nth-child(5)").Attr("data-timestamp")
		if exists {
			timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
			if err == nil {
				item.CreatedDate = timestamp
			}
		}

		// Column 6: Seeders
		seedersStr := s.Find("td:nth-child(6)").Text()
		seeders, err := strconv.ParseUint(seedersStr, 10, 32)
		if err == nil {
			item.Seeders = uint32(seeders)
		}

		// Column 7: Leechers
		leechersStr := s.Find("td:nth-child(7)").Text()
		leechers, err := strconv.ParseUint(leechersStr, 10, 32)
		if err == nil {
			item.Leechers = uint32(leechers)
		}

		resources = append(resources, item)
	})

	// Pagination
	page := doc.Find("ul.pagination li.active").First().Text()
	page = strings.Replace(page, "(current)", "", -1) // Remove "(current)"
	page = strings.TrimSpace(page)                    // Trim leading/trailing whitespace
	currentPage, _ := strconv.Atoi(page)

	var totalPages int
	// The last page number is usually in the second to last `li` element.
	lastPageLi := doc.Find("ul.pagination li").Eq(-2)
	if lastPageLi.Length() > 0 {
		totalPages, _ = strconv.Atoi(lastPageLi.Text())
	}

	if currentPage == 0 && len(resources) > 0 {
		currentPage = 1
	}

	if totalPages == 0 && len(resources) > 0 {
		// if no total pages, but we have resources, there must be at least one page
		totalPages = 1
	}

	if totalPages < currentPage {
		// if total pages is less than current page, it means we failed to parse it
		// (e.g. `...`), so we just assume total pages is at least current page
		totalPages = currentPage
	}

	listResult := &indexers.ListResult{
		Pagination: indexers.Pagination{
			Page:       uint32(currentPage),
			TotalPages: uint32(totalPages),
			PageSize:   defaultPageSize,
			Total:      0, // Nyaa does not provide total items
		},
		Resources: resources,
	}

	return listResult, nil
}

// Detail of a resource.
func (c *Client) Detail(id string, fileList bool) (*indexers.ResourceDetail, *errors.HTTPStatusError) {
	url, err := url.JoinPath(c.getBaseURL(), "view", id)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to join path: %v", err))
	}

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to fetch detail page: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.NewHTTPStatusError(resp.StatusCode, fmt.Sprintf("failed to fetch detail page, status code: %d", resp.StatusCode))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to read response body: %v", err))
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to parse HTML: %v", err))
	}

	detail := &indexers.ResourceDetail{
		ListResourceItem: indexers.ListResourceItem{
			ID: id,
		},
	}

	// First panel include Title, Category, Size, CreatedDate
	firstPanel := doc.Find(".panel-default").First()

	// Extract Title
	detail.Title = strings.TrimSpace(firstPanel.Find(".panel-title").First().Text())

	firstPanelBody := firstPanel.Find(".panel-body").First()

	// firstPanelBody looks like a map:
	// key in .col-md-1 and value in .col-md-5
	keys := []string{}
	firstPanelBody.Find(".col-md-1").Each(func(i int, s *goquery.Selection) {
		key := s.Text()
		keys = append(keys, key)
	})

	firstPanelBody.Find(".col-md-5").Each(func(i int, s *goquery.Selection) {
		if i >= len(keys) {
			return
		}
		key := keys[i]
		switch key {
		case "Category:":
			{
				links := s.Find("a")
				href, exists := links.Last().Attr("href")
				if exists {
					detail.Category = c.CategoriesMap[strings.TrimPrefix(href, "/?c=")].Name
				}
			}
		case "File size:":
			{
				sizeStr := s.Text()
				detail.Size, err = humanSizeToBytes(sizeStr)
				if err != nil {
					logger.Info().Err(err).Msgf("humanSizeToBytes() invalid size string: %s", sizeStr)
				}
			}
		case "Seeders:":
			{
				seedersStr := s.Text()
				seeders, err := strconv.ParseUint(seedersStr, 10, 32)
				if err == nil {
					detail.Seeders = uint32(seeders)
				}
			}
		case "Leechers:":
			{
				leechersStr := s.Text()
				leechers, err := strconv.ParseUint(leechersStr, 10, 32)
				if err == nil {
					detail.Leechers = uint32(leechers)
				}
			}
		}
	})

	// Extract CreatedDate
	timestampStr, exists := doc.Find("div[data-timestamp]").First().Attr("data-timestamp")
	if exists {
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err == nil {
			detail.CreatedDate = timestamp
		}
	}

	// Extract Description
	detail.Description = doc.Find("#torrent-description").Text()

	// Extract Files
	if fileList {
		// Start parsing from the root of the file list
		doc.Find(".torrent-file-list > ul").Each(func(i int, s *goquery.Selection) {
			parseFileList(s, "", &detail.Files)
		})
	}

	return detail, nil
}

// parseFileList recursively parses the <ul> and <li> elements to build file paths.
func parseFileList(s *goquery.Selection, currentPath string, fileList *[]indexers.File) {
	s.ChildrenFiltered("li").Each(func(i int, li *goquery.Selection) {
		// Check if it's a folder (has an <a> tag with class="folder")
		folderLink := li.ChildrenFiltered("a.folder")
		if folderLink.Length() > 0 {
			folderName := strings.TrimSpace(folderLink.Text())

			newPath := currentPath
			if folderName != "" { // Only append if it's a non-empty folder name
				newPath = filepath.Join(currentPath, folderName)
			}

			// Recursively parse the nested <ul>
			nestedUl := li.ChildrenFiltered("ul")
			if nestedUl.Length() > 0 {
				parseFileList(nestedUl, newPath, fileList)
			}
		} else {
			// It's a file (has an <i> tag with class="fa-file")
			fileIcon := li.Find("i.fa-file")
			if fileIcon.Length() > 0 {
				// Get the text directly after the <i> tag (the file name)
				fileName := strings.TrimSpace(li.Contents().Not("i, span.file-size").Text())
				// Get the file size
				fileSize := li.Find("span.file-size").Text()
				fileSize = strings.Trim(fileSize, "()")
				size, err := humanSizeToBytes(fileSize)
				if err != nil {
					logger.Info().Err(err).Msgf("humanSizeToBytes() invalid size string: %s", fileSize)
				}

				fullPath := filepath.Join(currentPath, fileName)

				*fileList = append(*fileList, indexers.File{
					Name: fullPath,
					Size: size,
				})
			}
		}
	})
}

// Download the torrent file to given dir or return the magnet link.
func (c *Client) Download(id string) (*indexers.DownloadResult, *errors.HTTPStatusError) {
	fileName := fmt.Sprintf("%s.torrent", id)

	url, err := url.JoinPath(c.getBaseURL(), "download", fileName)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, fmt.Sprintf("failed to join path: %v", err))
	}

	meta, _, err := helpers.DownloadTorrentFileFromURL(c.httpClient, url, filepath.Join(c.torrentsDir, fileName))
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, err.Error())
	}

	return &indexers.DownloadResult{
		TorrentFilePath: filepath.Join(c.torrentsDir, fileName),
		TorrentHash:     meta.HashInfoBytes().HexString(),
	}, nil
}

func humanSizeToBytes(sizeStr string) (uint64, error) {
	if sizeStr == "" {
		return 0, nil
	}
	parts := strings.Split(sizeStr, " ")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}
	value, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size value: %s", parts[0])
	}
	unit := parts[1]
	var multiplier float64
	switch unit {
	case "B":
		multiplier = 1
	case "KiB":
		multiplier = 1024
	case "MiB":
		multiplier = 1024 * 1024
	case "GiB":
		multiplier = 1024 * 1024 * 1024
	case "TiB":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		multiplier = 1 // fallback
	}
	return uint64(value * multiplier), nil
}
