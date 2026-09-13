package protocol_test

import (
	"testing"

	"github.com/autoget-project/autoget/protocol"
)

func TestDeriveUploadID_Deterministic(t *testing.T) {
	torrentID := "4a1b2c3d4e5f"
	relPath := "Movie.2024.1080p/movie.mkv"

	id1 := protocol.DeriveUploadID(torrentID, relPath)
	id2 := protocol.DeriveUploadID(torrentID, relPath)

	if id1 != id2 {
		t.Fatalf("expected deterministic ID, got %s and %s", id1, id2)
	}

	if len(id1) != 27 { // "up_" (3) + 24 hex characters = 27
		t.Fatalf("expected length 27, got %d (%s)", len(id1), id1)
	}

	if id1[:3] != "up_" {
		t.Fatalf("expected prefix 'up_', got %s", id1)
	}

	// Different path should produce different ID
	id3 := protocol.DeriveUploadID(torrentID, "Movie.2024.1080p/sample.mkv")
	if id1 == id3 {
		t.Fatalf("expected different IDs for different paths, got identical %s", id1)
	}

	// Different torrent ID should produce different ID
	id4 := protocol.DeriveUploadID("other-torrent", relPath)
	if id1 == id4 {
		t.Fatalf("expected different IDs for different torrent IDs, got identical %s", id1)
	}
}

func TestUploadConstants(t *testing.T) {
	if protocol.UploadOffsetHeader != "Upload-Offset" {
		t.Errorf("unexpected UploadOffsetHeader: %s", protocol.UploadOffsetHeader)
	}
	if protocol.UploadLengthHeader != "Upload-Length" {
		t.Errorf("unexpected UploadLengthHeader: %s", protocol.UploadLengthHeader)
	}
	if protocol.UploadChecksumHeader != "Content-SHA256" {
		t.Errorf("unexpected UploadChecksumHeader: %s", protocol.UploadChecksumHeader)
	}
	if protocol.UploadContentType != "application/offset+octet-stream" {
		t.Errorf("unexpected UploadContentType: %s", protocol.UploadContentType)
	}
	if protocol.StatusUploading != "uploading" {
		t.Errorf("unexpected StatusUploading: %s", protocol.StatusUploading)
	}
	if protocol.StatusCompleted != "completed" {
		t.Errorf("unexpected StatusCompleted: %s", protocol.StatusCompleted)
	}
	if protocol.MaxChunkSize != 256*1024*1024 {
		t.Errorf("unexpected MaxChunkSize: %d", protocol.MaxChunkSize)
	}
}
