# Orders List / Report — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An owner-gated `GET /api/reports/orders` endpoint (mulan) + a SvelteKit `/orders` page (mulan-manager) that lists orders in a filterable, paginated table with expandable per-order detail.

**Architecture:** New Go feature package `internal/report/{domain,service,http}` with 5 batched sqlc queries (no N+1); a pure `assemble()` function reconstructs per-order totals (satang→THB) and nests line items / options / discounts. The SvelteKit page reuses the dashboard's `presetRange`, iOS components, and the bearer-attaching proxy.

**Tech Stack:** Go 1.25, chi, sqlc/pgx, Atlas (no schema change); SvelteKit, Svelte 5 runes, Tailwind v4, Vitest.

**Spec:** `docs/superpowers/specs/2026-06-14-orders-list-report-design.md`

**Branching:** This work spans two repos. In `mulan`, create branch `feat/orders-report` (the repo is currently on `feat/member` with uncommitted dashboard-gate changes — branch from a CLEAN tree; if those changes are in the way, the executor must stop and ask). In `mulan-manager`, create branch `feat/orders-report` off `main`.

**Money convention:** DB is int64 satang. JSON is THB (satang/100), matching the dashboard. Conversion happens ONLY at the assemble boundary.

---

## File Structure

**mulan (backend):**

| File | Responsibility |
|---|---|
| `internal/sql/reports.query.sql` (new) | 5 sqlc queries: orders page, count, items, item-options, discounts |
| `sqlc/*` (regenerated) | generated — never hand-edit |
| `internal/report/domain/order.go` (new) | JSON response types (`Order`, `LineItem`, `OptionLine`, `DiscountLine`, `Page`) |
| `internal/report/service/assemble.go` (new) | pure `assemble()` + satang→THB; row input structs |
| `internal/report/service/service.go` (new) | `Service.ListOrders` — fetch (sqlc) + map + `assemble` |
| `internal/report/service/assemble_test.go` (new) | table-driven unit tests for `assemble` |
| `internal/report/http/handler.go` (new) | param parse + `GET /orders` handler |
| `internal/report/http/handler_test.go` (new) | param parsing tests |
| `main.go` (modify) | construct service+handler, mount `/api/reports` under owner group |
| `CLAUDE.md` (modify) | document endpoint + route scoping |

**mulan-manager (frontend):**

| File | Responsibility |
|---|---|
| `src/lib/api/reports.ts` (new) | types + `ordersQS()` + `listOrders()` |
| `src/lib/api/reports.spec.ts` (new) | `ordersQS` unit tests |
| `src/routes/(app)/orders/+page.svelte` (new) | the page (filters, table, expand, load-more) |
| `src/lib/components/ios/SideNav.svelte` (modify) | add Orders link |
| `src/routes/(app)/more/+page.svelte` (modify) | add Orders link |
| `src/routes/api/[...path]/+server.ts` (modify) | add `reports` to ALLOW |

**Test commands:**
- Backend: `go test ./internal/report/...` · build: `go build ./...` · codegen: `task sqlcgen`
- Frontend: one file `npm run test:unit -- --run <path>` · `npm run check` · `npm run build`

---

## Task 1: SQL queries + sqlc codegen (mulan)

**Files:** Create `internal/sql/reports.query.sql`; regenerate `sqlc/`.

- [ ] **Step 1: Write the queries**

Create `internal/sql/reports.query.sql`:
```sql
-- name: ListOrdersPage :many
SELECT o.id, o.code, o.status, o.created_at, o.points_earned,
       COALESCE(m.name, '')  AS member_name,
       COALESCE(m.phone, '') AS member_phone
FROM orders o
LEFT JOIN members m ON m.id = o.member_id
WHERE (o.status = @status OR @status = '')
  AND o.created_at >= @from_at::timestamptz
  AND o.created_at <  @to_at::timestamptz
ORDER BY o.created_at DESC
LIMIT @lim OFFSET @off;

-- name: CountOrdersPage :one
SELECT COUNT(*)::bigint AS total
FROM orders o
WHERE (o.status = @status OR @status = '')
  AND o.created_at >= @from_at::timestamptz
  AND o.created_at <  @to_at::timestamptz;

-- name: ListOrderItemsByOrderIDs :many
SELECT oi.id, oi.order_id, oi.name,
       COALESCE(oi.base_option_name, '') AS base_option_name,
       oi.price, oi.qty
FROM order_items oi
WHERE oi.order_id = ANY(@order_ids::int[])
ORDER BY oi.id;

-- name: ListOrderItemOptionsByItemIDs :many
SELECT oio.order_item_id, oio.name, oio.price_delta
FROM order_item_options oio
WHERE oio.order_item_id = ANY(@item_ids::int[])
ORDER BY oio.id;

-- name: ListOrderDiscountsByOrderIDs :many
SELECT od.order_id, od.name, od.discount_type, od.amount, od.is_subsidy
FROM order_discounts od
WHERE od.order_id = ANY(@order_ids::int[])
ORDER BY od.id;
```

- [ ] **Step 2: Regenerate sqlc**

Run: `task sqlcgen`
Expected: completes without error; new generated methods `ListOrdersPage`, `CountOrdersPage`, `ListOrderItemsByOrderIDs`, `ListOrderItemOptionsByItemIDs`, `ListOrderDiscountsByOrderIDs` appear under `sqlc/`. (If sqlc reports an unknown column, STOP — verify the column name against `schema.hcl` and report.)

