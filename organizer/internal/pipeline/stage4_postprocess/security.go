package stage4postprocess

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autoget-project/autoget/organizer/internal/model"
)

// garbageExtensions lists companion junk files that must never be moved into
// the media library.
var garbageExtensions = map[string]struct{}{
	".nfo":     {},
	".url":     {},
	".torrent": {},
}

// SanitizeRelativeTarget cleans a relative target path and guarantees it stays
// inside the TARGET_DIR sandbox, defending against "../" traversal injection.
func SanitizeRelativeTarget(target string) (string, error) {
	if strings.TrimSpace(target) == "" {
		return "", fmt.Errorf("target path is empty")
	}
	if filepath.IsAbs(target) {
		return "", fmt.Errorf("absolute target path %q is not allowed", target)
	}
	cleaned := filepath.Clean(target)
	if cleaned == "." || cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") ||
		strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target path %q escapes the target sandbox", target)
	}
	return cleaned, nil
}

// allowedRoots lists the TARGET_DIR roots each category's planner may write
// to. Categories not listed (e.g. unknown) may not move anything.
var allowedRoots = func() map[model.Category][]model.TargetDir {
	m := map[model.Category][]model.TargetDir{
		model.CategoryMovie:     {model.TargetDirMovie, model.TargetDirAnimMovie},
		model.CategoryTVSeries:  {model.TargetDirTVSeries, model.TargetDirAnimTVSeries},
		model.CategoryBangoPorn: {model.TargetDirJAV, model.TargetDirJAVVR, model.TargetDirMadou},
		model.CategoryPorn:      {model.TargetDirPorn, model.TargetDirPornVR},
	}
	for _, c := range model.SimpleMoveCategories {
		m[c] = []model.TargetDir{model.TargetDir(c)}
	}
	return m
}()

// validRoot reports whether the first segment of the cleaned target is an
// allowed library root for the category.
func validRoot(cat model.Category, target string) bool {
	root, _, _ := strings.Cut(filepath.ToSlash(target), "/")
	for _, r := range allowedRoots[cat] {
		if string(r) == root {
			return true
		}
	}
	return false
}

// isInputOrParent reports whether file is an input or a directory containing one.
func isInputOrParent(file string, files []string) bool {
	for _, f := range files {
		if f == file || strings.HasPrefix(f, file+"/") {
			return true
		}
	}
	return false
}

// PostProcessDetail captures diagnostic information from Stage 4 postprocessing.
type PostProcessDetail struct {
	ForcedSkips []string `json:"forced_skips"`
}

// SanitizePlan enforces the physical security bottom line on every action:
//   - garbage files (.nfo, .url, .torrent ...) are forced to skip;
//   - move actions without a target are forced to skip;
//   - every move target is filepath.Clean'ed, and any absolute or escaping
//     ("../") target is rejected and forced to skip;
//   - actions for files that are not inputs (nor a directory holding one), or
//     that repeat an earlier action's file, are dropped (first wins);
//   - move targets outside the category's library roots, or that change the
//     file extension, are forced to skip;
//   - a target already planned for an earlier action is forced to skip (first wins).
func SanitizePlan(plan []model.PlanAction, files []string, cat model.Category) []model.PlanAction {
	sanitized, _ := SanitizePlanWithDetail(plan, files, cat)
	return sanitized
}

// SanitizePlanWithDetail performs SanitizePlan and returns PostProcessDetail.
func SanitizePlanWithDetail(plan []model.PlanAction, files []string, cat model.Category) ([]model.PlanAction, PostProcessDetail) {
	sanitized := make([]model.PlanAction, 0, len(plan))
	var detail PostProcessDetail
	// seen maps a cleaned target to the first file planned for it.
	seen := map[string]string{}
	seenFiles := map[string]struct{}{}
	for _, action := range plan {
		if !isInputOrParent(action.File, files) {
			detail.ForcedSkips = append(detail.ForcedSkips, action.File+": not an input file, action dropped")
			continue
		}
		if _, dup := seenFiles[action.File]; dup {
			detail.ForcedSkips = append(detail.ForcedSkips, action.File+": repeated file, action dropped")
			continue
		}
		seenFiles[action.File] = struct{}{}
		if _, ok := garbageExtensions[strings.ToLower(filepath.Ext(action.File))]; ok {
			detail.ForcedSkips = append(detail.ForcedSkips, action.File+": garbage extension")
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		if action.Action != "move" {
			sanitized = append(sanitized, action)
			continue
		}
		if action.Target == nil || strings.TrimSpace(*action.Target) == "" {
			detail.ForcedSkips = append(detail.ForcedSkips, action.File+": move action without target")
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		cleaned, err := SanitizeRelativeTarget(*action.Target)
		if err != nil {
			detail.ForcedSkips = append(detail.ForcedSkips, fmt.Sprintf("%s: invalid target %q (%v)", action.File, *action.Target, err))
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		if !validRoot(cat, cleaned) {
			detail.ForcedSkips = append(detail.ForcedSkips, fmt.Sprintf("%s: target %q outside allowed roots for %s", action.File, cleaned, cat))
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		if !strings.EqualFold(filepath.Ext(action.File), filepath.Ext(cleaned)) {
			detail.ForcedSkips = append(detail.ForcedSkips, fmt.Sprintf("%s: target %q changes file extension", action.File, cleaned))
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		if other, dup := seen[cleaned]; dup {
			detail.ForcedSkips = append(detail.ForcedSkips, fmt.Sprintf("%s: duplicate target %s (also planned for %s)", action.File, cleaned, other))
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		seen[cleaned] = action.File
		target := cleaned
		sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "move", Target: &target})
	}
	return sanitized, detail
}
