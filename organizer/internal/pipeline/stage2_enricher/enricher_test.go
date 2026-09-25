package stage2enricher

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/model"
)

type mockTMDBSource struct {
	findByIMDbIDFunc  func(ctx context.Context, imdbID string) (metadata.FindResult, error)
	searchMoviesFunc  func(ctx context.Context, title string) ([]metadata.Movie, error)
	searchTVShowsFunc func(ctx context.Context, title string) ([]metadata.TVShow, error)
}

func (m *mockTMDBSource) FindByIMDbID(ctx context.Context, imdbID string) (metadata.FindResult, error) {
	if m.findByIMDbIDFunc != nil {
		return m.findByIMDbIDFunc(ctx, imdbID)
	}
	return metadata.FindResult{}, nil
}
func (m *mockTMDBSource) SearchMovies(ctx context.Context, title string) ([]metadata.Movie, error) {
	if m.searchMoviesFunc != nil {
		return m.searchMoviesFunc(ctx, title)
	}
	return nil, nil
}
func (m *mockTMDBSource) SearchTVShows(ctx context.Context, title string) ([]metadata.TVShow, error) {
	if m.searchTVShowsFunc != nil {
		return m.searchTVShowsFunc(ctx, title)
	}
	return nil, nil
}

type mockJAVSource struct {
	searchJapanesePornFunc func(ctx context.Context, bango string) ([]metadata.JAV, error)
}

func (m *mockJAVSource) SearchJapanesePorn(ctx context.Context, bango string) ([]metadata.JAV, error) {
	if m.searchJapanesePornFunc != nil {
		return m.searchJapanesePornFunc(ctx, bango)
	}
	return nil, nil
}

func TestEnricher_MovieSuccessAndAnimation(t *testing.T) {
	t.Parallel()

	tmdbMock := &mockTMDBSource{
		findByIMDbIDFunc: func(ctx context.Context, imdbID string) (metadata.FindResult, error) {
			return metadata.FindResult{
				Movies: []metadata.Movie{
					{
						Title:            "Spirited Away",
						ReleaseDate:      "2001-07-20",
						OriginalLanguage: "ja",
						GenreIDs:         []int{16},
					},
				},
			}, nil
		},
	}

	enricher := NewEnricher(tmdbMock, nil, nil, nil)

	res, err := enricher.Enrich(context.Background(), model.CategoryMovie,
		[]string{"Spirited.Away.2001.mkv"}, map[string]interface{}{"imdb_id": "tt0245429"}, nil)
	require.NoError(t, err)

	assert.Equal(t, "Spirited Away", res.Title)
	assert.Equal(t, 2001, res.Year)
	assert.True(t, res.IsAnim)
	assert.Equal(t, model.LanguageJapanese, res.Language)
}

func TestEnricher_DegradationProtection_M6(t *testing.T) {
	t.Parallel()

	// M6: when metadata sources fail or return nothing, the enricher must
	// degrade to filename-derived metadata instead of surfacing a fatal error.
	tmdbMock := &mockTMDBSource{
		findByIMDbIDFunc: func(ctx context.Context, imdbID string) (metadata.FindResult, error) {
			return metadata.FindResult{}, errors.New("network connection refused")
		},
		searchMoviesFunc: func(ctx context.Context, title string) ([]metadata.Movie, error) {
			return nil, errors.New("search error")
		},
	}
	javMock := &mockJAVSource{
		searchJapanesePornFunc: func(ctx context.Context, bango string) ([]metadata.JAV, error) {
			return nil, errors.New("remote timeout")
		},
	}

	store := NewActorStore(filepath.Join(t.TempDir(), "actor.json"), "", nil)
	enricher := NewEnricher(tmdbMock, javMock, store, nil)
	ctx := context.Background()

	// Movie falls back to the filename.
	resMovie, err := enricher.Enrich(ctx, model.CategoryMovie,
		[]string{"Inception.2010.mkv"}, map[string]interface{}{"imdb_id": "tt9999999"}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, resMovie.Title)
	assert.Equal(t, 2010, resMovie.Year)

	// Bango falls back to the filename-derived bango.
	resBango, err := enricher.Enrich(ctx, model.CategoryBangoPorn, []string{"SSIS-001.mp4"}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "SSIS-001", resBango.Bango)
}

