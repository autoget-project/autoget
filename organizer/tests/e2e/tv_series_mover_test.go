package e2e

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

// TestE2E_TVSeriesMover drives the TV series mover through the full pipeline:
// episodes land in the Season directory, the companion subtitle is renamed
// next to its matched video, and cover art / partial files are skipped.
func TestE2E_TVSeriesMover(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		s.seedDownloadFile(t, "dl", "My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP1.en.ass",
			"[Script Info]\nTitle: My Date with a Vampire EP1\n")

		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir: "dl",
			Files: []string{
				"My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP1.mkv",
				"My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP2.mkv",
				"My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP1.en.ass",
				"My.Date.with.a.Vampire.Season.02.2000/cover.jpg",
				"My.Date.with.a.Vampire.Season.02.2000/behind the scenes.mp4.part",
			},
			Metadata: map[string]interface{}{"organizer_category": "tv_series", "title": "我和僵尸有个约会", "year": 1998},
		})

		require.Equal(t, 200, code, body)
		var resp model.PlanResponse
		decodeBody(t, code, body, &resp)
		assert.Nil(t, resp.Error)
		require.NotEmpty(t, resp.Plan)

		planMap := make(map[string]model.PlanAction)
		for _, act := range resp.Plan {
			planMap[act.File] = act
		}

		// Episode 1 invariant: tv_series/, title, Season 02 / S02E01
		ep1, hasEp1 := planMap["My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP1.mkv"]
		require.True(t, hasEp1, "episode 1 should be planned")
		assert.Equal(t, "move", ep1.Action)
		require.NotNil(t, ep1.Target)
		assert.True(t, strings.HasPrefix(*ep1.Target, "tv_series/"))
		assert.Contains(t, *ep1.Target, "我和僵尸有个约会")
		assert.Contains(t, *ep1.Target, "Season 02")
		assert.Contains(t, *ep1.Target, "S02E01")
		assert.True(t, strings.HasSuffix(*ep1.Target, ".mkv"))

		// Episode 2 invariant: Season 02 / S02E02
		ep2, hasEp2 := planMap["My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP2.mkv"]
		require.True(t, hasEp2, "episode 2 should be planned")
		assert.Equal(t, "move", ep2.Action)
		require.NotNil(t, ep2.Target)
		assert.True(t, strings.HasPrefix(*ep2.Target, "tv_series/"))
		assert.Contains(t, *ep2.Target, "Season 02")
		assert.Contains(t, *ep2.Target, "S02E02")

		// Subtitle invariant: Season 02 / S02E01.*.ass with ISO language tag
		sub, hasSub := planMap["My.Date.with.a.Vampire.Season.02.2000/My.Date.with.a.Vampire.Season.02.2000.EP1.en.ass"]
		require.True(t, hasSub, "companion subtitle should be planned")
		assert.Equal(t, "move", sub.Action)
		require.NotNil(t, sub.Target)
		assert.True(t, strings.HasPrefix(*sub.Target, "tv_series/"))
		assert.Contains(t, *sub.Target, "Season 02")
		assert.Contains(t, *sub.Target, "S02E01")
		assert.Contains(t, *sub.Target, "eng")
		assert.True(t, strings.HasSuffix(*sub.Target, ".ass"))

		// Skips (if present in plan)
		if cover, ok := planMap["My.Date.with.a.Vampire.Season.02.2000/cover.jpg"]; ok {
			assert.Equal(t, "skip", cover.Action)
			assert.Nil(t, cover.Target)
		}
		if part, ok := planMap["My.Date.with.a.Vampire.Season.02.2000/behind the scenes.mp4.part"]; ok {
			assert.Equal(t, "skip", part.Action)
			assert.Nil(t, part.Target)
		}
	})
}

// TestE2E_AnimeEpisodeRoutingToAnimTVSeries covers unconventional episode
// labels plus animation routing (H4): a CJK-numbered anime episode with the
// "动画" genre flag routes to anim_tv_series as Season 01 / S01E03.
func TestE2E_AnimeEpisodeRoutingToAnimTVSeries(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		file := "[NC-Raws] 葬送的芙莉莲 - 第03话/[NC-Raws] 葬送的芙莉莲 - 第03话 [1080p][Baha][WEB-DL].mp4"

		code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
			Dir:   "frieren",
			Files: []string{file},
			// "动画" genre flips IsAnim in Stage 2 -> anim_tv_series root (H4).
			Metadata: map[string]interface{}{"genre": "动画"},
		})

		require.Equal(t, 200, code, body)
		var resp model.PlanResponse
		decodeBody(t, code, body, &resp)
		assert.Nil(t, resp.Error)
		require.Len(t, resp.Plan, 1)

		act := resp.Plan[0]
		assert.Equal(t, "move", act.Action)
		require.NotNil(t, act.Target)

		// Invariants:
		// 1. Root must be anim_tv_series/ because of "动画" genre
		assert.True(t, strings.HasPrefix(*act.Target, "anim_tv_series/"))
		// 2. Title must be an official form of the series. Without TMDB the
		// title comes from Stage 1's clean_title, which varies by provider:
		// zh-CN official (simplified or traditional), romaji, or English
		// official are all accepted.
		titleForms := []string{"葬送的芙莉莲", "葬送的芙莉蓮", "Sousou no Frieren", "Frieren: Beyond Journey's End"}
		matched := false
		for _, form := range titleForms {
			if strings.Contains(*act.Target, form) {
				matched = true
				break
			}
		}
		assert.True(t, matched, "target %q should contain one of the official titles %v", *act.Target, titleForms)
		// 3. Season should be Season 01
		assert.Contains(t, *act.Target, "Season 01")
		// 4. Episode must be recognized as S01E03
		assert.Contains(t, *act.Target, "S01E03")
		assert.True(t, strings.HasSuffix(*act.Target, ".mp4"))
	})
}
