package stage2enricher

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

var (
	yearRegex  = regexp.MustCompile(`\b(19\d\d|20\d\d)\b`)
	bangoRegex = regexp.MustCompile(`(?i)([A-Z]{2,5}-\d{2,7}|FC2(?:-PPV)?-\d{4,8}|(?:MD|MDCM|MDHG|MDHT|MDL|MDSR|MSD)-\d{2,7})`)
)

// madouLabels is the exact Madou label prefix set.
var madouLabels = map[string]struct{}{
	"MD": {}, "MDCM": {}, "MDHG": {}, "MDHT": {}, "MDL": {}, "MDSR": {}, "MSD": {},
}

// isMadouBango reports whether the bango's hyphen-prefixed label belongs to the
// exact Madou label set. A bare HasPrefix("MD") would misclassify mainstream
// JAV labels such as MIDE/MIDD/MDBK/MDYD.
func isMadouBango(bango string) bool {
	prefix, _, _ := strings.Cut(strings.ToUpper(bango), "-")
	_, ok := madouLabels[prefix]
	return ok
}

// TMDBSource is the movie/TV metadata source consumed by Stage 2
// (implemented by metadata.TMDBClient).
type TMDBSource interface {
	SearchMovies(ctx context.Context, title string) ([]metadata.Movie, error)
	SearchTVShows(ctx context.Context, title string) ([]metadata.TVShow, error)
	FindByIMDbID(ctx context.Context, imdbID string) (metadata.FindResult, error)
}

// JAVSource is the JAV metadata source consumed by Stage 2
// (implemented by metadata.MetatubeClient).
type JAVSource interface {
	SearchJapanesePorn(ctx context.Context, bango string) ([]metadata.JAV, error)
}

// Enricher orchestrates Stage 2 metadata retrieval and degradation protection (M6).
type Enricher struct {
	tmdb       TMDBSource
	jav        JAVSource
	actorStore *ActorStore
	aiProvider ai.Provider
}

// NewEnricher creates a new Enricher instance. tmdb and jav may be nil, in
// which case the corresponding lookups degrade to local filename metadata.
func NewEnricher(tmdb TMDBSource, jav JAVSource, actorStore *ActorStore, aiProvider ai.Provider) *Enricher {
	return &Enricher{
		tmdb:       tmdb,
		jav:        jav,
		actorStore: actorStore,
		aiProvider: aiProvider,
	}
}

// EnricherDetail captures diagnostic details and degradation warnings from Stage 2 enrichment.
type EnricherDetail struct {
	TMDBHit         bool     `json:"tmdb_hit"`
	MetaTubeHit     bool     `json:"metatube_hit"`
	ActorStoreHit   bool     `json:"actor_store_hit"`
	DegradeWarnings []string `json:"degrade_warnings"`
}

// Enrich enriches metadata according to media Category, applying graceful degradation on failures (M6).
func (e *Enricher) Enrich(ctx context.Context, cat model.Category, files []string, metadata map[string]interface{}, entities map[string]interface{}) (model.EnrichedMetadata, error) {
	meta, _, err := e.EnrichWithDetail(ctx, cat, files, metadata, entities)
	return meta, err
}

// EnrichWithDetail enriches metadata and returns detailed diagnostics and degradation warnings.
func (e *Enricher) EnrichWithDetail(ctx context.Context, cat model.Category, files []string, metadata map[string]interface{}, entities map[string]interface{}) (model.EnrichedMetadata, EnricherDetail, error) {
	var enriched model.EnrichedMetadata
	var detail EnricherDetail
	var err error
	switch cat {
	case model.CategoryMovie:
		enriched, detail, err = e.enrichMovieWithDetail(ctx, files, metadata, entities)
	case model.CategoryTVSeries:
		enriched, detail, err = e.enrichTVSeriesWithDetail(ctx, files, metadata, entities)
	case model.CategoryBangoPorn:
		enriched, detail, err = e.enrichBangoPornWithDetail(ctx, files, metadata, entities)
	case model.CategoryPorn:
		enriched, err = e.enrichPorn(ctx, files, metadata, entities)
	default:
		// simple categories (book, music, photobook, audio_book, music_video) or unknown: skip Stage 2
		return model.EnrichedMetadata{
			Language: model.LanguageOthers,
		}, detail, nil
	}
	return enriched, detail, err
}