- [ ] **Step 3: Build to confirm generated code compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Commit**
```bash
git add internal/sql/reports.query.sql sqlc/
git commit -m "feat(report): sqlc queries for orders list (page, count, items, options, discounts)"
```
(End every commit body with:
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>)

---

## Task 2: Domain types (mulan)

**Files:** Create `internal/report/domain/order.go`.

- [ ] **Step 1: Create the types**

Create `internal/report/domain/order.go`:
```go
package domain

import "time"

// OptionLine is a chosen option snapshotted on an order line. Price in THB.
type OptionLine struct {
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
}

// LineItem is one order line. Price (THB) is the snapshotted base/line price;
// Options carry +deltas (THB) shown in detail.
type LineItem struct {
	Name           string       `json:"name"`
	BaseOptionName string       `json:"base_option_name"`
	Qty            int32        `json:"qty"`
	Price          float64      `json:"price"`
	Options        []OptionLine `json:"options"`
}

// DiscountLine is an applied discount snapshot. Amount in THB.
type DiscountLine struct {
	Name         string  `json:"name"`
	DiscountType string  `json:"discount_type"`
	Amount       float64 `json:"amount"`
	IsSubsidy    bool    `json:"is_subsidy"`
}

// Order is one row of the orders report. Money fields are THB.
type Order struct {
	Code         string         `json:"code"`
	Status       string         `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	MemberName   string         `json:"member_name"`
	MemberPhone  string         `json:"member_phone"`
	PointsEarned int64          `json:"points_earned"`
	ItemCount    int            `json:"item_count"`
	Qty          int64          `json:"qty"`
	Gross        float64        `json:"gross"`
	Discount     float64        `json:"discount"`
	Subsidy      float64        `json:"subsidy"`
	Net          float64        `json:"net"`
	LineItems    []LineItem     `json:"line_items"`
	Discounts    []DiscountLine `json:"discounts"`
}

