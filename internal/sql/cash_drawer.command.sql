-- name: AppendCashDrawerEvent :one
INSERT INTO cash_drawer_audit (event_type, amount, delta, note, actor, terminal)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, event_type, amount, delta, note, actor, terminal, created_at, denominations;

-- name: SeedCashDrawerDenomination :exec
-- Idempotent insert of one denomination row; does nothing if it already exists
-- so startup can run unconditionally without clobbering live counts.
INSERT INTO cash_drawer_denominations (denomination, count)
VALUES ($1, 0)
ON CONFLICT (denomination) DO NOTHING;

-- name: SetCashDrawerDenomination :exec
-- Absolute set of one denomination's count. Negative counts are rejected by the
-- table CHECK; the caller validates before calling.
UPDATE cash_drawer_denominations
SET count = $2, updated_at = now()
WHERE denomination = $1;

-- name: AdjustCashDrawerDenomination :one
-- Relative add/remove (delta is signed). RETURNING lets the caller confirm the
-- row existed; the CHECK (count >= 0) makes an over-subtraction fail the
-- statement (and tx). The param is named `delta` (not `count`) to make clear it
-- is an increment, unlike SetCashDrawerDenomination's absolute count.
UPDATE cash_drawer_denominations
SET count = count + sqlc.arg(delta), updated_at = now()
WHERE denomination = sqlc.arg(denomination)
RETURNING denomination, count;

-- name: AppendCashDrawerDenomEvent :one
INSERT INTO cash_drawer_audit (event_type, amount, delta, note, actor, terminal, denominations)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, event_type, amount, delta, note, actor, terminal, created_at, denominations;
