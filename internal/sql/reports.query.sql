-- name: ListOrdersPage :many
SELECT o.id, o.code, o.status, o.created_at, o.paid_at, o.points_earned,
       COALESCE(m.name, '')  AS member_name,
       COALESCE(m.phone, '') AS member_phone
FROM orders o
LEFT JOIN members m ON m.id = o.member_id
WHERE (o.status = @status OR @status = '')
  AND o.created_at >= @from_at::timestamptz
  AND o.created_at <  @to_at::timestamptz
ORDER BY o.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountOrdersPage :one
SELECT COUNT(*)::bigint AS total
FROM orders o
WHERE (o.status = @status OR @status = '')
  AND o.created_at >= @from_at::timestamptz
  AND o.created_at <  @to_at::timestamptz;

-- name: ListOrderItemsByOrderIDs :many
SELECT oi.id, oi.order_id, oi.name,
       COALESCE(oi.base_option_name, '') AS base_option_name,
       oi.price, oi.qty
FROM order_items oi
WHERE oi.order_id = ANY(@order_ids::int[])
ORDER BY oi.id;

-- name: ListOrderItemOptionsByItemIDs :many
SELECT oio.order_item_id, oio.name, oio.price_delta
FROM order_item_options oio
WHERE oio.order_item_id = ANY(@item_ids::int[])
ORDER BY oio.id;

-- name: ListOrderDiscountsByOrderIDs :many
SELECT od.order_id, od.name, od.discount_type, od.amount, od.is_subsidy
FROM order_discounts od
WHERE od.order_id = ANY(@order_ids::int[])
ORDER BY od.id;
