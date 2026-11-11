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

// hstsValue pins the host to HTTPS for a year. No includeSubDomains or preload:
// the policy is sticky in browsers, so it stays scoped to this host.
const hstsValue = "max-age=31536000"

// permissionsPolicy turns off powerful browser features the app never uses.
const permissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=()"

// SecurityHeaders adds the baseline browser-facing response headers. It covers
// the SPA and API responses alike.
//
// Cross-Origin-Opener-Policy and Cross-Origin-Resource-Policy are same-origin:
// the SPA, its assets and the /api/v1/image proxy are only ever loaded by the
// app's own pages, and the OIDC login is a top-level redirect (no popup), so
// neither header interferes with it.
//
// Strict-Transport-Security is sent only when hsts is true (the configured
// public URL is https) or the request itself arrived over TLS. It is never
// sent on plain HTTP, where browsers ignore it anyway.
func SecurityHeaders(hsts bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Permissions-Policy", permissionsPolicy)
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if hsts || (c.Request != nil && c.Request.TLS != nil) {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		c.Next()
	}
}
