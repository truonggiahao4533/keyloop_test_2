package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

// writeError aborts the request with the standard error shape.
func writeError(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, errorResponse{Error: errorBody{Code: code, Message: msg, RequestID: requestID(c)}})
}

// writeAppError maps a domain/app error to an HTTP error response.
func writeAppError(c *gin.Context, err error) {
	var de *domain.Error
	msg := "internal error"
	if errors.As(err, &de) {
		msg = de.Msg
	}
	switch {
	case errors.Is(err, domain.ErrValidation):
		writeError(c, http.StatusBadRequest, "VALIDATION_ERROR", msg)
	case errors.Is(err, domain.ErrNotFound):
		writeError(c, http.StatusNotFound, "NOT_FOUND", msg)
	case errors.Is(err, domain.ErrConflict):
		writeError(c, http.StatusConflict, "CONFLICT", msg)
	case errors.Is(err, domain.ErrForbidden):
		writeError(c, http.StatusForbidden, "FORBIDDEN", msg)
	case errors.Is(err, context.DeadlineExceeded):
		slog.ErrorContext(c.Request.Context(), "request timed out", "err", err, "request_id", requestID(c))
		writeError(c, http.StatusGatewayTimeout, "TIMEOUT", "request timed out")
	default:
		slog.ErrorContext(c.Request.Context(), "internal error", "err", err, "request_id", requestID(c))
		writeError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
	}
}
