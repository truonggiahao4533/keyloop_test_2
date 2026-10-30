package http

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Limits are the request hardening settings.
type Limits struct {
	MaxBodyBytes   int64         // 0 means 1 MB
	RequestTimeout time.Duration // 0 means 5 s
	RatePerSecond  float64       // per client IP; 0 disables rate limiting
	RateBurst      int
}

func (l Limits) withDefaults() Limits {
	if l.MaxBodyBytes == 0 {
		l.MaxBodyBytes = 1 << 20
	}
	if l.RequestTimeout == 0 {
		l.RequestTimeout = 5 * time.Second
	}
	if l.RateBurst == 0 {
		l.RateBurst = max(1, int(2*l.RatePerSecond))
	}
	return l
}

// bodyLimitMiddleware caps the request body; decodeJSON turns the overflow
// into a 413.
func bodyLimitMiddleware(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

// timeoutMiddleware bounds the whole request; repositories add their own
// tighter per-call timeout.
func timeoutMiddleware(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// ipRateLimiter is a token bucket per client IP. Idle buckets are dropped
// so the map cannot grow without bound.
type ipRateLimiter struct {
	mu        sync.Mutex
	limit     rate.Limit
	burst     int
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	lim  *rate.Limiter
	seen time.Time
}

const bucketIdleTTL = 5 * time.Minute

func newIPRateLimiter(perSecond float64, burst int) *ipRateLimiter {
	return &ipRateLimiter{limit: rate.Limit(perSecond), burst: burst, buckets: map[string]*bucket{}, lastSweep: time.Now()}
}

func (l *ipRateLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) > bucketIdleTTL {
		for k, b := range l.buckets {
			if now.Sub(b.seen) > bucketIdleTTL {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{lim: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[ip] = b
	}
	b.seen = now
	return b.lim.AllowN(now, 1)
}

func rateLimitMiddleware(perSecond float64, burst int) gin.HandlerFunc {
	l := newIPRateLimiter(perSecond, burst)
	return func(c *gin.Context) {
		if !l.allow(c.ClientIP(), time.Now()) {
			c.Header("Retry-After", "1")
			writeError(c, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests")
			return
		}
		c.Next()
	}
}
