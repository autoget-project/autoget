package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLogOmitsQueryString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var buf bytes.Buffer
	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipQueryString: true, Output: &buf}))
	r.GET("/auth/callback", func(c *gin.Context) { c.Status(http.StatusOK) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/auth/callback?code=abc&state=x", nil))

	out := buf.String()
	if !strings.Contains(out, "/auth/callback") {
		t.Errorf("missing path: %q", out)
	}
	for _, s := range []string{"abc", "state=x", "code="} {
		if strings.Contains(out, s) {
			t.Errorf("log leaks %q: %q", s, out)
		}
	}
}
