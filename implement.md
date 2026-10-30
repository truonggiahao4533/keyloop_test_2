# Intelligent Inventory Dashboard: Agent Implementation Guide (Single Dealership)

You are implementing the **backend** of the Intelligent Inventory Dashboard (Go + PostgreSQL) for **one specific dealership**. There is no multi-tenancy in the MVP. Follow the steps **in order**. After each step: run the listed verification, make sure it passes, commit with the given message, then continue. Do not add features that are not in this document.

## Ground rules

- **Go 1.27** (`go 1.27` in `go.mod`, `golang:1.27` as the Docker build image), Gin, pgx v5, sqlc, goose, `log/slog`, Prometheus client, OpenTelemetry, testcontainers-go.
- Clean/Hexagonal layering: `domain` has no imports from adapters; `app` depends on `domain` ports only; adapters implement ports.
- **Single dealership:** no `dealership_id` column, claim, parameter, or table anywhere. Do not build tenant scoping.
- **Age is computed, never stored.** `age_days = today - received_at`. No cron job, no stored aging flag.
- The clock is injected (`Clock` interface) so tests can control "today".
- Errors use one JSON shape: `{"error":{"code":"...","message":"...","request_id":"..."}}`.
- Never mock the database in tests. Use testcontainers Postgres with the same image as compose (`postgres:18`).
- Keep the code easy to extend to multi-tenancy later (see Step 13), but do not implement it now.

## Target layout

```
cmd/api/main.go
internal/domain/        vehicle.go, action.go, errors.go, ports.go, clock.go
internal/app/           list_vehicles.go, aging.go, summary.go, log_action.go,
                        settings.go, ingest.go
internal/adapter/http/  router.go, handlers_*.go, middleware_*.go, dto.go
internal/adapter/postgres/  repo.go, tx.go (sqlc generated code in db/gen)
internal/platform/      config.go, logger.go, metrics.go, tracing.go, auth.go
db/migrations/          *.sql (goose)
db/queries/             *.sql (sqlc)
api/openapi.yaml
scripts/                seed/main.go, smoke.sh, k6/list.js
docs/system-design.md, docs/decision-log.md
docker-compose.yml, Dockerfile, Makefile, sqlc.yaml, README.md
```

---

## Step 0: Bootstrap

1. `go mod init`, set `go 1.27` in `go.mod`, create the directory layout above. The `Dockerfile` uses a multi-stage build: `golang:1.27` to build, a minimal runtime image (distroless or alpine) to run. If `go version` or the image tag doesn't resolve to 1.27, stop and tell me rather than falling back to an older version.
2. `Makefile` targets: `run`, `test`, `lint`, `migrate-up`, `sqlc`, `seed`, `seed-load`, `smoke`.
3. `docker-compose.yml`: `postgres:18` (healthcheck) and `api` (depends on healthy db). For the Postgres 18 image, mount the data volume at `/var/lib/postgresql` (not `/var/lib/postgresql/data`), or the container may refuse to start.
4. `internal/platform/config.go`: env config (`PORT`, `DATABASE_URL`, `JWT_SECRET`, `OTLP_ENDPOINT`).
5. `GET /healthz` (always 200) and `GET /readyz` (pings DB, 503 on failure).

**Verify:** `docker compose up -d && curl -s localhost:8080/readyz` returns 200.
**Commit:** `chore: bootstrap service skeleton`

## Step 1: Schema and migrations

Create goose migrations:

