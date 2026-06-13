# Role + Denomination-Aware Cash Drawer — Design

Date: 2026-06-13
Status: Approved (pending spec review)
Feature branch base: `feat/member`

## Goal

Two coupled capabilities for the Mulan POS:

1. **Cashier roles.** Add a `role` to POS cashiers (`cashier` | `manager`). The
   `manager` role may edit the cash drawer's denomination counts; a plain
   `cashier` may not. (`manager_users` already has `owner | staff` and is
   untouched.)
2. **Denomination-aware cash drawer.** The drawer tracks how many of each bill
   and coin it holds. A manager sets/adjusts those counts. On every CASH sale the
   cashier enters the exact denominations the customer handed over; the drawer
   auto-adds the tender and auto-subtracts the change. The POS computes the change
   as the actual bills/coins to hand back, drawn from current stock.

## Decisions (locked during brainstorming)

- Denominations tracked (THB, 9): 1000, 500, 100, 50, 20, 10, 5, 2, 1.
- **Single** physical drawer / single terminal. Counts **auto-adjust** on every
  cash sale (+tender, −change). Manager edits only at shift start / corrections.
- Cash amount due **rounds to nearest ฿1** (smallest tracked coin). Card/QR charge
  the exact satang total. Only cash sales touch the drawer.
- Change shortfall policy: **block until restocked** — if exact change cannot be
  made from current stock, the cash sale cannot complete (`409`). The **only**
  escape is a **manager** restocking the drawer via the adjust endpoint,
  **confirming with cashier id + PIN**; once stock makes the change feasible, the
  sale completes through the normal path. There is **no** force-complete override
  and **no** negative-count path — counts are always `>= 0` (DB CHECK enforced).
- Storage: **current-state denomination table** (source of truth) + the existing
  `cash_drawer_audit` as history (extended with a per-denom delta). Total derived
  = Σ(denom×count); this replaces the old single-float concept.
- Change algorithm: **bounded coin-change DP**, not greedy — greedy with limited
  stock can wrongly report "impossible" and block a makeable sale.

## Out of scope (YAGNI)

Multiple terminals/drawers, satang coins (0.25/0.50), loyalty-point/change
redemption, shift-close reconciliation reports. Not built now.

---

## 1. Data model

### 1.1 `cashiers.role` (new column)

```
role varchar(20) NOT NULL DEFAULT 'cashier'   CHECK (role IN ('cashier','manager'))
```

Existing rows migrate to `'cashier'`. Exposed in cashier create/update, the
list/login API responses, and the manager web cashier editor.

### 1.2 `cash_drawer_denominations` (new table — drawer state)

| column        | type        | notes                                              |
|---------------|-------------|----------------------------------------------------|
| `denomination`| int, PK     | value in **satang**: 100000,50000,10000,5000,2000,1000,500,200,100 |
| `count`       | int NOT NULL DEFAULT 0 | CHECK `count >= 0`                      |
| `updated_at`  | timestamptz NOT NULL DEFAULT now() |                             |

Single logical drawer (no `drawer_id` — single terminal). The 9 rows are seeded
(count 0) on first startup, same pattern as the `settings` seed. Total float =
Σ(denomination × count).

### 1.3 `cash_drawer_audit` (extend existing)

Add `denominations jsonb NULL` holding the **signed per-denomination delta** for
the event, e.g. `{"10000": 1, "500": -2}` (one ฿100 in, two ฿5 out). Keep the
existing `amount` / `delta` (total, satang) columns. Event types: existing
`set` / `clear` / `adjust` / `kick` / `open_for_change`, plus new **`sale`** for
the auto-adjust on cash checkout. `set`/`adjust`/`sale` now also write the
`denominations` delta; `kick`/`open_for_change` leave it null.

---

## 2. Role enforcement (no cashier session exists)

POS cashier login is **stateless** — it returns the cashier record, no bearer
token; the POS holds id/role client-side. All `/api/cash-drawer` routes are in
the **Open** group (POS-shared, no manager-auth). Therefore drawer-write
authorization is done **per request by re-verifying the cashier**:

- Drawer **writes** (`PUT /denominations`, `POST /denominations/adjust`) require
  **`cashier_id` + `pin`** in the body. The backend bcrypt-verifies the PIN
  against that cashier and checks `role == 'manager'` and `active`. Failure →
  `403`. This reuses the existing cashier login verification path and logs the
  manager as `actor` in the audit row.
