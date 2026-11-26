package rsshelper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/db/dbtest"
	"github.com/autoget-project/autoget/backend/internal/errors"
)

func TestRenderRSSResult(t *testing.T) {
	tests := []struct {
		name                   string
		indexer                string
		downloadStarted        []string
		downloadPendingToStart []string
		expectedSubstrings     []string
		notExpectedSubstrings  []string
	}{
		{
			name:    "Both lists populated",
			indexer: "TestIndexer",
			downloadStarted: []string{
				"Item A",
				"Item B",
			},
			downloadPendingToStart: []string{
				"Item C",
				"Item D",
			},
			expectedSubstrings: []string{
				"# TestIndexer RSS",
				"## Download Started",
				"- Item A",
				"- Item B",
				"## Download Pending to Start",
				"- Item C",
				"- Item D",
			},
			notExpectedSubstrings: []string{},
		},
		{
			name:            "DownloadStarted empty",
			indexer:         "TestIndexer",
			downloadStarted: []string{},
			downloadPendingToStart: []string{
				"Item C",
				"Item D",
			},
			expectedSubstrings: []string{
				"# TestIndexer RSS",
				"## Download Pending to Start",
				"- Item C",
				"- Item D",
			},
			notExpectedSubstrings: []string{
				"## Download Started",
			},
		},
		{
			name:    "DownloadPendingToStart empty",
			indexer: "TestIndexer",
			downloadStarted: []string{
				"Item A",
				"Item B",
			},
			downloadPendingToStart: []string{},
			expectedSubstrings: []string{
				"# TestIndexer RSS",
				"## Download Started",
				"- Item A",
				"- Item B",
			},
			notExpectedSubstrings: []string{
				"## Download Pending to Start",
			},
		},
		{
			name:                   "Both lists empty",
			indexer:                "TestIndexer",
			downloadStarted:        []string{},
			downloadPendingToStart: []string{},
			expectedSubstrings: []string{
				"# TestIndexer RSS",
			},
			notExpectedSubstrings: []string{
				"## Download Started",
				"## Download Pending to Start",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := RenderRSSResult(tt.indexer, tt.downloadStarted, tt.downloadPendingToStart)
			if err != nil {
				t.Fatalf("RenderRSSResult returned an error: %v", err)
			}

			for _, expected := range tt.expectedSubstrings {
				if !strings.Contains(result, expected) {
					t.Errorf("Expected to find substring:\n%q\nBut got:\n%q", expected, result)
				}
			}

			for _, notExpected := range tt.notExpectedSubstrings {
				if strings.Contains(result, notExpected) {
					t.Errorf("Expected NOT to find substring:\n%q\nBut found:\n%q", notExpected, result)
				}
			}
		})
	}
}

type fakeIndexer struct {
	indexers.IIndexer
	downloadErr *errors.HTTPStatusError
	downloads   int
}

func (f *fakeIndexer) Name() string           { return "fake" }
func (f *fakeIndexer) DownloaderName() string { return "dl" }
func (f *fakeIndexer) Detail(id string, _ bool) (*indexers.ResourceDetail, *errors.HTTPStatusError) {
	return &indexers.ResourceDetail{Title: "T " + id}, nil
}
func (f *fakeIndexer) Download(id string, before indexers.BeforePlaceFunc) (*indexers.DownloadResult, *errors.HTTPStatusError) {
	f.downloads++
	if f.downloadErr != nil {
		return nil, f.downloadErr
	}
	if err := before("hash-" + id); err != nil {
		return nil, errors.NewHTTPStatusError(409, err.Error())
	}
	return &indexers.DownloadResult{TorrentHash: "hash-" + id}, nil
}

type fakeNotifier struct{ msgs []string }

func (n *fakeNotifier) SendMessage(m string) error         { n.msgs = append(n.msgs, m); return nil }
func (n *fakeNotifier) SendMarkdownMessage(m string) error { n.msgs = append(n.msgs, m); return nil }

func setupRSS(t *testing.T, action string) *gorm.DB {
	t.Helper()
	d, err := dbtest.SqliteForTest()
	require.NoError(t, err)
	require.NoError(t, db.AddSearch(d, &db.RSSSearch{Indexer: "fake", Text: "foo", Action: action}))
	return d
}

func searches(t *testing.T, d *gorm.DB) []*db.RSSSearch {
	t.Helper()
	s, err := db.GetSearchsByIndexer(d, "fake")
	require.NoError(t, err)
	return s
}

var rssItems = []*indexers.RSSItem{{ResID: "1", Title: "Foo bar"}}

func TestSearchRSS_DownloadSuccess(t *testing.T) {
	d := setupRSS(t, "download")
	idx := &fakeIndexer{}
	SearchRSS(idx, d, &fakeNotifier{}, rssItems)

	st, err := db.GetDownloadStatus(d, "hash-1")
	require.NoError(t, err)
	assert.Equal(t, "fake", st.ResIndexer)
	assert.Empty(t, searches(t, d))
}

func TestSearchRSS_DownloadFailureRetries(t *testing.T) {
	d := setupRSS(t, "download")
	idx := &fakeIndexer{downloadErr: errors.NewHTTPStatusError(502, "boom")}
	SearchRSS(idx, d, &fakeNotifier{}, rssItems)

	s := searches(t, d)
	require.Len(t, s, 1)
	assert.Empty(t, s[0].ResID)
	_, err := db.GetDownloadStatus(d, "hash-1")
	assert.Error(t, err)

	SearchRSS(idx, d, &fakeNotifier{}, rssItems)
	assert.Equal(t, 2, idx.downloads)
}

func TestSearchRSS_ConflictConsumesSearch(t *testing.T) {
	d := setupRSS(t, "download")
	require.NoError(t, d.Create(&db.DownloadStatus{ID: "hash-1"}).Error)
	SearchRSS(&fakeIndexer{}, d, &fakeNotifier{}, rssItems)
	assert.Empty(t, searches(t, d))
}

func TestSearchRSS_NotificationStoresResID(t *testing.T) {
	d := setupRSS(t, "notification")
	idx := &fakeIndexer{}
	SearchRSS(idx, d, &fakeNotifier{}, rssItems)

	s := searches(t, d)
	require.Len(t, s, 1)
	assert.Equal(t, "1", s[0].ResID)
	assert.Equal(t, 0, idx.downloads)
}
