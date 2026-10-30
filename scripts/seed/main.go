// Command seed loads a realistic single-dealership stock into the database.
//
//	seed                 ~500 vehicles (the MVP demo data set)
//	seed -n 250000       load-test volume (performance testing only)
//	seed -reset          wipe vehicles/actions/idempotency keys first
//	seed -if-empty       do nothing when vehicles already exist
//
// received_at is relative to today, spread over 0-240 days with ~30% older
// than 90 days. Three VINs BNDRY0000000000{89,90,91} sit exactly on the
// aging boundary for demos.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/big"
	"math/rand/v2"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type model struct {
	name string
	base int // MSRP-ish base price in dollars
}

var catalog = []struct {
	make   string
	models []model
}{
	{"Toyota", []model{{"Corolla", 22000}, {"Camry", 28000}, {"RAV4", 31000}, {"Highlander", 40000}, {"Tacoma", 33000}}},
	{"Honda", []model{{"Civic", 24000}, {"Accord", 29000}, {"CR-V", 32000}, {"Pilot", 41000}}},
	{"Ford", []model{{"F-150", 45000}, {"Escape", 29000}, {"Explorer", 39000}, {"Mustang", 35000}}},
	{"Chevrolet", []model{{"Silverado", 44000}, {"Equinox", 28000}, {"Malibu", 25000}}},
	{"Nissan", []model{{"Altima", 26000}, {"Rogue", 29000}, {"Sentra", 21000}}},
	{"Hyundai", []model{{"Elantra", 22000}, {"Tucson", 29000}, {"Santa Fe", 33000}}},
	{"Kia", []model{{"Forte", 21000}, {"Sportage", 28000}, {"Telluride", 38000}}},
	{"Subaru", []model{{"Forester", 29000}, {"Outback", 31000}}},
}

var actionTypes = []string{
	"PRICE_REDUCTION_PLANNED", "PRICE_REDUCED", "TRANSFER_PLANNED",
	"WHOLESALE_PLANNED", "PROMOTION_PLANNED", "NO_ACTION_NEEDED",
}

var notes = []string{
	"Reviewed at weekly stock meeting",
	"Competitor listing is 5% lower",
	"Sister store has demand for this trim",
	"Feature in weekend promotion",
	"Send to auction if unsold in 2 weeks",
	"",
}

const vinChars = "ABCDEFGHJKLMNPRSTUVWXYZ0123456789" // no I, O, Q

type vehicle struct {
	id         uuid.UUID
	vin        string
	make       string
	model      string
	year       int32
	price      pgtype.Numeric
	receivedAt time.Time
	status     string
	actionType *string
	actionAt   *time.Time
}

type action struct {
	id         uuid.UUID
	vehicleID  uuid.UUID
	actionType string
	note       *string
	createdAt  time.Time
}

