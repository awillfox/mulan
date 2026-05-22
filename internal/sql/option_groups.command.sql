-- name: CreateOptionGroup :one
-- owner_menu_id NULL = shared preset; set = private isolated copy for a menu.
INSERT INTO option_groups (name, selection_mode, owner_menu_id)
VALUES ($1, $2, $3)
RETURNING id, name, selection_mode, owner_menu_id;

-- name: UpdateOptionGroup :one
UPDATE option_groups SET name = $2, selection_mode = $3
WHERE id = $1
RETURNING id, name, selection_mode, owner_menu_id;

-- name: DeleteOptionGroup :exec
DELETE FROM option_groups WHERE id = $1;

-- name: DeletePrivateGroupsForMenu :exec
-- Drops every isolated group owned by the menu (options + menu links cascade).
DELETE FROM option_groups WHERE owner_menu_id = $1;
