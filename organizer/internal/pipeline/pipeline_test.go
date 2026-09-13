package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/ai/mock"
	"github.com/autoget-project/autoget/organizer/internal/model"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
)

func newTestPipeline(t *testing.T, prov *mock.Provider, dir string) *Pipeline {
	t.Helper()
	return NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), dir, "tp-test-target", nil)
}

func TestCreatePlan_TVSeriesFullFlow(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()

	// Seed files: dirty video name, companion subtitle and garbage.
	subDir := filepath.Join(downloadDir, "show1")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	subtitleContent := "1\n00:00:01,000 --> 00:00:02,000\n你好世界\n"
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "show.chs.srt"), []byte(subtitleContent), 0o644))

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic naming","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response: `{"plan":[{"file":"[HDSky] 我的剧 S01E01.mkv","action":"move",
			"target":"tv_series/Others/我的剧 (2020)/Season 01/我的剧 (2020) S01E01.mkv"}]}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Response: `{"plan":[{"file":"show.chs.srt","action":"move",
			"matched_video":"[HDSky] 我的剧 S01E01.mkv","language":"Chinese"}]}`,
	})

	p := newTestPipeline(t, prov, downloadDir)

	resp, err := p.CreatePlan(context.Background(), "show1",
		[]string{"[HDSky] 我的剧 S01E01.mkv", "show.chs.srt", "cover.nfo"},
		map[string]interface{}{"title": "我的剧", "year": 2020.0})
	require.NoError(t, err)
	require.Nil(t, resp.Error, "normal planning must keep error null")

	actionsByFile := map[string]model.PlanAction{}
	for _, a := range resp.Plan {
		actionsByFile[a.File] = a
	}

	video := actionsByFile["[HDSky] 我的剧 S01E01.mkv"]
	require.NotNil(t, video.Target)
	assert.Equal(t, "move", video.Action)
	assert.Equal(t, "tv_series/Others/我的剧 (2020)/Season 01/我的剧 (2020) S01E01.mkv", *video.Target)

	sub := actionsByFile["show.chs.srt"]
	require.NotNil(t, sub.Target)
	assert.Equal(t, "move", sub.Action)
	assert.Equal(t, "tv_series/Others/我的剧 (2020)/Season 01/我的剧 (2020) S01E01.简体中文.chi.srt", *sub.Target)

	garbage := actionsByFile["cover.nfo"]
	assert.Equal(t, "skip", garbage.Action)
	assert.Nil(t, garbage.Target, "garbage must be skipped with null target")
}

func TestCreatePlan_SubtitlePairingFailureDegradesGracefully(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	subDir := filepath.Join(downloadDir, "movie1")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.srt"), []byte("hello"), 0o644))

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"movie","reason":"single feature","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes movie downloads",
		Response: `{"plan":[{"file":"movie.mkv","action":"move",
			"target":"movie/Others/电影 (2000)/电影 (2000).mkv"}]}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Error:         errors.New("subtitle pairing offline"),
	})

	p := newTestPipeline(t, prov, downloadDir)

	resp, err := p.CreatePlan(context.Background(), "movie1",
		[]string{"movie.mkv", "sub.srt"},
		map[string]interface{}{"title": "电影", "year": 2000.0})
	require.NoError(t, err, "subtitle pairing failure must degrade, not fail")
	require.Nil(t, resp.Error, "error must stay null on degradation")

	videoFound, subFound := false, false
	for _, a := range resp.Plan {
		if a.File == "movie.mkv" && a.Action == "move" {
			videoFound = true
		}
		if a.File == "sub.srt" {
			subFound = true
		}
	}
	assert.True(t, videoFound, "video plan must survive subtitle failure")
	assert.False(t, subFound, "subtitle must be left unplanned after pairing failure")
}

func TestCreatePlan_UnknownCategoryEmptyPlan(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"unknown","reason":"random junk","entities":{}}`,
	})

	p := newTestPipeline(t, prov, t.TempDir())

	resp, err := p.CreatePlan(context.Background(), "junk", []string{"junk.bin"}, nil)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
	assert.Empty(t, resp.Plan, "unknown category must return an empty plan")
}

func TestCreatePlan_Stage3LLMFailureIsFatal(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic naming","entities":{}}`,
	})
	// No tv planner rule -> mock provider returns its default error.

	p := newTestPipeline(t, prov, t.TempDir())

	_, err := p.CreatePlan(context.Background(), "show", []string{"show/ep01.mkv"}, nil)
	assert.Error(t, err, "stage 3 LLM failure must surface as a fatal error")
}
