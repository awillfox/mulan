# Role + Denomination-Aware Cash Drawer — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track the cash drawer's bill/coin counts; let a manager-role cashier set/adjust them (id+PIN); on each cash sale enter the exact denominations tendered, auto-adjust the drawer, and compute the change as real bills/coins — blocking the sale if exact change can't be made until a manager restocks.

**Architecture:** Backend (`mulan`) owns drawer state in Postgres: a new `cash_drawer_denominations` current-state table (source of truth) plus the existing `cash_drawer_audit` extended with a per-denom JSON delta. A bounded coin-change DP computes change against live stock. Cash-sale adjustment runs inside the existing checkout transaction via a new `cashdrawer` service method injected into `OrderService`. POS cashiers gain a `role`; drawer writes re-verify `cashier_id + pin` server-side (no session exists). The POS template (`mulan-agent`) gets per-denom tender entry + live change preview; the SvelteKit `mulan-manager` repo gets a role selector.

**Tech Stack:** Go 1.25, chi/v5, pgx/v5, sqlc, Atlas (schema.hcl), Rhymond/go-money, html/template + vanilla JS (POS), SvelteKit (mulan-manager). Money is int64 satang everywhere except display.

---

## Conventions (fullstack_dev)

- Money: DB `bigint` satang; API JSON exposes THB via `money.New(satang, money.THB).AsMajorUnits()`; denom **keys** and **counts** are plain ints (counts are quantities, not money). Float only at display.
- After editing `internal/sql/*.sql` or `schema.hcl`: run the matching `task` and commit generated `sqlc/` output **in the same commit** as the SQL/schema change.
- `context.Context` first arg on every I/O function. Errors wrap with `fmt.Errorf("...: %w", err)`.
- Multi-table writes share one `pgx.Tx` via `queries.WithTx(tx)`.
- Run `go fmt ./...` before every commit. `go build ./...` and `go test ./...` must pass.

## Denomination constants (used throughout)

Nine THB denominations. Stored/keyed in **satang**:

| ฿ (baht) | satang key |
|---|---|
| 1000 | 100000 |
| 500 | 50000 |
| 100 | 10000 |
| 50 | 5000 |
| 20 | 2000 |
| 10 | 1000 |
| 5 | 500 |
| 2 | 200 |
| 1 | 100 |

`cash_drawer_audit.denominations` JSON keys are **satang strings**, values signed counts, e.g. `{"10000":1,"500":-2}` = one ฿100 in, two ฿5 out.

## Manual prerequisite (one-time, before Task 1)

`task migrate-dev` applies `schema.hcl` to the **dev** DB (`$PSQL_URL`, a prod clone per `project_db_safety`). Confirm `.env` has `PSQL_URL` + `PSQL_DEV_URL` set and pointing at the **local** DB, never production. All `task migrate-dev` / `task generate-sql-schema` / `task sqlcgen` in this plan run against that local DB.

---

## File Map

**Backend (`mulan`):**
- `schema.hcl` — add `cashiers.role`; new table `cash_drawer_denominations`; add `cash_drawer_audit.denominations` + extend event_type CHECK with `sale`.
- `internal/sql/cashiers.command.sql`, `internal/sql/cashiers.query.sql` — thread `role` through create/update + all selects.
- `internal/sql/cash_drawer.command.sql`, `internal/sql/cash_drawer.query.sql` — denomination get/set/adjust + audit insert with denominations.
- `internal/cashier/service/cashier.go` — `role` on create/update; `VerifyManager(id, pin)`.
- `internal/cashier/http/handler.go` — `role` in DTOs.
- `internal/cashdrawer/service/denominations.go` (new) — `CurrentDenoms`, `SetDenoms`, `AdjustDenoms`, `ApplyCashSale`.
- `internal/cashdrawer/service/change.go` (new) — `MakeChange` bounded-DP + `Denominations` slice.
- `internal/cashdrawer/service/change_test.go`, `denominations_test.go` (new) — unit tests.
- `internal/cashdrawer/http/denominations.go` (new) — denom HTTP handlers + role/PIN gate.
- `internal/order/service/order.go` — `Checkout` takes cash tender; calls `ApplyCashSale` in tx; new sentinel errors.
- `internal/order/http/handler.go` — checkout request gains `payment_method` + `cash_tender`; response gains `rounded_due` + `change_breakdown`; error mapping.
- `main.go` — construct `cashDrawerSvc` before `orderSvc`, inject it; register denom routes + cashier-svc into drawer handler.

**Agent (`mulan-agent`):**
- `mulan-agent/main.go` — checkout proxy forwards `cash_tender` + reads `change_breakdown`.
- `mulan-agent/templates/pos/index.html` — login stores role; drawer denom editor; per-denom tender + live change preview; change modal shows breakdown.

**Frontend (`../mulan-manager`):**
- `src/lib/api/cashiers.ts` — `role` field + params.
- `src/routes/(app)/cashiers/+page.svelte` — role selector + display.

---

# PHASE A — Cashier role

### Task A1: Add `role` column to `cashiers` (schema)

**Files:**
- Modify: `schema.hcl` (cashiers table, after the `active` column block ~line 137)

- [ ] **Step 1: Add the column + CHECK**

In `schema.hcl`, inside `table "cashiers"`, add a `role` column immediately after the `active` column block (before `created_at`):

```hcl
  column "role" {
    type    = varchar(20)
    null    = false
    default = "cashier"
  }
```

And add a check constraint inside the same table block, after the `index "cashiers_login_id_key"` block:

```hcl
  check "cashiers_role_check" {
    expr = "role IN ('cashier', 'manager')"
  }
```

- [ ] **Step 2: Apply to dev DB**

Run: `task migrate-dev`
Expected: Atlas prints a plan adding the `role` column + check and applies it with no error. Existing rows default to `'cashier'`.

- [ ] **Step 3: Regenerate inspected SQL schema**

Run: `task generate-sql-schema`
Expected: `schema.sql` updated to include `role` on `cashiers`.

- [ ] **Step 4: Commit**

```bash
go fmt ./... && git add schema.hcl schema.sql
git commit -m "feat(cashier): add role column (cashier|manager)"
```

---

### Task A2: Thread `role` through cashier SQL + regenerate sqlc

**Files:**
- Modify: `internal/sql/cashiers.command.sql`
- Modify: `internal/sql/cashiers.query.sql`

- [ ] **Step 1: Update command SQL**

Replace the full contents of `internal/sql/cashiers.command.sql` with:

> NOTE (plan fix): column order in every SELECT/RETURNING list MUST match the
> table's physical column order, which has `role` **last** (Atlas appended it).
> Putting `role` mid-list makes sqlc emit per-query `*Row` structs instead of
> reusing `sqlc.Cashier`, breaking downstream code that returns `sqlc.Cashier`.
> Keep `role` last.

```sql
-- name: CreateCashier :one
INSERT INTO cashiers (login_id, name, pin_hash, role)
VALUES ($1, $2, $3, $4)
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at, role;

-- name: UpdateCashier :one
UPDATE cashiers
SET name       = $2,
    active     = $3,
    role       = $4,
    updated_at = now()
WHERE id = $1
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at, role;

-- name: UpdateCashierPin :one
UPDATE cashiers
SET pin_hash   = $2,
    updated_at = now()
WHERE id = $1
RETURNING id, login_id, name, pin_hash, active, created_at, updated_at, role;

-- name: DeleteCashier :exec
DELETE FROM cashiers WHERE id = $1;
```

- [ ] **Step 2: Update query SQL**

Replace the full contents of `internal/sql/cashiers.query.sql` with:

```sql
-- name: ListCashiers :many
SELECT id, login_id, name, pin_hash, active, created_at, updated_at, role
FROM cashiers
ORDER BY login_id;

-- name: GetCashierByLoginID :one
SELECT id, login_id, name, pin_hash, active, created_at, updated_at, role
FROM cashiers WHERE login_id = $1;

-- name: GetCashier :one
SELECT id, login_id, name, pin_hash, active, created_at, updated_at, role
FROM cashiers WHERE id = $1;
```

- [ ] **Step 3: Regenerate sqlc**

Run: `task sqlcgen`
Expected: `sqlc/cashiers.command.sql.go` + `.query.sql.go` regenerate; `sqlc.Cashier` gains a `Role string` field; `CreateCashierParams`/`UpdateCashierParams` gain `Role string`.

- [ ] **Step 4: Verify build (will fail at call sites — expected)**

Run: `go build ./... 2>&1 | head`
Expected: compile errors in `internal/cashier/service/cashier.go` (missing `Role` in params). This is expected; Task A3 fixes them.

- [ ] **Step 5: Commit**

```bash
git add internal/sql/cashiers.command.sql internal/sql/cashiers.query.sql sqlc/
git commit -m "feat(cashier): thread role through sqlc queries"
```

---

### Task A3: Cashier service — role on create/update + `VerifyManager`

**Files:**
- Modify: `internal/cashier/service/cashier.go`
- Test: `internal/cashier/service/cashier_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/cashier/service/cashier_test.go`:

```go
package service

import "testing"

// validRole mirrors the DB CHECK so the service rejects bad roles before hitting
// Postgres. Kept as a tiny pure helper so it is unit-testable without a DB.
func TestValidRole(t *testing.T) {
	cases := map[string]bool{
		"cashier": true,
		"manager": true,
		"owner":   false,
		"":        false,
		"Manager": false,
	}
	for in, want := range cases {
		if got := validRole(in); got != want {
			t.Errorf("validRole(%q) = %v, want %v", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cashier/service/ -run TestValidRole -v`
Expected: FAIL — `undefined: validRole`.

- [ ] **Step 3: Implement role support in the service**

In `internal/cashier/service/cashier.go`:

(a) Add an error sentinel to the `var (...)` block:

```go
	ErrNotManager  = errors.New("cashier is not a manager")
	ErrInvalidRole = errors.New("invalid role")
```

(b) Add the helper + constants near the top (after the `var (...)` block):

```go
const (
	RoleCashier = "cashier"
	RoleManager = "manager"
)

func validRole(r string) bool {
	return r == RoleCashier || r == RoleManager
}
```