// Page is the paginated response body.
type Page struct {
	Orders []Order `json:"orders"`
	Total  int64   `json:"total"`
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Commit**
```bash
git add internal/report/domain/order.go
git commit -m "feat(report): domain types for orders report"
```

---

## Task 3: Pure assemble() + unit tests (mulan)

**Files:** Create `internal/report/service/assemble.go`, `internal/report/service/assemble_test.go`.

- [ ] **Step 1: Write the failing test**

Create `internal/report/service/assemble_test.go`:
```go
package service

import (
	"testing"
	"time"
)

func TestAssemble(t *testing.T) {
	created := time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC)
	orders := []OrderRow{
		{ID: 1, Code: "AAA", Status: "paid", CreatedAt: created, MemberName: "Cream", MemberPhone: "08", PointsEarned: 9},
		{ID: 2, Code: "BBB", Status: "open", CreatedAt: created, MemberName: "", MemberPhone: "", PointsEarned: 0},
	}
	items := []ItemRow{
		{ID: 10, OrderID: 1, Name: "Americano", BaseOptionName: "Iced", Price: 8000, Qty: 1}, // 80.00 + opt
		{ID: 20, OrderID: 2, Name: "Latte", BaseOptionName: "", Price: 5000, Qty: 2},          // 50.00 x2
	}
	options := []OptionRow{
		{OrderItemID: 10, Name: "Oat milk", PriceDelta: 1000}, // +10.00
	}
	discounts := []DiscountRow{
		{OrderID: 1, Name: "Staff", DiscountType: "fixed", Amount: 2000, IsSubsidy: false}, // -20.00
	}

	got := assemble(orders, items, options, discounts)

	if len(got) != 2 {
		t.Fatalf("want 2 orders, got %d", len(got))
	}
	a := got[0]
	if a.Code != "AAA" {
		t.Fatalf("want AAA first, got %s", a.Code)
	}
	if a.Gross != 90.00 { // (80 + 10) * 1
		t.Errorf("gross: want 90, got %v", a.Gross)
	}
	if a.Discount != 20.00 {
		t.Errorf("discount: want 20, got %v", a.Discount)
	}
	if a.Net != 70.00 {
		t.Errorf("net: want 70, got %v", a.Net)
	}
	if a.ItemCount != 1 || a.Qty != 1 {
		t.Errorf("counts: want 1/1, got %d/%d", a.ItemCount, a.Qty)
	}
	if len(a.LineItems) != 1 || len(a.LineItems[0].Options) != 1 || a.LineItems[0].Options[0].PriceDelta != 10.00 {
		t.Errorf("line/options not assembled: %+v", a.LineItems)
	}
	b := got[1]
	if b.Gross != 100.00 || b.Discount != 0 || b.Net != 100.00 { // 50 x2, no discounts
		t.Errorf("open order: want gross 100/disc 0/net 100, got %v/%v/%v", b.Gross, b.Discount, b.Net)
	}
	if b.MemberName != "" {
		t.Errorf("walk-in member should be empty, got %q", b.MemberName)
	}
}

func TestSubsidySplit(t *testing.T) {
	got := assemble(
		[]OrderRow{{ID: 1, Code: "X", Status: "paid"}},
		[]ItemRow{{ID: 1, OrderID: 1, Name: "i", Price: 10000, Qty: 1}},
		nil,
		[]DiscountRow{
			{OrderID: 1, Name: "d", DiscountType: "fixed", Amount: 1000, IsSubsidy: false},
			{OrderID: 1, Name: "s", DiscountType: "fixed", Amount: 3000, IsSubsidy: true},
		},
	)
	if got[0].Discount != 10.00 || got[0].Subsidy != 30.00 || got[0].Net != 90.00 {
		t.Errorf("want disc 10/subsidy 30/net 90, got %v/%v/%v", got[0].Discount, got[0].Subsidy, got[0].Net)
	}
}
```

- [ ] **Step 2: Run it — verify fail**

Run: `go test ./internal/report/service/`
Expected: FAIL — `assemble`, `OrderRow`, etc. undefined.

- [ ] **Step 3: Implement**

Create `internal/report/service/assemble.go`:
```go
package service

import (
	"time"

	"mulan/internal/report/domain"
)

// Row structs decouple assemble() from sqlc-generated types so it is unit
// testable. Money fields are satang (int64); assemble converts to THB.
type OrderRow struct {
	ID           int32
	Code         string
	Status       string
	CreatedAt    time.Time
	MemberName   string
	MemberPhone  string
	PointsEarned int64
}
type ItemRow struct {
	ID             int32
	OrderID        int32
	Name           string
	BaseOptionName string
	Price          int64
	Qty            int32
}
type OptionRow struct {
	OrderItemID int32
	Name        string
	PriceDelta  int64
}
type DiscountRow struct {
	OrderID      int32
	Name         string
	DiscountType string
	Amount       int64
	IsSubsidy    bool
}

func thb(satang int64) float64 { return float64(satang) / 100 }

// assemble stitches the batched rows into report orders, preserving the order
// of `orders` (already sorted newest-first by the query). Gross includes option
// price deltas, matching dashboard revenue.
func assemble(orders []OrderRow, items []ItemRow, options []OptionRow, discounts []DiscountRow) []domain.Order {
	optByItem := map[int32][]OptionRow{}
	for _, o := range options {
		optByItem[o.OrderItemID] = append(optByItem[o.OrderItemID], o)
	}
	itemsByOrder := map[int32][]ItemRow{}
	for _, it := range items {
		itemsByOrder[it.OrderID] = append(itemsByOrder[it.OrderID], it)
	}
	discByOrder := map[int32][]DiscountRow{}
	for _, d := range discounts {
		discByOrder[d.OrderID] = append(discByOrder[d.OrderID], d)
	}

	out := make([]domain.Order, 0, len(orders))
	for _, o := range orders {
		ord := domain.Order{
			Code:         o.Code,
			Status:       o.Status,
			CreatedAt:    o.CreatedAt,
			MemberName:   o.MemberName,
			MemberPhone:  o.MemberPhone,
			PointsEarned: o.PointsEarned,
			LineItems:    []domain.LineItem{},
			Discounts:    []domain.DiscountLine{},
		}

		var grossSat, qty int64
		for _, it := range itemsByOrder[o.ID] {
			var optSat int64
			optLines := []domain.OptionLine{}
			for _, op := range optByItem[it.ID] {
				optSat += op.PriceDelta
				optLines = append(optLines, domain.OptionLine{Name: op.Name, PriceDelta: thb(op.PriceDelta)})
			}
			grossSat += (it.Price + optSat) * int64(it.Qty)
			qty += int64(it.Qty)
			ord.LineItems = append(ord.LineItems, domain.LineItem{
				Name:           it.Name,
				BaseOptionName: it.BaseOptionName,
				Qty:            it.Qty,
				Price:          thb(it.Price),
				Options:        optLines,
			})
		}
		ord.ItemCount = len(ord.LineItems)
		ord.Qty = qty

		var discSat, subSat int64
		for _, d := range discByOrder[o.ID] {
			if d.IsSubsidy {
				subSat += d.Amount
			} else {
				discSat += d.Amount
			}
			ord.Discounts = append(ord.Discounts, domain.DiscountLine{
				Name:         d.Name,
				DiscountType: d.DiscountType,
				Amount:       thb(d.Amount),
				IsSubsidy:    d.IsSubsidy,
			})
		}

		ord.Gross = thb(grossSat)
		ord.Discount = thb(discSat)
		ord.Subsidy = thb(subSat)
		ord.Net = thb(grossSat - discSat)
		out = append(out, ord)
	}
	return out
}
```

- [ ] **Step 4: Run it — verify pass**

Run: `go test ./internal/report/service/`
Expected: PASS (TestAssemble, TestSubsidySplit).

- [ ] **Step 5: Commit**
```bash
git add internal/report/service/assemble.go internal/report/service/assemble_test.go
git commit -m "feat(report): pure assemble() reconstructing order totals (TDD)"
```

---

## Task 4: Service.ListOrders (mulan)

**Files:** Create `internal/report/service/service.go`.

(No new unit test — DB-bound; the compute path is covered by Task 3. Verified by `go build` + the handler test + manual.)

- [ ] **Step 1: Implement**

Create `internal/report/service/service.go`:
```go
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/internal/report/domain"
	"mulan/sqlc"
)

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

type ListParams struct {
	Status string // "" = all
	From   time.Time
	To     time.Time
	Limit  int32
	Offset int32
}

func (s *Service) ListOrders(ctx context.Context, p ListParams) (domain.Page, error) {
	fromAt := pgtype.Timestamptz{Time: p.From, Valid: true}
	toAt := pgtype.Timestamptz{Time: p.To, Valid: true}

	total, err := s.q.CountOrdersPage(ctx, sqlc.CountOrdersPageParams{
		Status: p.Status, FromAt: fromAt, ToAt: toAt,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("count orders: %w", err)
	}

	rows, err := s.q.ListOrdersPage(ctx, sqlc.ListOrdersPageParams{
		Status: p.Status, FromAt: fromAt, ToAt: toAt, Lim: p.Limit, Off: p.Offset,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("list orders: %w", err)
	}

	orderRows := make([]OrderRow, len(rows))
	ids := make([]int32, len(rows))
	for i, r := range rows {
		orderRows[i] = OrderRow{
			ID:           r.ID,
			Code:         r.Code,
			Status:       r.Status,
			CreatedAt:    r.CreatedAt.Time,
			MemberName:   r.MemberName,
			MemberPhone:  r.MemberPhone,
			PointsEarned: r.PointsEarned,
		}
		ids[i] = r.ID
	}

	if len(ids) == 0 {
		return domain.Page{Orders: []domain.Order{}, Total: total}, nil
	}

	itemRows, err := s.q.ListOrderItemsByOrderIDs(ctx, ids)
	if err != nil {
		return domain.Page{}, fmt.Errorf("list items: %w", err)
	}
	items := make([]ItemRow, len(itemRows))
	itemIDs := make([]int32, len(itemRows))
	for i, it := range itemRows {
		items[i] = ItemRow{
			ID: it.ID, OrderID: it.OrderID, Name: it.Name,
			BaseOptionName: it.BaseOptionName, Price: it.Price, Qty: it.Qty,
		}
		itemIDs[i] = it.ID
	}

	var options []OptionRow
	if len(itemIDs) > 0 {
		optRows, err := s.q.ListOrderItemOptionsByItemIDs(ctx, itemIDs)
		if err != nil {
			return domain.Page{}, fmt.Errorf("list options: %w", err)
		}
		options = make([]OptionRow, len(optRows))
		for i, op := range optRows {
			options[i] = OptionRow{OrderItemID: op.OrderItemID, Name: op.Name, PriceDelta: op.PriceDelta}
		}
	}

	discRows, err := s.q.ListOrderDiscountsByOrderIDs(ctx, ids)
	if err != nil {
		return domain.Page{}, fmt.Errorf("list discounts: %w", err)
	}
	discounts := make([]DiscountRow, len(discRows))
	for i, d := range discRows {
		discounts[i] = DiscountRow{
			OrderID: d.OrderID, Name: d.Name, DiscountType: d.DiscountType,
			Amount: d.Amount, IsSubsidy: d.IsSubsidy,
		}
	}

	return domain.Page{Orders: assemble(orderRows, items, options, discounts), Total: total}, nil
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: success. (If a sqlc field name differs — e.g. the generated param for `LIMIT @lim` is not `Lim`, or `ListOrderItemsByOrderIDs` takes a named-params struct instead of a bare `[]int32` — adjust the call sites to the ACTUAL generated names from Task 1's output, and report the adjustment. Do not invent names.)

- [ ] **Step 3: Commit**
```bash
git add internal/report/service/service.go
git commit -m "feat(report): ListOrders service (batched fetch + assemble)"
```

---

## Task 5: HTTP handler + tests (mulan)

**Files:** Create `internal/report/http/handler.go`, `internal/report/http/handler_test.go`.

- [ ] **Step 1: Implement the handler**

Create `internal/report/http/handler.go`:
```go
package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"mulan/internal/httpx"
	"mulan/internal/report/service"
	"mulan/internal/response"
)

