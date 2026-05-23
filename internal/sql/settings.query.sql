-- name: GetSettings :one
SELECT id, shop_name, vat_percent, receipt_footer, points_per_baht, updated_at FROM settings WHERE id = 1;

-- name: GetSettingsLogo :one
SELECT logo, logo_mime, updated_at FROM settings WHERE id = 1;
