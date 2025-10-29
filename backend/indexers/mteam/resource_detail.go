package mteam

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/errors"
)

type resourceDetail struct {
	searchResponseItem

	OriginFileName string `json:"originFileName"`
	Descr          string `json:"descr"`
	Mediainfo      string `json:"mediainfo"`
}

type detailResponse struct {
	Code    interface{}    `json:"code"` // maybe string or int
	Message string         `json:"message"`
	Data    resourceDetail `json:"data"`
}

type filesResponse struct {
	Code    interface{} `json:"code"` // maybe string or int
	Message string      `json:"message"`
	Data    []struct {
		CreatedDate      string `json:"createdDate"`
		LastModifiedDate string `json:"lastModifiedDate"`
		ID               string `json:"id"`
		Torrent          string `json:"torrent"`
		Name             string `json:"name"`
		Size             string `json:"size"`
	} `json:"data"`
}

func (m *MTeam) Detail(id string, fileList bool) (*indexers.ResourceDetail, *errors.HTTPStatusError) {
	_, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusBadRequest, "invalid id")
	}

	resp := &detailResponse{}
	er := makeMultipartAPICall(m.config.getBaseURL(), "/api/torrent/detail", m.config.APIKey, map[string]string{
		"id": id,
	}, resp)
	if er != nil {
		return nil, er
	}

	if resp.Code != "0" {
		logger.Error().Any("code", resp.Code).Str("message", resp.Message).Str("API", "/api/torrent/detail").Msg("API error")
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, resp.Message)
	}

	time, _ := parseTime(resp.Data.CreatedDate)
	seeders, _ := strconv.Atoi(resp.Data.Status.Seeders)
	leechers, _ := strconv.Atoi(resp.Data.Status.Leechers)
	size, _ := strconv.ParseUint(resp.Data.Size, 10, 64)

	images := []string{}
	for _, img := range resp.Data.ImageList {
		images = append(images, imageUseProxy(img))
	}

	cat := m.prefetched.Categories.Infos[resp.Data.Category].Name

	res := &indexers.ResourceDetail{
		ListResourceItem: indexers.ListResourceItem{
			ID:          resp.Data.ID,
			Title:       resp.Data.Name,
			Title2:      resp.Data.SmallDescr,
			CreatedDate: time,
			Category:    cat,
			Size:        size,
			Resolution:  m.prefetched.Standards[resp.Data.Standard],
			Seeders:     uint32(seeders),
			Leechers:    uint32(leechers),
			DBs:         resp.Data.extractDBInfo(),
			Images:      images,
			Free:        resp.Data.Status.Discount == "FREE",
		},
		Mediainfo:   resp.Data.Mediainfo,
		Description: resp.Data.Descr,
		Metadata:    resp.Data.extractMetadata(cat),
	}

	if !fileList {
		return res, nil
	}

	filesResp := &filesResponse{}
	er = makeMultipartAPICall(m.config.getBaseURL(), "/api/torrent/files", m.config.APIKey, map[string]string{
		"id": id,
	}, filesResp)
	if er != nil {
		return nil, er
	}

	if filesResp.Code != "0" {
		logger.Error().Any("code", filesResp.Code).Str("message", filesResp.Message).Str("API", "/api/torrent/files").Msg("API error")
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, filesResp.Message)
	}

	for _, file := range filesResp.Data {
		size, _ := strconv.ParseUint(file.Size, 10, 64)
		res.Files = append(res.Files, indexers.File{
			Name: file.Name,
			Size: size,
		})
	}

	return res, nil
}

func makeMultipartAPICall(baseURL, path, apiKey string, vars map[string]string, o interface{}) *errors.HTTPStatusError {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for k, v := range vars {
		_ = writer.WriteField(k, v)
	}
	err := writer.Close()
	if err != nil {
		return errors.NewHTTPStatusError(http.StatusInternalServerError, "close multipart")
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+path, body)
	if err != nil {
		return errors.NewHTTPStatusError(http.StatusInternalServerError, "failed to new request")
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-api-key", apiKey)

	client := http.Client{
		Timeout: httpTimeout,
	}
	r, err := client.Do(req)
	if err != nil {
		return errors.NewHTTPStatusError(http.StatusInternalServerError, "failed to request")
	}

	if r.StatusCode != http.StatusOK {
		logger.Error().Err(err).Str("indexer", name).Int("status_code", r.StatusCode).Msg("API error")
		return errors.NewHTTPStatusError(r.StatusCode, "search request failed")
	}

	err = json.NewDecoder(r.Body).Decode(o)
	if err != nil {
		return errors.NewHTTPStatusError(http.StatusInternalServerError, "failed to unmarshal response")
	}

	return nil
}
