package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// staticKeySet verifies tokens signed with a single test key, replacing the
// remote JWKS fetch.
type staticKeySet struct {
	key *rsa.PublicKey
}

func (s *staticKeySet) VerifySignature(_ context.Context, token string) ([]byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, assert.AnError
	}
	signingInput := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	hashed := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(s.key, crypto.SHA256, hashed[:], sig); err != nil {
		return nil, err
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	return payload, nil
}

// testHarness wraps the service with a token signer for tests.
type testHarness struct {
	svc   *Service
	token func() string
}

func (h *testHarness) Middleware() gin.HandlerFunc { return h.svc.Middleware() }

func (h *testHarness) SetupRouter(r *gin.Engine) { h.svc.SetupRouter(r) }

// testService builds a service whose verifier checks tokens signed by a
// locally generated key. Discovery is bypassed by seeding the provider cache.
func testService(t *testing.T) *testHarness {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	signedToken := func() string {
		now := time.Now()
		hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
		payload, _ := json.Marshal(map[string]any{
			"iss": "https://provider.example.com",
			"aud": "test-client",
			"sub": "user1",
			"iat": now.Unix(),
			"exp": now.Add(time.Hour).Unix(),
		})
		signingInput := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(payload)
		hashed := sha256.Sum256([]byte(signingInput))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
		require.NoError(t, err)
		return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
	}

	s := &Service{
		cfg: &Config{
			Issuer:          "https://provider.example.com",
			ClientID:        "test-client",
			ClientSecret:    "test-secret",
			RedirectBaseURL: "https://app.example.com",
		},
		provider: &provider{
			oauth: &oauth2.Config{
				ClientID:     "test-client",
				ClientSecret: "test-secret",
				Endpoint: oauth2.Endpoint{
					AuthURL:  "https://provider.example.com/authorize",
					TokenURL: "https://provider.example.com/token",
				},
				RedirectURL: "https://app.example.com/auth/callback",
				Scopes:      []string{"openid", "profile", "offline_access"},
			},
			verifier: oidc.NewVerifier("https://provider.example.com",
				&staticKeySet{key: &key.PublicKey},
				&oidc.Config{ClientID: "test-client"}),
		},
		states: map[string]stateEntry{},
	}
	return &testHarness{svc: s, token: signedToken}
}

// tokenEndpoint returns a service whose refresh calls hit the given handler.
func testServiceWithTokenEndpoint(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Service{
		cfg: &Config{
			Issuer:          "https://provider.example.com",
			ClientID:        "test-client",
			ClientSecret:    "test-secret",
			RedirectBaseURL: "https://app.example.com",
		},
		provider: &provider{
			oauth: &oauth2.Config{
				ClientID:     "test-client",
				ClientSecret: "test-secret",
				Endpoint:     oauth2.Endpoint{TokenURL: srv.URL},
			},
		},
		states: map[string]stateEntry{},
	}
}

