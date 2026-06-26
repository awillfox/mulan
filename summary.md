# Mulan POS System

## Overview
Go-based Point of Sale (POS) system with two separate modules: main server and device agent.

## Modules

### mulan (main server)
- API server on `:8080`
- Serves manager web UI at `/manager`
- REST API for menus and menu categories
- PostgreSQL database via `pgx/v5`

### mulan-agent (device agent)
- Device agent on `:8081`
- Serves POS UI at `/pos` (fixed 1024x768 for Flytech POS485)
- Controls VFD display on COM3 (scrolls "Hua Mulan" / "Mulan Project")
- Communicates with mulan API via configurable `API_BASE`

## Tech Stack
- **Language:** Go 1.25
- **Router:** chi/v5
- **DB:** PostgreSQL (pgx/v5, pgxpool)
- **Config:** Viper (reads .env)
- **Money:** Rhymond/go-money (THB), stored as int64 satang
- **SQL Generation:** sqlc
- **Schema:** Atlas (HCL)
- **Frontend:** Tailwind CSS (CDN), Go html/template

## API Endpoints
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/menus` | List menus (mock data) |
| GET | `/api/menu-categories` | List categories |
| POST | `/api/menu-categories` | Create category `{name}` |
| PATCH | `/api/menu-categories/{id}` | Update category `{name}` |
| DELETE | `/api/menu-categories/{id}` | Delete category |

## Database Schema

### menu_categories
| Column | Type | Notes |
|--------|------|-------|
| id | serial (PK) | auto-increment |
| name | varchar(255) | not null |

### menus
| Column | Type | Notes |
|--------|------|-------|
| id | serial (PK) | auto-increment |
| name | varchar(255) | not null |
| price | bigint | stored in satang |
| category_id | int (FK) | nullable, SET_NULL on delete |

## Project Structure
```
mulan/
  main.go                      # entry point: config, DB, chi router
  internal/
    menu/                      # menu domain
      domain/menu.go
      service/menu.go
      http/handler.go, router.go
    menucategory/              # category domain
      domain/category.go
      service/category.go
      http/handler.go, router.go
    web/
      handler.go               # serves manager templates
  mulan-agent/
    main.go                    # VFD + POS server entry
    lib/vfd/engine.go          # VFD display control
    templates/                 # POS templates
  schema.hcl                   # Atlas HCL schema
  sqlc.yml                     # sqlc config
  Taskfile.yml                 # task runner
  templates/                   # manager templates
```

## Dev Commands
- `task migrate-dev` — apply schema to dev DB
- `task migrate-prod` — apply schema to prod DB
- `task generate-sql-schema` — export SQL schema from dev DB
- `task sqlcgen` — generate Go code from SQL queries

## Hardware Target
- **POS Terminal:** Flytech POS485, 15" 1024x768, Windows 11
- **Cash Drawer:** GS-410B
- **VFD Display:** COM3
