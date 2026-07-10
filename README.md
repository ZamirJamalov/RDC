# RDC — Retail Decision Engine

Kredit müraciətlərinin avtomatik qərarlaşdırılması sistemi. Müştəri kredit müraciəti edir, sistem kredit tarixçəsini (LW vasitəsilə), AKB skorunu, qara siyahını, SIMA KYC və MyGov məlumatlarını yoxlayır və avtomatik qərar verir.

## Quick Start

### Option 1: Docker Compose (recommended)

```bash
# 1. Clone
git clone https://github.com/ZamirJamalov/RDC.git
cd RDC

# 2. Configure
cp .env.example .env
# Edit .env — set DB_PASSWORD at minimum

# 3. Run
docker-compose up -d

# 4. Verify
curl http://localhost:8000/api/applications/1
```

### Option 2: Local Go + SQL Server

```bash
# Prerequisites: Go 1.25+, SQL Server (Express is fine)

# 1. Clone
git clone https://github.com/ZamirJamalov/RDC.git
cd RDC/source

# 2. Configure
cp ../.env.example ../.env
# Edit .env — set DB_HOST, DB_USER, DB_PASSWORD

# 3. Export env vars
export $(cat ../.env | grep -v '^#' | xargs)

# 4. Run
go run .
```

Server starts on `:8000` (configurable via `SERVER_ADDR`).

## Configuration

### Required Environment Variables

| Variable | Description |
|---|---|
| `DB_HOST` | SQL Server hostname or IP |
| `DB_USER` | SQL Server login user |
| `DB_PASSWORD` | SQL Server login password |

### Optional Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DB_PORT` | `1433` | SQL Server port |
| `DB_NAME` | `RDC` | Database name |
| `SERVER_ADDR` | `:8000` | HTTP listen address |
| `MIGRATIONS_DROP_RECREATE` | `true` | **DANGER**: drops tables on startup. MUST be `false` in production |
| `LOG_LEVEL` | `info` | One of: debug, info, warn, error |
| `LW_USE_MOCK` | `true` | Mock LW provider (dev) vs real HTTP (prod) |
| `LW_BASE_URL` | `http://localhost:8080` | LW system URL (when `LW_USE_MOCK=false`) |
| `LW_API_KEY` | — | LW API key (required when `LW_USE_MOCK=false`) |
| `LW_TIMEOUT_S` | `30` | LW HTTP timeout |
| `OTP_USE_MOCK` | `true` | Mock OTP (logs codes) vs real SMS gateway |
| `OTP_BASE_URL` | — | SMS gateway URL |
| `OTP_API_KEY` | — | SMS gateway API key |
| `OTP_SENDER` | `RDC` | SMS sender ID |
| `OTP_TIMEOUT_S` | `10` | SMS gateway timeout |
| `SIMA_USE_MOCK` | `true` | Mock SIMA KYC vs real HTTP |
| `SIMA_BASE_URL` | — | SIMA API URL |
| `SIMA_API_KEY` | — | SIMA API key |
| `SIMA_TIMEOUT_S` | `15` | SIMA timeout |
| `MYGOV_USE_MOCK` | `true` | Mock MyGov vs real HTTP |
| `MYGOV_BASE_URL` | — | MyGov API URL |
| `MYGOV_API_KEY` | — | MyGov API key |
| `MYGOV_TIMEOUT_S` | `15` | MyGov timeout |
| `MIN_OFFICIAL_INCOME_AZN` | `300` | Minimum official income for approval |

## API Endpoints

### Loan Applications

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/applications` | Create a new loan application |
| `GET` | `/api/applications/offer` | Get available amount/term ranges for a customer |
| `GET` | `/api/applications/{id}` | Get application by ID |
| `GET` | `/api/applications/{id}/status` | Get full status (checks + decision) |
| `PUT` | `/api/applications/{id}/status` | Manual approve/reject (operator) |
| `GET` | `/api/applications/{id}/checks` | Get all check results |

### OTP (SMS Verification)

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/otp/send` | Send 6-digit code via SMS |
| `POST` | `/api/otp/verify` | Verify code, get verification token |

### LW Router (external data)

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/router/personal-info` | DIN personal info |
| `GET` | `/api/router/akb-score` | AKB credit score |
| `GET` | `/api/router/akb-history` | AKB full history |
| `GET` | `/api/lw/blacklist` | Blacklist check |
| `GET` | `/api/router/asan-finance` | ASAN Finance income |
| `POST` | `/api/lw/loans/approve` | Push approved loan to LW |
| `POST` | `/api/router/sima/init` | SIMA KYC initiation |

### MyGov

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/mygov/permission-link` | Generate MyGov permission URL |
| `POST` | `/api/mygov/fetch-data` | Fetch authorized data from MyGov |

### Expert (Operator) Panel

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/expert/queue` | List pending_approval applications |
| `GET` | `/api/expert/{id}` | Get application for review |
| `PUT` | `/api/expert/{id}/approve` | Approve (requires credit_level) |
| `PUT` | `/api/expert/{id}/reject` | Reject (optional reason) |

### Callbacks (from LW)

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/rdc/callback/sima-result` | SIMA KYC completion callback |