- This directly satisfies the requirement: when a sale is blocked for lack of
  change, a manager restocks via the adjust endpoint **confirmed by id + PIN**,
  then the sale completes through the normal path.
- Cash **sales** (the auto-adjust) need **no** role — any active cashier completes
  them. Only manual set/adjust are manager-gated.
- POS additionally hides the drawer editor for non-manager cashiers (UX), but the
  server check is authoritative.

Assumption: the terminal is a trusted kiosk on the tailnet; PIN re-verification is
the strongest control available without building a cashier session system. If
that's wrong, a full cashier-session/token system would be required (deferred).

---

## 3. Change algorithm — bounded coin-change DP

Because cash due is rounded to whole ฿1, every change amount is a whole-baht
multiple. Compute the **minimum-count** breakdown using a bounded coin-change DP
over baht units, limited by current stock per denomination:

- Input: `change_baht`, `stock map[denomBaht]count`.
- DP `best[0..change_baht]` = min bills/coins to form that amount using available
  stock; reconstruct the chosen counts. Denominations: 1000,500,100,50,20,10,5,2,1.
- Output: `(breakdown map[denom]count, makeable bool)`. `makeable == false` when no
  combination within stock reaches the amount.
- Cost is trivial (9 denoms, amount typically < a few thousand). Greedy is
  explicitly **rejected** because with limited stock it can fail to find a valid
  combination that exists, which would wrongly block a sale.

Unit tests must include: exact change with plenty of stock; zero change; a case
where greedy-largest-first fails but a valid combo exists (limited stock); and a
genuinely infeasible case.

---

## 4. Service layer (`internal/cashdrawer/service`)

New methods alongside the existing float/audit service:

- `CurrentDenoms(ctx) -> (counts map[int64]int, totalSatang int64, err)`
- `SetDenoms(ctx, counts map[int64]int, actor string) (AuditEvent, error)` —
  absolute set; computes per-denom delta vs current, writes state rows + `set`
  audit. Manager-gated at HTTP layer.
- `AdjustDenoms(ctx, delta map[int64]int, actor string) (AuditEvent, error)` —
  relative add/remove (e.g. add a coin roll); guards resulting counts ≥ 0; writes
  `adjust` audit. Manager-gated.
- `MakeChange(ctx, changeSatang int64) (breakdown map[int64]int, makeable bool, err)`
  — reads current stock, runs the DP. Pure read, no mutation.
- `ApplyCashSale(ctx, q *sqlc.Queries /* tx-bound */, tender, change map[int64]int, actor string) error`
  — runs **inside the checkout transaction**. Applies `+tender − change` to state
  and guards every resulting count ≥ 0 (a change denom exceeding stock aborts the
  tx — but checkout only reaches here after `MakeChange` confirmed feasibility, so
  the guard is a belt-and-suspenders invariant). Appends `sale` audit with the net
  delta. Any error aborts → checkout tx rolls back.

PIN/role verification lives in (or is shared with) the cashier service and is
called by the HTTP handlers before `SetDenoms`/`AdjustDenoms`.

---

## 5. API

### 5.1 Cash drawer (extends `/api/cash-drawer`, Open group)

| method | path | body | auth | returns |
|--------|------|------|------|---------|
| GET  | `/denominations` | — | open | `{counts:{denom:count}, total}` (THB display) |
| PUT  | `/denominations` | `{cashier_id, pin, counts:{denom:count}}` | manager (id+PIN) | updated state |
| POST | `/denominations/adjust` | `{cashier_id, pin, delta:{denom:count}}` | manager (id+PIN) | updated state |
| POST | `/change-preview` | `{due, tender:{denom:count}}` | open (advisory) | `{rounded_due, change_total, breakdown, makeable}` |

`change-preview` is called live as the cashier enters tender, so the POS shows the
rounded due, the change total, and the exact bills/coins to return — all before
committing. It is advisory only; checkout re-computes authoritatively.

### 5.2 Checkout (extends `POST /api/orders/{code}/checkout`)

Request gains, for cash: `{payment_method:"cash", cash_tender:{denom:count}}`.
Backend:

1. Compute authoritative order total (existing path).
2. Round cash due to nearest ฿1.
3. `tender_total = Σ cash_tender`; if `< rounded_due` → `400`.
4. `change = tender_total − rounded_due`; `MakeChange(change)`.
   - makeable → proceed.
   - not makeable → `409` with the shortfall, so the POS prompts a manager to
     restock (adjust, id+PIN) and the cashier retries checkout.
