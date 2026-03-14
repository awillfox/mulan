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
  PRIMARY KEY ("id"),
  CONSTRAINT "orders_code_key" UNIQUE ("code")
);
-- Create "order_items" table
CREATE TABLE "public"."order_items" (
  "id" serial NOT NULL,
  "order_id" integer NOT NULL,
  "menu_id" integer NULL,
  "name" character varying(255) NOT NULL,
  "price" bigint NOT NULL,
  "qty" integer NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "order_items_menu_id_fkey" FOREIGN KEY ("menu_id") REFERENCES "public"."menus" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "order_items_order_id_fkey" FOREIGN KEY ("order_id") REFERENCES "public"."orders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
