-- name: CreateOrder :one
INSERT INTO orders (code, status)
VALUES ($1, 'open')
RETURNING id, code, status, created_at;

-- name: CreateOrderItem :exec
INSERT INTO order_items (order_id, menu_id, name, price, qty)
VALUES ($1, $2, $3, $4, $5);

-- name: PayOrder :exec
UPDATE orders SET status = 'paid' WHERE code = $1;
