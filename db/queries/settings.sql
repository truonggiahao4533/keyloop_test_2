-- name: GetAgingThreshold :one
SELECT aging_threshold_days FROM app_settings WHERE id;

-- name: SetAgingThreshold :exec
UPDATE app_settings SET aging_threshold_days = @days WHERE id;
