package handlers

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	staticRoot = "/html"
)

// ServeStatic serves the built frontend from staticRoot, when it exists.
func ServeStatic(router *gin.Engine) {
	// check if the frontend build dist /html exists
	if _, err := os.Stat(staticRoot); os.IsNotExist(err) {
		return
	}
	serveStatic(router, staticRoot)
}

// serveStatic registers the static routes for root. The SPA shell and its
// assets are public: document requests never carry the Bearer token, so
// guarding them would trap the browser in a login redirect loop.
func serveStatic(router *gin.Engine, root string) {
	// Hashed assets never change for a given URL, so they can be cached forever.
	assets := router.Group("/assets", cacheImmutable())
	assets.StaticFS("/", http.Dir(root+"/assets"))

	// serve icon.svg (unhashed, so it must be revalidated)
	router.GET("/icon.svg", cacheRevalidate(root+"/icon.svg"))

	// serve the theme bootstrap (kept external for the CSP, unhashed so must not be cached long)
	router.GET("/theme-init.js", cacheRevalidate(root+"/theme-init.js"))

	// serve index.html; unmatched paths are deep links into the SPA
	router.NoRoute(func(c *gin.Context) {
		// An unmatched API path is a client mistake, not a page: answer with
		// JSON so API callers are not handed the HTML shell.
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.File(root + "/index.html")
	})
}

// cacheImmutable marks a response as safe to cache forever (content-addressed).
func cacheImmutable() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
		c.Next()
	}
}

// cacheRevalidate serves an unhashed file that must be revalidated on every use.
func cacheRevalidate(path string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-cache")
		c.File(path)
	}
}
