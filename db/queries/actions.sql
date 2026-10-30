-- name: InsertAction :one
-- clock_timestamp(), not now(): concurrent logs on one vehicle serialize on
-- the row lock, and the later writer must get the later timestamp.
INSERT INTO vehicle_actions (id, vehicle_id, action_type, note, created_by, created_at)
VALUES (@id, @vehicle_id, @action_type, @note, @created_by, clock_timestamp())
RETURNING *;

-- name: ListActions :many
SELECT * FROM vehicle_actions
WHERE vehicle_id = @vehicle_id
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: LockIdempotencyKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(@key::text, 0));

-- name: GetIdempotencyKey :one
SELECT * FROM idempotency_keys WHERE key = @key;

-- name: InsertIdempotencyKey :exec
INSERT INTO idempotency_keys (key, request_hash, response_status, response_body)
VALUES (@key, @request_hash, @response_status, @response_body);
