package mteam

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/indexers/rsshelper"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/autoget-project/autoget/backend/internal/db/dbtest"
)

// The fixtures are hand-written from the response structs in this package,
// with no real ids, tokens or urls; they exercise the parsing paths offline.
var (
	//go:embed test_data/search.json
	searchFixture []byte
	//go:embed test_data/detail.json
	detailFixture []byte
	//go:embed test_data/files.json
	filesFixture []byte
)

type fixtureServer struct {
	*httptest.Server

	mu         sync.Mutex
	searchBody searchRequest
	apiKeys    []string
	torrent    []byte
}

func newFixtureServer(t *testing.T) *fixtureServer {
	t.Helper()

	// A minimal private torrent built in-test, so no binary fixture is needed.
	private := true
	info := metainfo.Info{
		Name:        "fixture",
		PieceLength: 16384,
		Length:      16384,
		Pieces:      make([]byte, 20),
		Private:     &private,
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))

	fs := &fixtureServer{torrent: torrent.Bytes()}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/torrent/search", func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		fs.apiKeys = append(fs.apiKeys, r.Header.Get("x-api-key"))
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&fs.searchBody))
		_, _ = w.Write(searchFixture)
	})
	mux.HandleFunc("/api/torrent/detail", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(detailFixture)
	})
	mux.HandleFunc("/api/torrent/files", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(filesFixture)
	})
	mux.HandleFunc("/api/torrent/genDlToken", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(genDownloadLinkResponse{Code: "0", Message: "SUCCESS", Data: fs.URL + "/dl"})
	})
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fs.torrent)
	})
	mux.HandleFunc("/rss", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rssResp))
	})

	fs.Server = httptest.NewServer(mux)
	t.Cleanup(fs.Close)
	return fs
}

func TestListFixture(t *testing.T) {
	fs := newFixtureServer(t)
	m := NewMTeam(&Config{BaseURL: fs.URL, APIKey: "k"}, MTeamTypeNormal, "", nil, nil)
	require.NotNil(t, m)

	got, err := m.List(&indexers.ListRequest{
		Category:  "401",
		Keyword:   "fixture",
		Page:      1,
		PageSize:  10,
		Free:      true,
		Standards: []string{indexers.Resolution4K, indexers.Resolution1080p, "bogus"},
	})
	require.Nil(t, err)

	fs.mu.Lock()
	assert.Equal(t, "FREE", fs.searchBody.Discount)
	assert.Equal(t, []string{"6", "1"}, fs.searchBody.Standards)
	assert.Equal(t, "fixture", fs.searchBody.Keyword)
	assert.Equal(t, []string{"401"}, fs.searchBody.Categories)
	assert.Equal(t, []string{"k"}, fs.apiKeys)
	fs.mu.Unlock()

	assert.Equal(t, indexers.Pagination{Page: 1, PageSize: 10, Total: 4, TotalPages: 1}, got.Pagination)
	require.Len(t, got.Resources, 4)

	first := got.Resources[0]
	assert.Equal(t, "100001", first.ID)
	assert.Equal(t, "Fixture Movie 2025 2160p", first.Title)
	assert.Equal(t, "Fixture subtitle one", first.Title2)
	assert.Equal(t, "电影/SD", first.Category)
	assert.Equal(t, uint64(8000000000), first.Size)
	assert.Equal(t, indexers.Resolution4K, first.Resolution)
	assert.Equal(t, uint32(12), first.Seeders)
	assert.Equal(t, uint32(3), first.Leechers)
	assert.True(t, first.Free)
	assert.Equal(t, []string{"中字"}, first.Labels)
	assert.Equal(t, "https://kp.m-team.cc/detail/100001", first.DetailsURL)
	assert.Equal(t, []string{"/api/v1/image?url=" + url.QueryEscape("https://img.m-team.cc/images/2025/01/01/fixture1.jpg")}, first.Images)
	assert.Equal(t, int64(1735689600), first.CreatedDate) // 2025-01-01 08:00 Asia/Shanghai
	require.Len(t, first.DBs, 2)
	assert.Equal(t, "douban", first.DBs[0].DB)
	assert.Equal(t, "imdb", first.DBs[1].DB)

	second := got.Resources[1]
	assert.True(t, second.Free, "ongoing mall single free counts as free")
	assert.Equal(t, indexers.Resolution1080p, second.Resolution)
	assert.Equal(t, []string{"https://example.com/other.jpg"}, second.Images)

	third := got.Resources[2]
	assert.False(t, third.Free)
	assert.Equal(t, indexers.Resolution720p, third.Resolution)
	assert.Empty(t, third.Images)
}

