# Base Option Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-menu "Base Option" — a set of named, absolute-priced variants (Hot=50, Iced=80) where picking one sets the line's base price; print `Americano (Iced)  80` with no price breakdown; plus a one-off converter that migrates existing delta-based "Serve" groups.

**Architecture:** A dedicated `menu_base_options` table (per-menu, absolute satang prices). Checkout uses the chosen base option's price as the line base instead of `menus.price`, snapshotting the variant name onto `order_items.base_option_name`. A new `internal/baseoption` feature (service + http) mirrors the `optiongroup` pattern and exposes `PUT /api/menus/{id}/base-options`. POS shows a required base-option picker; the receipt renders the variant inline. `cmd/convert-base-option` turns `Serve` groups into base options (`price = menu.price + delta`).

**Tech Stack:** Go 1.25, pgx/v5, sqlc, Atlas (schema.hcl), chi/v5, go-money, html/template, Tailwind. Two modules: `mulan` (server) and `mulan-agent` (device).

---

## Testing & DB Safety (read first)

- **`PSQL_URL`** in `.env` points at a **local** database. For realistic testing, clone production data into a local DB and aim `PSQL_URL` at it.
- **`PRODUCTION_PSQL_URL`** is for deploy / migrate-to-prod ONLY. **Never** run `task migrate-dev`, `task sqlcgen` side effects, or `cmd/convert-base-option --apply` against production. Do not modify `PRODUCTION_PSQL_URL`.
- Atlas `migrate-dev` needs a scratch `PSQL_DEV_URL` (an empty DB or `docker://postgres/16/dev`). If `task migrate-dev` errors on a missing dev-url, set `PSQL_DEV_URL` in `.env` before continuing — do not point it at production.
- Schema workflow every time you touch `schema.hcl`: `task migrate-dev` → `task generate-sql-schema` → `task sqlcgen` → `go build ./...`.

**Assuming:** `PSQL_URL` is a writable local DB containing menu/option data. If wrong, the migration and converter steps cannot be verified locally — stop and ask.

---

## File Structure

**Create:**
- `internal/sql/base_options.query.sql` — read queries for `menu_base_options`.
- `internal/sql/base_options.command.sql` — clear/create commands.
- `internal/sql/convert.query.sql` — converter-only join query.
- `internal/baseoption/service/baseoption.go` — base-option service (ForMenus, SetForMenu).
- `internal/baseoption/http/handler.go` — `PUT /api/menus/{id}/base-options`.
- `cmd/convert-base-option/main.go` — one-off Serve→base converter.
- `cmd/convert-base-option/price.go` — pure `basePriceFor` helper.
- `cmd/convert-base-option/price_test.go` — unit test for the price formula.
- `mulan-agent/lib/printer/escpos_test.go` — unit test for `displayName`.

**Modify:**
- `schema.hcl` — new `menu_base_options` table + `order_items.base_option_name` column.
- `internal/sql/orders.command.sql` — `CreateOrderItem` writes `base_option_name`.
- `internal/menu/http/handler.go` (+ `handler_test.go`) — embed `base_options[]` in menu response.
- `internal/order/domain/order.go` — `CheckoutResultItem.BaseOptionName`.
- `internal/order/service/order.go` (+ `order_test.go`) — base option load, validation, pricing, snapshot.
- `internal/order/http/handler.go` — request/response `base_option_id` / `base_option_name`.
- `main.go` — wire base-option service/handler + route.
- `mulan-agent/lib/printer/escpos.go` — `OrderItem.BaseOptionName` + `displayName()`.
- `mulan-agent/main.go` — pass base option through request + into printer.
- `mulan-agent/templates/pos/index.html` — base-option picker, cart label, payload.
- `templates/manager/items.html` — base-option editor section.
- `CLAUDE.md` — document the feature + endpoint.

---

## Task 1: Schema + sqlc plumbing

**Files:**
- Modify: `schema.hcl`
- Create: `internal/sql/base_options.query.sql`, `internal/sql/base_options.command.sql`
- Modify: `internal/sql/orders.command.sql`

- [ ] **Step 1: Add the `menu_base_options` table to `schema.hcl`**

Insert this block immediately after the `table "menus" { ... }` block (around line 553):

```hcl
table "menu_base_options" {
  schema = schema.public

  column "id" {
    type = serial
    null = false
  }
  column "menu_id" {
    type = int
    null = false
  }
  column "name" {
    type = varchar(255)
    null = false
  }
  # Absolute, VAT-inclusive satang price. Picking this base option sets the
  # whole line's base price (it does NOT add to menus.price).
  column "price" {
    type = bigint
    null = false
  }
  column "sort_order" {
    type    = int
    null    = false
    default = 0
  }

  primary_key {
    columns = [column.id]
  }
  foreign_key "fk_mbo_menu" {
    columns     = [table.menu_base_options.column.menu_id]
    ref_columns = [table.menus.column.id]
    on_delete   = CASCADE
  }
  index "menu_base_options_menu_sort" {
    columns = [column.menu_id, column.sort_order]
  }
}
```

- [ ] **Step 2: Add the `base_option_name` snapshot column to `order_items`**

In `schema.hcl`, inside `table "order_items"`, add this column block immediately after the `qty` column (after line 329, before the `primary_key` block). Appending last keeps the physical column order matching a fresh `CREATE`, so sqlc reuses the `OrderItem` model:

```hcl
  # Snapshot of the chosen base option's name (e.g. "Iced"). NULL when the
  # menu has no base options. The chosen base price is stored in `price`.
  column "base_option_name" {
    type = varchar(255)
    null = true
  }
```

- [ ] **Step 3: Write `internal/sql/base_options.query.sql`**

```sql
-- name: ListBaseOptionsByMenuIDs :many
SELECT id, menu_id, name, price, sort_order
FROM menu_base_options
WHERE menu_id = ANY(@menu_ids::int[])
ORDER BY menu_id, sort_order, id;

-- name: ListMenuBaseOptions :many
SELECT id, menu_id, name, price, sort_order
FROM menu_base_options
WHERE menu_id = $1
ORDER BY sort_order, id;
```

- [ ] **Step 4: Write `internal/sql/base_options.command.sql`**

```sql
-- name: ClearMenuBaseOptions :exec
DELETE FROM menu_base_options WHERE menu_id = $1;

-- name: CreateMenuBaseOption :one
INSERT INTO menu_base_options (menu_id, name, price, sort_order)
VALUES ($1, $2, $3, $4)
RETURNING id, menu_id, name, price, sort_order;
```

- [ ] **Step 5: Add the converter join query `internal/sql/convert.query.sql`**

