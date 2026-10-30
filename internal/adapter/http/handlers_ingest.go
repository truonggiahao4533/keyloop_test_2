package http

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/app"
)

type ingestVehicleDTO struct {
	VIN        string       `json:"vin"`
	Make       string       `json:"make"`
	Model      string       `json:"model"`
	Year       int          `json:"year"`
	Price      *json.Number `json:"price"`
	ReceivedAt string       `json:"received_at"`
	Status     string       `json:"status"`
}

type ingestRequest struct {
	Vehicles []ingestVehicleDTO `json:"vehicles"`
}

type ingestResponse struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
}

// POST /v1/ingest/vehicles
func (h *handlers) ingest(c *gin.Context) {
	var req ingestRequest
	if err := decodeJSON(c, &req); err != nil {
		writeBodyError(c, err)
		return
	}
	batch := make([]app.IngestVehicle, len(req.Vehicles))
	for i, v := range req.Vehicles {
		batch[i] = app.IngestVehicle{
			VIN: v.VIN, Make: v.Make, Model: v.Model, Year: v.Year,
			ReceivedAt: v.ReceivedAt, Status: v.Status,
		}
		if v.Price != nil {
			p := v.Price.String()
			batch[i].Price = &p
		}
	}
	res, err := h.app.Ingest(c.Request.Context(), batch)
	if err != nil {
		writeAppError(c, err)
		return
	}
	h.metrics.IngestRows.WithLabelValues("inserted").Add(float64(res.Inserted))
	h.metrics.IngestRows.WithLabelValues("updated").Add(float64(res.Updated))
	h.metrics.IngestRows.WithLabelValues("unchanged").Add(float64(res.Unchanged))
	c.JSON(http.StatusOK, ingestResponse{Inserted: res.Inserted, Updated: res.Updated})
}
