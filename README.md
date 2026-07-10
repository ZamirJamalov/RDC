# RDC — Retail Decision Engine

Loan application credit-decision service. Accepts loan applications,
runs the credit decision pipeline (active-loan check, payment-history check,
credit-level determination), and either auto-approves (elite level) or
defers to manual operator approval.

## Quick Start

### Prerequisites
- Go 1.25+
- Microsoft SQL Server (Express is fine for dev) reachable from your machine

### Setup
```bash
# 1. Clone the repo
git clone https://github.com/ZamirJamalov/RDC.git
cd RDC/source

# 2. Copy the env template and fill in your DB credentials
cp ../.env.example ../.env
# Edit .env — at minimum set DB_HOST, DB_USER, DB_PASSWORD

# 3. Export the env vars (or use a tool like direnv)
export $(cat ../.env | grep -v '^#' | xargs)

# 4. Run migrations + start the server
go run .
```

The server starts on `:8000` (configurable via `SERVER_ADDR`).

### Required Environment Variables

| Variable | Required | Default | Description |
|---|:---:|---|---|
| `DB_HOST` | ✅ | — | SQL Server hostname or IP |
| `DB_USER` | ✅ | — | SQL Server login user |
| `DB_PASSWORD` | ✅ | — | SQL Server login password |
| `DB_PORT` | ❌ | `1433` | SQL Server port |
| `DB_NAME` | ❌ | `RDC` | Database name |
| `SERVER_ADDR` | ❌ | `:8000` | HTTP listen address |
| `MIGRATIONS_DROP_RECREATE` | ❌ | `true` | **DANGER**: drops & recreates all tables on startup. MUST be `false` in production |
| `LOG_LEVEL` | ❌ | `info` | One of: `debug`, `info`, `warn`, `error` |

> **Security:** the app will refuse to start if any required env var is missing — there are NO hardcoded credentials.

## API Endpoints

### Loan Applications
| Method | Path | Description |
|---|---|---|
| `POST` | `/api/applications` | Create a new loan application (triggers async credit engine) |
| `GET` | `/api/applications/{id}` | Get application by ID |
| `GET` | `/api/applications/{id}/status` | Get full status: checks + decision |
| `PUT` | `/api/applications/{id}/status` | Manual approve/reject (operator) |
| `GET` | `/api/applications/{id}/checks` | Get all check results |

### LW Mock (dev/test only)
| Method | Path | Description |
|---|---|---|
| `POST` | `/api/mock/lw/setup` | Set up mock loan data for a customer |
| `GET` | `/api/mock/lw/query?customer_pin=...` | Query mock loan data |

## Architecture

```
main.go                          — wiring + graceful shutdown
internal/
├── handler/                     — HTTP handlers + router + middleware chain
│   ├── application_handler.go
│   ├── lw_mock_handler.go
│   └── router.go
├── middleware/                  — RequestID, Recovery, Logger
├── migration/                   — SQL migration runner (GO-batch aware)
├── model/                       — domain types + status constants
├── repository/                  — DB access (raw SQL)
│   ├── application_repo.go
│   └── credit_level_repo.go
└── service/                     — business logic
    ├── application_service.go          — customer flow (Create, Get, GetStatus)
    ├── application_service_status.go   — operator workflow (UpdateStatus)
    ├── credit_engine.go                — pipeline orchestrator
    ├── credit_checks.go                — parallel checks + decision
    └── credit_level.go                 — credit-level determination (pure functions)
pkg/lw/                          — LW provider interface + mock
config/                          — env-based config
migrations/                      — SQL files (idempotent)
```

## Credit Decision Pipeline

```
CreateApplication (sync)
  ├─ validate request
  ├─ check for duplicate pending application
  ├─ PreValidate (sync): determine credit level + check rate exists
  └─ insert application (status=pending)
       │
       └─→ async: ProcessApplication
              ├─ status → checking
              ├─ fetch customer loans from LW (mock)
              ├─ parallel checks:
              │    ├─ active-loan check
              │    └─ payment-history check
              ├─ determine credit level (new / trusted / valuable / elite)
              ├─ determine unlock phase (1 = first loan, 2 = 1+ approved)
              ├─ save check results
              └─ decision:
                   ├─ active loan            → rejected
                   ├─ late payments          → rejected
                   ├─ no applicable rate     → rejected
                   ├─ elite level            → approved (auto)
                   └─ new/trusted/valuable   → pending_approval (manual)
```

## Development

### Build & Run
```bash
go build -o rdc .
./rdc
```

### Vet
```bash
go vet ./...
```

### Test
```bash
go test ./...
```
> Note: tests are scheduled for a later phase (see `Docs/ROADMAP.md` G-27 / T-0.9–T-0.10).

## Roadmap

See [`Docs/ROADMAP.md`](Docs/ROADMAP.md) for the full gap-filling plan between the ALPUL Flow diagram and the current implementation, organized into Phases 0–6.
