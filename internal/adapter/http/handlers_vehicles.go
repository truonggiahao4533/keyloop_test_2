package http

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

type handlers struct {
	app     *app.Service
	metrics *platform.Metrics
}

// GET /v1/vehicles
func (h *handlers) listVehicles(c *gin.Context) {
	q, err := parseListQuery(c)
	if err != nil {
		writeAppError(c, err)
		return
	}
	if v := c.Query("aging"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeAppError(c, domain.Validationf("aging must be true or false"))
			return
		}
		q.Aging = b
	}
	page, err := h.app.ListVehicles(c.Request.Context(), q)
	if err != nil {
		writeAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, toVehicleList(page))
}

func parseListQuery(c *gin.Context) (app.ListQuery, error) {
	q := app.ListQuery{Cursor: c.Query("cursor")}
	if v := c.Query("make"); v != "" {
		q.Make = &v
	}
	if v := c.Query("model"); v != "" {
		q.Model = &v
	}
	var err error
	if q.MinAgeDays, err = queryInt(c, "min_age_days"); err != nil {
		return q, err
	}
	if q.MaxAgeDays, err = queryInt(c, "max_age_days"); err != nil {
		return q, err
	}
	if q.Limit, err = queryInt(c, "limit"); err != nil {
		return q, err
	}
	return q, nil
}

// queryInt parses an optional integer query parameter.
func queryInt(c *gin.Context, name string) (*int, error) {
	v, ok := c.GetQuery(name)
	if !ok || v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, domain.Validationf("%s must be an integer", name)
	}
	return &n, nil
}

// GET /v1/vehicles/aging
func (h *handlers) listAging(c *gin.Context) {
	q, err := parseListQuery(c)
	if err != nil {
		writeAppError(c, err)
		return
	}
	page, err := h.app.ListAging(c.Request.Context(), q)
	if err != nil {
		writeAppError(c, err)
		return
	}
	c.JSON(http.StatusOK, toVehicleList(page))
}
