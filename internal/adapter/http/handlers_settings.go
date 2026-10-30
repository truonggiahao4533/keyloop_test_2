package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

type thresholdDTO struct {
	Days *int `json:"days"`
}

// GET /v1/settings/aging-threshold
func (h *handlers) getThreshold(c *gin.Context) {
	days, err := h.app.AgingThreshold(c.Request.Context())
	if err != nil {
		writeAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, thresholdDTO{Days: &days})
}

// PUT /v1/settings/aging-threshold
func (h *handlers) putThreshold(c *gin.Context) {
	var req thresholdDTO
	if err := decodeJSON(c, &req); err != nil {
		writeBodyError(c, err)
		return
	}
	if req.Days == nil {
		writeAppError(c, domain.Validationf("days is required"))
		return
	}
	if err := h.app.SetAgingThreshold(c.Request.Context(), *req.Days); err != nil {
		writeAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, req)
}
