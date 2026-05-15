-- name: CreateOrder :one
INSERT INTO orders (code, status)
VALUES ($1, 'open')
RETURNING id, code, status, created_at;

-- name: CreateOrderItem :one
INSERT INTO order_items (order_id, menu_id, name, price, qty)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: CreateOrderItemOption :exec
INSERT INTO order_item_options (order_item_id, option_id, name, price_delta)
VALUES ($1, $2, $3, $4);

-- name: PayOrder :exec
UPDATE orders SET status = 'paid' WHERE code = $1;

-- name: HoldOrder :one
UPDATE orders
SET status = 'held',
    held_at = now(),
    held_label = $2,
    held_payload = $3
WHERE code = $1
RETURNING id, code, status, created_at, held_at, held_label, held_payload;

-- name: ResumeOrder :one
-- Returns the payload captured at hold time, then atomically clears it.
-- Postgres evaluates the prev CTE against the snapshot before the UPDATE,
-- so RETURNING can surface the pre-update jsonb. Without the CTE the
-- subquery would observe the new (empty) value and we'd lose the cart.
WITH prev AS (
    SELECT o.held_payload AS old_payload
    FROM orders o
    WHERE o.code = $1 AND o.status = 'held'
)
UPDATE orders AS u
SET status = 'open',
    held_at = NULL,
    held_label = NULL,
    held_payload = '{}'::jsonb
WHERE u.code = $1 AND u.status = 'held'
RETURNING u.id, u.code, u.status, u.created_at,
          (SELECT old_payload FROM prev) AS held_payload;

-- name: DiscardHeldOrder :exec
DELETE FROM orders WHERE code = $1 AND status = 'held';