func TestListFixtureExcludeGayContent(t *testing.T) {
	fs := newFixtureServer(t)
	m := NewMTeam(&Config{BaseURL: fs.URL, APIKey: "k", ExcludeGayContent: true}, MTeamTypeNormal, "", nil, nil)
	require.NotNil(t, m)

	got, err := m.List(&indexers.ListRequest{Category: "401", Page: 1, PageSize: 10})
	require.Nil(t, err)
	require.Len(t, got.Resources, 3)
	for _, r := range got.Resources {
		assert.NotEqual(t, "100004", r.ID)
	}
}

func TestDetailFixture(t *testing.T) {
	fs := newFixtureServer(t)
	m := NewMTeam(&Config{BaseURL: fs.URL, APIKey: "k"}, MTeamTypeNormal, "", nil, nil)
	require.NotNil(t, m)

	got, err := m.Detail("100001", true)
	require.Nil(t, err)
	assert.Equal(t, "100001", got.ID)
	assert.Equal(t, "Fixture Movie 2025 2160p", got.Title)
	assert.Equal(t, indexers.Resolution4K, got.Resolution)
	assert.Equal(t, "General\nFormat : Matroska", got.Mediainfo)
	assert.Equal(t, "[b]Fixture[/b] description", got.Description)
	assert.Equal(t, []indexers.File{
		{Name: "Fixture.Movie.2025.mkv", Size: 7999000000},
		{Name: "Fixture.Movie.2025.srt", Size: 1000000},
	}, got.Files)
	assert.Equal(t, "tt1000001", got.Metadata["imdb_id"])

	noFiles, err := m.Detail("100001", false)
	require.Nil(t, err)
	assert.Empty(t, noFiles.Files)

	_, err = m.Detail("not-a-number", true)
	require.NotNil(t, err)
	assert.Equal(t, http.StatusBadRequest, err.Code)
}

func TestDownloadFixture(t *testing.T) {
	fs := newFixtureServer(t)
	dir := t.TempDir()
	d, err := dbtest.SqliteForTest()
	require.NoError(t, err)
	m := NewMTeam(&Config{BaseURL: fs.URL, APIKey: "k"}, MTeamTypeNormal, dir, d, nil)
	require.NotNil(t, m)

	res, herr := m.Download("100001", nil)
	require.Nil(t, herr)
	assert.Equal(t, filepath.Join(dir, "m-team.100001.torrent"), res.TorrentFilePath)
	assert.FileExists(t, res.TorrentFilePath)

	mi, err := metainfo.LoadFromFile(res.TorrentFilePath)
	require.NoError(t, err)
	assert.Equal(t, mi.HashInfoBytes().HexString(), res.TorrentHash)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	assert.True(t, *info.Private)

	// A second download of the same torrent is a conflict.
	require.NoError(t, d.Create(&db.DownloadStatus{ID: res.TorrentHash}).Error)
	_, herr = m.Download("100001", nil)
	require.NotNil(t, herr)
	assert.Equal(t, http.StatusConflict, herr.Code)
}

type recordingNotifier struct{ msgs []string }

func (n *recordingNotifier) SendMessage(msg string) error {
	n.msgs = append(n.msgs, msg)
	return nil
}

func TestRSSFixture(t *testing.T) {
	fs := newFixtureServer(t)
	dir := t.TempDir()
	d, err := dbtest.SqliteForTest()
	require.NoError(t, err)
	notifier := &recordingNotifier{}
	m := NewMTeam(&Config{BaseURL: fs.URL, APIKey: "k", RSS: fs.URL + "/rss", Downloader: "dl"}, MTeamTypeNormal, dir, d, notifier)
	require.NotNil(t, m)

	dlSearch := &db.RSSSearch{Indexer: m.Name(), Text: "Match Search 1", Action: "download"}
	require.NoError(t, db.AddSearch(d, dlSearch))
	notifySearch := &db.RSSSearch{Indexer: m.Name(), Text: "Match Search 1", Action: "notification"}
	require.NoError(t, db.AddSearch(d, notifySearch))

	items, err := m.pullRSS()
	require.NoError(t, err)
	require.Len(t, items, 2)

	rsshelper.SearchRSS(m, d, notifier, items)

	// The download search is consumed and a tracked download exists.
	var count int64
	require.NoError(t, d.Model(&db.DownloadStatus{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var dlAfter db.RSSSearch
	assert.ErrorContains(t, d.First(&dlAfter, dlSearch.ID).Error, "record not found")

	// The notification search keeps the matched resource.
	var notifyAfter db.RSSSearch
	require.NoError(t, d.First(&notifyAfter, notifySearch.ID).Error)
	assert.Equal(t, "111111", notifyAfter.ResID)
	assert.Equal(t, "Match Search 1", notifyAfter.Title)

	require.Len(t, notifier.msgs, 1)
	assert.Contains(t, notifier.msgs[0], "m-team RSS")
	assert.Contains(t, notifier.msgs[0], "Download started:\n- Match Search 1")
	assert.Contains(t, notifier.msgs[0], "Download pending to start:\n- Match Search 1")
}
