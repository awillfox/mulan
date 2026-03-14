-- name: ListMenuCategories :many
SELECT id, name FROM menu_categories ORDER BY id;

-- name: GetMenuCategory :one
SELECT id, name FROM menu_categories WHERE id = $1;
