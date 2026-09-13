// Package service implements the physical execution layer behind the REST
// API: it atomically moves planned files into the media library and archives
// the emptied source directory.
package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/model"
)

// Executor performs physical file moves and source directory archiving.
type Executor struct {
	// downloadDir is the DOWNLOAD_COMPLETED_DIR root holding per-request
	// download sub directories.
	downloadDir string
	// targetDir is the TARGET_DIR root of the media library.
	targetDir string
}

// NewExecutor creates a new Executor.
func NewExecutor(downloadDir, targetDir string) *Executor {
	return &Executor{
		downloadDir: downloadDir,
		targetDir:   targetDir,
	}
}

// ExecutePlan walks over every plan action with full aggregation failure
// semantics (L11): a missing source file never aborts the remaining legal
// moves. Only when every move succeeds is the source directory archived into
// {downloadDir}/archive/{dir}, guarded by a source-dir existence pre-check
// (L15) that silently skips archiving when the directory is already gone.
//
// ctx is currently unused: os.Rename/os.Stat are not context-aware, but the
// parameter is kept in the interface so future cancellable execution backends
// stay source-compatible.
func (e *Executor) ExecutePlan(ctx context.Context, dir string, plan []model.PlanAction) (model.ExecuteResponse, error) {
	failedMove := make([]model.PlanFailed, 0)

	// Defense-in-depth: never trust client dir/file/target; escapes are
	// recorded as failed moves.
	baseDir, err := safeJoin(e.downloadDir, dir)
	if err != nil {
		return model.ExecuteResponse{FailedMove: failedMove}, fmt.Errorf("invalid request dir %q: %w", dir, err)
	}

	for _, action := range plan {
		if action.Action != "move" {
			continue
		}

		sourcePath, srcErr := safeJoin(baseDir, action.File)

		// Source existence pre-check (compatible with both files and dirs).
		if srcErr != nil {
			failedMove = append(failedMove, model.PlanFailed{
				File:   action.File,
				Action: action.Action,
				Target: action.Target,
				Reason: "file not found",
			})
			// L11: keep executing subsequent legal moves instead of aborting.
			continue
		}
		if _, err := os.Stat(sourcePath); err != nil {
			failedMove = append(failedMove, model.PlanFailed{
				File:   action.File,
				Action: action.Action,
				Target: action.Target,
				Reason: "file not found",
			})
			// L11: keep executing subsequent legal moves instead of aborting.
			continue
		}

		if action.Target == nil || strings.TrimSpace(*action.Target) == "" {
			failedMove = append(failedMove, model.PlanFailed{
				File:   action.File,
				Action: action.Action,
				Target: action.Target,
				Reason: "move action without target",
			})
			continue
		}

		targetRel, tgtErr := safeJoinRel(*action.Target)
		if tgtErr != nil {
			failedMove = append(failedMove, model.PlanFailed{
				File:   action.File,
				Action: action.Action,
				Target: action.Target,
				Reason: fmt.Sprintf("invalid target: %v", tgtErr),
			})
			continue
		}
		targetFile := filepath.Join(e.targetDir, targetRel)
		if err := os.MkdirAll(filepath.Dir(targetFile), 0755); err != nil {
			failedMove = append(failedMove, model.PlanFailed{
				File:   action.File,
				Action: action.Action,
				Target: action.Target,
				Reason: fmt.Sprintf("create target dir failed: %v", err),
			})
			continue
		}

		if err := os.Rename(sourcePath, targetFile); err != nil {
			failedMove = append(failedMove, model.PlanFailed{
				File:   action.File,
				Action: action.Action,
				Target: action.Target,
				Reason: fmt.Sprintf("rename failed: %v", err),
			})
		}
	}

	// L11: any aggregated failure means the caller must NOT archive the
	// source directory.
	if len(failedMove) > 0 {
		return model.ExecuteResponse{FailedMove: failedMove}, nil
	}

	if err := e.archiveSourceDir(dir); err != nil {
		return model.ExecuteResponse{FailedMove: failedMove}, err
	}

	return model.ExecuteResponse{FailedMove: failedMove}, nil
}

// archiveSourceDir moves {downloadDir}/{dir} into {downloadDir}/archive/{dir}.
// L15: when the source directory has already been cleaned externally the
// archive step is silently skipped and treated as success.
func (e *Executor) archiveSourceDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}

	sourceDir, err := safeJoin(e.downloadDir, dir)
	if err != nil {
		return fmt.Errorf("invalid source dir %q: %w", dir, err)
	}
	if _, err := os.Stat(sourceDir); err != nil {
		if os.IsNotExist(err) {
			// L15: already cleaned externally -> skip archiving without error.
			log.Printf("[L15] source dir %s no longer exists, skip archiving", sourceDir)
			return nil
		}
		return fmt.Errorf("stat source dir %s failed: %w", sourceDir, err)
	}

	archivePath := filepath.Join(e.downloadDir, "archive", dir)
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		return fmt.Errorf("create archive parent dir failed: %w", err)
	}

	if err := os.Rename(sourceDir, archivePath); err != nil {
		return fmt.Errorf("archive source dir %s failed: %w", sourceDir, err)
	}
	return nil
}

// safeJoinRel validates that rel is a safe relative path: not absolute and
// free of any ".." traversal segments.
func safeJoinRel(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute path %q is not allowed", rel)
	}
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the allowed directory", rel)
	}
	return cleaned, nil
}

// safeJoin joins base with the sanitized rel and verifies the cleaned result
// still stays under base (defense-in-depth against traversal).
func safeJoin(base, rel string) (string, error) {
	cleaned, err := safeJoinRel(rel)
	if err != nil {
		return "", err
	}
	joined := filepath.Clean(filepath.Join(base, cleaned))
	if baseCleaned := filepath.Clean(base); !strings.HasPrefix(joined, baseCleaned+string(filepath.Separator)) && joined != baseCleaned {
		return "", fmt.Errorf("path %q escapes the allowed directory", rel)
	}
	return joined, nil
}
