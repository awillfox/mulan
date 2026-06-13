-- name: GetCurrentCashDrawerFloat :one
-- Returns the most recent absolute float reading (set / clear events carry an
-- amount; kicks and open_for_change do not). Denomination events also use
-- event_type 'set'/'adjust' but carry a non-NULL denominations payload, so they
-- are excluded here — this query is only about the legacy single-float reading.
-- NULL is acceptable — it just means no float has been recorded yet.
SELECT id, event_type, amount, created_at
FROM cash_drawer_audit
WHERE event_type IN ('set','clear')
  AND denominations IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: ListCashDrawerAudit :many
SELECT id, event_type, amount, delta, note, actor, terminal, created_at, denominations
FROM cash_drawer_audit
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: CountCashDrawerAudit :one
SELECT COUNT(*)::bigint FROM cash_drawer_audit;

-- name: ListCashDrawerDenominations :many
SELECT denomination, count, updated_at
FROM cash_drawer_denominations
ORDER BY denomination DESC;
