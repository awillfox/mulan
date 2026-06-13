-- name: CreateManagerUser :one
INSERT INTO manager_users (username, password_hash, name, role)
VALUES ($1, $2, $3, $4)
RETURNING id, username, password_hash, name, role, active, created_at, updated_at;

-- name: CreateManagerSession :one
INSERT INTO manager_sessions (manager_user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING id, manager_user_id, token_hash, expires_at, created_at, revoked_at;

-- name: RevokeManagerSession :exec
UPDATE manager_sessions
SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: DeleteExpiredManagerSessions :exec
DELETE FROM manager_sessions
WHERE expires_at < now();