const (
	maxRangeDays = 92
	defaultLimit = 100
	maxLimit     = 200
)

var shopLocation = mustLoad("Asia/Bangkok")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/orders", h.ListOrders)
}

// parseListParams reads from/to (shop-local ISO dates, default last 7 days),
// status (paid|open|held, default all), and limit/offset.
func parseListParams(r *http.Request) (service.ListParams, error) {
	now := time.Now().In(shopLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shopLocation)
	from := today.AddDate(0, 0, -6)
	to := today.Add(24 * time.Hour)

	if t, ok, err := httpx.DateQuery(r, "from", shopLocation); err != nil {
		return service.ListParams{}, err
	} else if ok {
		from = t
	}
	if t, ok, err := httpx.DateQuery(r, "to", shopLocation); err != nil {
		return service.ListParams{}, err
	} else if ok {
		to = t.Add(24 * time.Hour)
	}
	if !to.After(from) {
		return service.ListParams{}, errBadRange
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return service.ListParams{}, errRangeTooLarge
	}

	status := r.URL.Query().Get("status")
	switch status {
	case "", "paid", "open", "held":
	default:
		return service.ListParams{}, errBadStatus
	}

	limit := int32(defaultLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return service.ListParams{}, errBadLimit
		}
		if n > maxLimit {
			n = maxLimit
		}
		limit = int32(n)
	}
	var offset int32
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return service.ListParams{}, errBadOffset
		}
		offset = int32(n)
	}

	return service.ListParams{Status: status, From: from, To: to, Limit: limit, Offset: offset}, nil
}

