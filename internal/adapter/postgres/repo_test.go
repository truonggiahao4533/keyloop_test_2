package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres"
	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres/pgtest"
	"github.com/haotruong/inventory-dashboard/internal/domain"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

var today = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func vin(i int) string { return fmt.Sprintf("TESTVIN%010d", i) }

func vehicle(i int, mk, model string, ageDays int) domain.Vehicle {
	return domain.Vehicle{
		VIN: vin(i), Make: mk, Model: model, Year: 2024,
		PriceCents: ptr(int64(2500000 + i)),
		ReceivedAt: today.AddDate(0, 0, -ageDays),
		Status:     domain.StatusInStock,
	}
}

func listAll(t *testing.T, r *postgres.VehicleRepo, f domain.VehicleFilter, pageSize int) []domain.VehicleRow {
	t.Helper()
	var (
		all []domain.VehicleRow
		cur *domain.Cursor
	)
	for range 1000 {
		page, next, err := r.List(context.Background(), f, cur, pageSize)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > pageSize {
			t.Fatalf("page has %d rows, limit %d", len(page), pageSize)
		}
		all = append(all, page...)
		if next == nil {
			return all
		}
		cur = next
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func TestListFilters(t *testing.T) {
	repos := postgres.NewRepos(pgtest.Pool(t), 0)
	ctx := context.Background()
	vs := []domain.Vehicle{
		vehicle(1, "Toyota", "Corolla", 10),
		vehicle(2, "Toyota", "Camry", 95),
		vehicle(3, "Honda", "Civic", 91),
		vehicle(4, "Honda", "Civic", 90),
		vehicle(5, "Ford", "F-150", 200),
	}
	sold := vehicle(6, "Toyota", "Corolla", 150)
	sold.Status = domain.StatusSold
	vs = append(vs, sold)
	if _, err := repos.Vehicles.Upsert(ctx, vs); err != nil {
		t.Fatal(err)
	}

	vins := func(rows []domain.VehicleRow) []string {
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.VIN
		}
		return out
	}
	cases := []struct {
		name string
		f    domain.VehicleFilter
		want []string // in (received_at, id) order: oldest first
	}{
		{"all in stock, sold excluded", domain.VehicleFilter{}, []string{vin(5), vin(2), vin(3), vin(4), vin(1)}},
		{"make", domain.VehicleFilter{Make: ptr("Toyota")}, []string{vin(2), vin(1)}},
		{"make+model", domain.VehicleFilter{Make: ptr("Honda"), Model: ptr("Civic")}, []string{vin(3), vin(4)}},
		{"model only", domain.VehicleFilter{Model: ptr("Corolla")}, []string{vin(1)}},
		{"received_before (aging cutoff 90)", domain.VehicleFilter{ReceivedBefore: ptr(domain.AgingCutoff(today, 90))}, []string{vin(5), vin(2), vin(3)}},
		{"received_after", domain.VehicleFilter{ReceivedAfter: ptr(today.AddDate(0, 0, -91))}, []string{vin(3), vin(4), vin(1)}},
		{"range", domain.VehicleFilter{ReceivedBefore: ptr(today.AddDate(0, 0, -90)), ReceivedAfter: ptr(today.AddDate(0, 0, -95))}, []string{vin(2), vin(3), vin(4)}},
		{"no match", domain.VehicleFilter{Make: ptr("Kia")}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := vins(listAll(t, repos.Vehicles, c.f, 2))
			if !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestListPaginationNoGapsNoDuplicates(t *testing.T) {
	repos := postgres.NewRepos(pgtest.Pool(t), 0)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(1, 2))
	const n = 237
	vs := make([]domain.Vehicle, n)
	for i := range vs {
		// Few distinct dates so many rows tie on received_at.
		vs[i] = vehicle(i, "Toyota", "Corolla", rng.IntN(15))
	}
	if _, err := repos.Vehicles.Upsert(ctx, vs); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{1, 7, 50, 236, 237, 500} {
		rows := listAll(t, repos.Vehicles, domain.VehicleFilter{}, size)
		if len(rows) != n {
			t.Fatalf("page size %d: got %d rows, want %d", size, len(rows), n)
		}
		seen := map[uuid.UUID]bool{}
		for i, r := range rows {
			if seen[r.ID] {
				t.Fatalf("page size %d: duplicate %s", size, r.ID)
			}
			seen[r.ID] = true
			if i > 0 {
				p := rows[i-1]
				if r.ReceivedAt.Before(p.ReceivedAt) || (r.ReceivedAt.Equal(p.ReceivedAt) && r.ID.String() <= p.ID.String()) {
					t.Fatalf("page size %d: out of order at %d", size, i)
				}
			}
		}
	}
}

func TestUpsertIsIdempotentAndKeepsCurrentAction(t *testing.T) {
	repos := postgres.NewRepos(pgtest.Pool(t), 0)
	ctx := context.Background()
	vs := []domain.Vehicle{vehicle(1, "Toyota", "Corolla", 10), vehicle(2, "Honda", "Civic", 100)}
	res, err := repos.Vehicles.Upsert(ctx, vs)
	if err != nil || res != (domain.UpsertResult{Inserted: 2}) {
		t.Fatalf("first upsert = %+v, %v", res, err)
	}
	res, err = repos.Vehicles.Upsert(ctx, vs)
	if err != nil || res != (domain.UpsertResult{}) {
		t.Fatalf("same batch again = %+v, %v", res, err)
	}

	rows, _, _ := repos.Vehicles.List(ctx, domain.VehicleFilter{Make: ptr("Honda")}, nil, 10)
	if _, _, err := repos.Actions.Log(ctx, rows[0].ID, domain.NewAction{ID: uuid.New(), Type: domain.ActionPriceReduced, CreatedBy: "m"}, "k1", "h1"); err != nil {
		t.Fatal(err)
	}
	vs[1].PriceCents = ptr(int64(1999900))
	res, err = repos.Vehicles.Upsert(ctx, vs)
	if err != nil || res != (domain.UpsertResult{Updated: 1}) {
		t.Fatalf("changed batch = %+v, %v", res, err)
	}
	rows, _, _ = repos.Vehicles.List(ctx, domain.VehicleFilter{Make: ptr("Honda")}, nil, 10)
	if got := rows[0]; *got.PriceCents != 1999900 || got.CurrentAction == nil || *got.CurrentAction != domain.ActionPriceReduced {
		t.Fatalf("after update: price=%v action=%v", *got.PriceCents, got.CurrentAction)
	}
}

func TestLogActionIdempotencyAndErrors(t *testing.T) {
	repos := postgres.NewRepos(pgtest.Pool(t), 0)
	ctx := context.Background()
	if _, err := repos.Vehicles.Upsert(ctx, []domain.Vehicle{vehicle(1, "Toyota", "Corolla", 100)}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := repos.Vehicles.List(ctx, domain.VehicleFilter{}, nil, 1)
	id := rows[0].ID
	na := domain.NewAction{ID: uuid.New(), Type: domain.ActionTransferPlanned, Note: ptr("to north lot"), CreatedBy: "alice"}

	first, replay, err := repos.Actions.Log(ctx, id, na, "key-1", "hash-a")
	if err != nil || replay {
		t.Fatalf("first log: replay=%v err=%v", replay, err)
	}
	again, replay, err := repos.Actions.Log(ctx, id, domain.NewAction{ID: uuid.New(), Type: domain.ActionTransferPlanned, Note: ptr("to north lot"), CreatedBy: "alice"}, "key-1", "hash-a")
	if err != nil || !replay || again.ID != first.ID || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replay: %+v replay=%v err=%v", again, replay, err)
	}
	if _, _, err := repos.Actions.Log(ctx, id, na, "key-1", "hash-b"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("different hash: err=%v, want ErrConflict", err)
	}
	if _, _, err := repos.Actions.Log(ctx, uuid.New(), na, "key-2", "hash-c"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown vehicle: err=%v, want ErrNotFound", err)
	}
	hist, err := repos.Actions.History(ctx, id, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history = %d rows, %v", len(hist), err)
	}
	if _, err := repos.Actions.History(ctx, uuid.New(), 10); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("history unknown vehicle: err=%v", err)
	}
}

func TestLogActionConcurrent(t *testing.T) {
	repos := postgres.NewRepos(pgtest.Pool(t), 0)
	ctx := context.Background()
	if _, err := repos.Vehicles.Upsert(ctx, []domain.Vehicle{vehicle(1, "Toyota", "Corolla", 100)}); err != nil {
		t.Fatal(err)
	}
	rows, _, _ := repos.Vehicles.List(ctx, domain.VehicleFilter{}, nil, 1)
	id := rows[0].ID

	const n = 10
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			typ := domain.ActionTypes[i%len(domain.ActionTypes)]
			_, _, err := repos.Actions.Log(ctx, id, domain.NewAction{ID: uuid.New(), Type: typ, CreatedBy: "m"}, fmt.Sprintf("k%d", i), "h")
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	hist, err := repos.Actions.History(ctx, id, 100)
	if err != nil || len(hist) != n {
		t.Fatalf("history = %d rows, %v", len(hist), err)
	}
	rows, _, _ = repos.Vehicles.List(ctx, domain.VehicleFilter{}, nil, 1)
	if *rows[0].CurrentAction != hist[0].Type || !rows[0].CurrentActionAt.Equal(hist[0].CreatedAt) {
		t.Fatalf("current action %v@%v, latest history %v@%v", *rows[0].CurrentAction, rows[0].CurrentActionAt, hist[0].Type, hist[0].CreatedAt)
	}
}

func TestSettings(t *testing.T) {
	repos := postgres.NewRepos(pgtest.Pool(t), 0)
	ctx := context.Background()
	if d, err := repos.Settings.Threshold(ctx); err != nil || d != 90 {
		t.Fatalf("default threshold = %d, %v", d, err)
	}
	if err := repos.Settings.SetThreshold(ctx, 60); err != nil {
		t.Fatal(err)
	}
	if d, _ := repos.Settings.Threshold(ctx); d != 60 {
		t.Fatalf("threshold = %d, want 60", d)
	}
}
