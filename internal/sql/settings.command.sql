-- name: SeedSettings :exec
INSERT INTO settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- name: UpdateSettings :one
UPDATE settings
SET shop_name      = $1,
    vat_percent    = $2,
    receipt_footer = $3,
    updated_at     = now()
WHERE id = 1
RETURNING id, shop_name, vat_percent, receipt_footer, updated_at;

-- name: SetSettingsLogo :exec
UPDATE settings
SET logo = $1, logo_mime = $2, updated_at = now()
WHERE id = 1;

-- name: ClearSettingsLogo :exec
UPDATE settings
SET logo = NULL, logo_mime = NULL, updated_at = now()
WHERE id = 1;
