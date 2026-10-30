package http

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

type actionResp struct {
	ID         string  `json:"id"`
	VehicleID  string  `json:"vehicle_id"`
	ActionType string  `json:"action_type"`
	Note       *string `json:"note"`
	CreatedBy  string  `json:"created_by"`
	CreatedAt  string  `json:"created_at"`
}

// oneVehicle seeds a vehicle of the given age and returns its id.
func (e *testEnv) oneVehicle(age int) string {
	e.t.Helper()
	e.seed(veh(1, "Toyota", "Camry", age))
	var l listResp
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(e.t, &l)
	return l.Items[0].ID
}

func (e *testEnv) countActions() int {
	e.t.Helper()
	var n int
	if err := pgPool(e).QueryRow(context.Background(), "SELECT count(*) FROM vehicle_actions").Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestLogActionSuccessAndReplay(t *testing.T) {
	e := newTestEnv(t)
	id := e.oneVehicle(120)
	path := "/v1/vehicles/" + id + "/actions"
	body := `{"action_type":"PRICE_REDUCTION_PLANNED","note":"drop 5%"}`

	r1 := e.do(platform.RoleManager, "POST", path, body, "Idempotency-Key", "abc-1")
	if r1.Status != 201 || r1.Header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("first = %d %s", r1.Status, r1.Body)
	}
	var a actionResp
	r1.decode(t, &a)
	if a.VehicleID != id || a.ActionType != "PRICE_REDUCTION_PLANNED" || *a.Note != "drop 5%" || a.CreatedBy != "user-manager" {
		t.Fatalf("action = %+v", a)
	}

	// Same key, same request (different JSON formatting): stored response.
	r2 := e.do(platform.RoleManager, "POST", path, `{ "note": "drop 5%", "action_type": "PRICE_REDUCTION_PLANNED" }`, "Idempotency-Key", "abc-1")
	if r2.Status != 201 || r2.Header.Get("Idempotency-Replayed") != "true" || !bytes.Equal(r1.Body, r2.Body) {
		t.Fatalf("replay = %d %s, want identical 201", r2.Status, r2.Body)
	}
	if n := e.countActions(); n != 1 {
		t.Fatalf("%d action rows after replay, want 1", n)
	}

	// Same key, different body: 409.
	r3 := e.do(platform.RoleManager, "POST", path, `{"action_type":"PRICE_REDUCED"}`, "Idempotency-Key", "abc-1")
	if r3.Status != 409 {
		t.Fatalf("key reuse = %d %s, want 409", r3.Status, r3.Body)
	}

	// Current action is reflected in the list.
	var l listResp
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(t, &l)
	if it := l.Items[0]; it.CurrentAction == nil || *it.CurrentAction != "PRICE_REDUCTION_PLANNED" || it.CurrentActionAt == nil {
		t.Fatalf("list current_action = %v", it.CurrentAction)
	}
}

