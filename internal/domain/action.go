package domain

import (
	"time"

	"github.com/google/uuid"
)

// ActionType is what a manager decided to do with a vehicle.
type ActionType string

const (
	ActionPriceReductionPlanned ActionType = "PRICE_REDUCTION_PLANNED"
	ActionPriceReduced          ActionType = "PRICE_REDUCED"
	ActionTransferPlanned       ActionType = "TRANSFER_PLANNED"
	ActionWholesalePlanned      ActionType = "WHOLESALE_PLANNED"
	ActionPromotionPlanned      ActionType = "PROMOTION_PLANNED"
	ActionNoActionNeeded        ActionType = "NO_ACTION_NEEDED"
)

// ActionTypes lists every valid action type, mirroring the DB enum.
var ActionTypes = []ActionType{
	ActionPriceReductionPlanned, ActionPriceReduced, ActionTransferPlanned,
	ActionWholesalePlanned, ActionPromotionPlanned, ActionNoActionNeeded,
}

// MaxNoteLen is the maximum action note length in characters.
const MaxNoteLen = 1000

// ParseActionType validates s against the enum.
func ParseActionType(s string) (ActionType, error) {
	for _, t := range ActionTypes {
		if string(t) == s {
			return t, nil
		}
	}
	return "", Validationf("unknown action_type %q", s)
}

// Action is a logged decision on a vehicle.
type Action struct {
	ID        uuid.UUID
	VehicleID uuid.UUID
	Type      ActionType
	Note      *string
	CreatedBy string
	CreatedAt time.Time
}

// NewAction is the input for logging an action.
type NewAction struct {
	ID        uuid.UUID
	Type      ActionType
	Note      *string
	CreatedBy string
}
