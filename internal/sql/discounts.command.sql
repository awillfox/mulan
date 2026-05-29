-- name: CreateDiscount :one
INSERT INTO discounts (name, discount_type, value, active, is_subsidy)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, discount_type, value, active, created_at, is_subsidy;

-- name: UpdateDiscount :one
UPDATE discounts SET name = $2, discount_type = $3, value = $4, active = $5, is_subsidy = $6
WHERE id = $1
RETURNING id, name, discount_type, value, active, created_at, is_subsidy;

-- name: DeleteDiscount :exec
DELETE FROM discounts WHERE id = $1;

-- name: CreateOrderDiscount :exec
INSERT INTO order_discounts (order_id, order_item_id, discount_id, name, discount_type, amount, is_subsidy)
VALUES ($1, $2, $3, $4, $5, $6, $7);
