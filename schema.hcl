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
  column "created_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.id]
  }
  index "orders_code_key" {
    columns = [column.code]
    unique  = true
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
