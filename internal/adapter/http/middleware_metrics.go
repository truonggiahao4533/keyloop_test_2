package http

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/platform"
)

// metricsMiddleware records request count and latency by route template.
func metricsMiddleware(m *platform.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := routeLabel(c)
		m.HTTPRequests.WithLabelValues(route, c.Request.Method, strconv.Itoa(c.Writer.Status())).Inc()
		m.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}
}