func TestMiddlewareDisabled(t *testing.T) {
	var s *Service
	router := gin.New()
	router.Group("/api/v1", s.Middleware()).GET("/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareUnauthenticated(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)
	router.Group("/api/v1", s.Middleware()).GET("/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Even a browser-style request gets 401: the SPA shell is served publicly
	// and the frontend drives the login redirect itself.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareValidToken(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)
	router.Group("/api/v1", s.Middleware()).GET("/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	req.Header.Set("Authorization", "Bearer "+s.token())
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareGarbageToken(t *testing.T) {
	s := testService(t)
	router := gin.New()
	router.Group("/api/v1", s.Middleware()).GET("/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// A provider that cannot be discovered must not push clients into the login
// flow; they get 503 instead of 401.
func TestMiddlewareProviderUnavailable(t *testing.T) {
	s := &Service{
		cfg:    &Config{Issuer: "http://127.0.0.1:1", ClientID: "c", ClientSecret: "s", RedirectBaseURL: "http://app"},
		states: map[string]stateEntry{},
	}
	router := gin.New()
	router.Group("/api/v1", s.Middleware()).GET("/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestLoginRedirectsToProviderAndSetsCookie(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login?redirect=%2Fsearch%3Fq%3Dfoo", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.True(t, strings.HasPrefix(loc, s.svc.provider.oauth.Endpoint.AuthURL), loc)
	assert.Contains(t, loc, "state=")

	cookie := findCookie(w, stateCookieName)
	require.NotNil(t, cookie, "state cookie must be set")
	assert.True(t, cookie.HttpOnly)
	assert.Contains(t, loc, "state="+cookie.Value)
}

func TestCallbackWithoutCookieIsRejected(t *testing.T) {
	s := testService(t)
	s.svc.addState("known-state", "/")
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=known-state&code=x", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCallbackInvalidState(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=bogus&code=x", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "bogus"})
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCallbackSuccess(t *testing.T) {
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","id_token":"it","token_type":"Bearer","expires_in":3600}`))
	})
	s.addState("state-1", "/search?q=foo")
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "state-1"})
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `data-redirect="/search?q=foo"`)
	assert.Contains(t, body, `&#34;access_token&#34;:&#34;at&#34;`)

	// state is single-use
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/auth/callback?state=state-1&code=abc", nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: "state-1"})
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCallbackPageHasNoInlineScript(t *testing.T) {
	var buf strings.Builder
	require.NoError(t, callbackTpl.Execute(&buf, callbackPage{Redirect: "/", Tokens: `{}`}))

	page := buf.String()
	assert.NotContains(t, page, "<script>", "the CSP forbids inline scripts")
	assert.Contains(t, page, `<script src="/auth/callback.js"></script>`)
}

func TestCallbackScriptIsServed(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback.js", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "javascript")
	assert.Contains(t, w.Body.String(), "autoget_auth")
}

func TestRefreshSuccessRotating(t *testing.T) {
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new_at","refresh_token":"new_rt","id_token":"new_it","token_type":"Bearer","expires_in":300}`))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(`{"refresh_token":"old_rt"}`))
	req.Header.Set("Content-Type", "application/json")
	router := gin.New()
	s.SetupRouter(router)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "new_at", got["access_token"])
	assert.Equal(t, "new_rt", got["refresh_token"])
	// expires_in is a lifetime in seconds, not an absolute timestamp
	expiresIn, ok := got["expires_in"].(float64)
	require.True(t, ok)
	assert.Greater(t, expiresIn, float64(0))
	assert.LessOrEqual(t, expiresIn, float64(3600))
}

// A provider that does not rotate the refresh token must not blank out the
// stored value.
func TestRefreshNonRotatingKeepsRefreshToken(t *testing.T) {
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new_at","token_type":"Bearer","expires_in":300}`))
	})
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(`{"refresh_token":"old_rt"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "old_rt", got["refresh_token"])
}

func TestRefreshInvalidGrantReturns401(t *testing.T) {
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Used refresh token"}`))
	})
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(`{"refresh_token":"used"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// A transient provider failure must not log the user out: 503, not 401.
func TestRefreshTransientFailureReturns503(t *testing.T) {
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(`{"refresh_token":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRefreshRequiresToken(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNewTokenResponseExpiresIn(t *testing.T) {
	resp := newTokenResponse(&oauth2.Token{
		AccessToken: "at",
		Expiry:      time.Now().Add(90 * time.Second),
	})
	assert.InDelta(t, 90, resp.ExpiresIn, 2)

	// A token without expiry yields 0 rather than a bogus timestamp.
	assert.Zero(t, newTokenResponse(&oauth2.Token{AccessToken: "at"}).ExpiresIn)
}

func TestStateRoundTrip(t *testing.T) {
	s := testService(t)
	s.svc.addState("v", "/search?q=foo")

	redirect, ok := s.svc.checkState("v")
	require.True(t, ok)
	assert.Equal(t, "/search?q=foo", redirect)

	// consumed: second check fails
	_, ok = s.svc.checkState("v")
	assert.False(t, ok)
}

func TestSanitizeRedirect(t *testing.T) {
	assert.Equal(t, "/", sanitizeRedirect(""))
	assert.Equal(t, "/", sanitizeRedirect("https://evil.example.com"))
	assert.Equal(t, "/", sanitizeRedirect("//evil.example.com"))
	assert.Equal(t, "/search?q=foo", sanitizeRedirect("/search?q=foo"))
}

func TestBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, bearerToken(req))

	req.Header.Set("Authorization", "Bearer abc")
	assert.Equal(t, "abc", bearerToken(req))

	req.Header.Set("Authorization", "bearer abc")
	assert.Equal(t, "abc", bearerToken(req))
}

func TestRandomToken(t *testing.T) {
	a, err := randomToken()
	require.NoError(t, err)
	b, err := randomToken()
	require.NoError(t, err)
	assert.Len(t, a, 32)
	assert.NotEqual(t, a, b)
}

func TestConfigValidate(t *testing.T) {
	assert.Error(t, (&Config{}).Validate())
	assert.Error(t, (&Config{
		Issuer:          "not-a-url",
		ClientID:        "id",
		ClientSecret:    "secret",
		RedirectBaseURL: "https://app.example.com",
	}).Validate())
	assert.Error(t, (&Config{
		Issuer:          "https://provider.example.com",
		ClientID:        "id",
		ClientSecret:    "secret",
		RedirectBaseURL: "app.example.com",
	}).Validate())

	assert.NoError(t, (&Config{
		Issuer:          "https://provider.example.com",
		ClientID:        "id",
		ClientSecret:    "secret",
		RedirectBaseURL: "https://app.example.com",
	}).Validate())
}

func findCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}
