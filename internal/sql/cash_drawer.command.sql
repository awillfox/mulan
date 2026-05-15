-- name: AppendCashDrawerEvent :one
INSERT INTO cash_drawer_audit (event_type, amount, delta, note, actor, terminal)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, event_type, amount, delta, note, actor, terminal, created_at;