func (e *Enricher) enrichMovieWithDetail(ctx context.Context, files []string, metadata map[string]interface{}, entities map[string]interface{}) (model.EnrichedMetadata, EnricherDetail, error) {
	var enriched model.EnrichedMetadata
	var detail EnricherDetail
	enriched.Language = model.LanguageOthers

	imdbID := getIMDbID(metadata, entities)
	titleCandidate := getTitleCandidate(files, metadata, entities)

	// Step 1: Try find_by_imdb_id if imdbID is present
	var movieFound bool
	if imdbID != "" && e.tmdb != nil {
		res, err := e.tmdb.FindByIMDbID(ctx, imdbID)
		if err == nil {
			if len(res.Movies) > 0 {
				movieFound = true
				detail.TMDBHit = true
				e.populateMovieFromTMDB(&enriched, res.Movies[0])
			}
		} else {
			warn := fmt.Sprintf("tmdb find_by_imdb_id failed for movie (%s): %v, falling back to title search", imdbID, err)
			detail.DegradeWarnings = append(detail.DegradeWarnings, warn)
		}
	}

	// Step 2: Fallback to search_movies by title
	if !movieFound && titleCandidate != "" && e.tmdb != nil {
		movies, err := e.tmdb.SearchMovies(ctx, titleCandidate)
		if err == nil {
			if len(movies) > 0 {
				movieFound = true
				detail.TMDBHit = true
				e.populateMovieFromTMDB(&enriched, movies[0])
			}
		} else {
			warn := fmt.Sprintf("tmdb search_movies failed for (%s): %v", titleCandidate, err)
			detail.DegradeWarnings = append(detail.DegradeWarnings, warn)
		}
	}

	// Step 3: Final fallback using cleaned name from stage 1 or filenames
	if !movieFound {
		if enriched.Title == "" {
			enriched.Title = titleCandidate
		}
		if enriched.Year == 0 {
			enriched.Year = extractYear(files, metadata)
		}
	}

	// Double-check is_anim indicators
	if !enriched.IsAnim {
		enriched.IsAnim = detectIsAnim(files, metadata, enriched.Title)
	}

	return enriched, detail, nil
}

func (e *Enricher) enrichTVSeriesWithDetail(ctx context.Context, files []string, metadata map[string]interface{}, entities map[string]interface{}) (model.EnrichedMetadata, EnricherDetail, error) {
	var enriched model.EnrichedMetadata
	var detail EnricherDetail
	enriched.Language = model.LanguageOthers

	imdbID := getIMDbID(metadata, entities)
	titleCandidate := getTitleCandidate(files, metadata, entities)

	var tvFound bool
	if imdbID != "" && e.tmdb != nil {
		res, err := e.tmdb.FindByIMDbID(ctx, imdbID)
		if err == nil {
			if len(res.TVs) > 0 {
				tvFound = true
				detail.TMDBHit = true
				e.populateTVFromTMDB(&enriched, res.TVs[0])
			}
		} else {
			warn := fmt.Sprintf("tmdb find_by_imdb_id failed for tv_series (%s): %v, falling back to title search", imdbID, err)
			detail.DegradeWarnings = append(detail.DegradeWarnings, warn)
		}
	}

	// Fallback to search_tv_shows
	if !tvFound && titleCandidate != "" && e.tmdb != nil {
		tvs, err := e.tmdb.SearchTVShows(ctx, titleCandidate)
		if err == nil {
			if len(tvs) > 0 {
				tvFound = true
				detail.TMDBHit = true
				e.populateTVFromTMDB(&enriched, tvs[0])
			}
		} else {
			warn := fmt.Sprintf("tmdb search_tv_shows failed for (%s): %v", titleCandidate, err)
			detail.DegradeWarnings = append(detail.DegradeWarnings, warn)
		}
	}

	if !tvFound {
		if enriched.Title == "" {
			enriched.Title = titleCandidate
		}
		if enriched.Year == 0 {
			enriched.Year = extractYear(files, metadata)
		}
	}

	if !enriched.IsAnim {
		enriched.IsAnim = detectIsAnim(files, metadata, enriched.Title)
	}

	return enriched, detail, nil
}

