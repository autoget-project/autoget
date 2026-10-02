package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/backend/internal/auth"
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
	setupRouter(r, authSvc, func(g *gin.RouterGroup) {
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

// When the auth section is omitted the API is left open.
func TestSetupRouterWithoutAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authSvc, err := auth.New(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, authSvc)

	r := gin.New()
	setupRouter(r, authSvc, func(g *gin.RouterGroup) {
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
