# TODO — Finish: SvelteKit `mulan-manager` replaces the Go `/manager` pages

**Goal:** Retire the legacy Go `html/template` `/manager/*` pages in favour of the
SvelteKit `mulan-manager` app (separate repo, `../mulan-manager`), then **re-close
the dashboard auth hole** that was opened as a stop-gap.

**Why now:** On 2026-06-14 `GET /api/dashboard/*` was moved to the OPEN route block
(see `main.go`) because the Go `/manager` dashboard sends no bearer token, so an
owner-gated dashboard returned 401 and the page rendered zeros. That open gate
exposes revenue / top-menus / subsidies / heatmap to anyone who can reach
`:8085` on the tailnet. Finishing the SvelteKit cutover lets us re-gate it.

---

## Verified current state (2026-06-14)

- **Every Go page already has a SvelteKit route** (existence verified; feature
  parity NOT yet audited — see Phase 1):

  | Go page (`templates/manager/…`) | main.go route | SvelteKit route (`../mulan-manager/src/routes`) |
  |---|---|---|
  | `index.html` (dashboard) | `/manager` | `/(app)/+page.svelte` |
  | `items.html` (items + categories + base options + option-group attach) | `/manager/items` | `/(app)/menu/+page.svelte` |
  | `option_groups.html` | `/manager/option-groups` | `/(app)/option-groups/+page.svelte` |
  | `discounts.html` | `/manager/discounts` | `/(app)/discounts/+page.svelte` |
  | `members.html` | `/manager/members` | `/(app)/members/+page.svelte` |
  | `cashiers.html` | `/manager/cashiers` | `/(app)/cashiers/+page.svelte` |
  | `settings.html` | `/manager/settings` | `/(app)/settings/+page.svelte` |

- SvelteKit has extras with **no** Go equivalent: `/(app)/drawer` (owner-only
  drawer denomination mgmt), `/(app)/more`, `/login`, `/logout`.
- SvelteKit auth works: the API proxy (`src/routes/api/[...path]/+server.ts`)
  reads the session token from cookies and attaches it as the bearer on every
  forwarded `/api/*` call; `dashboard` is already in the proxy `ALLOW` list, and
  `/(app)/+page.svelte` fetches `/api/dashboard` + `/api/dashboard/top-menus`.
  **Therefore re-gating `/api/dashboard/*` under `RequireRole(owner)` will NOT
  break SvelteKit** — only the Go page (being deleted) depends on the open gate.

**Assumption (load-bearing, unverified):** each SvelteKit page is at full feature
parity with its Go counterpart. If wrong → deleting a Go page removes a feature
users still rely on. Phase 1 must confirm before Phase 2.

---

## Phase 1 — Audit feature parity (per page, before deleting anything)

For each page, compare the SvelteKit route against the Go template + its CLAUDE.md
spec; list any missing actions/fields; close gaps in `../mulan-manager`.

- [ ] **Dashboard** (`/(app)/` vs `index.html`): KPIs, compare/prev-period,
      sales-by-day chart, heatmap, top-menus, subsidies waterfall, date presets.
- [ ] **Items** (`/(app)/menu` vs `items.html`): item CRUD, category CRUD,
      **base options** editor (`PUT …/base-options`), **option-group attach** incl.
      isolated/"Customize" clone flow (`PUT …/option-groups`), enable/disable toggle.
- [ ] **Option groups** (`/(app)/option-groups` vs `option_groups.html`): group CRUD
      + per-option CRUD, selection modes (`single_required`/`single_optional`/`multi`).
- [ ] **Discounts** (`/(app)/discounts` vs `discounts.html`): CRUD, fixed/percent,
      `active`, `is_subsidy` flag.
- [ ] **Members** (`/(app)/members` vs `members.html`): CRUD, phone-dup 409 handling,
      order history view, points display.
- [ ] **Cashiers** (`/(app)/cashiers` vs `cashiers.html`): CRUD, PIN set/update,
      `role` (`cashier|manager`).
- [ ] **Settings** (`/(app)/settings` vs `settings.html`): shop name, VAT %,
      `points_per_baht`, logo upload/serve.

## Phase 2 — Cutover

- [ ] Confirm the render frontend's `API_BASE` points at **prod**
      `http://100.86.43.70:8085` (over Tailscale), not the old chaiyarak dev box.
- [ ] Announce/confirm staff are using the SvelteKit manager for all flows above.

## Phase 3 — Remove the Go pages

- [ ] Delete page routes in `main.go` (`/manager`, `/manager/items`,
      `/manager/option-groups`, `/manager/discounts`, `/manager/members`,
      `/manager/cashiers`, `/manager/settings`).
- [ ] Delete `internal/web/handler.go` manager handlers (+ package if now empty).
- [ ] Delete `templates/manager/*.html` and `templates/layouts/manager.html`
      (and the prod `templates.bak*` / `templates.old` dirs on the server).
- [ ] Grep for dangling refs to the removed templates/routes; `go build ./...`.

## Phase 4 — Re-gate the dashboard API (closes the security hole)

- [ ] In `main.go`, move `r.Route("/dashboard", dashboardHandler.Routes)` from the
      OPEN block **back under** the `RequireManager` + `RequireRole(owner)` group
      (revert the 2026-06-14 stop-gap; remove its explanatory comment).
- [ ] Verify: no-token `GET /api/dashboard/` → **401**; SvelteKit dashboard (logged
      in as owner) still renders today's numbers via the bearer-attaching proxy.
- [ ] Confirm `dashboard` stays in the SvelteKit proxy `ALLOW` list.

## Phase 5 — Docs

- [ ] CLAUDE.md: remove the open-dashboard note + Go `/manager` page entries from
      the **Pages** section; move `/api/dashboard/*` back under `RequireRole(owner)`
      in the route-scoping list.
- [ ] CLAUDE.md: fix the stale **Deployment** section — prod is `coffee@100.86.43.70`,
      `/home/coffee/mulan`, systemd `mulan.service`; chaiyarak `100.109.90.83` is the
      **dev** box (clone DB). (Tracked separately from this cutover.)

---

## Verify done

- [ ] All 7 flows work in SvelteKit against prod.
- [ ] `curl http://100.86.43.70:8085/api/dashboard/` (no token) → **401**.
- [ ] No Go `/manager` routes/templates remain; `go build ./...` + `go test ./...` pass.
