-- name: CreateCashier :one
INSERT INTO cashiers (login_id, name, pin_hash)
VALUES ($1, $2, $3)
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at;

-- name: UpdateCashier :one
UPDATE cashiers
SET name       = $2,
    active     = $3,
    updated_at = now()
WHERE id = $1
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at;

-- name: UpdateCashierPin :one
UPDATE cashiers
SET pin_hash   = $2,
    updated_at = now()
WHERE id = $1
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at;

-- name: DeleteCashier :exec
DELETE FROM cashiers WHERE id = $1;