```sql
-- name: ListMenuLinksByGroupName :many
-- For the Serve->base-option converter: every (menu, group) link whose group
-- name matches case-insensitively, with the menu price + group owner needed
-- to convert deltas to absolute prices and decide isolated-clone disposal.
SELECT mog.menu_id        AS menu_id,
       og.id              AS group_id,
       og.owner_menu_id   AS owner_menu_id,
       m.price            AS menu_price
FROM menu_option_groups mog
JOIN option_groups og ON og.id = mog.option_group_id
JOIN menus m          ON m.id  = mog.menu_id
WHERE lower(og.name) = lower(@name::text)
ORDER BY mog.menu_id;
```

- [ ] **Step 6: Update `CreateOrderItem` in `internal/sql/orders.command.sql`**

Replace the existing `CreateOrderItem` block (lines 6-9) with:

```sql
-- name: CreateOrderItem :one
INSERT INTO order_items (order_id, menu_id, name, price, qty, base_option_name)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;
```

- [ ] **Step 7: Apply schema to the LOCAL DB and regenerate**

Run (against `PSQL_URL` only — never production):

```bash
task migrate-dev
task generate-sql-schema
task sqlcgen
```

Expected: `migrate-dev` reports the new table + column applied; `sqlcgen` completes with no errors. If `migrate-dev` complains about a missing `--dev-url`, set `PSQL_DEV_URL` in `.env` (see Testing & DB Safety) and re-run.

- [ ] **Step 8: Verify generated code**

Run:

```bash
grep -n "MenuBaseOption\|BaseOptionName\|ListMenuLinksByGroupName" sqlc/models.go sqlc/*.sql.go | head
go build ./...
```

Expected: a `MenuBaseOption` struct exists in `sqlc/models.go`; `CreateOrderItemParams` now has a `BaseOptionName pgtype.Text` field; `ListMenuLinksByGroupNameRow` exists; `go build ./...` passes (existing `CreateOrderItem` call sites omit the new field, defaulting to NULL — still compiles).

- [ ] **Step 9: Commit**

```bash
git add schema.hcl schema.sql internal/sql sqlc
git commit -m "feat(sql): add menu_base_options table + order_items.base_option_name"
```

---

## Task 2: Base-option service + HTTP + menu response embed

**Files:**
- Create: `internal/baseoption/service/baseoption.go`
- Create: `internal/baseoption/http/handler.go`
- Modify: `internal/menu/http/handler.go`
- Test: `internal/menu/http/handler_test.go`
- Modify: `main.go`

- [ ] **Step 1: Write the base-option service `internal/baseoption/service/baseoption.go`**

```go
package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/sqlc"
)

type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewService(pool *pgxpool.Pool, q *sqlc.Queries) *Service {
	return &Service{pool: pool, q: q}
}

// BaseOption is one named, absolute-priced variant of a menu.
type BaseOption struct {
	ID    int32
	Name  string
	Price int64 // satang, absolute
}

// Spec is one base option in a replace-all request.
type Spec struct {
	Name  string
	Price int64 // satang
}

// ForMenus loads the base options attached to each of the given menus.
func (s *Service) ForMenus(ctx context.Context, menuIDs []int32) (map[int32][]BaseOption, error) {
	out := make(map[int32][]BaseOption, len(menuIDs))
	if len(menuIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.ListBaseOptionsByMenuIDs(ctx, menuIDs)
	if err != nil {
		return nil, fmt.Errorf("list base options: %w", err)
	}
	for _, r := range rows {
		out[r.MenuID] = append(out[r.MenuID], BaseOption{ID: r.ID, Name: r.Name, Price: r.Price})
	}
	return out, nil
}

// SetForMenu replaces a menu's base options in one transaction. An empty
// specs slice removes all base options for the menu.
func (s *Service) SetForMenu(ctx context.Context, menuID int32, specs []Spec) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	if err := q.ClearMenuBaseOptions(ctx, menuID); err != nil {
		return fmt.Errorf("clear base options: %w", err)
	}
	for i, sp := range specs {
		if _, err := q.CreateMenuBaseOption(ctx, sqlc.CreateMenuBaseOptionParams{
			MenuID:    menuID,
			Name:      sp.Name,
			Price:     sp.Price,
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("create base option: %w", err)
		}
	}
	return tx.Commit(ctx)
}
```

- [ ] **Step 2: Write the base-option HTTP handler `internal/baseoption/http/handler.go`**

```go
package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"mulan/internal/baseoption/service"
	"mulan/internal/httpx"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

type baseOptionInput struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"` // THB
}

type setBaseOptionsRequest struct {
	BaseOptions []baseOptionInput `json:"base_options"`
}

// satangFromTHB converts a client THB amount to integer satang with bank-style
// rounding so 0.07 doesn't truncate.
func satangFromTHB(thb float64) int64 {
	return int64(math.Round(thb * 100))
}

// SetMenuBaseOptions replaces the menu's base options. PUT /api/menus/{id}/base-options
func (h *Handler) SetMenuBaseOptions(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req setBaseOptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	specs := make([]service.Spec, 0, len(req.BaseOptions))
	for _, b := range req.BaseOptions {
		if b.Name == "" {
			continue
		}
		if b.Price < 0 {
			response.Error(w, r, http.StatusBadRequest, "price must be >= 0", errors.New("negative price"))
			return
		}
		specs = append(specs, service.Spec{Name: b.Name, Price: satangFromTHB(b.Price)})
	}
	if err := h.svc.SetForMenu(r.Context(), id, specs); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to set base options", err)
		return
	}
	response.NoContent(w, r)
}
```

- [ ] **Step 3: Embed `base_options[]` in the menu response — edit `internal/menu/http/handler.go`**

Add the import (in the import block, with the other internal imports):

```go
	baseoptionservice "mulan/internal/baseoption/service"
