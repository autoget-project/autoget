package stage3planner

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/ptr"
)

// SimplePlan archives the 5 simple categories (photobook, audio_book, book,
// music, music_video) with the local three-branch strategy (M7):
//  1. single file: target = {category}/{filename};
//  2. many files directly under the hash dir: the whole hash dir is moved,
//     target = {category}/{hash_dir};
//  3. files spread across sub directories: each sub dir is archived with the
//     meaningless hash layer stripped, file = {hash_dir}/{d}, target = {category}/{d}.
//
// Assumption: inputs follow the real torrent layout "{hash}/..." (every file
// path contains at least one "/"). Multiple top-level flat files (no "/")
// yield an empty plan instead of an error.
func SimplePlan(cat model.Category, files []string) []model.PlanAction {
	categoryDir := string(cat)

	// Branch 1: single file goes straight into the category dir.
	if len(files) == 1 {
		f := files[0]
		return []model.PlanAction{{
			File:   f,
			Action: "move",
			Target: ptr.Str(path.Join(categoryDir, filepath.Base(f))),
		}}
	}

	fileUnderHashDir := false
	var dirsUnderHashDir []string
	seenDirs := make(map[string]struct{})
	hashDir := strings.Split(files[0], "/")[0]
	for _, f := range files {
		parts := strings.Split(f, "/")
		if len(parts) == 2 {
			fileUnderHashDir = true
			break
		}
		if len(parts) > 2 {
			if _, ok := seenDirs[parts[1]]; !ok {
				seenDirs[parts[1]] = struct{}{}
				dirsUnderHashDir = append(dirsUnderHashDir, parts[1])
			}
		}
	}

	// Branch 2: many files under the hash dir -> move the whole hash dir.
	if fileUnderHashDir {
		return []model.PlanAction{{
			File:   hashDir,
			Action: "move",
			Target: ptr.Str(path.Join(categoryDir, hashDir)),
		}}
	}

	// Branch 3 (M7): archive each sub dir, stripping the hash wrapper layer.
	actions := make([]model.PlanAction, 0, len(dirsUnderHashDir))
	for _, d := range dirsUnderHashDir {
		actions = append(actions, model.PlanAction{
			File:   hashDir + "/" + d,
			Action: "move",
			Target: ptr.Str(path.Join(categoryDir, d)),
		})
	}
	return actions
}
