package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tpdbCounts records how many times each TPDB endpoint was hit within a case.
type tpdbCounts struct {
	scenes int
	movies int
}

type tpdbTestCase struct {
	name            string
	handler         func(t *testing.T, w http.ResponseWriter, r *http.Request, c *tpdbCounts)
	counts          tpdbCounts
	wantCounts      *tpdbCounts
	wantResult      []TPDBVideo
	wantErr         bool
	wantErrContains string
}

// tpdbItem builds a minimal TPDB candidate JSON object with only the
// slug/title/type fields populated.
func tpdbItem(slug, title, typ string) map[string]any {
	return map[string]any{"slug": slug, "title": title, "type": typ}
}

// writeTPDBResponse encodes a TPDB-shaped search response body.
func writeTPDBResponse(t *testing.T, w http.ResponseWriter, data []map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func TestThePornDBClient_SearchVideos(t *testing.T) {
	t.Parallel()

	tests := []tpdbTestCase{
		{
			name: "scenes and movies merged with per-type trimming and field mapping",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, c *tpdbCounts) {
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				assert.Equal(t, "Agatha Vega", r.URL.Query().Get("q"))
				switch r.URL.Path {
				case "/scenes":
					c.scenes++
					items := []map[string]any{{
						"slug":       "tushyraw-stunning-agatha-likes-it-in-the-ass",
						"title":      "Stunning Agatha Likes It In The Ass",
						"type":       "scene",
						"date":       "2026-08-30",
						"site":       map[string]any{"name": "Tushy Raw"},
						"performers": []map[string]any{{"name": "Agatha Vega"}},
						"tags":       []map[string]any{{"name": "Anal"}},
					}}
					for i := 1; i <= 11; i++ {
						items = append(items, tpdbItem(fmt.Sprintf("scene-%02d", i), fmt.Sprintf("Scene %02d", i), "scene"))
					}
					writeTPDBResponse(t, w, items)
				case "/movies":
					c.movies++
					items := []map[string]any{{
						"slug":       "girlsway-runaway-brides-regret",
						"title":      "Runaway Bride's Regret",
						"type":       "movie",
						"date":       "2026-09-06",
						"site":       map[string]any{"name": "GirlsWay"},
						"performers": []map[string]any{{"name": "Agatha Vega"}, {"name": "Kira Noir"}},
					}}
					for i := 1; i <= 2; i++ {
						items = append(items, tpdbItem(fmt.Sprintf("movie-%02d", i), fmt.Sprintf("Movie %02d", i), "movie"))
					}
					writeTPDBResponse(t, w, items)
				default:
					t.Errorf("unexpected tpdb endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			},
			wantCounts: &tpdbCounts{scenes: 1, movies: 1},
			wantResult: []TPDBVideo{
				{
					Slug:       "tushyraw-stunning-agatha-likes-it-in-the-ass",
					Title:      "Stunning Agatha Likes It In The Ass",
					Type:       "scene",
					Date:       "2026-08-30",
					Site:       "Tushy Raw",
					Performers: []string{"Agatha Vega"},
					Tags:       []string{"Anal"},
				},
				{Slug: "scene-01", Title: "Scene 01", Type: "scene"},
				{Slug: "scene-02", Title: "Scene 02", Type: "scene"},
				{Slug: "scene-03", Title: "Scene 03", Type: "scene"},
				{Slug: "scene-04", Title: "Scene 04", Type: "scene"},
				{Slug: "scene-05", Title: "Scene 05", Type: "scene"},
				{Slug: "scene-06", Title: "Scene 06", Type: "scene"},
				{Slug: "scene-07", Title: "Scene 07", Type: "scene"},
				{Slug: "scene-08", Title: "Scene 08", Type: "scene"},
				{Slug: "scene-09", Title: "Scene 09", Type: "scene"},
				{
					Slug:       "girlsway-runaway-brides-regret",
					Title:      "Runaway Bride's Regret",
					Type:       "movie",
					Date:       "2026-09-06",
					Site:       "GirlsWay",
					Performers: []string{"Agatha Vega", "Kira Noir"},
				},
				{Slug: "movie-01", Title: "Movie 01", Type: "movie"},
				{Slug: "movie-02", Title: "Movie 02", Type: "movie"},
			},
		},
		{
			name: "empty results on both endpoints",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, c *tpdbCounts) {
				switch r.URL.Path {
				case "/scenes":
					c.scenes++
				case "/movies":
					c.movies++
				default:
					t.Errorf("unexpected tpdb endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				writeTPDBResponse(t, w, []map[string]any{})
			},
			wantResult: []TPDBVideo{},
		},
		{
			name: "scenes 401 fails whole search",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, c *tpdbCounts) {
				switch r.URL.Path {
				case "/scenes":
					c.scenes++
					http.Error(w, "unauthorized", http.StatusUnauthorized)
				default:
					t.Errorf("unexpected tpdb endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			},
			wantErr:         true,
			wantErrContains: "401",
		},
		{
			name: "movies failure fails whole search after scenes success",
			handler: func(t *testing.T, w http.ResponseWriter, r *http.Request, c *tpdbCounts) {
				switch r.URL.Path {
				case "/scenes":
					c.scenes++
					writeTPDBResponse(t, w, []map[string]any{})
				case "/movies":
					c.movies++
					http.Error(w, "boom", http.StatusInternalServerError)
				default:
					t.Errorf("unexpected tpdb endpoint: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			},
			wantErr:         true,
			wantErrContains: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.handler(t, w, r, &tt.counts)
			}))
			t.Cleanup(server.Close)

			client := NewThePornDB("test-token")
			client.baseURL = server.URL

			got, err := client.SearchVideos(context.Background(), "Agatha Vega")

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)
				if tt.wantErrContains != "" {
					assert.ErrorContains(t, err, tt.wantErrContains)
				}
				return
			}

			require.NoError(t, err)
			if tt.wantCounts != nil {
				assert.Equal(t, *tt.wantCounts, tt.counts)
			}
			assert.Equal(t, tt.wantResult, got)
		})
	}
}
