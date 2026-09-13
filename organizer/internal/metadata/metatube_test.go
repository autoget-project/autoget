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

func TestMetatubeClient_SearchJapanesePorn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiKey     string
		handler    http.HandlerFunc
		wantResult []JAV
		wantErr    bool
	}{
		{
			name: "plain result without details",
			handler: func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/movies/search", r.URL.Path)
				assert.Equal(t, "SSIS-698", r.URL.Query().Get("q"))
				assert.Empty(t, r.Header.Get("Authorization"))

				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{
							"id":           "javdb:ssis-698",
							"number":       "SSIS-698",
							"title":        "SSIS-698 作品",
							"provider":     "javdb",
							"actors":       []string{"吉根ゆりあ"},
							"release_date": "2022-01-01",
						},
					},
				})
			},
			wantResult: []JAV{
				{
					Number:      "SSIS-698",
					Title:       "SSIS-698 作品",
					Provider:    "javdb",
					Actors:      []string{"吉根ゆりあ"},
					ReleaseDate: "2022-01-01",
				},
			},
		},
		{
			name:   "avbase result enriched with details and bearer auth",
			apiKey: "secret",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/movies/AVBASE/ssis-698" {
					assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"data": map[string]interface{}{
							"maker":  "S1 NO.1 STYLE",
							"label":  "S1",
							"series": "SSIS",
							"genres": []string{"巨乳", "単体作品"},
						},
					})
					return
				}

				assert.Equal(t, "/v1/movies/search", r.URL.Path)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []map[string]interface{}{
						{
							"id":       "ssis-698",
							"number":   "SSIS-698",
							"title":    "SSIS-698 AVBASE",
							"provider": "AVBASE",
						},
					},
				})
			},
			wantResult: []JAV{
				{
					Number:   "SSIS-698",
					Title:    "SSIS-698 AVBASE",
					Provider: "AVBASE",
					Maker:    "S1 NO.1 STYLE",
					Label:    "S1",
					Series:   "SSIS",
					Tags:     []string{"巨乳", "単体作品"},
				},
			},
		},
		{
			name: "empty data",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": []map[string]interface{}{}})
			},
			wantResult: []JAV{},
		},
		{
			name: "http error status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			wantErr: true,
		},
		{
			name: "invalid json body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not-json"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			t.Cleanup(server.Close)

			client := NewMetatube(server.URL, tt.apiKey)
			got, err := client.SearchJapanesePorn(context.Background(), "SSIS-698")

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResult, got)
		})
	}
}
