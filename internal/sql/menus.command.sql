-- name: CreateMenu :one
INSERT INTO menus (name, price, category_id, vfd_name, favourite)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, price, category_id, vfd_name, active, favourite, sort_order;

-- name: UpdateMenu :one
UPDATE menus SET name = $2, price = $3, category_id = $4, vfd_name = $5, favourite = $6
WHERE id = $1
RETURNING id, name, price, category_id, vfd_name, active, favourite, sort_order;

-- name: ToggleMenu :one
UPDATE menus SET active = NOT active WHERE id = $1
RETURNING id, name, price, category_id, vfd_name, active, favourite, sort_order;

-- name: DeleteMenu :exec
DELETE FROM menus WHERE id = $1;

-- name: SetMenuOrder :exec
-- Assigns sort_order = 1-based position for each id in @ids, but only for menus
-- that belong to @category_id (IS NOT DISTINCT FROM handles a NULL category).
-- Ids from another category are silently skipped, so a bad request can never
-- reorder a different category.
UPDATE menus
SET sort_order = data.ord
FROM (
    SELECT unnest(@ids::int[]) AS id,
           generate_subscripts(@ids::int[], 1) AS ord
) AS data
WHERE menus.id = data.id
  AND menus.category_id IS NOT DISTINCT FROM @category_id;
