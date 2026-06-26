-- name: GetAssignedWifiUserByOrder :one
SELECT * FROM guest_wifi_users
WHERE order_id = sqlc.arg('order_id')
  AND state IN ('assigned', 'active')
LIMIT 1;

-- name: UsernameExists :one
SELECT count(*) FROM guest_wifi_users WHERE username = sqlc.arg('username');

-- name: GetPendingWifiUser :one
SELECT * FROM guest_wifi_users
WHERE state = 'pending'
ORDER BY id
LIMIT 1;

-- name: ListGuestWifiUsers :many
SELECT * FROM guest_wifi_users
ORDER BY id;
