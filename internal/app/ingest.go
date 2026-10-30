package app

import (
	"context"
	"strings"
	"time"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

// MaxIngestBatch is the largest accepted ingest batch.
const MaxIngestBatch = 1000

// IngestVehicle is one vehicle from the inventory feed.
type IngestVehicle struct {
	VIN        string
	Make       string
	Model      string
	Year       int
	Price      *string // decimal, e.g. "24999.00"; nil for no price
	ReceivedAt string  // YYYY-MM-DD
	Status     string  // empty means IN_STOCK
}

// IngestResult reports what a batch changed. Unchanged rows are neither
// inserted nor updated.
type IngestResult struct {
	Inserted  int
	Updated   int
	Unchanged int
}

// Ingest validates the batch and upserts it by VIN. It never changes a
// vehicle's current action.
func (s *Service) Ingest(ctx context.Context, batch []IngestVehicle) (IngestResult, error) {
	if len(batch) == 0 || len(batch) > MaxIngestBatch {
		return IngestResult{}, domain.Validationf("vehicles must contain 1 to %d items", MaxIngestBatch)
	}
	today := s.clock.Today()
	seen := make(map[string]int, len(batch))
	vs := make([]domain.Vehicle, len(batch))
	for i, in := range batch {
		v, err := toVehicle(in, today)
		if err != nil {
			return IngestResult{}, domain.Validationf("vehicles[%d]: %s", i, err.Error())
		}
		if j, dup := seen[v.VIN]; dup {
			return IngestResult{}, domain.Validationf("vehicles[%d]: duplicate vin %s (also at index %d)", i, v.VIN, j)
		}
		seen[v.VIN] = i
		vs[i] = v
	}
	res, err := s.vehicles.Upsert(ctx, vs)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Inserted: res.Inserted, Updated: res.Updated, Unchanged: len(vs) - res.Inserted - res.Updated}, nil
}

func toVehicle(in IngestVehicle, today time.Time) (domain.Vehicle, error) {
	v := domain.Vehicle{
		VIN:    strings.ToUpper(strings.TrimSpace(in.VIN)),
		Make:   strings.TrimSpace(in.Make),
		Model:  strings.TrimSpace(in.Model),
		Year:   in.Year,
		Status: domain.StatusInStock,
	}
	if !validVIN(v.VIN) {
		return v, domain.Validationf("vin must be 17 characters A-Z/0-9 excluding I, O, Q")
	}
	if v.Make == "" || len(v.Make) > 64 || v.Model == "" || len(v.Model) > 64 {
		return v, domain.Validationf("make and model are required (max 64 characters)")
	}
	if v.Year < 1900 || v.Year > today.Year()+2 {
		return v, domain.Validationf("year %d is out of range", v.Year)
	}
	if in.Price != nil {
		c, err := parsePriceCents(*in.Price)
		if err != nil {
			return v, err
		}
		v.PriceCents = &c
	}
	d, err := time.Parse(time.DateOnly, in.ReceivedAt)
	if err != nil {
		return v, domain.Validationf("received_at must be a YYYY-MM-DD date")
	}
	if d.After(today) {
		return v, domain.Validationf("received_at must not be in the future")
	}
	v.ReceivedAt = d
	if in.Status != "" {
		if v.Status, err = domain.ParseVehicleStatus(in.Status); err != nil {
			return v, err
		}
	}
	return v, nil
}

func validVIN(s string) bool {
	if len(s) != 17 {
		return false
	}
	for _, r := range s {
		alnum := r >= '0' && r <= '9' || r >= 'A' && r <= 'Z'
		if !alnum || r == 'I' || r == 'O' || r == 'Q' {
			return false
		}
	}
	return true
}

// parsePriceCents parses a non-negative decimal with at most 2 fraction
// digits that fits NUMERIC(12,2).
func parsePriceCents(s string) (int64, error) {
	bad := domain.Validationf("price must be a non-negative number with at most 2 decimals, below 10000000000")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" || len(whole) > 10 || len(frac) > 2 {
		return 0, bad
	}
	var cents int64
	for _, r := range whole + (frac + "00")[:2] {
		if r < '0' || r > '9' {
			return 0, bad
		}
		cents = cents*10 + int64(r-'0')
	}
	return cents, nil
}
