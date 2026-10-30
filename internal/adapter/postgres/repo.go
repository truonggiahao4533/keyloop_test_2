// Package postgres implements the domain repository ports on PostgreSQL.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig configures the connection pool.
type PoolConfig struct {
	URL      string
	MaxConns int32
}

// NewPool opens a pgx pool and verifies connectivity.
func NewPool(ctx context.Context, pc PoolConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(pc.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if pc.MaxConns > 0 {
		cfg.MaxConns = pc.MaxConns
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}
