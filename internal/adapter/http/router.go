// Package http is the HTTP (Gin) driving adapter.
package http

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

// Pinger reports whether a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the router dependencies.
type Deps struct {
	DB     Pinger
	App    *app.Service
	Auth   *platform.Authenticator
	Logger *slog.Logger
}

// NewRouter builds the Gin engine with all routes and middleware.
func NewRouter(d Deps) *gin.Engine {
	r, v1 := newEngine(d)
	h := &handlers{app: d.App}
	v1.GET("/vehicles", require(platform.PermRead), h.listVehicles)
	v1.GET("/vehicles/aging", require(platform.PermRead), h.listAging)
	v1.GET("/dashboard/summary", require(platform.PermRead), h.summary)
	v1.POST("/vehicles/:id/actions", require(platform.PermLogAction), h.logAction)
	v1.GET("/vehicles/:id/actions", require(platform.PermRead), h.actionHistory)
	return r
}

// newEngine wires the middleware chain and the unauthenticated routes, and
// returns the authenticated /v1 group.
func newEngine(d Deps) (*gin.Engine, *gin.RouterGroup) {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(
		requestIDMiddleware(),
		recoverMiddleware(d.Logger),
		loggerMiddleware(d.Logger),
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

	v1 := r.Group("/v1", authMiddleware(d.Auth))
	return r, v1
}
