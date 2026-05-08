-- name: CreateOptionGroup :one
INSERT INTO option_groups (name, selection_mode)
VALUES ($1, $2)
RETURNING id, name, selection_mode;

-- name: UpdateOptionGroup :one
UPDATE option_groups SET name = $2, selection_mode = $3
WHERE id = $1
RETURNING id, name, selection_mode;

-- name: DeleteOptionGroup :exec
DELETE FROM option_groups WHERE id = $1;
