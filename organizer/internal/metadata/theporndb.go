package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	tpdbDefaultBaseURL = "https://api.theporndb.net"

	// tpdbLimitVideoPerType caps candidates per endpoint (scene and movie are
	// queried separately), so the planner consumes a bounded candidate set.
	tpdbLimitVideoPerType = 10
)

// ThePornDBClient queries ThePornDB API for non-JAV porn scenes and movies.
// It only executes queries: query construction and any other semantic work
// belong to the LLM planner.
type ThePornDBClient struct {
	apiToken   string
	baseURL    string
	httpClient *http.Client
}

// NewThePornDB creates a ThePornDB client authenticated with a Bearer apiToken.
func NewThePornDB(apiToken string) *ThePornDBClient {
	return &ThePornDBClient{
		apiToken:   apiToken,
		baseURL:    tpdbDefaultBaseURL,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
}

// TPDBVideo is a trimmed ThePornDB scene/movie search candidate.
type TPDBVideo struct {
	Slug       string   `json:"slug"`
	Title      string   `json:"title"`
	Type       string   `json:"type"` // "scene" or "movie"
	Date       string   `json:"date"` // ISO YYYY-MM-DD
	Site       string   `json:"site"`
	Performers []string `json:"performers"`
	Tags       []string `json:"tags,omitempty"`
}

type tpdbSearchResponse struct {
	Data []struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
		Type  string `json:"type"`
		Date  string `json:"date"`
		Site  struct {
			Name string `json:"name"`
		} `json:"site"`
		Performers []struct {
			Name string `json:"name"`
		} `json:"performers"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	} `json:"data"`
}

// SearchVideos searches ThePornDB for scenes and movies matching query. Scene
// results come first, movie results after; each type is trimmed to
// tpdbLimitVideoPerType candidates. Both endpoints must succeed, otherwise an
// error is returned with no partial results.
func (c *ThePornDBClient) SearchVideos(ctx context.Context, query string) ([]TPDBVideo, error) {
	scenes, err := c.search(ctx, "/scenes", query)
	if err != nil {
		return nil, err
	}
	movies, err := c.search(ctx, "/movies", query)
	if err != nil {
		return nil, err
	}
	return append(scenes, movies...), nil
}

func (c *ThePornDBClient) search(ctx context.Context, endpoint, query string) ([]TPDBVideo, error) {
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid tpdb url: %w", err)
	}
	u.RawQuery = url.Values{"q": {query}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tpdb request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tpdb http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return nil, fmt.Errorf("tpdb api returned status %d: %s", resp.StatusCode, string(body))
	}

	var res tpdbSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode tpdb response: %w", err)
	}

	videos := make([]TPDBVideo, 0, len(res.Data))
	for _, item := range res.Data {
		if len(videos) >= tpdbLimitVideoPerType {
			break
		}
		video := TPDBVideo{
			Slug:  item.Slug,
			Title: item.Title,
			Type:  item.Type,
			Date:  item.Date,
			Site:  item.Site.Name,
		}
		for _, p := range item.Performers {
			video.Performers = append(video.Performers, p.Name)
		}
		for _, t := range item.Tags {
			video.Tags = append(video.Tags, t.Name)
		}
		videos = append(videos, video)
	}
	return videos, nil
}
