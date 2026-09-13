package stage1classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

// Confidence represents the verdict confidence of a specialist checker.
type Confidence string

const (
	ConfidenceYes   Confidence = "yes"
	ConfidenceNo    Confidence = "no"
	ConfidenceMaybe Confidence = "maybe"
)

// CheckerEntities contains metadata entities extracted by a specialist.
type CheckerEntities struct {
	IMDbID     string   `json:"imdb_id,omitempty"`
	DmmID      string   `json:"dmm_id,omitempty"`
	Bango      string   `json:"bango,omitempty"`
	CleanTitle string   `json:"clean_title,omitempty"`
	Year       int      `json:"year,omitempty"`
	Actors     []string `json:"actors,omitempty"`
	// IsVR is the content's Virtual Reality property, grounded in the search
	// context or studio/series knowledge. It is a semantic judgment, never a
	// filename pattern rule.
	IsVR bool `json:"is_vr,omitempty"`
}

// CheckerResponse is the structured response from a specialist checker.
type CheckerResponse struct {
	Confidence Confidence      `json:"confidence"`
	Reason     string          `json:"reason"`
	Entities   CheckerEntities `json:"entities"`
}

// CheckerResult is the internal output paired with its Category.
type CheckerResult struct {
	Category model.Category
	Response CheckerResponse
	Err      error
}

const (
	pornCheckerPrompt = `You are a specialist media classifier determining if the given files represent Western/general adult video (porn) content that does NOT use Japanese bango (JAV) numbering.

Instructions:
- Return "yes" if the files represent Western or general non-bango adult video content (e.g. Western adult studios, English performer names, descriptive adult titles).
- Return "no" if the files are Japanese adult video (JAV) with bango numbering, or mainstream non-adult movies/TV/music.
- Return "maybe" if there is ambiguity.
- Extract any clean_title, year, or actors if recognized.
- Extract is_vr as a semantic judgment about the content: set it true only when the content is Virtual Reality (VR) adult video — for example search_context names a VR studio/release, the studio/series is known to be VR, or the filename clearly marks a VR release. Do not set it from a filename token rule alone.
Return your answer strictly matching the required JSON schema.`

	bangoPornCheckerPrompt = `You are a specialist media classifier determining if the given files represent Japanese adult video (JAV) or Asian bango porn content.

Instructions:
- Return "yes" ONLY if the files have Japanese bango numbering (e.g. SSIS-123, FC2-PPV-123456, or Madou series numbering like MD-0123).
- Return "no" if there is no Japanese/Asian bango numbering, or if it is Western/general porn, or mainstream movies/TV shows.
- Return "maybe" if a bango code might be present but ambiguous.
- Extract bango, dmm_id, clean_title, or actors if recognized.
- Extract is_vr as a semantic judgment: set it true when the JAV release is Virtual Reality (VR) — typically the bango label itself is a VR series (label ending in VR like IPVR/SIVR) or search_context confirms a VR release.
Return your answer strictly matching the required JSON schema.`

	movieCheckerPrompt = `You are a specialist media classifier determining if the given files represent a standalone movie or film.

Instructions:
- Return "yes" if the files represent a standalone movie, film, or feature-length animated movie.
- Return "no" if the files are adult/porn content, episodic TV series, or music/audio.
- Return "maybe" if ambiguous.
- Extract clean_title, year, or imdb_id if recognized.
Return your answer strictly matching the required JSON schema.`

	tvSeriesCheckerPrompt = `You are a specialist media classifier determining if the given files represent episodic TV series or drama episodes.

Instructions:
- Return "yes" if the files represent episodic TV shows, drama series, seasons, or multi-episode releases (e.g. S01E01, 1x01, EP01).
- Return "no" if the files are standalone movies, adult/porn videos, or music/audio.
- Return "maybe" if ambiguous.
- Extract clean_title, year, or imdb_id if recognized.
Return your answer strictly matching the required JSON schema.`

	audioBookCheckerPrompt = `You are a specialist media classifier determining if the given audio files represent an audiobook.

Instructions:
- Return "yes" if the files represent spoken word audiobooks, narration, or novel chapters.
- Chapter/part markers in filenames (e.g. "Chapter 01", "Chapter_02_Silent_Spring", "Prologue", "Part 2", "第01章") or a novel/story title with sequential narration are STRONG audiobook indicators: return "yes" confidently, not "maybe".
- Return "no" if the files are musical albums, songs, or video media. Numbered tracks named after songs, artists, or albums (e.g. "01. Welcome to New York") are music.
- Return "maybe" only if genuinely ambiguous.
- Extract clean_title or author if recognized.
Return your answer strictly matching the required JSON schema.`

	musicCheckerPrompt = `You are a specialist media classifier determining if the given audio files represent music songs or musical albums.

Instructions:
- Return "yes" if the files represent musical songs, music albums, artist discographies, or soundtrack tracks.
- Return "no" if the files are audiobooks/spoken-word narration (chapter/part/prologue markers or novel titles indicate audiobooks) or video media.
- Return "maybe" only if genuinely ambiguous.
- Extract clean_title or artist/actors if recognized.
Return your answer strictly matching the required JSON schema.`

	photobookCheckerPrompt = `You are a specialist media classifier determining if the given files represent a photobook or photo album.

Instructions:
- Return "yes" if the files represent a photography book, gravure photo set, or image collection.
- Return "no" if the files are primarily movies, tv shows, audio, or text books.
- Return "maybe" if ambiguous.
- Extract clean_title if recognized.
Return your answer strictly matching the required JSON schema.`

	musicVideoCheckerPrompt = `You are a specialist media classifier determining if the given files represent a music video (MV) or live concert recording.

Instructions:
- Return "yes" if the files represent a music video (MV) or concert video.
- Return "no" if the files are movies, tv series, porn, or audio-only tracks.
- Return "maybe" if ambiguous.
- Extract clean_title if recognized.
Return your answer strictly matching the required JSON schema.`
)