```sql
CREATE TYPE action_type AS ENUM (
  'PRICE_REDUCTION_PLANNED','PRICE_REDUCED','TRANSFER_PLANNED',
  'WHOLESALE_PLANNED','PROMOTION_PLANNED','NO_ACTION_NEEDED');

-- Single-row settings table (the dealership's configuration)
CREATE TABLE app_settings (
  id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
  aging_threshold_days INT NOT NULL DEFAULT 90
    CHECK (aging_threshold_days BETWEEN 1 AND 3650)
);
INSERT INTO app_settings DEFAULT VALUES;

CREATE TABLE vehicles (
  id UUID PRIMARY KEY,
  vin TEXT NOT NULL UNIQUE,
  make TEXT NOT NULL,
  model TEXT NOT NULL,
  year INT NOT NULL,
  price NUMERIC(12,2),
  received_at DATE NOT NULL,
  status TEXT NOT NULL DEFAULT 'IN_STOCK' CHECK (status IN ('IN_STOCK','SOLD','REMOVED')),
  current_action_type action_type,
  current_action_at TIMESTAMPTZ
);
CREATE INDEX idx_vehicles_age ON vehicles (received_at, id)  WHERE status = 'IN_STOCK';
CREATE INDEX idx_vehicles_mm  ON vehicles (make, model)      WHERE status = 'IN_STOCK';

CREATE TABLE vehicle_actions (
  id UUID PRIMARY KEY,
  vehicle_id UUID NOT NULL REFERENCES vehicles(id),
  action_type action_type NOT NULL,
  note TEXT CHECK (char_length(note) <= 1000),
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_actions_vehicle ON vehicle_actions (vehicle_id, created_at DESC);

CREATE TABLE idempotency_keys (
  key TEXT PRIMARY KEY,
  request_hash TEXT NOT NULL,
  response_status INT NOT NULL,
  response_body JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Configure `sqlc.yaml` for pgx/v5 and generate into `db/gen`.

**Verify:** `make migrate-up` succeeds on a clean DB; `\d vehicles` shows both partial indexes; `SELECT * FROM app_settings` returns one row with 90.
**Commit:** `feat(db): schema, indexes, sqlc config`

## Step 2: Domain and ports

- `domain.Vehicle`, `domain.Action`, `domain.ActionType` (validated against the enum; unknown value returns `ErrValidation`).
- `domain.AgeDays(receivedAt, today time.Time) int` and `IsAging(age, threshold int) bool` (`age > threshold`; "over 90" means day 91+).
- Ports (`ports.go`):

```go
type VehicleRepo interface {
    List(ctx context.Context, f VehicleFilter, c *Cursor, limit int) ([]VehicleRow, *Cursor, error)
    Summary(ctx context.Context, today time.Time, threshold int) (Summary, error)
    Upsert(ctx context.Context, vs []Vehicle) (UpsertResult, error)
}
type ActionRepo interface {
    Log(ctx context.Context, vehicleID uuid.UUID, a NewAction, idemKey, reqHash string) (Action, replay bool, error)
    History(ctx context.Context, vehicleID uuid.UUID, limit int) ([]Action, error)
}
type SettingsRepo interface {
    Threshold(ctx context.Context) (int, error)
    SetThreshold(ctx context.Context, days int) error
}
type Clock interface{ Today() time.Time }
```

- Domain errors: `ErrNotFound`, `ErrValidation`, `ErrConflict`, `ErrForbidden`.

**Verify:** unit tests for `AgeDays` / `IsAging` boundaries (day 90 is NOT aging, day 91 is) and enum validation.
**Commit:** `feat(domain): entities, rules, ports`

## Step 3: Postgres adapter

Implement the repos with sqlc queries. The core list query (use `sqlc.narg` for optional filters):

```sql
-- name: ListVehicles :many
SELECT v.*
FROM vehicles v
WHERE v.status = 'IN_STOCK'
  AND (sqlc.narg('make')::text  IS NULL OR v.make  = sqlc.narg('make'))
  AND (sqlc.narg('model')::text IS NULL OR v.model = sqlc.narg('model'))
  AND (sqlc.narg('received_before')::date IS NULL OR v.received_at <= sqlc.narg('received_before'))
  AND (sqlc.narg('received_after')::date  IS NULL OR v.received_at >= sqlc.narg('received_after'))
  AND (sqlc.narg('cur_date')::date IS NULL OR (v.received_at, v.id) > (sqlc.narg('cur_date'), sqlc.narg('cur_id')))
