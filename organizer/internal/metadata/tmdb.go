// Package metadata provides the local metadata source clients used by Stage 2
// enrichment: TMDB for movies/TV shows and Metatube for JAV content. They call
// the upstream APIs directly, replacing the former remote metadata-mcp server.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"
)

// DefaultTimeout is the default request timeout for metadata API calls.
const DefaultTimeout = 30 * time.Second

// maxResponseBytes bounds response body reads (~10MiB) to protect against a
// misbehaving upstream streaming unbounded data.
const maxResponseBytes = 10 << 20

// tmdbGenreAnimation is the TMDB genre id for Animation (movies and TV).
const tmdbGenreAnimation = 16

const tmdbDefaultBaseURL = "https://api.themoviedb.org/3"

// TMDBClient queries The Movie Database API v3.
type TMDBClient struct {
	apiKey     string
	language   string
	baseURL    string
	httpClient *http.Client
}

// NewTMDB creates a TMDB client. language is an ISO locale such as "zh-CN"
// controlling the localization of returned titles.
func NewTMDB(apiKey, language string) *TMDBClient {
	return &TMDBClient{
		apiKey:     apiKey,
		language:   language,
		baseURL:    tmdbDefaultBaseURL,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
}

// Movie is a TMDB movie search/find result.
type Movie struct {
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"original_title"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	ReleaseDate      string  `json:"release_date"`
	GenreIDs         []int   `json:"genre_ids"`
	Genres           []Genre `json:"genres"`
}

// TVShow is a TMDB TV search/find result.
type TVShow struct {
	Name             string  `json:"name"`
	OriginalName     string  `json:"original_name"`
	OriginalLanguage string  `json:"original_language"`
	Overview         string  `json:"overview"`
	FirstAirDate     string  `json:"first_air_date"`
	GenreIDs         []int   `json:"genre_ids"`
	Genres           []Genre `json:"genres"`
}

// Genre is a named TMDB genre (returned by detail endpoints).
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// IsAnimation reports whether the movie carries the TMDB animation genre.
func (m Movie) IsAnimation() bool {
	return genreIDsContain(m.GenreIDs, m.Genres)
}

// IsAnimation reports whether the TV show carries the TMDB animation genre.
func (t TVShow) IsAnimation() bool {
	return genreIDsContain(t.GenreIDs, t.Genres)
}

func genreIDsContain(ids []int, genres []Genre) bool {
	if slices.Contains(ids, tmdbGenreAnimation) {
		return true
	}
	for _, g := range genres {
		if g.ID == tmdbGenreAnimation {
			return true
		}
	}
	return false
}

// FindResult is the outcome of an external-id (IMDb) lookup.
type FindResult struct {
	Movies []Movie  `json:"movie_results"`
	TVs    []TVShow `json:"tv_results"`
}

type tmdbSearchResults[T any] struct {
	Results []T `json:"results"`
}

type tmdbShowRef struct {
	ShowID int `json:"show_id"`
}

type tmdbFindResponse struct {
	MovieResults     []Movie       `json:"movie_results"`
	TVResults        []TVShow      `json:"tv_results"`
	TVEpisodeResults []tmdbShowRef `json:"tv_episode_results"`
	TVSeasonResults  []tmdbShowRef `json:"tv_season_results"`
}

// SearchMovies searches TMDB for movies by title.
func (c *TMDBClient) SearchMovies(ctx context.Context, title string) ([]Movie, error) {
	var res tmdbSearchResults[Movie]
	err := c.get(ctx, "/search/movie", url.Values{
		"query":         {title},
		"include_adult": {"true"},
	}, &res)
	return res.Results, err
}

// SearchTVShows searches TMDB for TV shows by title.
func (c *TMDBClient) SearchTVShows(ctx context.Context, title string) ([]TVShow, error) {
	var res tmdbSearchResults[TVShow]
	err := c.get(ctx, "/search/tv", url.Values{
		"query":         {title},
		"include_adult": {"true"},
	}, &res)
	return res.Results, err
}

// FindByIMDbID finds TMDB entries via an IMDb ID. Season/episode hits are
// resolved up to their parent TV series.
func (c *TMDBClient) FindByIMDbID(ctx context.Context, imdbID string) (FindResult, error) {
	var res tmdbFindResponse
	if err := c.get(ctx, "/find/"+imdbID, url.Values{
		"external_source": {"imdb_id"},
	}, &res); err != nil {
		return FindResult{}, err
	}

	for _, ref := range append(res.TVEpisodeResults, res.TVSeasonResults...) {
		tv, err := c.tvShow(ctx, ref.ShowID)
		if err != nil {
			continue
		}
		res.TVResults = append(res.TVResults, tv)
	}

	return FindResult{Movies: res.MovieResults, TVs: res.TVResults}, nil
}

// tvShow fetches a TV series detail by TMDB id.
func (c *TMDBClient) tvShow(ctx context.Context, id int) (TVShow, error) {
	var tv TVShow
	err := c.get(ctx, "/tv/"+strconv.Itoa(id), nil, &tv)
	return tv, err
}

func (c *TMDBClient) get(ctx context.Context, path string, query url.Values, out interface{}) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("invalid tmdb url: %w", err)
	}

	q := u.Query()
	for k, v := range query {
		q[k] = v
	}
	q.Set("api_key", c.apiKey)
	q.Set("language", c.language)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create tmdb request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tmdb http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return fmt.Errorf("tmdb api returned status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("failed to decode tmdb response: %w", err)
	}
	return nil
}
