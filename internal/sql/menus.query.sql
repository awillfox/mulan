-- name: ListMenus :many
SELECT id, name, price, category_id, vfd_name, active, favourite, sort_order
FROM menus
ORDER BY category_id, sort_order, name;

-- name: GetMenu :one
SELECT id, name, price, category_id, vfd_name, active, favourite, sort_order
FROM menus WHERE id = $1;

-- name: GetMenusByIDs :many
SELECT id, name, price, category_id, vfd_name, active, favourite, sort_order
FROM menus
WHERE id = ANY(@ids::int[]);
