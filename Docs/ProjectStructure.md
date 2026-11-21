# Project Structure — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-002 |
| As-of date | 2026-11-21 |
| Source specification | `Design.md` v1.6.1 |
| Scope | Phase 0 skeleton (platform layer only; no business features) |
| Related | `Docs/ProjectStatus.md`, `Docs/ImplementationPlan.md` §Phase 1, `README.md` |

This document maps every generated directory and major file to its
responsibility. It is updated whenever the layout changes.

## 1. Directory tree

```text
GuildBank/
├── cmd/
│   └── guildbank/
│       └── main.go              Process entrypoint: flags, logging, signals, exit codes
├── internal/
│   ├── domain/
│   │   └── setup.go             Shared specification types (D-02 setup state)
│   ├── service/
│   │   ├── health.go            Readiness aggregation over injected interfaces
│   │   └── health_test.go
│   ├── repository/
│   │   ├── sqlite/
│   │   │   ├── store.go         SQLite open/pragmas/setup probe/checkpoint
│   │   │   └── store_test.go
│   │   └── mariadb/
│   │       ├── store.go         MariaDB pool + bounded ping
│   │       └── store_test.go
│   ├── api/
│   │   ├── router.go            Gin engine, route table, JSON error envelope
│   │   ├── middleware.go        Request ID, access log, panic recovery
│   │   ├── health.go            /healthz and /readyz handlers
│   │   ├── health_test.go
│   │   └── importguard_test.go  Fails if api imports a repository
│   ├── web/
│   │   ├── embed.go             Embedded template/static filesystems
│   │   ├── embed_test.go        Import guard + embed checks
│   │   ├── templates/
│   │   │   └── placeholder.html  Phase 2 replaces with setup/login templates
│   │   └── static/
│   │       └── placeholder.css   Phase 2 vendors Bootstrap 5 and HTMX here
│   └── platform/
│       ├── config/
│       │   ├── config.go        Defaults -> env -> flags, validation, redaction
│       │   └── config_test.go
│       ├── logging/
│       │   ├── logging.go       slog JSON/text handler, request-ID context
│       │   └── logging_test.go
│       └── app/
│           ├── app.go           Wiring, startup, graceful shutdown, store close
│           └── app_test.go
├── Docs/                        Frozen specification set and this document
├── .github/workflows/ci.yml     Format, vet, race tests, build, Docker build
├── .dockerignore
├── .env.example                 Documented non-secret local configuration
├── .gitignore
├── Dockerfile                   Multi-stage, static binary, non-root, /config
├── docker-compose.yml           App + MariaDB development topology
├── Makefile                     build/test/lint/docker targets
├── README.md                    Build, run and configuration instructions
├── go.mod / go.sum              Module and pinned dependency graph
└── Design.md                    Authoritative specification v1.6.1
```

Tests sit beside the package they exercise, which keeps fixtures and fakes
local and makes `go test ./...` the single test entrypoint.

## 2. Directory responsibilities

### `cmd/guildbank`

The only `package main`. It parses configuration, constructs the JSON logger,
installs `SIGINT`/`SIGTERM` handling and maps outcomes to exit codes:
`0` success/help, `2` configuration or logging error, `1` runtime failure.
It contains no business logic so the process can be embedded or tested by
calling `platform/app.Run` directly.

### `internal/domain`

Pure specification types shared across layers. Phase 0 defines
`SetupState` (unknown/pending/complete), derived from the `Users` table per
D-02. No I/O, no imports of other application packages. Inventory, import and
audit types arrive with their phases.

### `internal/service`

Business logic and orchestration. Phase 0 has only `HealthService`, which
combines a SQLite pinger, a setup probe and a MariaDB pinger behind
interfaces declared in this package. It never imports a repository: the
concrete stores are injected by `platform/app`. This is the boundary that
later phases extend with settings, inventory, import and audit services.

### `internal/repository/sqlite`

