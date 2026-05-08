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
