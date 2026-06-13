# Mulan POS System

## Project Overview
Go-based Point of Sale (POS) system. Two modules:
- **mulan** — main server (API, manager web UI)
- **mulan-agent** — device agent running on POS terminal, serves POS UI, controls cash drawer (GS-410B) and VFD display (COM3)
- **mulan-manager** (separate repo, `../mulan-manager`) — phone-first iOS-style **SvelteKit** frontend for the manager pages, deployed on render.com, consuming this backend's `/api/*` over Tailscale. Progressively replacing the Go `html/template` `/manager/*` pages (those still serve until fully replaced).

## Deployment
The backend runs on tailnet node **chaiyarak** (`100.109.90.83`) as **systemd service `mulan`** in `~/mulan-deploy/` (static `CGO_ENABLED=0` linux/amd64 binary + `templates/` + `elements/` + `.env`; DB = local Postgres on that host). Update: cross-build → `scp` to `~/mulan-deploy/mulan.new` → `mv -f mulan.new mulan` (can't overwrite a running binary — ETXTBSY) → `sudo systemctl restart mulan`. The render frontend reaches it at `http://100.109.90.83:8085` over Tailscale.

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
- `/manager/settings` — shop name + VAT percent (persisted in DB)

## API Endpoints

**Auth (manager) — manager `/api/*` routes are auth-scoped; see Manager Authentication below. POS-shared reads stay open.**
- `POST /api/auth/login` — `{username,password}` → `{token, expires_at, user:{id,username,name,role}}` (opaque bearer)
- `POST /api/auth/logout` · `GET /api/auth/me` · `POST /api/auth/change-password` `{current_password,new_password}` — require bearer
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
- `PUT /api/menus/{id}/option-groups` — set the menu's option-group list `{groups: [..]}` (replaces all). Each entry is either shared `{isolated:false, id}` or isolated `{isolated:true, name, selection_mode, options:[{name, price_delta}]}` (price_delta in THB)
- `PUT /api/menus/{id}/base-options` — set the menu's base options `{base_options: [{name, price}]}` (replaces all; price in THB, absolute). Empty list = no base option.
- `GET /api/discounts` — list all preset discounts (manager)
- `GET /api/discounts/active` — list active discounts only (POS picker)
- `POST /api/discounts` — create `{name, discount_type, value, active, is_subsidy}` (types: `fixed`/`percent`; `is_subsidy` = sponsor-covered)
- `PATCH /api/discounts/{id}` — update a discount
- `DELETE /api/discounts/{id}` — delete a discount

## Option Groups
Shared, reusable option groups attach to menus via `menu_option_groups`. Each group has a selection mode: `single_required` (must pick one), `single_optional` (zero or one), `multi` (any). Options carry a `price_delta` in satang. Order lines snapshot selected options into `order_item_options` (name + price_delta) so receipts stay stable when groups/options edit later. Menu API responses include `option_groups` populated with their options. POS opens a modal whenever a clicked menu has any attached group; selected options print as indented sub-lines on both the order bill (kitchen) and the receipt.

### Isolated (per-menu) groups
When attaching a group to a menu item, the manager can tick **Customize** to isolate it. An isolated group is a private clone: `option_groups.owner_menu_id` points at the owning menu (NULL = shared preset). It is hidden from the shared list (`ListOptionGroups` filters `owner_menu_id IS NULL`) so it never appears when editing other items, and its options/prices are editable inline in the item dialog without touching the source preset. `SetMenuGroups` fully replaces a menu's groups in one transaction — it clears links, drops the menu's old private groups (`DeletePrivateGroupsForMenu`), then re-attaches shared groups and recreates isolated ones. Deleting a menu cascades its private groups. Menu API `option_groups[]` entries carry an `isolated` bool.

## Base Option
A menu may have at most one **Base Option** set: named, absolute-priced variants
(e.g. Hot=50, Iced=80) stored in `menu_base_options(menu_id, name, price, sort_order)`.
Picking one at POS *sets* the line's base price (it replaces `menus.price`, which
becomes a fallback used only when a menu has no base options). Selection is
required when a menu has base options. Normal `+delta` option groups still stack
on top. The chosen variant is snapshotted onto `order_items.base_option_name`
(and the chosen price into `order_items.price`), so receipts stay stable. The
receipt/kitchen bill print the item inline as `Americano (Iced)  80` with no
price breakdown for the base option. Managed in `/manager/items`; set via
`PUT /api/menus/{id}/base-options`.

Legacy delta-based "Serve" groups are migrated by `cmd/convert-base-option`
(`base.price = menu.price + delta`; dry-run by default, `--apply` to commit,
`--source-name` to override the matched group name). Runs against the local DB.

