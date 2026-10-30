package http

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres/pgtest"
	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func TestObservability(t *testing.T) {
	e := newTestEnv(t)
	var logs bytes.Buffer
	metrics := platform.NewMetrics()
	metrics.RegisterPool(pgtest.Pool(t))
	svc := app.New(e.repos.Vehicles, e.repos.Actions, e.repos.Settings, domain.FixedClock(today))
	e.handler = NewRouter(Deps{
		DB: e.pool, App: svc, Auth: platform.NewAuthenticator(testSecret),
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Metrics: metrics,
	})
	id := e.oneVehicle(100)
	const secretNote = "do-not-log-this-note"
	e.do(platform.RoleManager, "POST", "/v1/vehicles/"+id+"/actions", `{"action_type":"PRICE_REDUCED","note":"`+secretNote+`"}`, "Idempotency-Key", "obs-1")
	e.do(platform.RoleIngest, "POST", "/v1/ingest/vehicles", ingestBody(ingestVeh("JTMWRREV0RD000001", "1", "2026-01-01")))
	e.do(platform.RoleViewer, "GET", "/v1/vehicles/"+id+"/actions", nil)

	// Inbound request ids are propagated; otherwise one is generated.
	r := e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil, "X-Request-Id", "req-abc-123")
	if got := r.Header.Get("X-Request-Id"); got != "req-abc-123" {
		t.Fatalf("X-Request-Id = %q", got)
	}
	r = e.do(platform.RoleViewer, "GET", "/v1/vehicles", nil, "X-Request-Id", "bad id with spaces")
	if got := r.Header.Get("X-Request-Id"); got == "" || strings.Contains(got, " ") {
		t.Fatalf("generated X-Request-Id = %q", got)
	}

	// Log lines carry the required fields and never the token or note.
	var found bool
	for line := range strings.SplitSeq(strings.TrimSpace(logs.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("non-JSON log line %q", line)
		}
		if m["request_id"] == "req-abc-123" {
			found = true
			for _, k := range []string{"route", "status", "latency_ms", "subject"} {
				if _, ok := m[k]; !ok {
					t.Errorf("log line missing %s: %s", k, line)
				}
			}
			if m["route"] != "/v1/vehicles" || m["subject"] != "user-viewer" {
				t.Errorf("log line = %s", line)
			}
		}
	}
	if !found {
		t.Fatal("no log line with propagated request id")
	}
	for _, secret := range []string{e.tokens[platform.RoleManager], secretNote} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("logs contain a token or note")
		}
	}

	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	body := w.Body.String()
	for _, want := range []string{
		`http_requests_total{method="GET",route="/v1/vehicles",status="200"}`,
		`http_requests_total{method="POST",route="/v1/vehicles/:id/actions",status="201"}`,
		`http_request_duration_seconds_bucket{route="/v1/vehicles/:id/actions"`,
		`actions_logged_total{action_type="PRICE_REDUCED"} 1`,
		`ingest_rows_total{result="inserted"} 1`,
		`db_pool_max_conns`,
		`db_pool_acquired_conns`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %s", want)
		}
	}
	if strings.Contains(body, id) {
		t.Error("/metrics uses a raw path as a label")
	}
}
