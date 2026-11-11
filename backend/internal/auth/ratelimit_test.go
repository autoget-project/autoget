package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func TestRefreshLimiterPerClient(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := newRefreshLimiter(clk.now)

	for i := 0; i < refreshPerIPBurst; i++ {
		ok, _ := l.allow("1.1.1.1")
		require.True(t, ok, "request %d", i)
	}
	ok, wait := l.allow("1.1.1.1")
	assert.False(t, ok)
	assert.Equal(t, refreshPerIPInterval, wait)

	// Another client is unaffected.
	ok, _ = l.allow("2.2.2.2")
	assert.True(t, ok)

	// One interval later a single request is allowed again.
	clk.t = clk.t.Add(refreshPerIPInterval)
	ok, _ = l.allow("1.1.1.1")
	assert.True(t, ok)
	ok, _ = l.allow("1.1.1.1")
	assert.False(t, ok)
}

func TestRefreshLimiterGlobalBackstop(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := newRefreshLimiter(clk.now)

	// Distinct clients (e.g. spoofed X-Forwarded-For) still hit the global cap.
	for i := 0; i < refreshGlobalBurst; i++ {
		ok, _ := l.allow(fmt.Sprintf("10.0.0.%d", i))
		require.True(t, ok, "request %d", i)
	}
	ok, _ := l.allow("10.9.9.9")
	assert.False(t, ok)
}

func TestRefreshLimiterBoundedAndEvicts(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := newRefreshLimiter(clk.now)
	// Keep the global bucket out of the way. Only valid because the fake
	// clock does not advance between calls, so refill()'s burst clamp never
	// runs and shrinks this back down.
	l.global.tokens = 1e9

	for i := 0; i < maxLimiterClients+50; i++ {
		l.allow(fmt.Sprintf("c%d", i))
	}
	assert.Equal(t, maxLimiterClients, len(l.clients))

	// After everyone has been idle long enough, new clients evict old ones.
	clk.t = clk.t.Add(time.Hour)
	l.global.tokens = 1e9
	l.allow("fresh")
	assert.Less(t, len(l.clients), maxLimiterClients)
	assert.Contains(t, l.clients, "fresh")
}

func TestRefreshLimiterFloodingClientDoesNotDrainGlobal(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := newRefreshLimiter(clk.now)

	allowed := 0
	for i := 0; i < 1000; i++ {
		if ok, _ := l.allow("10.0.0.1"); ok {
			allowed++
		}
	}
	assert.Equal(t, refreshPerIPBurst, allowed)

	ok, _ := l.allow("10.0.0.2")
	assert.True(t, ok, "other client must still be allowed")
}

func TestRefreshLimiterTableFullFallsBackToGlobal(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1000, 0)}
	l := newRefreshLimiter(clk.now)
	l.global.tokens = 1e9 // see TestRefreshLimiterBoundedAndEvicts
	for i := 0; i < maxLimiterClients; i++ {
		l.allow(fmt.Sprintf("c%d", i))
	}
	require.Equal(t, maxLimiterClients, len(l.clients))

	// All clients are active, so a new one is untracked and only the global
	// bucket limits it.
	for i := 0; i < 50; i++ {
		ok, _ := l.allow("newcomer")
		assert.True(t, ok)
	}
	assert.NotContains(t, l.clients, "newcomer")
	assert.Equal(t, maxLimiterClients, len(l.clients))

	l.global.tokens = 0
	ok, wait := l.allow("newcomer")
	assert.False(t, ok)
	assert.Greater(t, wait, time.Duration(0))
}

func refreshRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.1:1234"
	return req
}

func TestRefreshEndpointRateLimited(t *testing.T) {
	calls := 0
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"a","token_type":"Bearer","expires_in":300}`))
	})
	router := gin.New()
	s.SetupRouter(router)

	for i := 0; i < refreshPerIPBurst; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, refreshRequest(`{"refresh_token":"rt"}`))
		require.Equal(t, http.StatusOK, w.Code, "request %d", i)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, refreshRequest(`{"refresh_token":"rt"}`))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Contains(t, w.Body.String(), "too many requests")
	assert.Equal(t, refreshPerIPBurst, calls, "limited request must not reach the provider")
}

func TestRefreshOversizedBodyRejected(t *testing.T) {
	calls := 0
	s := testServiceWithTokenEndpoint(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	router := gin.New()
	s.SetupRouter(router)

	big := `{"refresh_token":"` + strings.Repeat("a", maxRefreshBodyBytes) + `"}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, refreshRequest(big))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Zero(t, calls)
}
