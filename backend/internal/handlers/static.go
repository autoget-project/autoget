package handlers

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

const (
	staticRoot = "/html"
)

func ServeStatic(router *gin.Engine) {

	// check if the frontend build dist /html exists
	if _, err := os.Stat(staticRoot); os.IsNotExist(err) {
		return
	}

	// serve assets
	router.StaticFS("/assets", http.Dir(staticRoot+"/assets"))

	// serve icon.svg
	router.StaticFile("/icon.svg", staticRoot+"/icon.svg")

	// serve index.html
	router.NoRoute(func(c *gin.Context) {
		c.File(staticRoot + "/index.html")
	})
}
