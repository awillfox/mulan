-- name: ListDiscounts :many
SELECT id, name, discount_type, value, active, created_at
FROM discounts
ORDER BY id;

-- name: ListActiveDiscounts :many
SELECT id, name, discount_type, value, active, created_at
FROM discounts
WHERE active = true
ORDER BY id;

-- name: GetDiscountsByIDs :many
SELECT id, name, discount_type, value, active, created_at
FROM discounts
WHERE id = ANY(@ids::int[])
ORDER BY id;
