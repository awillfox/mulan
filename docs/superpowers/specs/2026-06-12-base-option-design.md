# Base Option — Design

**Date:** 2026-06-12
**Branch:** feat/member
**Status:** Approved design, pre-implementation

## Problem

The shop currently models drink "Serve" variants (Hot / Iced) as a regular
delta-based option group: menu price `50` + an `Iced +30` option. This prints a
`+30` breakdown line on the receipt and is just another multi-select group with
no structural guarantee of "exactly one, always".

We want a first-class **Base Option**: a per-menu set of named, **absolute**
priced variants where picking one *sets* the line's base price (Hot = 50,
Iced = 80), the receipt shows only the final price (no `+30` breakdown), and a
menu may have **at most one** such set (or none).

This replaces the "Serve" concept. Existing "Serve" groups are migrated to the
new model by a one-off `cmd/` converter.

## Decisions (locked)

| Question | Decision |
|---|---|
| Pricing model | **Absolute** — chosen base option price *replaces* the line base. |
| Cardinality | Max **1** base option set per menu, or none. |
| Receipt/kitchen display | Variant inline with item name: `Americano (Iced)  80`. No price breakdown for the base option. |
| POS selection | **Required** — must pick one before the item can be added. |
| Data model | **Dedicated per-menu table** `menu_base_options` (not a flag on `option_groups`). |
| Stacking | Normal `+delta` option groups still apply *on top* of the base. |
| Converter source match | Exact `option_groups.name == 'Serve'` (case-insensitive), `--source-name` flag override. |
| Converter old-group disposal | Detach menu link + delete isolated per-menu Serve clones; **keep** the shared `Serve` preset (left unlinked). |
| Converter safety | **Dry-run default**; `--apply` commits in a tx. Idempotent (skips menus already having base options). |

## Why dedicated table (model B)

Base options carry an **absolute** price and are inherently per-menu, always
single-required, always max-one. The existing `option_groups`/`options` machinery
stores **deltas**; reusing it would mean overloading `options.price_delta` to
sometimes mean an absolute price — magic that violates "no overloaded columns".
A small dedicated table keeps `price` meaning price and makes the cardinality
structural rather than enforced by validation.

## Data model

```hcl
table "menu_base_options" {
  id         serial PK
  menu_id    int  NOT NULL  FK -> menus.id  ON DELETE CASCADE
  name       varchar(255) NOT NULL
  price      bigint NOT NULL          # satang, ABSOLUTE, VAT-inclusive
  sort_order int NOT NULL default 0
  index (menu_id, sort_order)
}

# snapshot of the chosen base option onto the paid line (stable if menu edits later)
table "order_items" {
  ... existing ...
  base_option_name varchar(255) NULL   # NEW
}
```

- **Effective base price** of a line = chosen base option `price` when the menu
  has base options, else `menu.price`.
- `order_items.price` stores the **effective base** (chosen base price, or
  `menu.price` when no base options). `order_items.base_option_name` snapshots
  the variant name. The receipt is built from these, so editing/deleting the
  menu later does not change a past receipt.
- `menus.price` is kept (schema `NOT NULL`). It is the **fallback** used only
  when a menu has no base options; **ignored** at checkout when base options
  exist.

## Backend (main module)

### sqlc
New file `internal/sql/base_options.{query,command}.sql`:
- `ListBaseOptionsByMenuIDs(menu_ids []int)` — for menu API embed + checkout validation.
- `ListBaseOptionsByMenu(menu_id)` — converter idempotency check / single-menu read.
- `ClearMenuBaseOptions(menu_id)` — replace-all step.
- `CreateMenuBaseOption(menu_id, name, price, sort_order)`.
- `order_items` snapshot: extend `CreateOrderItem` to also set `base_option_name`.

Run `task sqlcgen` after.

### Menu API
`internal/menu/http/handler.go` — `menuResponse` gains:
```go
BaseOptions []menuBaseOptionResponse `json:"base_options"`
// {id int32, name string, price float64}  // price in THB
```
Populated by a `ListBaseOptionsByMenuIDs` lookup alongside the existing
`option_groups` embed.

### New set endpoint (mirrors `SetMenuGroups`)
`PUT /api/menus/{id}/base-options`
```json
{ "base_options": [ {"name": "Hot", "price": 50}, {"name": "Iced", "price": 80} ] }
```
Service method replaces all in one tx: `ClearMenuBaseOptions` then
`CreateMenuBaseOption` per entry (sort_order = array index). Empty list = remove
all base options. Implemented as a new `internal/baseoption/{service,http}`
feature mirroring the `optiongroup` package (keeps the menu feature focused);
route registered next to the option-groups route in `main.go`.

### Checkout (`internal/order/service/order.go`)
- `CheckoutItemInput += BaseOptionID int32` (0 = none).
- Load base options for the order's menus (map `menuID -> []baseOption`,
  and `baseOptionID -> baseOption`).
- Per line validation:
  - Menu **has** base options → `BaseOptionID` must be > 0 **and** belong to that
    menu. Else `ErrMissingBaseOption` / `ErrInvalidBaseOption` (new sentinels,
    mapped to 400 by the handler).
  - Menu has **no** base options → a non-zero `BaseOptionID` is rejected
    (`ErrInvalidBaseOption`).
