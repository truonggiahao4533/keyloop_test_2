package app

import (
	"context"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

// Threshold bounds, matching the app_settings CHECK constraint.
const (
	MinThresholdDays = 1
	MaxThresholdDays = 3650
)

// AgingThreshold returns the configured aging threshold in days.
func (s *Service) AgingThreshold(ctx context.Context) (int, error) {
	return s.settings.Threshold(ctx)
}

// SetAgingThreshold changes the aging threshold. Aging is derived at query
// time, so the change applies to the next request with no recomputation.
func (s *Service) SetAgingThreshold(ctx context.Context, days int) error {
	if days < MinThresholdDays || days > MaxThresholdDays {
		return domain.Validationf("days must be between %d and %d", MinThresholdDays, MaxThresholdDays)
	}
	return s.settings.SetThreshold(ctx, days)
}