func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	p, err := parseListParams(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	page, err := h.svc.ListOrders(r.Context(), p)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to load orders", err)
		return
	}
	response.OK(w, r, page)
}
```

Create the sentinel errors in the same package — add to the bottom of `handler.go`:
```go
import "errors"

var (
	errBadRange      = errors.New("to must be after from")
	errRangeTooLarge = errors.New("range too large")
	errBadStatus     = errors.New("invalid status")
	errBadLimit      = errors.New("invalid limit")
	errBadOffset     = errors.New("invalid offset")
)
```
(Place the `errors` import in the existing import block, not a second one — merge it.)

- [ ] **Step 2: Write the test**

Create `internal/report/http/handler_test.go`:
```go
package http

import (
	"net/http/httptest"
	"testing"
)

func req(qs string) *httptest.ResponseRecorder { return httptest.NewRecorder() }

func TestParseListParams(t *testing.T) {
	mk := func(q string) (any, error) {
		r := httptest.NewRequest("GET", "/orders"+q, nil)
		return parseListParams(r)
	}

	t.Run("defaults to last 7 days, all statuses", func(t *testing.T) {
		p, err := mk("")
		if err != nil {
			t.Fatal(err)
		}
		got := p.(interface{ String() string })
		_ = got
	})

	t.Run("rejects bad status", func(t *testing.T) {
		if _, err := mk("?status=bogus"); err == nil {
			t.Fatal("want error for bad status")
		}
	})
	t.Run("rejects range over cap", func(t *testing.T) {
		if _, err := mk("?from=2026-01-01&to=2026-12-31"); err == nil {
			t.Fatal("want error for range too large")
		}
	})
	t.Run("clamps limit to max", func(t *testing.T) {
		p, err := mk("?limit=9999")
		if err != nil {
			t.Fatal(err)
		}
		if p.Limit != maxLimit {
			t.Fatalf("want limit clamped to %d, got %d", maxLimit, p.Limit)
		}
	})
	t.Run("rejects negative offset", func(t *testing.T) {
		if _, err := mk("?offset=-1"); err == nil {
			t.Fatal("want error for negative offset")
		}
	})
}
```

NOTE: `parseListParams` returns `(service.ListParams, error)`, a concrete type — so the test must import the service type. Replace the `mk` closure and the two cases that read `p.Limit` with a version that uses the concrete type:
```go
import "mulan/internal/report/service"

func TestParseListParams(t *testing.T) {
	mk := func(q string) (service.ListParams, error) {
		r := httptest.NewRequest("GET", "/orders"+q, nil)
		return parseListParams(r)
	}
	if _, err := mk(""); err != nil {
		t.Fatalf("defaults should parse: %v", err)
	}
	if _, err := mk("?status=bogus"); err == nil {
		t.Fatal("want error for bad status")
	}
	if _, err := mk("?from=2026-01-01&to=2026-12-31"); err == nil {
		t.Fatal("want error for range too large")
	}
	p, err := mk("?limit=9999")
	if err != nil {
		t.Fatalf("limit parse: %v", err)
	}
	if p.Limit != maxLimit {
		t.Fatalf("want limit clamped to %d, got %d", maxLimit, p.Limit)
	}
	if _, err := mk("?offset=-1"); err == nil {
		t.Fatal("want error for negative offset")
	}
}
```
Use ONLY this second version (delete the first sketch + the unused `req` helper). The final `handler_test.go` imports: `net/http/httptest`, `testing`, `mulan/internal/report/service`.

- [ ] **Step 3: Run it — verify pass**

Run: `go test ./internal/report/...`
Expected: PASS (assemble tests + handler param tests).

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**
```bash
git add internal/report/http/handler.go internal/report/http/handler_test.go
git commit -m "feat(report): GET /orders handler + param-parse tests"
```

---

## Task 6: Wire route + docs (mulan)

**Files:** Modify `main.go`, `CLAUDE.md`.

- [ ] **Step 1: Add imports + construct handler in main.go**

In `main.go`, add to the import block (with the other feature imports):
```go
	reporthttp "mulan/internal/report/http"
	reportservice "mulan/internal/report/service"
```
After the dashboard handler is constructed (near `dashboardHandler := ...`), add:
```go
	reportSvc := reportservice.NewService(queries)
	reportHandler := reporthttp.NewHandler(reportSvc)
```

- [ ] **Step 2: Mount the route under the owner group**

In `main.go`, inside the `RequireRole(owner)` group (the block that begins `r.Use(managerauthhttp.RequireRole(managerauthdomain.RoleOwner))`), add a line next to the other owner mounts (e.g. right after the discounts writes):
```go
				r.Route("/reports", reportHandler.Routes)
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 4: Document in CLAUDE.md**

In `CLAUDE.md`:
- Under **API Endpoints**, add a line:
  `- GET /api/reports/orders?from=&to=&status=&limit=&offset= — owner-gated paginated order list with reconstructed totals + line items (status: paid|open|held, empty=all; THB money; default 7-day range, limit 100/max 200)`
- In the **Route scoping** `RequireRole(owner)` bullet, append `, /api/reports/*` to the list of owner-gated routes.
- In the **Adding a manager route** note's proxy reminder, no action (handled in Task 9).

