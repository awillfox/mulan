-- name: CreateMenu :one
INSERT INTO menus (name, price, category_id, vfd_name)
VALUES ($1, $2, $3, $4)
RETURNING id, name, price, category_id, vfd_name;

-- name: UpdateMenu :one
UPDATE menus SET name = $2, price = $3, category_id = $4, vfd_name = $5
WHERE id = $1
RETURNING id, name, price, category_id, vfd_name;

-- name: DeleteMenu :exec
DELETE FROM menus WHERE id = $1;