(c) Change `Create` to accept and validate `role`:

```go
func (s *Service) Create(ctx context.Context, loginID, name, pin, role string) (sqlc.Cashier, error) {
	loginID = strings.TrimSpace(loginID)
	name = strings.TrimSpace(name)
	if role == "" {
		role = RoleCashier
	}
	if !validRole(role) {
		return sqlc.Cashier{}, ErrInvalidRole
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), 10)
	if err != nil {
		return sqlc.Cashier{}, err
	}
	c, err := s.q.CreateCashier(ctx, sqlc.CreateCashierParams{
		LoginID: loginID,
		Name:    name,
		PinHash: string(hash),
		Role:    role,
	})
	if err != nil && strings.Contains(err.Error(), "cashiers_login_id_key") {
		return sqlc.Cashier{}, ErrLoginIDTaken
	}
	return c, err
}
```

(d) Change `Update` to accept and validate `role`:

```go
func (s *Service) Update(ctx context.Context, id int32, name string, active bool, role string) (sqlc.Cashier, error) {
	if role == "" {
		role = RoleCashier
	}
	if !validRole(role) {
		return sqlc.Cashier{}, ErrInvalidRole
	}
	c, err := s.q.UpdateCashier(ctx, sqlc.UpdateCashierParams{
		ID:     id,
		Name:   strings.TrimSpace(name),
		Active: active,
		Role:   role,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Cashier{}, ErrCashierNotFound
		}
		return sqlc.Cashier{}, err
	}
	return c, nil
}
```

(e) Add `VerifyManager` (reused by drawer handlers + checkout). Append to the file:

```go
// VerifyManager loads a cashier by id and confirms it is an active manager whose
// PIN matches. Used to gate drawer denomination writes from the POS, where no
// session token exists. Returns ErrInvalidCredentials on bad id/pin and
// ErrNotManager when the cashier exists but lacks the manager role.
func (s *Service) VerifyManager(ctx context.Context, id int32, pin string) (sqlc.Cashier, error) {
	c, err := s.q.GetCashier(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Cashier{}, ErrInvalidCredentials
		}
		return sqlc.Cashier{}, err
	}
	if !c.Active {
		return sqlc.Cashier{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(c.PinHash), []byte(pin)); err != nil {
		return sqlc.Cashier{}, ErrInvalidCredentials
	}
	if c.Role != RoleManager {
		return sqlc.Cashier{}, ErrNotManager
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cashier/service/ -run TestValidRole -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
go fmt ./... && git add internal/cashier/service/
git commit -m "feat(cashier): role on create/update + VerifyManager(id,pin)"
```

---

### Task A4: Cashier HTTP — expose & accept `role`

**Files:**
- Modify: `internal/cashier/http/handler.go`

- [ ] **Step 1: Add `role` to response + request DTOs**

In `internal/cashier/http/handler.go`, update `cashierResponse`:

```go
type cashierResponse struct {
	ID      int32  `json:"id"`
	LoginID string `json:"login_id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Active  bool   `json:"active"`
}
```

Update `createRequest` and `updateRequest`:

```go
type createRequest struct {
	LoginID string `json:"login_id"`
	Name    string `json:"name"`
	PIN     string `json:"pin"`
	Role    string `json:"role"`
}

