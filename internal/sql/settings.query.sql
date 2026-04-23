-- name: GetSettings :one
SELECT id, shop_name, vat_percent, updated_at FROM settings WHERE id = 1;