Owns `/config/guildbank.db` (application data: Users, Settings, Sessions,
AuditOutbox in later phases). It opens the file, creates the parent
directory, applies `busy_timeout(5000)`, `journal_mode(WAL)`,
`synchronous(NORMAL)` and `foreign_keys(1)`, caps the pool at one connection
to avoid writer contention (IC-08), exposes `SetupStatus` and checkpoints the
WAL on `Close`. It contains no schema migrations yet: Phase 2 adds them under
`PRAGMA user_version`.

### `internal/repository/mariadb`

Owns the guild store (`GuildBank`: Items, GuildBankItems, ImportBatch,
ImportList, Audit). It builds a DSN with `go-sql-driver/mysql` (UTF-8,
`parseTime`, UTC), sets explicit pool limits (20 open / 10 idle, 30-minute
lifetime, 5-minute idle timeout), and pings with a five-second bound. The
password exists only in the driver configuration and is never logged.
`Items` remains externally owned and read-only; this layer will only query the
contract columns.

### `internal/api`

HTTP entry layer. It depends on `service` interfaces and must not import
`repository` — a rule enforced by `importguard_test.go`. It installs the
middleware order required by IC-20 today (recovery → request ID → access log)
and reserves the later chain (rate limit → session → CSRF → authorization)
for Phase 2. JSON errors use `{"error":{"code","message","request_id"}}`.

### `internal/web`

Server-side rendering assets, embedded with `embed.FS` so the container needs
no asset files on disk. Phase 0 contains placeholders only; Phase 2 vendors
Bootstrap 5 and HTMX locally (no CDN) and adds setup/login templates.

### `internal/platform/config`

Configuration framework: built-in defaults, then environment variables, then
flags. It validates ranges, log vocabulary and trusted-proxy syntax before the
process starts; invalid configuration exits with code 2 and a usage message.
`MariaDB.Redacted()` is the only supported way to print the guild-store
target.

### `internal/platform/logging`

Structured logging built on `log/slog`. JSON is the default; `text` exists for
local development. The handler normalizes timestamps to UTC RFC 3339 and the
package carries request IDs in `context.Context`.

### `internal/platform/app`

The composition root and startup framework. It verifies the data directory is
writable, opens SQLite, attempts MariaDB when configured, wires the services
and router, serves HTTP with bounded timeouts, and on shutdown stops accepting
connections, drains for at most ten seconds, checkpoints SQLite and closes
both pools. A MariaDB failure is logged and surfaced through `/readyz` rather
than preventing startup; Phase 3 makes the pool hot-swappable.

## 3. Request and startup flow

```text
main
 └─ config.Load(defaults -> env -> flags, validate)
 └─ logging.New(slog, JSON)
 └─ app.Run
     ├─ ensureWritableDir(/config)
     ├─ sqlite.Open  ── pragmas, ping, single writer
     ├─ mariadb.Open ── OPTIONAL: pool + 5s ping (failure = degraded)
     ├─ service.NewHealthService(sqlite, setup, mariadb)
     ├─ api.NewRouter(recovery -> requestID -> accessLog, /healthz, /readyz)
     └─ http.Server.ListenAndServe
         └─ on SIGTERM: Shutdown(10s) -> sqlite wal_checkpoint(TRUNCATE) -> close
```

`GET /healthz` is liveness only and always returns `200`. `GET /readyz`
returns `200` only when SQLite responds, an administrator exists and MariaDB
is configured and reachable; otherwise it returns `503` with one entry per
check so operators can distinguish setup-pending from store-unreachable.

## 4. Implementation decisions recorded at Phase 0

