-- name: CreateMenu :one
INSERT INTO menus (name, price, category_id, vfd_name, favourite)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, price, category_id, vfd_name, active, favourite;

-- name: UpdateMenu :one
UPDATE menus SET name = $2, price = $3, category_id = $4, vfd_name = $5, favourite = $6
WHERE id = $1
RETURNING id, name, price, category_id, vfd_name, active, favourite;

-- name: ToggleMenu :one
UPDATE menus SET active = NOT active WHERE id = $1
RETURNING id, name, price, category_id, vfd_name, active, favourite;

-- name: DeleteMenu :exec
DELETE FROM menus WHERE id = $1;
