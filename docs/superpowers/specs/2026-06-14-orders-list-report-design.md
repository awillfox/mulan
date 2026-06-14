# Design — Orders list / report page

**Date:** 2026-06-14
**Repos:** `mulan` (Go backend — new owner-gated endpoint) + `mulan-manager` (SvelteKit — new `/orders` page)
**Scope:** Full-stack. No schema change.

## Problem

There is no way to browse past orders. The manager needs a page that lists orders
in a table with per-order detail, filterable by date (and status), to review sales
history.

## Goals

1. Owner-gated endpoint that returns orders for a date range with reconstructed totals + line items.
2. SvelteKit `/orders` page: table of orders, date filter (presets + custom range), status filter, expandable per-order detail, pagination.
3. Reuse existing patterns (dashboard `presetRange`, iOS components, money-as-satang). Keep the iOS look.

## Non-goals

- No payment-method column — cash/card is **not persisted** anywhere; would need a schema change. Explicitly out.
- No schema change, no edits to orders, no CSV export (later).
- Does not touch the open `/api/dashboard/*` routes; order history is sensitive and stays owner-gated.

## Constraints / facts (verified 2026-06-14)

- `orders` columns: `id, code, status, member_id, points_earned, created_at, held_at, held_label, held_payload`. **No total / payment / cashier columns** → totals reconstructed.
- `order_items`: `order_id, menu_id, name, price (bigint satang), qty (int), base_option_name`.
- `order_item_options`: `order_item_id, name, price_delta` (snapshot of chosen options).
- `order_discounts`: `order_id, order_item_id (null=whole-order), name, discount_type, amount (bigint satang), is_subsidy`.
- `members`: `phone, name (nullable)`.
- Money is int64 satang in DB; dashboard JSON convention is **THB (satang/100)** — this endpoint follows the same (frontend uses the existing `baht()`).
- `response.OK` wraps payloads in `{"data": ...}`.
- Manager proxy `ALLOW` (`mulan-manager/src/routes/api/[...path]/+server.ts`) currently lists: discounts, dashboard, auth/*, menus, menu-categories, option-groups, options, members, cashiers, cash-drawer, settings. **Add `reports`.**

## Backend (mulan)

### Endpoint
`GET /api/reports/orders?from=&to=&status=&limit=&offset=` — **owner-gated** (`RequireManager` + `RequireRole(owner)`), mounted at `/api/reports` (NOT under the open `/dashboard`).

**Query params:**
- `from`, `to` — inclusive ISO `yyyy-mm-dd`, shop-local (`Asia/Bangkok`). Handler converts to `[from 00:00, to+1d 00:00)` timestamptz, same as the dashboard `rangeFromQuery`. Range capped at 92 days (reuse the dashboard cap).
- `status` — optional: `paid|open|held`. Empty/absent ⇒ all statuses.
- `limit` — default 100, max 200. `offset` — default 0.

**Response** `{"data": { "orders": [Order...], "total": <int> }}` where `total` is the full filtered count (for pagination), and each `Order`:
```jsonc
{
  "code": "BY4AWV6Y",
  "status": "paid",
  "created_at": "2026-06-14T15:13:40+07:00",
  "member_name": "Cream",          // "" when walk-in
  "member_phone": "0812345678",    // "" when walk-in
  "points_earned": 15,
  "item_count": 1,                 // distinct lines
  "qty": 1,                        // total units
  "gross": 90.00,                  // THB, Σ (price + Σ option price_delta) × qty
  "discount": 0,                   // THB, Σ non-subsidy order_discounts
  "subsidy": 0,                    // THB, Σ subsidy order_discounts
  "net": 90.00,                    // THB, gross − discount
  "line_items": [
    { "name": "Americano", "base_option_name": "Iced", "qty": 1, "price": 80.00,
      "options": [ { "name": "Oat milk", "price_delta": 10.00 } ] }
  ],
  "discounts": [ { "name": "Staff 10%", "discount_type": "percent", "amount": 0, "is_subsidy": false } ]
}
```
Open/held orders: no snapshotted discounts ⇒ `discount/subsidy = 0`, `net = gross`.

### Implementation — `internal/report/{domain,service,http}` (new feature pkg)
No N+1. Service does:
1. **Page query** `ListOrdersPage(status, from, to, limit, offset)` → orders + `members.name/phone` via LEFT JOIN, ordered `created_at DESC`. Plus `CountOrders(status, from, to)` for `total`.
2. **Batch items** `ListOrderItemsByOrderIDs(ids)` → `order_id, id, name, base_option_name, price, qty`.
3. **Batch item-options** `ListOrderItemOptionsByOrderItemIDs(itemIds)` → `order_item_id, name, price_delta`.
4. **Batch discounts** `ListOrderDiscountsByOrderIDs(ids)` → `order_id, name, discount_type, amount, is_subsidy`.
5. Stitch in Go: index items by `order_id`, options by `order_item_id`, discounts by `order_id`. Compute `gross = Σ ((item.price + Σ option.price_delta) × qty)` (option deltas included, matching dashboard `PeriodSummary` revenue), `discount = Σ amount where not is_subsidy`, `subsidy = Σ amount where is_subsidy`, `net = gross − discount`. Convert satang→THB (÷100) at the JSON boundary only.

SQL lives in `internal/sql/reports.query.sql`; regenerate with `task sqlcgen`.

### Route registration (`main.go`)
Inside the `RequireManager` → `RequireRole(owner)` group, add `r.Route("/reports", reportHandler.Routes)` (handler registers `GET /orders`). Document in CLAUDE.md route-scoping (owner group) + API list.

## Frontend (mulan-manager)

### `src/lib/api/reports.ts`
Types (`OrderRow`, `OrderLine`, `OrderDiscount`, `OrdersPage`) mirroring the JSON above + `listOrders({from,to,status,limit,offset})` calling `/api/reports/orders?...` through the proxy (bearer auto-attached).

### `src/routes/(app)/orders/+page.svelte`
- `NavBar title="Orders"`.
- **Date filter:** reuse `presetRange` (`$lib/dashboard/range`) for Today/7D/30D/90D chips, **plus** two `TextField`/date inputs for a custom From/To (custom overrides preset). Default preset **7D**.
- **Status filter:** `SegmentedControl` — All / Paid / Open / Held. Default **Paid** (sends `status=paid`; "All" sends no status).
- **Table:** wrapped in `overflow-x-auto` (scrolls on phone, full on desktop). Columns: Date/time · Code · Status · Items · Gross · Discount · Subsidy · Net. Money via `baht()`.
- **Expandable row:** clicking a row toggles a detail panel beneath it showing line items (name + base option + options with `price_delta`, qty, price), applied discounts, and member (name/phone) + points. Detail is already in the loaded payload — pure local toggle, no extra fetch.
- **Pagination:** `limit=100`; a "Load more" button appends the next offset while `loaded < total`; show `loaded / total`.
- Loading (`Spinner`), error (`EmptyState` + retry via `showToast`), empty ("No orders in this range").
- On filter change (preset/custom/status), reset offset and refetch.

### Nav + proxy
- Add `{ href: '/orders', label: 'Orders', icon: '🧾' }` to `SideNav.svelte` (under the top group) and to the `/more` page list (mobile).
- Add `'reports'` to the proxy `ALLOW` array.

## Data flow

`/orders` page holds `{ preset|customRange, status, offset, orders[], total }` in `$state`. Filter change → compute range (custom overrides preset) → `listOrders(...)` → store. "Load more" → fetch next offset, append. Money arrives as THB; format with `baht()`. No money math in JS beyond display.

## Error handling

- Backend: invalid date / range >92d / bad limit → 400. DB error → 500. Empty result → `{orders:[],total:0}` (200).
- Frontend: non-OK → toast + `EmptyState`; keep last data on refetch failure.

## Testing

**Backend (Go):**
- Service stitch/compute unit test (table-driven): given fake items/discounts, assert `gross/discount/subsidy/net`, satang→THB, walk-in vs member, open-order (no discounts) case.
- Handler test: param parsing (status filter, range cap 400, limit clamp), JSON shape.

**Frontend:**
- `reports.ts` query-string builder unit test (`*.spec.ts`): correct params incl. omitted `status` for "All".
- Component test (`*.svelte.spec.ts`): renders rows from sample payload; row expand reveals line items; empty state.
- Manual: phone (table scrolls) + desktop (full table, sidebar), filter + load-more against real data.

## Risks

- **Large ranges / many orders:** bounded by `limit` (≤200) + offset pagination; `total` lets the UI stop. Range capped 92d.
- **Totals correctness:** reconstructed, not snapshotted — must match the checkout math (gross = Σ price×qty incl. option deltas already baked into `order_items.price`? NO — option deltas live in `order_item_options`). **Decision:** `gross` = Σ(`order_items.price`×qty) only; option price_deltas are shown in detail but NOT added into the order gross here, because checkout snapshots the chosen base price into `order_items.price` and records option deltas separately — matching how the dashboard `PeriodSummary` sums `(oi.price + Σ option delta)×qty`. To stay consistent with dashboard revenue, **gross = Σ((price + Σ option price_delta)×qty)**. Implement gross including option deltas; verify against a known paid order's dashboard figure.

## Out of scope / later

- CSV/print export, payment-method column (needs schema change), free-text search.
