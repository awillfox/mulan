-- name: ClearMenuBaseOptions :exec
DELETE FROM menu_base_options WHERE menu_id = $1;

-- name: CreateMenuBaseOption :one
INSERT INTO menu_base_options (menu_id, name, price, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING id, menu_id, name, price, sort_order;
