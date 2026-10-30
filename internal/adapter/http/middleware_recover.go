package http

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// recoverMiddleware turns a panic into a 500 with the standard error shape.
func recoverMiddleware(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}
			log.ErrorContext(c.Request.Context(), "panic", "panic", rec, "request_id", requestID(c), "stack", string(debug.Stack()))
			if !c.Writer.Written() {
				writeError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
			} else {
				c.Abort()
			}
		}()
		c.Next()
	}
}
