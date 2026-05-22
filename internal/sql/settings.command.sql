-- name: SeedSettings :exec
INSERT INTO settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- name: UpdateSettings :one
UPDATE settings
SET shop_name       = $1,
    vat_percent     = $2,
    points_per_baht = $3,
    updated_at      = now()
WHERE id = 1
RETURNING id, shop_name, vat_percent, points_per_baht, updated_at;
