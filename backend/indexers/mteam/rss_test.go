package mteam

import (
	_ "embed"
	"testing"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	//go:embed test_data/rss.xml
	rssResp string
)

func TestParseRSSItem(t *testing.T) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseString(rssResp)
	require.NoError(t, err)
	require.Len(t, feed.Items, 2)

	m := NewMTeam(&Config{
		APIKey: "api-key",
	}, MTeamTypeNormal, "", nil, nil)

	got := m.ParseRSSItem(feed.Items[0])

	want := &indexers.RSSItem{
		ResID:     "111111",
		Title:     "Match Search 1",
		Catergory: "AV(無碼)/HD Uncensored",
		URL:       "https://rss.m-team.cc/api/rss/dlv2?uid=111111",
	}

	assert.Equal(t, want, got)
}
