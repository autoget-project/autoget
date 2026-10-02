package handlers

import "github.com/gin-gonic/gin"

// contentSecurityPolicy locks the browser down to same-origin script
// execution. There is no inline script anywhere (the theme bootstrap is
// /theme-init.js and the OAuth callback runs /auth/callback.js), so script-src
// needs neither 'unsafe-inline' nor a nonce, which is what makes the policy
// worth having: an injected inline script cannot run. style-src keeps
// 'unsafe-inline' because Lit sets style attributes; img-src allows https: for
// indexer images that are not routed through the /api/v1/image proxy.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: https:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'"

// SecurityHeaders adds the baseline browser-facing response headers. It covers
// the SPA and API responses alike.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		c.Next()
	}
}
