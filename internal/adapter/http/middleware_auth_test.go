package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"github.com/haotruong/inventory-dashboard/internal/platform"
)

const testSecret = "test-secret-0123456789"

type okPinger struct{}

func (okPinger) Ping(context.Context) error { return nil }

func mint(t *testing.T, role platform.Role) string {
	t.Helper()
	tok, err := platform.NewAuthenticator(testSecret).Mint("user-"+string(role), role, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestAuthAndRBAC(t *testing.T) {
	r, v1 := newEngine(Deps{DB: okPinger{}, Auth: platform.NewAuthenticator(testSecret)})
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"subject": principal(c).Subject}) }
	v1.GET("/read", require(platform.PermRead), ok)
	v1.POST("/act", require(platform.PermLogAction), ok)
	v1.PUT("/threshold", require(platform.PermSetThreshold), ok)
	v1.POST("/ingest", require(platform.PermIngest), ok)

	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "x", "role": "manager", "exp": time.Now().Add(-time.Hour).Unix(),
	}).SignedString([]byte(testSecret))
	noExp, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "x", "role": "manager"}).SignedString([]byte(testSecret))
	badRole, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "x", "role": "admin", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testSecret))
	otherKey, _ := platform.NewAuthenticator("another-secret-0123456789").Mint("x", platform.RoleManager, time.Hour)
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "x", "role": "manager", "exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)

	viewer, manager, ingest := mint(t, platform.RoleViewer), mint(t, platform.RoleManager), mint(t, platform.RoleIngest)
	cases := []struct {
		name, method, path, auth string
		want                     int
	}{
		{"missing token", "GET", "/v1/read", "", 401},
		{"not bearer", "GET", "/v1/read", "Basic abc", 401},
		{"garbage token", "GET", "/v1/read", "Bearer abc.def.ghi", 401},
		{"expired", "GET", "/v1/read", "Bearer " + expired, 401},
		{"no exp", "GET", "/v1/read", "Bearer " + noExp, 401},
		{"unknown role", "GET", "/v1/read", "Bearer " + badRole, 401},
		{"wrong key", "GET", "/v1/read", "Bearer " + otherKey, 401},
		{"alg none", "GET", "/v1/read", "Bearer " + none, 401},

		{"viewer read", "GET", "/v1/read", "Bearer " + viewer, 200},
		{"viewer act", "POST", "/v1/act", "Bearer " + viewer, 403},
		{"viewer threshold", "PUT", "/v1/threshold", "Bearer " + viewer, 403},
		{"viewer ingest", "POST", "/v1/ingest", "Bearer " + viewer, 403},

		{"manager read", "GET", "/v1/read", "Bearer " + manager, 200},
		{"manager act", "POST", "/v1/act", "Bearer " + manager, 200},
		{"manager threshold", "PUT", "/v1/threshold", "Bearer " + manager, 200},
		{"manager ingest", "POST", "/v1/ingest", "Bearer " + manager, 403},

		{"ingest read", "GET", "/v1/read", "Bearer " + ingest, 403},
		{"ingest act", "POST", "/v1/act", "Bearer " + ingest, 403},
		{"ingest threshold", "PUT", "/v1/threshold", "Bearer " + ingest, 403},
		{"ingest ingest", "POST", "/v1/ingest", "Bearer " + ingest, 200},

		{"healthz needs no token", "GET", "/healthz", "", 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(c.method, c.path, nil)
			if c.auth != "" {
				req.Header.Set("Authorization", c.auth)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", w.Code, c.want, w.Body)
			}
			if w.Code >= 400 {
				var e errorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Error.Code == "" || e.Error.RequestID == "" {
					t.Fatalf("bad error body %s", w.Body)
				}
				if e.Error.RequestID != w.Header().Get("X-Request-Id") {
					t.Fatalf("request_id %q != header %q", e.Error.RequestID, w.Header().Get("X-Request-Id"))
				}
			}
		})
	}
}
