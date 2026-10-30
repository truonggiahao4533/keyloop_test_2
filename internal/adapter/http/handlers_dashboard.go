package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /v1/dashboard/summary
func (h *handlers) summary(c *gin.Context) {
	s, err := h.app.Summary(c.Request.Context())
	if err != nil {
		writeAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, toSummary(s))
}
