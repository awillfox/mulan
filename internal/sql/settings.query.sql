-- name: GetSettings :one
SELECT id, shop_name, vat_percent, points_per_baht, updated_at FROM settings WHERE id = 1;
