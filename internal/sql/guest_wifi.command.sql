-- name: CreateGuestWifiUser :one
INSERT INTO guest_wifi_users (username, state)
VALUES (sqlc.arg('username'), 'pending')
RETURNING *;

-- name: AssignGuestWifiUser :one
UPDATE guest_wifi_users
SET state = 'assigned',
    order_id = sqlc.arg('order_id'),
    assigned_at = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: ActivateGuestWifiUser :exec
UPDATE guest_wifi_users
SET state = 'active',
    expires_at = now() + interval '2 hours'
WHERE order_id = sqlc.arg('order_id')
  AND state = 'assigned';

-- name: ExpireGuestWifiUsers :many
UPDATE guest_wifi_users
SET state = 'expired'
WHERE state = 'active'
  AND expires_at < now()
RETURNING *;

-- name: CountPendingGuestWifiUsers :one
SELECT count(*) FROM guest_wifi_users WHERE state = 'pending';
