package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// VehicleRepo reads and writes vehicles.
type VehicleRepo interface {
	// List returns in-stock vehicles ordered by (received_at, id), starting
	// after c (nil for the first page), and the cursor of the next page.
	List(ctx context.Context, f VehicleFilter, c *Cursor, limit int) ([]VehicleRow, *Cursor, error)
	Summary(ctx context.Context, today time.Time, threshold int) (Summary, error)
	// Upsert inserts or updates vehicles by VIN. It never touches the
	// current action fields.
	Upsert(ctx context.Context, vs []Vehicle) (UpsertResult, error)
}

// ActionRepo records manager actions.
type ActionRepo interface {
	// Log stores the action atomically with the idempotency record. If
	// idemKey was already used with reqHash it returns the stored action and
	// replay=true; with a different hash it returns ErrConflict.
	Log(ctx context.Context, vehicleID uuid.UUID, a NewAction, idemKey, reqHash string) (Action, bool, error)
	// History returns the vehicle's actions, newest first.
	History(ctx context.Context, vehicleID uuid.UUID, limit int) ([]Action, error)
}

// SettingsRepo reads and writes the dealership configuration.
type SettingsRepo interface {
	Threshold(ctx context.Context) (int, error)
	SetThreshold(ctx context.Context, days int) error
}
