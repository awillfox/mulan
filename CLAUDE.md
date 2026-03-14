# Mulan POS System

## Project Overview
Go-based Point of Sale (POS) system. Two modules:
- **mulan** — main server (API, manager web UI)
- **mulan-agent** — device agent running on POS terminal, serves POS UI, controls cash drawer (GS-410B) and VFD display (COM3)

Claude will update CLAUDE.md a long the way

## Target Hardware
- **POS Terminal:** Flytech POS485, 15" display (1024x768), Windows 11 with VFD display on COM3

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
- `/pos` — POS interface, fixed 1024x768 for 15" Flytech POS485 **(served by mulan-agent)**
- `/manager` — dashboard, responsive for modern devices **(served by mulan)**

## API Endpoints
- `GET /api/menus` — returns list of menus (currently mock data)
- `GET /api/menu-categories` — list all menu categories
- `POST /api/menu-categories` — create a menu category `{name}`
- `PATCH /api/menu-categories/{id}` — update a menu category `{name}`
- `DELETE /api/menu-categories/{id}` — delete a menu category

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
- `.env` — environment config (PSQL_URL, etc.)
- `mulan-agent/main.go` — agent entry point: VFD display, POS web server
- `mulan-agent/.env` — agent config (API_BASE, PORT)
