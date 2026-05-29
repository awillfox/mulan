-- name: ListMembers :many
SELECT id, phone, name, points, created_at, updated_at
FROM members
WHERE (
    phone ILIKE '%' || sqlc.narg('q') || '%'
    OR name ILIKE '%' || sqlc.narg('q') || '%'
    OR sqlc.narg('q') IS NULL
)
ORDER BY id;

-- name: GetMember :one
SELECT id, phone, name, points, created_at, updated_at
FROM members
WHERE id = $1;

-- name: FindMemberByPhone :one
SELECT id, phone, name, points, created_at, updated_at
FROM members
WHERE phone = $1;

-- name: ListOrdersByMember :many
SELECT o.id, o.code, o.created_at, o.points_earned,
       COALESCE(SUM(oi.price * oi.qty), 0)::bigint AS subtotal
FROM orders o
LEFT JOIN order_items oi ON oi.order_id = o.id
WHERE o.member_id = $1 AND o.status = 'paid'
GROUP BY o.id
ORDER BY o.created_at DESC;
