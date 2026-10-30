package app

import (
	"context"
	"math"

	"github.com/haotruong/inventory-dashboard/internal/domain"
)

// DashboardSummary is the aging overview for managers.
type DashboardSummary struct {
	domain.Summary
	AgingPct      float64 // percent of in-stock vehicles that are aging, 1 decimal
	ThresholdDays int
}

// Summary computes the dashboard summary for today and the configured threshold.
func (s *Service) Summary(ctx context.Context) (DashboardSummary, error) {
	threshold, err := s.settings.Threshold(ctx)
	if err != nil {
		return DashboardSummary{}, err
	}
	sum, err := s.vehicles.Summary(ctx, s.clock.Today(), threshold)
	if err != nil {
		return DashboardSummary{}, err
	}
	out := DashboardSummary{Summary: sum, ThresholdDays: threshold}
	if sum.TotalInStock > 0 {
		out.AgingPct = math.Round(1000*float64(sum.AgingCount)/float64(sum.TotalInStock)) / 10
	}
	return out, nil
}