func (e *Enricher) enrichBangoPornWithDetail(ctx context.Context, files []string, metadata map[string]interface{}, entities map[string]interface{}) (model.EnrichedMetadata, EnricherDetail, error) {
	var enriched model.EnrichedMetadata
	var detail EnricherDetail
	enriched.Language = model.LanguageJapanese

	searchKey := getBangoCandidate(files, metadata, entities)
	bangoCandidate := searchKey

	// Search JAV info via the local Metatube client
	if searchKey != "" && e.jav != nil {
		javs, err := e.jav.SearchJapanesePorn(ctx, searchKey)
		if err == nil {
			if len(javs) > 0 {
				detail.MetaTubeHit = true
				jav := javs[0]
				for _, a := range jav.Actors {
					if actStr := strings.TrimSpace(a); actStr != "" {
						enriched.Actors = append(enriched.Actors, actStr)
					}
				}
				enriched.Maker = jav.Maker
				enriched.Title = jav.Title
			}
		} else {
			warn := fmt.Sprintf("metatube search_japanese_porn failed for (%s): %v", searchKey, err)
			detail.DegradeWarnings = append(detail.DegradeWarnings, warn)
		}
	}

	if fb := bangoFromFiles(files); fb != "" {
		bangoCandidate = fb
	}
	enriched.Bango = bangoCandidate

	if isMadouBango(bangoCandidate) {
		enriched.FromMadou = true
		enriched.Language = model.LanguageChinese
	}

	if len(enriched.Actors) == 0 {
		if actorArr := toStringSlice(entities["actors"]); len(actorArr) > 0 {
			enriched.Actors = actorArr
		} else if actorArr := toStringSlice(metadata["actors"]); len(actorArr) > 0 {
			enriched.Actors = actorArr
		}
	}

	if !enriched.IsVR {
		if v, ok := boolField(entities, "is_vr"); ok {
			enriched.IsVR = v
		} else if v, ok := boolField(metadata, "is_vr"); ok {
			enriched.IsVR = v
		}
	}
	if !enriched.IsVR && bangoLabelEndsInVR(bangoCandidate) {
		enriched.IsVR = true
	}

	// ActorStore directory resolution & maintenance
	if e.actorStore != nil {
		if len(enriched.Actors) > 0 {
			actorDir, err := e.actorStore.SearchAndEnrichActor(ctx, enriched.Actors)
			if err == nil && actorDir != "" {
				detail.ActorStoreHit = true
				enriched.Actors = append([]string{actorDir}, enriched.Actors...)
			}
		}
	}

	return enriched, detail, nil
}

func (e *Enricher) enrichPorn(ctx context.Context, files []string, metadata map[string]interface{}, entities map[string]interface{}) (model.EnrichedMetadata, error) {
	var enriched model.EnrichedMetadata
	enriched.Language = model.LanguageEnglish

	titleCandidate := getTitleCandidate(files, metadata, entities)
	enriched.Title = titleCandidate

	// VR is a semantic property decided by Stage 1 (web-search grounded LLM
	// reasoning) or declared by the upstream metadata. The filename is never
	// pattern-matched for VR markers here.
	if v, ok := boolField(entities, "is_vr"); ok {
		enriched.IsVR = v
	} else if v, ok := boolField(metadata, "is_vr"); ok {
		enriched.IsVR = v
	}
	return enriched, nil
}

// boolField reads a tolerant boolean (bool, or a "true"/"false"-style string
// after a JSON round-trip) from a metadata/entities map.
func boolField(m map[string]interface{}, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "1", "yes", "y":
			return true, true
		case "false", "0", "no", "n", "":
			return false, true
		}
	case float64:
		return t != 0, true
	}
	return false, false
}

// bangoLabelEndsInVR reports whether the canonical bango's label segment ends
// in VR — the JAV VR series numbering convention (IPVR-002, SIVR-...,
// HNVR-...). This is a property of the bango code itself, not a filename
// pattern heuristic.
func bangoLabelEndsInVR(bango string) bool {
	m := bangoRegex.FindString(strings.ToUpper(bango))
	if m == "" {
		return false
	}
	label, _, _ := strings.Cut(m, "-")
	return strings.HasSuffix(label, "VR")
}

// toStringSlice normalizes a value that is either []string or a JSON-round-
// tripped []interface{} of strings.
func toStringSlice(v interface{}) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []interface{}:
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// applyYearFromDate parses the leading year from a TMDB date field.
func applyYearFromDate(enriched *model.EnrichedMetadata, date string) {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			enriched.Year = y
		}
	}
}

