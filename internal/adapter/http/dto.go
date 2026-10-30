package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/app"
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

type vehicleDTO struct {
	ID              string       `json:"id"`
	VIN             string       `json:"vin"`
	Make            string       `json:"make"`
	Model           string       `json:"model"`
	Year            int          `json:"year"`
	Price           *json.Number `json:"price"`
	ReceivedAt      string       `json:"received_at"`
	AgeDays         int          `json:"age_days"`
	IsAging         bool         `json:"is_aging"`
	CurrentAction   *string      `json:"current_action"`
	CurrentActionAt *time.Time   `json:"current_action_at"`
}

type vehicleListDTO struct {
	Items      []vehicleDTO `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

func toVehicleList(p app.VehiclePage) vehicleListDTO {
	out := vehicleListDTO{Items: make([]vehicleDTO, len(p.Items))}
	for i, v := range p.Items {
		d := vehicleDTO{
			ID:              v.ID.String(),
			VIN:             v.VIN,
			Make:            v.Make,
			Model:           v.Model,
			Year:            v.Year,
			Price:           formatCents(v.PriceCents),
			ReceivedAt:      v.ReceivedAt.Format(time.DateOnly),
			AgeDays:         v.AgeDays,
			IsAging:         v.IsAging,
			CurrentActionAt: v.CurrentActionAt,
		}
		if v.CurrentAction != nil {
			a := string(*v.CurrentAction)
			d.CurrentAction = &a
		}
		out.Items[i] = d
	}
	if p.NextCursor != "" {
		out.NextCursor = &p.NextCursor
	}
	return out
}

// formatCents renders cents as a JSON number with two decimals, or null.
func formatCents(c *int64) *json.Number {
	if c == nil {
		return nil
	}
	n := json.Number(fmt.Sprintf("%d.%02d", *c/100, *c%100))
	return &n
}

type makeCountDTO struct {
	Make  string `json:"make"`
	Count int    `json:"count"`
}

type actionCountDTO struct {
	Action *string `json:"action"`
	Count  int     `json:"count"`
}

type summaryDTO struct {
	TotalInStock  int              `json:"total_in_stock"`
	AgingCount    int              `json:"aging_count"`
	AgingPct      float64          `json:"aging_pct"`
	ThresholdDays int              `json:"threshold_days"`
	AgingByMake   []makeCountDTO   `json:"aging_by_make"`
	AgingByAction []actionCountDTO `json:"aging_by_action"`
}

func toSummary(s app.DashboardSummary) summaryDTO {
	out := summaryDTO{
		TotalInStock:  s.TotalInStock,
		AgingCount:    s.AgingCount,
		AgingPct:      s.AgingPct,
		ThresholdDays: s.ThresholdDays,
		AgingByMake:   make([]makeCountDTO, len(s.AgingByMake)),
		AgingByAction: make([]actionCountDTO, len(s.AgingByAction)),
	}
	for i, m := range s.AgingByMake {
		out.AgingByMake[i] = makeCountDTO{Make: m.Make, Count: m.Count}
	}
	for i, a := range s.AgingByAction {
		d := actionCountDTO{Count: a.Count}
		if a.Action != nil {
			v := string(*a.Action)
			d.Action = &v
		}
		out.AgingByAction[i] = d
	}
	return out
}
