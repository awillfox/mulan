-- name: TopMenusBySales :many
SELECT oi.name,
       SUM(oi.qty)::bigint              AS qty_sold,
       SUM(oi.price * oi.qty)::bigint   AS revenue
FROM order_items oi
JOIN orders o ON o.id = oi.order_id
WHERE o.status = 'paid'
  AND o.created_at >= $1::timestamptz
  AND o.created_at < $2::timestamptz
GROUP BY oi.name
ORDER BY qty_sold DESC
LIMIT 10;
