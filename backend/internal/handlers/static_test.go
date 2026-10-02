package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The SPA shell and its assets must be served without auth; only /api is
// guarded (see cmd.setupRouter). This also covers the theme bootstrap, which is
// a separate file so the CSP can forbid inline scripts.
func TestServeStaticServesShellAndAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "assets"), 0o755))
	writeFile(t, filepath.Join(root, "index.html"), "<html>shell</html>")
	writeFile(t, filepath.Join(root, "icon.svg"), "<svg/>")
	writeFile(t, filepath.Join(root, "theme-init.js"), "/*theme*/")
	writeFile(t, filepath.Join(root, "assets", "app.js"), "/*app*/")

	r := gin.New()
	r.Use(SecurityHeaders())
	serveStatic(r, root)

	cases := []struct {
		path, want  string
		wantCaching string
	}{
		{"/theme-init.js", "theme", "no-cache"},
		{"/icon.svg", "svg", "no-cache"},
		{"/assets/app.js", "app", "public, max-age=31536000, immutable"},
		{"/", "shell", "no-cache"},
		{"/indexers/mteam", "shell", "no-cache"}, // deep links fall through to the shell
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), tc.want)
			assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
			assert.Equal(t, tc.wantCaching, w.Header().Get("Cache-Control"))
		})
	}
}

// An unmatched API path must not be answered with the HTML shell.
func TestServeStaticUnknownAPIIs404JSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "index.html"), "<html>shell</html>")

	r := gin.New()
	serveStatic(r, root)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/typo", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.NotContains(t, w.Body.String(), "shell")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