func TestEnricher_BangoVRAndMadou(t *testing.T) {
	t.Parallel()

	enricher := NewEnricher(nil, nil, nil, nil)
	ctx := context.Background()

	// Madou label detection.
	resMadou, err := enricher.Enrich(ctx, model.CategoryBangoPorn, []string{"MDCM-0005.mp4"}, nil, nil)
	require.NoError(t, err)
	assert.True(t, resMadou.FromMadou)
	assert.Equal(t, model.LanguageChinese, resMadou.Language)

	// Every exact madou label prefix must be recognized.
	for _, bango := range []string{"MD-003", "MDSR-001", "MDHG-012", "MDHT-004", "MDL-002", "MSD-0001"} {
		res, err := enricher.Enrich(ctx, model.CategoryBangoPorn, []string{bango + ".mp4"}, nil, nil)
		require.NoError(t, err)
		assert.True(t, res.FromMadou, "expected %s to be Madou", bango)
		assert.Equal(t, model.LanguageChinese, res.Language, "expected %s to be Chinese", bango)
	}

	// Mainstream JAV labels starting with "MD" must NOT be flagged as Madou.
	for _, bango := range []string{"MIDE-612", "MIDD-826", "MDBK-018", "MDYD-528", "MDX-001"} {
		res, err := enricher.Enrich(ctx, model.CategoryBangoPorn, []string{bango + ".mp4"}, nil, nil)
		require.NoError(t, err)
		assert.False(t, res.FromMadou, "expected %s NOT to be Madou", bango)
		assert.Equal(t, model.LanguageJapanese, res.Language, "expected %s to keep Japanese", bango)
	}

	// VR detection.
	resVR, err := enricher.Enrich(ctx, model.CategoryBangoPorn, []string{"IPVR-002.mp4"}, nil, nil)
	require.NoError(t, err)
	assert.True(t, resVR.IsVR)
}

func TestEnricher_PornVRFromDeclaredMetadata(t *testing.T) {
	t.Parallel()

	enricher := NewEnricher(nil, nil, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name     string
		files    []string
		metadata map[string]interface{}
		entities map[string]interface{}
		wantVR   bool
	}{
		// VR is declared by the upstream request metadata (the deterministic
		// boundary signal a downloader can attach to a known VR release).
		{"metadata declares vr", []string{"SlrOriginals.26.12.25.Christmas.Special.mp4"}, map[string]interface{}{"is_vr": true}, nil, true},
		// VR is declared by the Stage 1 entities (web-search grounded LLM
		// reasoning: SearchContext.is_vr -> checker/arbiter entities).
		{"entities declare vr", []string{"Wankz.26.01.01.Scene.XXX.mp4"}, nil, map[string]interface{}{"is_vr": true}, true},
		// String-typed true survives a JSON round-trip through the payload.
		{"string true tolerated", []string{"x.mp4"}, nil, map[string]interface{}{"is_vr": "true"}, true},
		{"explicit false stays false", []string{"x.mp4"}, map[string]interface{}{"is_vr": false}, nil, false},
		// No declaration: the filename is NEVER sniffed for VR markers — a VR
		// token or a JAV-style VR label inside a porn filename is not evidence
		// on its own.
		{"no declaration despite vr-looking name", []string{"SlrOriginals.26.12.25.Christmas.Special.VR.XXX.1080p.mp4"}, nil, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := enricher.Enrich(ctx, model.CategoryPorn, tt.files, tt.metadata, tt.entities)
			require.NoError(t, err)
			assert.Equal(t, tt.wantVR, res.IsVR, "files %v metadata %v entities %v", tt.files, tt.metadata, tt.entities)
		})
	}
}

