package mteam

import (
	"os"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	_ indexers.IIndexer = (*MTeam)(nil)

	apiKey = os.Getenv("MTEAM_API_KEY")
)

func TestCategories(t *testing.T) {
	if apiKey == "" {
		t.Skip("MTEAM_API_KEY not set")
	}

	m := NewMTeam(&Config{
		APIKey: apiKey,
	}, MTeamTypeNormal, "", nil, nil)
	require.NotNil(t, m)

	got, err := m.Categories()
	require.Nil(t, err)
	assert.Len(t, got, 1)
}

func TestList(t *testing.T) {
	if apiKey == "" {
		t.Skip("MTEAM_API_KEY not set")
	}

	m := NewMTeam(&Config{
		APIKey: apiKey,
	}, MTeamTypeNormal, "", nil, nil)
	require.NotNil(t, m)

	tests := []struct {
		name      string
		category  string
		keyword   string
		page      uint32
		pageSize  uint32
		free      bool
		standards []string
	}{
		{
			name:     "normal root",
			category: "normal",
			keyword:  "",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "adult root",
			category: "adult",
			keyword:  "",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "normal sub",
			category: "434",
			keyword:  "",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "adult sub",
			category: "433",
			keyword:  "",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "adult mid level",
			category: "115",
			keyword:  "",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "normal with keyword",
			category: "normal",
			keyword:  "地狱",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "adult with keyword",
			category: "adult",
			keyword:  "sone",
			page:     1,
			pageSize: 2,
		},
		{
			name:     "normal with standards",
			category: "adult",
			page:     1,
			pageSize: 2,
			standards: []string{
				indexers.Resolution8K,
				indexers.Resolution4K,
				indexers.Resolution1080p,
				indexers.Resolution1080i,
				indexers.Resolution720p,
				indexers.ResolutionSD,
			},
		},
		{
			name:     "normal with free",
			category: "adult",
			page:     1,
			pageSize: 2,
			free:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := m.List(&indexers.ListRequest{
				Category:  tc.category,
				Keyword:   tc.keyword,
				Page:      tc.page,
				PageSize:  tc.pageSize,
				Free:      tc.free,
				Standards: tc.standards,
			})
			require.Nil(t, err)
			assert.NotEmpty(t, got.Resources)
		})
	}
}

func TestDetail(t *testing.T) {
	if apiKey == "" {
		t.Skip("MTEAM_API_KEY not set")
	}

	m := NewMTeam(&Config{
		APIKey: apiKey,
	}, MTeamTypeNormal, "", nil, nil)
	require.NotNil(t, m)

	res, err := m.Detail("913855", true)
	require.Nil(t, err)
	assert.NotNil(t, res)
}

func TestDownload(t *testing.T) {
	if apiKey == "" {
		t.Skip("MTEAM_API_KEY not set")
	}

	dir := t.TempDir()
	d, err := db.SqliteForTest()
	require.NoError(t, err)
	m := NewMTeam(&Config{
		APIKey: apiKey,
	}, MTeamTypeNormal, dir, d, nil)
	require.NotNil(t, m)

	res, err := m.Download("913855")
	require.Nil(t, err)

	assert.NotEmpty(t, res.TorrentFilePath)
	assert.FileExists(t, res.TorrentFilePath)
	assert.NotEmpty(t, res.TorrentHash)

	mi, er := metainfo.LoadFromFile(res.TorrentFilePath)
	require.NoError(t, er)
	info, er := mi.UnmarshalInfo()
	require.NoError(t, er)
	assert.True(t, *info.Private)
}
