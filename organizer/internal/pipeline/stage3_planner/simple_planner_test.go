package stage3planner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autoget-project/organizer/internal/model"
)

// planActionsEqual compares two plans ignoring order.
func planActionsEqual(a, b []model.PlanAction) bool {
	if len(a) != len(b) {
		return false
	}
	used := make([]bool, len(b))
	for _, action := range a {
		found := false
		for i, other := range b {
			if used[i] {
				continue
			}
			if action.File == other.File && action.Action == other.Action {
				if (action.Target == nil) != (other.Target == nil) {
					continue
				}
				if action.Target != nil && *action.Target != *other.Target {
					continue
				}
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func mustTarget(s string) *string { return &s }

func TestSimplePlanSingleFile(t *testing.T) {
	t.Parallel()

	// Branch 1: a single file moves under the category root.
	got := SimplePlan(model.CategoryMovie, []string{"torrent_hash/movie.mp4"})
	want := []model.PlanAction{
		{File: "torrent_hash/movie.mp4", Action: "move", Target: mustTarget("movie/movie.mp4")},
	}
	assert.True(t, planActionsEqual(got, want), "unexpected plan: %+v", got)
}

func TestSimplePlanMultipleFilesUnderHashDir(t *testing.T) {
	t.Parallel()

	// Branch 2: multiple files directly under the hash dir move as a whole.
	got := SimplePlan(model.CategoryTVSeries, []string{"torrent_hash/episode1.mp4", "torrent_hash/episode2.mp4"})
	want := []model.PlanAction{
		{File: "torrent_hash", Action: "move", Target: mustTarget("tv_series/torrent_hash")},
	}
	assert.True(t, planActionsEqual(got, want), "unexpected plan: %+v", got)
}

func TestSimplePlanFilesInSubDirs_Branch3_M7(t *testing.T) {
	t.Parallel()

	// Branch 3 (M7): sub dirs are archived with the hash layer stripped.
	got := SimplePlan(model.CategoryBook, []string{
		"torrent_hash/chapter1/page1.pdf",
		"torrent_hash/chapter1/page2.pdf",
		"torrent_hash/chapter2/page1.pdf",
	})
	want := []model.PlanAction{
		{File: "torrent_hash/chapter1", Action: "move", Target: mustTarget("book/chapter1")},
		{File: "torrent_hash/chapter2", Action: "move", Target: mustTarget("book/chapter2")},
	}
	assert.True(t, planActionsEqual(got, want), "unexpected plan: %+v", got)

	// Explicit M7 assertion: file = {hash_dir}/{d}, target = {category}/{d}.
	for _, action := range got {
		if assert.NotNil(t, action.Target) {
			assert.False(t, strings.Contains(*action.Target, "torrent_hash"),
				"branch 3 target must not keep the hash dir: %s", *action.Target)
		}
	}
}

func TestSimplePlanMixedFilesAndDirsUnderHashDir(t *testing.T) {
	t.Parallel()

	// Mixed files and sub dirs under the hash dir: branch 2 wins and the
	// whole directory moves.
	got := SimplePlan(model.CategoryMusic, []string{
		"torrent_hash/song.mp3",
		"torrent_hash/album_art/cover.jpg",
	})
	want := []model.PlanAction{
		{File: "torrent_hash", Action: "move", Target: mustTarget("music/torrent_hash")},
	}
	assert.True(t, planActionsEqual(got, want), "unexpected plan: %+v", got)
}

func TestSimplePlan_AllFiveCategories(t *testing.T) {
	t.Parallel()

	for _, cat := range model.SimpleMoveCategories {
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()

			got := SimplePlan(cat, []string{"hash/item.dat"})
			if assert.Len(t, got, 1) {
				if assert.NotNil(t, got[0].Target) {
					assert.Contains(t, *got[0].Target, string(cat)+"/",
						"target must live under the category root")
				}
			}
		})
	}
}
