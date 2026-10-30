# System Design: Intelligent Inventory Dashboard

## Performance and EXPLAIN plans

Measured on 2026-10-30 against `docker compose` on one Linux workstation (22 cores, 15 GB RAM). The setup was one API container with a pool of 20 connections, `postgres:18` with default settings, and k6 running on the same host. The data came from `make seed-load`: 250,000 vehicles (242,577 in stock, 73,001 aging at threshold 90) and 60,359 actions.

### k6: `scripts/k6/list.js` (50 VUs, 1 minute)

The mix is the list with no filter, make + model, a 30-day age window, aging, aging + make, and aging paged 3 cursors deep.

| Metric | Result | Target |
|---|---|---|
| p95 `http_req_duration` | **25.0 ms** | < 100 ms ✓ |
| median / p90 / max | 11.8 ms / 21.3 ms / 119.7 ms | |
| Throughput | 3,740 req/s (224,522 requests) | |
| Failed requests / failed checks | 0.00% / 0.00% | < 1% ✓ |

### EXPLAIN (ANALYZE, BUFFERS) at 250k rows

These plans come from the exact `ListVehicles` sqlc query, prepared with parameters the way pgx sends it, under the pool's `plan_cache_mode = force_custom_plan`. Every list and aging variant uses `idx_vehicles_age`, and none does a sequential scan. Keyset pagination keeps a deep page (case 3) as cheap as the first page.

**1. List, no filters (first page, limit 50)**

```
 Limit  (cost=0.42..5.97 rows=51 width=82) (actual time=0.050..0.366 rows=51.00 loops=1)
   Buffers: shared hit=52
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..26390.81 rows=242600 width=82) (actual time=0.049..0.359 rows=51.00 loops=1)
         Index Searches: 1
         Buffers: shared hit=52
 Planning:
   Buffers: shared hit=208
 Planning Time: 0.848 ms
 Execution Time: 0.400 ms
```

**2. Aging (threshold 90 => received_at <= today-91), first page**

```
 Limit  (cost=0.42..13.08 rows=51 width=82) (actual time=0.021..0.047 rows=51.00 loops=1)
   Buffers: shared hit=52
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..18349.01 rows=73905 width=82) (actual time=0.021..0.042 rows=51.00 loops=1)
         Index Cond: (received_at <= '2026-07-31'::date)
         Index Searches: 1
         Buffers: shared hit=52
 Planning:
   Buffers: shared hit=12
 Planning Time: 0.195 ms
 Execution Time: 0.061 ms
```

**3. Aging, deep page (cursor after 5000 aging rows)**

```
 Limit  (cost=0.42..26.17 rows=101 width=82) (actual time=0.042..0.685 rows=101.00 loops=1)
   Buffers: shared hit=96
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..18450.79 rows=72356 width=82) (actual time=0.041..0.672 rows=101.00 loops=1)
         Index Cond: ((received_at <= '2026-07-31'::date) AND (ROW(received_at, id) > ROW('2026-03-14'::date, '01a18bc6-8ab8-7658-aaea-1f3bdeeea660'::uuid)))
         Index Searches: 1
         Buffers: shared hit=96
 Planning Time: 0.161 ms
 Execution Time: 0.726 ms
```

**4. Age range 120-150 days**

```
 Limit  (cost=0.42..53.82 rows=51 width=82) (actual time=0.049..0.160 rows=51.00 loops=1)
   Buffers: shared hit=49
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..15348.78 rows=14658 width=82) (actual time=0.048..0.154 rows=51.00 loops=1)
         Index Cond: ((received_at <= '2026-07-02'::date) AND (received_at >= '2026-06-02'::date))
         Index Searches: 1
         Buffers: shared hit=49
 Planning:
   Buffers: shared hit=3
 Planning Time: 0.149 ms
 Execution Time: 0.174 ms
```

**5. Aging + make**

```
 Limit  (cost=0.42..103.81 rows=51 width=82) (actual time=0.013..2.252 rows=51.00 loops=1)
   Buffers: shared hit=441
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..18533.77 rows=9142 width=82) (actual time=0.013..2.243 rows=51.00 loops=1)
         Index Cond: (received_at <= '2026-07-31'::date)
         Filter: (make = 'Toyota'::text)
         Rows Removed by Filter: 408
         Index Searches: 1
         Buffers: shared hit=441
 Planning Time: 0.128 ms
 Execution Time: 2.268 ms
```

**6. make + model (no age filter)**

```
 Limit  (cost=0.42..1530.61 rows=51 width=82) (actual time=0.031..3.308 rows=51.00 loops=1)
   Buffers: shared hit=1553
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..27603.81 rows=920 width=82) (actual time=0.030..3.298 rows=51.00 loops=1)
         Filter: ((make = 'Honda'::text) AND (model = 'Civic'::text))
         Rows Removed by Filter: 1570
         Index Searches: 1
         Buffers: shared hit=1553
 Planning Time: 0.145 ms
 Execution Time: 3.344 ms
```

**7. Same as 4 with a generic plan (why plan_cache_mode=force_custom_plan)**

```
 Limit  (cost=0.42..3003.36 rows=5 width=82) (actual time=27.984..28.012 rows=51.00 loops=1)
   Buffers: shared hit=41559
   ->  Index Scan using idx_vehicles_age on vehicles v  (cost=0.42..30029.81 rows=50 width=82) (actual time=27.982..28.006 rows=51.00 loops=1)
         Filter: ((($1 IS NULL) OR (make = $1)) AND (($2 IS NULL) OR (model = $2)) AND (($3 IS NULL) OR (received_at <= $3)) AND (($4 IS NULL) OR (received_at >= $4)) AND (($5 IS NULL) OR (ROW(received_at, id) > ROW($5, $6))))
         Rows Removed by Filter: 44003
         Index Searches: 1
         Buffers: shared hit=41559
 Planning Time: 0.210 ms
 Execution Time: 28.051 ms
```

Case 7 is the reason the pool sets `plan_cache_mode = force_custom_plan`. A cached generic plan cannot fold away the `$n IS NULL OR …` filters, so the age bounds stop being index conditions. Postgres then walks the index from the oldest row and filters out 44k rows: 28 ms instead of 0.17 ms. Without the setting, pgx's statement cache would hit this after the fifth execution of the statement.