type flexibleCheckerResponse struct {
	Confidence Confidence      `json:"confidence"`
	Category   model.Category  `json:"category"`
	Reason     string          `json:"reason"`
	Entities   CheckerEntities `json:"entities"`
}

func runSpecialistChecker(ctx context.Context, provider ai.Provider, cat model.Category, promptTpl string, files []string, metadata map[string]interface{}, searchCtx SearchContext) (CheckerResponse, error) {
	if provider == nil {
		return CheckerResponse{Confidence: ConfidenceNo}, fmt.Errorf("ai provider is nil")
	}

	payload := map[string]interface{}{
		"files":    files,
		"metadata": metadata,
	}
	if searchCtx.HasInfo() {
		payload["search_context"] = searchCtx
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return CheckerResponse{Confidence: ConfidenceNo}, fmt.Errorf("failed to marshal payload: %w", err)
	}

	fullPrompt := fmt.Sprintf("%s\n\nInput:\n%s", promptTpl, string(payloadBytes))
	var resp flexibleCheckerResponse
	if err := provider.GenerateStructured(ctx, fullPrompt, CheckerResponse{}, &resp); err != nil {
		return CheckerResponse{Confidence: ConfidenceNo}, err
	}

	res := CheckerResponse{
		Confidence: resp.Confidence,
		Reason:     resp.Reason,
		Entities:   resp.Entities,
	}

	// Normalize confidence
	switch strings.ToLower(string(res.Confidence)) {
	case "yes":
		res.Confidence = ConfidenceYes
	case "maybe":
		res.Confidence = ConfidenceMaybe
	case "no":
		res.Confidence = ConfidenceNo
	default:
		// If confidence was omitted but Category was provided (e.g. from mock or fallback)
		if resp.Category != "" {
			if resp.Category == cat {
				res.Confidence = ConfidenceYes
			} else {
				res.Confidence = ConfidenceNo
			}
		} else {
			res.Confidence = ConfidenceNo
		}
	}

	return res, nil
}

// selectCandidates returns the plausible categories based on file extensions and metadata.
func selectCandidates(files []string, metadata map[string]interface{}) []model.Category {
	var hasVideo, hasAudio, hasImage, hasDoc bool

	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if _, ok := videoExtensions[ext]; ok {
			hasVideo = true
		} else if _, ok := audioExtensions[ext]; ok {
			hasAudio = true
		} else if _, ok := bookExtensions[ext]; ok {
			hasDoc = true
		} else if _, ok := imageExtensions[ext]; ok {
			hasImage = true
		}
	}

	var candidates []model.Category

	if hasVideo {
		candidates = append(candidates,
			model.CategoryPorn,
			model.CategoryBangoPorn,
			model.CategoryMovie,
			model.CategoryTVSeries,
			model.CategoryMusicVideo,
		)
	}

	if hasAudio && !hasVideo {
		candidates = append(candidates,
			model.CategoryAudioBook,
			model.CategoryMusic,
		)
	}

	if hasImage && !hasVideo {
		candidates = append(candidates,
			model.CategoryPhotobook,
		)
	}

	if hasDoc && len(candidates) == 0 {
		candidates = append(candidates, model.CategoryBook)
	}

	// If no candidate was identified (e.g. empty or unknown extension), default to all media categories
	if len(candidates) == 0 {
		candidates = []model.Category{
			model.CategoryMovie,
			model.CategoryTVSeries,
			model.CategoryPorn,
			model.CategoryBangoPorn,
			model.CategoryAudioBook,
			model.CategoryMusic,
			model.CategoryPhotobook,
		}
	}

	return candidates
}

func getCheckerPrompt(cat model.Category) (string, bool) {
	switch cat {
	case model.CategoryPorn:
		return pornCheckerPrompt, true
	case model.CategoryBangoPorn:
		return bangoPornCheckerPrompt, true
	case model.CategoryMovie:
		return movieCheckerPrompt, true
	case model.CategoryTVSeries:
		return tvSeriesCheckerPrompt, true
	case model.CategoryAudioBook:
		return audioBookCheckerPrompt, true
	case model.CategoryMusic:
		return musicCheckerPrompt, true
	case model.CategoryPhotobook:
		return photobookCheckerPrompt, true
	case model.CategoryMusicVideo:
		return musicVideoCheckerPrompt, true
	default:
		return "", false
	}
}
