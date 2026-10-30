package http

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	headerRequestID = "X-Request-Id"
	ctxRequestID    = "request_id"
)

// requestIDMiddleware propagates a safe inbound X-Request-Id or creates one,
// and echoes it on the response.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(headerRequestID)
		if !validRequestID(id) {
			id = uuid.NewString()
		}
		c.Set(ctxRequestID, id)
		c.Header(headerRequestID, id)
		c.Next()
	}
}

func validRequestID(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func requestID(c *gin.Context) string { return c.GetString(ctxRequestID) }
