package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/domain"
)

const (
	headerIdempotencyKey = "Idempotency-Key"
	headerReplayed       = "Idempotency-Replayed"
)

type logActionRequest struct {
	ActionType string  `json:"action_type"`
	Note       *string `json:"note"`
}

// POST /v1/vehicles/:id/actions
func (h *handlers) logAction(c *gin.Context) {
	id, ok := vehicleID(c)
	if !ok {
		return
	}
	var req logActionRequest
	if err := decodeJSON(c, &req); err != nil {
		writeBodyError(c, err)
		return
	}
	a, replay, err := h.app.LogAction(c.Request.Context(), app.LogActionInput{
		VehicleID:      id,
		ActionType:     req.ActionType,
		Note:           req.Note,
		Actor:          principal(c).Subject,
		IdempotencyKey: c.GetHeader(headerIdempotencyKey),
		RequestHash:    requestHash(id, req),
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	if replay {
		c.Header(headerReplayed, "true")
	}
	c.JSON(http.StatusCreated, toActionDTO(a))
}

// GET /v1/vehicles/:id/actions
func (h *handlers) actionHistory(c *gin.Context) {
	id, ok := vehicleID(c)
	if !ok {
		return
	}
	limit, err := queryInt(c, "limit")
	if err != nil {
		writeAppError(c, err)
		return
	}
	actions, err := h.app.History(c.Request.Context(), id, limit)
	if err != nil {
		writeAppError(c, err)
		return
	}
	out := actionListDTO{Items: make([]actionDTO, len(actions))}
	for i, a := range actions {
		out.Items[i] = toActionDTO(a)
	}
	c.JSON(http.StatusOK, out)
}

func vehicleID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeAppError(c, domain.Validationf("vehicle id must be a UUID"))
		return uuid.Nil, false
	}
	return id, true
}

// requestHash fingerprints the normalized request (target vehicle and body),
// so formatting differences in the JSON do not count as a different request.
func requestHash(id uuid.UUID, req logActionRequest) string {
	b, _ := json.Marshal(struct {
		VehicleID string `json:"v"`
		logActionRequest
	}{id.String(), req})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