func TestLogActionErrors(t *testing.T) {
	e := newTestEnv(t)
	id := e.oneVehicle(10) // not aging: actions are still allowed
	path := "/v1/vehicles/" + id + "/actions"
	ok := `{"action_type":"NO_ACTION_NEEDED"}`
	cases := []struct {
		name   string
		role   platform.Role
		path   string
		body   string
		key    string
		status int
	}{
		{"non-aging vehicle ok", platform.RoleManager, path, ok, "k-ok", 201},
		{"unknown enum", platform.RoleManager, path, `{"action_type":"SELL_IT"}`, "k1", 400},
		{"missing action_type", platform.RoleManager, path, `{}`, "k2", 400},
		{"unknown field", platform.RoleManager, path, `{"action_type":"PRICE_REDUCED","x":1}`, "k3", 400},
		{"malformed json", platform.RoleManager, path, `{`, "k4", 400},
		{"note too long", platform.RoleManager, path, fmt.Sprintf(`{"action_type":"PRICE_REDUCED","note":%q}`, strings.Repeat("é", 1001)), "k5", 400},
		{"missing key", platform.RoleManager, path, ok, "", 400},
		{"key too long", platform.RoleManager, path, ok, strings.Repeat("k", 129), 400},
		{"bad vehicle id", platform.RoleManager, "/v1/vehicles/nope/actions", ok, "k6", 400},
		{"unknown vehicle", platform.RoleManager, "/v1/vehicles/0195a2b0-0000-7000-8000-000000000000/actions", ok, "k7", 404},
		{"viewer forbidden", platform.RoleViewer, path, ok, "k8", 403},
		{"ingest forbidden", platform.RoleIngest, path, ok, "k9", 403},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var hdr []string
			if c.key != "" {
				hdr = []string{"Idempotency-Key", c.key}
			}
			r := e.do(c.role, "POST", c.path, c.body, hdr...)
			if r.Status != c.status {
				t.Fatalf("status = %d, want %d: %s", r.Status, c.status, r.Body)
			}
		})
	}
	if n := e.countActions(); n != 1 {
		t.Fatalf("%d action rows, want 1", n)
	}

	// A failed request does not burn the key: retrying k7 on a real vehicle works.
	if r := e.do(platform.RoleManager, "POST", path, ok, "Idempotency-Key", "k7"); r.Status != 201 {
		t.Fatalf("retry after 404 = %d %s", r.Status, r.Body)
	}

	// Sold vehicles cannot be acted on.
	sold := veh(1, "Toyota", "Camry", 10)
	sold.Status = domain.StatusSold
	e.seed(sold)
	if r := e.do(platform.RoleManager, "POST", path, ok, "Idempotency-Key", "k-sold"); r.Status != 409 {
		t.Fatalf("sold vehicle = %d %s, want 409", r.Status, r.Body)
	}
}

func TestLogActionConcurrentAndHistory(t *testing.T) {
	e := newTestEnv(t)
	id := e.oneVehicle(100)
	path := "/v1/vehicles/" + id + "/actions"

	const n = 12
	var wg sync.WaitGroup
	statuses := make([]int, n)
	for i := range n {
		wg.Go(func() {
			typ := domain.ActionTypes[i%len(domain.ActionTypes)]
			r := e.do(platform.RoleManager, "POST", path, fmt.Sprintf(`{"action_type":%q}`, typ), "Idempotency-Key", fmt.Sprintf("c-%d", i))
			statuses[i] = r.Status
		})
	}
	// The same key raced from several goroutines yields exactly one row.
	for range 4 {
		wg.Go(func() {
			e.do(platform.RoleManager, "POST", path, `{"action_type":"PROMOTION_PLANNED"}`, "Idempotency-Key", "same")
		})
	}
	wg.Wait()
	for i, s := range statuses {
		if s != 201 {
			t.Fatalf("request %d status %d", i, s)
		}
	}

	var h struct {
		Items []actionResp `json:"items"`
	}
	r := e.do(platform.RoleViewer, "GET", path+"?limit=200", nil)
	if r.Status != 200 {
		t.Fatalf("history = %d %s", r.Status, r.Body)
	}
	r.decode(t, &h)
	if len(h.Items) != n+1 {
		t.Fatalf("history has %d rows, want %d", len(h.Items), n+1)
	}
	for i := 1; i < len(h.Items); i++ {
		// Parse: RFC 3339 strings with trimmed zeros do not sort lexically.
		cur, _ := time.Parse(time.RFC3339Nano, h.Items[i].CreatedAt)
		prev, _ := time.Parse(time.RFC3339Nano, h.Items[i-1].CreatedAt)
		if cur.After(prev) {
			t.Fatalf("history not newest first at %d", i)
		}
	}
	var l listResp
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(t, &l)
	if got := *l.Items[0].CurrentAction; got != h.Items[0].ActionType {
		t.Fatalf("current action %s, latest history %s", got, h.Items[0].ActionType)
	}

	// History errors.
	if r := e.do(platform.RoleViewer, "GET", "/v1/vehicles/0195a2b0-0000-7000-8000-000000000000/actions", nil); r.Status != 404 {
		t.Fatalf("unknown vehicle history = %d", r.Status)
	}
	if r := e.do(platform.RoleViewer, "GET", path+"?limit=0", nil); r.Status != 400 {
		t.Fatalf("bad limit history = %d", r.Status)
	}
	e.do(platform.RoleViewer, "GET", path+"?limit=2", nil).decode(t, &h)
	if len(h.Items) != 2 {
		t.Fatalf("history limit=2 returned %d", len(h.Items))
	}
}
