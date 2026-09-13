package protocol

import (
	"crypto/sha256"
	"encoding/hex"
)

// Header constants for resumable upload protocol.
const (
	UploadOffsetHeader   = "Upload-Offset"
	UploadLengthHeader   = "Upload-Length"
	UploadChecksumHeader = "Content-SHA256"
	UploadContentType    = "application/offset+octet-stream"
)

// Path constants for upload API endpoints.
const (
	UploadPathInit   = "/v1/upload/init"
	UploadPathPrefix = "/v1/upload/"
)

// UploadStatus represents the status of an upload session.
const (
	StatusUploading = "uploading"
	StatusCompleted = "completed"
)

// MaxChunkSize defines the hard upper bound for a single upload chunk (256 MB).
const MaxChunkSize = 256 * 1024 * 1024

// UploadInitRequest represents the request body for POST /v1/upload/init.
type UploadInitRequest struct {
	TorrentID    string `json:"torrent_id"`
	RelativePath string `json:"relative_path"`
	TotalSize    int64  `json:"total_size"`
	Checksum     string `json:"checksum,omitempty"`
	ModifyTime   int64  `json:"modify_time,omitempty"`
}

// UploadStatusResponse represents the response body for POST /v1/upload/init and GET /v1/upload/{upload_id}.
type UploadStatusResponse struct {
	UploadID  string `json:"upload_id"`
	Offset    int64  `json:"offset"`
	TotalSize int64  `json:"total_size,omitempty"`
	Status    string `json:"status"` // "uploading" or "completed"
}

// UploadFinishRequest represents the optional request body for POST /v1/upload/{upload_id}/finish.
// Passing TorrentID and RelativePath allows idempotent retry if the upload metadata was already deleted.
type UploadFinishRequest struct {
	TorrentID    string `json:"torrent_id,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
}

// UploadFinishResponse represents the response body for POST /v1/upload/{upload_id}/finish.
type UploadFinishResponse struct {
	Status string `json:"status"` // "completed"
}

// UploadError represents the standard error response body {"error": "..."}.
type UploadError struct {
	Error string `json:"error"`
}

// DeriveUploadID generates a deterministic upload identifier from torrent_id and relative_path:
// "up_" + hex(sha256(torrent_id + "\x00" + relative_path))[:24]
func DeriveUploadID(torrentID, relPath string) string {
	h := sha256.New()
	h.Write([]byte(torrentID))
	h.Write([]byte{0})
	h.Write([]byte(relPath))
	hashHex := hex.EncodeToString(h.Sum(nil))
	return "up_" + hashHex[:24]
}
