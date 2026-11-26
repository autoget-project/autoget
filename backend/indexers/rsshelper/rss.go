package rsshelper

import (
	"bytes"
	_ "embed"
	"net/http"
	"strings"
	"text/template"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/notify"
)

//go:embed rss.txt
var rssTemplateContent string

var rssTemplate *template.Template

func init() {
	var err error
	rssTemplate, err = template.New("rss").Parse(rssTemplateContent)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse RSS template")
	}
}

type RSSResultTemplateData struct {
	Indexer                string
	DownloadStarted        []string
	DownloadPendingToStart []string
}

func RenderRSSResult(indexer string, downloadStarted []string, downloadPendingToStart []string) (string, error) {
	data := RSSResultTemplateData{
		Indexer:                indexer,
		DownloadStarted:        downloadStarted,
		DownloadPendingToStart: downloadPendingToStart,
	}

	var buf bytes.Buffer
	err := rssTemplate.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

var (
	logger = log.With().Str("module", "rsshelper").Logger()
)

func SearchRSS(index indexers.IIndexer, d *gorm.DB, notify notify.INotifier, items []*indexers.RSSItem) {
	searchs, err := db.GetSearchsByIndexer(d, index.Name())
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get searchs from database")
		return
	}

	downloadStarted := []string{}
	downloadPendingToStart := []string{}

	for _, item := range items {
		for _, search := range searchs {
			if search.ResID != "" {
				continue
			}
			if strings.Contains(strings.ToLower(item.Title), search.Text) {
				search.Title = item.Title
				search.URL = item.URL
				search.Catergory = item.Catergory

				if search.Action == "download" {
					if err := indexers.DownloadTracked(d, index, item.ResID); err != nil && err.Code != http.StatusConflict {
						logger.Error().Err(err).Msg("Failed to download torrent")
						continue
					}
					// Downloaded, or already tracked (409): consume the search. Mark it in
					// memory so later items in this batch skip it.
					search.ResID = item.ResID

					if err := db.DeleteSearch(d, search.ID); err != nil {
						logger.Error().Err(err).Msg("Failed to delete search")
						continue
					}

					downloadStarted = append(downloadStarted, search.Title)
				} else if search.Action == "notification" {
					search.ResID = item.ResID
					if err := db.UpdateSearch(d, search); err != nil {
						logger.Error().Err(err).Msg("Failed to update search")
						continue
					}
					downloadPendingToStart = append(downloadPendingToStart, search.Title)
				}
			}
		}
	}

	if len(downloadStarted) > 0 || len(downloadPendingToStart) > 0 {
		msg, err := RenderRSSResult(index.Name(), downloadStarted, downloadPendingToStart)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to render RSS result")
			return
		}
		if err := notify.SendMessage(msg); err != nil {
			logger.Error().Err(err).Msg("Failed to send RSS notification")
		}
	}
}
