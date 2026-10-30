package http

import (
	"fmt"
	"strings"
	"testing"

	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func TestThresholdSettings(t *testing.T) {
	e := newTestEnv(t)
	var th struct {
		Days int `json:"days"`
	}
	e.do(platform.RoleViewer, "GET", "/v1/settings/aging-threshold", nil).decode(t, &th)
	if th.Days != 90 {
		t.Fatalf("default days = %d", th.Days)
	}
	if r := e.do(platform.RoleManager, "PUT", "/v1/settings/aging-threshold", `{"days":60}`); r.Status != 200 {
		t.Fatalf("put = %d %s", r.Status, r.Body)
	}
	e.do(platform.RoleManager, "GET", "/v1/settings/aging-threshold", nil).decode(t, &th)
	if th.Days != 60 {
		t.Fatalf("days after put = %d", th.Days)
	}
	for _, body := range []string{`{"days":0}`, `{"days":3651}`, `{}`, `{"days":"60"}`, `{"days":60.5}`, `not json`} {
		if r := e.do(platform.RoleManager, "PUT", "/v1/settings/aging-threshold", body); r.Status != 400 {
			t.Errorf("put %s = %d, want 400", body, r.Status)
		}
	}
	for _, d := range []int{1, 3650} {
		if r := e.do(platform.RoleManager, "PUT", "/v1/settings/aging-threshold", fmt.Sprintf(`{"days":%d}`, d)); r.Status != 200 {
			t.Errorf("put days=%d = %d", d, r.Status)
		}
	}
}

func ingestBody(vs ...string) string {
	return `{"vehicles":[` + strings.Join(vs, ",") + `]}`
}

func ingestVeh(vin string, price string, received string) string {
	return fmt.Sprintf(`{"vin":%q,"make":"Toyota","model":"RAV4","year":2025,"price":%s,"received_at":%q}`, vin, price, received)
}

type ingestResp struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
}

func TestIngest(t *testing.T) {
	e := newTestEnv(t)
	batch := ingestBody(
		ingestVeh("JTMWRREV0RD000001", "31999.00", "2026-07-01"),
		ingestVeh("JTMWRREV0RD000002", "null", "2026-10-30"),
	)
	var res ingestResp
	r := e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", batch)
	if r.Status != 200 {
		t.Fatalf("ingest = %d %s", r.Status, r.Body)
	}
	r.decode(t, &res)
	if res != (ingestResp{Inserted: 2}) {
		t.Fatalf("first ingest = %+v", res)
	}
	e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", batch).decode(t, &res)
	if res != (ingestResp{}) {
		t.Fatalf("re-sent batch = %+v, want inserted 0", res)
	}

	// Log an action, then re-ingest with a price change: current action stays.
	var l listResp
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(t, &l)
	if r := e.do(platform.RoleManager, "POST", "/v1/vehicles/"+l.Items[0].ID+"/actions", `{"action_type":"PRICE_REDUCED"}`, "Idempotency-Key", "i1"); r.Status != 201 {
		t.Fatalf("log action = %d", r.Status)
	}
	e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", ingestBody(
		ingestVeh("jtmwrrev0rd000001", "29999.00", "2026-07-01"), // lower-case VIN is normalized
		ingestVeh("JTMWRREV0RD000003", "25000", "2026-10-01"),
	)).decode(t, &res)
	if res != (ingestResp{Inserted: 1, Updated: 1}) {
		t.Fatalf("changed batch = %+v", res)
	}
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(t, &l)
	first := l.Items[0]
	if first.VIN != "JTMWRREV0RD000001" || *first.Price != 29999 || first.CurrentAction == nil || *first.CurrentAction != "PRICE_REDUCED" {
		t.Fatalf("after re-ingest: %+v", first)
	}

	// Marking a vehicle SOLD removes it from the in-stock list.
	sold := `{"vin":"JTMWRREV0RD000002","make":"Toyota","model":"RAV4","year":2025,"received_at":"2026-10-30","status":"SOLD"}`
	e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", ingestBody(sold)).decode(t, &res)
	e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil).decode(t, &l)
	if res.Updated != 1 || len(l.Items) != 2 {
		t.Fatalf("sold: %+v, %d in stock", res, len(l.Items))
	}
}

