-- name: TopMenusBySales :many
SELECT oi.name,
       SUM(oi.qty)::bigint              AS qty_sold,
       SUM(oi.price * oi.qty)::bigint   AS revenue
FROM order_items oi
JOIN orders o ON o.id = oi.order_id
WHERE o.status = 'paid'
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at < sqlc.arg('to_at')::timestamptz
GROUP BY oi.name
ORDER BY qty_sold DESC
LIMIT 10;

-- name: SalesByDay :many
SELECT date_trunc('day', o.created_at AT TIME ZONE sqlc.arg('tz')::text)::date AS day,
       COALESCE(SUM(oi.price * oi.qty), 0)::bigint AS revenue,
       COUNT(DISTINCT o.id)::bigint                AS orders,
       COALESCE(SUM(oi.qty), 0)::bigint            AS items
FROM orders o
LEFT JOIN order_items oi ON oi.order_id = o.id
WHERE o.status = 'paid'
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at <  sqlc.arg('to_at')::timestamptz
GROUP BY day
ORDER BY day;

-- name: SalesByHourDOW :many
SELECT EXTRACT(DOW  FROM o.created_at AT TIME ZONE sqlc.arg('tz')::text)::int AS dow,
       EXTRACT(HOUR FROM o.created_at AT TIME ZONE sqlc.arg('tz')::text)::int AS hour,
       COALESCE(SUM(oi.price * oi.qty), 0)::bigint AS revenue,
       COUNT(DISTINCT o.id)::bigint                AS orders
FROM orders o
LEFT JOIN order_items oi ON oi.order_id = o.id
WHERE o.status = 'paid'
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at <  sqlc.arg('to_at')::timestamptz
GROUP BY dow, hour
ORDER BY dow, hour;

-- name: PeriodSummary :one
SELECT COALESCE(SUM(oi.price * oi.qty), 0)::bigint AS revenue,
       COUNT(DISTINCT o.id)::bigint                AS orders,
       COALESCE(SUM(oi.qty), 0)::bigint            AS items
FROM orders o
LEFT JOIN order_items oi ON oi.order_id = o.id
WHERE o.status = 'paid'
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at <  sqlc.arg('to_at')::timestamptz;
