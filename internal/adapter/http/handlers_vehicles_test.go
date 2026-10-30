package http

import (
	"context"
	"slices"
	"testing"

	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func seedListFixture(e *testEnv) {
	sold := veh(99, "Toyota", "Corolla", 300)
	sold.Status = domain.StatusSold
	e.seed(
		veh(1, "Toyota", "Corolla", 0),
		veh(2, "Toyota", "Camry", 10),
		veh(3, "Honda", "Civic", 89),
		veh(4, "Honda", "Civic", 90),
		veh(5, "Toyota", "Corolla", 91),
		veh(6, "Ford", "F-150", 100),
		veh(7, "Honda", "Accord", 200),
		sold,
	)
}

func TestListVehiclesFilters(t *testing.T) {
	e := newTestEnv(t)
	seedListFixture(e)
	cases := []struct {
		query string
		want  []int // ages, oldest first
	}{
		{"", []int{200, 100, 91, 90, 89, 10, 0}},
		{"?make=Toyota", []int{91, 10, 0}},
		{"?model=Civic", []int{90, 89}},
		{"?make=Honda&model=Civic", []int{90, 89}},
		{"?make=Honda&model=Corolla", nil},
		{"?make=&model=", []int{200, 100, 91, 90, 89, 10, 0}},
		{"?min_age_days=90", []int{200, 100, 91, 90}},
		{"?max_age_days=90", []int{90, 89, 10, 0}},
		{"?min_age_days=89&max_age_days=91", []int{91, 90, 89}},
		{"?min_age_days=90&max_age_days=90", []int{90}},
		{"?aging=true", []int{200, 100, 91}},
		{"?aging=false", []int{200, 100, 91, 90, 89, 10, 0}},
		{"?aging=true&make=Toyota", []int{91}},
		{"?aging=true&make=Honda&model=Civic", nil},
		{"?aging=true&max_age_days=150", []int{100, 91}},
		{"?aging=true&min_age_days=150", []int{200}},
		{"?aging=true&min_age_days=10", []int{200, 100, 91}},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			if got := e.listAll("/v1/vehicles" + c.query); !slices.Equal(got, c.want) {
				t.Fatalf("ages = %v, want %v", got, c.want)
			}
		})
	}
}

func TestListVehiclesBoundaryAndFields(t *testing.T) {
	e := newTestEnv(t)
	seedListFixture(e)
	r := e.do(platform.RoleViewer, "GET", "/v1/vehicles?min_age_days=89&max_age_days=91", nil)
	var l listResp
	r.decode(t, &l)
	want := map[int]bool{89: false, 90: false, 91: true}
	if len(l.Items) != 3 {
		t.Fatalf("got %d items", len(l.Items))
	}
	for _, it := range l.Items {
		if it.IsAging != want[it.AgeDays] {
			t.Errorf("age %d: is_aging=%v, want %v", it.AgeDays, it.IsAging, want[it.AgeDays])
		}
		if it.Price == nil || it.ReceivedAt != today.AddDate(0, 0, -it.AgeDays).Format("2006-01-02") || it.CurrentAction != nil {
			t.Errorf("unexpected fields %+v", it)
		}
	}
	if l.NextCursor != nil {
		t.Errorf("next_cursor = %v, want null", *l.NextCursor)
	}

	// The threshold is read from settings: at 89 days, the 90-day car ages.
	if err := e.repos.Settings.SetThreshold(context.Background(), 89); err != nil {
		t.Fatal(err)
	}
	if got := e.listAll("/v1/vehicles?aging=true"); !slices.Equal(got, []int{200, 100, 91, 90}) {
		t.Fatalf("aging at threshold 89 = %v", got)
	}
}

func TestListVehiclesPaginationAndDefaults(t *testing.T) {
	e := newTestEnv(t)
	var vs []domain.Vehicle
	for i := range 120 {
		vs = append(vs, veh(i, "Kia", "Forte", i%7))
	}
	e.seed(vs...)

	var l listResp
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(t, &l)
	if len(l.Items) != 50 || l.NextCursor == nil {
		t.Fatalf("default page: %d items, next=%v", len(l.Items), l.NextCursor)
	}
	e.do(platform.RoleViewer, "GET", "/v1/vehicles?limit=200", nil).decode(t, &l)
	if len(l.Items) != 120 || l.NextCursor != nil {
		t.Fatalf("limit 200: %d items, next=%v", len(l.Items), l.NextCursor)
	}

	seen := map[string]bool{}
	path := "/v1/vehicles?limit=13"
	for page := 0; ; page++ {
		var p listResp
		e.do(platform.RoleViewer, "GET", path, nil).decode(t, &p)
		for _, it := range p.Items {
			if seen[it.ID] {
				t.Fatalf("duplicate %s", it.ID)
			}
			seen[it.ID] = true
		}
		if p.NextCursor == nil {
			break
		}
		path = "/v1/vehicles?limit=13&cursor=" + *p.NextCursor
	}
	if len(seen) != 120 {
		t.Fatalf("paged through %d vehicles, want 120", len(seen))
	}
}

func TestListVehiclesValidation(t *testing.T) {
	e := newTestEnv(t)
	for _, q := range []string{
		"limit=0", "limit=201", "limit=-1", "limit=abc", "limit=1.5",
		"min_age_days=-1", "max_age_days=x", "min_age_days=10&max_age_days=5",
		"aging=maybe", "cursor=not-a-cursor", "cursor=e30",
	} {
		t.Run(q, func(t *testing.T) {
			r := e.do(platform.RoleViewer, "GET", "/v1/vehicles?"+q, nil)
			if r.Status != 400 {
				t.Fatalf("status = %d, want 400: %s", r.Status, r.Body)
			}
			var er errorResponse
			r.decode(t, &er)
			if er.Error.Code != "VALIDATION_ERROR" {
				t.Fatalf("code = %q", er.Error.Code)
			}
		})
	}
}
