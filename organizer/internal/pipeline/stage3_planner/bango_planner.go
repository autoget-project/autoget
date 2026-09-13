package stage3planner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/ptr"
)

// BangoFilenameMapping maps one video file to its new canonical filename.
type BangoFilenameMapping struct {
	File        string `json:"file"`
	NewFilename string `json:"new_filename"`
}

// BangoLLMResponse is the strict structured output schema of the bango planner LLM.
type BangoLLMResponse struct {
	Filenames []BangoFilenameMapping `json:"filenames"`
}

// BangoPlanner plans bango_porn downloads. The Go code derives the target
// directory through the decision matrix (madou / jav_vr / jav + actor subdir +
// VR subdir); the LLM only maps messy filenames to canonical bango names with
// the "-C" Chinese-subtitle rule taking priority over multi-part renumbering.
type BangoPlanner struct {
	provider ai.Provider
}

// NewBangoPlanner creates a new BangoPlanner.
func NewBangoPlanner(provider ai.Provider) *BangoPlanner {
	return &BangoPlanner{provider: provider}
}

// ResolveBangoTargetDir applies the bango target directory decision matrix:
//  1. root: madou (from_madou has top priority) > jav_vr (is_vr) > jav;
//  2. actor subdir: canonical actor directory resolved by Stage 2, or 素人
//     (amateur) when no actor is known;
//  3. VR: an extra {bango} sub-directory under the actor directory.
func ResolveBangoTargetDir(meta model.EnrichedMetadata) string {
	root := string(model.TargetDirJAV)
	if meta.IsVR {
		root = string(model.TargetDirJAVVR)
	}
	if meta.FromMadou {
		root = string(model.TargetDirMadou)
	}

	dir := path.Join(root, BangoActorDir(meta))
	if meta.IsVR {
		dir = path.Join(dir, meta.Bango)
	}
	return dir
}

// BangoActorDir resolves the actor sub-directory: Stage 2 stores the canonical
// actor directory as the first element of Actors; without any actor the entry
// falls into the 素人 (amateur) directory.
func BangoActorDir(meta model.EnrichedMetadata) string {
	if len(meta.Actors) > 0 && strings.TrimSpace(meta.Actors[0]) != "" {
		return strings.TrimSpace(meta.Actors[0])
	}
	return "素人"
}

// Plan generates the bango move plan combining the decision matrix (Go) and
// filename canonicalization (LLM).
func (p *BangoPlanner) Plan(ctx context.Context, pc *PlannerContext) ([]model.PlanAction, error) {
	videos, _, others := partitionFiles(pc.Files)
	if len(videos) == 0 {
		// No videos: skip the wasted LLM call; garbage files stay skip actions
		// and subtitles are handled by Stage 4.
		return skipOthers(others), nil
	}
	targetDir := ResolveBangoTargetDir(pc.Metadata)

	input := map[string]interface{}{
		"target_dir": targetDir,
		"files":      videos,
	}

	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("bango planner failed to marshal input: %w", err)
	}

	var resp BangoLLMResponse
	prompt := fmt.Sprintf(bangoPlannerPrompt, string(payload))
	if err := p.provider.GenerateStructured(ctx, prompt, BangoLLMResponse{}, &resp); err != nil {
		return nil, fmt.Errorf("bango planner llm generation failed: %w", err)
	}
	for _, m := range resp.Filenames {
		log.Printf("stage3 bango llm: file=%q new_filename=%q target_dir=%q", m.File, m.NewFilename, targetDir)
	}

	byFile := make(map[string]string, len(resp.Filenames))
	for _, m := range resp.Filenames {
		if m.File == "" || strings.TrimSpace(m.NewFilename) == "" {
			continue
		}
		// Filename-only contract: defensively flatten any path the LLM adds.
		byFile[m.File] = filepath.Base(filepath.Clean(strings.TrimSpace(m.NewFilename)))
	}

	actions := make([]model.PlanAction, 0, len(videos)+len(others))
	for _, v := range videos {
		if name, ok := byFile[v]; ok {
			actions = append(actions, model.PlanAction{
				File:   v,
				Action: "move",
				Target: ptr.Str(path.Join(targetDir, name)),
			})
			continue
		}
		actions = append(actions, model.PlanAction{File: v, Action: "skip"})
	}
	for _, o := range others {
		actions = append(actions, model.PlanAction{File: o, Action: "skip"})
	}
	return actions, nil
}

const bangoPlannerPrompt = `You are a specialized file organizer for media files with bango (番号) identifiers. Your task is to generate new filenames for video files based on their bango identifiers. The target directory is pre-computed; you only return new filenames.

## Core Rules:
1. **Default behavior**: Generate the new filename using the bango.ext format.
2. **Bango extraction**: Extract the bango from filenames - these are typically alphanumeric codes like "SSIS-698", "FC2-1234567", "MDX-0123".
3. **Filename only**: Only return the new filename, not the full path. Ignore the provided target_dir.

## Special Cases:

### Case 1: bango-C.ext format (HIGHEST PRIORITY)
- If the filename contains a bango with "-C" suffix (indicating Chinese subtitles), keep the "-C" suffix and always convert the bango portion to UPPERCASE (e.g., "ssis-698-C.mp4" -> "SSIS-698-C.mp4").
- The "-C" Chinese-subtitle rule takes precedence over multi-part renumbering: "SSIS-698-C.mp4" stays "SSIS-698-C.mp4" even when other parts exist.

### Case 2: Uppercase bango
- Always use UPPERCASE for the entire bango portion (e.g. "ssis-698.mp4" -> "SSIS-698.mp4").

### Case 3: Multi-part files
- Multiple files sharing the same bango with different suffixes (A/B/C, cd1/cd2, partA/partB, 上卷/下卷, DISC1/DISC2...) must be renumbered to "bango.part.1.ext", "bango.part.2.ext", "bango.part.3.ext", and so on, preserving the original file extension and following the natural watching order.
- Example: "SSIS-698-A.mp4" -> "SSIS-698.part.1.mp4"; "SSIS-698-B.mp4" -> "SSIS-698.part.2.mp4".

## Important Notes:
- Always preserve the original file extension.
- Skip any files that do not appear to be video files or do not contain a recognizable bango pattern.
- Every input file must appear exactly once in the output.

Respond strictly following the required JSON schema.

Input:
%s`