### Mock Data (dev/test only)

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/mock/lw/setup` | Set up mock loan data |
| `GET` | `/api/mock/lw/query` | Query mock loan data |

## Credit Decision Pipeline

```
CreateApplication (sync)
  ├─ validate request
  ├─ check for duplicate pending application
  ├─ PreValidate (sync): determine credit level + check rate exists
  └─ insert application (status=pending)
       │
       └─→ async: ProcessApplication (with retry)
              ├─ status → checking
              ├─ fetch customer loans from LW
              ├─ resolve AKB score from LW (fallback to request)
              ├─ blacklist check (fail-open)
              ├─ determine credit level (new/trusted/valuable/elite)
              ├─ determine unlock phase (1 = first loan, 2 = 1+ approved)
              ├─ run checks (parallel):
              │    ├─ active-loan check
              │    ├─ payment-history check
              │    ├─ credit-level check
              │    └─ blacklist check
              ├─ compute decision:
              │    ├─ blacklisted         → rejected
              │    ├─ active loan         → rejected
              │    ├─ late payments       → rejected
              │    ├─ no applicable rate  → rejected
              │    ├─ elite level         → approved (auto) → LW.ApproveLoan
              │    └─ new/trusted/valuable → pending_approval (manual)
              └─ save checks + decision in transaction
```

## Architecture

```
source/
├── main.go                              — wiring + graceful shutdown
├── main_helpers.go                      — provider factories, log level
├── config/config.go                     — env-based configuration
├── internal/
│   ├── handler/                         — HTTP handlers
│   │   ├── application_handler.go       — loan application CRUD + offer
│   │   ├── otp_handler.go               — OTP send/verify
│   │   ├── lw_router_handler.go         — LW router endpoints
│   │   ├── lw_callback_handler.go       — SIMA callback
│   │   ├── mygov_handler.go             — MyGov endpoints
│   │   ├── expert_handler.go            — operator panel
│   │   ├── lw_mock_handler.go           — mock LW data setup
│   │   └── router.go                    — route registration + middleware
│   ├── middleware/                      — RequestID, Recovery, Logger
│   ├── migration/                       — SQL migration runner
│   ├── model/                           — domain types + constants
│   ├── repository/                      — DB access (raw SQL)
│   │   ├── application_repo.go          — loan applications + checks
│   │   ├── credit_level_repo.go         — credit levels + rates
│   │   ├── otp_repo.go                  — OTP codes
│   │   ├── sima_repo.go                 — SIMA sessions
│   │   ├── mygov_repo.go                — MyGov permissions
│   │   └── tx.go                        — transaction helper
│   └── service/                         — business logic
│       ├── application_service.go       — customer flow
│       ├── application_service_status.go — operator workflow
│       ├── credit_engine.go             — pipeline orchestrator
│       ├── credit_checks.go             — parallel checks
│       ├── credit_decision.go           — decision + LW approve
│       ├── credit_level.go              — credit level logic
│       ├── retry.go                     — async retry
│       ├── otp_service.go               — OTP send/verify
│       ├── otp_helpers.go               — code generation
│       ├── sima_service.go              — SIMA KYC
│       ├── mygov_service.go             — MyGov data access
│       └── contact_check_service.go     — contacts + address validation
├── pkg/                                 — external provider packages
│   ├── lw/                              — LW (loan workflow) system
│   │   ├── provider.go                  — interface
│   │   ├── mock_provider.go             — dev/test
│   │   ├── http_provider.go             — production
│   │   └── model.go                     — request/response types
│   ├── otp/                             — SMS gateway
│   │   ├── provider.go
│   │   ├── mock_provider.go
│   │   └── http_provider.go
│   ├── sima/                            — SIMA KYC
│   │   ├── provider.go
│   │   ├── mock_provider.go
│   │   └── http_provider.go
│   └── mygov/                           — MyGov e-government
│       ├── provider.go
│       ├── mock_provider.go
│       └── http_provider.go
└── migrations/                          — SQL files (idempotent)
    ├── 001_init.sql                     — schema + seed data
    ├── 002_otp_codes.sql                — OTP codes table
    ├── 003_sima_mygov.sql               — SIMA + MyGov tables
    └── 004_application_extra_fields.sql — income + contacts + address
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
go test ./... -v
```

### Docker
```bash
docker build -t rdc-server .
docker run -p 8000:8000 --env-file .env rdc-server
```

### Docker Compose
```bash
docker-compose up -d        # start RDC + SQL Server
docker-compose logs -f rdc  # view logs
docker-compose down          # stop
```

## Credit Levels

| Level | Amount Range | Terms | Min Rate | Max Rate |
|---|---|---|---|---|
| `new` | 100–500 AZN | 3 months | 30% | 30% |
| `trusted` | 100–900 AZN | 3, 6 months | 27% | 29% |
| `valuable` | 100–1300 AZN | 3, 6, 9 months | 25% | 28% |
| `elite` | 100–3000 AZN | 3, 6, 9, 12 months | 20% | 27% |

Each level has **phase 1** (first loan) and **phase 2** (after 1+ approved loan) with different rate ranges.

## Roadmap

See [`Docs/ROADMAP.md`](Docs/ROADMAP.md) for the full gap-filling plan between the ALPUL Flow diagram and the implementation, organized into Phases 0–6.

**Phase 0–6: COMPLETE** ✅

## License

Proprietary — All rights reserved.
