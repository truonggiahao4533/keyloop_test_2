package http

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/haotruong/inventory-dashboard/internal/platform"
)

const specPath = "../../../api/openapi.yaml"

func loadSpec(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("spec is invalid: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return doc, router
}

// TestContract sends real requests through the router (backed by Postgres)
// and validates each request and response against api/openapi.yaml,
// including undocumented status codes.
func TestContract(t *testing.T) {
	_, router := loadSpec(t)
	e := newTestEnv(t)
	seedListFixture(e)
	var l listResp
	e.do(platform.RoleViewer, "GET", "/v1/vehicles?limit=1", nil).decode(t, &l)
	id := l.Items[0].ID
	missing := "0195a2b0-0000-7000-8000-000000000000"
	actionsPath := "/v1/vehicles/" + id + "/actions"

	type call struct {
		role         platform.Role
		method, path string
		body         string
		headers      []string
		want         int
		// invalidRequest marks requests that deliberately violate the spec;
		// only the (error) response is validated.
		invalidRequest bool
	}
	calls := []call{
		{"", "GET", "/healthz", "", nil, 200, false},
		{"", "GET", "/readyz", "", nil, 200, false},
		{"", "GET", "/metrics", "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/vehicles", "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/vehicles?make=Toyota&model=Corolla&min_age_days=0&max_age_days=365&aging=true&limit=1", "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/vehicles?limit=2", "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/vehicles?limit=999", "", nil, 400, true},
		{platform.RoleViewer, "GET", "/v1/vehicles?cursor=bogus", "", nil, 400, false},
		{"", "GET", "/v1/vehicles", "", nil, 401, false},
		{platform.RoleIngest, "GET", "/v1/vehicles", "", nil, 403, false},
		{platform.RoleViewer, "GET", "/v1/vehicles/aging?limit=2", "", nil, 200, false},
		{platform.RoleManager, "POST", actionsPath, `{"action_type":"PRICE_REDUCTION_PLANNED","note":"5% off"}`, []string{"Idempotency-Key", "contract-1"}, 201, false},
		{platform.RoleManager, "POST", actionsPath, `{"action_type":"PRICE_REDUCTION_PLANNED","note":"5% off"}`, []string{"Idempotency-Key", "contract-1"}, 201, false},
		{platform.RoleManager, "POST", actionsPath, `{"action_type":"PRICE_REDUCED"}`, []string{"Idempotency-Key", "contract-1"}, 409, false},
		{platform.RoleManager, "POST", actionsPath, `{"action_type":"NOPE"}`, []string{"Idempotency-Key", "contract-2"}, 400, true},
		{platform.RoleManager, "POST", "/v1/vehicles/" + missing + "/actions", `{"action_type":"PRICE_REDUCED"}`, []string{"Idempotency-Key", "contract-3"}, 404, false},
		{platform.RoleViewer, "POST", actionsPath, `{"action_type":"PRICE_REDUCED"}`, []string{"Idempotency-Key", "contract-4"}, 403, false},
		{platform.RoleViewer, "GET", actionsPath, "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/vehicles/" + missing + "/actions", "", nil, 404, false},
		{platform.RoleViewer, "GET", "/v1/dashboard/summary", "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/settings/aging-threshold", "", nil, 200, false},
		{platform.RoleManager, "PUT", "/v1/settings/aging-threshold", `{"days":75}`, nil, 200, false},
		{platform.RoleManager, "PUT", "/v1/settings/aging-threshold", `{"days":0}`, nil, 400, true},
		{platform.RoleViewer, "PUT", "/v1/settings/aging-threshold", `{"days":75}`, nil, 403, false},
		{platform.RoleIngest, "POST", "/v1/ingest/vehicles", ingestBody(ingestVeh("JTMWRREV0RD000001", "31999.99", "2026-07-01"), ingestVeh("JTMWRREV0RD000002", "null", "2026-07-01")), nil, 200, false},
		{platform.RoleIngest, "POST", "/v1/ingest/vehicles", ingestBody(ingestVeh("JTMWRREV0RD000001", "1", "2099-01-01")), nil, 400, false},
		{platform.RoleManager, "POST", "/v1/ingest/vehicles", ingestBody(ingestVeh("JTMWRREV0RD000001", "1", "2026-01-01")), nil, 403, false},
		// The summary and a vehicle with a current action and a null price.
		{platform.RoleViewer, "GET", "/v1/dashboard/summary", "", nil, 200, false},
		{platform.RoleViewer, "GET", "/v1/vehicles?limit=200", "", nil, 200, false},
	}

	for i, c := range calls {
		t.Run(fmt.Sprintf("%02d %s %s %d", i, c.method, c.path, c.want), func(t *testing.T) {
			var body io.Reader
			if c.body != "" {
				body = bytes.NewBufferString(c.body)
			}
			req := httptest.NewRequest(c.method, "http://localhost:8080"+c.path, body)
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if c.role != "" {
				req.Header.Set("Authorization", "Bearer "+e.tokens[c.role])
			}
			for j := 0; j+1 < len(c.headers); j += 2 {
				req.Header.Set(c.headers[j], c.headers[j+1])
			}
			route, params, err := router.FindRoute(req)
			if err != nil {
				t.Fatalf("route not in spec: %v", err)
			}
			in := &openapi3filter.RequestValidationInput{
				Request: req, PathParams: params, Route: route,
				Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
			}
			if !c.invalidRequest {
				if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
					t.Fatalf("request does not match spec: %v", err)
				}
				if req.Body != nil {
					// ValidateRequest consumed and restored the body; rewind for the handler.
					req.Body = io.NopCloser(bytes.NewBufferString(c.body))
				}
			}

			w := httptest.NewRecorder()
			e.handler.ServeHTTP(w, req)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, c.want, w.Body)
			}
			out := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: in,
				Status:                 w.Code,
				Header:                 w.Header(),
				Body:                   io.NopCloser(bytes.NewReader(w.Body.Bytes())),
				Options:                &openapi3filter.Options{IncludeResponseStatus: true},
			}
			if err := openapi3filter.ValidateResponse(context.Background(), out); err != nil {
				t.Fatalf("response does not match spec: %v\n%s", err, w.Body)
			}
		})
	}
}
