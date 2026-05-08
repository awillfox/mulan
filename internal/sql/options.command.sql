-- name: CreateOption :one
INSERT INTO options (option_group_id, name, price_delta, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING id, option_group_id, name, price_delta, sort_order;

-- name: UpdateOption :one
UPDATE options SET name = $2, price_delta = $3, sort_order = $4
WHERE id = $1
RETURNING id, option_group_id, name, price_delta, sort_order;

-- name: DeleteOption :exec
DELETE FROM options WHERE id = $1;

-- name: GetOptionsByIDs :many
SELECT id, option_group_id, name, price_delta, sort_order
FROM options
WHERE id = ANY(@ids::int[]);

-- name: ListOptionsByGroup :many
SELECT id, option_group_id, name, price_delta, sort_order
FROM options
WHERE option_group_id = $1
ORDER BY sort_order, id;

-- name: AttachMenuOptionGroup :exec
INSERT INTO menu_option_groups (menu_id, option_group_id, sort_order)
VALUES ($1, $2, $3)
ON CONFLICT (menu_id, option_group_id) DO UPDATE SET sort_order = EXCLUDED.sort_order;

-- name: DetachMenuOptionGroup :exec
DELETE FROM menu_option_groups WHERE menu_id = $1 AND option_group_id = $2;

-- name: ClearMenuOptionGroups :exec
DELETE FROM menu_option_groups WHERE menu_id = $1;
