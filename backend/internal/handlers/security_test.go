package handlers

import (
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
	r.Use(SecurityHeaders())
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
}

// The headers must also cover responses produced by the 404 handler, which is
// where unmatched deep links land.
func TestSecurityHeadersOnNoRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/some/deep/link", nil)
	r.ServeHTTP(w, req)

	assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
}
