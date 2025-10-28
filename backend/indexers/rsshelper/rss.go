package rsshelper

import (
	"bytes"
	_ "embed"
	"strings"
	"text/template"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/notify"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

//go:embed rss.md
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
				search.ResID = item.ResID
				search.Catergory = item.Catergory

				err = db.UpdateSearch(d, search)
				if err != nil {
					logger.Error().Err(err).Msg("Failed to update search")
					continue
				}

				if search.Action == "download" {
					_, err := index.Download(search.ResID)
					if err != nil {
						logger.Error().Err(err).Msg("Failed to download torrent")
						continue
					}

					if err := db.DeleteSearch(d, search.ID); err != nil {
						logger.Error().Err(err).Msg("Failed to delete search")
						continue
					}

					downloadStarted = append(downloadStarted, search.Title)
				} else if search.Action == "notification" {
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
		if err := notify.SendMarkdownMessage(msg); err != nil {
			logger.Error().Err(err).Msg("Failed to send RSS notification")
		}
	}
}