```

Add the response type after `menuOptionGroupResponse` (after line 38):

```go
type menuBaseOptionResponse struct {
	ID    int32   `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}
```

Add `BaseOptions` to `menuResponse` (after the `OptionGroups` field, line 47):

```go
	BaseOptions  []menuBaseOptionResponse  `json:"base_options"`
```

In `toMenuResponse`, default the slice so JSON is `[]` not `null` — add to the returned struct literal (after `OptionGroups: []menuOptionGroupResponse{},`):

```go
		BaseOptions:  []menuBaseOptionResponse{},
```

Add a converter after `toMenuOptionGroups` (after line 94):

```go
func toMenuBaseOptions(opts []baseoptionservice.BaseOption) []menuBaseOptionResponse {
	out := make([]menuBaseOptionResponse, len(opts))
	for i, o := range opts {
		out[i] = menuBaseOptionResponse{
			ID:    o.ID,
			Name:  o.Name,
			Price: money.New(o.Price, money.THB).AsMajorUnits(),
		}
	}
	return out
}
```

Add the service to the handler struct (in `MenuHandler`, after `optionsvc`):

```go
	baseoptionsvc *baseoptionservice.Service
```

Update the constructor signature + body:

```go
func NewMenuHandler(s *service.MenuService, optionsvc *optiongroupservice.Service, baseoptionsvc *baseoptionservice.Service, h *hub.Hub) *MenuHandler {
	return &MenuHandler{svc: s, optionsvc: optionsvc, baseoptionsvc: baseoptionsvc, hub: h}
}
```

In `List`, after the `groupsByMenu` block (after line 118), load base options and populate each response:

```go
	baseByMenu, err := h.baseoptionsvc.ForMenus(r.Context(), ids)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list menu base options", err)
		return
	}
```

Then inside the `for i, m := range menus` loop, after `mr.OptionGroups = ...` (line 122):

```go
		mr.BaseOptions = toMenuBaseOptions(baseByMenu[m.ID])
```

- [ ] **Step 4: Write the failing test for the base-option converter in `internal/menu/http/handler_test.go`**

Add at the end of the file:

```go
func TestToMenuBaseOptions(t *testing.T) {
	in := []baseoptionservice.BaseOption{
		{ID: 1, Name: "Hot", Price: 5000},
		{ID: 2, Name: "Iced", Price: 8000},
	}
	got := toMenuBaseOptions(in)
	if len(got) != 2 {
		t.Fatalf("len = %d want 2", len(got))
	}
	if got[0].Name != "Hot" || got[0].Price != 50 {
		t.Errorf("got[0] = %+v want {1 Hot 50}", got[0])
	}
	if got[1].Name != "Iced" || got[1].Price != 80 {
		t.Errorf("got[1] = %+v want {2 Iced 80}", got[1])
	}
}
```

Add the import to the test file:

```go
	baseoptionservice "mulan/internal/baseoption/service"
```

- [ ] **Step 5: Run the test — expect FAIL to compile (handler not yet wired)**

Run: `go test ./internal/menu/http/ -run TestToMenuBaseOptions -v`
Expected: compile error or FAIL until Step 3's converter exists. (If Step 3 is already saved, it should PASS — that's fine.)

- [ ] **Step 6: Wire the service + route in `main.go`**

Add imports (with the other internal imports):

```go
	baseoptionhttp "mulan/internal/baseoption/http"
	baseoptionservice "mulan/internal/baseoption/service"
```

After the `optionGroupSvc` / `optionGroupHandler` lines (after line 63), construct the base-option service + handler:

```go
	baseOptionSvc := baseoptionservice.NewService(pool, queries)
	baseOptionHandler := baseoptionhttp.NewHandler(baseOptionSvc)
```

Update the menu handler construction (line 66) to pass the new service:

```go
	menuHandler := menuhttp.NewMenuHandler(menuSvc, optionGroupSvc, baseOptionSvc, eventHub)
```

Register the route inside the `/menus` route block (after the `option-groups` PUT, line 142):

```go
			r.Put("/{id}/base-options", baseOptionHandler.SetMenuBaseOptions)
```

- [ ] **Step 7: Build + run the test — expect PASS**

Run:

```bash
go build ./...
go test ./internal/menu/http/ -run TestToMenuBaseOptions -v
```

Expected: build passes; test PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/baseoption internal/menu/http main.go
git commit -m "feat(baseoption): service, PUT /api/menus/{id}/base-options, menu embed"
```

---

## Task 3: Checkout pricing, validation, snapshot

**Files:**
- Modify: `internal/order/domain/order.go`
- Modify: `internal/order/service/order.go`
- Test: `internal/order/service/order_test.go`
- Modify: `internal/order/http/handler.go`

- [ ] **Step 1: Add `BaseOptionName` to the domain result item — `internal/order/domain/order.go`**

In `CheckoutResultItem` (after the `Options []SelectedOption` field, line 21), add:

```go
	BaseOptionName string // chosen base option name, empty when none
```

- [ ] **Step 2: Write the failing unit test for base resolution — `internal/order/service/order_test.go`**

Add the import `"mulan/sqlc"` is already present. Append:

```go
func TestResolveLineBase(t *testing.T) {
	baseOpts := []sqlc.MenuBaseOption{
		{ID: 10, MenuID: 1, Name: "Hot", Price: 5000},
		{ID: 11, MenuID: 1, Name: "Iced", Price: 8000},
	}
	tests := []struct {
		name       string
		menuPrice  int64
		opts       []sqlc.MenuBaseOption
		baseID     int32
		wantPrice  int64
		wantName   string
		wantErr    error
	}{
		{"no base opts, none chosen", 4500, nil, 0, 4500, "", nil},
		{"no base opts, id supplied -> reject", 4500, nil, 99, 0, "", ErrInvalidBaseOption},
		{"has base opts, valid pick", 0, baseOpts, 11, 8000, "Iced", nil},
		{"has base opts, none chosen -> reject", 0, baseOpts, 0, 0, "", ErrMissingBaseOption},
		{"has base opts, unknown id -> reject", 0, baseOpts, 999, 0, "", ErrInvalidBaseOption},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			price, name, err := resolveLineBase(tc.menuPrice, tc.opts, tc.baseID)
			if err != tc.wantErr {
				t.Fatalf("err = %v want %v", err, tc.wantErr)
			}
			if err == nil && (price != tc.wantPrice || name != tc.wantName) {
				t.Errorf("got (%d,%q) want (%d,%q)", price, name, tc.wantPrice, tc.wantName)
			}
		})
	}
}
```

- [ ] **Step 3: Run the test — expect FAIL (function + errors undefined)**

Run: `go test ./internal/order/service/ -run TestResolveLineBase -v`
Expected: compile error — `undefined: resolveLineBase`, `ErrMissingBaseOption`, `ErrInvalidBaseOption`.

- [ ] **Step 4: Add sentinel errors + the pure helper in `internal/order/service/order.go`**

In the `var ( ... )` sentinel block (after `ErrMissingRequired`, line 38), add:

```go
	ErrMissingBaseOption = errors.New("missing base option")
	ErrInvalidBaseOption = errors.New("invalid base option for menu")
