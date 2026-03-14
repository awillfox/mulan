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
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_menu_category" FOREIGN KEY ("category_id") REFERENCES "public"."menu_categories" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
