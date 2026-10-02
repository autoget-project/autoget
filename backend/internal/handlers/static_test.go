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
		wantNoCache bool
	}{
		{"/theme-init.js", "theme", true},
		{"/icon.svg", "svg", false},
		{"/assets/app.js", "app", false},
		{"/", "shell", true},
		{"/indexers/mteam", "shell", true}, // deep links fall through to the shell
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), tc.want)
			assert.NotEmpty(t, w.Header().Get("Content-Security-Policy"))
			if tc.wantNoCache {
				assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
