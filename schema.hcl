schema "public" {}

table "settings" {
  schema = schema.public

  column "id" {
    type    = int
    null    = false
    default = 1
  }
  column "shop_name" {
    type    = varchar(255)
    null    = false
    default = "My Shop"
  }
  column "vat_percent" {
    type    = double_precision
    null    = false
    default = 0
  }
  column "receipt_footer" {
    type    = varchar(255)
    null    = false
    default = "Thank you! Come again!"
  }
  column "points_per_baht" {
    type    = double_precision
    null    = false
    default = 1
  }
  column "logo" {
    type = bytea
    null = true
  }
  column "logo_mime" {
    type = varchar(60)
    null = true
  }
  column "updated_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.id]
  }
  check "settings_singleton" {
    expr = "id = 1"
  }
}

table "orders" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "code" {
    type = varchar(20)
    null = false
  }
  column "status" {
    type = varchar(20)
    null = false
    default = "open"
  }
  column "member_id" {
    type = int
    null = true
  }
  column "points_earned" {
    type    = bigint
    null    = false
    default = 0
  }
  column "created_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }
  column "held_at" {
    type = timestamptz
    null = true
  }
  column "held_label" {
    type = varchar(120)
    null = true
  }
  column "held_payload" {
    type    = jsonb
    null    = false
    default = sql("'{}'::jsonb")
  }

  primary_key {
    columns = [column.id]
  }
  index "orders_code_key" {
    columns = [column.code]
    unique  = true
  }
  index "orders_status_held_at" {
    columns = [column.status, column.held_at]
  }
  foreign_key "fk_orders_member" {
    columns     = [column.member_id]
    ref_columns = [table.members.column.id]
    on_delete   = SET_NULL
  }
}

table "cashiers" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "login_id" {
    type = varchar(20)
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "pin_hash" {
    type = varchar(255)
    null = false
  }
  column "active" {
    type    = boolean
    null    = false
    default = true
  }
  column "created_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }
  column "updated_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.id]
  }
  index "cashiers_login_id_key" {
    columns = [column.login_id]
    unique  = true
  }
}

table "members" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "phone" {
    type = varchar(20)
    null = false
  }
  column "name" {
    type = varchar(255)
    null = true
  }
  column "points" {
    type    = bigint
    null    = false
    default = 0
  }
  column "created_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }
  column "updated_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.id]
  }
  index "members_phone_key" {
    columns = [column.phone]
    unique  = true
  }
}

table "cash_drawer_audit" {
  schema = schema.public

  column "id" {
    type = bigserial
    null = false
  }
  column "event_type" {
    type = varchar(20)
    null = false
  }
  column "amount" {
    type = bigint
    null = true
  }
  column "delta" {
    type = bigint
    null = true
  }
  column "note" {
    type = varchar(255)
    null = true
  }
  column "actor" {
    type = varchar(120)
    null = true
  }
  column "terminal" {
    type = varchar(120)
    null = true
  }
  column "created_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.id]
  }
  index "cash_drawer_audit_created_at" {
    columns = [column.created_at]
  }
  index "cash_drawer_audit_event_type_created_at" {
    columns = [column.event_type, column.created_at]
  }
  check "cash_drawer_audit_event_type" {
    expr = "event_type IN ('set','clear','adjust','kick','open_for_change')"
  }
}

table "order_items" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "order_id" {
    type = int
    null = false
  }
  column "menu_id" {
    type = int
    null = true
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "price" {
    type = bigint
    null = false
  }
  column "qty" {
    type = int
    null = false
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_order_items_order" {
    columns     = [column.order_id]
    ref_columns = [table.orders.column.id]
    on_delete   = CASCADE
  }
  foreign_key "fk_order_items_menu" {
    columns     = [column.menu_id]
    ref_columns = [table.menus.column.id]
    on_delete   = SET_NULL
  }
}

table "menu_categories" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }

  primary_key {
    columns = [column.id]
  }
}

