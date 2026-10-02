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
// locally generated key.
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
		states: map[string]stateEntry{},
	}
	return &testHarness{svc: s, token: signedToken}
}

func TestMiddlewareDisabled(t *testing.T) {
	var s *Service
	router := gin.New()
	router.Use(s.Middleware())
	router.GET("/api/v1/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareUnauthenticated(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)
	router.Use(s.Middleware())
	router.GET("/api/v1/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	// API-style request: no Accept: text/html -> 401
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// browser navigation -> redirect to login with redirect param
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/search?q=foo", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/auth/login?redirect=")
}

func TestMiddlewareValidToken(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)
	router.Use(s.Middleware())
	router.GET("/api/v1/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	req.Header.Set("Authorization", "Bearer "+s.token())
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareGarbageToken(t *testing.T) {
	s := testService(t)
	router := gin.New()
	router.Use(s.Middleware())
	router.GET("/api/v1/indexers", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/indexers", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCallbackInvalidState(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/callback?state=bogus&code=x", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLoginRedirectsToProvider(t *testing.T) {
	s := testService(t)
	router := gin.New()
	s.SetupRouter(router)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/login?redirect=%2Fsearch%3Fq%3Dfoo", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.True(t, strings.HasPrefix(loc, s.svc.oauth.Endpoint.AuthURL), loc)
	assert.Contains(t, loc, "state=")
}

func TestStateRoundTrip(t *testing.T) {
	s := testService(t)
	v := s.svc.addState("/search?q=foo")

	redirect, ok := s.svc.checkState(v)
	require.True(t, ok)
	assert.Equal(t, "/search?q=foo", redirect)

	// consumed: second check fails
	_, ok = s.svc.checkState(v)
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

func TestConfigValidate(t *testing.T) {
	err := (&Config{}).Validate()
	assert.Error(t, err)

	err = (&Config{
		Issuer:          "https://provider.example.com",
		ClientID:        "id",
		ClientSecret:    "secret",
		RedirectBaseURL: "https://app.example.com",
	}).Validate()
	assert.NoError(t, err)
}
