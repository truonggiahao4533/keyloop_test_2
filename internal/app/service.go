// Package app holds the use cases. It depends only on domain ports.
package app

import "github.com/haotruong/inventory-dashboard/internal/domain"

// Service implements the inventory use cases.
type Service struct {
	vehicles domain.VehicleRepo
	actions  domain.ActionRepo
	settings domain.SettingsRepo
	clock    domain.Clock
}

// New builds the Service.
func New(v domain.VehicleRepo, a domain.ActionRepo, s domain.SettingsRepo, c domain.Clock) *Service {
	return &Service{vehicles: v, actions: a, settings: s, clock: c}
}