type updateRequest struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Role   string `json:"role"`
}
```

- [ ] **Step 2: Build a response helper and use it everywhere**

Add a helper near the DTOs:

```go
func toCashierResponse(c sqlc.Cashier) cashierResponse {
	return cashierResponse{ID: c.ID, LoginID: c.LoginID, Name: c.Name, Role: c.Role, Active: c.Active}
}
```

Add the import `"mulan/sqlc"` to the import block. Then replace each inline `cashierResponse{ID: c.ID, ...}` construction in `Login`, `List`, `Create`, `Update` with `toCashierResponse(c)`. (In `List`, use `out[i] = toCashierResponse(c)`.)

- [ ] **Step 3: Pass role into the service calls + map ErrInvalidRole**

In `Create`, change the service call to:

```go
	c, err := h.svc.Create(r.Context(), req.LoginID, req.Name, req.PIN, req.Role)
	if err != nil {
		if errors.Is(err, service.ErrLoginIDTaken) {
			response.Error(w, r, http.StatusConflict, "login ID already in use", err)
			return
		}
		if errors.Is(err, service.ErrInvalidRole) {
			response.Error(w, r, http.StatusBadRequest, "invalid role", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to create cashier", err)
		return
	}
	response.Created(w, r, toCashierResponse(c))
```

In `Update`, change the service call to `h.svc.Update(r.Context(), int32(id), req.Name, req.Active, req.Role)` and add the same `ErrInvalidRole` → 400 mapping before the generic 500.

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 5: Commit**

```bash
go fmt ./... && git add internal/cashier/http/
git commit -m "feat(cashier): accept + expose role over HTTP"
```

---

# PHASE B — Drawer denomination model

### Task B1: Schema — denominations table + audit JSON column

**Files:**
- Modify: `schema.hcl`

- [ ] **Step 1: Add the `cash_drawer_denominations` table**

In `schema.hcl`, add a new table block (place it right after the `cash_drawer_audit` table block, ~line 301):

```hcl
table "cash_drawer_denominations" {
  schema = schema.public

  column "denomination" {
    type = integer
    null = false
  }
  column "count" {
    type    = integer
    null    = false
    default = 0
  }
  column "updated_at" {
    type    = timestamptz
    null    = false
    default = sql("now()")
  }

  primary_key {
    columns = [column.denomination]
  }
  check "cash_drawer_denominations_count_nonneg" {
    expr = "count >= 0"
  }
}
```

- [ ] **Step 2: Add the `denominations` column + new event type to `cash_drawer_audit`**

Inside `table "cash_drawer_audit"`, add a column after `terminal` (before `created_at`):

```hcl
  column "denominations" {
    type = jsonb
    null = true
  }
```

And replace the existing check:

```hcl
  check "cash_drawer_audit_event_type" {
    expr = "event_type IN ('set','clear','adjust','kick','open_for_change')"
  }
```

with:

```hcl
  check "cash_drawer_audit_event_type" {
    expr = "event_type IN ('set','clear','adjust','kick','open_for_change','sale')"
  }
```

- [ ] **Step 3: Apply + regenerate schema**

Run: `task migrate-dev && task generate-sql-schema`
Expected: Atlas adds the table, the `denominations` column, and updates the check; `schema.sql` reflects all three.

- [ ] **Step 4: Commit**

```bash
git add schema.hcl schema.sql
git commit -m "feat(cashdrawer): denominations table + audit jsonb delta + sale event"
```

---

### Task B2: (FOLDED into B3 + B5)

> **Restructured during execution to avoid a multi-task broken-build window.**
> The seed **query** (`SeedCashDrawerDenomination`) is added in B3 (with the other
> denomination SQL, so the package still builds — generated code is just unused).
> The startup seed **call** (`cashDrawerSvc.SeedDenominations(ctx)`) moves to B5,
> where the service method, the `pool` constructor change, and the `main.go` wiring
> all land together and the build stays green. No standalone B2 commit.

---

### Task B3: Denomination SQL queries (incl. seed) + regenerate

**Files:**
- Modify: `internal/sql/cash_drawer.query.sql`
- Modify: `internal/sql/cash_drawer.command.sql`

This task only adds hand-written SQL and regenerates sqlc. It adds NO Go call sites,
so the project still builds cleanly afterwards (the new generated query methods are
simply unused until B5).

- [ ] **Step 1: Add the read query**

Append to `internal/sql/cash_drawer.query.sql`:

```sql
-- name: ListCashDrawerDenominations :many
SELECT denomination, count, updated_at
FROM cash_drawer_denominations
ORDER BY denomination DESC;
```

- [ ] **Step 2: Add the write + seed queries**

Append to `internal/sql/cash_drawer.command.sql`:

```sql
-- name: SeedCashDrawerDenomination :exec
-- Idempotent insert of one denomination row; does nothing if it already exists
-- so startup can run unconditionally without clobbering live counts.
INSERT INTO cash_drawer_denominations (denomination, count)
VALUES ($1, 0)
ON CONFLICT (denomination) DO NOTHING;

-- name: SetCashDrawerDenomination :exec
-- Absolute set of one denomination's count. Negative counts are rejected by the
-- table CHECK; the caller validates before calling.
UPDATE cash_drawer_denominations
SET count = $2, updated_at = now()
WHERE denomination = $1;

-- name: AdjustCashDrawerDenomination :one
-- Relative add/remove. RETURNING lets the caller confirm the row existed; the
-- CHECK (count >= 0) makes an over-subtraction fail the statement (and tx).
UPDATE cash_drawer_denominations
SET count = count + $2, updated_at = now()
WHERE denomination = $1
RETURNING denomination, count;
```

- [ ] **Step 3: Add the audit-with-denominations insert**

Append to `internal/sql/cash_drawer.command.sql`:

```sql
-- name: AppendCashDrawerDenomEvent :one
INSERT INTO cash_drawer_audit (event_type, amount, delta, note, actor, terminal, denominations)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, event_type, amount, delta, note, actor, terminal, created_at, denominations;
```
(`denominations` is listed LAST so this query also maps to `sqlc.CashDrawerAudit` rather than a one-off `*Row` type — consistent with the existing audit queries.)

- [ ] **Step 3b: Keep the EXISTING audit queries mapping to `sqlc.CashDrawerAudit`**

> Adding the `denominations` column to `cash_drawer_audit` changes the table model
> (`sqlc.CashDrawerAudit` gains `Denominations []byte`). The two pre-existing
> queries that previously returned `sqlc.CashDrawerAudit` (`AppendCashDrawerEvent`
> RETURNING, `ListCashDrawerAudit` SELECT) will otherwise stop matching the table
> and sqlc will emit per-query `*Row` structs — breaking `toAuditEvent(sqlc.CashDrawerAudit)`
> in `internal/cashdrawer/service/service.go`. Fix: append `denominations` LAST to
> both column lists (matching the table's physical order) so they keep mapping to
> `sqlc.CashDrawerAudit`. Do NOT add interface/wrapper plumbing to service.go.

In `internal/sql/cash_drawer.command.sql`, change the existing `AppendCashDrawerEvent` RETURNING to end with `denominations`:
```sql
RETURNING id, event_type, amount, delta, note, actor, terminal, created_at, denominations;
```
In `internal/sql/cash_drawer.query.sql`, change the existing `ListCashDrawerAudit` SELECT list to end with `denominations`:
```sql
SELECT id, event_type, amount, delta, note, actor, terminal, created_at, denominations
FROM cash_drawer_audit
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;
```
(`GetCurrentCashDrawerFloat` selects only a partial column set and already returns its own Row type — leave it unchanged.) `service.go` stays exactly as it was on the base commit (original `toAuditEvent(r sqlc.CashDrawerAudit)`).

- [ ] **Step 4: Regenerate sqlc**

Run: `task sqlcgen`
Expected: `SeedCashDrawerDenomination`, `ListCashDrawerDenominations`, `SetCashDrawerDenomination`, `AdjustCashDrawerDenomination`, `AppendCashDrawerDenomEvent` generated. Note: adding `denominations` to the table means the existing `AppendCashDrawerEvent` RETURNING (which lists explicit columns) is unaffected; `CashDrawerAudit` model gains `Denominations []byte`.

- [ ] **Step 5: Build (stays clean)**

Run: `go build ./...`
Expected: success — only generated (unused) code was added.

- [ ] **Step 6: Commit**

```bash
git add internal/sql/cash_drawer.query.sql internal/sql/cash_drawer.command.sql sqlc/
git commit -m "feat(cashdrawer): denomination get/set/adjust/seed + denom audit queries"
```

---

### Task B4: Change-making DP + denomination list

**Files:**
- Create: `internal/cashdrawer/service/change.go`
- Test: `internal/cashdrawer/service/change_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cashdrawer/service/change_test.go`:

```go
package service

import (
	"reflect"
	"testing"
)

func TestMakeChangeBreakdown(t *testing.T) {
	tests := []struct {
		name        string
		changeBaht  int
		stock       map[int]int // denomBaht -> count
		wantOK      bool
		wantBreak   map[int]int
	}{
		{
			name:       "zero change is trivially makeable, empty breakdown",
			changeBaht: 0,
			stock:      map[int]int{20: 5},
			wantOK:     true,
			wantBreak:  map[int]int{},
		},
		{
			name:       "exact with plenty of stock uses fewest coins",
			changeBaht: 67,
			stock:      map[int]int{50: 2, 10: 5, 5: 5, 2: 5, 1: 5},
			wantOK:     true,
			wantBreak:  map[int]int{50: 1, 10: 1, 5: 1, 2: 1}, // 50+10+5+2 = 67
		},
		{
			name:       "greedy would fail but DP finds it",
			changeBaht: 60,
			stock:      map[int]int{50: 1, 20: 3}, // greedy takes 50 then stuck; 20*3 works
			wantOK:     true,
			wantBreak:  map[int]int{20: 3},
		},
		{
			name:       "infeasible: cannot reach amount with stock",
			changeBaht: 8,
			stock:      map[int]int{50: 1}, // no small coins
			wantOK:     false,
			wantBreak:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBreak, gotOK := makeChangeBaht(tc.changeBaht, tc.stock)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if tc.wantOK && !reflect.DeepEqual(gotBreak, tc.wantBreak) {
				t.Fatalf("breakdown = %v, want %v", gotBreak, tc.wantBreak)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cashdrawer/service/ -run TestMakeChangeBreakdown -v`
Expected: FAIL — `undefined: makeChangeBaht`.

- [ ] **Step 3: Implement the DP**

Create `internal/cashdrawer/service/change.go`:

```go
package service

// DenominationsSatang lists the nine tracked THB denominations, in satang,
// largest first. This is the canonical order used for storage, display, and the
// change-making DP.
var DenominationsSatang = []int64{100000, 50000, 10000, 5000, 2000, 1000, 500, 200, 100}

// denomsBaht is DenominationsSatang expressed in whole baht (since cash due is
// rounded to ฿1, every change amount is a whole-baht multiple).
var denomsBaht = []int{1000, 500, 100, 50, 20, 10, 5, 2, 1}

// makeChangeBaht returns the minimum-count breakdown (denomBaht -> count) that
// forms changeBaht using only the supplied stock, and whether it is possible.
//
// It is a bounded coin-change DP: dp[v] is the fewest bills/coins to form v, and
// from[v] records the denomination taken last to reach v. Each denomination is
// processed once with a per-pass usage counter (num[]) so its stock limit is
// respected. Greedy-largest-first is deliberately NOT used: with limited stock it
// can wrongly report "impossible" when a valid combination exists, which would
// block a makeable sale.
func makeChangeBaht(changeBaht int, stock map[int]int) (map[int]int, bool) {
	if changeBaht == 0 {
		return map[int]int{}, true
	}
	const inf = 1 << 30
	dp := make([]int, changeBaht+1)
	from := make([]int, changeBaht+1)
	for i := 1; i <= changeBaht; i++ {
		dp[i] = inf
		from[i] = -1
	}
	for _, d := range denomsBaht {
		c := stock[d]
		if c <= 0 || d > changeBaht {
			continue
		}
		num := make([]int, changeBaht+1) // count of d used to reach v in this pass
		for v := d; v <= changeBaht; v++ {
			if dp[v-d] == inf {
				continue
			}
			if dp[v-d]+1 < dp[v] && num[v-d]+1 <= c {
				dp[v] = dp[v-d] + 1
				num[v] = num[v-d] + 1
				from[v] = d
			}
		}
	}
	if dp[changeBaht] >= inf {
		return nil, false
	}
	out := make(map[int]int)
	for v := changeBaht; v > 0; {
		d := from[v]
		out[d]++
		v -= d
	}
	return out, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cashdrawer/service/ -run TestMakeChangeBreakdown -v`
Expected: PASS (all four sub-tests).

- [ ] **Step 5: Commit**

```bash
go fmt ./... && git add internal/cashdrawer/service/change.go internal/cashdrawer/service/change_test.go
git commit -m "feat(cashdrawer): bounded-DP change maker"
```

---

### Task B5: Denomination service — Current / Seed / Set / Adjust / MakeChange / ApplyCashSale

**Files:**
- Create: `internal/cashdrawer/service/denominations.go`
- Test: `internal/cashdrawer/service/denominations_test.go`

- [ ] **Step 1: Write the failing test (pure helpers only)**

The DB-touching methods are exercised by the checkout integration test (Phase D) and manual verification; here we unit-test the pure delta/JSON helpers. Create `internal/cashdrawer/service/denominations_test.go`:

```go
package service

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDenomDeltaJSON(t *testing.T) {
	delta := map[int64]int{10000: 1, 500: -2}
	b, err := denomDeltaJSON(delta)
	if err != nil {
		t.Fatalf("denomDeltaJSON: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]int{"10000": 1, "500": -2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json = %v, want %v", got, want)
	}
}

func TestTotalSatang(t *testing.T) {
	counts := map[int64]int{100000: 2, 10000: 3, 100: 5} // 2000 + 300 + 5 = 2305 baht
	if got := totalSatang(counts); got != 230500 {
		t.Fatalf("totalSatang = %d, want 230500", got)
	}
}

func TestChangeStockBaht(t *testing.T) {
	counts := map[int64]int{50000: 1, 2000: 3} // ฿500 x1, ฿20 x3
	got := changeStockBaht(counts)
	want := map[int]int{500: 1, 20: 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stock = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cashdrawer/service/ -run 'TestDenomDeltaJSON|TestTotalSatang|TestChangeStockBaht' -v`
Expected: FAIL — undefined helpers.

- [ ] **Step 3: Implement the service**

Create `internal/cashdrawer/service/denominations.go`:

```go
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

// ErrChangeNotMakeable is returned when the drawer cannot form the requested
// change from current stock. The HTTP layer maps it to 409 so the POS can prompt
// a manager to restock.
var ErrChangeNotMakeable = errors.New("cannot make exact change from drawer")

// DenomState is one denomination's current count, satang-keyed.
type DenomState struct {
	Denomination int64
	Count        int
}

// SeedDenominations inserts the nine tracked denomination rows if missing. Safe
// to call on every startup (ON CONFLICT DO NOTHING).
func (s *Service) SeedDenominations(ctx context.Context) error {
	for _, d := range DenominationsSatang {
		if err := s.q.SeedCashDrawerDenomination(ctx, int32(d)); err != nil {
			return fmt.Errorf("seed denom %d: %w", d, err)
		}
	}
	return nil
}

// CurrentDenoms returns the current count per denomination and the derived total
// in satang.
func (s *Service) CurrentDenoms(ctx context.Context) (map[int64]int, int64, error) {
	rows, err := s.q.ListCashDrawerDenominations(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list denominations: %w", err)
	}
	counts := make(map[int64]int, len(rows))
	for _, r := range rows {
		counts[int64(r.Denomination)] = int(r.Count)
	}
	return counts, totalSatang(counts), nil
}

// SetDenoms writes an absolute count for every supplied denomination, records the
// signed delta vs the previous state into the audit log, and returns the new
// total. Unknown denomination keys are rejected. Runs in a transaction so the
// state and audit row commit together.
func (s *Service) SetDenoms(ctx context.Context, counts map[int64]int, actor string) (int64, error) {
	if err := validateDenomKeys(counts); err != nil {
		return 0, err
	}
	for _, v := range counts {
		if v < 0 {
			return 0, fmt.Errorf("count must be >= 0")
		}
	}
	prev, _, err := s.CurrentDenoms(ctx)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	delta := make(map[int64]int)
	for _, d := range DenominationsSatang {
		newC, ok := counts[d]
		if !ok {
			newC = prev[d] // unspecified denominations keep their current count
		}
		if diff := newC - prev[d]; diff != 0 {
			delta[d] = diff
		}
		if err := q.SetCashDrawerDenomination(ctx, sqlc.SetCashDrawerDenominationParams{
			Denomination: int32(d),
			Count:        int32(newC),
		}); err != nil {
			return 0, fmt.Errorf("set denom %d: %w", d, err)
		}
	}
	if err := appendDenomAudit(ctx, q, "set", delta, actor); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	newCounts, _, err := s.CurrentDenoms(ctx)
	if err != nil {
		return 0, err
	}
	return totalSatang(newCounts), nil
}

// AdjustDenoms applies a relative change per denomination (add a coin roll, remove
// a bundle). A subtraction that would drive a count below zero fails the tx (table
// CHECK). Records the delta as an 'adjust' audit row.
func (s *Service) AdjustDenoms(ctx context.Context, delta map[int64]int, actor string) (int64, error) {
	if err := validateDenomKeys(delta); err != nil {
		return 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	applied := make(map[int64]int)
	for _, d := range DenominationsSatang {
		diff, ok := delta[d]
		if !ok || diff == 0 {
			continue
		}
		if _, err := q.AdjustCashDrawerDenomination(ctx, sqlc.AdjustCashDrawerDenominationParams{
			Denomination: int32(d),
			Delta:        int32(diff),
		}); err != nil {
			return 0, fmt.Errorf("adjust denom %d (count would go negative?): %w", d, err)
		}
		applied[d] = diff
	}
	if err := appendDenomAudit(ctx, q, "adjust", applied, actor); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	newCounts, _, err := s.CurrentDenoms(ctx)
	if err != nil {
		return 0, err
	}
	return totalSatang(newCounts), nil
}

// MakeChange computes the bills/coins to return for changeSatang against current
// stock. changeSatang must be a whole-baht multiple (cash due is rounded to ฿1).
// Returns ErrChangeNotMakeable when the drawer cannot form it.
func (s *Service) MakeChange(ctx context.Context, changeSatang int64) (map[int64]int, error) {
	counts, _, err := s.CurrentDenoms(ctx)
	if err != nil {
		return nil, err
	}
	breakBaht, ok := makeChangeBaht(int(changeSatang/100), changeStockBaht(counts))
	if !ok {
		return nil, ErrChangeNotMakeable
	}
	out := make(map[int64]int, len(breakBaht))
	for dBaht, n := range breakBaht {
		out[int64(dBaht)*100] = n
	}
	return out, nil
}

// ApplyCashSale adds the tendered denominations and removes the change
// denominations from the drawer, then appends a 'sale' audit row with the net
// delta. It MUST be called with a tx-bound *sqlc.Queries (the checkout tx) so the
// drawer movement commits atomically with the order. A change denom exceeding
// stock fails the tx via the CHECK; callers run MakeChange first so this is an
// invariant guard, not the primary feasibility check.
func (s *Service) ApplyCashSale(ctx context.Context, q *sqlc.Queries, tender, change map[int64]int, actor string) error {
	net := make(map[int64]int)
	for d, n := range tender {
		net[d] += n
	}
	for d, n := range change {
		net[d] -= n
	}
	for _, d := range DenominationsSatang {
		diff := net[d]
		if diff == 0 {
			continue
		}
		if _, err := q.AdjustCashDrawerDenomination(ctx, sqlc.AdjustCashDrawerDenominationParams{
			Denomination: int32(d),
			Delta:        int32(diff),
		}); err != nil {
			return fmt.Errorf("apply cash sale denom %d: %w", d, err)
		}
	}
	return appendDenomAudit(ctx, q, "sale", net, actor)
}

// ── helpers ────────────────────────────────────────────────────────

func totalSatang(counts map[int64]int) int64 {
	var t int64
	for d, c := range counts {
		t += d * int64(c)
	}
	return t
}

func changeStockBaht(counts map[int64]int) map[int]int {
	out := make(map[int]int, len(counts))
	for d, c := range counts {
		out[int(d/100)] = c
	}
	return out
}

func validateDenomKeys(m map[int64]int) error {
	valid := make(map[int64]struct{}, len(DenominationsSatang))
	for _, d := range DenominationsSatang {
		valid[d] = struct{}{}
	}
	for d := range m {
		if _, ok := valid[d]; !ok {
			return fmt.Errorf("unknown denomination: %d", d)
		}
	}
	return nil
}

func denomDeltaJSON(delta map[int64]int) ([]byte, error) {
	m := make(map[string]int, len(delta))
	for d, n := range delta {
		m[strconv.FormatInt(d, 10)] = n
	}
	return json.Marshal(m)
}

// appendDenomAudit writes an audit row carrying the signed per-denom delta and
// the net satang amount moved.
func appendDenomAudit(ctx context.Context, q *sqlc.Queries, eventType string, delta map[int64]int, actor string) error {
	js, err := denomDeltaJSON(delta)
	if err != nil {
		return fmt.Errorf("encode denom delta: %w", err)
	}
	var net int64
	for d, n := range delta {
		net += d * int64(n)
	}
	var actorText pgtype.Text
	if actor != "" {
		actorText = pgtype.Text{String: actor, Valid: true}
	}
	_, err = q.AppendCashDrawerDenomEvent(ctx, sqlc.AppendCashDrawerDenomEventParams{
		EventType:     eventType,
		Delta:         pgtype.Int8{Int64: net, Valid: true},
		Actor:         actorText,
		Denominations: js,
	})
	if err != nil {
		return fmt.Errorf("append %s audit: %w", eventType, err)
	}
	return nil
}
```

- [ ] **Step 4: Add `pool` to the Service struct**

The new methods need a `*pgxpool.Pool` for their own transactions. In `internal/cashdrawer/service/service.go`, update the struct + constructor:

```go
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewService(pool *pgxpool.Pool, q *sqlc.Queries) *Service {
	return &Service{pool: pool, q: q}
}
```

Add `"github.com/jackc/pgx/v5/pgxpool"` to that file's imports.

- [ ] **Step 5: Update the constructor call + seed at startup in main.go**

In `main.go`, change `cashDrawerSvc := cashdrawerservice.NewService(queries)` to `cashDrawerSvc := cashdrawerservice.NewService(pool, queries)`.

Then, immediately AFTER that line, add the startup seed call (folded here from former Task B2 so the build stays green):

```go
	if err := cashDrawerSvc.SeedDenominations(context.Background()); err != nil {
		log.Fatalf("seed cash drawer denominations: %v", err)
	}
```

Use whatever startup context the surrounding `main()` already uses — if there's an existing `ctx` in scope at that point, use it instead of `context.Background()`. Ensure `context` and `log` are imported (they already are in `main.go`).

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/cashdrawer/service/ -run 'TestDenomDeltaJSON|TestTotalSatang|TestChangeStockBaht' -v`
Expected: PASS.

Run: `go build ./...`
Expected: success — the `SeedDenominations` call resolves and the seed runs at startup.

- [ ] **Step 7: Commit**

```bash
go fmt ./... && git add internal/cashdrawer/service/ main.go
git commit -m "feat(cashdrawer): denomination service (current/set/adjust/makechange/applysale)"
```

---

# PHASE C — Drawer denomination HTTP API

### Task C1: Denomination handlers + role/PIN gate

**Files:**
- Create: `internal/cashdrawer/http/denominations.go`
- Modify: `internal/cashdrawer/http/handler.go` (wire routes + cashier verifier)
- Modify: `main.go` (pass cashier service to the drawer handler)

- [ ] **Step 1: Define a verifier interface + extend the handler**

In `internal/cashdrawer/http/handler.go`, add an interface and a field. Add near the top (after imports):

```go
// ManagerVerifier authorizes drawer writes by re-checking a cashier's id + PIN
// and manager role (POS has no session token). Implemented by cashier/service.
type ManagerVerifier interface {
	VerifyManager(ctx context.Context, id int32, pin string) (sqlc.Cashier, error)
}
```

Add imports `"context"` and `"mulan/sqlc"` to that file. Change the `Handler` struct + constructor:

```go
type Handler struct {
	svc      *service.Service
	verifier ManagerVerifier
}

func NewHandler(svc *service.Service, verifier ManagerVerifier) *Handler {
	return &Handler{svc: svc, verifier: verifier}
}
```

Register the new routes inside `Routes`:

```go
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.current)
	r.Put("/float", h.setFloat)
	r.Delete("/float", h.clearFloat)
	r.Post("/kick", h.logKick)
	r.Get("/audit", h.listAudit)
	r.Get("/denominations", h.getDenominations)
	r.Put("/denominations", h.setDenominations)
	r.Post("/denominations/adjust", h.adjustDenominations)
	r.Post("/change-preview", h.changePreview)
}
```

- [ ] **Step 2: Implement the denomination handlers**

Create `internal/cashdrawer/http/denominations.go`:

```go
package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/Rhymond/go-money"

	cashiersvc "mulan/internal/cashier/service"
	"mulan/internal/cashdrawer/service"
	"mulan/internal/response"
)

// denomsResponse reports current counts (satang-keyed via string) and the derived
// total in THB.
type denomsResponse struct {
	Counts map[string]int `json:"counts"`
	Total  float64        `json:"total"`
}

func toDenomsResponse(counts map[int64]int, totalSatang int64) denomsResponse {
	out := denomsResponse{Counts: make(map[string]int, len(counts))}
	for d, c := range counts {
		out.Counts[itoa(d)] = c
	}
	out.Total = money.New(totalSatang, money.THB).AsMajorUnits()
	return out
}

func (h *Handler) getDenominations(w http.ResponseWriter, r *http.Request) {
	counts, total, err := h.svc.CurrentDenoms(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to read denominations", err)
		return
	}
	response.OK(w, r, toDenomsResponse(counts, total))
}

type denomWriteRequest struct {
	CashierID int32          `json:"cashier_id"`
	PIN       string         `json:"pin"`
	Counts    map[string]int `json:"counts"` // for PUT (absolute)
	Delta     map[string]int `json:"delta"`  // for adjust (relative)
}

// authorize verifies the request's cashier_id + pin belong to an active manager
// and returns the actor name to stamp on the audit row.
func (h *Handler) authorize(w http.ResponseWriter, r *http.Request, id int32, pin string) (string, bool) {
	c, err := h.verifier.VerifyManager(r.Context(), id, pin)
	if err != nil {
		if errors.Is(err, cashiersvc.ErrInvalidCredentials) || errors.Is(err, cashiersvc.ErrNotManager) {
			response.Error(w, r, http.StatusForbidden, "manager id + PIN required", err)
			return "", false
		}
		response.Error(w, r, http.StatusInternalServerError, "authorization failed", err)
		return "", false
	}
	return c.Name, true
}

func (h *Handler) setDenominations(w http.ResponseWriter, r *http.Request) {
	var req denomWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	actor, ok := h.authorize(w, r, req.CashierID, req.PIN)
	if !ok {
		return
	}
	counts, err := parseDenomMap(req.Counts)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	total, err := h.svc.SetDenoms(r.Context(), counts, actor)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "failed to set denominations", err)
		return
	}
	newCounts, _, _ := h.svc.CurrentDenoms(r.Context())
	response.OK(w, r, toDenomsResponse(newCounts, total))
}

func (h *Handler) adjustDenominations(w http.ResponseWriter, r *http.Request) {
	var req denomWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	actor, ok := h.authorize(w, r, req.CashierID, req.PIN)
	if !ok {
		return
	}
	delta, err := parseDenomMap(req.Delta)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	total, err := h.svc.AdjustDenoms(r.Context(), delta, actor)
	if err != nil {
		response.Error(w, r, http.StatusConflict, "adjust failed (insufficient stock?)", err)
		return
	}
	newCounts, _, _ := h.svc.CurrentDenoms(r.Context())
	response.OK(w, r, toDenomsResponse(newCounts, total))
}

type changePreviewRequest struct {
	Due    float64        `json:"due"`    // THB amount due (POS estimate)
	Tender map[string]int `json:"tender"` // denom satang string -> count
}

type changePreviewResponse struct {
	RoundedDue  float64        `json:"rounded_due"`
	ChangeTotal float64        `json:"change_total"`
	Breakdown   map[string]int `json:"breakdown"`
	Makeable    bool           `json:"makeable"`
}

func (h *Handler) changePreview(w http.ResponseWriter, r *http.Request) {
	var req changePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	tender, err := parseDenomMap(req.Tender)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	roundedDue := roundToBahtSatang(satangFromTHB(req.Due))
	var tenderSatang int64
	for d, c := range tender {
		tenderSatang += d * int64(c)
	}
	out := changePreviewResponse{
		RoundedDue: money.New(roundedDue, money.THB).AsMajorUnits(),
		Breakdown:  map[string]int{},
	}
	if tenderSatang < roundedDue {
		out.Makeable = false
		out.ChangeTotal = money.New(tenderSatang-roundedDue, money.THB).AsMajorUnits()
		response.OK(w, r, out)
		return
	}
	changeSatang := tenderSatang - roundedDue
	out.ChangeTotal = money.New(changeSatang, money.THB).AsMajorUnits()
	breakdown, err := h.svc.MakeChange(r.Context(), changeSatang)
	if err != nil {
		out.Makeable = false
		response.OK(w, r, out)
		return
	}
	out.Makeable = true
	for d, n := range breakdown {
		out.Breakdown[itoa(d)] = n
	}
	response.OK(w, r, out)
}

// roundToBahtSatang rounds a satang amount to the nearest whole baht (100 satang).
func roundToBahtSatang(satang int64) int64 {
	return int64(math.Round(float64(satang)/100.0)) * 100
}
```

- [ ] **Step 3: Add the shared parse/format helpers**

Append to `internal/cashdrawer/http/denominations.go`:

```go
import "strconv" // add to the import block above, not a second block

func itoa(d int64) string { return strconv.FormatInt(d, 10) }

// parseDenomMap converts a JSON object keyed by satang strings into an int64-keyed
// map, rejecting non-numeric keys.
func parseDenomMap(in map[string]int) (map[int64]int, error) {
	out := make(map[int64]int, len(in))
	for k, v := range in {
		d, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			return nil, errInvalidDenomKey
		}
		out[d] = v
	}
	return out, nil
}

var errInvalidDenomKey = errors.New("invalid denomination key")
```

(Place the single `strconv` import alongside the existing imports rather than as a separate statement — the inline `import` line above is a reminder, not literal placement. Consolidate all imports into the one block at the top of the file.)

- [ ] **Step 4: Wire the cashier service into the drawer handler in main.go**

In `main.go`, change `cashDrawerHandler := cashdrawerhttp.NewHandler(cashDrawerSvc)` to:

```go
	cashDrawerHandler := cashdrawerhttp.NewHandler(cashDrawerSvc, cashierSvc)
```

Ensure `cashierSvc` is constructed before this line (it is, at ~line 110, before ~line 117).

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Manual smoke test (record the before/after)**

Start the server (`go run .`) against the dev DB, then in another shell:

```bash
# read (open, no auth) — expect 9 denominations, total 0
curl -s localhost:8085/api/cash-drawer/denominations | jq
# write without PIN — expect 403
curl -s -X PUT localhost:8085/api/cash-drawer/denominations \
  -H 'Content-Type: application/json' \
  -d '{"cashier_id":1,"pin":"0000","counts":{"10000":10,"100":20}}' -i | head -1
```

Expected: first returns `{"counts":{...},"total":0}`; second returns `403` (unless cashier 1 is a manager with PIN 0000). Create a manager cashier via the API to test the success path:

```bash
curl -s -X POST localhost:8085/api/cash-drawer/change-preview \
  -H 'Content-Type: application/json' \
  -d '{"due":47.25,"tender":{"5000":1}}' | jq
```

Expected: `rounded_due: 47`, `change_total: 3`, and `makeable:false` if the drawer is empty (no coins) — confirming block-until-restocked.

- [ ] **Step 7: Commit**

```bash
go fmt ./... && git add internal/cashdrawer/http/ main.go
git commit -m "feat(cashdrawer): denomination + change-preview endpoints (manager id+PIN gated)"
```

---

# PHASE D — Checkout integration

### Task D1: `Checkout` accepts cash tender + applies the sale in-tx

**Files:**
- Modify: `internal/order/service/order.go`
- Modify: `main.go` (inject cashdrawer service into OrderService)

- [ ] **Step 1: Add sentinel errors + a drawer applier interface**

In `internal/order/service/order.go`, add to the `var (...)` sentinel block:

```go
	ErrShortTender     = errors.New("cash tendered is less than amount due")
	ErrChangeNotMakeable = errors.New("cannot make change from drawer")
```

Add an interface + a field to `OrderService`. After the imports, add:

```go
// CashDrawer is the subset of cashdrawer/service used at checkout. Defined here
// (consumer side) to avoid a hard import cycle and keep the dependency explicit.
type CashDrawer interface {
	MakeChange(ctx context.Context, changeSatang int64) (map[int64]int, error)
	ApplyCashSale(ctx context.Context, q *sqlc.Queries, tender, change map[int64]int, actor string) error
}
```

Update the struct + constructor:

```go
type OrderService struct {
	pool     *pgxpool.Pool
	q        *sqlc.Queries
	settings *settingsservice.SettingsService
	drawer   CashDrawer
}

func NewOrderService(pool *pgxpool.Pool, q *sqlc.Queries, settings *settingsservice.SettingsService, drawer CashDrawer) *OrderService {
	return &OrderService{pool: pool, q: q, settings: settings, drawer: drawer}
}
```

- [ ] **Step 2: Add a cash-tender parameter to `Checkout`**

Add a payment input type (after `CustomerInput`):

```go
// CashPayment carries the per-denomination tender for a cash sale. Empty (nil or
// IsCash=false) means a non-cash sale and the drawer is untouched. Tender is
// satang-keyed denomination -> count.
type CashPayment struct {
	IsCash bool
	Tender map[int64]int
	Actor  string
}
```

Change the `Checkout` signature to accept it:

```go
func (s *OrderService) Checkout(ctx context.Context, code string, items []CheckoutItemInput, orderDiscountIDs []int32, customer CustomerInput, cash CashPayment) (*domain.CheckoutResult, error) {
```

- [ ] **Step 3: Apply the cash sale inside the existing tx**

In `Checkout`, locate the block that computes `totalSatang := t.CustomerPays` (just after `computeOrderTotals`). Immediately **after** `totalSatang := t.CustomerPays` and **before** the `var memberID ...` member block, insert:

```go
	// Cash payment: round the amount due to whole baht, validate tender, compute
	// the change breakdown against live drawer stock, and apply the movement in
	// this same transaction so the drawer can never drift from the sale.
	var roundedDueSatang, changeSatang int64
	var changeBreakdown map[int64]int
	if cash.IsCash {
		roundedDueSatang = roundToBaht(totalSatang)
		var tenderSatang int64
		for d, c := range cash.Tender {
			tenderSatang += d * int64(c)
		}
		if tenderSatang < roundedDueSatang {
			return nil, ErrShortTender
		}
		changeSatang = tenderSatang - roundedDueSatang
		changeBreakdown, err = s.drawer.MakeChange(ctx, changeSatang)
		if err != nil {
			return nil, ErrChangeNotMakeable
		}
		if err := s.drawer.ApplyCashSale(ctx, q, cash.Tender, changeBreakdown, cash.Actor); err != nil {
			return nil, fmt.Errorf("apply cash sale: %w", err)
		}
	}
```

Add the `roundToBaht` helper near the other helpers at the bottom of the file:

```go
// roundToBaht rounds a satang amount to the nearest whole baht (100 satang).
// Cash is settled in whole baht because the smallest tracked coin is ฿1.
func roundToBaht(satang int64) int64 {
	r := satang % 100
	if r == 0 {
		return satang
	}
	if r >= 50 {
		return satang - r + 100
	}
	return satang - r
}
```

Note: `MakeChange` reads current stock via the **non-tx** service `q`, which is acceptable on a single terminal (no concurrent checkout). `ApplyCashSale` uses the tx-bound `q` so the write is atomic. The CHECK on `count` makes any miscalculation abort the whole checkout.

- [ ] **Step 4: Return rounded due + change in the result**

Add fields to `domain.CheckoutResult` (in `internal/order/domain/`). Open the file defining `CheckoutResult` (find with `grep -rn "type CheckoutResult" internal/order/domain/`) and add:

```go
	RoundedDue      float64        `json:"rounded_due"`
	Change          float64        `json:"change"`
	ChangeBreakdown map[string]int `json:"change_breakdown"`
```

Then in `Checkout`'s final `return &domain.CheckoutResult{...}`, add:

```go
		RoundedDue:      float64(roundedDueSatang) / 100,
		Change:          float64(changeSatang) / 100,
		ChangeBreakdown: breakdownToStringMap(changeBreakdown),
```

Add the helper at the bottom of `order.go`:

```go
// breakdownToStringMap converts a satang-keyed change breakdown to the
// string-keyed shape the JSON API uses (nil-safe → empty map).
func breakdownToStringMap(b map[int64]int) map[string]int {
	out := make(map[string]int, len(b))
	for d, n := range b {
		out[strconv.FormatInt(d, 10)] = n
	}
	return out
}
```

Add `"strconv"` to the imports of `order.go`.

- [ ] **Step 5: Inject the drawer service in main.go**

In `main.go`, the construction order must be: build `cashDrawerSvc` **before** `orderSvc`. Move `cashDrawerSvc := cashdrawerservice.NewService(pool, queries)` (and its `SeedDenominations` call) above the `orderSvc := orderservice.NewOrderService(...)` line (~line 100). Then change the orderSvc construction to:

```go
	orderSvc := orderservice.NewOrderService(pool, queries, settingsSvc, cashDrawerSvc)
```

- [ ] **Step 6: Build (handler call site will fail — expected)**

Run: `go build ./... 2>&1 | head`
Expected: error in `internal/order/http/handler.go` — `Checkout` now wants a 6th arg. Fixed in D2.

- [ ] **Step 7: Commit**

```bash
go fmt ./... && git add internal/order/service/order.go internal/order/domain/ main.go
git commit -m "feat(order): cash tender → drawer auto-adjust + change in checkout tx"
```

---

### Task D2: Checkout HTTP — accept `cash_tender`, return change breakdown

**Files:**
- Modify: `internal/order/http/handler.go`

- [ ] **Step 1: Extend the checkout request DTO**

In `internal/order/http/handler.go`, add fields to `checkoutRequest`:

```go
type checkoutRequest struct {
	Items         []checkoutItemRequest `json:"items"`
	DiscountIDs   []int32               `json:"discount_ids"`
	CustomerPhone string                `json:"customer_phone"`
	CustomerName  string                `json:"customer_name"`
	PaymentMethod string                `json:"payment_method"`
	CashTender    map[string]int        `json:"cash_tender"` // satang string -> count
	CashierName   string                `json:"cashier_name"`
}
```

- [ ] **Step 2: Build the `CashPayment` and pass it to the service**

In `checkout`, after building `items` and before the `result, err := h.svc.Checkout(...)` call, add:

```go
	cash := service.CashPayment{}
	if req.PaymentMethod == "cash" {
		tender := make(map[int64]int, len(req.CashTender))
		for k, v := range req.CashTender {
			d, perr := strconv.ParseInt(k, 10, 64)
			if perr != nil || !trackedDenom(d) {
				response.Error(w, r, http.StatusBadRequest, "invalid denomination key", perr)
				return
			}
			if v < 0 {
				response.Error(w, r, http.StatusBadRequest, "tender count must be >= 0", nil)
				return
			}
			tender[d] = v
		}
		cash = service.CashPayment{IsCash: true, Tender: tender, Actor: req.CashierName}
	}
```

Add a small file-local helper (and import the cashdrawer service for the denomination
set) so an untracked tender denomination can't slip past the authoritative backend
(it would otherwise be summed into the tender total but silently dropped by
`ApplyCashSale`, which only iterates the nine tracked denominations — a drawer/sale
mismatch):

```go
import cashdrawerservice "mulan/internal/cashdrawer/service"

// trackedDenom reports whether d (satang) is one of the nine tracked denominations.
func trackedDenom(d int64) bool {
	for _, x := range cashdrawerservice.DenominationsSatang {
		if x == d {
			return true
		}
	}
	return false
}
```

Change the service call to:

```go
	result, err := h.svc.Checkout(r.Context(), code, items, req.DiscountIDs, service.CustomerInput{
		Phone: req.CustomerPhone,
		Name:  req.CustomerName,
	}, cash)
```

Add `"strconv"` to the import block.

- [ ] **Step 3: Map the new errors**

In `classifyCheckoutError`, add cases before the `default`:

```go
	case errors.Is(err, service.ErrShortTender):
		return http.StatusBadRequest, "cash tendered is less than amount due"
	case errors.Is(err, service.ErrChangeNotMakeable):
		return http.StatusConflict, "cannot make exact change — restock the drawer"
```

- [ ] **Step 4: Add the new fields to the response DTO + mapping**

Add to `checkoutResponse`:

```go
	RoundedDue      float64        `json:"rounded_due"`
	Change          float64        `json:"change"`
	ChangeBreakdown map[string]int `json:"change_breakdown"`
```

In the final `response.OK(w, r, checkoutResponse{...})`, add:

```go
		RoundedDue:      result.RoundedDue,
		Change:          result.Change,
		ChangeBreakdown: result.ChangeBreakdown,
```

- [ ] **Step 5: Build**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Update existing checkout tests if they call `Checkout` directly**

Run: `go test ./internal/order/... 2>&1 | head -30`
Expected: `order_test.go` may fail to compile because `Checkout` gained a parameter. If so, update each call site in `internal/order/service/order_test.go` to pass `service.CashPayment{}` (non-cash) as the final argument, e.g.:

```go
res, err := svc.Checkout(ctx, code, items, nil, CustomerInput{}, CashPayment{})
```

Re-run: `go test ./internal/order/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
go fmt ./... && git add internal/order/http/handler.go internal/order/service/order_test.go
git commit -m "feat(order): checkout HTTP accepts cash_tender, returns change breakdown"
```

---

### Task D3: Integration verification — cash sale adjusts the drawer atomically

**Files:**
- Manual verification (no new file); record before/after.

- [ ] **Step 1: Seed a manager cashier + stock the drawer**

With the server running on dev DB:

```bash
# create a manager cashier (owner manager-auth required for POST /api/cashiers;
# for local dev you can insert directly if no owner token handy):
#   psql "$PSQL_URL" -c "UPDATE cashiers SET role='manager' WHERE id=1;"
# stock the drawer via the API (manager id+PIN):
curl -s -X PUT localhost:8085/api/cash-drawer/denominations \
  -H 'Content-Type: application/json' \
  -d '{"cashier_id":1,"pin":"<PIN>","counts":{"100000":5,"50000":5,"10000":10,"5000":10,"2000":10,"1000":20,"500":20,"200":20,"100":50}}' | jq
```

Expected: returns counts + a non-zero `total`.

- [ ] **Step 2: Create an order, then checkout with cash tender**

```bash
CODE=$(curl -s -X POST localhost:8085/api/orders | jq -r .data.code)
# (add a known item via the POS or directly; assume total ~฿47.25 → rounds to ฿47)
curl -s -X POST localhost:8085/api/orders/$CODE/checkout \
  -H 'Content-Type: application/json' \
  -d "{\"payment_method\":\"cash\",\"cash_tender\":{\"5000\":1},\"cashier_name\":\"test\",\"items\":[{\"menu_id\":<ID>,\"qty\":1,\"option_ids\":[],\"discount_ids\":[]}]}" | jq '{rounded_due,change,change_breakdown}'
```

Expected: `rounded_due: 47`, `change: 3`, `change_breakdown: {"100":3}` (three ฿1) or `{"200":1,"100":1}` depending on min-coin (DP minimises count → `{"200":1,"100":1}` = one ฿2 + one ฿1 = 2 coins, fewer than three ฿1). Confirm the breakdown sums to ฿3.

- [ ] **Step 2b: Confirm the drawer changed**

```bash
curl -s localhost:8085/api/cash-drawer/denominations | jq
psql "$PSQL_URL" -c "SELECT event_type, delta, denominations FROM cash_drawer_audit ORDER BY id DESC LIMIT 1;"
```

Expected: the ฿50 count increased by 1; the change denominations decreased; a `sale` audit row with a `denominations` JSON net delta and `delta` = `4700` (net satang into the drawer).

- [ ] **Step 3: Confirm block-until-restocked**

Empty the small coins (`PUT` counts with `"100":0,"200":0,...`), then checkout a sale needing ฿3 change:

Expected: HTTP `409` `"cannot make exact change — restock the drawer"`, and the order remains unpaid (`SELECT status FROM orders WHERE code=...` → `open`), proving the tx rolled back.

- [ ] **Step 4: No code change — note the verification in the commit trailer of the next task.**

---

# PHASE E — POS UI (mulan-agent)

> Frontend tasks are verified in the browser at the POS viewport (per fullstack_dev), not by unit tests. Each task ends by loading `/pos` and exercising the flow.

### Task E1: Agent checkout proxy forwards cash tender + reads change breakdown

**Files:**
- Modify: `mulan-agent/main.go`

- [ ] **Step 1: Add tender fields to the agent's checkout request**

In `mulan-agent/main.go`, the `checkoutRequest` struct (~line 306) has `CashTendered`/`CashChange float64`. Add:

```go
	CashTender  map[string]int `json:"cash_tender,omitempty"`  // satang string -> count
	CashierName string         `json:"cashier_name,omitempty"`
```

- [ ] **Step 2: Forward them to the backend**

Find `callCheckout(apiBase, req.OrderCode, req.Items, req.DiscountIDs, req.CustomerPhone, req.CustomerName)` (~line 367) and its definition (~line 424). Change the signature and body of `callCheckout` to also take + send `paymentMethod string, cashTender map[string]int, cashierName string`, adding them to the JSON body it POSTs:

```go
func callCheckout(apiBase, code string, items any, discountIDs []int32, customerPhone, customerName, paymentMethod string, cashTender map[string]int, cashierName string) (*checkoutResponse, error) {
	payload := map[string]any{
		"items":          items,
		"discount_ids":   discountIDs,
		"customer_phone": customerPhone,
		"customer_name":  customerName,
		"payment_method": paymentMethod,
		"cash_tender":    cashTender,
		"cashier_name":   cashierName,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(apiBase+"/api/orders/"+code+"/checkout", "application/json", bytes.NewReader(body))
	// ...rest unchanged...
}
```

(If `callCheckout` currently builds the body differently, adapt to send the same keys. Keep the existing response decode + `checkoutEnvelope` logic.)

Update the call site (~line 367):

```go
	result, err := callCheckout(apiBase, req.OrderCode, req.Items, req.DiscountIDs, req.CustomerPhone, req.CustomerName, req.PaymentMethod, req.CashTender, req.CashierName)
```

- [ ] **Step 3: Propagate a 409 (not-makeable) back to the POS instead of a generic 502**

Where `callCheckout` returns an error and the handler does `http.Error(w, "checkout failed", http.StatusBadGateway)` (~line 370), make `callCheckout` surface the upstream status. Simplest: have `callCheckout` return the HTTP status code; when it is `409`, respond `http.Error(w, "cannot make change", http.StatusConflict)`. Minimal change: if the upstream body decodes to an error envelope, include its message. (Acceptable interim: keep 502 but log the upstream status — the POS will show a generic failure; Task E3 adds a pre-check via `change-preview` so 409 is rarely hit at confirm time.)

- [ ] **Step 4: Add `change_breakdown` to the agent's `checkoutResponse`**

In `mulan-agent/main.go`, add to the `checkoutResponse` struct (~line 330):

```go
	RoundedDue      float64        `json:"rounded_due"`
	Change          float64        `json:"change"`
	ChangeBreakdown map[string]int `json:"change_breakdown"`
```

(The agent uses these for the receipt/kick if desired; the POS also gets them via its own backend call. Receipt formatting of the breakdown is optional and out of scope — leave the receipt as-is unless trivially additive.)

- [ ] **Step 5: Build the agent**

Run: `cd mulan-agent && go build ./... && cd ..`
Expected: success.

- [ ] **Step 6: Commit**

```bash
cd mulan-agent && go fmt ./... && cd ..
git add mulan-agent/main.go
git commit -m "feat(agent): forward cash_tender to backend checkout"
```

---

### Task E2: POS login stores role; manager-only drawer denomination editor

**Files:**
- Modify: `mulan-agent/templates/pos/index.html`

- [ ] **Step 1: Store the cashier role on login**

In the login handler (`loginNext`, ~line 1250), `loggedInCashier = cashier;` already stores the whole object, so `loggedInCashier.role` is available. Add a helper near `fmt` (~line 1306):

```js
function isManager() { return (loggedInCashier && loggedInCashier.role === 'manager'); }
```

- [ ] **Step 2: Add a drawer denomination editor modal**

In the settings sheet (near the existing "Cash drawer float" block, ~line 1048), replace the single float row with a denomination grid OR add a new "Cash drawer denominations" section. Add this markup inside the settings sheet body:

```html
<!-- Cash drawer denominations (manager only) -->
<div id="denom-editor" style="display:none;margin-top:16px">
  <div style="font-weight:600;font-size:14px;color:var(--on-surf);margin-bottom:8px">Cash drawer denominations</div>
  <div id="denom-grid" style="display:grid;grid-template-columns:repeat(3,1fr);gap:8px"></div>
  <div style="display:flex;gap:8px;margin-top:10px;align-items:center">
    <div style="flex:1;font-size:13px;color:var(--on-surf-var)">Total: <span id="denom-total" class="mono">฿0.00</span></div>
    <button class="btn-filled" onclick="saveDenoms()" style="height:44px;padding:0 18px">Save (manager)</button>
  </div>
</div>
```

- [ ] **Step 3: Add the denomination JS (render, edit, save)**

Add near the other drawer functions (after `setFloat`, ~line 2470):

```js
// ── Cash drawer denominations ─────────────────────────────────────
const DENOMS_SATANG = [100000,50000,10000,5000,2000,1000,500,200,100];
let denomCounts = {}; // satang string -> count (working copy in the editor)

function denomBaht(satang) { return satang / 100; }

async function loadDenoms() {
    try {
        const r = await fetch(API_BASE + '/api/cash-drawer/denominations');
        const d = await r.json().then(x => x.data ?? x);
        denomCounts = {};
        DENOMS_SATANG.forEach(s => { denomCounts[s] = (d.counts && d.counts[s]) || 0; });
    } catch { denomCounts = {}; DENOMS_SATANG.forEach(s => denomCounts[s] = 0); }
    renderDenomEditor();
}

function renderDenomEditor() {
    const editor = document.getElementById('denom-editor');
    editor.style.display = isManager() ? '' : 'none';
    if (!isManager()) return;
    const grid = document.getElementById('denom-grid');
    grid.innerHTML = DENOMS_SATANG.map(s => `
        <div style="background:var(--surf-2);border-radius:var(--shape-md);padding:8px;text-align:center">
            <div style="font-size:12px;color:var(--on-surf-var)">฿${denomBaht(s).toLocaleString('th-TH')}</div>
            <input type="number" min="0" value="${denomCounts[s]||0}"
                   onchange="denomCounts['${s}']=Math.max(0,parseInt(this.value)||0); renderDenomTotal();"
                   style="width:100%;text-align:center;font-family:'Roboto Mono',monospace;font-size:16px;border:1px solid var(--outline-soft);border-radius:8px;padding:6px 0;margin-top:4px">
        </div>`).join('');
    renderDenomTotal();
}

function renderDenomTotal() {
    let total = 0;
    DENOMS_SATANG.forEach(s => { total += s * (denomCounts[s]||0); });
    document.getElementById('denom-total').textContent = fmt(total/100);
}

async function saveDenoms() {
    if (!isManager()) { snack('Manager only.', 'warn'); return; }
    const pin = prompt('Enter your manager PIN to save the drawer:');
    if (!pin) return;
    const counts = {};
    DENOMS_SATANG.forEach(s => { counts[s] = denomCounts[s]||0; });
    try {
        const r = await fetch(API_BASE + '/api/cash-drawer/denominations', {
            method: 'PUT', headers: {'Content-Type':'application/json'},
            body: JSON.stringify({ cashier_id: loggedInCashier.id, pin, counts }),
        });
        if (r.status === 403) { snack('Manager id + PIN rejected.', 'danger'); return; }
        if (!r.ok) { snack('Failed to save drawer.', 'danger'); return; }
        snack('Drawer updated.', 'success');
        await loadDenoms();
    } catch { snack('Failed to save drawer.', 'danger'); }
}
```

- [ ] **Step 4: Load denominations when the settings sheet opens**

Find the function that opens the settings sheet (search `openSettings`) and add `loadDenoms();` to its body so the editor populates on open.

- [ ] **Step 5: Verify in browser**

Reload `/pos`. Log in as a **manager** cashier → open settings → the denomination grid shows with current counts; edit a count, Save, enter PIN → toast "Drawer updated"; reopen to confirm persistence. Log in as a plain **cashier** → the denomination editor is hidden.

- [ ] **Step 6: Commit**

```bash
git add mulan-agent/templates/pos/index.html
git commit -m "feat(pos): manager-only cash drawer denomination editor"
```

---

### Task E3: POS per-denomination tender + live change breakdown

**Files:**
- Modify: `mulan-agent/templates/pos/index.html`

- [ ] **Step 1: Replace the quick-cash grid with a per-denomination tender pad**

In the tender modal cash pane (the `<div class="quick-cash" id="quick-cash">` at ~line 991), add a denomination tender pad below it. Add markup after the quick-cash div:

```html
<div id="tender-denoms" style="display:grid;grid-template-columns:repeat(3,1fr);gap:6px;margin-bottom:10px"></div>
<div id="change-breakdown" style="font-size:13px;color:var(--on-surf-var);min-height:20px;margin-bottom:8px"></div>
```

- [ ] **Step 2: Build the tender pad + state**

Add near `buildQuickCash` (~line 2078):

```js
let tenderDenoms = {}; // satang string -> count tendered this sale

function buildTenderDenoms() {
    tenderDenoms = {};
    const wrap = document.getElementById('tender-denoms');
    wrap.innerHTML = DENOMS_SATANG.map(s => `
        <button type="button" onclick="addTenderDenom(${s})"
            style="height:44px;border:1px solid var(--outline-soft);border-radius:10px;background:var(--surf-2);font-weight:600">
            +฿${denomBaht(s).toLocaleString('th-TH')}
        </button>`).join('');
    renderTender();
}

function addTenderDenom(s) {
    tenderDenoms[s] = (tenderDenoms[s]||0) + 1;
    cashTendered = tenderSatangTotal() / 100;
    renderTender();
    schedulePreview();
}

function tenderSatangTotal() {
    let t = 0;
    DENOMS_SATANG.forEach(s => { t += s * (tenderDenoms[s]||0); });
    return t;
}
```

- [ ] **Step 3: Reset the pad when the cash pane opens**

In `openTenderDirect` (~line 2022), after `cashTendered = 0;` add `buildTenderDenoms();`. In `kpClear` (~line 2099) add `tenderDenoms = {};` and `buildTenderDenoms();` so Clear resets the denomination tally.

- [ ] **Step 4: Live change preview against the drawer**

Add a debounced preview that calls the backend and shows the exact bills/coins to return:

```js
let previewTimer = null;
function schedulePreview() {
    clearTimeout(previewTimer);
    previewTimer = setTimeout(runChangePreview, 200);
}

async function runChangePreview() {
    const out = document.getElementById('change-breakdown');
    if (payMethod !== 'cash') { out.textContent = ''; return; }
    const due = orderTotals().grand;
    try {
        const r = await fetch(API_BASE + '/api/cash-drawer/change-preview', {
            method: 'POST', headers: {'Content-Type':'application/json'},
            body: JSON.stringify({ due, tender: tenderDenoms }),
        });
        const d = await r.json().then(x => x.data ?? x);
        const confirmBtn = document.getElementById('tender-confirm');
        if (d.change_total < 0) {
            out.innerHTML = `<span style="color:var(--danger)">Short ฿${Math.abs(d.change_total).toFixed(2)}</span>`;
            confirmBtn.disabled = true;
            return;
        }
        if (!d.makeable) {
            out.innerHTML = `<span style="color:var(--danger)">⚠ Cannot make ฿${d.change_total.toFixed(2)} change — restock the drawer (manager)</span>`;
            confirmBtn.disabled = true;
            return;
        }
        const parts = Object.keys(d.breakdown || {})
            .sort((a,b)=>b-a)
            .map(s => `${d.breakdown[s]}×฿${denomBaht(parseInt(s)).toLocaleString('th-TH')}`);
        out.innerHTML = parts.length
            ? `Change ฿${d.change_total.toFixed(2)} → ${parts.join(', ')}`
            : `Exact — no change`;
        confirmBtn.disabled = false;
    } catch { out.textContent = ''; }
}
```

- [ ] **Step 5: Send the denomination tender at checkout**

In `confirmTender` (~line 2137) and `doCheckout` (~line 2177), pass the denomination map. Change `confirmTender`:

```js
async function confirmTender() {
    const change = payMethod === 'cash' ? Math.max(0, cashTendered - orderTotals().grand) : 0;
    closeTender();
    await doCheckout(payMethod, cashTendered, change, payMethod === 'cash' ? tenderDenoms : null);
}
```

Change `doCheckout`'s signature to `async function doCheckout(method, tendered, change, tenderDenomMap)` and in the `fetch('/checkout', ...)` body add:

```js
                cash_tender: method === 'cash' ? (tenderDenomMap || {}) : null,
                cashier_name: loggedInCashier ? loggedInCashier.name : '',
```

- [ ] **Step 6: Handle a 409 from checkout (drawer short at confirm time)**

In `doCheckout`, the `fetch('/checkout')` currently doesn't check `res.ok`. Capture the response and, on `409`, snack a restock message and re-enable Pay instead of clearing the order:

```js
        const res = await fetch('/checkout', { /* ...existing... */ });
        if (res.status === 409) {
            snack('Cannot make change — restock the drawer (manager), then retry.', 'danger');
            payBtn.disabled = false;
            return;
        }
        if (!res.ok) { throw new Error('checkout failed'); }
```

(Place this in the existing `try` block, replacing the bare `await fetch('/checkout', ...)`.)

- [ ] **Step 7: Show the change breakdown in the change-due modal**

In `openChangeModal` (~line 2149), after setting `change-amount`, optionally render the breakdown. Pass the breakdown from `doCheckout` if available (the agent `/checkout` response includes `change_breakdown`). Minimal: keep the modal as-is (total change is enough); the live preview already showed the breakdown pre-confirm. (Breakdown-in-modal is a nice-to-have; skip unless the agent response is already wired to return it.)

- [ ] **Step 8: Verify in browser (full cash flow)**

Reload `/pos`. Add items totalling e.g. ฿47.25. Pay → cash. Tap `+฿50` once → preview shows "Rounded due ฿47 … Change ฿3 → 1×฿2, 1×฿1" (or similar minimal breakdown), Confirm enabled. Empty the drawer's coins (as manager) and repeat → preview shows red "Cannot make ฿3 change — restock", Confirm disabled. Restock as manager, retry → completes; verify the drawer counts updated via the denomination editor.

- [ ] **Step 9: Commit**

```bash
git add mulan-agent/templates/pos/index.html
git commit -m "feat(pos): per-denomination cash tender + live change breakdown"
```

---

# PHASE F — mulan-manager role assignment

### Task F1: API client — role field + params

**Files:**
- Modify: `../mulan-manager/src/lib/api/cashiers.ts`

- [ ] **Step 1: Add `role` to the interface + functions**

In `../mulan-manager/src/lib/api/cashiers.ts`:

Update the interface:

```ts
export interface Cashier {
	id: number;
	login_id: string;
	name: string;
	role: 'cashier' | 'manager';
	active: boolean;
}
```

Update `createCashier` to take + send `role`:

```ts
export const createCashier = (login_id: string, name: string, pin: string, role: 'cashier' | 'manager') =>
	fetch('/api/cashiers', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ login_id, name, pin, role })
	}).then(async (r) => {
		if (r.status === 409) throw new Error('Login ID already in use');
		return j<Cashier>(r);
	});
```

Update `updateCashier`:

```ts
export const updateCashier = (id: number, name: string, active: boolean, role: 'cashier' | 'manager') =>
	fetch(`/api/cashiers/${id}`, {
		method: 'PATCH',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ name, active, role })
	}).then((r) => j<Cashier>(r));
```

- [ ] **Step 2: Build/typecheck**

Run: `cd ../mulan-manager && npm run check 2>&1 | head -30`
Expected: type errors at the `+page.svelte` call sites (missing `role` arg) — fixed in F2. No errors in `cashiers.ts` itself.

- [ ] **Step 3: Commit**

```bash
cd ../mulan-manager
git add src/lib/api/cashiers.ts
git commit -m "feat(cashiers): role field in API client"
cd ../mulan
```

---

### Task F2: Cashiers page — role selector + display

**Files:**
- Modify: `../mulan-manager/src/routes/(app)/cashiers/+page.svelte`

- [ ] **Step 1: Read the page to find the create/edit form + row markup**

Run: `sed -n '1,200p' ../mulan-manager/src/routes/(app)/cashiers/+page.svelte`
Identify: the local state for the create form, the edit state, the `createCashier`/`updateCashier` call sites, and where each cashier row renders.

- [ ] **Step 2: Add role state + a selector to the create form**

Add a `role` variable to the create-form state (default `'cashier'`), and a `<select>` bound to it next to the name/PIN inputs:

```svelte
<select bind:value={newRole} class="<existing input classes>">
	<option value="cashier">Cashier</option>
	<option value="manager">Manager</option>
</select>
```

Declare `let newRole: 'cashier' | 'manager' = 'cashier';` alongside the other `let new...` form vars. Pass it in the create call: `await createCashier(newLoginId, newName, newPin, newRole);` and reset `newRole = 'cashier'` after success.

- [ ] **Step 3: Add role to the edit flow**

Wherever a cashier is edited (inline or modal), bind a role `<select>` to the edited cashier's `role` and pass it: `await updateCashier(c.id, c.name, c.active, c.role);` Match the file's existing edit pattern (if it edits a copy object, set `.role` on that copy).

- [ ] **Step 4: Show the role on each row**

In the cashier row markup, render a small badge:

```svelte
<span class="<existing badge classes>">{c.role === 'manager' ? 'Manager' : 'Cashier'}</span>
```

- [ ] **Step 5: Typecheck + run**

Run: `cd ../mulan-manager && npm run check 2>&1 | head -20`
Expected: no type errors.

Run the dev server (`npm run dev`) and load the cashiers page: create a manager, confirm the badge shows; edit a cashier's role and confirm it persists (re-fetch shows the new role). Verify against the backend: `psql "$PSQL_URL" -c "SELECT login_id, role FROM cashiers;"`.

- [ ] **Step 6: Commit**

```bash
cd ../mulan-manager
git add "src/routes/(app)/cashiers/+page.svelte"
git commit -m "feat(cashiers): assign role from manager UI"
cd ../mulan
```

---

## CLAUDE.md update (after all phases)

- [ ] Update the API endpoint list in `mulan/CLAUDE.md`:
  - Under cash-drawer/open group: add `GET /api/cash-drawer/denominations`, `POST /api/cash-drawer/change-preview`.
  - Note manager-gated (id+PIN): `PUT /api/cash-drawer/denominations`, `POST /api/cash-drawer/denominations/adjust`.
  - Note `cashiers` now carry a `role` (`cashier|manager`) and checkout accepts `cash_tender`.
  - Add a "Cash drawer denominations" section summarising the model + block-until-restocked.
  Commit: `docs: cash drawer denominations + cashier role`.

---

## Self-Review (completed during planning)

**Spec coverage:**
- Cashier role (§1.1, §2, §5.3) → Tasks A1–A4, F1–F2. ✓
- `cash_drawer_denominations` + audit jsonb + sale event (§1.2, §1.3) → B1–B3. ✓
- Role enforcement id+PIN (§2) → A3 `VerifyManager`, C1 `authorize`. ✓
- Bounded DP (§3) → B4 with greedy-fails test. ✓
- Service methods (§4) → B5. ✓
- API endpoints (§5.1) → C1. ✓
- Checkout integration (§5.2) → D1–D3. ✓
- POS UI (§6) → E1–E3. ✓
- mulan-manager (§5.4) → F1–F2. ✓
- Errors (§7) → C1 (403/409), D1/D2 (400/409). ✓
- Tests (§8) → B4 DP tests, B5 helper tests, D2/D3 integration. ✓

**Placeholder scan:** No "TBD/TODO". E3 step 7 (breakdown-in-modal) and E1 step 3 (409 propagation) are explicitly marked optional/interim with the concrete minimal path stated — not placeholders.

**Type consistency:** `VerifyManager(ctx, int32, string) (sqlc.Cashier, error)` consistent across A3/C1/D-interface. `ApplyCashSale(ctx, *sqlc.Queries, tender, change map[int64]int, actor string) error` consistent B5/D1. `MakeChange(ctx, int64) (map[int64]int, error)` consistent B5/D1. Denomination JSON keys are satang strings throughout (audit, API, POS). `CashPayment{IsCash, Tender, Actor}` consistent D1/D2.

**Known risk to watch during execution:** `MakeChange` in `Checkout` reads stock via the non-tx `s.q` while `ApplyCashSale` writes via tx `q`. Safe on a single terminal (the locked assumption). If multi-terminal is ever added, move the read inside the tx with `SELECT ... FOR UPDATE`.
