package mteam

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/autoget-project/autoget/backend/indexers"
	"github.com/autoget-project/autoget/backend/internal/errors"
	"github.com/autoget-project/autoget/backend/internal/helpers"
)

type genDownloadLinkResponse struct {
	Code    interface{} `json:"code"` // maybe string or int
	Message string      `json:"message"`
	Data    string      `json:"data"`
}

func (m *MTeam) Download(id string) (*indexers.DownloadResult, *errors.HTTPStatusError) {
	_, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return nil, errors.NewHTTPStatusError(http.StatusBadRequest, "invalid id")
	}

	resp := &genDownloadLinkResponse{}
	er := makeMultipartAPICall(m.config.getBaseURL(), "/api/torrent/genDlToken", m.config.APIKey, map[string]string{
		"id": id,
	}, resp)
	if er != nil {
		return nil, er
	}

	if resp.Code != "0" {
		logger.Error().Any("code", resp.Code).Str("message", resp.Message).Str("API", "/api/torrent/genDlToken").Msg("API error")
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, resp.Message)
	}

	destFilePath := filepath.Join(m.torrentsDir, name+"."+id+".torrent")

	me, _, err := helpers.DownloadTorrentFileFromURL(http.DefaultClient, resp.Data, destFilePath, m.db)
	if err != nil {
		// Check if this is a duplicate download error
		if strings.Contains(err.Error(), "duplicate download:") {
			return nil, errors.NewHTTPStatusError(http.StatusConflict, err.Error())
		}
		return nil, errors.NewHTTPStatusError(http.StatusInternalServerError, err.Error())
	}

	return &indexers.DownloadResult{
		TorrentFilePath: destFilePath,
		TorrentHash:     me.HashInfoBytes().HexString(),
	}, nil
}
