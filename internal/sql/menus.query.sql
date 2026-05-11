-- name: ListMenus :many
SELECT id, name, price, category_id, vfd_name, active FROM menus ORDER BY id;

-- name: GetMenu :one
SELECT id, name, price, category_id, vfd_name, active FROM menus WHERE id = $1;

-- name: GetMenusByIDs :many
SELECT id, name, price, category_id, vfd_name, active
FROM menus
WHERE id = ANY(@ids::int[]);
