package stage4postprocess

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/ptr"
)

// SubtitlePreviewLines is the number of leading lines read from each subtitle
// file for semantic language/episode analysis (L8).
const SubtitlePreviewLines = 30

// SubtitleMatchResult is a single subtitle mapping produced by the pairing LLM.
type SubtitleMatchResult struct {
	File         string `json:"file"`
	Action       string `json:"action"`
	MatchedVideo string `json:"matched_video,omitempty"`
	Language     string `json:"language,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// SubtitleLLMResponse is the strict structured output schema of the subtitle
// pairing LLM.
type SubtitleLLMResponse struct {
	Plan []SubtitleMatchResult `json:"plan"`
}

// SubtitlePlanner pairs companion subtitle files with Stage 3 planned videos
// via LLM semantic analysis (Stage 4). Naming output is canonical:
// <VideoBaseName>.<Language>.<ISO639-2>.<ext> with Japanese strictly "jpn".
type SubtitlePlanner struct {
	provider    ai.Provider
	downloadDir string
}

// NewSubtitlePlanner creates a new SubtitlePlanner.
func NewSubtitlePlanner(provider ai.Provider, downloadDir string) *SubtitlePlanner {
	return &SubtitlePlanner{provider: provider, downloadDir: downloadDir}
}

// ReadSubtitlePreview joins DOWNLOAD_COMPLETED_DIR/{dir}/{file} (L8) and
// returns the first SubtitlePreviewLines lines with whitespace trimmed. Read
// failures are returned as error text so the LLM treats the file as corrupted.
// dir/file are validated to stay under downloadDir (traversal defense).
func ReadSubtitlePreview(downloadDir, dir, file string) string {
	fullPath, err := safeSubtitleJoin(downloadDir, dir, file)
	if err != nil {
		return fmt.Sprintf("Error reading file %s: %v", file, err)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return fmt.Sprintf("Error reading file %s: %v", file, err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > SubtitlePreviewLines {
		lines = lines[:SubtitlePreviewLines]
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.Join(lines, "\n")
}

// PairSubtitles asks the LLM to match every subtitle file to its planned video
// and derives the canonical subtitle target path. Unmatched or corrupted
// subtitles are marked skip.
func (sp *SubtitlePlanner) PairSubtitles(ctx context.Context, dir string, subtitleFiles []string, videoPlan []model.PlanAction) ([]model.PlanAction, error) {
	items := make([]model.SubtitleMatchItem, 0, len(subtitleFiles))
	for _, f := range subtitleFiles {
		items = append(items, model.SubtitleMatchItem{
			Filename:       f,
			ContentPreview: ReadSubtitlePreview(sp.downloadDir, dir, f),
		})
	}

	input := map[string]interface{}{
		"files":           items,
		"video_move_plan": map[string]interface{}{"plan": videoPlan},
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("subtitle planner failed to marshal input: %w", err)
	}

	var resp SubtitleLLMResponse
	prompt := fmt.Sprintf(subtitlePlannerPrompt, string(payload))
	if err := sp.provider.GenerateStructured(ctx, prompt, SubtitleLLMResponse{}, &resp); err != nil {
		return nil, fmt.Errorf("subtitle llm generation failed: %w", err)
	}
	for _, item := range resp.Plan {
		log.Printf("stage4 subtitle llm: file=%q action=%q matched_video=%q language=%q reason=%q",
			item.File, item.Action, item.MatchedVideo, item.Language, item.Reason)
	}

	videoTarget := make(map[string]string, len(videoPlan))
	for _, a := range videoPlan {
		if a.Action == "move" && a.Target != nil {
			videoTarget[a.File] = *a.Target
		}
	}

	actions := make([]model.PlanAction, 0, len(subtitleFiles))
	covered := make(map[string]struct{}, len(subtitleFiles))
	for _, item := range resp.Plan {
		if item.File == "" {
			continue
		}
		// Dedupe by file (keep first) to defend the exactly-once contract
		// against duplicate LLM entries.
		if _, dup := covered[item.File]; dup {
			continue
		}
		covered[item.File] = struct{}{}
		isMoveAction := item.Action == "move" || item.Action == "rename_and_move"
		if !isMoveAction || item.MatchedVideo == "" {
			log.Printf("stage4 subtitle pairing: %q forced skip (llm action=%q, matched_video=%q)", item.File, item.Action, item.MatchedVideo)
			actions = append(actions, model.PlanAction{File: item.File, Action: "skip"})
			continue
		}
		videoTargetPath, ok := videoTarget[item.MatchedVideo]
		if !ok {
			log.Printf("stage4 subtitle pairing: %q forced skip (matched video %q not in the video move plan)", item.File, item.MatchedVideo)
			actions = append(actions, model.PlanAction{File: item.File, Action: "skip"})
			continue
		}
		lang := model.ISO639ToLanguage(item.Language)
		actions = append(actions, model.PlanAction{
			File:   item.File,
			Action: "move",
			Target: ptr.Str(SubtitleTargetPath(videoTargetPath, lang, strings.ToLower(filepath.Ext(item.File)))),
		})
	}

	// Subtitles never mentioned by the LLM are explicitly skipped.
	for _, f := range subtitleFiles {
		if _, ok := covered[f]; !ok {
			log.Printf("stage4 subtitle pairing: %q forced skip (not mentioned by the subtitle llm)", f)
			actions = append(actions, model.PlanAction{File: f, Action: "skip"})
		}
	}
	return actions, nil
}

// SubtitleTargetPath builds "<VideoBaseName>.<Language>.<ISO639-2>.<ext>"
// placed next to the planned video; Japanese strictly uses "jpn". Languages
// that cannot be determined keep the plain video base name.
func SubtitleTargetPath(videoTarget string, lang model.Language, subExt string) string {
	videoDir := filepath.Dir(videoTarget)
	videoBase := filepath.Base(videoTarget)
	stem := strings.TrimSuffix(videoBase, filepath.Ext(videoBase))

	label, iso := subtitleLanguageParts(lang)
	if label == "" {
		return filepath.Join(videoDir, stem+subExt)
	}
	return filepath.Join(videoDir, fmt.Sprintf("%s.%s.%s%s", stem, label, iso, subExt))
}

// subtitleLanguageParts maps a Language to its human label and ISO 639-2 code.
func subtitleLanguageParts(lang model.Language) (label, iso string) {
	switch lang {
	case model.LanguageChinese:
		return "简体中文", "chi"
	case model.LanguageEnglish:
		return "English", "eng"
	case model.LanguageJapanese:
		return "日本語", "jpn"
	case model.LanguageKorean:
		return "한국어", "kor"
	default:
		return "", ""
	}
}

// safeSubtitleJoin joins downloadDir/{dir}/{file} after verifying the cleaned
// path stays under downloadDir, rejecting absolute paths and ".." traversal.
func safeSubtitleJoin(downloadDir, dir, file string) (string, error) {
	for _, part := range []string{dir, file} {
		if filepath.IsAbs(part) {
			return "", fmt.Errorf("absolute path %q is not allowed", part)
		}
		cleaned := filepath.Clean(part)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path %q escapes the download directory", part)
		}
	}
	joined := filepath.Clean(filepath.Join(downloadDir, dir, file))
	base := filepath.Clean(downloadDir)
	if !strings.HasPrefix(joined, base+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the download directory", file)
	}
	return joined, nil
}

const subtitlePlannerPrompt = `Task: You are an AI system that organizes subtitle files to match their corresponding video files that have already been planned for movement.

You must analyze subtitle files along with the provided video movement plan, and produce a JSON plan describing how each subtitle file should be renamed and moved to match its video counterpart.

Specifics:
1. Input:
   - JSON object containing:
     - "files": array of subtitle file objects, each with:
       - "filename": subtitle file path (.srt, .ass, .sub, etc.)
       - "content_preview": subtitle file content (first 30 lines)
     - "video_move_plan": the movement plan for video files with their target locations
2. Analyze:
   - Use the content_preview field to examine subtitle content and determine the language (Chinese characters, Japanese kana, English text, etc.).
   - Match subtitle files to their corresponding video files by season/episode numbers or movie identity, using semantic matching - never rigid filename prefix slicing.
   - If the content shows an error or is empty, consider the file corrupted or unreadable.
3. Output:
   - For every subtitle file return "file", "action" ("move" or "skip") and the "matched_video" (the exact original video path from the video movement plan).
   - If the subtitle matches a planned video, set "action": "move".
   - Also return the detected "language" as one of: Chinese, English, Japanese, Korean, Others.
4. Edge cases:
   - If no matching video file is found: "action": "skip".
   - If the subtitle file content shows an error or is empty: "action": "skip".
   - Only subtitle files (.srt, .ass, .sub, .ssa, .vtt) are provided as input.

Respond strictly following the required JSON schema.

Input:
%s`
