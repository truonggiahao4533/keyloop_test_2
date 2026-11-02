# Intelligent Inventory Dashboard: Backend

Go + PostgreSQL API for **one dealership's** vehicle inventory. It does three things:

1. **Filterable inventory list:** filter by make, model and age, with keyset pagination.
2. **Aging stock:** a vehicle is *aging* when `age_days > threshold`. The threshold defaults to 90 and is configurable, so day 91 is aging and day 90 is not. Age is computed at query time from `received_at` and is never stored.
3. **Actions:** managers log what they plan to do with a vehicle. Logging is idempotent, keeps a history, and sets the vehicle's current action.

See [docs/system-design.md](docs/system-design.md) for the architecture and the reasoning behind it, and [api/openapi.yaml](api/openapi.yaml) for the API contract.

## Quick start

Requirements: Docker with Compose v2. To develop without Docker you also need Go 1.27 and `jq` (for `make smoke`).

```bash
docker compose up -d --build   # postgres:18 → migrate → seed (first start only) → api
curl -s localhost:8080/readyz  # {"status":"ok"}
make smoke                     # end-to-end check of every endpoint
```

On first start, compose migrates the database and seeds about 500 vehicles. That stock spreads over 0 to 240 days, with about 30% older than 90 days. Three demo VINs sit exactly on the boundary: `BNDRY000000000089`, `…090` and `…091`. Later starts skip seeding, so logged actions survive `docker compose restart` and `down`/`up`. Run `docker compose down -v` to start from scratch.

Postgres is published on host port **5433** (set `DB_PORT` to change it) and the API on **8080** (`API_PORT`).

## Tokens

Every `/v1` endpoint needs an HS256 JWT with claims `{sub, role}`. Mint a demo token (valid for 24 h) with:

```bash
export TOKEN=$(docker compose exec -T api /api token -role manager -sub alice)
# or, with Go installed:  make token ROLE=manager SUB=alice
```

| Operation | `viewer` | `manager` | `ingest` |
|---|:-:|:-:|:-:|
| List vehicles, aging stock, action history, summary, read threshold | ✓ | ✓ | |
| Log action, change threshold | | ✓ | |
| Ingest vehicles | | | ✓ |

A missing, invalid or expired token returns `401`. A role that is not allowed returns `403`.

## Swagger UI

Open **http://localhost:8080/docs**. The raw spec is served at `/openapi.yaml`. To try the endpoints, click **Authorize** and paste a token from the next section. Paste the token alone, without `Bearer `.

## API by example

Every error has the same shape, and every response carries an `X-Request-Id` header:

```json
{"error":{"code":"VALIDATION_ERROR","message":"limit must be between 1 and 200","request_id":"7c0e…"}}
```

```bash
H="Authorization: Bearer $TOKEN"

# Filterable list (all filters optional; oldest first; limit 1–200, default 50)
curl -s -H "$H" 'localhost:8080/v1/vehicles?make=Toyota&model=Camry&min_age_days=30&max_age_days=120&limit=20' | jq

# Next page: pass next_cursor back as cursor
curl -s -H "$H" "localhost:8080/v1/vehicles?limit=20&cursor=<next_cursor>" | jq

# Aging stock (same as /v1/vehicles?aging=true)
curl -s -H "$H" 'localhost:8080/v1/vehicles/aging?limit=10' | jq

# Dashboard summary
curl -s -H "$H" localhost:8080/v1/dashboard/summary | jq

# Log an action (manager). Idempotency-Key is required (1–128 chars).
ID=$(curl -s -H "$H" 'localhost:8080/v1/vehicles/aging?limit=1' | jq -r '.items[0].id')
curl -s -H "$H" -H 'Idempotency-Key: 6f1c2a' -H 'Content-Type: application/json' \
  -d '{"action_type":"PRICE_REDUCTION_PLANNED","note":"Drop 5% this weekend"}' \
  localhost:8080/v1/vehicles/$ID/actions | jq
#   same key + same body  → same 201 response, header Idempotency-Replayed: true
#   same key + other body → 409

# Action history (newest first)
curl -s -H "$H" localhost:8080/v1/vehicles/$ID/actions | jq

# Aging threshold: read (viewer/manager), change (manager, 1–3650)
curl -s -H "$H" localhost:8080/v1/settings/aging-threshold
curl -s -X PUT -H "$H" -H 'Content-Type: application/json' -d '{"days":60}' localhost:8080/v1/settings/aging-threshold

# Ingest (ingest role): upsert by VIN, max 1000 per batch
ING=$(docker compose exec -T api /api token -role ingest -sub dms-feed)
curl -s -H "Authorization: Bearer $ING" -H 'Content-Type: application/json' -d '{
  "vehicles":[{"vin":"JTMWRREV0RD000001","make":"Toyota","model":"RAV4","year":2025,
               "price":31999.00,"received_at":"2026-07-01"}]}' \
  localhost:8080/v1/ingest/vehicles
#   {"inserted":1,"updated":0}; re-sending the same batch gives {"inserted":0,"updated":0}

# Operations (no token)
curl -s localhost:8080/healthz
curl -s localhost:8080/readyz
curl -s localhost:8080/metrics | grep http_requests_total
```

