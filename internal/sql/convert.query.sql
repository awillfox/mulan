-- name: ListMenuLinksByGroupName :many
-- For the Serve->base-option converter: every (menu, group) link whose group
-- name matches case-insensitively, with the menu price + group owner needed
-- to convert deltas to absolute prices and decide isolated-clone disposal.
SELECT mog.menu_id        AS menu_id,
       og.id              AS group_id,
       og.owner_menu_id   AS owner_menu_id,
       m.price            AS menu_price
FROM menu_option_groups mog
JOIN option_groups og ON og.id = mog.option_group_id
JOIN menus m          ON m.id  = mog.menu_id
WHERE lower(og.name) = lower(@name::text)
ORDER BY mog.menu_id;