- [ ] **Step 5: Commit**
```bash
git add main.go CLAUDE.md
git commit -m "feat(report): mount owner-gated /api/reports/orders + docs"
```

---

## Task 7: Frontend API client + tests (mulan-manager)

**Files:** Create `src/lib/api/reports.ts`, `src/lib/api/reports.spec.ts`.

(Work in the `mulan-manager` repo on branch `feat/orders-report`.)

- [ ] **Step 1: Write the failing test**

Create `src/lib/api/reports.spec.ts`:
```ts
import { describe, it, expect } from 'vitest';
import { ordersQS } from './reports';

describe('ordersQS', () => {
	it('includes from/to and pagination', () => {
		expect(ordersQS({ from: '2026-06-08', to: '2026-06-14', limit: 100, offset: 0 })).toBe(
			'from=2026-06-08&to=2026-06-14&limit=100&offset=0'
		);
	});
	it('omits status when empty (All)', () => {
		const qs = ordersQS({ from: '2026-06-08', to: '2026-06-14' });
		expect(qs).toBe('from=2026-06-08&to=2026-06-14');
	});
	it('includes status when set', () => {
		expect(ordersQS({ from: '2026-06-08', to: '2026-06-14', status: 'paid' })).toContain(
			'status=paid'
		);
	});
});
```

- [ ] **Step 2: Run it — verify fail**

Run: `npm run test:unit -- --run src/lib/api/reports.spec.ts`
Expected: FAIL — cannot resolve `./reports`.

- [ ] **Step 3: Implement**

Create `src/lib/api/reports.ts`:
```ts
export interface OptionLine {
	name: string;
	price_delta: number;
}
export interface OrderLine {
	name: string;
	base_option_name: string;
	qty: number;
	price: number;
	options: OptionLine[];
}
export interface OrderDiscount {
	name: string;
	discount_type: string;
	amount: number;
	is_subsidy: boolean;
}
export interface OrderRow {
	code: string;
	status: string;
	created_at: string;
	member_name: string;
	member_phone: string;
	points_earned: number;
	item_count: number;
	qty: number;
	gross: number;
	discount: number;
	subsidy: number;
	net: number;
	line_items: OrderLine[];
	discounts: OrderDiscount[];
}
export interface OrdersPage {
	orders: OrderRow[];
	total: number;
}
export interface OrdersQuery {
	from: string;
	to: string;
	status?: string;
	limit?: number;
	offset?: number;
}

export function ordersQS(q: OrdersQuery): string {
	const p = new URLSearchParams();
	p.set('from', q.from);
	p.set('to', q.to);
	if (q.status) p.set('status', q.status);
	if (q.limit != null) p.set('limit', String(q.limit));
	if (q.offset != null) p.set('offset', String(q.offset));
	return p.toString();
}

async function j<T>(res: Response): Promise<T> {
	const b = await res.json().catch(() => ({}));
	if (!res.ok) throw new Error(b?.error || `HTTP ${res.status}`);
	return b.data as T;
}

export const listOrders = (q: OrdersQuery) =>
	fetch(`/api/reports/orders?${ordersQS(q)}`).then((r) => j<OrdersPage>(r));
```

- [ ] **Step 4: Run it — verify pass**

Run: `npm run test:unit -- --run src/lib/api/reports.spec.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**
```bash
git add src/lib/api/reports.ts src/lib/api/reports.spec.ts
git commit -m "feat(manager): reports api client + ordersQS (TDD)"
```

---

## Task 8: Orders page + nav + proxy (mulan-manager)

**Files:** Create `src/routes/(app)/orders/+page.svelte`; Modify `src/lib/components/ios/SideNav.svelte`, `src/routes/(app)/more/+page.svelte`, `src/routes/api/[...path]/+server.ts`.

- [ ] **Step 1: Add `reports` to the proxy ALLOW**

In `src/routes/api/[...path]/+server.ts`, add `'reports'` to the `ALLOW` array (e.g. after `'members'`):
```ts
	'members',
	'reports',
