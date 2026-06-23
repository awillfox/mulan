-- name: CreateCashier :one
INSERT INTO cashiers (login_id, name, pin_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at, role;

-- name: UpdateCashier :one
UPDATE cashiers
SET name       = $2,
    active     = $3,
    role       = $4,
    updated_at = now()
WHERE id = $1
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at, role;

-- name: UpdateCashierPin :one
UPDATE cashiers
SET pin_hash   = $2,
    updated_at = now()
WHERE id = $1
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at, role;

-- name: DeleteCashier :exec
DELETE FROM cashiers WHERE id = $1;
