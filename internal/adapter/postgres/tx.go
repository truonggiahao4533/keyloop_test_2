package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/haotruong/inventory-dashboard/db/gen"
)

// withTx runs fn in a transaction, committing on success and rolling back
// on error or panic.
func withTx(ctx context.Context, pool *pgxpool.Pool, opts pgx.TxOptions, fn func(q *gen.Queries) error) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit
	if err := fn(gen.New(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
