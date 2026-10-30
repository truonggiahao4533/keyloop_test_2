-- name: ListVehicles :many
SELECT v.*
FROM vehicles v
WHERE v.status = 'IN_STOCK'
  AND (sqlc.narg('make')::text  IS NULL OR v.make  = sqlc.narg('make'))
  AND (sqlc.narg('model')::text IS NULL OR v.model = sqlc.narg('model'))
  AND (sqlc.narg('received_before')::date IS NULL OR v.received_at <= sqlc.narg('received_before'))
  AND (sqlc.narg('received_after')::date  IS NULL OR v.received_at >= sqlc.narg('received_after'))
  AND (sqlc.narg('cur_date')::date IS NULL OR (v.received_at, v.id) > (sqlc.narg('cur_date'), sqlc.narg('cur_id')::uuid))
ORDER BY v.received_at, v.id
LIMIT @lim;

-- name: CountInStock :one
SELECT count(*) FROM vehicles WHERE status = 'IN_STOCK';

-- name: AgingByMake :many
SELECT make, count(*) AS count
FROM vehicles
WHERE status = 'IN_STOCK' AND received_at <= @cutoff
GROUP BY make
ORDER BY count DESC, make;

-- name: AgingByAction :many
SELECT current_action_type, count(*) AS count
FROM vehicles
WHERE status = 'IN_STOCK' AND received_at <= @cutoff
GROUP BY current_action_type
ORDER BY count DESC, current_action_type NULLS LAST;

-- name: UpsertVehicles :many
-- Unchanged rows are skipped by the WHERE clause, so they are not returned.
INSERT INTO vehicles (id, vin, make, model, year, price, received_at, status)
SELECT unnest(@ids::uuid[]), unnest(@vins::text[]), unnest(@makes::text[]),
  unnest(@models::text[]), unnest(@years::int[]), unnest(@prices::numeric[]),
  unnest(@received_ats::date[]), unnest(@statuses::text[])
ON CONFLICT (vin) DO UPDATE SET
  make = EXCLUDED.make, model = EXCLUDED.model, year = EXCLUDED.year,
  price = EXCLUDED.price, received_at = EXCLUDED.received_at, status = EXCLUDED.status
WHERE (vehicles.make, vehicles.model, vehicles.year, vehicles.price, vehicles.received_at, vehicles.status)
  IS DISTINCT FROM (EXCLUDED.make, EXCLUDED.model, EXCLUDED.year, EXCLUDED.price, EXCLUDED.received_at, EXCLUDED.status)
RETURNING (xmax = 0)::boolean AS inserted;

-- name: LockVehicle :one
SELECT status FROM vehicles WHERE id = @id FOR UPDATE;

-- name: VehicleExists :one
SELECT EXISTS (SELECT 1 FROM vehicles WHERE id = @id);

-- name: SetCurrentAction :exec
UPDATE vehicles SET current_action_type = @action_type, current_action_at = @action_at
WHERE id = @id;
