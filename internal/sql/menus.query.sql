-- name: ListMenus :many
SELECT id, name, price, category_id, vfd_name FROM menus ORDER BY id;

-- name: GetMenu :one
SELECT id, name, price, category_id, vfd_name FROM menus WHERE id = $1;