ORDER BY v.received_at, v.id
LIMIT @lim;
```

Notes:
- Age is NOT selected from SQL. Compute `age_days` and `is_aging` in the app layer from `received_at`, the injected clock, and the threshold, so tests are deterministic.
- Age filters are converted in the app layer to `received_*` dates (`min_age_days=N` becomes `received_before = today - N`), so the index is used.
- Fetch `limit+1` rows to detect `next_cursor`. Cursor is base64(JSON `{d, id}`).
- `Log` runs in ONE transaction: check the idempotency key, verify the vehicle exists (else `ErrNotFound`), insert into `vehicle_actions`, update `vehicles.current_action_*`, store the idempotency record, commit.
- Use `pgxpool` with a sane max connections, and wrap every call in a context timeout (default 3 s).

**Verify:** integration tests with testcontainers for list filters and pagination (no gaps or duplicates across pages).
**Commit:** `feat(postgres): repositories`

## Step 4: Seed data

- `scripts/seed/main.go` (`make seed`): one dealership's realistic stock of about 500 vehicles, `received_at` spread over 0 to 240 days with roughly 30% older than 90 days. Realistic make/model sets (Toyota, Honda, Ford, ...). Include a few vehicles at exactly 89, 90, 91 days for demos.
- `make seed-load`: generates about 250k rows. **Only for performance testing** in Step 12, to prove the design holds well beyond the MVP size. Never run by default.

**Verify:** `make seed` finishes in a few seconds; about 500 rows in `vehicles`.
**Commit:** `feat: seed scripts`

## Step 5: Auth stub and middleware

- JWT HS256 with claims `{sub, role}`; roles: `viewer`, `manager`, `ingest`.
- Middleware order: `request-id` then `recover` then `logger` then `metrics` then `otel` then `auth`.
- Put `Principal{Subject, Role}` in the context.
- Permissions: `viewer` can read; `manager` can read, log actions, and change the threshold; `ingest` can only call ingest.
- `make token ROLE=manager` mints a demo token.

**Verify:** missing or invalid token returns 401; role violations return 403; tests cover each.
**Commit:** `feat(auth): JWT claims middleware and RBAC`

## Step 6: Vehicle list endpoint

`GET /v1/vehicles?make=&model=&min_age_days=&max_age_days=&aging=true&cursor=&limit=`

- `limit` default 50, max 200. Validate and reject bad values with 400.
- `aging=true` uses the configured threshold: `received_before = today - threshold` (so exactly 90 days is not aging).
- Response items: `id, vin, make, model, year, price, received_at, age_days, is_aging, current_action, current_action_at`, plus `next_cursor`.
- Fetch the threshold once per request.

**Verify:** integration tests for each filter and combinations; boundary vehicles at exactly 90 and 91 days.
**Commit:** `feat(api): filterable vehicle list`

## Step 7: Aging endpoint and summary

- `GET /v1/vehicles/aging`: same as list with `aging=true`, ordered oldest first, same pagination.
- `GET /v1/dashboard/summary`:

```json
{ "total_in_stock": 512, "aging_count": 143, "aging_pct": 27.9,
  "threshold_days": 90,
  "aging_by_make": [{"make":"Toyota","count":31}],
  "aging_by_action": [{"action":"PRICE_REDUCTION_PLANNED","count":20},{"action":null,"count":88}] }