```

- [ ] **Step 2: Create the page**

Create `src/routes/(app)/orders/+page.svelte`:
```svelte
<script lang="ts">
	import NavBar from '$lib/components/ios/NavBar.svelte';
	import Card from '$lib/components/ios/Card.svelte';
	import Spinner from '$lib/components/ios/Spinner.svelte';
	import EmptyState from '$lib/components/ios/EmptyState.svelte';
	import SegmentedControl from '$lib/components/ios/SegmentedControl.svelte';
	import { showToast } from '$lib/components/ios/toast.svelte';
	import { baht } from '$lib/format';
	import { presetRange, type Preset } from '$lib/dashboard/range';
	import { listOrders, type OrderRow } from '$lib/api/reports';

	const PAGE = 100;

	const presets = [
		{ label: 'Today', value: 'today' },
		{ label: '7D', value: '7d' },
		{ label: '30D', value: '30d' },
		{ label: '90D', value: '90d' }
	];
	const statuses = [
		{ label: 'Paid', value: 'paid' },
		{ label: 'All', value: '' },
		{ label: 'Open', value: 'open' },
		{ label: 'Held', value: 'held' }
	];

	let preset = $state('7d');
	let status = $state('paid');
	let customFrom = $state('');
	let customTo = $state('');

	let orders = $state<OrderRow[]>([]);
	let total = $state(0);
	let loading = $state(true);
	let loadingMore = $state(false);
	let errored = $state(false);
	let expanded = $state<string | null>(null);

	function range(): { from: string; to: string } {
		if (customFrom && customTo) return { from: customFrom, to: customTo };
		return presetRange(preset as Preset, new Date());
	}

	async function load(reset = true) {
		if (reset) {
			loading = true;
			errored = false;
			expanded = null;
		} else {
			loadingMore = true;
		}
		try {
			const { from, to } = range();
			const offset = reset ? 0 : orders.length;
			const page = await listOrders({ from, to, status: status || undefined, limit: PAGE, offset });
			orders = reset ? page.orders : [...orders, ...page.orders];
			total = page.total;
		} catch (e) {
			if (reset) errored = true;
			showToast((e as Error).message, 'error');
		} finally {
			loading = false;
			loadingMore = false;
		}
	}

	function toggle(code: string) {
		expanded = expanded === code ? null : code;
	}

	const dt = (iso: string) => new Date(iso).toLocaleString('en-GB', { dateStyle: 'short', timeStyle: 'short' });

	// Refetch whenever any filter changes. customFrom/customTo only take effect
	// once BOTH are set (a complete range); otherwise the preset drives.
	$effect(() => {
		// read the deps so the effect tracks them
		void preset;
		void status;
		void (customFrom && customTo);
		load(true);
	});
</script>

<NavBar title="Orders" />

