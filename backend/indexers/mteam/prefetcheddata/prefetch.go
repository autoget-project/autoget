package prefetcheddata

import (
	"encoding/json"
	"fmt"
	"net/http"

	_ "embed"

	"github.com/autoget-project/autoget/backend/indexers"
)

type prefetched struct {
	Categories  *categoryJSON      `json:"categories"`
	Countries   map[string]Country `json:"countries"`
	Mediums     map[string]string  `json:"mediums"`
	Standards   map[string]string  `json:"standards"`
	VideoCodecs map[string]string  `json:"video_codecs"`
	AudioCodecs map[string]string  `json:"audio_codecs"`
	Sources     map[string]string  `json:"sources"`
}

func FetchAll(apiKey string, excludeGayContent bool) (*prefetched, error) {
	p := &prefetched{}
	var err error
	p.Categories, err = fetchCategories(apiKey, excludeGayContent)
	if err != nil {
		return nil, err
	}

	p.Countries, err = fetchCountryList(apiKey)
	if err != nil {
		return nil, err
	}

	p.Mediums, err = fetchMediumList(apiKey)
	if err != nil {
		return nil, err
	}

	p.Standards, err = fetchStandardList(apiKey)
	if err != nil {
		return nil, err
	}

	p.VideoCodecs, err = fetchVideoCodecList(apiKey)
	if err != nil {
		return nil, err
	}

	p.AudioCodecs, err = fetchAudioCodecList(apiKey)
	if err != nil {
		return nil, err
	}

	p.Sources, err = fetchSourceList(apiKey)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func fetchMTeamAPI(url, apiKey string, obj interface{}) error {
	client := http.DefaultClient
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("x-api-key", apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(obj); err != nil {
		return fmt.Errorf("failed to decode response body: %w, body: %s", err, resp.Body)
	}

	return nil
}

// Data uses to read the embeded data.json
type Data struct {
	Categories struct {
		Tree  []indexers.Category     `json:"tree"`
		Infos map[string]CategoryInfo `json:"flat"`
	} `json:"categories"`
	Countries   map[string]Country `json:"countries"`
	Mediums     map[string]string  `json:"mediums"`
	Standards   map[string]string  `json:"standards"`
	Teams       map[string]string  `json:"teams"`
	VideoCodecs map[string]string  `json:"video_codecs"`
	AudioCodecs map[string]string  `json:"audio_codecs"`
	Sources     map[string]string  `json:"sources"`
}

//go:embed data.json
var dataJSON []byte

func Read() (*Data, error) {
	data := &Data{}
	if err := json.Unmarshal(dataJSON, data); err != nil {
		return nil, err
	}
	return data, nil
}
