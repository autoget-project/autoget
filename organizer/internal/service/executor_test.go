package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/ptr"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestExecutePlan_SingleFileMoveAndArchive(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()
	mustWriteFile(t, filepath.Join(downloadDir, "d1", "movie.mkv"), "data")

	exec := NewExecutor(downloadDir, targetDir)
	resp, err := exec.ExecutePlan(context.Background(), "d1", []model.PlanAction{
		{File: "movie.mkv", Action: "move", Target: ptr.Str("movie/Others/Movie (2000)/Movie (2000).mkv")},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.FailedMove)

	assert.FileExists(t, filepath.Join(targetDir, "movie", "Others", "Movie (2000)", "Movie (2000).mkv"))
	assert.NoDirExists(t, filepath.Join(downloadDir, "d1"), "source dir must be gone after archiving")
	assert.DirExists(t, filepath.Join(downloadDir, "archive", "d1"))
}

func TestExecutePlan_DirectoryAtomicMove(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()
	mustWriteFile(t, filepath.Join(downloadDir, "d2", "season", "ep01.mkv"), "a")
	mustWriteFile(t, filepath.Join(downloadDir, "d2", "season", "ep02.mkv"), "b")

	exec := NewExecutor(downloadDir, targetDir)
	resp, err := exec.ExecutePlan(context.Background(), "d2", []model.PlanAction{
		{File: "season", Action: "move", Target: ptr.Str("tv_series/Others/Show (2020)/Season 01")},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.FailedMove, "directory move must succeed atomically")

	seasonDir := filepath.Join(targetDir, "tv_series", "Others", "Show (2020)", "Season 01")
	assert.FileExists(t, filepath.Join(seasonDir, "ep01.mkv"))
	assert.FileExists(t, filepath.Join(seasonDir, "ep02.mkv"))
}

func TestExecutePlan_MixedFailureAggregationKeepsLegalMoves(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()
	mustWriteFile(t, filepath.Join(downloadDir, "d3", "good.mkv"), "data")

	exec := NewExecutor(downloadDir, targetDir)
	resp, err := exec.ExecutePlan(context.Background(), "d3", []model.PlanAction{
		{File: "missing.mkv", Action: "move", Target: ptr.Str("movie/Others/M (2000)/M (2000).mkv")},
		{File: "good.mkv", Action: "move", Target: ptr.Str("movie/Others/M (2000)/M (2000) part.2.mkv")},
	})
	require.NoError(t, err, "aggregated failure must not surface as a fatal error")

	require.Len(t, resp.FailedMove, 1)
	failed := resp.FailedMove[0]
	assert.Equal(t, "missing.mkv", failed.File)
	assert.Equal(t, "move", failed.Action)
	assert.Equal(t, "file not found", failed.Reason)
	require.NotNil(t, failed.Target, "failed entry must carry the original target")
	assert.Equal(t, "movie/Others/M (2000)/M (2000).mkv", *failed.Target)

	// The legal move must still be executed, and the source directory must
	// stay in place (never archived) when any failure exists.
	assert.FileExists(t, filepath.Join(targetDir, "movie", "Others", "M (2000)", "M (2000) part.2.mkv"))
	assert.DirExists(t, filepath.Join(downloadDir, "d3"))
	assert.NoDirExists(t, filepath.Join(downloadDir, "archive", "d3"))
}

func TestExecutePlan_SourceDirAlreadyCleanedSkipsArchive(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()

	// The source dir "d4" never existed / was cleaned externally: no move
	// actions, so everything "succeeds" and the archive step must be skipped
	// without any error.
	exec := NewExecutor(downloadDir, targetDir)
	resp, err := exec.ExecutePlan(context.Background(), "d4", []model.PlanAction{
		{File: "note.txt", Action: "skip", Target: nil},
	})
	require.NoError(t, err, "missing source dir must not error")
	assert.Empty(t, resp.FailedMove)
	assert.NoDirExists(t, filepath.Join(downloadDir, "archive", "d4"))
}

func TestExecutePlan_SkipActionsAreIgnored(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	targetDir := t.TempDir()
	mustWriteFile(t, filepath.Join(downloadDir, "d5", "cover.nfo"), "junk")

	exec := NewExecutor(downloadDir, targetDir)
	resp, err := exec.ExecutePlan(context.Background(), "d5", []model.PlanAction{
		{File: "cover.nfo", Action: "skip", Target: nil},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.FailedMove, "skip actions must not fail")

	// Full success (only skips) still archives the source dir with the
	// skipped files inside.
	assert.FileExists(t, filepath.Join(downloadDir, "archive", "d5", "cover.nfo"))
	assert.NoDirExists(t, filepath.Join(downloadDir, "d5"), "source dir must be gone after archiving")
}
