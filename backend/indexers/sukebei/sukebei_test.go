package sukebei

import (
	_ "embed"
	"testing"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCategories(t *testing.T) {
	n := NewClient(&nyaa.Config{UseProxy: true}, "", nil, nil)
	got, err := n.Categories()
	require.Nil(t, err)
	assert.NotEmpty(t, got)
	assert.Equal(t, "Real Life - Videos", got[9].Name)
}

func TestList(t *testing.T) {
	n := NewClient(&nyaa.Config{UseProxy: true}, "", nil, nil)

	tests := []struct {
		name     string
		req      *indexers.ListRequest
		wantPage uint32
	}{
		{
			name:     "Default",
			req:      &indexers.ListRequest{},
			wantPage: 1,
		},
		{
			name:     "Category",
			req:      &indexers.ListRequest{Category: "1_1"},
			wantPage: 1,
		},
		{
			name:     "Keyword",
			req:      &indexers.ListRequest{Keyword: "中文字幕"},
			wantPage: 1,
		},
		{
			name:     "Page",
			req:      &indexers.ListRequest{Page: 2},
			wantPage: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := n.List(tt.req)
			require.Nil(t, err)
			assert.Equal(t, tt.wantPage, got.Pagination.Page)
			assert.NotEmpty(t, got.Resources)

			// test on the first item
			firstItem := got.Resources[0]
			assert.NotEmpty(t, firstItem.ID)
			assert.NotEmpty(t, firstItem.Title)
			assert.NotEmpty(t, firstItem.Category)
			assert.NotEmpty(t, firstItem.CreatedDate)
			assert.NotEmpty(t, firstItem.Size)
		})
	}
}

func TestDownload(t *testing.T) {
	dir := t.TempDir()
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	n := NewClient(&nyaa.Config{UseProxy: true}, dir, d, nil)
	got, err := n.Download("4322631")
	require.Nil(t, err)
	assert.NotEmpty(t, got.TorrentFilePath)
	assert.FileExists(t, got.TorrentFilePath)
	assert.Equal(t, "540b136f03c15c003823d7b9869a0008f44b5d29", got.TorrentHash)
}

func TestDetail(t *testing.T) {
	n := NewClient(&nyaa.Config{UseProxy: true}, "", nil, nil)
	got, err := n.Detail("4322631", true)
	require.Nil(t, err)

	assert.Equal(t, "4322631", got.ID)
	assert.NotEmpty(t, got.Title)
	assert.NotEmpty(t, got.Category)
	assert.NotEmpty(t, got.CreatedDate)
	assert.NotEmpty(t, got.Size)
	assert.NotEmpty(t, got.Description)
	assert.NotEmpty(t, got.Files)
}

type fakeNotifier struct {
	message string
}

func (f *fakeNotifier) SendMessage(message string) error {
	f.message = message
	return nil
}

func (f *fakeNotifier) SendMarkdownMessage(message string) error {
	f.message = message
	return nil
}

var (
	//go:embed test_data/rss.xml
	rssResp string
)

func TestSearchRSS(t *testing.T) {
	dir := t.TempDir()
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	notifier := &fakeNotifier{}
	n := NewClient(&nyaa.Config{UseProxy: true}, dir, d, notifier)

	search1 := &db.RSSSearch{
		Indexer: "sukebei",
		Text:    "Match Search 1",
		Action:  "download",
	}
	db.AddSearch(d, search1)

	search2 := &db.RSSSearch{
		Indexer: "sukebei",
		Text:    "Match Search 2",
		Action:  "notification",
	}
	db.AddSearch(d, search2)

	fp := gofeed.NewParser()
	feed, err := fp.ParseString(rssResp)
	require.NoError(t, err)
	require.Len(t, feed.Items, 3)

	items := []*indexers.RSSItem{}
	for _, item := range feed.Items {
		items = append(items, n.ParseRSSItem(item))
	}

	n.SearchRSS(items)

	assert.Contains(t, notifier.message, "# sukebei RSS")
	assert.Contains(t, notifier.message, "## Download Started\n\n- Match Search 1")
	assert.Contains(t, notifier.message, "## Download Pending to Start\n\n- Match Search 2")

	search1After := &db.RSSSearch{}
	search1After.ID = search1.ID
	assert.ErrorIs(t, d.First(&search1After).Error, gorm.ErrRecordNotFound)

	search2After := &db.RSSSearch{}
	search2After.ID = search2.ID
	assert.NoError(t, d.First(&search2After).Error)
	assert.Equal(t, "4326217", search2After.ResID)
	assert.Equal(t, "Match Search 2", search2After.Title)
	assert.Equal(t, "Art - Pictures", search2After.Catergory)
	assert.Equal(t, "https://sukebei.nyaa.si/download/4326217.torrent", search2After.URL)
}