## Discounts
Preset discounts created in `/manager/discounts`, applied by the cashier at POS.
A discount is either `fixed` (flat THB off) or `percent` (% off). The `discounts.value`
column stores both scaled by 100: satang for fixed, hundredths-of-percent for percent
(10% = 1000). API JSON exposes `value` already divided by 100 (THB / percent).
Discounts apply at two scopes: per cart line and whole order. Checkout computes
authoritative totals — per-line discounts reduce each line, whole-order discounts
reduce the net subtotal, then VAT is computed on the discounted subtotal (discount
before VAT). Multiple discounts may stack; each is clamped so no line and no order
total goes below zero. Every applied discount is snapshotted into `order_discounts`
(name + type + computed satang amount; `order_item_id` null for whole-order) so
receipts/reports stay stable when a preset is later edited or deleted. Checkout
response includes `discount` (total THB off) and a `discounts` list. Inactive
discounts are rejected at checkout.

### Subsidise discounts
A discount can be flagged `is_subsidy` (checkbox in `/manager/discounts`,
orthogonal to fixed/percent). A subsidise discount means a sponsor covers the
gap: the customer pays less but the shop is made whole (full item revenue). It
does NOT reduce reported revenue, and VAT is reckoned on the full pre-subsidy
amount. `is_subsidy` is snapshotted into `order_discounts` so reports stay
stable. Checkout response adds `subsidy` (THB). Prices are VAT-inclusive and the
backend computes VAT as the inclusive portion of the shop-received amount
(`computeOrderTotals` in `internal/order/service/totals.go`) — this replaced an
earlier bug that added VAT on top. Reports show a waterfall: Gross − Discounts =
Net sales, + Subsidy; `/api/dashboard/subsidies` lists subsidy spend by program.

## Settings (DB-backed)
Single-row `settings` table (PK check `id = 1`). Seeded on first startup with defaults. Holds `shop_name`, `vat_percent` (double precision, 0 disables VAT), and `points_per_baht` (double precision, default 1 = 1 loyalty point per ฿1; 0 disables earning). `SettingsService` caches the row in memory and refreshes on update. Shop name is delivered to the agent via the `/api/orders/{code}/checkout` response (no STORE_NAME env var).

## Membership / Loyalty
Optional, phone-keyed membership. `members` table: `phone` (unique), `name` (optional), `points` (bigint balance), timestamps. A phone is captured at the POS via a modal on **Pay** (Skip = no member); the modal live-looks-up an existing member via `/api/members/lookup`. On checkout, if a phone is provided, the order service find-or-creates the member, awards `floor(total_paid_THB × points_per_baht)` points, and snapshots `orders.member_id` + `orders.points_earned` — all inside the existing checkout transaction (atomic, no double-award on re-checkout). **Points are earn-and-track only for now — no redemption.** Members are also managed manually at `/manager/members`. The receipt prints a member/points footer block; the kitchen order bill does not. Earn rate is configurable in Settings (`points_per_baht`).

## Manager Authentication
`internal/managerauth/` — opaque bearer-token sessions for the SvelteKit manager, **separate from POS `cashiers`** (which stay PIN-only). Tables: `manager_users` (username, bcrypt `password_hash`, name, `role` = `owner|staff`, active) + `manager_sessions` (SHA-256-hashed token, `expires_at`, `revoked_at`; 30-day TTL). Middleware `RequireManager` (valid session → user in ctx) + `RequireRole(owner)`. Seed: `go run ./cmd/create-manager-user -username U -password P -name N -role owner|staff`. Login bcrypt-verifies and runs a dummy compare on unknown user (constant-time, anti-enumeration); passwords ≥8 chars; change-password validates the current password.

**Route scoping (in `main.go`'s `/api` block):**
- **Open** (POS/agent/shared, no auth): `GET /api/menus`, `GET /api/menu-categories`, `GET /api/settings`(+`/logo`), `GET /api/members/lookup`, `POST /api/cashiers/login`, `/api/orders`, `/api/cash-drawer`, `/api/wifi`, `GET /api/discounts/active`, `POST /api/auth/login`.
- **`RequireManager`** (any logged-in manager, reads): `GET /api/option-groups`, `GET /api/members`(+`/{id}/orders`), `GET /api/cashiers`, `GET /api/discounts`, `/api/auth/{me,logout,change-password}`.
- **`RequireRole(owner)`** (writes + owner data): all menu/category/option-group/option/member/cashier/settings **writes** (incl. `PUT …/option-groups`, `…/base-options`, `…/toggle`), discount writes, `/api/dashboard/*`.

Adding a manager route: register it in the right group in `main.go` AND add its prefix to the proxy `ALLOW` in mulan-manager's `src/routes/api/[...path]/+server.ts`. **Never wrap a POS-shared read** (it breaks the POS) — verify with a no-token curl returning 200.

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
