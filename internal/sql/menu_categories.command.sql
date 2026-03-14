-- name: CreateMenuCategory :one
INSERT INTO menu_categories (name) VALUES ($1)
RETURNING id, name;

-- name: UpdateMenuCategory :one
UPDATE menu_categories SET name = $2 WHERE id = $1
RETURNING id, name;

-- name: DeleteMenuCategory :exec
DELETE FROM menu_categories WHERE id = $1;
