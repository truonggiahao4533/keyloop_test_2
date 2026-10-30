// Package pgtest starts a throwaway PostgreSQL (same image as compose) for
// integration tests. The database is never mocked.
package pgtest

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres"
)

// Image must match docker-compose.yml.
const Image = "postgres:18"

var pool *pgxpool.Pool

// Main starts the container, migrates it, runs the tests and cleans up.
// Call it from TestMain.
func Main(m *testing.M) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, Image,
		tcpostgres.WithDatabase("inventory"),
		tcpostgres.WithUsername("inventory"),
		tcpostgres.WithPassword("inventory"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start postgres:", err)
		os.Exit(1)
	}
	code := func() int {
		defer func() { _ = ctr.Terminate(context.Background()) }()
		url, err := ctr.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		pool, err = postgres.NewPool(ctx, postgres.PoolConfig{URL: url, MaxConns: 20})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer pool.Close()
		if err := postgres.Migrate(ctx, pool); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

// Pool returns a pool to a clean database. Tests using it must not run in
// parallel with each other.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, `
		TRUNCATE vehicle_actions, vehicles, idempotency_keys;
		UPDATE app_settings SET aging_threshold_days = 90;`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
	return pool
}