```

Add the helper (place it next to `resolveLineOptions`, after line 520):

```go
// resolveLineBase returns the per-unit base price and snapshot name for a line.
// When the menu has base options exactly one valid baseOptionID is required and
// its absolute price becomes the line base. When the menu has none, baseOptionID
// must be zero and the base is the menu's own price.
func resolveLineBase(menuPrice int64, baseOpts []sqlc.MenuBaseOption, baseOptionID int32) (int64, string, error) {
	if len(baseOpts) == 0 {
		if baseOptionID != 0 {
			return 0, "", ErrInvalidBaseOption
		}
		return menuPrice, "", nil
	}
	if baseOptionID == 0 {
		return 0, "", ErrMissingBaseOption
	}
	for _, b := range baseOpts {
		if b.ID == baseOptionID {
			return b.Price, b.Name, nil
		}
	}
	return 0, "", ErrInvalidBaseOption
}

// textOrNull wraps a snapshot string into pgtype.Text, mapping "" to SQL NULL.
func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
```

- [ ] **Step 5: Run the test — expect PASS**

Run: `go test ./internal/order/service/ -run TestResolveLineBase -v`
Expected: PASS.

- [ ] **Step 6: Add `BaseOptionID` to the checkout input + load base options**

In `CheckoutItemInput` (after `OptionIDs []int32`, line 79), add:

```go
	BaseOptionID int32   // chosen base option (0 = none)
```

Add a loader next to `loadOptions` (after line 498):

```go
// loadBaseOptions returns the base options attached to each menu being ordered,
// keyed by menu id. Menus with no base options simply have no entry.
func loadBaseOptions(ctx context.Context, q *sqlc.Queries, menus map[int32]sqlc.Menu) (map[int32][]sqlc.MenuBaseOption, error) {
	ids := make([]int32, 0, len(menus))
	for id := range menus {
		ids = append(ids, id)
	}
	out := make(map[int32][]sqlc.MenuBaseOption, len(menus))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListBaseOptionsByMenuIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load base options: %w", err)
	}
	for _, b := range rows {
		out[b.MenuID] = append(out[b.MenuID], b)
	}
	return out, nil
}
```

- [ ] **Step 7: Use base options in `Checkout`**

In `Checkout`, after the `optByID, err := loadOptions(...)` block (after line 135), add:

```go
	baseOptsByMenu, err := loadBaseOptions(ctx, q, menuByID)
	if err != nil {
		return nil, err
	}
```

Inside the `for _, in := range items` loop, replace the pricing + insert region. The current block is (lines 155-173):

```go
		opts, deltaSum, err := resolveLineOptions(in, optByID, allowedGroupByMenu[m.ID])
		if err != nil {
			return nil, err
		}

		unitPrice := m.Price + deltaSum
		lineTotal := unitPrice * int64(in.Qty)
		subtotalSatang += lineTotal

		itemID, err := q.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID: order.ID,
			MenuID:  pgtype.Int4{Int32: m.ID, Valid: true},
			Name:    m.Name,
			Price:   m.Price,
			Qty:     in.Qty,
		})
```

Replace it with:

```go
		opts, deltaSum, err := resolveLineOptions(in, optByID, allowedGroupByMenu[m.ID])
		if err != nil {
			return nil, err
		}

		basePrice, baseName, err := resolveLineBase(m.Price, baseOptsByMenu[m.ID], in.BaseOptionID)
		if err != nil {
			return nil, err
		}

		unitPrice := basePrice + deltaSum
		lineTotal := unitPrice * int64(in.Qty)
		subtotalSatang += lineTotal

		itemID, err := q.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID:        order.ID,
			MenuID:         pgtype.Int4{Int32: m.ID, Valid: true},
			Name:           m.Name,
			Price:          basePrice,
			Qty:            in.Qty,
			BaseOptionName: textOrNull(baseName),
		})
```

And update the `resultItems = append(...)` (lines 219-224) to carry the name:

```go
		resultItems = append(resultItems, domain.CheckoutResultItem{
			Name:           m.Name,
			Price:          basePrice,
			Qty:            in.Qty,
			Options:        opts,
			BaseOptionName: baseName,
		})
```

- [ ] **Step 8: Map the request + response in `internal/order/http/handler.go`**

Add `BaseOptionID` to `checkoutItemRequest` (after `MenuID`, line 155):

```go
	BaseOptionID int32   `json:"base_option_id"`
```

In the `items[i] = service.CheckoutItemInput{...}` mapping (line 222), add:

```go
			BaseOptionID: it.BaseOptionID,
```

Add `BaseOptionName` to `checkoutItemResponse` (after `Options`, line 177):

```go
	BaseOptionName string                   `json:"base_option_name,omitempty"`
```

In the `respItems[i] = checkoutItemResponse{...}` build (line 249), add:

```go
			BaseOptionName: it.BaseOptionName,
```

Add the new errors to `classifyCheckoutError` (after the `ErrInvalidOption` case, line 312):

```go
	case errors.Is(err, service.ErrMissingBaseOption):
		return http.StatusBadRequest, "base option required"
	case errors.Is(err, service.ErrInvalidBaseOption):
		return http.StatusBadRequest, "invalid base option for menu"
```

- [ ] **Step 9: Build + test**

Run:

```bash
go build ./...
go test ./internal/order/...
```

Expected: build passes; all order tests PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/order
git commit -m "feat(order): base option pricing, validation + snapshot at checkout"
```

---

## Task 4: Receipt + agent rendering

**Files:**
- Modify: `mulan-agent/lib/printer/escpos.go`
- Test: `mulan-agent/lib/printer/escpos_test.go`
- Modify: `mulan-agent/main.go`

- [ ] **Step 1: Add `BaseOptionName` + `displayName()` to `mulan-agent/lib/printer/escpos.go`**

In the `OrderItem` struct (after `Options []OrderItemOption`, line 92), add:

```go
	BaseOptionName string  // chosen base option, e.g. "Iced" (empty = none)
```

Add a method right after the `UnitPrice()` method (after line 101):

```go
// displayName returns the line label with the chosen base option in
// parentheses, e.g. "Americano (Iced)". The base option's price is already
// baked into Price, so it is never printed as a separate sub-line.
func (it OrderItem) displayName() string {
	if it.BaseOptionName != "" {
		return it.Name + " (" + it.BaseOptionName + ")"
	}
	return it.Name
}
```

- [ ] **Step 2: Use `displayName()` in both prints**

In `PrintOrderBill`, the item loop (line 166), change:

```go
		writeln(fmt.Sprintf("%d x %s", it.Qty, it.Name))
```

to:

```go
		writeln(fmt.Sprintf("%d x %s", it.Qty, it.displayName()))
```

In `PrintReceipt`, the item loop (line 274), change:

