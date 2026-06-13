-- name: ListCashiers :many
SELECT id, login_id, name, pin_hash, active, created_at, updated_at, role
FROM cashiers
ORDER BY login_id;

-- name: GetCashierByLoginID :one
SELECT id, login_id, name, pin_hash, active, created_at, updated_at, role
FROM cashiers WHERE login_id = $1;

-- name: GetCashier :one
SELECT id, login_id, name, pin_hash, active, created_at, updated_at, role
FROM cashiers WHERE id = $1;
