package http

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// loggerMiddleware writes one structured line per request. It never logs
// headers or bodies, so tokens and action notes stay out of the logs.
func loggerMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		}
		log.LogAttrs(c.Request.Context(), level, "request",
			slog.String("request_id", requestID(c)),
			slog.String("method", c.Request.Method),
			slog.String("route", routeLabel(c)),
			slog.Int("status", status),
			slog.Float64("latency_ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("subject", principal(c).Subject),
		)
	}
}

// routeLabel is the route template (e.g. /v1/vehicles/:id/actions), never
// the raw path, to keep log fields and metric labels low-cardinality.
func routeLabel(c *gin.Context) string {
	if r := c.FullPath(); r != "" {
		return r
	}
	return "unmatched"
}
