package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres"
	"github.com/haotruong/inventory-dashboard/internal/adapter/postgres/pgtest"
	"github.com/haotruong/inventory-dashboard/internal/app"
	"github.com/haotruong/inventory-dashboard/internal/domain"
	"github.com/haotruong/inventory-dashboard/internal/platform"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

// today is the fixed "today" for every HTTP integration test.
var today = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)

type testEnv struct {
	t       *testing.T
	handler http.Handler
	repos   postgres.Repos
	tokens  map[platform.Role]string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	pool := pgtest.Pool(t)
	repos := postgres.NewRepos(pool, 0)
	svc := app.New(repos.Vehicles, repos.Actions, repos.Settings, domain.FixedClock(today))
	h := NewRouter(Deps{DB: pool, App: svc, Auth: platform.NewAuthenticator(testSecret)})
	env := &testEnv{t: t, handler: h, repos: repos, tokens: map[platform.Role]string{}}
	for _, r := range []platform.Role{platform.RoleViewer, platform.RoleManager, platform.RoleIngest} {
		env.tokens[r] = mint(t, r)
	}
	return env
}

type response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
}

func (e *testEnv) do(role platform.Role, method, path string, body any, headers ...string) response {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = bytes.NewBufferString(b)
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+e.tokens[role])
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.handler.ServeHTTP(w, req)
	return response{Status: w.Code, Header: w.Header(), Body: w.Body.Bytes()}
}

// seed inserts vehicles; ages are relative to today.
func (e *testEnv) seed(vs ...domain.Vehicle) {
	e.t.Helper()
	if _, err := e.repos.Vehicles.Upsert(context.Background(), vs); err != nil {
		e.t.Fatal(err)
	}
}

func veh(i int, make_, model string, age int) domain.Vehicle {
	price := int64(2000000 + i*100 + 99)
	return domain.Vehicle{
		VIN: fmt.Sprintf("HTTPVIN%010d", i), Make: make_, Model: model, Year: 2024,
		PriceCents: &price, ReceivedAt: today.AddDate(0, 0, -age), Status: domain.StatusInStock,
	}
}

type listResp struct {
	Items []struct {
		ID              string   `json:"id"`
		VIN             string   `json:"vin"`
		Make            string   `json:"make"`
		Model           string   `json:"model"`
		Price           *float64 `json:"price"`
		ReceivedAt      string   `json:"received_at"`
		AgeDays         int      `json:"age_days"`
		IsAging         bool     `json:"is_aging"`
		CurrentAction   *string  `json:"current_action"`
		CurrentActionAt *string  `json:"current_action_at"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func (l listResp) ages() []int {
	out := make([]int, len(l.Items))
	for i, it := range l.Items {
		out[i] = it.AgeDays
	}
	return out
}

// listAll follows next_cursor until the last page and returns all ages.
func (e *testEnv) listAll(path string) []int {
	e.t.Helper()
	var ages []int
	url := path
	for range 100 {
		r := e.do(platform.RoleViewer, "GET", url, nil)
		if r.Status != 200 {
			e.t.Fatalf("GET %s = %d %s", url, r.Status, r.Body)
		}
		var l listResp
		r.decode(e.t, &l)
		ages = append(ages, l.ages()...)
		if l.NextCursor == nil {
			return ages
		}
		sep := "?"
		if bytes.ContainsRune([]byte(path), '?') {
			sep = "&"
		}
		url = path + sep + "cursor=" + *l.NextCursor
	}
	e.t.Fatal("pagination did not terminate")
	return nil
}
