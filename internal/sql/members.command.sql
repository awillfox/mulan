-- name: CreateMember :one
INSERT INTO members (phone, name)
VALUES ($1, $2)
RETURNING id, phone, name, points, created_at, updated_at;

-- name: UpdateMember :one
UPDATE members
SET phone = $2,
    name = $3,
    updated_at = now()
WHERE id = $1
RETURNING id, phone, name, points, created_at, updated_at;

-- name: DeleteMember :exec
DELETE FROM members WHERE id = $1;

-- name: AddMemberPoints :one
UPDATE members
SET points = points + sqlc.arg('delta'),
    updated_at = now()
WHERE id = sqlc.arg('id')
RETURNING id, phone, name, points, created_at, updated_at;
