package helpers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownloadTorrentFileFromURL_TooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", maxTorrentSize+10)))
	}))
	defer srv.Close()

	_, _, err := DownloadTorrentFileFromURL(srv.Client(), srv.URL, t.TempDir()+"/x.torrent", nil)
	assert.ErrorContains(t, err, "too large")
}

func TestDownloadTorrentFileFromURL_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := DownloadTorrentFileFromURL(srv.Client(), srv.URL, t.TempDir()+"/x.torrent", nil)
	assert.ErrorContains(t, err, "HTTP status error")
}
