package helpers

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/autoget-project/autoget/backend/internal/db"
	herrors "github.com/autoget-project/autoget/backend/internal/errors"
)

// maxTorrentSize caps the size of a downloaded .torrent file (typical ones are < 1 MiB).
const maxTorrentSize = 20 << 20

// ErrDuplicateDownload is returned when a torrent with the same hash is already tracked.
var ErrDuplicateDownload = errors.New("duplicate download")

// DownloadHTTPError maps an error from DownloadTorrentFileFromURL (including errors
// returned by its beforePlace hook) to an HTTP status error: duplicates become 409.
func DownloadHTTPError(err error) *herrors.HTTPStatusError {
	if errors.Is(err, ErrDuplicateDownload) || errors.Is(err, gorm.ErrDuplicatedKey) {
		return herrors.NewHTTPStatusError(http.StatusConflict, "duplicate download")
	}
	log.Error().Err(err).Msg("download failed")
	return herrors.NewHTTPStatusError(http.StatusInternalServerError, "failed to download torrent")
}

// DownloadTorrentFileFromURL downloads a torrent from a given URL and atomically places it at dest,
// while checking for duplicates using the provided database connection.
// The data is first written to a temp ".part" file next to dest; then beforePlace (may be nil) is
// called with the torrent hash (e.g. to insert the DB row), and finally the file is renamed into place.
// If beforePlace succeeded but the rename fails, the caller is responsible for rolling back.
func DownloadTorrentFileFromURL(httpClient *http.Client, url string, dest string, dbClient *gorm.DB, beforePlace func(hash string) error) (*metainfo.MetaInfo, *metainfo.Info, error) {
	// Get the data
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP GET error: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("HTTP status error: %d %s", resp.StatusCode, resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentSize+1))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response body: %w", err)
	}
	if len(data) > maxTorrentSize {
		return nil, nil, fmt.Errorf("torrent file too large (limit %d bytes)", maxTorrentSize)
	}

	m, err := metainfo.Load(bytes.NewReader(data))
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
		return nil, nil, fmt.Errorf("%w: torrent with hash %s already exists", ErrDuplicateDownload, torrentHash)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		// Database error (not a "not found" error)
		return nil, nil, fmt.Errorf("database error checking for duplicates: %w", err)
	}

	// Write to a temp file (".part" so the torrent watcher ignores it).
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".tmp-*.part")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return nil, nil, fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, nil, fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, nil, fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return nil, nil, fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if beforePlace != nil {
		if err := beforePlace(torrentHash); err != nil {
			return nil, nil, fmt.Errorf("before place hook: %w", err)
		}
	}

	if err := os.Rename(tmpName, dest); err != nil {
		return nil, nil, fmt.Errorf("failed to move torrent into place: %w", err)
	}

	return m, &info, nil
}
