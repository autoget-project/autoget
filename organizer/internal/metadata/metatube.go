package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
)

// metatubeProviderAVBASE identifies AVBASE results, which carry the richest
// metadata and get an extra detail lookup.
const metatubeProviderAVBASE = "AVBASE"

// MetatubeClient queries a Metatube server for JAV metadata.
type MetatubeClient struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

// NewMetatube creates a Metatube client. apiKey is optional (Bearer auth when
// set); apiURL is the Metatube server root, e.g. "https://metatube.example.com".
func NewMetatube(apiURL, apiKey string) *MetatubeClient {
	return &MetatubeClient{
		apiURL:     apiURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
}

// JAV is a JAV metadata record returned by Metatube.
type JAV struct {
	Number      string   `json:"number"`
	Title       string   `json:"title"`
	Provider    string   `json:"provider"`
	Actors      []string `json:"actors,omitempty"`
	ReleaseDate string   `json:"release_date"`
	Maker       string   `json:"maker,omitempty"`
	Label       string   `json:"label,omitempty"`
	Series      string   `json:"series,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type metatubeSearchResponse struct {
	Data []struct {
		ID          string   `json:"id"`
		Number      string   `json:"number"`
		Title       string   `json:"title"`
		Provider    string   `json:"provider"`
		Actors      []string `json:"actors,omitempty"`
		ReleaseDate string   `json:"release_date"`
	} `json:"data"`
}

type metatubeDetailsResponse struct {
	Data struct {
		Maker  string   `json:"maker,omitempty"`
		Label  string   `json:"label,omitempty"`
		Series string   `json:"series,omitempty"`
		Genres []string `json:"genres,omitempty"`
	} `json:"data"`
}

// SearchJapanesePorn searches JAV content by bango (番号), e.g. "SSIS-698".
// AVBASE hits are enriched with maker/label/series/tags from the detail API.
func (c *MetatubeClient) SearchJapanesePorn(ctx context.Context, bango string) ([]JAV, error) {
	u, err := url.Parse(c.apiURL)
	if err != nil {
		return nil, fmt.Errorf("invalid metatube url: %w", err)
	}
	u.Path = "/v1/movies/search"
	u.RawQuery = url.Values{"q": {bango}}.Encode()

	var res metatubeSearchResponse
	if err := c.get(ctx, u.String(), &res); err != nil {
		return nil, err
	}

	javs := make([]JAV, 0, len(res.Data))
	for _, item := range res.Data {
		jav := JAV{
			Number:      item.Number,
			Title:       item.Title,
			Provider:    item.Provider,
			Actors:      item.Actors,
			ReleaseDate: item.ReleaseDate,
		}

		if item.Provider == metatubeProviderAVBASE {
			var details metatubeDetailsResponse
			detailURL, err := c.detailURL(item.ID)
			if err == nil {
				if err := c.get(ctx, detailURL, &details); err == nil {
					jav.Maker = details.Data.Maker
					jav.Label = details.Data.Label
					jav.Series = details.Data.Series
					jav.Tags = details.Data.Genres
				}
			}
		}

		javs = append(javs, jav)
	}
	return javs, nil
}

func (c *MetatubeClient) detailURL(id string) (string, error) {
	u, err := url.Parse(c.apiURL)
	if err != nil {
		return "", fmt.Errorf("invalid metatube url: %w", err)
	}
	u.Path = path.Join("/v1/movies", metatubeProviderAVBASE, id)
	return u.String(), nil
}

func (c *MetatubeClient) get(ctx context.Context, target string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("failed to create metatube request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Add("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metatube http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return fmt.Errorf("metatube api returned status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("failed to decode metatube response: %w", err)
	}
	return nil
}
