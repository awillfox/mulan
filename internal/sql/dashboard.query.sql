-- name: TopMenusBySales :many
-- row_limit NULL means "no limit" (Postgres treats LIMIT NULL as unbounded):
-- the dashboard's item-mix donut passes 10, the "All items" list passes NULL.
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
LIMIT sqlc.narg('row_limit')::bigint;

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
SELECT
  COALESCE(SUM((oi.price + COALESCE(opt.delta, 0)) * oi.qty), 0)::bigint AS revenue,
  COUNT(DISTINCT o.id)::bigint                                           AS orders,
  COALESCE(SUM(oi.qty), 0)::bigint                                       AS items
FROM orders o
LEFT JOIN order_items oi ON oi.order_id = o.id
LEFT JOIN (
  SELECT order_item_id, SUM(price_delta) AS delta
  FROM order_item_options
  GROUP BY order_item_id
) opt ON opt.order_item_id = oi.id
WHERE o.status = 'paid'
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at <  sqlc.arg('to_at')::timestamptz;

-- name: DiscountSummary :one
-- Totals applied discounts for a period, split by subsidy flag. Aggregated on
-- its own (NOT joined into the order_items revenue sum) so revenue is never
-- cartesian-multiplied.
SELECT
  COALESCE(SUM(od.amount) FILTER (WHERE NOT od.is_subsidy), 0)::bigint AS discount,
  COALESCE(SUM(od.amount) FILTER (WHERE     od.is_subsidy), 0)::bigint AS subsidy
FROM order_discounts od
JOIN orders o ON o.id = od.order_id
WHERE o.status = 'paid'
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at <  sqlc.arg('to_at')::timestamptz;

-- name: SubsidyByProgram :many
-- Per-program subsidy spend for a period (the "subsidy by program" breakdown).
SELECT od.name, SUM(od.amount)::bigint AS amount
FROM order_discounts od
JOIN orders o ON o.id = od.order_id
WHERE o.status = 'paid' AND od.is_subsidy
  AND o.created_at >= sqlc.arg('from_at')::timestamptz
  AND o.created_at <  sqlc.arg('to_at')::timestamptz
GROUP BY od.name
ORDER BY amount DESC;
