package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateImageURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		ok   bool
	}{
		{"good", "https://img.m-team.cc/images/2024/a.jpg", true},
		{"good with query", "https://img.m-team.cc/images/a.jpg?x=1", true},
		{"http scheme", "http://img.m-team.cc/images/a.jpg", false},
		{"other host", "https://example.com/images/a.jpg", false},
		{"suffix host", "https://img.m-team.cc.evil.com/images/a.jpg", false},
		{"userinfo", "https://img.m-team.cc@evil.com/images/a.jpg", false},
		{"userinfo same host", "https://u@img.m-team.cc/images/a.jpg", false},
		{"dot dot", "https://img.m-team.cc/images/../secret", false},
		{"encoded dot dot", "https://img.m-team.cc/images/%2e%2e/secret", false},
		{"double slash", "https://img.m-team.cc/images//a.jpg", false},
		{"port", "https://img.m-team.cc:8443/images/a.jpg", false},
		{"fragment", "https://img.m-team.cc/images/a.jpg#x", false},
		{"wrong prefix", "https://img.m-team.cc/other/a.jpg", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateImageURL(tt.raw)
			assert.Equal(t, tt.ok, err == nil)
		})
	}
}

func imageTestSetup(t *testing.T, h http.HandlerFunc) (*Service, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	oldHost, oldClient := imageHost, imageClient
	t.Cleanup(func() { imageHost, imageClient = oldHost, oldClient })

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	imageHost = u.Host
	c := *srv.Client()
	c.CheckRedirect = oldClient.CheckRedirect
	imageClient = &c

	s, _, _, _ := testSetup(t)
	return s, srv.URL + "/images/a.png"
}

func doImage(t *testing.T, s *Service, target string) *httptest.ResponseRecorder {
	t.Helper()
	_, router, _, _ := testSetup(t)
	_ = s
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/image?url="+url.QueryEscape(target), nil)
	router.ServeHTTP(w, req)
	return w
}

func TestImageProxy(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		s, target := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "https://kp.m-team.cc/", r.Header.Get("referer"))
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("png"))
		})
		w := doImage(t, s, target)
		assert.Equal(t, 200, w.Code)
		assert.Equal(t, "png", w.Body.String())
		assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
		assert.Contains(t, w.Header().Get("Cache-Control"), "max-age")
		assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	})

	t.Run("non-image content type", func(t *testing.T) {
		s, target := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		})
		assert.Equal(t, 502, doImage(t, s, target).Code)
	})

	t.Run("upstream 404", func(t *testing.T) {
		s, target := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		})
		assert.Equal(t, 404, doImage(t, s, target).Code)
	})

	t.Run("upstream 500", func(t *testing.T) {
		s, target := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		})
		w := doImage(t, s, target)
		assert.Equal(t, 502, w.Code)
		assert.NotContains(t, w.Body.String(), "500")
	})

	t.Run("cross-host redirect", func(t *testing.T) {
		s, target := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://evil.example.com/images/a.png", http.StatusFound)
		})
		assert.Equal(t, 502, doImage(t, s, target).Code)
	})

	t.Run("oversized content length", func(t *testing.T) {
		s, target := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Length", strconv.Itoa(maxImageSize+1))
			_, _ = w.Write([]byte(strings.Repeat("a", maxImageSize+1)))
		})
		assert.Equal(t, 502, doImage(t, s, target).Code)
	})

	t.Run("bad url", func(t *testing.T) {
		s, _ := imageTestSetup(t, func(w http.ResponseWriter, r *http.Request) {})
		assert.Equal(t, 400, doImage(t, s, "https://evil.example.com/images/a.png").Code)
	})
}
