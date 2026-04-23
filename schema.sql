-- Add new schema named "public"
CREATE SCHEMA IF NOT EXISTS "public";
-- Set comment to schema: "public"
COMMENT ON SCHEMA "public" IS 'standard public schema';
-- Create "menu_categories" table
CREATE TABLE "public"."menu_categories" (
  "id" serial NOT NULL,
  "name" character varying(255) NOT NULL,
  PRIMARY KEY ("id")
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
