# Mulan POS System

A Go-based Point of Sale (POS) system designed for restaurant/retail use on a Flytech POS485 terminal running Windows 11.

## Architecture

The system is split into two modules:

| Module | Description | Default Port |
|---|---|---|
| `mulan` | Main server — API, PostgreSQL, manager web UI | `8080` |
| `mulan-agent` | Device agent — POS UI, cash drawer (GS-410B), VFD display (COM3), receipt printer | `8081` |

The agent runs on the POS terminal and communicates with the main server over HTTP.

## Tech Stack

- **Language:** Go 1.25
- **Database:** PostgreSQL (pgx/v5, pgxpool)
- **Router:** chi/v5
- **Config:** Viper (reads `.env`)
- **Money:** go-money (THB) — stored as int64 satang, float only for display
- **SQL Generation:** sqlc
- **Schema Management:** Atlas (HCL)
- **Frontend:** Tailwind CSS (CDN), Go `html/template`
- **Task Runner:** Taskfile

## Target Hardware

- **POS Terminal:** Flytech POS485, 15" display (1024×768), Windows 11
- **VFD Display:** COM3
- **Cash Drawer:** GS-410B (controlled via `inpoutx64.dll`)

---

## Setup

### Prerequisites

- Go 1.25+
- PostgreSQL
- [Atlas](https://atlasgo.io/) — schema management
- [sqlc](https://sqlc.dev/) — SQL code generation
- [Task](https://taskfile.dev/) — task runner

### Environment — mulan (main server)

Create `.env` in the project root:

```env
PSQL_URL=postgres://user:password@localhost:5432/mulan
PSQL_DEV_URL=postgres://user:password@localhost:5432/mulan_dev
PSQL_PROD_URL=postgres://user:password@host:5432/mulan
PORT=8080
```

### Environment — mulan-agent

Create `.env` inside `mulan-agent/`:

```env
API_BASE=http://localhost:8080
PORT=8081
INPOUTX64_DLL=C:\Tools\inpoutx64.dll
RECEIPT_PRINTER_ADDR=192.168.1.100:9100
STORE_NAME=Hua Mulan
```

`RECEIPT_PRINTER_ADDR` is optional. If not set, receipt printing is disabled.

---

## Development Commands

Run from the project root:

```bash
task migrate-dev        # Apply schema.hcl to dev database
task migrate-prod       # Apply schema.hcl to production database
task generate-sql-schema  # Export SQL schema from dev DB → schema.sql
task sqlcgen            # Regenerate Go code from SQL queries (sqlc/)
```

### Run the main server

```bash
go run main.go
```

### Run the agent

```bash
cd mulan-agent
go run main.go
```

---

## Pages

### Manager UI (`mulan`)

Served on the main server. Responsive layout for modern browsers.

| Path | Description | Template |
|---|---|---|
| `/manager` | Dashboard — today's sales summary and top menus | `templates/manager/index.html` |
| `/manager/items` | Item manager — create, edit, toggle, delete menus | `templates/manager/items.html` |

Layout: `templates/layouts/manager.html`

### POS UI (`mulan-agent`)

Fixed 1024×768 layout for the Flytech 15" display.

| Path | Description | Template |
|---|---|---|
| `/pos` | POS interface — browse menus, add to cart, checkout | `mulan-agent/templates/pos/index.html` |

Layout: `mulan-agent/templates/layouts/pos.html`

---

## API Endpoints

All API endpoints are served by the **main server** (`mulan`).

### Menus

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/menus` | List all menus |
| `POST` | `/api/menus` | Create a menu `{name, price, category_id?, vfd_name?}` |
| `PATCH` | `/api/menus/{id}` | Update a menu `{name, price, category_id?, vfd_name?}` |
| `PATCH` | `/api/menus/{id}/toggle` | Toggle menu active/inactive (broadcasts SSE event) |
| `DELETE` | `/api/menus/{id}` | Delete a menu |

### Menu Categories

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/menu-categories` | List all categories |
| `POST` | `/api/menu-categories` | Create a category `{name}` |
| `PATCH` | `/api/menu-categories/{id}` | Update a category `{name}` |
| `DELETE` | `/api/menu-categories/{id}` | Delete a category |

### Orders

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/orders` | Create a new order, returns `{code}` |
| `POST` | `/api/orders/{code}/checkout` | Finalize order `{items:[{menu_id,name,price,qty}]}`, returns totals |

### Dashboard

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/dashboard` | Today's sales summary (revenue, order count) |
| `GET` | `/api/dashboard/top-menus` | Top-selling menus. Optional query params: `from=YYYY-MM-DD&to=YYYY-MM-DD` |

### Server-Sent Events

| Method | Path | Description |
|---|---|---|
| `GET` | `/events` | SSE stream — broadcasts `menu_status` events when a menu is toggled |

---

## Agent Endpoints

Served by **mulan-agent** on the POS terminal.

| Method | Path | Description |
|---|---|---|
| `GET` | `/pos` | POS UI page |
| `POST` | `/vfd/item` | Display item on VFD: `{name, qty, total}`. Clears after 10s inactivity |
| `POST` | `/cash-drawer/open` | Open the GS-410B cash drawer |
| `POST` | `/checkout` | Full checkout flow: saves order via mulan API, prints receipt, opens cash drawer |

---

## Project Structure

```
mulan/
├── main.go                          # Server entry point
├── schema.hcl                       # Atlas HCL database schema
├── schema.sql                       # Exported SQL schema (generated)
├── sqlc.yml                         # sqlc configuration
├── Taskfile.yml                     # Task runner commands
├── .env                             # Environment config (not committed)
├── internal/
│   ├── hub/                         # SSE event hub
│   │   └── hub.go
│   ├── menu/
│   │   ├── domain/menu.go           # Menu entity
│   │   ├── service/menu.go          # Business logic
│   │   └── http/                    # HTTP handlers + routes
│   ├── menucategory/
│   │   ├── domain/category.go       # Category entity
│   │   ├── service/category.go      # Business logic
│   │   └── http/                    # HTTP handlers + routes
│   ├── order/
│   │   ├── domain/order.go          # Order entity
│   │   ├── service/order.go         # Business logic (checkout, summary)
│   │   └── http/handler.go          # HTTP handlers
│   └── web/
│       └── handler.go               # Manager page renderer
├── sqlc/                            # Generated Go code from sqlc
├── templates/
│   ├── layouts/
│   │   └── manager.html             # Manager layout
│   └── manager/
│       ├── index.html               # Dashboard page
│       └── items.html               # Item manager page
└── mulan-agent/
    ├── main.go                      # Agent entry point
    ├── .env                         # Agent config (not committed)
    ├── lib/
    │   ├── cashdrawer/              # GS-410B cash drawer control
    │   ├── vfd/                     # VFD display engine (COM3)
    │   └── printer/                 # Receipt printer (TCP/ESC-POS)
    └── templates/
        ├── layouts/
        │   └── pos.html             # POS layout
        └── pos/
            └── index.html           # POS page
```

---

## Database Schema

| Table | Description |
|---|---|
| `menus` | Menu items with name, price (satang), category, VFD name, active flag |
| `menu_categories` | Category names for grouping menus |
| `orders` | Order header with unique code and status (`open` / `paid`) |
| `order_items` | Line items linked to an order and optionally a menu |

Prices are stored as `bigint` in satang (1 THB = 100 satang).

---

## Money Handling

All money is stored as `int64` satang internally. The `go-money` library is used for arithmetic to avoid floating-point errors. Values are converted to THB float only at the API boundary for display.