func TestIngestValidation(t *testing.T) {
	e := newTestEnv(t)
	ok := ingestVeh("JTMWRREV0RD000001", "1.00", "2026-01-01")
	big := make([]string, 1001)
	for i := range big {
		big[i] = ingestVeh(fmt.Sprintf("JTMWRREV0RD%06d", i), "1", "2026-01-01")
	}
	cases := map[string]string{
		"empty":         ingestBody(),
		"missing list":  `{}`,
		"too many":      ingestBody(big...),
		"duplicate vin": ingestBody(ok, ok),
		"short vin":     ingestBody(ingestVeh("JTMWRREV0RD00001", "1", "2026-01-01")),
		"vin with O":    ingestBody(ingestVeh("JTMWRREV0RD00000O", "1", "2026-01-01")),
		"future date":   ingestBody(ingestVeh("JTMWRREV0RD000001", "1", "2026-10-31")),
		"bad date":      ingestBody(ingestVeh("JTMWRREV0RD000001", "1", "01/02/2026")),
		"neg price":     ingestBody(ingestVeh("JTMWRREV0RD000001", "-1", "2026-01-01")),
		"3 decimals":    ingestBody(ingestVeh("JTMWRREV0RD000001", "1.001", "2026-01-01")),
		"bad status":    ingestBody(`{"vin":"JTMWRREV0RD000001","make":"A","model":"B","year":2025,"received_at":"2026-01-01","status":"LOST"}`),
		"no make":       ingestBody(`{"vin":"JTMWRREV0RD000001","make":"","model":"B","year":2025,"received_at":"2026-01-01"}`),
		"bad year":      ingestBody(`{"vin":"JTMWRREV0RD000001","make":"A","model":"B","year":1800,"received_at":"2026-01-01"}`),
		"unknown field": ingestBody(`{"vin":"JTMWRREV0RD000001","make":"A","model":"B","year":2025,"received_at":"2026-01-01","color":"red"}`),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if r := e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", body); r.Status != 400 {
				t.Fatalf("status = %d, want 400: %s", r.Status, r.Body)
			}
		})
	}
	if r := e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", ingestBody(big[:1000]...)); r.Status != 200 {
		t.Fatalf("1000-vehicle batch = %d %s", r.Status, r.Body)
	}
}

// TestRBACMatrix checks every endpoint against every role.
func TestRBACMatrix(t *testing.T) {
	e := newTestEnv(t)
	id := e.oneVehicle(100)
	routes := []struct {
		method, path, body string
		allowed            map[platform.Role]bool
	}{
		{"GET", "/v1/vehicles", "", map[platform.Role]bool{"viewer": true, "manager": true}},
		{"GET", "/v1/vehicles/aging", "", map[platform.Role]bool{"viewer": true, "manager": true}},
		{"GET", "/v1/dashboard/summary", "", map[platform.Role]bool{"viewer": true, "manager": true}},
		{"GET", "/v1/vehicles/" + id + "/actions", "", map[platform.Role]bool{"viewer": true, "manager": true}},
		{"POST", "/v1/vehicles/" + id + "/actions", `{"action_type":"PRICE_REDUCED"}`, map[platform.Role]bool{"manager": true}},
		{"GET", "/v1/settings/aging-threshold", "", map[platform.Role]bool{"viewer": true, "manager": true}},
		{"PUT", "/v1/settings/aging-threshold", `{"days":90}`, map[platform.Role]bool{"manager": true}},
		{"POST", "/v1/ingest/vehicles", ingestBody(ingestVeh("JTMWRREV0RD000009", "1", "2026-01-01")), map[platform.Role]bool{"ingest": true}},
	}
	for _, rt := range routes {
		for _, role := range []platform.Role{"", platform.RoleViewer, platform.RoleManager, platform.RoleIngest} {
			t.Run(fmt.Sprintf("%s %s as %q", rt.method, rt.path, role), func(t *testing.T) {
				var body any
				if rt.body != "" {
					body = rt.body
				}
				r := e.do(role, rt.method, rt.path, body, "Idempotency-Key", "rbac-"+string(role))
				switch {
				case role == "":
					if r.Status != 401 {
						t.Fatalf("status %d, want 401", r.Status)
					}
				case rt.allowed[role]:
					if r.Status >= 400 {
						t.Fatalf("status %d, want success: %s", r.Status, r.Body)
					}
				default:
					if r.Status != 403 {
						t.Fatalf("status %d, want 403", r.Status)
					}
				}
			})
		}
	}
}
