package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
)

// Service provides the optional OAuth/OIDC login flow and bearer-token
// verification. A nil *Service disables auth entirely: middleware passes
// through and no routes are registered. It is stateless: tokens live in
// the browser's localStorage and are verified against the provider JWKS.
type Service struct {
	oauth      *oauth2.Config
	verifier   *oidc.IDTokenVerifier
	httpClient *http.Client

	statesMu sync.Mutex
	states   map[string]stateEntry
}

type stateEntry struct {
	redirect string
	exp      time.Time
}

const stateLifetime = 10 * time.Minute

// New creates the auth service using OIDC discovery from the issuer.
// The returned service is nil-safe.
func New(ctx context.Context, cfg *Config) (*Service, error) {
	if cfg == nil {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	ctx = oidc.ClientContext(ctx, httpClient)

	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover provider: %w", err)
	}

	log.Info().Str("issuer", cfg.Issuer).Msg("auth enabled")
	return &Service{
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.RedirectBaseURL + "/auth/callback",
			Scopes:       []string{"openid", "profile", "offline_access"},
		},
		verifier:   provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		httpClient: httpClient,
		states:     map[string]stateEntry{},
	}, nil
}

// SetupRouter registers the auth routes. It is a no-op when auth is disabled.
func (s *Service) SetupRouter(router *gin.Engine) {
	if s == nil {
		return
	}
	router.GET("/auth/login", s.login)
	router.GET("/auth/callback", s.callback)
	router.POST("/auth/refresh", s.refresh)
}

// Middleware protects the app when auth is enabled. API requests carrying a
// valid Bearer access token pass; everything else gets 401. Page navigations
// are redirected to the login route so a fresh visit starts the OAuth flow.
// It is a pass-through when auth is disabled.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s == nil {
			c.Next()
			return
		}

		token := bearerToken(c.Request)
		if token != "" && s.verifyAccessToken(c.Request.Context(), token) {
			c.Next()
			return
		}

		if wantsHTML(c.Request) {
			// preserve the original page for post-login redirect
			original := c.Request.URL.RequestURI()
			c.Abort()
			c.Redirect(http.StatusFound, "/auth/login?redirect="+url.QueryEscape(original))
			return
		}
		c.AbortWithStatus(http.StatusUnauthorized)
	}
}

// verifyAccessToken accepts the provider's signed access token or ID token.
func (s *Service) verifyAccessToken(ctx context.Context, raw string) bool {
	if _, err := s.verifier.Verify(ctx, raw); err == nil {
		return true
	}
	return false
}

func (s *Service) login(c *gin.Context) {
	redirect := sanitizeRedirect(c.Query("redirect"))
	state := s.addState(redirect)
	c.Redirect(http.StatusFound, s.oauth.AuthCodeURL(state))
}

var callbackTpl = template.Must(template.New("callback").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Logging in…</title></head>
<body>
<p>Logging in…</p>
<div id="redirect" data-redirect="{{.Redirect}}"></div>
<script type="application/json" id="tokens">{{.Tokens}}</script>
<script>
(function () {
  var redirect = document.getElementById("redirect").dataset.redirect || "/";
  try {
    var tokens = JSON.parse(document.getElementById("tokens").textContent);
    localStorage.setItem("autoget_auth", JSON.stringify(tokens));
  } catch (e) { /* fall through */ }
  window.location.replace(redirect);
})();
</script>
</body>
</html>`))

type callbackPage struct {
	Redirect string
	Tokens   template.JS
}

func (s *Service) callback(c *gin.Context) {
	if errDesc := c.Query("error"); errDesc != "" {
		c.String(http.StatusBadGateway, "auth error: %s", errDesc)
		return
	}
	redirect, ok := s.checkState(c.Query("state"))
	if !ok {
		c.String(http.StatusBadRequest, "invalid state")
		return
	}

	ctx := c.Request.Context()
	tok, err := s.oauth.Exchange(ctx, c.Query("code"))
	if err != nil {
		log.Error().Err(err).Msg("code exchange failed")
		c.String(http.StatusBadGateway, "code exchange failed")
		return
	}

	idToken, _ := tok.Extra("id_token").(string)

	tokens := tokenResponse{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      idToken,
		ExpiresIn:    tok.Expiry.Unix(),
		TokenType:    tok.TokenType,
	}

	// redirect target traveled through the OAuth state, not the query;
	// inject it into the page as a data attribute
	data, _ := json.Marshal(tokens)
	page := callbackPage{
		Redirect: redirect,
		Tokens:   template.JS(data),
	}
	if err := callbackTpl.Execute(c.Writer, page); err != nil {
		log.Error().Err(err).Msg("render callback page failed")
	}
}

// refresh exchanges a refresh token for new tokens, keeping the
// client_secret server-side.
func (s *Service) refresh(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token is required"})
		return
	}

	ctx := c.Request.Context()
	ts := s.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: body.RefreshToken})
	tok, err := ts.Token()
	if err != nil {
		log.Debug().Err(err).Msg("token refresh failed")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh failed"})
		return
	}

	idToken := ""
	if id, ok := tok.Extra("id_token").(string); ok {
		idToken = id
	}
	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      idToken,
		ExpiresIn:    tok.Expiry.Unix(),
		TokenType:    tok.TokenType,
	})
}

func (s *Service) addState(redirect string) string {
	v := randomToken()
	s.statesMu.Lock()
	defer s.statesMu.Unlock()
	// opportunistic cleanup
	for k, e := range s.states {
		if time.Now().After(e.exp) {
			delete(s.states, k)
		}
	}
	s.states[v] = stateEntry{redirect: redirect, exp: time.Now().Add(stateLifetime)}
	return v
}

// checkState consumes the state value, returning the redirect target and
// whether the state was valid.
func (s *Service) checkState(v string) (string, bool) {
	s.statesMu.Lock()
	defer s.statesMu.Unlock()
	e, ok := s.states[v]
	delete(s.states, v)
	return e.redirect, ok && time.Now().Before(e.exp)
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

func wantsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// sanitizeRedirect only allows same-origin relative paths.
func sanitizeRedirect(v string) string {
	if v == "" || v[0] != '/' || (len(v) > 1 && v[1] == '/') {
		return "/"
	}
	return v
}

// randomToken returns a 128-bit random hex string.
func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	const hexDigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexDigits[v>>4]
		out[i*2+1] = hexDigits[v&0x0f]
	}
	return string(out)
}
