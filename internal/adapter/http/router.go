// Package http is the HTTP (Gin) driving adapter.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"

	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

// Pinger reports whether a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the router dependencies.
type Deps struct {
	DB      Pinger
	App     *app.Service
	Auth    *platform.Authenticator
	Logger  *slog.Logger
	Metrics *platform.Metrics // nil creates a private registry
	Limits  Limits
}

// NewRouter builds the Gin engine with all routes and middleware.
func NewRouter(d Deps) *gin.Engine {
	if d.Metrics == nil {
		d.Metrics = platform.NewMetrics()
	}
	r, v1 := newEngine(d)
	h := &handlers{app: d.App, metrics: d.Metrics}
	v1.GET("/vehicles", require(platform.PermRead), h.listVehicles)
	v1.GET("/vehicles/aging", require(platform.PermRead), h.listAging)
	v1.GET("/dashboard/summary", require(platform.PermRead), h.summary)
	v1.POST("/vehicles/:id/actions", require(platform.PermLogAction), h.logAction)
	v1.GET("/vehicles/:id/actions", require(platform.PermRead), h.actionHistory)
	v1.GET("/settings/aging-threshold", require(platform.PermRead), h.getThreshold)
	v1.PUT("/settings/aging-threshold", require(platform.PermSetThreshold), h.putThreshold)
	v1.POST("/ingest/vehicles", require(platform.PermIngest), h.ingest)
	return r
}

// newEngine wires the middleware chain and the unauthenticated routes, and
// returns the authenticated /v1 group.
func newEngine(d Deps) (*gin.Engine, *gin.RouterGroup) {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.Metrics == nil {
		d.Metrics = platform.NewMetrics()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	// Rate limiting keys on the client IP: use the socket address and ignore
	// X-Forwarded-For unless a trusted proxy is configured in front.
	_ = r.SetTrustedProxies(nil)
	r.Use(
		requestIDMiddleware(),
		recoverMiddleware(d.Logger),
		loggerMiddleware(d.Logger),
		metricsMiddleware(d.Metrics),
		otelgin.Middleware(platform.ServiceName, otelgin.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/metrics" && r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
		})),
	)
	r.NoRoute(func(c *gin.Context) { writeError(c, http.StatusNotFound, "NOT_FOUND", "route not found") })
	r.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed")
	})

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			writeError(c, http.StatusServiceUnavailable, "UNAVAILABLE", "database unavailable")
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/metrics", gin.WrapH(d.Metrics.Handler()))
	r.GET("/openapi.yaml", serveSpec)
	r.GET("/docs", serveDocs)

	lim := d.Limits.withDefaults()
	v1 := r.Group("/v1")
	if lim.RatePerSecond > 0 {
		v1.Use(rateLimitMiddleware(lim.RatePerSecond, lim.RateBurst))
	}
	v1.Use(
		bodyLimitMiddleware(lim.MaxBodyBytes),
		timeoutMiddleware(lim.RequestTimeout),
		authMiddleware(d.Auth),
	)
	return r, v1
}
