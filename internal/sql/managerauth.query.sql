-- name: GetManagerUserByUsername :one
SELECT id, username, password_hash, name, role, active, created_at, updated_at
FROM manager_users
WHERE username = $1;

-- name: GetManagerSessionWithUser :one
SELECT s.id            AS session_id,
       s.expires_at    AS expires_at,
       s.revoked_at    AS revoked_at,
       u.id            AS user_id,
       u.username      AS username,
       u.name          AS name,
       u.role          AS role,
       u.active        AS active
FROM manager_sessions s
JOIN manager_users u ON u.id = s.manager_user_id
WHERE s.token_hash = $1;
