package stage4postprocess

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
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

	sanitized := SanitizePlan(plan)
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
		{File: "e.mkv", Action: "move", Target: mustStrPtr("a/./b/../c.mkv")},
	}

	sanitized := SanitizePlan(plan)

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
	assert.Equal(t, "a/c.mkv", *cleaned.Target, "valid target must be cleaned")
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
