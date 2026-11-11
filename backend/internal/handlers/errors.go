package handlers

import (
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	herrors "github.com/autoget-project/autoget/backend/internal/errors"
)

// respondError logs err (with request context) and responds with only the
// public message, so internal error text never reaches the client.
func respondError(c *gin.Context, status int, publicMsg string, err error) {
	if err != nil {
		log.Error().Err(err).
			Str("method", c.Request.Method).
			Str("path", c.Request.URL.Path).
			Int("status", status).
			Msg(publicMsg)
	}
	c.JSON(status, gin.H{"error": publicMsg})
}

// respondIndexerError responds with an indexer error. Messages of client
// errors (<500) are kept as they are meaningful to the user; server errors are
// logged and replaced by a generic message.
func respondIndexerError(c *gin.Context, e *herrors.HTTPStatusError) {
	if e.Code >= 500 {
		respondError(c, e.Code, "internal error", e)
		return
	}
	c.JSON(e.Code, gin.H{"error": e.Message})
}
