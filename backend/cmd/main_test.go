package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/backend/downloaders"
	"github.com/autoget-project/autoget/backend/indexers/mteam"
	"github.com/autoget-project/autoget/backend/indexers/nyaa"
	"github.com/autoget-project/autoget/backend/internal/auth"
	"github.com/autoget-project/autoget/backend/internal/config"
)

// setupRouter must guard only the API. The SPA shell and its assets are
// document requests that never carry the Bearer token kept in localStorage, so
// guarding them would trap the browser in a login redirect loop.
func TestSetupRouterProtectsOnlyAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authSvc, err := auth.New(context.Background(), &auth.Config{
		Issuer:          "http://127.0.0.1:1", // unreachable on purpose
		ClientID:        "id",
		ClientSecret:    "secret",
		RedirectBaseURL: "http://app.example.com",
	})
	require.NoError(t, err)
	require.NotNil(t, authSvc)

	r := gin.New()
	setupRouter(r, authSvc, false, func(g *gin.RouterGroup) {
		g.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	})

	// A public route outside /api is not guarded (a guarded one would 401).
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", "text/html")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))

	// The API requires a token.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// /auth/login is public and reaches the provider (unreachable here).
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// With auth.disabled the API is left open.
func TestSetupRouterWithoutAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authSvc, err := auth.New(context.Background(), &auth.Config{Disabled: true})
	require.NoError(t, err)
	require.Nil(t, authSvc)

	r := gin.New()
	setupRouter(r, authSvc, false, func(g *gin.RouterGroup) {
		g.GET("/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// No auth routes are registered when auth is disabled.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewServerTimeouts(t *testing.T) {
	srv := newServer(":0", http.NotFoundHandler())
	assert.NotZero(t, srv.ReadHeaderTimeout)
	assert.NotZero(t, srv.ReadTimeout)
	assert.NotZero(t, srv.IdleTimeout)
}

func TestNewCronRecoversFromPanickingJob(t *testing.T) {
	c := newCron()
	ran := make(chan struct{})
	_, err := c.AddFunc("@every 1s", func() {
		defer close(ran)
		panic("boom")
	})
	require.NoError(t, err)
	c.Start()

	select {
	case <-ran:
	case <-time.After(3 * time.Second):
		t.Fatal("job did not run")
	}
	select {
	case <-c.Stop().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("cron did not stop")
	}
}

func TestHSTSEnabled(t *testing.T) {
	assert.False(t, hstsEnabled(nil))
	assert.False(t, hstsEnabled(&auth.Config{Disabled: true, RedirectBaseURL: "https://a.example.com"}))
	assert.False(t, hstsEnabled(&auth.Config{RedirectBaseURL: "http://a.example.com"}))
	assert.True(t, hstsEnabled(&auth.Config{RedirectBaseURL: "https://a.example.com"}))
}

type fakeDownloader struct {
	downloaders.IDownloader
}

func (fakeDownloader) TorrentsDir() string { return "/tmp/torrents" }

func TestBuildIndexers(t *testing.T) {
	dls := map[string]downloaders.IDownloader{"dl": fakeDownloader{}}

	t.Run("nil sections give empty map", func(t *testing.T) {
		m, err := buildIndexers(&config.Config{}, dls, nil, nil, cron.New())
		require.NoError(t, err)
		assert.Empty(t, m)
	})

	t.Run("unknown downloader returns error", func(t *testing.T) {
		m, err := buildIndexers(&config.Config{Nyaa: &nyaa.Config{Downloader: "nope"}}, dls, nil, nil, cron.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown downloader "nope"`)
		assert.Nil(t, m)
	})

	t.Run("mteam gives normal and adult indexers", func(t *testing.T) {
		cfg := &config.Config{MTeam: &mteam.Config{APIKey: "k", Downloader: "dl"}}
		m, err := buildIndexers(cfg, dls, nil, nil, cron.New())
		require.NoError(t, err)
		assert.Len(t, m, 2)
	})
}
