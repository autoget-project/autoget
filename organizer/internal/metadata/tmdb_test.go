package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTMDBTestClient wires a TMDBClient against a fake API server built from
// per-path handlers.
func newTMDBTestClient(t *testing.T, handler http.HandlerFunc) *TMDBClient {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := NewTMDB("test-key", "zh-CN")
	client.baseURL = server.URL
	return client
}

func TestTMDBClient_SearchMovies(t *testing.T) {
	t.Parallel()

	client := newTMDBTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search/movie", r.URL.Path)
		q := r.URL.Query()
		assert.Equal(t, "test-key", q.Get("api_key"))
		assert.Equal(t, "zh-CN", q.Get("language"))
		assert.Equal(t, "Spirited Away", q.Get("query"))
		assert.Equal(t, "true", q.Get("include_adult"))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"title":             "千与千寻",
					"original_title":    "千と千尋の神隠し",
					"original_language": "ja",
					"release_date":      "2001-07-20",
					"genre_ids":         []int{16, 10751},
				},
			},
		})
	})

	movies, err := client.SearchMovies(context.Background(), "Spirited Away")
	require.NoError(t, err)
	require.Len(t, movies, 1)

	assert.Equal(t, "千与千寻", movies[0].Title)
	assert.Equal(t, "千と千尋の神隠し", movies[0].OriginalTitle)
	assert.Equal(t, "ja", movies[0].OriginalLanguage)
	assert.Equal(t, "2001-07-20", movies[0].ReleaseDate)
	assert.True(t, movies[0].IsAnimation())
}

func TestTMDBClient_SearchTVShows(t *testing.T) {
	t.Parallel()

	client := newTMDBTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search/tv", r.URL.Path)
		assert.Equal(t, "Severance", r.URL.Query().Get("query"))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{
					"name":              "人生切割术",
					"original_name":     "Severance",
					"original_language": "en",
					"first_air_date":    "2022-02-18",
					"genre_ids":         []int{18, 9648},
				},
			},
		})
	})

	tvs, err := client.SearchTVShows(context.Background(), "Severance")
	require.NoError(t, err)
	require.Len(t, tvs, 1)

	assert.Equal(t, "人生切割术", tvs[0].Name)
	assert.Equal(t, "Severance", tvs[0].OriginalName)
	assert.Equal(t, "2022-02-18", tvs[0].FirstAirDate)
	assert.False(t, tvs[0].IsAnimation())
}

func TestTMDBClient_FindByIMDbID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		payload   string
		wantTVLen int
		wantMovie bool
	}{
		{
			name: "movie and tv hits",
			payload: `{
				"movie_results": [{"title": "Inception", "release_date": "2010-07-16", "genre_ids": [28]}],
				"tv_results": [{"name": "Inception: The Series", "first_air_date": "2021-01-01"}],
				"tv_episode_results": [],
				"tv_season_results": []
			}`,
			wantTVLen: 1,
			wantMovie: true,
		},
		{
			name: "episode hit resolves parent series",
			payload: `{
				"movie_results": [],
				"tv_results": [],
				"tv_episode_results": [{"show_id": 94605}],
				"tv_season_results": []
			}`,
			wantTVLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTMDBTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/find/tt1375666":
					assert.Equal(t, "imdb_id", r.URL.Query().Get("external_source"))
					_, _ = w.Write([]byte(tt.payload))
				case "/tv/94605":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"name":              "人生切割术",
						"original_name":     "Severance",
						"original_language": "en",
						"first_air_date":    "2022-02-18",
						"genres":            []map[string]interface{}{{"id": 18, "name": "剧情"}},
					})
				default:
					http.NotFound(w, r)
				}
			})

			res, err := client.FindByIMDbID(context.Background(), "tt1375666")
			require.NoError(t, err)
			assert.Len(t, res.TVs, tt.wantTVLen)

			if tt.wantMovie {
				require.Len(t, res.Movies, 1)
				assert.Equal(t, "Inception", res.Movies[0].Title)
				assert.False(t, res.Movies[0].IsAnimation())
			}
		})
	}
}

func TestTMDBClient_IsAnimationFromDetailGenres(t *testing.T) {
	t.Parallel()

	// Detail endpoints return named genres instead of genre_ids.
	tv := TVShow{
		Name:   "Severance",
		Genres: []Genre{{ID: 16, Name: "动画"}},
	}
	assert.True(t, tv.IsAnimation())
}

func TestTMDBClient_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "http error status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			},
		},
		{
			name: "invalid json body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not-json"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTMDBTestClient(t, tt.handler)

			_, err := client.SearchMovies(context.Background(), "any")
			require.Error(t, err)

			_, err = client.SearchTVShows(context.Background(), "any")
			require.Error(t, err)

			_, err = client.FindByIMDbID(context.Background(), "tt0000001")
			require.Error(t, err)
		})
	}
}
