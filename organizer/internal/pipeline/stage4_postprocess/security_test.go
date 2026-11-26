package stage4postprocess

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/model"
)

func mustStrPtr(s string) *string { return &s }

func TestSanitizeRelativeTarget_TraversalBlocked(t *testing.T) {
	t.Parallel()

	malicious := []string{
		"../../etc/passwd",
		"../secret",
		"jav/../../escape",
		"a/../../b",
		"..",
		"/etc/passwd",
		"/tv_series/x",
		"",
		"   ",
	}

	for _, target := range malicious {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			_, err := SanitizeRelativeTarget(target)
			assert.Error(t, err, "target %q must be rejected", target)
		})
	}
}

func TestSanitizeRelativeTarget_ValidPathsCleaned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"tv_series/Chinese/X (2020)/Season 01/X S01E01.mkv", "tv_series/Chinese/X (2020)/Season 01/X S01E01.mkv"},
		{"a/./b//c", "a/b/c"},
		{"jav/素人/SSIS-698.mp4", "jav/素人/SSIS-698.mp4"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()

			got, err := SanitizeRelativeTarget(tt.input)
			require.NoError(t, err, "target %q must be accepted", tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizePlan_GarbageFilesForcedSkip(t *testing.T) {
	t.Parallel()

	plan := []model.PlanAction{
		{File: "Movie/cover.nfo", Action: "move", Target: mustStrPtr("movie/X/cover.nfo")},
		{File: "Movie/site.url", Action: "move", Target: mustStrPtr("movie/X/site.url")},
		{File: "Movie/movie.torrent", Action: "move", Target: mustStrPtr("movie/X/movie.torrent")},
		{File: "Movie/movie.mkv", Action: "move", Target: mustStrPtr("movie/X/movie.mkv")},
	}

	files := []string{"Movie/cover.nfo", "Movie/site.url", "Movie/movie.torrent", "Movie/movie.mkv"}
	sanitized := SanitizePlan(plan, files, model.CategoryMovie)
	assert.Len(t, sanitized, len(plan), "sanitize must not drop actions")

	for _, file := range []string{"Movie/cover.nfo", "Movie/site.url", "Movie/movie.torrent"} {
		action := findAction(t, sanitized, file)
		assert.Equal(t, "skip", action.Action, "garbage file %s must be skipped", file)
		assert.Nil(t, action.Target, "garbage file %s must carry a null target", file)
	}

	keep := findAction(t, sanitized, "Movie/movie.mkv")
	require.Equal(t, "move", keep.Action)
	require.NotNil(t, keep.Target)
	assert.Equal(t, "movie/X/movie.mkv", *keep.Target)
}

func TestSanitizePlan_MoveWithoutTargetOrEscapingTargetSkipped(t *testing.T) {
	t.Parallel()

	plan := []model.PlanAction{
		{File: "a.mkv", Action: "move"},
		{File: "b.mkv", Action: "move", Target: mustStrPtr("")},
		{File: "c.mkv", Action: "move", Target: mustStrPtr("../../etc/passwd")},
		{File: "d.mkv", Action: "skip"},
		{File: "e.mkv", Action: "move", Target: mustStrPtr("movie/./b/../c.mkv")},
	}

	sanitized := SanitizePlan(plan, []string{"a.mkv", "b.mkv", "c.mkv", "d.mkv", "e.mkv"}, model.CategoryMovie)

	for _, file := range []string{"a.mkv", "b.mkv", "c.mkv"} {
		action := findAction(t, sanitized, file)
		assert.Equal(t, "skip", action.Action, "file %s must end up skipped", file)
		assert.Nil(t, action.Target, "file %s must carry a null target", file)
	}

	skip := findAction(t, sanitized, "d.mkv")
	assert.Equal(t, "skip", skip.Action, "skip actions must pass through unchanged")
	assert.Nil(t, skip.Target)

	cleaned := findAction(t, sanitized, "e.mkv")
	require.Equal(t, "move", cleaned.Action)
	require.NotNil(t, cleaned.Target)
	assert.Equal(t, "movie/c.mkv", *cleaned.Target, "valid target must be cleaned")
}

func findAction(t *testing.T, actions []model.PlanAction, file string) model.PlanAction {
	t.Helper()
	for _, a := range actions {
		if a.File == file {
			return a
		}
	}
	t.Fatalf("action for file %q not found in plan", file)
	return model.PlanAction{}
}

func TestSanitizePlanWithDetail(t *testing.T) {
	t.Parallel()

	plan := []model.PlanAction{
		{File: "cover.nfo", Action: "move", Target: mustStrPtr("movie/cover.nfo")},
		{File: "bad.mkv", Action: "move", Target: mustStrPtr("../../etc/passwd")},
		{File: "good.mkv", Action: "move", Target: mustStrPtr("movie/good.mkv")},
	}

	sanitized, detail := SanitizePlanWithDetail(plan, []string{"cover.nfo", "bad.mkv", "good.mkv"}, model.CategoryMovie)
	require.Len(t, sanitized, 3)
	assert.Len(t, detail.ForcedSkips, 2)
}

func TestSanitizePlanWithDetail_DuplicateTargetKeepsFirst(t *testing.T) {
	t.Parallel()

	plan := []model.PlanAction{
		{File: "a.mp4", Action: "move", Target: mustStrPtr("porn/x.mp4")},
		{File: "b.mp4", Action: "move", Target: mustStrPtr("porn/./x.mp4")},
	}

	sanitized, detail := SanitizePlanWithDetail(plan, []string{"a.mp4", "b.mp4"}, model.CategoryPorn)
	require.Len(t, sanitized, 2)
	assert.Equal(t, "move", sanitized[0].Action)
	assert.Equal(t, "skip", sanitized[1].Action)
	assert.Nil(t, sanitized[1].Target)
	require.Len(t, detail.ForcedSkips, 1)
	assert.Contains(t, detail.ForcedSkips[0], "duplicate target")
	assert.Contains(t, detail.ForcedSkips[0], "a.mp4")
}

func TestSanitizePlanWithDetail_Invariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		cat        model.Category
		files      []string
		plan       []model.PlanAction
		wantLen    int
		wantAction string // action of plan[0]'s result (when wantLen > 0)
		wantSkip   string // substring expected in ForcedSkips
	}{
		{"unknown file dropped", model.CategoryMovie, []string{"a.mkv"},
			[]model.PlanAction{{File: "ghost.mkv", Action: "move", Target: mustStrPtr("movie/g.mkv")}}, 0, "", "not an input"},
		{"duplicate file dropped", model.CategoryMovie, []string{"a.mkv"},
			[]model.PlanAction{
				{File: "a.mkv", Action: "move", Target: mustStrPtr("movie/a.mkv")},
				{File: "a.mkv", Action: "move", Target: mustStrPtr("movie/b.mkv")},
			}, 1, "move", "repeated file"},
		{"simple plan directory action allowed", model.CategoryBook, []string{"h/x.epub", "h/y.epub"},
			[]model.PlanAction{{File: "h", Action: "move", Target: mustStrPtr("book/h")}}, 1, "move", ""},
		{"wrong root for movie", model.CategoryMovie, []string{"a.mkv"},
			[]model.PlanAction{{File: "a.mkv", Action: "move", Target: mustStrPtr("tv_series/a.mkv")}}, 1, "skip", "outside allowed roots"},
		{"wrong root for jav", model.CategoryBangoPorn, []string{"a.mp4"},
			[]model.PlanAction{{File: "a.mp4", Action: "move", Target: mustStrPtr("porn/a.mp4")}}, 1, "skip", "outside allowed roots"},
		{"anim_movie allowed for movie", model.CategoryMovie, []string{"a.mkv"},
			[]model.PlanAction{{File: "a.mkv", Action: "move", Target: mustStrPtr("anim_movie/a.mkv")}}, 1, "move", ""},
		{"extension change skipped", model.CategoryMovie, []string{"a.mkv"},
			[]model.PlanAction{{File: "a.mkv", Action: "move", Target: mustStrPtr("movie/a.mp4")}}, 1, "skip", "extension"},
		{"extension case-insensitive", model.CategoryMovie, []string{"a.MKV"},
			[]model.PlanAction{{File: "a.MKV", Action: "move", Target: mustStrPtr("movie/a.mkv")}}, 1, "move", ""},
		{"unknown category moves nothing", model.CategoryUnknown, []string{"a.mkv"},
			[]model.PlanAction{{File: "a.mkv", Action: "move", Target: mustStrPtr("movie/a.mkv")}}, 1, "skip", "outside allowed roots"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sanitized, detail := SanitizePlanWithDetail(tt.plan, tt.files, tt.cat)
			require.Len(t, sanitized, tt.wantLen)
			if tt.wantLen > 0 {
				assert.Equal(t, tt.wantAction, sanitized[0].Action)
			}
			if tt.wantSkip == "" {
				assert.Empty(t, detail.ForcedSkips)
				return
			}
			require.NotEmpty(t, detail.ForcedSkips)
			assert.Contains(t, detail.ForcedSkips[0], tt.wantSkip)
		})
	}
}