- Pricing: `unitPrice := effectiveBase + deltaSum` where
  `effectiveBase = baseOption.price` (or `m.Price` when none). Was
  `m.Price + deltaSum`.
- Snapshot: `CreateOrderItem` with `Price = effectiveBase`,
  `BaseOptionName = baseOption.name` (NULL when none).
- `domain.CheckoutResultItem += BaseOptionName string`; response item `Price` =
  effective base.

Discounts, VAT (`computeOrderTotals`), and loyalty points are unchanged — they
already operate on the line totals / gross, which now reflect the base option
price automatically.

### Checkout response
`internal/order/http/handler.go` — `checkoutItemResponse += BaseOptionName string`
(`json:"base_option_name"`).

## Receipt / agent (mulan-agent)

- `mulan-agent/main.go`: `checkoutItem += BaseOptionName string`; pass it into
  `printer.OrderItem`.
- `lib/printer/escpos.go`: `OrderItem += BaseOptionName string`. A small helper
  `displayName()` returns `Name (BaseOptionName)` when set, else `Name`. Used in
  **both** `PrintOrderBill` (kitchen) and `PrintReceipt`.
- `UnitPrice()` is unchanged: the base option price is already in `Price`, and
  the base option prints **no** sub-line (only normal `+delta` options do).

Result:
```
Americano (Iced)        80
  + Extra shot          15      <- only normal delta options break down
```

## POS (`mulan-agent/templates/pos/index.html`)

- **Menu card**: for a base-option menu show the lowest base price as `฿50+`
  (computed from `menu.base_options`), since there is no single fixed price.
- **Modal** (`openOptionsModal`): when `menu.base_options.length > 0`, render a
  required single-select **Base Option** radio section *first* (each row shows
  its absolute price), above the normal option groups. The Add button is blocked
  until a base option is selected. A menu with base options always opens the
  modal even if it has no other groups.
- **Cart** (`addLine` / `unitPrice`): the line carries
  `base_option_id`, `base_option_name`, and `line.price = chosenBase.price`.
  `unitPrice` still adds the normal option deltas on top. The cart row shows
  `Americano (Iced)`.
- **Checkout payload**: each item line gains `base_option_id`.
- **Held orders**: the held payload is opaque POS-owned JSON, so
  `base_option_id`/`base_option_name` ride along in the line objects and restore
  on resume with no backend change.

## Manager (`templates/manager/items.html`)

- New **Base Option** editor section in the item dialog: a single list of
  `(name, price-THB)` rows with add/remove. An empty list means the menu has no
  base option. Because it is one list, the "max one set" rule is structural.
- The menu **price field is greyed out** when ≥1 base option row exists (its
  value is ignored at checkout).
- `saveMenu()` issues `PUT /api/menus/{id}/base-options` with the collected rows,
  alongside the existing `PUT /api/menus/{id}/option-groups` call.

## Converter — `cmd/convert-base-option/main.go`

One-off migration that turns existing delta-based `Serve` groups into base
options.

**Connection:** reads `PSQL_URL` from `.env` via viper (same as `main.go`);
optional `--db` flag to override the DSN.

**Flags:**
- `--source-name string` (default `Serve`) — option group name to convert
  (matched case-insensitively).
- `--apply` — actually write. Without it, dry-run (print plan, write nothing).

**Algorithm:**
1. Find every `menu_option_groups` link whose group `name ILIKE source-name`.
   For each `(menu, group)`:
   - Load `menu.price`, and the group's `options(name, price_delta, sort_order)`.
   - **Idempotency:** if the menu already has rows in `menu_base_options`, skip
     it (log "skipped — already converted").
   - Compute base options: for each option →
     `{ name, price = menu.price + price_delta, sort_order }`.
2. **Print the plan** per menu: menu name + price, and each option as
   `Hot  +0  -> 50`, `Iced +30 -> 80`, plus the disposal action
   (`detach link`, and `delete isolated clone` when `group.owner_menu_id == menu.id`).
3. With `--apply`, run **one transaction**:
   - Insert the `menu_base_options` rows.
   - Delete the `menu_option_groups` link (detach).
   - If `group.owner_menu_id == menu.id` (isolated per-menu clone) → delete that
     `option_groups` row (cascades its options). Shared preset
     (`owner_menu_id IS NULL`) is **left intact**, just unlinked.
   - Commit.
4. Print a summary: converted / skipped / isolated-clones-deleted counts.

**Price formula rationale:** `menu.price` is the price of the line with the
zero-delta serve option; `menu.price + price_delta` is exactly what the customer
paid for any given serve, so it is the correct absolute price regardless of
whether the cheapest option's delta is 0.

## Blast radius / reversibility

- **Schema change** is additive: one new table + one nullable column. Existing
  menus (no base options) checkout/print exactly as today.
- **Converter** mutates existing menu setup. Mitigations: dry-run is the default
  and shows the exact plan; `--apply` runs in a single transaction; deleting
  isolated clones is the only unrecoverable step, gated behind dry-run review.
  Recommend a DB backup before `--apply`.
- **Rollback** of the feature: drop `menu_base_options` + the `base_option_name`
  column. No existing-order data is rewritten by the schema change itself.

## Out of scope (YAGNI)

- Per-menu custom label for the base option set (always labelled "Base Option").
- Redeeming/reporting changes — base option price flows through existing gross /
  discount / VAT / points paths unchanged.
- Multiple base option sets per menu.