```go
		writeRow(it.Name, fmt.Sprintf("%d   %9.2f", it.Qty, amount))
```

to:

```go
		writeRow(it.displayName(), fmt.Sprintf("%d   %9.2f", it.Qty, amount))
```

- [ ] **Step 3: Write the failing test `mulan-agent/lib/printer/escpos_test.go`**

```go
package printer

import "testing"

func TestOrderItemDisplayName(t *testing.T) {
	tests := []struct {
		name string
		item OrderItem
		want string
	}{
		{"with base option", OrderItem{Name: "Americano", BaseOptionName: "Iced"}, "Americano (Iced)"},
		{"no base option", OrderItem{Name: "Croissant"}, "Croissant"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.displayName(); got != tc.want {
				t.Errorf("displayName() = %q want %q", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 4: Run the test (mulan-agent module)**

Run: `cd mulan-agent && go test ./lib/printer/ -run TestOrderItemDisplayName -v && cd ..`
Expected: PASS.

- [ ] **Step 5: Pass the base option through the agent — `mulan-agent/main.go`**

Add `BaseOptionID` to `checkoutRequestItem` (after `Qty`, line 300) so the POS value forwards to mulan untouched:

```go
	BaseOptionID int32   `json:"base_option_id"`
```

Add `BaseOptionName` to `checkoutItem` (after `Options`, line 325):

```go
	BaseOptionName string           `json:"base_option_name"`
```

In the printer mapping loop (line 380), add the field to the `printer.OrderItem{...}` literal:

```go
				items[i] = printer.OrderItem{
					Name:           it.Name,
					Qty:            int(it.Qty),
					Price:          it.Price,
					Options:        opts,
					BaseOptionName: it.BaseOptionName,
				}
```

- [ ] **Step 6: Build the agent module**

Run: `cd mulan-agent && go build ./... && go vet ./... && cd ..`
Expected: builds clean.

- [ ] **Step 7: Commit**

```bash
git add mulan-agent/lib/printer mulan-agent/main.go
git commit -m "feat(agent): print 'Item (BaseOption)' inline, pass base_option_id through"
```

---

## Task 5: POS base-option picker

**Files:**
- Modify: `mulan-agent/templates/pos/index.html`

All line numbers below are pre-edit; re-grep if they drift.

- [ ] **Step 1: Open the modal when a menu has base options — `addToOrder` (line 1615)**

Replace the function with:

```js
async function addToOrder(menuId) {
    const menu = menus.find(m => m.id === menuId);
    if (!menu) return;
    const hasBase = (menu.base_options || []).length > 0;
    const hasOpts = (menu.option_groups || []).length > 0;
    if (hasBase || hasOpts) {
        openOptionsModal(menu);
        return;
    }
    await addLine(menu, [], null);
}
```

- [ ] **Step 2: Render the base-option section first — `openOptionsModal` (line 1778)**

Replace the `body.innerHTML = (menu.option_groups || []).map(...)` assignment (lines 1782-1802) with a version that prepends the base section:

```js
    const baseHtml = (menu.base_options || []).length ? `
        <div class="opt-group" data-base="1">
            <div style="display:flex;align-items:center;justify-content:space-between;gap:8px">
                <h4>Base Option</h4>
                <span class="req-tag required">Required</span>
            </div>
            <div class="opt-list">
                ${menu.base_options.map(b => `
                    <label class="opt-row">
                        <input type="radio" name="base-opt" value="${b.id}" data-name="${esc(b.name)}" data-price="${b.price}" onchange="optRowSync(this)">
                        <span class="o-name">${esc(b.name)}</span>
                        <span class="o-price">${fmt(b.price)}</span>
                    </label>
                `).join('')}
            </div>
        </div>` : '';
    const groupsHtml = (menu.option_groups || []).map(g => {
        const isMulti = g.selection_mode === 'multi';
        const required = g.selection_mode === 'single_required';
        const inputType = isMulti ? 'checkbox' : 'radio';
        return `
            <div class="opt-group" data-group-id="${g.id}" data-mode="${esc(g.selection_mode)}">
                <div style="display:flex;align-items:center;justify-content:space-between;gap:8px">
                    <h4>${esc(g.name)}</h4>
                    <span class="req-tag ${required ? 'required' : ''}">${required ? 'Required' : (isMulti ? 'Pick any' : 'Optional')}</span>
                </div>
                <div class="opt-list">
                    ${(g.options || []).map(o => `
                        <label class="opt-row">
                            <input type="${inputType}" name="grp-${g.id}" value="${o.id}" data-name="${esc(o.name)}" data-delta="${o.price_delta}" onchange="optRowSync(this)">
                            <span class="o-name">${esc(o.name)}</span>
                            <span class="o-price">${o.price_delta > 0 ? '+'+fmt(o.price_delta) : (o.price_delta < 0 ? '−'+fmt(Math.abs(o.price_delta)) : '')}</span>
                        </label>
                    `).join('')}
                </div>
            </div>`;
    }).join('');
    body.innerHTML = baseHtml + groupsHtml;
