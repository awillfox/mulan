-- name: ListOptionGroups :many
-- Shared preset groups only. Private isolated copies (owner_menu_id set)
-- belong to a single menu and must not show in the manager or item picker.
SELECT id, name, selection_mode, owner_menu_id
FROM option_groups
WHERE owner_menu_id IS NULL
ORDER BY id;

-- name: GetOptionGroup :one
SELECT id, name, selection_mode, owner_menu_id
FROM option_groups WHERE id = $1;

-- name: GetOptionGroupsByIDs :many
SELECT id, name, selection_mode, owner_menu_id
FROM option_groups
WHERE id = ANY(@ids::int[])
ORDER BY id;

-- name: ListOptionsByGroups :many
SELECT id, option_group_id, name, price_delta, sort_order
FROM options
WHERE option_group_id = ANY(@group_ids::int[])
ORDER BY option_group_id, sort_order, id;

-- name: ListMenuOptionGroupLinks :many
SELECT menu_id, option_group_id, sort_order
FROM menu_option_groups
WHERE menu_id = ANY(@menu_ids::int[])
ORDER BY menu_id, sort_order, option_group_id;
