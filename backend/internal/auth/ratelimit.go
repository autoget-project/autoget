package auth

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// maxRefreshBodyBytes caps the /auth/refresh request body; a refresh
	// token JSON document is far smaller.
	maxRefreshBodyBytes = 8 << 10

	// Per client IP: burst of 10, refilling one request every 6s (10/min).
	refreshPerIPBurst    = 10
	refreshPerIPInterval = 6 * time.Second
	// Global backstop: burst of 60, refilling one per second (60/min). Needed
	// because ClientIP can be spoofed via X-Forwarded-For when the proxy
	// chain is not locked down.
	refreshGlobalBurst    = 60
	refreshGlobalInterval = time.Second
	// maxLimiterClients bounds memory; when full, idle (fully refilled)
	// buckets are evicted, and if none are idle new clients share the global
	// bucket only.
	maxLimiterClients = 10000
)

// bucket is a token bucket. Tokens refill continuously at one per interval.
type bucket struct {
	tokens float64
	last   time.Time
}

// refill adds the tokens accrued since the last call, capped at burst.
func (b *bucket) refill(now time.Time, burst int, interval time.Duration) {
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = math.Min(float64(burst), b.tokens+float64(elapsed)/float64(interval))
	}
	b.last = now
}

// wait returns how long until the bucket holds a full token (0 if it does).
func (b *bucket) wait(interval time.Duration) time.Duration {
	if b.tokens >= 1 {
		return 0
	}
	return time.Duration((1 - b.tokens) * float64(interval))
}

// refreshLimiter applies a per-client and a global token bucket.
type refreshLimiter struct {
	now func() time.Time

	mu      sync.Mutex
	global  bucket
	clients map[string]*bucket
}

func newRefreshLimiter(now func() time.Time) *refreshLimiter {
	return &refreshLimiter{
		now:     now,
		global:  bucket{tokens: refreshGlobalBurst, last: now()},
		clients: map[string]*bucket{},
	}
}

// allow reports whether the client may proceed, or how long to wait.
func (l *refreshLimiter) allow(client string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.global.refill(now, refreshGlobalBurst, refreshGlobalInterval)

	// Table full of active clients: the global bucket alone applies.
	var b *bucket
	if c, ok := l.clients[client]; ok {
		b = c
	} else {
		if len(l.clients) >= maxLimiterClients {
			l.evictIdle(now)
		}
		if len(l.clients) < maxLimiterClients {
			b = &bucket{tokens: refreshPerIPBurst, last: now}
			l.clients[client] = b
		}
	}

	// Check both buckets before consuming from either, so a client denied by
	// its own limit never drains the global bucket (and vice versa).
	var wait time.Duration
	if b != nil {
		b.refill(now, refreshPerIPBurst, refreshPerIPInterval)
		wait = b.wait(refreshPerIPInterval)
	}
	if gw := l.global.wait(refreshGlobalInterval); gw > wait {
		wait = gw
	}
	if wait > 0 {
		return false, wait
	}
	if b != nil {
		b.tokens--
	}
	l.global.tokens--
	return true, 0
}

// evictIdle drops buckets that would be full again, i.e. clients that have
// been quiet long enough to be indistinguishable from new ones.
func (l *refreshLimiter) evictIdle(now time.Time) {
	full := refreshPerIPBurst * refreshPerIPInterval
	for k, b := range l.clients {
		if now.Sub(b.last) >= full {
			delete(l.clients, k)
		}
	}
}

// limitRefresh is gin middleware rejecting excess /auth/refresh calls.
func (s *Service) limitRefresh(c *gin.Context) {
	ok, wait := s.refreshLimit.allow(c.ClientIP())
	if ok {
		c.Next()
		return
	}
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
}