```

- [ ] **Step 3: Enforce + capture the base pick — `confirmOptions` (line 1817)**

Replace the function with:

```js
function confirmOptions() {
    if (!pendingMenu) return;
    let baseOption = null;
    if ((pendingMenu.base_options || []).length) {
        const checked = document.querySelector('#options-body [data-base] input:checked');
        if (!checked) { snack('Pick a base option', 'warn'); return; }
        baseOption = {
            id: parseInt(checked.value),
            name: checked.dataset.name,
            price: parseFloat(checked.dataset.price) || 0,
        };
    }
    const groupBlocks = document.querySelectorAll('#options-body [data-group-id]');
    const selected = [];
    for (const block of groupBlocks) {
        const mode = block.dataset.mode;
        const checked = block.querySelectorAll('input:checked');
        if (mode === 'single_required' && checked.length === 0) {
            snack('Pick an option for ' + block.querySelector('h4').textContent, 'warn');
            return;
        }
        for (const inp of checked) {
            selected.push({
                id: parseInt(inp.value),
                name: inp.dataset.name,
                price_delta: parseFloat(inp.dataset.delta) || 0,
            });
        }
    }
    const m = pendingMenu;
    closeOptionsModal();
    addLine(m, selected, baseOption);
}
```

- [ ] **Step 4: Carry the base option on the cart line — `addLine` (line 1625)**

Replace the function with:

```js
async function addLine(menu, selectedOptions, baseOption) {
    await ensureOrderCode();
    const baseId = baseOption ? baseOption.id : null;
    const key = lineKey(menu.id, selectedOptions) + '|b' + (baseId ?? '');
    const existing = order.find(o => o.key === key);
    if (existing) existing.qty++;
    else order.push({
        key, id: menu.id, name: menu.name,
        price: baseOption ? baseOption.price : menu.price, qty: 1,
        options: selectedOptions,
        base_option_id: baseId,
        base_option_name: baseOption ? baseOption.name : null,
        vfd_name: menu.vfd_name || menu.name,
    });
    renderOrder({ flashKey: key });

    const line = order.find(o => o.key === key);
    fetch('/vfd/item', {
        method: 'POST', headers: {'Content-Type':'application/json'},
        body: JSON.stringify({ name: line.vfd_name, qty: line.qty, total: unitPrice(line) * line.qty }),
    }).catch(() => {});
}
```

- [ ] **Step 5: Verify every `addLine(` caller passes the third arg**

Run: `grep -n "addLine(" mulan-agent/templates/pos/index.html`
Expected callers: `addToOrder` → `addLine(menu, [], null)` (Step 1), `confirmOptions` → `addLine(m, selected, baseOption)` (Step 3). If any other caller exists (e.g. barcode/resume path), add a trailing `, null` so it passes no base option. A missing third arg yields `undefined`, which the function treats as "no base option" — still safe, but make it explicit.

- [ ] **Step 6: Show the variant in the cart line — `renderOrder` (line 1708)**

Replace the line-name div (line 1708):

```js
                <div class="l-name">${esc(o.name)}</div>
```

with:

```js
                <div class="l-name">${esc(o.name)}${o.base_option_name ? ` <span class="mono" style="color:var(--on-surf-var)">(${esc(o.base_option_name)})</span>` : ''}</div>
```

(`unitPrice` needs no change: `o.price` is already the chosen base price.)

- [ ] **Step 7: Send `base_option_id` in the checkout payload — `doCheckout` (line 2157)**

Replace the `items: order.map(...)` block (lines 2157-2162) with:

```js
                items: order.map(o => ({
                    menu_id: o.id, name: o.name, price: o.price, qty: o.qty,
                    base_option_id: o.base_option_id || 0,
                    option_ids: (o.options || []).map(op => op.id),
                    options: o.options || [],
                    discount_ids: (o.discounts || []).map(d => d.id),
                })),
```

- [ ] **Step 8: Show "฿50+" on base-option menu cards — `menuCardHtml` (line 1482)**

Replace the active-card branch (lines 1489-1494) with:

```js
    const hasOpts = (m.option_groups || []).length > 0;
    const bases = m.base_options || [];
    const priceLabel = bases.length
        ? fmt(Math.min(...bases.map(b => b.price))) + '+'
        : fmt(m.price);
    return `<button class="menu-card" data-menu-id="${m.id}" onclick="addToOrder(${m.id})">
        ${(hasOpts || bases.length) ? '<span class="opt-flag"><span class="material-symbols-rounded">tune</span></span>' : ''}
        <div class="name">${esc(m.name)}</div>
        <div class="price">${priceLabel}</div>
    </button>`;
```

- [ ] **Step 9: Manual smoke test in the browser**

Start both servers (mulan + mulan-agent) against the local DB and open `/pos`. With a menu that has base options (create one via the manager in Task 6, or use a converted Serve menu after Task 7):
1. Card shows `฿<lowest>+`.
2. Tapping it opens the modal with a required **Base Option** radio group; **Add** is blocked until a base option is picked.
3. The cart line shows `Americano (Iced)` and the correct price.
4. Checkout prints `Americano (Iced)  80` with no base sub-line.

This is a visual verification — note in the commit what you observed.

- [ ] **Step 10: Commit**

```bash
git add mulan-agent/templates/pos/index.html
git commit -m "feat(pos): required base-option picker, inline cart label, payload + card price"
```

---

## Task 6: Manager base-option editor

**Files:**
- Modify: `templates/manager/items.html`

- [ ] **Step 1: Add the Base Option editor markup to the item dialog**

In `templates/manager/items.html`, insert a new section inside the dialog body between the VFD field block (closes at line 643) and the Option groups block (opens at line 644). Insert before `<div class="col-span-2">` on line 644:

```html
                <div class="col-span-2">
                    <div class="flex items-center justify-between mb-1.5">
                        <label class="m3-label" style="margin:0">Base Option <span style="color:var(--md-sys-color-outline)">· one set; absolute price replaces item price</span></label>
                        <button type="button" class="m3-btn-text" style="height:28px;padding:0 10px;font-size:13px" onclick="addBaseOptionRow()">
                            <span class="material-symbols-rounded" style="font-size:16px">add</span>Add base option
                        </button>
                    </div>
                    <div id="base-options-rows" class="space-y-1.5"></div>
                </div>
```

- [ ] **Step 2: Add the base-option editor JS**

In the `<script>` section, immediately before `function openMenuModal(id)` (line 1065), add:

```js
// ── Base option editor (per-item) ─────────────────────────────────
// baseOptionState holds {name, price(THB)} rows. Empty = menu has no base
// option. When non-empty the menu price field is ignored at checkout.
let baseOptionState = [];

function buildBaseOptionState(m) {
    baseOptionState = ((m && m.base_options) || []).map(b => ({ name: b.name, price: b.price }));
}

function baseOptRowHtml(i, b) {
    return `
        <div class="base-opt-row grid grid-cols-5 gap-2 items-center" data-bidx="${i}">
            <input type="text" class="m3-input m3-input-sm col-span-3 base-opt-name" value="${esc(b.name)}" placeholder="e.g. Iced">
            <input type="number" step="0.01" min="0" class="m3-input m3-input-sm col-span-1 base-opt-price" value="${b.price}" placeholder="0.00">
            <button type="button" class="m3-icon-btn col-span-1 justify-self-end" onclick="removeBaseOption(${i})" title="Remove">
                <span class="material-symbols-rounded" style="font-size:18px">close</span>
            </button>
        </div>`;
}

function renderBaseOptions() {
    const el = document.getElementById('base-options-rows');
    el.innerHTML = baseOptionState.length
        ? baseOptionState.map((b, i) => baseOptRowHtml(i, b)).join('')
        : `<p class="text-xs" style="color:var(--md-sys-color-on-surface-variant)">No base option. The item uses its own price above.</p>`;
    syncMenuPriceDisabled();
}

// When the menu has base options, its own price is ignored at checkout, so
// grey the field out to avoid confusion.
function syncMenuPriceDisabled() {
    const price = document.getElementById('menu-price');
    const on = baseOptionState.length > 0;
    price.disabled = on;
    price.style.opacity = on ? '0.5' : '';
    price.title = on ? 'Ignored — base option prices apply' : '';
}

function syncBaseOptionsFromDOM() {
    baseOptionState = Array.from(document.querySelectorAll('#base-options-rows .base-opt-row')).map(r => ({
        name: r.querySelector('.base-opt-name').value,
        price: parseFloat(r.querySelector('.base-opt-price').value) || 0,
    }));
}

function addBaseOptionRow() {
    syncBaseOptionsFromDOM();
    baseOptionState.push({ name: '', price: 0 });
    renderBaseOptions();
    const rows = document.querySelectorAll('#base-options-rows .base-opt-row');
    if (rows.length) rows[rows.length - 1].querySelector('.base-opt-name').focus();
}

function removeBaseOption(i) {
    syncBaseOptionsFromDOM();
    baseOptionState.splice(i, 1);
    renderBaseOptions();
}

function collectBaseOptionsPayload() {
    syncBaseOptionsFromDOM();
    return baseOptionState
        .map(b => ({ name: (b.name || '').trim(), price: b.price || 0 }))
        .filter(b => b.name);
}
```

- [ ] **Step 3: Initialise the editor when the dialog opens — `openMenuModal` (line 1074)**

After `buildMenuGroupState(m);` (line 1074), add:

```js
    buildBaseOptionState(m);
    renderBaseOptions();
```

- [ ] **Step 4: Persist base options on save — `saveMenu` (line 1386)**

In `saveMenu`, replace the `if (menuId) { ... }` block that does the option-groups PUT (lines 1386-1391) with one that also PUTs base options:

```js
    if (menuId) {
        await fetch(`/api/menus/${menuId}/option-groups`, {
            method: 'PUT', headers: {'Content-Type':'application/json'},
            body: JSON.stringify({ groups }),
        });
        await fetch(`/api/menus/${menuId}/base-options`, {
            method: 'PUT', headers: {'Content-Type':'application/json'},
            body: JSON.stringify({ base_options: collectBaseOptionsPayload() }),
        });
    }
```

Also collect base options up front next to `const groups = collectMenuGroupsPayload();` (line 1371) is not required — `collectBaseOptionsPayload()` is called inline above. But guard the price check: `saveMenu` bails when `isNaN(price)` (line 1369). When base options exist the price field is disabled and may be empty. Replace line 1369:

```js
    if (!name || isNaN(price)) return;
```

with:

```js
    const hasBase = baseOptionState.length > 0;
    if (!name || (!hasBase && isNaN(price))) return;
```

And make the body tolerate a NaN price when base options exist — replace line 1370:

```js
    const body = { name, price, category_id: catVal ? parseInt(catVal) : null, vfd_name: vfd };
```

with:

```js
    const body = { name, price: isNaN(price) ? 0 : price, category_id: catVal ? parseInt(catVal) : null, vfd_name: vfd };
```

- [ ] **Step 5: Manual smoke test**

Open `/manager/items`, edit an item:
1. Add two base options (Hot=50, Iced=80). The price field greys out.
2. Save, reopen — the two rows persist.
3. Remove both, save, reopen — price field is editable again.
4. Confirm `GET /api/menus` returns `base_options: [{name:"Hot",price:50}, ...]` for that item:

```bash
curl -s localhost:8085/api/menus | python3 -m json.tool | grep -A6 base_options | head
```

(Use your actual mulan port from `.env`.)

- [ ] **Step 6: Commit**

```bash
git add templates/manager/items.html
git commit -m "feat(manager): base-option editor in item dialog + base-options PUT"
```

---

## Task 7: Serve → Base Option converter

**Files:**
- Create: `cmd/convert-base-option/price.go`
- Create: `cmd/convert-base-option/price_test.go`
- Create: `cmd/convert-base-option/main.go`

- [ ] **Step 1: Write the pure price helper `cmd/convert-base-option/price.go`**

```go
package main

// basePriceFor converts a legacy delta-based option into the absolute base
// price: the menu's price is the price of the zero-delta serve, so the menu
// price plus the option's delta is exactly what the customer paid for that
// serve. Both values are satang.
func basePriceFor(menuPrice, delta int64) int64 {
	return menuPrice + delta
}
```

- [ ] **Step 2: Write the failing test `cmd/convert-base-option/price_test.go`**

```go
package main

import "testing"

func TestBasePriceFor(t *testing.T) {
	tests := []struct {
		menuPrice, delta, want int64
	}{
		{5000, 0, 5000},     // Hot: 50฿ + 0
		{5000, 3000, 8000},  // Iced: 50฿ + 30
		{5000, -500, 4500},  // discounted serve
	}
	for _, tc := range tests {
		if got := basePriceFor(tc.menuPrice, tc.delta); got != tc.want {
			t.Errorf("basePriceFor(%d,%d) = %d want %d", tc.menuPrice, tc.delta, got, tc.want)
		}
	}
}
```

- [ ] **Step 3: Run the test — expect PASS**

Run: `go test ./cmd/convert-base-option/ -run TestBasePriceFor -v`
Expected: PASS.

- [ ] **Step 4: Write the converter `cmd/convert-base-option/main.go`**

```go
// Command convert-base-option migrates legacy delta-based "Serve" option
// groups into the absolute-priced menu_base_options model.
//
//	go run ./cmd/convert-base-option              # dry-run, prints the plan
//	go run ./cmd/convert-base-option --apply      # commit in a transaction
//	go run ./cmd/convert-base-option --source-name Size
//
// Runs against PSQL_URL (the local DB). Never point --db or PSQL_URL at
// production; production is migrated at deploy time.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/internal/config"
	"mulan/sqlc"
)

