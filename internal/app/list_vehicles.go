package app

import (
	"context"
	"time"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

// Paging limits for list endpoints.
const (
	DefaultLimit = 50
	MaxLimit     = 200
	maxAgeDays   = 36500
)

// ListQuery is a vehicle list request. Nil/empty fields are not applied.
type ListQuery struct {
	Make       *string
	Model      *string
	MinAgeDays *int
	MaxAgeDays *int
	Aging      bool // only vehicles older than the configured threshold
	Cursor     string
	Limit      *int // nil means DefaultLimit
}

// VehicleView is a vehicle with its age derived at query time.
type VehicleView struct {
	domain.VehicleRow
	AgeDays int
	IsAging bool
}

// VehiclePage is one page of a vehicle listing.
type VehiclePage struct {
	Items      []VehicleView
	NextCursor string // empty on the last page
}

// ListVehicles returns in-stock vehicles, oldest first. Age filters are
// translated into received_at bounds so the (received_at, id) index is used.
func (s *Service) ListVehicles(ctx context.Context, q ListQuery) (VehiclePage, error) {
	limit, err := resolveLimit(q.Limit)
	if err != nil {
		return VehiclePage{}, err
	}
	if err := validateAges(q.MinAgeDays, q.MaxAgeDays); err != nil {
		return VehiclePage{}, err
	}
	var cur *domain.Cursor
	if q.Cursor != "" {
		if cur, err = domain.DecodeCursor(q.Cursor); err != nil {
			return VehiclePage{}, err
		}
	}
	threshold, err := s.settings.Threshold(ctx) // once per request
	if err != nil {
		return VehiclePage{}, err
	}
	today := s.clock.Today()

	f := domain.VehicleFilter{Make: nonEmpty(q.Make), Model: nonEmpty(q.Model)}
	if q.MinAgeDays != nil {
		f.ReceivedBefore = earliest(f.ReceivedBefore, today.AddDate(0, 0, -*q.MinAgeDays))
	}
	if q.Aging {
		f.ReceivedBefore = earliest(f.ReceivedBefore, domain.AgingCutoff(today, threshold))
	}
	if q.MaxAgeDays != nil {
		d := today.AddDate(0, 0, -*q.MaxAgeDays)
		f.ReceivedAfter = &d
	}

	rows, next, err := s.vehicles.List(ctx, f, cur, limit)
	if err != nil {
		return VehiclePage{}, err
	}
	page := VehiclePage{Items: make([]VehicleView, len(rows))}
	for i, r := range rows {
		age := domain.AgeDays(r.ReceivedAt, today)
		page.Items[i] = VehicleView{VehicleRow: r, AgeDays: age, IsAging: domain.IsAging(age, threshold)}
	}
	if next != nil {
		page.NextCursor = next.Encode()
	}
	return page, nil
}

func resolveLimit(l *int) (int, error) {
	if l == nil {
		return DefaultLimit, nil
	}
	if *l < 1 || *l > MaxLimit {
		return 0, domain.Validationf("limit must be between 1 and %d", MaxLimit)
	}
	return *l, nil
}

func validateAges(minAge, maxAge *int) error {
	for name, v := range map[string]*int{"min_age_days": minAge, "max_age_days": maxAge} {
		if v != nil && (*v < 0 || *v > maxAgeDays) {
			return domain.Validationf("%s must be between 0 and %d", name, maxAgeDays)
		}
	}
	if minAge != nil && maxAge != nil && *minAge > *maxAge {
		return domain.Validationf("min_age_days must not exceed max_age_days")
	}
	return nil
}

func nonEmpty(s *string) *string {
	if s == nil || *s == "" {
		return nil
	}
	return s
}

func earliest(cur *time.Time, t time.Time) *time.Time {
	if cur != nil && cur.Before(t) {
		return cur
	}
	return &t
}
