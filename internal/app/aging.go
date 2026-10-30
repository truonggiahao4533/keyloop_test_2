package app

import "context"

// ListAging returns aging stock (older than the configured threshold),
// oldest first, with the same filters and pagination as ListVehicles.
func (s *Service) ListAging(ctx context.Context, q ListQuery) (VehiclePage, error) {
	q.Aging = true
	return s.ListVehicles(ctx, q)
}
