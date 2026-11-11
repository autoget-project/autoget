package handlers

import (
	tls_ "crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// directive returns the value of a CSP directive, e.g. directive(csp, "script-src").
func directive(csp, name string) string {
	for _, d := range strings.Split(csp, ";") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(d), name+" "); ok {
			return rest
		}
	}
	return ""
}

func TestSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders(false))
	r.GET("/assets/app.js", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	csp := w.Header().Get("Content-Security-Policy")
	require.NotEmpty(t, csp)

	// Scripts must be same-origin only: no inline, no nonce-free escape hatch.
	assert.Equal(t, "'self'", directive(csp, "script-src"))
	assert.NotContains(t, csp, "'unsafe-inline' 'self'")
	assert.NotContains(t, directive(csp, "script-src"), "unsafe")
	assert.Contains(t, csp, "object-src 'none'")
	assert.Contains(t, csp, "frame-ancestors 'none'")

	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "no-referrer", w.Header().Get("Referrer-Policy"))
	assert.Equal(t, "camera=(), microphone=(), geolocation=(), payment=()", w.Header().Get("Permissions-Policy"))
	assert.Equal(t, "same-origin", w.Header().Get("Cross-Origin-Opener-Policy"))
	assert.Equal(t, "same-origin", w.Header().Get("Cross-Origin-Resource-Policy"))
	assert.Empty(t, w.Header().Get("Strict-Transport-Security"))
}

func TestSecurityHeadersHSTS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	serve := func(hsts bool, tls bool) http.Header {
		r := gin.New()
		r.Use(SecurityHeaders(hsts))
		r.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tls {
			req.TLS = &tls_.ConnectionState{}
		}
		r.ServeHTTP(w, req)
		return w.Header()
	}

	assert.Equal(t, "max-age=31536000", serve(true, false).Get("Strict-Transport-Security"))
	assert.Equal(t, "max-age=31536000", serve(false, true).Get("Strict-Transport-Security"))
	assert.Empty(t, serve(false, false).Get("Strict-Transport-Security"))
}

// The headers must also cover responses produced by the 404 handler, which is
// where unmatched deep links land.
func TestSecurityHeadersOnNoRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders(false))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/some/deep/link", nil)
	r.ServeHTTP(w, req)

	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
}
