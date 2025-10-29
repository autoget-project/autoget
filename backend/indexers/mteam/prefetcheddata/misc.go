package prefetcheddata

import (
	"strings"
)

type listResponse struct {
	Code    interface{} `json:"code"`
	Message string      `json:"message"`
	Data    []struct {
		CreatedDate      string `json:"createdDate"`
		LastModifiedDate string `json:"lastModifiedDate"`
		ID               string `json:"id"`
		Order            string `json:"order"`
		NameChs          string `json:"nameChs"`
		NameCht          string `json:"nameCht"`
		NameEng          string `json:"nameEng"`
		Name             string `json:"name"`
		Pic              string `json:"pic"`
	} `json:"data"`
}

func fetchMediumList(apiKey string) (map[string]string, error) {
	list := &listResponse{}
	if err := fetchMTeamAPI(baseURL+"/api/torrent/mediumList", apiKey, list); err != nil {
		return nil, err
	}

	m := make(map[string]string)
	for _, it := range list.Data {
		m[it.ID] = it.NameChs
	}
	return m, nil
}

func fetchVideoCodecList(apiKey string) (map[string]string, error) {
	list := &listResponse{}
	if err := fetchMTeamAPI(baseURL+"/api/torrent/videoCodecList", apiKey, list); err != nil {
		return nil, err
	}

	m := make(map[string]string)
	for _, it := range list.Data {
		m[it.ID] = it.Name
	}
	return m, nil
}

func fetchAudioCodecList(apiKey string) (map[string]string, error) {
	list := &listResponse{}
	if err := fetchMTeamAPI(baseURL+"/api/torrent/audioCodecList", apiKey, list); err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, it := range list.Data {
		m[it.ID] = it.Name
	}
	return m, nil
}

func fetchSourceList(apiKey string) (map[string]string, error) {
	list := &listResponse{}
	if err := fetchMTeamAPI(baseURL+"/api/torrent/sourceList", apiKey, list); err != nil {
		return nil, err
	}

	m := make(map[string]string)
	for _, it := range list.Data {
		m[it.ID] = it.NameChs
	}
	return m, nil
}

func fetchStandardList(apiKey string) (map[string]string, error) {
	list := &listResponse{}
	if err := fetchMTeamAPI(baseURL+"/api/torrent/standardList", apiKey, list); err != nil {
		return nil, err
	}

	m := make(map[string]string)
	for _, it := range list.Data {
		m[it.ID] = it.Name
	}
	return m, nil
}

type Country struct {
	Name string `json:"name"`
	Flag string `json:"flag"`
}

func fetchCountryList(apiKey string) (map[string]Country, error) {
	list := &listResponse{}
	if err := fetchMTeamAPI(baseURL+"/api/system/countryList", apiKey, list); err != nil {
		return nil, err
	}

	staticFlagURL := strings.ReplaceAll(baseURL, "api", "static") + "/static/flag/"

	m := make(map[string]Country)
	for _, it := range list.Data {
		m[it.ID] = Country{
			Name: it.Name,
			Flag: staticFlagURL + it.Pic,
		}
	}
	return m, nil
}