Action types: `PRICE_REDUCTION_PLANNED`, `PRICE_REDUCED`, `TRANSFER_PLANNED`, `WHOLESALE_PLANNED`, `PROMOTION_PLANNED`, `NO_ACTION_NEEDED`. Actions are allowed on any in-stock vehicle, not only aging ones, so managers can act early.

## Make targets

| Target | What it does |
|---|---|
| `make run` | Run the API locally (needs `DATABASE_URL`; the default points at the compose DB on :5433) |
| `make test` | `go test -race ./...`; integration tests start `postgres:18` with testcontainers (needs Docker) |
| `make lint` | `go vet` + `golangci-lint` |
| `make migrate-up` | Apply goose migrations (embedded in the binary) |
| `make sqlc` | Regenerate `db/gen` from `db/queries` |
| `make seed` | Reset and seed about 500 vehicles |
| `make seed-load` | Reset and seed 250k vehicles. **For performance testing only.** |
| `make smoke` | Run `scripts/smoke.sh` against `API_URL` (default `http://localhost:8080`) |
| `make token ROLE=…` | Mint a demo JWT |

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP port |
| `DATABASE_URL` | required | Postgres DSN |
| `JWT_SECRET` | required (16+ chars) | HS256 signing secret |
| `DEALERSHIP_TIMEZONE` | `UTC` | IANA time zone that decides "today", e.g. `Asia/Ho_Chi_Minh`. Send `received_at` as a date in this zone |
| `OTLP_ENDPOINT` | empty (no export) | OTLP/gRPC trace endpoint, e.g. `jaeger:4317` |
| `DB_MAX_CONNS` | `20` | pgx pool size |

## Monitoring

Logs, metrics and traces can be viewed in Grafana. The stack is optional, so a plain `docker compose up` does not start it.

```bash
OTLP_ENDPOINT=jaeger:4317 docker compose --profile observability up -d
```

Open **http://localhost:3000** (no login needed). The **Inventory API** dashboard opens first.

| Service | What it does | Address |
|---|---|---|
| Grafana | Dashboard and log search | http://localhost:3000 |
| Prometheus | Scrapes `/metrics` every 15 s and keeps 7 days | http://localhost:9090 |
| Loki | Stores logs for 7 days | Used through Grafana |
| Alloy | Sends the `api`, `migrate` and `seed` container logs to Loki | Not published |
| Jaeger | Stores traces, while `OTLP_ENDPOINT` is set as above | http://localhost:16686 |

To follow one request, put its `X-Request-Id` in the dashboard's **Search logs** box. The box also matches anything else in the log line, such as a route, a status or a subject. For free-form queries, use Grafana's **Explore** page:

```
{service="api", level="ERROR"}
{service="api"} | json | status >= 500
{service="api"} | json | latency_ms > 100
```

Good to know:

- **Stopping:** use `docker compose --profile observability down`. Without the profile flag, the monitoring containers keep running. Adding `-v` also deletes the database volume.
- **Local use only:** Grafana allows anonymous editing, so Grafana and Prometheus only listen on localhost. Change `GRAFANA_PORT` or `PROMETHEUS_PORT` if those ports are taken.
- **Left out on purpose:** Postgres logs are not collected, because their error details can include row values such as action notes. `/metrics` scrape lines are dropped too, to keep the logs readable.
- **Disk space:** Loki stops accepting logs when the disk is 98% used. If logs stop arriving, check `docker compose logs loki` for `disk usage exceeded threshold`.

## Layout

```
cmd/api/                  entrypoint: serve | migrate | token
internal/domain/          entities, rules (AgeDays, IsAging), ports; no adapter imports
internal/app/             use cases: list, aging, summary, log action, settings, ingest
internal/adapter/http/    Gin router, handlers, middleware, DTOs (+ integration & contract tests)
internal/adapter/postgres/ repositories on sqlc/pgx, transactions, migrations
internal/platform/        config, logging, metrics, tracing, JWT auth
db/migrations, db/queries goose migrations, sqlc queries (generated code in db/gen)
api/openapi.yaml          API contract
scripts/                  seed, smoke test, k6 load test
docs/                     system design, decision log
observability/            Prometheus, Loki, Alloy and Grafana config, and the dashboard
```
