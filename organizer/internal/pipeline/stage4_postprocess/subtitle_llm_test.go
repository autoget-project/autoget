package stage4postprocess

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai/mock"
	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/ptr"
)

func TestReadSubtitlePreview_First30LinesAndPathJoin_L8(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()

	// L8: the preview must be read from DOWNLOAD_COMPLETED_DIR/{dir}/{file}.
	content := make([]string, 0, 40)
	for i := 1; i <= 40; i++ {
		content = append(content, "line-"+strings.Repeat("x", i%3)+"-"+string(rune('A'+i%26)))
	}
	full := filepath.Join(downloadDir, "movie1", "sub.srt")
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(strings.Join(content, "\n")), 0o644))

	preview := ReadSubtitlePreview(downloadDir, "movie1", "sub.srt")
	lines := strings.Split(preview, "\n")
	assert.Len(t, lines, SubtitlePreviewLines)
	assert.NotContains(t, preview, content[39], "preview must not contain lines beyond the first 30")

	// Missing files degrade to an error marker instead of crashing.
	missing := ReadSubtitlePreview(downloadDir, "movie1", "missing.srt")
	assert.Contains(t, missing, "Error reading file")
}

func TestSubtitleTargetPath_LanguageSuffixes(t *testing.T) {
	t.Parallel()

	videoTarget := "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.mkv"

	tests := []struct {
		name string
		lang model.Language
		want string
	}{
		{
			name: "japanese uses jpn",
			lang: model.LanguageJapanese,
			want: "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.日本語.jpn.srt",
		},
		{
			name: "chinese uses chi",
			lang: model.LanguageChinese,
			want: "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.简体中文.chi.srt",
		},
		{
			name: "english uses eng",
			lang: model.LanguageEnglish,
			want: "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.English.eng.srt",
		},
		{
			name: "unknown keeps plain base name",
			lang: model.LanguageOthers,
			want: "tv_series/Chinese/X (2020)/Season 01/X (2020) S01E01.srt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, SubtitleTargetPath(videoTarget, tt.lang, ".srt"))
		})
	}
}

func TestSubtitlePlanner_PairingWithJpnNaming(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	subDir := filepath.Join(downloadDir, "anime1")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	jpnSub := "1\n00:00:01,000 --> 00:00:02,000\nありがとう\n"
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "ep1.jp.srt"), []byte(jpnSub), 0o644))

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Response: `{"plan":[
			{"file":"ep1.jp.srt","action":"move","matched_video":"ep1.mkv","language":"Japanese"}
		]}`,
	})

	planner := NewSubtitlePlanner(prov, downloadDir)
	videoPlan := []model.PlanAction{
		{
			File:   "ep1.mkv",
			Action: "move",
			Target: ptr.Str("anim_tv_series/Japanese/某动画 (2023)/Season 01/某动画 (2023) S01E01.mkv"),
		},
	}

	actions, err := planner.PairSubtitles(context.Background(), "anime1", []string{"ep1.jp.srt"}, videoPlan)
	require.NoError(t, err)

	action := findAction(t, actions, "ep1.jp.srt")
	require.Equal(t, "move", action.Action)
	require.NotNil(t, action.Target)
	assert.Equal(t,
		"anim_tv_series/Japanese/某动画 (2023)/Season 01/某动画 (2023) S01E01.日本語.jpn.srt",
		*action.Target)

	// The prompt must embed the first-30-lines preview read via the dir join.
	calls := prov.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].Prompt, "ありがとう", "prompt must embed the subtitle content preview")
}

func TestSubtitlePlanner_UnmatchedAndUncoveredSkipped(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Response: `{"plan":[
			{"file":"d1/orphan.srt","action":"skip"},
			{"file":"d1/ghost.srt","action":"move","matched_video":"d1/not-in-plan.mkv","language":"Chinese"}
		]}`,
	})

	planner := NewSubtitlePlanner(prov, downloadDir)
	videoPlan := []model.PlanAction{
		{File: "d1/movie.mkv", Action: "move", Target: ptr.Str("movie/X (2000)/X (2000).mkv")},
	}

	actions, err := planner.PairSubtitles(context.Background(), "d1",
		[]string{"d1/orphan.srt", "d1/ghost.srt", "d1/uncovered.srt"}, videoPlan)
	require.NoError(t, err)

	orphan := findAction(t, actions, "d1/orphan.srt")
	assert.Equal(t, "skip", orphan.Action, "an unmatched subtitle must be skipped")

	ghost := findAction(t, actions, "d1/ghost.srt")
	assert.Equal(t, "skip", ghost.Action, "a matched video outside the plan must be skipped")

	uncovered := findAction(t, actions, "d1/uncovered.srt")
	assert.Equal(t, "skip", uncovered.Action, "an uncovered subtitle must be skipped")
}
