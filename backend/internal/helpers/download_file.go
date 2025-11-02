package helpers

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/autoget-project/autoget/backend/internal/db"
	"gorm.io/gorm"
)

// DownloadTorrentFileFromURL downloads a file from a given URL and saves it to a specified local path,
// while checking for duplicates using the provided database connection.
func DownloadTorrentFileFromURL(httpClient *http.Client, url string, dest string, dbClient *gorm.DB) (*metainfo.MetaInfo, *metainfo.Info, error) {
	// Get the data
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP GET error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP status error: %d %s", resp.StatusCode, resp.Status)
	}

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}

	m, err := metainfo.Load(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load metainfo: %w", err)
	}

	info, err := m.UnmarshalInfo()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal info: %w", err)
	}

	// Check for duplicate download
	torrentHash := m.HashInfoBytes().HexString()

	_, err = db.GetDownloadStatusByID(dbClient, torrentHash)
	if err == nil {
		// Found existing download status - this is a duplicate
		return nil, nil, fmt.Errorf("duplicate download: torrent with hash %s already exists", torrentHash)
	}
	if err != gorm.ErrRecordNotFound {
		// Database error (not a "not found" error)
		return nil, nil, fmt.Errorf("database error checking for duplicates: %w", err)
	}
	// err == gorm.ErrRecordNotFound means no duplicate found, which is what we want

	// Create the destination file
	out, err := os.Create(dest)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Write the response body to the file
	_, err = io.Copy(out, bytes.NewReader(buffer.Bytes()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to copy response body to file: %w", err)
	}

	return m, &info, nil
}
