package handlers

import (
	"net/http"
	"os"

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
	// serve assets
	router.StaticFS("/assets", http.Dir(root+"/assets"))

	// serve icon.svg
	router.StaticFile("/icon.svg", root+"/icon.svg")

	// serve the theme bootstrap (kept external for the CSP)
	router.StaticFile("/theme-init.js", root+"/theme-init.js")

	// serve index.html
	router.NoRoute(func(c *gin.Context) {
		c.File(root + "/index.html")
	})
}
