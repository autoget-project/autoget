package main

import (
	"context"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/autoget/backend/internal/auth"
	"github.com/autoget-project/autoget/backend/internal/handlers"
)

// notAPI lists registered routes under the documented prefixes that are static
// browser scripts rather than API endpoints.
var notAPI = map[string]bool{
	"GET /auth/callback.js": true,
	"GET /auth/denied.js":   true,
}

var ginParam = regexp.MustCompile(`:([A-Za-z0-9_]+)`)

// TestOpenAPISpecMatchesRoutes keeps openapi.yaml in sync with the routes:
// every registered /api/v1 and /auth route must be documented and every
// documented operation must be registered. Schemas are not checked.
func TestOpenAPISpecMatchesRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authSvc, err := auth.New(context.Background(), &auth.Config{
		Issuer:          "http://127.0.0.1:1", // unreachable on purpose
		ClientID:        "id",
		ClientSecret:    "secret",
		RedirectBaseURL: "http://app.example.com",
	})
	require.NoError(t, err)

	r := gin.New()
	svc := handlers.NewService(nil, nil, nil, nil, nil)
	setupRouter(r, authSvc, false, svc.SetupRouter)

	registered := map[string]bool{}
	for _, ri := range r.Routes() {
		if !strings.HasPrefix(ri.Path, "/api/v1/") && !strings.HasPrefix(ri.Path, "/auth/") {
			continue // SPA and static assets
		}
		key := ri.Method + " " + ginParam.ReplaceAllString(ri.Path, "{$1}")
		if notAPI[key] {
			continue
		}
		registered[key] = true
	}
	require.NotEmpty(t, registered)

	raw, err := os.ReadFile("../openapi.yaml")
	require.NoError(t, err)
	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &spec))

	documented := map[string]bool{}
	paths, _ := spec["paths"].(map[string]any)
	require.NotEmpty(t, paths)
	for p, item := range paths {
		ops, _ := item.(map[string]any)
		for method, op := range ops {
			if _, isOp := op.(map[string]any); !isOp {
				continue // e.g. path-level parameters
			}
			documented[strings.ToUpper(method)+" "+p] = true
		}
	}

	assert.Empty(t, diff(registered, documented), "routes missing from openapi.yaml")
	assert.Empty(t, diff(documented, registered), "openapi.yaml operations that are not registered")

	// Every $ref must resolve inside the document.
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				if ref, ok := c.(string); ok && k == "$ref" {
					assert.True(t, resolves(spec, ref), "unresolved $ref %s", ref)
				}
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(spec)
}

func diff(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func resolves(spec map[string]any, ref string) bool {
	rest, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return false
	}
	var cur any = spec
	for _, part := range strings.Split(rest, "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		if cur, ok = m[part]; !ok {
			return false
		}
	}
	return true
}