// populateMovieFromTMDB fills EnrichedMetadata from a TMDB movie result.
func (e *Enricher) populateMovieFromTMDB(enriched *model.EnrichedMetadata, m metadata.Movie) {
	if m.Title != "" {
		enriched.Title = m.Title
	}
	enriched.OriginalTitle = m.OriginalTitle
	applyYearFromDate(enriched, m.ReleaseDate)
	if m.OriginalLanguage != "" {
		enriched.Language = model.ISO639ToLanguage(m.OriginalLanguage)
	}
	enriched.IsAnim = m.IsAnimation()
}

// populateTVFromTMDB fills EnrichedMetadata from a TMDB TV result.
func (e *Enricher) populateTVFromTMDB(enriched *model.EnrichedMetadata, t metadata.TVShow) {
	if t.Name != "" {
		enriched.Title = t.Name
	}
	enriched.OriginalTitle = t.OriginalName
	applyYearFromDate(enriched, t.FirstAirDate)
	if t.OriginalLanguage != "" {
		enriched.Language = model.ISO639ToLanguage(t.OriginalLanguage)
	}
	enriched.IsAnim = t.IsAnimation()
}

func getIMDbID(metadata map[string]interface{}, entities map[string]interface{}) string {
	if entities != nil {
		if id, ok := entities["imdb_id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	if metadata != nil {
		if id, ok := metadata["imdb_id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

func getTitleCandidate(files []string, metadata map[string]interface{}, entities map[string]interface{}) string {
	if entities != nil {
		if t, ok := entities["clean_title"].(string); ok && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t)
		}
	}
	if metadata != nil {
		if t, ok := metadata["title"].(string); ok && strings.TrimSpace(t) != "" {
			return strings.TrimSpace(t)
		}
	}
	if len(files) > 0 {
		base := filepath.Base(files[0])
		ext := filepath.Ext(base)
		return strings.TrimSuffix(base, ext)
	}
	return ""
}

func getBangoCandidate(files []string, metadata map[string]interface{}, entities map[string]interface{}) string {
	if entities != nil {
		if b, ok := entities["bango"].(string); ok && strings.TrimSpace(b) != "" {
			return strings.ToUpper(strings.TrimSpace(b))
		}
		if dmm, ok := entities["dmm_id"].(string); ok && strings.TrimSpace(dmm) != "" {
			return strings.ToUpper(strings.TrimSpace(dmm))
		}
	}
	if metadata != nil {
		if dmm, ok := metadata["dmm_id"].(string); ok && strings.TrimSpace(dmm) != "" {
			return strings.ToUpper(strings.TrimSpace(dmm))
		}
	}
	return bangoFromFiles(files)
}

// bangoFromFiles extracts the canonical hyphenated bango from the filenames.
func bangoFromFiles(files []string) string {
	for _, f := range files {
		match := bangoRegex.FindString(filepath.Base(f))
		if match != "" {
			return strings.ToUpper(match)
		}
	}
	return ""
}

func extractYear(files []string, metadata map[string]interface{}) int {
	if metadata != nil {
		if y, ok := metadata["year"].(int); ok && y > 0 {
			return y
		}
		if y, ok := metadata["year"].(float64); ok && y > 0 {
			return int(y)
		}
	}
	for _, f := range files {
		matches := yearRegex.FindAllString(f, -1)
		for _, m := range matches {
			if y, err := strconv.Atoi(m); err == nil && y >= 1950 && y <= 2035 {
				return y
			}
		}
	}
	return 0
}

func detectIsAnim(files []string, metadata map[string]interface{}, title string) bool {
	checkList := []string{title}
	if metadata != nil {
		if desc, ok := metadata["description"].(string); ok {
			checkList = append(checkList, desc)
		}
		if genre, ok := metadata["genre"].(string); ok {
			checkList = append(checkList, genre)
		}
	}
	checkList = append(checkList, files...)

	for _, text := range checkList {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "anime") || strings.Contains(lower, "animation") ||
			strings.Contains(lower, "动画") || strings.Contains(lower, "動漫") ||
			strings.Contains(lower, "新番") {
			return true
		}
	}
	return false
}
