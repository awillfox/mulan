# Mulan POS System

## Project Overview
Go-based Point of Sale (POS) system. Two modules:
- **mulan** — main server (API, manager web UI)
- **mulan-agent** — device agent running on POS terminal, serves POS UI, controls cash drawer (GS-410B) and VFD display (COM3)

Claude will update CLAUDE.md a long the way

## Target Hardware
- **POS Terminal:** Flytech POS485, 15" display, Windows 11 with VFD display on COM3 (UI is responsive across viewport sizes)

- **Cash Drawer:** GS-410B 
## Tech Stack
- **Language:** Go 1.25
- **Database:** PostgreSQL (via pgx/v5, pgxpool)
- **Router:** chi/v5
- **Config:** Viper (reads .env)
- **Money:** Rhymond/go-money (THB only) — all money as int64 (satang), float only for display
- **SQL Generation:** sqlc
- **Schema Management:** Atlas (HCL schema)
- **Frontend:** Tailwind CSS (CDN), Go html/template
- **Task Runner:** Taskfile

## Development Commands
- `task migrate-dev` — apply schema to dev database
- `task migrate-prod` — apply schema to prod database
- `task generate-sql-schema` — export SQL schema from dev DB
- `task sqlcgen` — generate Go code from SQL queries

## Pages
- `/pos` — POS interface, responsive layout (target: 15" Flytech POS485, also usable on tablets/desktops) **(served by mulan-agent)**
- `/manager` — dashboard, responsive for modern devices **(served by mulan)**
- `/manager/items` — item/category manager (also attaches option groups to menus)
- `/manager/option-groups` — manage shared option groups + their options
- `/manager/members` — member directory: search, CRUD, per-member points + order history
- `/manager/settings` — shop name + VAT percent + loyalty earn rate (persisted in DB)

## API Endpoints
- `GET /api/menus` — returns list of menus (currently mock data)
- `GET /api/menu-categories` — list all menu categories
- `POST /api/menu-categories` — create a menu category `{name}`
- `PATCH /api/menu-categories/{id}` — update a menu category `{name}`
- `DELETE /api/menu-categories/{id}` — delete a menu category
- `GET /api/settings` — returns `{shop_name, vat_percent, points_per_baht}`
- `PATCH /api/settings` — updates `{shop_name, vat_percent, points_per_baht}`
- `GET /api/members` — list members (optional `?q=` search on phone/name)
- `POST /api/members` — create member `{phone, name?}` (409 on duplicate phone)
- `PATCH /api/members/{id}` — update member `{phone, name?}`
- `DELETE /api/members/{id}` — delete member (past orders kept, member_id set null)
- `GET /api/members/{id}/orders` — member's paid order history (code, date, subtotal, points_earned)
- `GET /api/members/lookup?phone=` — find one member by exact phone (404 if none)
- `GET /api/option-groups` — list groups with their options
- `POST /api/option-groups` — create group `{name, selection_mode}` (modes: `single_required`/`single_optional`/`multi`)
- `PATCH /api/option-groups/{id}` — update group
- `DELETE /api/option-groups/{id}` — delete group
- `POST /api/option-groups/{id}/options` — create option `{name, price_delta, sort_order}` (price_delta in THB)
- `PATCH /api/options/{id}` — update option
- `DELETE /api/options/{id}` — delete option
- `PUT /api/menus/{id}/option-groups` — set attached groups `{group_ids: [..]}` (replaces all)

## Option Groups
Shared, reusable option groups attach to menus via `menu_option_groups`. Each group has a selection mode: `single_required` (must pick one), `single_optional` (zero or one), `multi` (any). Options carry a `price_delta` in satang. Order lines snapshot selected options into `order_item_options` (name + price_delta) so receipts stay stable when groups/options edit later. Menu API responses include `option_groups` populated with their options. POS opens a modal whenever a clicked menu has any attached group; selected options print as indented sub-lines on both the order bill (kitchen) and the receipt.

## Settings (DB-backed)
Single-row `settings` table (PK check `id = 1`). Seeded on first startup with defaults. Holds `shop_name`, `vat_percent` (double precision, 0 disables VAT), and `points_per_baht` (double precision, default 1 = 1 loyalty point per ฿1; 0 disables earning). `SettingsService` caches the row in memory and refreshes on update. Shop name is delivered to the agent via the `/api/orders/{code}/checkout` response (no STORE_NAME env var).

## Membership / Loyalty
Optional, phone-keyed membership. `members` table: `phone` (unique), `name` (optional), `points` (bigint balance), timestamps. A phone is captured at the POS via a modal on **Pay** (Skip = no member); the modal live-looks-up an existing member via `/api/members/lookup`. On checkout, if a phone is provided, the order service find-or-creates the member, awards `floor(total_paid_THB × points_per_baht)` points, and snapshots `orders.member_id` + `orders.points_earned` — all inside the existing checkout transaction (atomic, no double-award on re-checkout). **Points are earn-and-track only for now — no redemption.** Members are also managed manually at `/manager/members`. The receipt prints a member/points footer block; the kitchen order bill does not. Earn rate is configurable in Settings (`points_per_baht`).

## Project Structure
- `main.go` — entry point: viper config, DB connection, chi router
- `internal/<feature>/domain/` — domain models/entities
- `internal/<feature>/service/` — business logic
- `internal/<feature>/http/` — HTTP handlers
- `internal/web/` — web page handlers (serves manager templates)
- `templates/layouts/` — layout templates (manager.html)
- `templates/manager/` — manager page templates
- `mulan-agent/templates/` — POS templates (layouts/pos.html, pos/index.html)
- `internal/sql/` — SQL query files for sqlc
- `sqlc/` — generated Go code from sqlc
- `schema.hcl` — Atlas HCL database schema
- `sqlc.yml` — sqlc configuration
- `.env` — environment config (PSQL_URL, PSQL_DEV_URL, PSQL_PROD_URL, PORT)
- `mulan-agent/main.go` — agent entry point: VFD display, POS web server
- `mulan-agent/.env` — agent config (API_BASE, PORT, INPOUTX64_DLL, RECEIPT_PRINTER_ADDR)
