-- Add new schema named "public"
CREATE SCHEMA IF NOT EXISTS "public";
-- Set comment to schema: "public"
COMMENT ON SCHEMA "public" IS 'standard public schema';
-- Create "option_groups" table
CREATE TABLE "public"."option_groups" (
  "id" serial NOT NULL,
  "name" character varying(255) NOT NULL,
  "selection_mode" character varying(20) NOT NULL DEFAULT 'single_required',
  PRIMARY KEY ("id"),
  CONSTRAINT "option_groups_selection_mode" CHECK ((selection_mode)::text = ANY ((ARRAY['single_required'::character varying, 'single_optional'::character varying, 'multi'::character varying])::text[]))
);
-- Create "settings" table
CREATE TABLE "public"."settings" (
  "id" integer NOT NULL DEFAULT 1,
  "shop_name" character varying(255) NOT NULL DEFAULT 'My Shop',
  "vat_percent" double precision NOT NULL DEFAULT 0,
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "settings_singleton" CHECK (id = 1)
);
-- Create "menu_categories" table
CREATE TABLE "public"."menu_categories" (
  "id" serial NOT NULL,
  "name" character varying(255) NOT NULL,
  PRIMARY KEY ("id")
);
-- Create "menus" table
CREATE TABLE "public"."menus" (
  "id" serial NOT NULL,
  "name" character varying(255) NOT NULL,
  "price" bigint NOT NULL,
  "category_id" integer NULL,
  "vfd_name" character varying(20) NULL,
  "active" boolean NOT NULL DEFAULT true,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_menu_category" FOREIGN KEY ("category_id") REFERENCES "public"."menu_categories" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create "menu_option_groups" table
CREATE TABLE "public"."menu_option_groups" (
  "menu_id" integer NOT NULL,
  "option_group_id" integer NOT NULL,
  "sort_order" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("menu_id", "option_group_id"),
  CONSTRAINT "fk_mog_group" FOREIGN KEY ("option_group_id") REFERENCES "public"."option_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "fk_mog_menu" FOREIGN KEY ("menu_id") REFERENCES "public"."menus" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "options" table
CREATE TABLE "public"."options" (
  "id" serial NOT NULL,
  "option_group_id" integer NOT NULL,
  "name" character varying(255) NOT NULL,
  "price_delta" bigint NOT NULL DEFAULT 0,
  "sort_order" integer NOT NULL DEFAULT 0,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_options_group" FOREIGN KEY ("option_group_id") REFERENCES "public"."option_groups" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "options_group_sort" to table: "options"
CREATE INDEX "options_group_sort" ON "public"."options" ("option_group_id", "sort_order");
-- Create "orders" table
CREATE TABLE "public"."orders" (
  "id" serial NOT NULL,
  "code" character varying(20) NOT NULL,
  "status" character varying(20) NOT NULL DEFAULT 'open',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "orders_code_key" to table: "orders"
CREATE UNIQUE INDEX "orders_code_key" ON "public"."orders" ("code");
-- Create "order_items" table
CREATE TABLE "public"."order_items" (
  "id" serial NOT NULL,
  "order_id" integer NOT NULL,
  "menu_id" integer NULL,
  "name" character varying(255) NOT NULL,
  "price" bigint NOT NULL,
  "qty" integer NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_order_items_menu" FOREIGN KEY ("menu_id") REFERENCES "public"."menus" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "fk_order_items_order" FOREIGN KEY ("order_id") REFERENCES "public"."orders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "order_item_options" table
CREATE TABLE "public"."order_item_options" (
  "id" serial NOT NULL,
  "order_item_id" integer NOT NULL,
  "option_id" integer NULL,
  "name" character varying(255) NOT NULL,
  "price_delta" bigint NOT NULL DEFAULT 0,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_oio_option" FOREIGN KEY ("option_id") REFERENCES "public"."options" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "fk_oio_order_item" FOREIGN KEY ("order_item_id") REFERENCES "public"."order_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "oio_order_item" to table: "order_item_options"
CREATE INDEX "oio_order_item" ON "public"."order_item_options" ("order_item_id");
