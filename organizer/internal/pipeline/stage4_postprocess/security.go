package stage4postprocess

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/model"
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

// SanitizePlan enforces the physical security bottom line on every action:
//   - garbage files (.nfo, .url, .torrent ...) are forced to skip;
//   - move actions without a target are forced to skip;
//   - every move target is filepath.Clean'ed, and any absolute or escaping
//     ("../") target is rejected and forced to skip.
func SanitizePlan(plan []model.PlanAction) []model.PlanAction {
	sanitized := make([]model.PlanAction, 0, len(plan))
	for _, action := range plan {
		if _, ok := garbageExtensions[strings.ToLower(filepath.Ext(action.File))]; ok {
			log.Printf("stage4 sanitize: %q forced skip (garbage extension)", action.File)
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		if action.Action != "move" {
			sanitized = append(sanitized, action)
			continue
		}
		if action.Target == nil || strings.TrimSpace(*action.Target) == "" {
			log.Printf("stage4 sanitize: %q forced skip (move action without target)", action.File)
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		cleaned, err := SanitizeRelativeTarget(*action.Target)
		if err != nil {
			log.Printf("stage4 sanitize: %q forced skip (invalid target %q: %v)", action.File, *action.Target, err)
			sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "skip"})
			continue
		}
		target := cleaned
		sanitized = append(sanitized, model.PlanAction{File: action.File, Action: "move", Target: &target})
	}
	return sanitized
}
