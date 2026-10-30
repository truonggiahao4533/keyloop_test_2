package http

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func TestAgingEndpoint(t *testing.T) {
	e := newTestEnv(t)
	seedListFixture(e)
	if got := e.listAll("/v1/vehicles/aging?limit=1"); !slices.Equal(got, []int{200, 100, 91}) {
		t.Fatalf("aging ages = %v", got)
	}
	if got := e.listAll("/v1/vehicles/aging?make=Honda"); !slices.Equal(got, []int{200}) {
		t.Fatalf("aging Honda ages = %v", got)
	}
	if r := e.do(platform.RoleViewer, "GET", "/v1/vehicles/aging?limit=500", nil); r.Status != 400 {
		t.Fatalf("bad limit status = %d", r.Status)
	}
}

type summaryResp struct {
	TotalInStock  int     `json:"total_in_stock"`
	AgingCount    int     `json:"aging_count"`
	AgingPct      float64 `json:"aging_pct"`
	ThresholdDays int     `json:"threshold_days"`
	AgingByMake   []struct {
		Make  string `json:"make"`
		Count int    `json:"count"`
	} `json:"aging_by_make"`
	AgingByAction []struct {
		Action *string `json:"action"`
		Count  int     `json:"count"`
	} `json:"aging_by_action"`
}

func TestSummaryMatchesBruteForce(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	rng := rand.New(rand.NewPCG(7, 7))
	makes := []string{"Toyota", "Honda", "Ford", "Kia"}

	type fixture struct {
		v      domain.Vehicle
		action *domain.ActionType
	}
	var fs []fixture
	for i := range 400 {
		v := veh(i, makes[rng.IntN(len(makes))], "M", rng.IntN(240))
		if rng.IntN(20) == 0 {
			v.Status = domain.StatusSold
		}
		fs = append(fs, fixture{v: v})
	}
	vs := make([]domain.Vehicle, len(fs))
	for i := range fs {
		vs[i] = fs[i].v
	}
	e.seed(vs...)

	// Log actions (sometimes two; the latest counts) on in-stock vehicles.
	byVIN := map[string]uuid.UUID{}
	for _, r := range e.listAllRows(t) {
		byVIN[r.VIN] = r.ID
	}
	for i := range fs {
		if fs[i].v.Status != domain.StatusInStock || rng.IntN(3) != 0 {
			continue
		}
		for k := range 1 + rng.IntN(2) {
			typ := domain.ActionTypes[rng.IntN(len(domain.ActionTypes))]
			key := fmt.Sprintf("%d-%d", i, k)
			if _, _, err := e.repos.Actions.Log(ctx, byVIN[fs[i].v.VIN], domain.NewAction{ID: uuid.New(), Type: typ, CreatedBy: "t"}, key, key); err != nil {
				t.Fatal(err)
			}
			fs[i].action = &typ
		}
	}

	for _, threshold := range []int{90, 30} {
		if err := e.repos.Settings.SetThreshold(ctx, threshold); err != nil {
			t.Fatal(err)
		}
		// Brute force from the fixtures.
		total, aging := 0, 0
		wantMake := map[string]int{}
		wantAction := map[string]int{}
		for _, f := range fs {
			if f.v.Status != domain.StatusInStock {
				continue
			}
			total++
			if !domain.IsAging(domain.AgeDays(f.v.ReceivedAt, today), threshold) {
				continue
			}
			aging++
			wantMake[f.v.Make]++
			a := "<none>"
			if f.action != nil {
				a = string(*f.action)
			}
			wantAction[a]++
		}

		var s summaryResp
		r := e.do(platform.RoleViewer, "GET", "/v1/dashboard/summary", nil)
		if r.Status != 200 {
			t.Fatalf("summary status %d: %s", r.Status, r.Body)
		}
		r.decode(t, &s)
		if s.TotalInStock != total || s.AgingCount != aging || s.ThresholdDays != threshold {
			t.Fatalf("threshold %d: got total=%d aging=%d thr=%d, want %d %d", threshold, s.TotalInStock, s.AgingCount, s.ThresholdDays, total, aging)
		}
		if want := math.Round(1000*float64(aging)/float64(total)) / 10; s.AgingPct != want {
			t.Fatalf("aging_pct = %v, want %v", s.AgingPct, want)
		}
		gotMake := map[string]int{}
		for _, m := range s.AgingByMake {
			gotMake[m.Make] = m.Count
		}
		gotAction := map[string]int{}
		for i, a := range s.AgingByAction {
			k := "<none>"
			if a.Action != nil {
				k = *a.Action
			}
			gotAction[k] = a.Count
			if i > 0 && a.Count > s.AgingByAction[i-1].Count {
				t.Fatalf("aging_by_action not sorted by count desc")
			}
		}
		if fmt.Sprint(gotMake) != fmt.Sprint(wantMake) {
			t.Fatalf("by make = %v, want %v", gotMake, wantMake)
		}
		if fmt.Sprint(gotAction) != fmt.Sprint(wantAction) {
			t.Fatalf("by action = %v, want %v", gotAction, wantAction)
		}
	}
}

func TestSummaryEmpty(t *testing.T) {
	e := newTestEnv(t)
	var s summaryResp
	e.do(platform.RoleViewer, "GET", "/v1/dashboard/summary", nil).decode(t, &s)
	if s.TotalInStock != 0 || s.AgingPct != 0 || s.AgingByMake == nil || s.AgingByAction == nil {
		t.Fatalf("empty summary = %+v", s)
	}
}

func (e *testEnv) listAllRows(t *testing.T) []domain.VehicleRow {
	var all []domain.VehicleRow
	var cur *domain.Cursor
	for {
		rows, next, err := e.repos.Vehicles.List(context.Background(), domain.VehicleFilter{}, cur, 200)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, rows...)
		if next == nil {
			return all
		}
		cur = next
	}
}
