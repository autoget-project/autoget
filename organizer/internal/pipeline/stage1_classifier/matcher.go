package stage1classifier

import (
	"log"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/autoget-project/organizer/internal/model"
)

// Supported extensions for rule matching.
var (
	bookExtensions = map[string]struct{}{
		".pdf":  {},
		".epub": {},
		".mobi": {},
		".azw3": {},
		".txt":  {},
	}
	audioExtensions = map[string]struct{}{
		".mp3":  {},
		".flac": {},
		".wav":  {},
		".ape":  {},
		".ogg":  {},
		".m4a":  {},
	}
	videoExtensions = map[string]struct{}{
		".mp4": {},
		".mkv": {},
		".avi": {},
		".mov": {},
		".wmv": {},
		".ts":  {},
	}
	imageExtensions = map[string]struct{}{
		".jpg":  {},
		".jpeg": {},
		".png":  {},
		".webp": {},
	}
)

var (
	standardBangoRegex = regexp.MustCompile(`^[A-Za-z]{3,5}-\d{3,4}$`)
	fc2BangoRegex      = regexp.MustCompile(`^(?i:FC2|FC2-PPV)-\d+$`)
)

// MatchByRules performs Stage 1 rule-based classification according to M8 ordering.
// Returns (result, matched) where matched is true if high-confidence rule was hit.
func MatchByRules(files []string, metadata map[string]interface{}) (model.ClassifierResult, bool) {
	// 1. organizer_category fault-tolerant parsing (M8a)
	if metadata != nil {
		if rawVal, ok := metadata["organizer_category"]; ok && rawVal != nil {
			if cats := parseOrganizerCategories(rawVal); len(cats) == 1 {
				log.Printf("stage1 rule match: organizer_category=%v -> category=%s", rawVal, cats[0])
				return model.ClassifierResult{
					Category: cats[0],
					NeedLLM:  false,
				}, true
			} else if len(cats) > 1 {
				// Multi-valued organizer_category means the upstream source
				// itself is unsure (e.g. ["bango_porn","porn"]): it is not a
				// high-confidence signal, so degrade to the LLM classifier.
				log.Printf("stage1 rule match: ambiguous organizer_category=%v (%s), degrading to LLM classification", rawVal, cats)
			} else {
				log.Printf("organizer_category provided but no valid Category found in: %v", rawVal)
			}
		}
	}

	// 2. Clear external metadata identifier: dmm_id -> bango_porn
	if metadata != nil {
		if dmmID, ok := metadata["dmm_id"]; ok && dmmID != nil {
			dmmStr := strings.TrimSpace(toString(dmmID))
			if dmmStr != "" {
				log.Printf("stage1 rule match: metadata dmm_id=%q -> category=bango_porn", dmmStr)
				entities := map[string]interface{}{
					"dmm_id": dmmStr,
				}
				return model.ClassifierResult{
					Category: model.CategoryBangoPorn,
					NeedLLM:  false,
					Entities: entities,
				}, true
			}
		}
	}

	if len(files) == 0 {
		return model.ClassifierResult{}, false
	}

	// 3. Pure eBook extensions -> book
	if allMatchExtensions(files, bookExtensions) {
		log.Printf("stage1 rule match: all files are eBook extensions -> category=book")
		return model.ClassifierResult{
			Category: model.CategoryBook,
			NeedLLM:  false,
		}, true
	}

	// 4. Pure audio disambiguation degradation (M8b Plan A)
	// When all files are audio, do NOT blindly return music! Return unmatched to let LLM disambiguate.
	if allMatchExtensions(files, audioExtensions) {
		return model.ClassifierResult{}, false
	}

	// 5. Strong feature naming:
	// Single main video file and filename precisely matches anchored standard bango regex.
	mainVideos := filterMainVideos(files)
	if len(mainVideos) == 1 {
		base := filepath.Base(mainVideos[0])
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)

		if standardBangoRegex.MatchString(stem) || fc2BangoRegex.MatchString(stem) {
			log.Printf("stage1 rule match: filename %q matches bango pattern -> category=bango_porn", base)
			entities := map[string]interface{}{
				"bango": strings.ToUpper(stem),
			}
			return model.ClassifierResult{
				Category: model.CategoryBangoPorn,
				NeedLLM:  false,
				Entities: entities,
			}, true
		}
	}

	// 6. No rule hit -> fallback to LLM
	return model.ClassifierResult{}, false
}

// parseOrganizerCategories collects the distinct valid categories from raw,
// preserving first-seen order. A single result is a high-confidence rule hit;
// multiple results signal upstream ambiguity that must be resolved by the LLM.
func parseOrganizerCategories(raw any) []model.Category {
	var candidates []model.Category
	appendValid := func(cat model.Category) {
		if !isValidCategory(cat) {
			return
		}
		for _, existing := range candidates {
			if existing == cat {
				return
			}
		}
		candidates = append(candidates, cat)
	}

	switch v := raw.(type) {
	case model.Category:
		appendValid(v)
	case string:
		appendValid(model.Category(strings.TrimSpace(v)))
	case []model.Category:
		for _, item := range v {
			appendValid(item)
		}
	case []string:
		for _, item := range v {
			appendValid(model.Category(strings.TrimSpace(item)))
		}
	case []interface{}:
		for _, item := range v {
			if strVal, ok := item.(string); ok {
				appendValid(model.Category(strings.TrimSpace(strVal)))
			} else if catVal, ok := item.(model.Category); ok {
				appendValid(catVal)
			}
		}
	}
	return candidates
}

func isValidCategory(c model.Category) bool {
	for _, recognized := range model.AllCategories {
		if recognized != model.CategoryUnknown && recognized == c {
			return true
		}
	}
	return false
}

func allMatchExtensions(files []string, extMap map[string]struct{}) bool {
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if _, ok := extMap[ext]; !ok {
			return false
		}
	}
	return true
}

func filterMainVideos(files []string) []string {
	var videos []string
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f))
		if _, ok := videoExtensions[ext]; ok {
			videos = append(videos, f)
		}
	}
	return videos
}

func toString(val any) string {
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}
