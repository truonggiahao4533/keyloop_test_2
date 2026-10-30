package app

import (
	"context"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

// MaxIdempotencyKeyLen bounds the Idempotency-Key header.
const MaxIdempotencyKeyLen = 128

// LogActionInput is a request to log a manager action on a vehicle.
type LogActionInput struct {
	VehicleID      uuid.UUID
	ActionType     string
	Note           *string
	Actor          string // authenticated subject
	IdempotencyKey string
	RequestHash    string // hash of the normalized request, for key-reuse detection
}

// LogAction records an action on any in-stock vehicle. Actions are allowed
// before a vehicle becomes aging so managers can act early. It returns
// replay=true when the idempotency key was already used for this request.
func (s *Service) LogAction(ctx context.Context, in LogActionInput) (domain.Action, bool, error) {
	if n := utf8.RuneCountInString(in.IdempotencyKey); n < 1 || n > MaxIdempotencyKeyLen {
		return domain.Action{}, false, domain.Validationf("Idempotency-Key header is required (1 to %d characters)", MaxIdempotencyKeyLen)
	}
	typ, err := domain.ParseActionType(in.ActionType)
	if err != nil {
		return domain.Action{}, false, err
	}
	note := in.Note
	if note != nil && *note == "" {
		note = nil
	}
	if note != nil && utf8.RuneCountInString(*note) > domain.MaxNoteLen {
		return domain.Action{}, false, domain.Validationf("note must be at most %d characters", domain.MaxNoteLen)
	}
	return s.actions.Log(ctx, in.VehicleID, domain.NewAction{
		ID:        uuid.Must(uuid.NewV7()),
		Type:      typ,
		Note:      note,
		CreatedBy: in.Actor,
	}, in.IdempotencyKey, in.RequestHash)
}

// History returns a vehicle's actions, newest first.
func (s *Service) History(ctx context.Context, vehicleID uuid.UUID, limit *int) ([]domain.Action, error) {
	l, err := resolveLimit(limit)
	if err != nil {
		return nil, err
	}
	return s.actions.History(ctx, vehicleID, l)
}