func TestEnricher_BangoDmmKeyFallsBackToFilenameBango(t *testing.T) {
	t.Parallel()

	// Spec M6: the dmm_id is only a search key; when the JAV search fails or
	// returns nothing, the final bango must fall back to the filename-derived
	// canonical hyphenated bango.
	tests := []struct {
		name     string
		mockFunc func(ctx context.Context, bango string) ([]metadata.JAV, error)
	}{
		{
			name: "metatube error",
			mockFunc: func(ctx context.Context, bango string) ([]metadata.JAV, error) {
				assert.Equal(t, "PRED00374", bango, "dmm-derived search key")
				return nil, errors.New("remote timeout")
			},
		},
		{
			name: "empty result",
			mockFunc: func(ctx context.Context, bango string) ([]metadata.JAV, error) {
				assert.Equal(t, "PRED00374", bango, "dmm-derived search key")
				return nil, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			enricher := NewEnricher(nil, &mockJAVSource{searchJapanesePornFunc: tt.mockFunc}, nil, nil)

			res, err := enricher.Enrich(
				context.Background(),
				model.CategoryBangoPorn,
				[]string{"PRED-374.mp4"},
				map[string]interface{}{"dmm_id": "pred00374"},
				nil,
			)
			require.NoError(t, err)
			assert.Equal(t, "PRED-374", res.Bango)
		})
	}
}

func TestEnricher_BangoEnrichedFromMetatube(t *testing.T) {
	t.Parallel()

	javMock := &mockJAVSource{
		searchJapanesePornFunc: func(ctx context.Context, bango string) ([]metadata.JAV, error) {
			return []metadata.JAV{
				{
					Number:   "SSIS-698",
					Title:    "SSIS-698 作品",
					Provider: "AVBASE",
					Actors:   []string{" 吉根ゆりあ ", "", "八掛うみ"},
					Maker:    "S1 NO.1 STYLE",
				},
			}, nil
		},
	}

	enricher := NewEnricher(nil, javMock, nil, nil)

	res, err := enricher.Enrich(
		context.Background(),
		model.CategoryBangoPorn,
		[]string{"SSIS-698.mp4"},
		nil,
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "SSIS-698", res.Bango)
	assert.Equal(t, "SSIS-698 作品", res.Title)
	assert.Equal(t, "S1 NO.1 STYLE", res.Maker)
	assert.Equal(t, []string{"吉根ゆりあ", "八掛うみ"}, res.Actors)
}

func TestEnricher_MovieTitleSearchFallback(t *testing.T) {
	t.Parallel()

	// No IMDb id: the enricher must go straight to the title search.
	tmdbMock := &mockTMDBSource{
		searchMoviesFunc: func(ctx context.Context, title string) ([]metadata.Movie, error) {
			assert.Equal(t, "Inception", title)
			return []metadata.Movie{
				{Title: "盗梦空间", OriginalTitle: "Inception", ReleaseDate: "2010-07-16", OriginalLanguage: "en"},
			}, nil
		},
	}

	enricher := NewEnricher(tmdbMock, nil, nil, nil)

	res, err := enricher.Enrich(
		context.Background(),
		model.CategoryMovie,
		[]string{"Inception.2010.mkv"},
		map[string]interface{}{"title": "Inception"},
		nil,
	)
	require.NoError(t, err)

	assert.Equal(t, "盗梦空间", res.Title)
	assert.Equal(t, "Inception", res.OriginalTitle)
	assert.Equal(t, 2010, res.Year)
	assert.Equal(t, model.LanguageEnglish, res.Language)
}

func TestEnricher_EnrichWithDetail(t *testing.T) {
	t.Parallel()

	tmdbMock := &mockTMDBSource{
		findByIMDbIDFunc: func(ctx context.Context, imdbID string) (metadata.FindResult, error) {
			return metadata.FindResult{}, errors.New("tmdb down")
		},
		searchMoviesFunc: func(ctx context.Context, title string) ([]metadata.Movie, error) {
			return nil, errors.New("search down")
		},
	}

	enricher := NewEnricher(tmdbMock, nil, nil, nil)
	meta, detail, err := enricher.EnrichWithDetail(
		context.Background(),
		model.CategoryMovie,
		[]string{"Inception.2010.mkv"},
		map[string]interface{}{"imdb_id": "tt1375666"},
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, "Inception.2010", meta.Title)
	assert.Equal(t, 2010, meta.Year)
	assert.False(t, detail.TMDBHit)
	assert.Len(t, detail.DegradeWarnings, 2)
}