func main() {
	apply := flag.Bool("apply", false, "commit changes (default: dry-run)")
	sourceName := flag.String("source-name", "Serve", "option group name to convert")
	dbURL := flag.String("db", "", "override PSQL_URL (local DB only)")
	flag.Parse()

	dsn := *dbURL
	if dsn == "" {
		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		dsn = cfg.PSQLURL
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()
	q := sqlc.New(pool)

	links, err := q.ListMenuLinksByGroupName(ctx, *sourceName)
	if err != nil {
		log.Fatalf("list links: %v", err)
	}
	if len(links) == 0 {
		fmt.Printf("No menus linked to a group named %q. Nothing to do.\n", *sourceName)
		return
	}

	var converted, skipped, clonesDeleted int
	for _, l := range links {
		existing, err := q.ListMenuBaseOptions(ctx, l.MenuID)
		if err != nil {
			log.Fatalf("check existing base options (menu %d): %v", l.MenuID, err)
		}
		if len(existing) > 0 {
			fmt.Printf("menu %d: SKIP — already has %d base option(s)\n", l.MenuID, len(existing))
			skipped++
			continue
		}
		opts, err := q.ListOptionsByGroup(ctx, l.GroupID)
		if err != nil {
			log.Fatalf("list options (group %d): %v", l.GroupID, err)
		}
		isolated := l.OwnerMenuID.Valid && l.OwnerMenuID.Int32 == l.MenuID

		fmt.Printf("menu %d (price %.2f฿) via group %d %q:\n", l.MenuID, float64(l.MenuPrice)/100, l.GroupID, *sourceName)
		for _, o := range opts {
			base := basePriceFor(l.MenuPrice, o.PriceDelta)
			fmt.Printf("    %-16s delta %+d -> base %.2f฿\n", o.Name, o.PriceDelta, float64(base)/100)
		}
		if isolated {
			fmt.Println("    actions: detach link + delete isolated clone")
		} else {
			fmt.Println("    actions: detach link (shared preset kept)")
		}

		if *apply {
			if err := applyOne(ctx, pool, q, l, opts, isolated); err != nil {
				log.Fatalf("apply menu %d: %v", l.MenuID, err)
			}
			if isolated {
				clonesDeleted++
			}
		}
		converted++
	}

	mode := "DRY-RUN (no writes)"
	if *apply {
		mode = "APPLIED"
	}
	fmt.Printf("\n%s — %d converted, %d skipped, %d isolated clones deleted\n", mode, converted, skipped, clonesDeleted)
	if !*apply && converted > 0 {
		fmt.Println("Re-run with --apply to commit (against the local DB only).")
	}
}

// applyOne converts a single (menu, group) link inside one transaction:
// insert base options, detach the menu link, and delete the group only when
// it is an isolated per-menu clone (shared presets are left intact). The pool
// is needed for BeginTx; q.WithTx binds the queries to the transaction.
func applyOne(ctx context.Context, pool *pgxpool.Pool, q *sqlc.Queries, l sqlc.ListMenuLinksByGroupNameRow, opts []sqlc.Option, isolated bool) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := q.WithTx(tx)

	for i, o := range opts {
		if _, err := qtx.CreateMenuBaseOption(ctx, sqlc.CreateMenuBaseOptionParams{
			MenuID:    l.MenuID,
			Name:      o.Name,
			Price:     basePriceFor(l.MenuPrice, o.PriceDelta),
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("create base option: %w", err)
		}
	}
	if err := qtx.DetachMenuOptionGroup(ctx, sqlc.DetachMenuOptionGroupParams{
		MenuID:        l.MenuID,
		OptionGroupID: l.GroupID,
	}); err != nil {
		return fmt.Errorf("detach: %w", err)
	}
	if isolated {
		if err := qtx.DeleteOptionGroup(ctx, l.GroupID); err != nil {
			return fmt.Errorf("delete isolated group: %w", err)
		}
	}
	return tx.Commit(ctx)
}
```

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: builds clean. (`ListOptionsByGroup`, `DetachMenuOptionGroup`, `DeleteOptionGroup` already exist in sqlc; `ListMenuLinksByGroupName`, `ListMenuBaseOptions`, `CreateMenuBaseOption` were generated in Task 1.)

- [ ] **Step 6: Dry-run against the LOCAL DB**

Run (PSQL_URL = local prod-clone only):

```bash
go run ./cmd/convert-base-option
```

Expected: prints a per-menu plan listing each Serve option and its computed absolute price (e.g. `Iced delta +3000 -> base 80.00฿`), ending with `DRY-RUN ... N converted`. **Verify the printed prices match expectations before applying.**

- [ ] **Step 7: Apply against the LOCAL DB**

```bash
go run ./cmd/convert-base-option --apply
```

Expected: `APPLIED — N converted, ...`. Then confirm idempotency:

```bash
go run ./cmd/convert-base-option --apply
```

Expected: every menu now reports `SKIP — already has N base option(s)`, `0 converted`.

- [ ] **Step 8: Verify in the DB**

```bash
go run ./cmd/convert-base-option            # dry-run shows nothing left to convert
```

Confirm a converted menu now serves base options via the API:

```bash
curl -s localhost:8085/api/menus | python3 -m json.tool | grep -B2 -A6 base_options | head -40
```

Expected: the previously-Serve menus carry `base_options[]` with absolute prices and no longer list the Serve option group.

- [ ] **Step 9: Commit**

```bash
git add cmd/convert-base-option
git commit -m "feat(cmd): convert-base-option migrates Serve groups to base options"
```

---

## Task 8: Documentation

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add the endpoint to the API list**

In `CLAUDE.md`, under `## API Endpoints`, after the `PUT /api/menus/{id}/option-groups` line, add:

```markdown
- `PUT /api/menus/{id}/base-options` — set the menu's base options `{base_options: [{name, price}]}` (replaces all; price in THB, absolute). Empty list = no base option.
```

Also add `base_options` to the menu response note: in the same section near the menus endpoints, ensure it's clear `GET`-ed menus include `base_options[]`.

- [ ] **Step 2: Add a Base Option section**

After the `## Option Groups` section (before `## Discounts`), add:

```markdown
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
```

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document Base Option feature + endpoint + converter"
```

---

## Final verification

- [ ] **Full build + test, both modules**

```bash
go build ./... && go test ./...
cd mulan-agent && go build ./... && go test ./... && cd ..
```

Expected: both modules build; all tests PASS.

- [ ] **End-to-end (local DB):** create a base-option menu in `/manager/items`, order it at `/pos` (modal forces the pick), check out, and confirm the receipt prints `Item (Variant)  <price>` with no base breakdown. Confirm a converted Serve menu behaves identically.

---

## Self-review notes (coverage vs spec)

- Absolute pricing replaces line base — Task 3 Step 7 (`unitPrice := basePrice + deltaSum`). ✓
- Max one set per menu — structural (single `menu_base_options` list; manager edits one list). ✓
- Required pick — POS Task 5 Step 3 + server `ErrMissingBaseOption` Task 3. ✓
- Inline receipt name, no breakdown — Task 4 `displayName()`. ✓
- Snapshot stability — `order_items.base_option_name` + `price` (Task 1/3). ✓
- Stacking with delta options — Task 3 keeps `resolveLineOptions` deltas. ✓
- Converter: exact name match + `--source-name`, detach + delete isolated only, dry-run default, idempotent — Task 7. ✓
- Held orders ride the opaque POS payload (base fields on the line object) — no backend change needed. ✓