5. `ApplyCashSale(...)` inside the existing checkout transaction (alongside member
   points etc.), so the drawer adjusts atomically and rolls back on any failure.
6. Response adds `rounded_due` and `change_breakdown` for display/print.

Non-cash payment methods do not touch the drawer.

### 5.3 Cashier (extends `/api/cashiers`)

`role` added to create/update request and to list/login responses. These writes
are already **owner-gated** by manager-auth (`RequireRole(owner)` per the route
scoping), so assigning a cashier's role from the web manager uses the existing
owner bearer token — **distinct** from the POS-side PIN re-challenge in §2, which
only guards drawer denomination edits on the terminal.

---

## 5.4 mulan-manager frontend (separate repo `../mulan-manager`, SvelteKit)

Cashier role is assigned from the existing cashiers page. Changes:

- `src/lib/api/cashiers.ts`: add `role: 'cashier' | 'manager'` to the `Cashier`
  interface; thread `role` through `createCashier(...)` and `updateCashier(...)`
  (PATCH body gains `role`).
- `src/routes/(app)/cashiers/+page.svelte`: role selector (cashier / manager) in
  the create and edit forms; show the role on each cashier row.
- Proxy `src/routes/api/[...path]/+server.ts`: `cashiers` is **already**
  allowlisted — no change. (Drawer endpoints are POS-only and are **not** added to
  the web proxy.)

No new auth wiring: cashier writes already require the owner manager session.

---

## 6. POS UI (`mulan-agent` `templates/pos/index.html`)

- **Login** stores the cashier's `role`. A manager-only **Drawer** editor (grid of
  the 9 denominations with count steppers, live total) calls `PUT /denominations`.
  Hidden for plain cashiers.
- **Tender dialog, cash tab:** replace the single quick-cash total with **per-denom
  tender entry** (tap a denomination to +1 it). Live `change-preview` shows rounded
  due, change total, and **the exact bills/coins to hand back**. If not makeable:
  red "cannot make change" with the shortfall and (for a manager) a restock
  affordance (opens the drawer editor / adjust, requires id+PIN); after restock the
  cashier retries the payment.
- On checkout: send the tender map; display the returned change breakdown (also on
  the printed receipt where appropriate); **re-fetch drawer counts** so the on-screen
  drawer reflects reality immediately ("realtime" = immediate re-fetch on the single
  terminal after each sale or manager edit).

---

## 7. Error handling

| case | result |
|------|--------|
| tender total < rounded due | `400` |
| change not makeable | `409` + shortfall detail (restock + retry) |
| non-manager / bad PIN attempts drawer write | `403` |
| `adjust` would drive a count below 0 | `409` |
| any drawer failure during checkout | whole checkout tx rolls back |

All money is int64 **satang** server-side; THB float only at display, via
go-money (matches existing convention).

---

## 8. Testing

- **DP unit tests:** exact change; zero change; limited-stock case where greedy
  fails but a valid combo exists; infeasible case.
- **Service tests:** `SetDenoms`/`AdjustDenoms` delta + audit row correctness;
  `AdjustDenoms` negative guard; `ApplyCashSale` +tender/−change and the negative
  guard; role+PIN gate (wrong PIN, wrong role, inactive → reject).
- **Checkout integration:** cash sale adjusts the drawer atomically; a forced
  failure after adjust rolls the drawer back; non-cash leaves the drawer unchanged;
  blocked-then-restock-then-complete happy path.

---

## 9. Pre-change pressure test

- **Blast radius:** new table + one new cashier column + new endpoints + checkout
  addition. A bug fails **loud** (checkout error / 4xx), not silently; the drawer
  count is the only mutable state and it is transactional. Worst case: a cash sale
  is wrongly blocked (operational stall) or the drawer count drifts — both visible
  and correctable via `set`/`adjust` + the audit log.
- **Assumptions:** trusted single-terminal kiosk on the tailnet; PIN re-verify is
  acceptable as the strongest available control; THB standard 9-denomination set is
  sufficient; cash totals are acceptably rounded to whole ฿1.
- **Reversibility:** schema via Atlas (additive — new table, new nullable/defaulted
  column; reversible). Drawer state always correctable by a manager `set`. Feature
  is additive to checkout; non-cash and existing flows unaffected.
- **Blind spots:** block-until-restocked can stall a queue if small coins run out —
  mitigated by the manager restock (adjust, id+PIN) then retry. Concurrency is a
  non-issue on one terminal but `ApplyCashSale` still runs inside the checkout tx
  for safety.
