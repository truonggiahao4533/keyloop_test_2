package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func TestLimits(t *testing.T) {
	r, v1 := newEngine(Deps{
		DB: okPinger{}, Auth: platform.NewAuthenticator(testSecret),
		Limits: Limits{MaxBodyBytes: 64, RequestTimeout: 50 * time.Millisecond, RatePerSecond: 1, RateBurst: 3},
	})
	v1.POST("/echo", func(c *gin.Context) {
		var v map[string]any
		if err := decodeJSON(c, &v); err != nil {
			writeBodyError(c, err)
			return
		}
		c.JSON(200, v)
	})
	v1.GET("/slow", func(c *gin.Context) {
		<-c.Request.Context().Done()
		writeAppError(c, c.Request.Context().Err())
	})
	tok := mint(t, platform.RoleViewer)
	send := func(method, path, body, ip string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Forwarded-For", "1.2.3.4") // must be ignored
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := send("POST", "/v1/echo", `{"a":"`+strings.Repeat("x", 100)+`"}`, "10.0.0.1"); got != http.StatusRequestEntityTooLarge {
		t.Errorf("large body = %d, want 413", got)
	}
	if got := send("POST", "/v1/echo", `{"a":1}`, "10.0.0.1"); got != 200 {
		t.Errorf("small body = %d", got)
	}
	if got := send("GET", "/v1/slow", "", "10.0.0.2"); got != http.StatusGatewayTimeout {
		t.Errorf("slow request = %d, want 504", got)
	}

	// Burst of 3 per IP, then 429; another IP is unaffected.
	var codes []int
	for range 5 {
		codes = append(codes, send("POST", "/v1/echo", `{}`, "10.0.0.3"))
	}
	if codes[2] != 200 || codes[3] != http.StatusTooManyRequests || codes[4] != http.StatusTooManyRequests {
		t.Errorf("rate limit codes = %v", codes)
	}
	if got := send("POST", "/v1/echo", `{}`, "10.0.0.4"); got != 200 {
		t.Errorf("other IP = %d", got)
	}
}

func TestIPRateLimiterSweepsIdleBuckets(t *testing.T) {
	l := newIPRateLimiter(1, 1)
	now := time.Now()
	l.allow("a", now)
	l.allow("b", now)
	l.allow("c", now.Add(2*bucketIdleTTL))
	if len(l.buckets) != 1 {
		t.Fatalf("buckets = %d, want 1 after sweep", len(l.buckets))
	}
}
