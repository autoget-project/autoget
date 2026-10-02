package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
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

const (
	stateLifetime = 10 * time.Minute
	// discoveryRetryInterval throttles retries while the provider is down, so
	// an outage does not turn every request into a discovery call.
	discoveryRetryInterval = 30 * time.Second
	stateCookieName        = "autoget_auth_state"
)

// Service provides the optional OAuth/OIDC login flow and bearer-token
// verification. A nil *Service disables auth entirely: the middleware passes
// through and no routes are registered. It is stateless: tokens live in the
// browser's localStorage and are verified against the provider JWKS.
type Service struct {
	cfg        *Config
	httpClient *http.Client
	// cookieSecure mirrors whether redirect_base_url is served over HTTPS.
	cookieSecure bool

	// The provider is discovered lazily so the app still starts when the
	// provider is temporarily unreachable.
	providerMu    sync.Mutex
	provider      *provider
	providerErr   error
	providerErrAt time.Time

	statesMu sync.Mutex
	states   map[string]stateEntry
}

// provider bundles what OIDC discovery yields.
type provider struct {
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

type stateEntry struct {
	redirect string
	exp      time.Time
}

// New creates the auth service. Discovery is attempted eagerly so
// misconfiguration surfaces at startup, but a provider that is down only logs
// a warning; the service retries on first use. The returned service is
// nil-safe.
func New(ctx context.Context, cfg *Config) (*Service, error) {
	if cfg == nil {
		return nil, nil
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	base, _ := url.Parse(cfg.RedirectBaseURL)
	s := &Service{
		cfg:          cfg,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		cookieSecure: base.Scheme == "https",
		states:       map[string]stateEntry{},
	}

	if _, err := s.ensureProvider(ctx); err != nil {
		log.Warn().Err(err).Str("issuer", cfg.Issuer).
			Msg("auth provider discovery failed at startup; will retry on first use")
	} else {
		log.Info().Str("issuer", cfg.Issuer).Msg("auth enabled")
	}
	return s, nil
}

// ensureProvider discovers the OIDC provider once and caches the result.
func (s *Service) ensureProvider(ctx context.Context) (*provider, error) {
	s.providerMu.Lock()
	defer s.providerMu.Unlock()

	if s.provider != nil {
		return s.provider, nil
	}
	if s.providerErr != nil && time.Since(s.providerErrAt) < discoveryRetryInterval {
		return nil, s.providerErr
	}

	discoveryCtx := oidc.ClientContext(ctx, s.httpClient)
	p, err := oidc.NewProvider(discoveryCtx, s.cfg.Issuer)
	if err != nil {
		s.providerErr = fmt.Errorf("discover provider: %w", err)
		s.providerErrAt = time.Now()
		return nil, s.providerErr
	}

	s.provider = &provider{
		oauth: &oauth2.Config{
			ClientID:     s.cfg.ClientID,
			ClientSecret: s.cfg.ClientSecret,
			Endpoint:     p.Endpoint(),
			RedirectURL:  strings.TrimRight(s.cfg.RedirectBaseURL, "/") + "/auth/callback",
			Scopes:       []string{"openid", "profile", "offline_access"},
		},
		// The provider's access token is a signed JWT whose audience is the
		// client id (see caddypaw authn), so the ID token verifier validates it
		// unchanged. An access token without aud=client_id would need a
		// different verifier.
		verifier: p.Verifier(&oidc.Config{ClientID: s.cfg.ClientID}),
	}
	s.providerErr = nil
	return s.provider, nil
}

// SetupRouter registers the auth routes. It is a no-op when auth is disabled.
func (s *Service) SetupRouter(router *gin.Engine) {
	if s == nil {
		return
	}
	router.GET("/auth/login", s.login)
	router.GET("/auth/callback", s.callback)
	router.GET("/auth/callback.js", s.callbackScript)
	router.POST("/auth/refresh", s.refresh)
}

// Middleware guards the API and is a pass-through when auth is disabled. A
// request carrying a valid Bearer token passes; a missing or invalid token
// gets 401, and an unreachable provider gets 503 so clients keep their session
// instead of being pushed through the login flow.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if s == nil {
			c.Next()
			return
		}

		token := bearerToken(c.Request)
		if token == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		ok, err := s.verifyAccessToken(c.Request.Context(), token)
		if err != nil {
			log.Error().Err(err).Msg("access token verification failed")
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		if !ok {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

func (s *Service) verifyAccessToken(ctx context.Context, raw string) (bool, error) {
	p, err := s.ensureProvider(ctx)
	if err != nil {
		return false, err
	}
	if _, err := p.verifier.Verify(ctx, raw); err != nil {
		return false, nil
	}
	return true, nil
}

func (s *Service) login(c *gin.Context) {
	p, err := s.ensureProvider(c.Request.Context())
	if err != nil {
		log.Error().Err(err).Msg("auth provider unavailable")
		c.String(http.StatusServiceUnavailable, "auth provider unavailable")
		return
	}

	state, err := randomToken()
	if err != nil {
		log.Error().Err(err).Msg("generate state failed")
		c.String(http.StatusInternalServerError, "login failed")
		return
	}
	s.addState(state, sanitizeRedirect(c.Query("redirect")))

	// Bind the state to this browser: a callback URL captured by an attacker
	// cannot then be replayed against another user's session (login CSRF).
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, state, int(stateLifetime.Seconds()), "/auth", "", s.cookieSecure, true)

	c.Redirect(http.StatusFound, p.oauth.AuthCodeURL(state))
}

var callbackTpl = template.Must(template.New("callback").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Logging in…</title></head>
<body>
<p>Logging in…</p>
<div id="tokens" data-redirect="{{.Redirect}}" data-tokens="{{.Tokens}}"></div>
<script src="/auth/callback.js"></script>
</body>
</html>`))

//go:embed callback.js
var callbackScriptJS string

type callbackPage struct {
	Redirect string
	Tokens   string
}

func (s *Service) callbackScript(c *gin.Context) {
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(callbackScriptJS))
}

func (s *Service) callback(c *gin.Context) {
	if errDesc := c.Query("error"); errDesc != "" {
		c.String(http.StatusBadGateway, "auth error: %s", errDesc)
		return
	}

	state := c.Query("state")
	cookie, err := c.Cookie(stateCookieName)
	if err != nil || cookie == "" || subtle.ConstantTimeCompare([]byte(cookie), []byte(state)) != 1 {
		c.String(http.StatusBadRequest, "invalid state")
		return
	}
	redirect, ok := s.checkState(state)
	if !ok {
		c.String(http.StatusBadRequest, "invalid state")
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(stateCookieName, "", -1, "/auth", "", s.cookieSecure, true)

	p, err := s.ensureProvider(c.Request.Context())
	if err != nil {
		log.Error().Err(err).Msg("auth provider unavailable")
		c.String(http.StatusServiceUnavailable, "auth provider unavailable")
		return
	}

	tok, err := p.oauth.Exchange(c.Request.Context(), c.Query("code"))
	if err != nil {
		log.Error().Err(err).Msg("code exchange failed")
		c.String(http.StatusBadGateway, "code exchange failed")
		return
	}

	data, err := json.Marshal(newTokenResponse(tok))
	if err != nil {
		log.Error().Err(err).Msg("marshal tokens failed")
		c.String(http.StatusInternalServerError, "login failed")
		return
	}

	if err := callbackTpl.Execute(c.Writer, callbackPage{Redirect: redirect, Tokens: string(data)}); err != nil {
		log.Error().Err(err).Msg("render callback page failed")
	}
}

// refresh exchanges a refresh token for new tokens, keeping the client_secret
// server-side.
func (s *Service) refresh(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token is required"})
		return
	}

	ctx := c.Request.Context()
	p, err := s.ensureProvider(ctx)
	if err != nil {
		log.Error().Err(err).Msg("auth provider unavailable")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth unavailable"})
		return
	}

	tok, err := p.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: body.RefreshToken}).Token()
	if err != nil {
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) && retrieveErr.ErrorCode == "invalid_grant" {
			// The grant is gone (expired, revoked or already used); the user
			// must authenticate again.
			log.Debug().Err(err).Msg("refresh token rejected")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh failed"})
			return
		}
		// Provider outage or misconfiguration: keep the client's session and
		// let it retry instead of forcing a re-login.
		log.Warn().Err(err).Msg("token refresh failed")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "auth unavailable"})
		return
	}

	c.JSON(http.StatusOK, newTokenResponse(tok))
}

// newTokenResponse maps an oauth2 token to the wire format, converting the
// absolute expiry to a lifetime in seconds (RFC 6749 §5.1).
func newTokenResponse(tok *oauth2.Token) tokenResponse {
	var expiresIn int64
	if !tok.Expiry.IsZero() {
		if remaining := int64(time.Until(tok.Expiry).Seconds()); remaining > 0 {
			expiresIn = remaining
		}
	}
	idToken, _ := tok.Extra("id_token").(string)
	return tokenResponse{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		IDToken:      idToken,
		ExpiresIn:    expiresIn,
		TokenType:    tok.TokenType,
	}
}

func (s *Service) addState(state, redirect string) {
	s.statesMu.Lock()
	defer s.statesMu.Unlock()
	// opportunistic cleanup
	for k, e := range s.states {
		if time.Now().After(e.exp) {
			delete(s.states, k)
		}
	}
	s.states[state] = stateEntry{redirect: redirect, exp: time.Now().Add(stateLifetime)}
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

// sanitizeRedirect only allows same-origin relative paths.
func sanitizeRedirect(v string) string {
	if v == "" || v[0] != '/' || (len(v) > 1 && v[1] == '/') {
		return "/"
	}
	return v
}

// randomToken returns a 128-bit random hex string.
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