table "option_groups" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "selection_mode" {
    type    = varchar(20)
    null    = false
    default = "single_required"
  }
  // NULL = shared preset group (shown in the manager / item picker).
  // Set = private "isolated" copy owned by a single menu, cloned from a
  // preset so its options and prices can be edited without touching the
  // shared preset. Hidden from the shared list; cascades when the menu dies.
  column "owner_menu_id" {
    type = int
    null = true
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_og_owner_menu" {
    columns     = [column.owner_menu_id]
    ref_columns = [table.menus.column.id]
    on_delete   = CASCADE
  }
  check "option_groups_selection_mode" {
    expr = "selection_mode IN ('single_required','single_optional','multi')"
  }
}

table "options" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "option_group_id" {
    type = int
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "price_delta" {
    type    = bigint
    null    = false
    default = 0
  }
  column "sort_order" {
    type    = int
    null    = false
    default = 0
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_options_group" {
    columns     = [column.option_group_id]
    ref_columns = [table.option_groups.column.id]
    on_delete   = CASCADE
  }
  index "options_group_sort" {
    columns = [column.option_group_id, column.sort_order]
  }
}

table "menu_option_groups" {
  schema = schema.public

  column "menu_id" {
    type = int
    null = false
  }
  column "option_group_id" {
    type = int
    null = false
  }
  column "sort_order" {
    type    = int
    null    = false
    default = 0
  }

  primary_key {
    columns = [column.menu_id, column.option_group_id]
  }
  foreign_key "fk_mog_menu" {
    columns     = [column.menu_id]
    ref_columns = [table.menus.column.id]
    on_delete   = CASCADE
  }
  foreign_key "fk_mog_group" {
    columns     = [column.option_group_id]
    ref_columns = [table.option_groups.column.id]
    on_delete   = CASCADE
  }
}

table "order_item_options" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "order_item_id" {
    type = int
    null = false
  }
  column "option_id" {
    type = int
    null = true
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "price_delta" {
    type    = bigint
    null    = false
    default = 0
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_oio_order_item" {
    columns     = [column.order_item_id]
    ref_columns = [table.order_items.column.id]
    on_delete   = CASCADE
  }
  foreign_key "fk_oio_option" {
    columns     = [column.option_id]
    ref_columns = [table.options.column.id]
    on_delete   = SET_NULL
  }
  index "oio_order_item" {
    columns = [column.order_item_id]
  }
}

table "menus" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "price" {
    type = bigint
    null = false
  }
  column "category_id" {
    type = int
    null = true
  }
  column "vfd_name" {
    type = varchar(20)
    null = true
  }
  column "active" {
    type    = boolean
    null    = false
    default = true
  }

  primary_key {
    columns = [column.id]
  }

  foreign_key "fk_menu_category" {
    columns     = [column.category_id]
    ref_columns = [table.menu_categories.column.id]
    on_delete   = SET_NULL
  }
}

table "discounts" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  # 'fixed' = flat THB off; 'percent' = percentage off.
  column "discount_type" {
    type    = varchar(20)
    null    = false
    default = "fixed"
  }
  # For 'fixed' this is satang (int64). For 'percent' this is
  # hundredths-of-a-percent (10% = 1000) so the column stays integer.
  column "value" {
    type    = bigint
    null    = false
    default = 0
  }
  column "active" {
    type    = boolean
    null    = false
    default = true
  }
  column "created_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.id]
  }
  check "discounts_discount_type" {
    expr = "discount_type IN ('fixed','percent')"
  }
}

# Snapshots every discount applied to a paid order. order_item_id is null
# for whole-order discounts and set for per-line discounts. name/type/amount
# are frozen at checkout so receipts and reports stay stable when a preset
# discount is later edited or deleted.
table "order_discounts" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "order_id" {
    type = int
    null = false
  }
  column "order_item_id" {
    type = int
    null = true
  }
  column "discount_id" {
    type = int
    null = true
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  column "discount_type" {
    type = varchar(20)
    null = false
  }
  column "amount" {
    type = bigint
    null = false
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_order_discounts_order" {
    columns     = [column.order_id]
    ref_columns = [table.orders.column.id]
    on_delete   = CASCADE
  }
  foreign_key "fk_order_discounts_item" {
    columns     = [column.order_item_id]
    ref_columns = [table.order_items.column.id]
    on_delete   = CASCADE
  }
  foreign_key "fk_order_discounts_discount" {
    columns     = [column.discount_id]
    ref_columns = [table.discounts.column.id]
    on_delete   = SET_NULL
  }
  index "order_discounts_order" {
    columns = [column.order_id]
  }
}