```

(`action: null` means aging with no action logged yet, which is the useful number for managers.)

**Verify:** summary numbers match a brute-force calculation in the test.
**Commit:** `feat(api): aging stock and dashboard summary`

## Step 8: Log action and history

- `POST /v1/vehicles/{id}/actions` (manager only), body `{action_type, note?}`, header `Idempotency-Key` (required, 1 to 128 chars).
- Idempotency: same key and same body hash returns the stored response with the original status (add header `Idempotency-Replayed: true`); same key and different body returns 409.
- Allow actions on any in-stock vehicle, not only aging ones (managers may act early). Document this choice.
- `GET /v1/vehicles/{id}/actions`: history, newest first.

**Verify:** tests for: success (201), replay (same response, one DB row), key reuse with a different body (409), unknown enum (400), unknown vehicle (404), concurrent logs (race test with `-race`, history has both rows, current action equals the latest).
**Commit:** `feat(api): action logging with idempotency`

## Step 9: Settings and ingest

- `GET /v1/settings/aging-threshold` (any role except ingest) and `PUT` (manager) with body `{days}` (1 to 3650).
- `POST /v1/ingest/vehicles` (role `ingest`). Body `{vehicles:[...]}`, max 1000 per batch, upsert on `vin`, response `{inserted, updated}`. Ingest must NOT touch `current_action_*`.

**Verify:** re-sending the same batch yields `inserted: 0`; an ingest token cannot call manager endpoints and vice versa.
**Commit:** `feat(api): threshold settings and ingest`

## Step 10: Observability

- **Logs:** JSON `slog`, fields `request_id, route, status, latency_ms, subject`. Call `slog.SetDefault` once in `main.go`. Never log tokens or note contents.
- **Metrics (`/metrics`):** `http_requests_total{route,method,status}`, `http_request_duration_seconds{route}`, `db_pool_*`, `actions_logged_total{action_type}`, `ingest_rows_total{result}`. Use the route template, not the raw path, as the label.
- **Tracing:** OTel HTTP middleware plus pgx tracer; export OTLP (optional in compose via an `otel-collector` or Jaeger service).
- Graceful shutdown (drain in-flight requests, close the pool).

**Verify:** `curl /metrics` shows the series after a few calls; logs show `request_id` propagated into the response header `X-Request-Id`.
**Commit:** `feat(obs): logging, metrics, tracing`

## Step 11: OpenAPI and cURL examples

- `api/openapi.yaml` covering all endpoints, schemas, error shape, auth scheme, and the idempotency header.
- Add a contract test that validates real responses against the spec (kin-openapi).
- `README.md` with a quick start, token minting, and a cURL example for each endpoint. Also `scripts/smoke.sh` running the full flow: list, aging, log action, history, summary.

**Verify:** `make smoke` exits 0 against `docker compose up`.
**Commit:** `docs: openapi spec, curl examples, smoke test`

## Step 12: Performance and hardening

- Run `make seed-load`, then `scripts/k6/list.js` (list and aging endpoints, 50 VUs, 1 minute). **Target:** p95 under 100 ms at 250k rows (the MVP itself is about 500, so this proves headroom).
- Run `EXPLAIN (ANALYZE, BUFFERS)` on the list and aging queries and confirm they use `idx_vehicles_age` (no seq scan at 250k rows). Paste the output into the design doc.
- Request body size limit (1 MB), per-request timeout (5 s), basic per-IP rate limiting.
- `go vet`, `golangci-lint`, `go test -race ./...` all clean.

**Commit:** `perf: load test, explain plans, hardening`

## Step 13: System Design Document

Write `docs/system-design.md` with these sections: architecture diagram (Mermaid), component roles, data flow (list, log action, ingest), technology choices with justifications, data model, aging design (query-time derivation vs cron, and why), observability strategy, scaling path (read replicas, SSE with `LISTEN/NOTIFY`, optional daily "became aging" event job), security, trade-offs, and the **GenAI usage section** (what AI drafted, what was changed or rejected, how outputs were verified: tests, EXPLAIN plans, manual review).

Copy `decision-log.md` into `docs/decision-log.md` and link it from the GenAI usage section as the record of what the owner changed and why. Do not edit or reword the owner's reasons. If the owner asks for a further change during implementation, append a row with their reason.

Include a **"Future: multi-dealership"** section describing the migration path, without implementing it:
1. Add a `dealerships` table and a `dealership_id` column to `vehicles`, `vehicle_actions`, and `idempotency_keys`; backfill with the existing dealership's ID.
2. Move `aging_threshold_days` from `app_settings` to `dealerships`.
3. Prefix the indexes with `dealership_id`, and make `vin` unique per dealership.
4. Add a `dealership_id` claim to the JWT and pass it to every repository method from the auth context, never from the request.
5. Add tenant isolation tests (cross-tenant access returns 404).

**Commit:** `docs: system design document`

---

## Definition of done

- [ ] `docker compose up` gives a working API with seeded data
- [ ] All three core requirements demonstrable via cURL: filterable list, aging stock, logged action persisting across restart
- [ ] Role-based authorization tests pass
- [ ] `go test -race ./...` is green
- [ ] OpenAPI spec validates and contract test passes
- [ ] p95 target met at 250k rows and EXPLAIN plans documented
- [ ] Design doc complete, including the GenAI section and the multi-dealership migration path