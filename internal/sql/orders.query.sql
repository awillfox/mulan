-- name: GetOrderByCode :one
SELECT id, code, status, created_at FROM orders WHERE code = $1;

-- name: SumOrderItems :one
SELECT COALESCE(SUM(price * qty), 0)::bigint AS subtotal
FROM order_items
WHERE order_id = $1;

-- name: SumTodaySales :one
SELECT COALESCE(SUM(oi.price * oi.qty), 0)::bigint AS total
FROM orders o
JOIN order_items oi ON oi.order_id = o.id
WHERE o.status = 'paid'
  AND o.created_at >= CURRENT_DATE
  AND o.created_at < CURRENT_DATE + INTERVAL '1 day';

-- name: CountTodayOrders :one
SELECT COUNT(*)::bigint AS count
FROM orders
WHERE status = 'paid'
  AND created_at >= CURRENT_DATE
  AND created_at < CURRENT_DATE + INTERVAL '1 day';

-- name: ListHeldOrders :many
SELECT id, code, status, created_at, held_at, held_label, held_payload
FROM orders
WHERE status = 'held'
ORDER BY held_at DESC NULLS LAST;

-- name: GetHeldOrder :one
SELECT id, code, status, created_at, held_at, held_label, held_payload
FROM orders
WHERE code = $1 AND status = 'held';