<div class="space-y-4 px-4 pt-2 pb-6">
	<SegmentedControl options={statuses} bind:value={status} />
	<SegmentedControl options={presets} bind:value={preset} />
	<div class="flex items-center gap-2 text-sm">
		<input type="date" bind:value={customFrom} class="rounded-lg border border-[var(--ios-separator)] bg-[var(--ios-card)] px-2 py-1 text-[var(--ios-label)]" />
		<span class="text-[var(--ios-label-secondary)]">→</span>
		<input type="date" bind:value={customTo} class="rounded-lg border border-[var(--ios-separator)] bg-[var(--ios-card)] px-2 py-1 text-[var(--ios-label)]" />
		{#if customFrom || customTo}
			<button class="text-[var(--ios-blue)]" onclick={() => { customFrom = ''; customTo = ''; }}>Clear</button>
		{/if}
	</div>

	{#if loading}
		<Spinner />
	{:else if errored}
		<EmptyState title="Couldn’t load orders" subtitle="Try again later." />
	{:else if orders.length === 0}
		<EmptyState title="No orders" subtitle="No orders in this range." />
	{:else}
		<p class="px-1 text-xs text-[var(--ios-label-secondary)]">{orders.length} of {total}</p>
		<Card padded={false}>
			<div class="overflow-x-auto">
				<table class="w-full min-w-[680px] text-sm">
					<thead>
						<tr class="border-b border-[var(--ios-separator)] text-left text-xs text-[var(--ios-label-secondary)]">
							<th class="px-3 py-2 font-medium">Date</th>
							<th class="px-3 py-2 font-medium">Code</th>
							<th class="px-3 py-2 font-medium">Status</th>
							<th class="px-3 py-2 text-right font-medium">Items</th>
							<th class="px-3 py-2 text-right font-medium">Gross</th>
							<th class="px-3 py-2 text-right font-medium">Discount</th>
							<th class="px-3 py-2 text-right font-medium">Subsidy</th>
							<th class="px-3 py-2 text-right font-medium">Net</th>
						</tr>
					</thead>
					<tbody>
						{#each orders as o (o.code)}
							<tr
								class="cursor-pointer border-b border-[var(--ios-separator)] hover:bg-[var(--ios-fill)]"
								onclick={() => toggle(o.code)}
							>
								<td class="px-3 py-2 whitespace-nowrap text-[var(--ios-label)]">{dt(o.created_at)}</td>
								<td class="px-3 py-2 font-mono text-[var(--ios-label)]">{o.code}</td>
								<td class="px-3 py-2 text-[var(--ios-label-secondary)]">{o.status}</td>
								<td class="px-3 py-2 text-right text-[var(--ios-label)]">{o.qty}</td>
								<td class="px-3 py-2 text-right font-mono text-[var(--ios-label)]">{baht(o.gross)}</td>
								<td class="px-3 py-2 text-right font-mono text-[var(--ios-label-secondary)]">{baht(o.discount)}</td>
								<td class="px-3 py-2 text-right font-mono text-[var(--ios-label-secondary)]">{baht(o.subsidy)}</td>
								<td class="px-3 py-2 text-right font-mono font-semibold text-[var(--ios-label)]">{baht(o.net)}</td>
							</tr>
							{#if expanded === o.code}
								<tr class="border-b border-[var(--ios-separator)] bg-[var(--ios-fill)]">
									<td colspan="8" class="px-4 py-3">
										<div class="space-y-2 text-sm">
											{#if o.member_name || o.member_phone}
												<p class="text-[var(--ios-label-secondary)]">
													Member: {o.member_name} {o.member_phone} · {o.points_earned} pts
												</p>
											{/if}
											{#each o.line_items as li (li.name + li.base_option_name)}
												<div>
													<div class="flex justify-between">
														<span class="text-[var(--ios-label)]">
															{li.qty}× {li.name}{li.base_option_name ? ` (${li.base_option_name})` : ''}
														</span>
														<span class="font-mono text-[var(--ios-label-secondary)]">{baht(li.price)}</span>
													</div>
													{#each li.options as op (op.name)}
														<div class="flex justify-between pl-4 text-[var(--ios-label-secondary)]">
															<span>+ {op.name}</span><span class="font-mono">{baht(op.price_delta)}</span>
														</div>
													{/each}
												</div>
											{/each}
											{#each o.discounts as d (d.name)}
												<div class="flex justify-between text-[var(--ios-label-secondary)]">
													<span>{d.is_subsidy ? 'Subsidy' : 'Discount'}: {d.name}</span>
													<span class="font-mono">{baht(d.amount)}</span>
												</div>
											{/each}
										</div>
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		</Card>
		{#if orders.length < total}
			<button
				class="w-full rounded-xl bg-[var(--ios-fill)] py-3 text-sm font-medium text-[var(--ios-blue)] disabled:opacity-50"
				disabled={loadingMore}
				onclick={() => load(false)}
			>
				{loadingMore ? 'Loading…' : 'Load more'}
			</button>
		{/if}
	{/if}
</div>
```

- [ ] **Step 3: Add Orders to SideNav**

In `src/lib/components/ios/SideNav.svelte`, add an Orders entry to the FIRST group's `items` array (the one with Dashboard/Menu/Members), after Members:
```js
				{ href: '/orders', label: 'Orders', icon: '🧾' },
```

- [ ] **Step 4: Add Orders to the `/more` page**

In `src/routes/(app)/more/+page.svelte`, add to the `Catalog` group's items (or the first group), matching the existing `{ href, label, icon }` shape:
```js
				{ href: '/orders', label: 'Orders', icon: '🧾' },
```
(Place it in whichever group reads best — the existing `groups` structure in that file. Match the surrounding object shape exactly.)

- [ ] **Step 5: Typecheck + tests + build**

Run: `npm run check`
Expected: 0 errors.
Run: `npm run test:unit -- --run`
Expected: all existing + new tests PASS.
Run: `npm run build`
Expected: success.

- [ ] **Step 6: Commit**
```bash
git add 'src/routes/(app)/orders/+page.svelte' 'src/lib/components/ios/SideNav.svelte' 'src/routes/(app)/more/+page.svelte' 'src/routes/api/[...path]/+server.ts'
git commit -m "feat(manager): orders list page + nav + proxy allow"
```

---

## Task 9: Full verification

**Files:** none.

- [ ] **Step 1: Backend tests + build**

In `mulan`:
Run: `go test ./... && go build ./...`
Expected: all pass, build succeeds.

- [ ] **Step 2: Frontend gates**

In `mulan-manager`:
Run: `npm run test:unit -- --run && npm run check && npm run build`
Expected: tests pass, 0 type errors, build succeeds.

- [ ] **Step 3: Format frontend branch files**

Run: `git diff --name-only main HEAD | while read f; do [ -f "$f" ] && echo "$f"; done | xargs npx prettier --write`
Then `git commit -am "chore(manager): prettier format orders page"` if anything changed.

- [ ] **Step 4: Manual end-to-end**

With the mulan backend running locally (against the dev clone DB) and a logged-in manager:
- Verify `GET /api/reports/orders?from=…&to=…` (with an owner bearer) returns data; and a no-token request returns **401** (owner-gated, NOT open).
- Load `/orders`: status filter (Paid/All/Open/Held), preset chips, custom From/To, row expand shows line items/options/discounts/member, "Load more" appends, count `loaded / total` updates.
- Cross-check one paid order's Net against the same day's figure logic.
- Phone width: table scrolls horizontally. Desktop: full table under the sidebar.

---

## Self-Review notes

- **Spec coverage:** endpoint owner-gated + not under /dashboard (T6) ✓; params from/to/status/limit/offset + 92d cap + limit clamp (T5) ✓; reconstructed totals incl. option deltas, satang→THB (T3) ✓; batched no-N+1 fetch (T4) ✓; nested line items/options/discounts + member/points (T2,T3) ✓; page table + columns Date/Code/Status/Items/Gross/Discount/Subsidy/Net (T8) ✓; presets + custom range (T8) ✓; status filter default Paid (T8) ✓; expandable detail (T8) ✓; Load-more pagination + total (T8) ✓; nav entries + proxy ALLOW `reports` (T6 doc, T8 code) ✓; loading/error/empty (T8) ✓; tests (T3,T5,T7) ✓.
- **Type consistency:** Go `service.ListParams{Status,From,To,Limit,Offset}` used by handler (T5) ↔ defined (T4); `OrderRow/ItemRow/OptionRow/DiscountRow` + `assemble` consistent T3↔T4; `domain.Order/Page` fields ↔ frontend `OrderRow/OrdersPage` JSON names (T2↔T7); `ordersQS`/`listOrders` consistent T7↔T8.
- **Known adjustment point:** sqlc generated param/field names (Task 1 output) are the source of truth — Tasks 4/5 note to align call sites to the actual generated names (`Lim`/`Off`, the `[]int32` array-arg signatures) if they differ.
- **Out of scope:** payment-method column (not stored), CSV export, search.
```