func main() {
	n := flag.Int("n", 500, "number of vehicles")
	reset := flag.Bool("reset", false, "truncate existing data first")
	ifEmpty := flag.Bool("if-empty", false, "skip when vehicles already exist")
	seed := flag.Uint64("seed", 42, "random seed")
	flag.Parse()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Fatal("DATABASE_URL is required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close(ctx)
	enum, err := conn.LoadType(ctx, "action_type")
	if err != nil {
		log.Fatal(err)
	}
	conn.TypeMap().RegisterType(enum)

	if *ifEmpty {
		var count int
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM vehicles").Scan(&count); err != nil {
			log.Fatal(err)
		}
		if count > 0 {
			fmt.Printf("seed: %d vehicles already present, skipping\n", count)
			return
		}
	}

	start := time.Now()
	rng := rand.New(rand.NewPCG(*seed, *seed)) //nolint:gosec // reproducible demo data, not security-sensitive
	vs, as := generate(*n, rng, time.Now().UTC())

	tx, err := conn.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if *reset {
		if _, err := tx.Exec(ctx, "TRUNCATE vehicle_actions, vehicles, idempotency_keys"); err != nil {
			log.Fatal(err)
		}
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vehicles"},
		[]string{"id", "vin", "make", "model", "year", "price", "received_at", "status", "current_action_type", "current_action_at"},
		pgx.CopyFromSlice(len(vs), func(i int) ([]any, error) {
			v := vs[i]
			return []any{v.id, v.vin, v.make, v.model, v.year, v.price, v.receivedAt, v.status, v.actionType, v.actionAt}, nil
		})); err != nil {
		log.Fatal("copy vehicles: ", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"vehicle_actions"},
		[]string{"id", "vehicle_id", "action_type", "note", "created_by", "created_at"},
		pgx.CopyFromSlice(len(as), func(i int) ([]any, error) {
			a := as[i]
			return []any{a.id, a.vehicleID, a.actionType, a.note, "seed", a.createdAt}, nil
		})); err != nil {
		log.Fatal("copy actions: ", err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err := conn.Exec(ctx, "ANALYZE vehicles; ANALYZE vehicle_actions"); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("seed: %d vehicles, %d actions in %s\n", len(vs), len(as), time.Since(start).Round(time.Millisecond))
}

func generate(n int, rng *rand.Rand, now time.Time) ([]vehicle, []action) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	vins := map[string]bool{}
	vs := make([]vehicle, 0, n)
	var as []action

	add := func(vin string, age int, demo bool) {
		c := catalog[rng.IntN(len(catalog))]
		m := c.models[rng.IntN(len(c.models))]
		year := int32(today.Year() - rng.IntN(4))
		// Older model years and random trim spread, priced like "24,899".
		dollars := float64(m.base) * (1 - 0.07*float64(int32(today.Year())-year)) * (0.95 + 0.15*rng.Float64())
		cents := int64(dollars/100)*10000 - 100
		v := vehicle{
			id:         uuid.Must(uuid.NewV7()),
			vin:        vin,
			make:       c.make,
			model:      m.name,
			year:       year,
			price:      pgtype.Numeric{Int: big.NewInt(cents), Exp: -2, Valid: true},
			receivedAt: today.AddDate(0, 0, -age),
			status:     "IN_STOCK",
		}
		if !demo && rng.IntN(100) < 3 {
			v.status = "SOLD"
		}
		// Managers have acted on ~40% of aging stock and a few younger cars.
		if (age > 90 && rng.IntN(100) < 40) || (age > 60 && rng.IntN(100) < 10) {
			k := 1 + rng.IntN(2)
			at := v.receivedAt.AddDate(0, 0, 60).Add(time.Duration(rng.IntN(24*60)) * time.Minute)
			for range k {
				at = at.Add(time.Duration(1+rng.IntN((age-59)*24)) * time.Hour)
				if at.After(now) {
					at = now.Add(-time.Duration(rng.IntN(3600)) * time.Second)
				}
				a := action{
					id:         uuid.Must(uuid.NewV7()),
					vehicleID:  v.id,
					actionType: actionTypes[rng.IntN(len(actionTypes))],
					createdAt:  at,
				}
				if note := notes[rng.IntN(len(notes))]; note != "" {
					a.note = &note
				}
				as = append(as, a)
				v.actionType, v.actionAt = &a.actionType, &a.createdAt
			}
		}
		vins[vin] = true
		vs = append(vs, v)
	}

	for _, age := range []int{89, 90, 91} {
		add(fmt.Sprintf("BNDRY%012d", age), age, true)
	}
	for len(vs) < n {
		age := rng.IntN(91) // 0..90
		if rng.IntN(100) < 30 {
			age = 91 + rng.IntN(150) // 91..240
		}
		vin := randomVIN(rng)
		if vins[vin] {
			continue
		}
		add(vin, age, false)
	}
	return vs, as
}

func randomVIN(rng *rand.Rand) string {
	b := make([]byte, 17)
	for i := range b {
		b[i] = vinChars[rng.IntN(len(vinChars))]
	}
	return string(b)
}
