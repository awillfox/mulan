-- name: ListBaseOptionsByMenuIDs :many
SELECT id, menu_id, name, price, sort_order
FROM menu_base_options
WHERE menu_id = ANY(@menu_ids::int[])
ORDER BY menu_id, sort_order, id;

-- name: ListMenuBaseOptions :many
SELECT id, menu_id, name, price, sort_order
FROM menu_base_options
WHERE menu_id = $1
ORDER BY sort_order, id;
