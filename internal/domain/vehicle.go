package domain

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// VehicleStatus is the stock status of a vehicle.
type VehicleStatus string

const (
	StatusInStock VehicleStatus = "IN_STOCK"
	StatusSold    VehicleStatus = "SOLD"
	StatusRemoved VehicleStatus = "REMOVED"
)

// ParseVehicleStatus validates s.
func ParseVehicleStatus(s string) (VehicleStatus, error) {
	switch v := VehicleStatus(s); v {
	case StatusInStock, StatusSold, StatusRemoved:
		return v, nil
	}
	return "", Validationf("unknown vehicle status %q", s)
}

// Vehicle is a vehicle as supplied by the inventory feed.
type Vehicle struct {
	ID         uuid.UUID
	VIN        string
	Make       string
	Model      string
	Year       int
	PriceCents *int64 // nil when no price is set
	ReceivedAt time.Time
	Status     VehicleStatus
}

// VehicleRow is a stored vehicle including its current action.
type VehicleRow struct {
	Vehicle
	CurrentAction   *ActionType
	CurrentActionAt *time.Time
}

// VehicleFilter narrows a vehicle listing. Nil fields are not applied.
// Age filters are expressed as received_at bounds so the index can be used.
type VehicleFilter struct {
	Make           *string
	Model          *string
	ReceivedBefore *time.Time // inclusive
	ReceivedAfter  *time.Time // inclusive
}

// Cursor is a keyset position in the (received_at, id) ordering.
type Cursor struct {
	Date time.Time
	ID   uuid.UUID
}

type cursorJSON struct {
	D  string    `json:"d"`
	ID uuid.UUID `json:"id"`
}

// Encode returns the opaque cursor token: base64(JSON {d, id}).
func (c Cursor) Encode() string {
	b, _ := json.Marshal(cursorJSON{D: c.Date.Format(time.DateOnly), ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses a token produced by Cursor.Encode.
func DecodeCursor(s string) (*Cursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, Validationf("invalid cursor")
	}
	var cj cursorJSON
	if err := json.Unmarshal(b, &cj); err != nil || cj.ID == uuid.Nil {
		return nil, Validationf("invalid cursor")
	}
	d, err := time.Parse(time.DateOnly, cj.D)
	if err != nil {
		return nil, Validationf("invalid cursor")
	}
	return &Cursor{Date: d, ID: cj.ID}, nil
}

// AgeDays is the number of whole days between receivedAt and today.
func AgeDays(receivedAt, today time.Time) int {
	return int(DateOf(today).Sub(DateOf(receivedAt)).Hours() / 24)
}

// IsAging reports whether a vehicle of the given age is aging stock.
// "Over 90 days" means day 91 and later, so the comparison is strict.
func IsAging(age, threshold int) bool { return age > threshold }

// AgingCutoff is the latest received_at date that is aging for the given
// threshold: received_at <= AgingCutoff(today, t) <=> AgeDays > t.
func AgingCutoff(today time.Time, threshold int) time.Time {
	return DateOf(today).AddDate(0, 0, -(threshold + 1))
}

// Summary aggregates aging stock for the dashboard.
type Summary struct {
	TotalInStock  int
	AgingCount    int
	AgingByMake   []MakeCount
	AgingByAction []ActionCount
}

// MakeCount is a per-make aging count.
type MakeCount struct {
	Make  string
	Count int
}

// ActionCount is a per-current-action aging count; nil Action means
// no action has been logged yet.
type ActionCount struct {
	Action *ActionType
	Count  int
}

// UpsertResult reports what an ingest batch changed.
type UpsertResult struct {
	Inserted int
	Updated  int
}
