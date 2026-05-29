-- Add new schema named "public"
CREATE SCHEMA IF NOT EXISTS "public";
-- Set comment to schema: "public"
COMMENT ON SCHEMA "public" IS 'standard public schema';
-- Create "cash_drawer_audit" table
CREATE TABLE "public"."cash_drawer_audit" (
  "id" bigserial NOT NULL,
  "event_type" character varying(20) NOT NULL,
  "amount" bigint NULL,
  "delta" bigint NULL,
  "note" character varying(255) NULL,
  "actor" character varying(120) NULL,
  "terminal" character varying(120) NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "cash_drawer_audit_event_type" CHECK ((event_type)::text = ANY ((ARRAY['set'::character varying, 'clear'::character varying, 'adjust'::character varying, 'kick'::character varying, 'open_for_change'::character varying])::text[]))
);
-- Create index "cash_drawer_audit_created_at" to table: "cash_drawer_audit"
CREATE INDEX "cash_drawer_audit_created_at" ON "public"."cash_drawer_audit" ("created_at");
-- Create index "cash_drawer_audit_event_type_created_at" to table: "cash_drawer_audit"
CREATE INDEX "cash_drawer_audit_event_type_created_at" ON "public"."cash_drawer_audit" ("event_type", "created_at");
-- Create "cashiers" table
CREATE TABLE "public"."cashiers" (
  "id" serial NOT NULL,
  "login_id" character varying(20) NOT NULL,
  "name" character varying(255) NOT NULL,
  "pin_hash" character varying(255) NOT NULL,
  "active" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "cashiers_login_id_key" to table: "cashiers"
CREATE UNIQUE INDEX "cashiers_login_id_key" ON "public"."cashiers" ("login_id");
-- Create "members" table
CREATE TABLE "public"."members" (
  "id" serial NOT NULL,
  "phone" character varying(20) NOT NULL,
  "name" character varying(255) NULL,
  "points" bigint NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id")
);
-- Create index "members_phone_key" to table: "members"
CREATE UNIQUE INDEX "members_phone_key" ON "public"."members" ("phone");
-- Create "settings" table
CREATE TABLE "public"."settings" (
  "id" integer NOT NULL DEFAULT 1,
  "shop_name" character varying(255) NOT NULL DEFAULT 'My Shop',
  "vat_percent" double precision NOT NULL DEFAULT 0,
  "receipt_footer" character varying(255) NOT NULL DEFAULT 'Thank you! Come again!',
  "logo" bytea NULL,
  "logo_mime" character varying(60) NULL,
  "updated_at" timestamptz NOT NULL DEFAULT now(),
  "points_per_baht" double precision NOT NULL DEFAULT 1,
  PRIMARY KEY ("id"),
  CONSTRAINT "settings_singleton" CHECK (id = 1)
);
-- Create "orders" table
CREATE TABLE "public"."orders" (
  "id" serial NOT NULL,
  "code" character varying(20) NOT NULL,
  "status" character varying(20) NOT NULL DEFAULT 'open',
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "held_at" timestamptz NULL,
  "held_label" character varying(120) NULL,
  "held_payload" jsonb NOT NULL DEFAULT '{}',
  "member_id" integer NULL,
  "points_earned" bigint NOT NULL DEFAULT 0,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_orders_member" FOREIGN KEY ("member_id") REFERENCES "public"."members" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "orders_code_key" to table: "orders"
CREATE UNIQUE INDEX "orders_code_key" ON "public"."orders" ("code");
-- Create index "orders_status_held_at" to table: "orders"
CREATE INDEX "orders_status_held_at" ON "public"."orders" ("status", "held_at");
-- Create "guest_wifi_users" table
CREATE TABLE "public"."guest_wifi_users" (
  "id" serial NOT NULL,
  "username" character varying(40) NOT NULL,
  "state" character varying(20) NOT NULL DEFAULT 'pending',
  "order_id" integer NULL,
  "assigned_at" timestamptz NULL,
  "expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_guest_wifi_order" FOREIGN KEY ("order_id") REFERENCES "public"."orders" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "guest_wifi_users_state_check" CHECK ((state)::text = ANY ((ARRAY['pending'::character varying, 'assigned'::character varying, 'active'::character varying, 'expired'::character varying])::text[]))
);
-- Create index "guest_wifi_users_state" to table: "guest_wifi_users"
CREATE INDEX "guest_wifi_users_state" ON "public"."guest_wifi_users" ("state");
-- Create index "guest_wifi_users_username_key" to table: "guest_wifi_users"
CREATE UNIQUE INDEX "guest_wifi_users_username_key" ON "public"."guest_wifi_users" ("username");
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
-- Create "option_groups" table
CREATE TABLE "public"."option_groups" (
  "id" serial NOT NULL,
  "name" character varying(255) NOT NULL,
  "selection_mode" character varying(20) NOT NULL DEFAULT 'single_required',
  "owner_menu_id" integer NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_og_owner_menu" FOREIGN KEY ("owner_menu_id") REFERENCES "public"."menus" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "option_groups_selection_mode" CHECK ((selection_mode)::text = ANY ((ARRAY['single_required'::character varying, 'single_optional'::character varying, 'multi'::character varying])::text[]))
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
-- Create "discounts" table
CREATE TABLE "public"."discounts" (
  "id" serial NOT NULL,
  "name" character varying(255) NOT NULL,
  "discount_type" character varying(20) NOT NULL DEFAULT 'fixed',
  "value" bigint NOT NULL DEFAULT 0,
  "active" boolean NOT NULL DEFAULT true,
  "created_at" timestamptz NOT NULL DEFAULT now(),
  "is_subsidy" boolean NOT NULL DEFAULT false,
  PRIMARY KEY ("id"),
  CONSTRAINT "discounts_discount_type" CHECK ((discount_type)::text = ANY ((ARRAY['fixed'::character varying, 'percent'::character varying])::text[]))
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
  CONSTRAINT "fk_order_items_menu" FOREIGN KEY ("menu_id") REFERENCES "public"."menus" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "fk_order_items_order" FOREIGN KEY ("order_id") REFERENCES "public"."orders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "order_discounts" table
CREATE TABLE "public"."order_discounts" (
  "id" serial NOT NULL,
  "order_id" integer NOT NULL,
  "order_item_id" integer NULL,
  "discount_id" integer NULL,
  "name" character varying(255) NOT NULL,
  "discount_type" character varying(20) NOT NULL,
  "amount" bigint NOT NULL,
  "is_subsidy" boolean NOT NULL DEFAULT false,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_order_discounts_discount" FOREIGN KEY ("discount_id") REFERENCES "public"."discounts" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "fk_order_discounts_item" FOREIGN KEY ("order_item_id") REFERENCES "public"."order_items" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "fk_order_discounts_order" FOREIGN KEY ("order_id") REFERENCES "public"."orders" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "order_discounts_order" to table: "order_discounts"
CREATE INDEX "order_discounts_order" ON "public"."order_discounts" ("order_id");
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