| # | Decision | Rationale | Status |
|---|---|---|---|
| PD-01 | Module path `github.com/Panazule/guildbank` (from the git remote, lowercase) | Stable import paths; easy to change before publishing | Provisional |
| PD-02 | SQLite driver `modernc.org/sqlite` | Pure Go; satisfies the static, CGO-free, non-root container requirement | Adopted for skeleton |
| PD-03 | One SQLite connection plus WAL and `busy_timeout(5000)` | Removes writer contention now; dedicated read/write pools can follow with measured need (IC-08) | Adopted for skeleton |
| PD-04 | MariaDB optional at startup; failure is logged and reported by `/readyz` | Phase 3 requires SQLite-backed login/settings to survive a guild-store outage | Adopted for skeleton |
| PD-05 | `GUILDBANK_MARIADB_*` environment bootstrap, superseded by Settings | Exercises the connection layer before the wizard exists; E-11 is unresolved, so this must not become the permanent model | **Approved 2026-11-21 as temporary bootstrap**; Settings become authoritative after the Setup Wizard |
| PD-06 | Setup readiness is a real `Users`-table probe (missing table = pending) | Implements D-02 early and flips to ready after Phase 2 creates the administrator; disabled admins still count so setup cannot reopen | Adopted for skeleton |
| PD-07 | `/readyz` returns `503` until Phase 2 creates an administrator and MariaDB is reachable | Follows IP Phase 1 acceptance and §7.5; container health uses `/healthz` | Adopted for skeleton |
| PD-08 | Gin release mode, custom recovery/request-ID/access-log middleware | Avoids Gin's unstructured defaults; matches IC-20 order and IC-24 logging | Adopted for skeleton |
| PD-09 | Schema migrations absent from Phase 0 | Migrations belong to Phases 2/3 and require approval of the DD DDL; no table is invented early | Deferred |

## 5. Scope boundaries

Implemented: module, layout, configuration, logging, both connection layers,
startup/shutdown, Gin server, middleware, health endpoints, embedded asset
scaffolding, container and CI.

Deliberately absent: user management, authentication, sessions, CSRF, rate
limiting, setup routes, import processing, inventory, audit, business logic,
templates beyond placeholders, and SQL migrations.

Known Phase 0 limitation: if MariaDB is configured but unreachable at
startup, `/readyz` keeps reporting the startup error until the process is
restarted; Phase 3 adds reconnect and hot-swap. The image build and
`go test -race` run in Linux CI; a local Windows race run requires
`CGO_ENABLED=1` and a C toolchain.

## 6. Validation performed

| Check | Result |
|---|---|
| `go build ./...` | Pass |
| `go vet ./...` | Pass, no findings |
| `go test ./...` | Pass (config, logging, app, sqlite, mariadb, service, api, web) |
| Runtime `/healthz` | `200` with `{"status":"ok","version":"dev","uptime_seconds":...}` |
| Runtime `/readyz` | `503` with sqlite `ok`, setup `pending`, mariadb `not_configured` |
| MariaDB connectivity | Live MariaDB 11.4 container: check reported `mariadb: ok`; overall `503` only because setup is pending |
| Unknown route | `404` JSON envelope with request ID |
| SQLite pragmas | `journal_mode=wal`, `busy_timeout=5000`, `foreign_keys=1` asserted in tests |
| Docker image | Built successfully; container runs as UID 10001, `/config` writable, `/healthz` 200 and `/readyz` 503 pre-setup |
| Compose topology | `docker compose up`: app and MariaDB both healthy; `/readyz` reported `mariadb: ok`, overall `503` only for setup pending |
| Volume persistence | `/config` is the named volume `guildbank_config`; `guildbank.db` and a sentinel survived `--force-recreate` and a `docker compose down`/`up` cycle |

## 7. Build and run

See `README.md` for local Windows/Linux commands, Compose usage and the full
configuration table. Short form:

```bash
go build -trimpath -o bin/guildbank ./cmd/guildbank
GUILDBANK_SQLITE_PATH=./config/guildbank.db ./bin/guildbank -port 8080
docker compose up --build
```

## 8. Maintenance rules

1. New code goes into the package that owns its layer; handlers must not
   import repositories (guard test).
2. Every new configuration value is added to `config.Config`, validation,
   `Usage()`, `README.md` and this table.
3. Every new route is added to the route table and to the default-deny
   authorization matrix when Phase 2 introduces auth.
4. Migrations are forward-only and versioned with `PRAGMA user_version`
   (SQLite) / `schema_migrations` (MariaDB), never edited after release.
5. This document and `ProjectStatus.md` are updated at the end of every phase.