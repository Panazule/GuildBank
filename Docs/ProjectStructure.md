# Project Structure — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-002 |
| As-of date | 2026-11-21 |
| Source specification | `Design.md` v1.6.1 |
| Scope | Phases 0–4 approved; Phase 1 identity/settings, Phase 2 item metadata, Phase 3 import pipeline and Phase 4 public inventory read path implemented (no manual inventory, general audit UI or user management) |
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
│   │   ├── setup.go              D-02 setup state
│   │   ├── identity.go           User/session/setting models, sentinel and validation errors
│   │   ├── item.go               Logical Item and ItemReference contracts
│   │   ├── import.go             Import batch/row/location/warning/commit contracts
│   │   └── inventory.go          Public inventory sort/filter/projection/page contracts
│   ├── service/
│   │   ├── health.go             Readiness aggregation over injected interfaces
│   │   ├── mask.go               Shared mask decoding and structured warnings
│   │   ├── class_mask.go         ClassList → ClassString
│   │   ├── race_mask.go          RaceList → RaceString
│   │   ├── item_metadata.go      EQUID resolution and GuildBankItems metadata generation
│   │   ├── password.go           bcrypt hashing, policy and timing equalizer
│   │   ├── auth.go               Login, session issuance/validation, logout, revocation
│   │   ├── rate_limit.go         D-12 IP+username login lockout
│   │   ├── setup.go              Wizard validation, DB test and atomic commit
│   │   ├── settings.go           Typed setting allowlist, validation and session policy
│   │   ├── import_parser.go      Canonical TSV parser, limits and diagnostics
│   │   ├── import_normalize.go   D-05/PD3-01 location and Hoard normalization
│   │   ├── import_sort.go        Natural sort keys for location and name
│   │   ├── import_upload.go      Metadata resolution, classification and durable staging
│   │   ├── import_commit.go      Selection, summary and atomic commit planning
│   │   ├── import_service.go     Import service wiring, reads and lifecycle
│   │   ├── import_warning.go     Structured warning encoding for WarningText
│   │   ├── inventory.go          Public filter validation, UTC availability, EQResource URL, mapping
│   │   └── *_test.go
│   ├── integration/
│   │   ├── items_test.go        Opt-in full-source Items metadata test
│   │   └── inventory_perf_test.go Opt-in 250k/1M public-inventory performance test
│   ├── repository/
│   │   ├── sqlite/
│   │   │   ├── store.go          Open/pragmas/setup probe/checkpoint
│   │   │   ├── migrate.go        Forward-only PRAGMA user_version runner
│   │   │   ├── migrations/       0001_phase1.sql (Users, Settings, Sessions)
│   │   │   ├── users.go          Account lookup, LastLogin, admin count
│   │   │   ├── sessions.go       Hashed-token CRUD, touch, expiry purge
│   │   │   ├── settings.go       Typed settings list/upsert
│   │   │   ├── setup.go          Atomic administrator + settings transaction
│   │   │   └── *_test.go
│   │   └── mariadb/
│   │       ├── store.go          MariaDB pool, bounded ping and connection test
│   │       ├── migrate.go        Embedded guild-store migration runner (GET_LOCK)
│   │       ├── migrations/       0001_guild_store.sql, 0002_public_inventory_indexes.sql, 0003_admin_inventory.sql, 0004_import_ignored_rows.sql, 0005_item_quantity_model.sql and 0006_inventory_movement.sql
│   │       ├── items.go          Read-only physical→logical Items repository
│   │       ├── imports.go        Batch/row/location/selection reads and staging
│   │       ├── imports_commit.go Advisory-locked atomic import commit and audit
│   │       ├── admin_backfill.go Startup LocationGroup/LocationSort repair
│   │       ├── spell_metadata_backfill.go Startup class/race repair for spell rows
│   │       ├── item_consolidation.go QTY-09 owner-run duplicate-item-stack consolidation
│   │       ├── inventory.go      Explicit public inventory projection/count/page/detail
│   │       ├── errors.go         MariaDB error classification
│   │       └── *_test.go
│   ├── api/
│   │   ├── router.go             Route groups, middleware order, JSON 404/405
│   │   ├── auth.go               Login/logout/session/csrf handlers
│   │   ├── setup.go              Setup page and step handlers
│   │   ├── settings.go           Settings page and REST handlers
│   │   ├── imports.go            Import upload, review, selection and commit handlers
│   │   ├── inventory.go          Public list/detail HTML and JSON handlers, DTO/view models
│   │   ├── views.go              Embedded template rendering
│   │   ├── ui.go                 Shared HTML page helpers
│   │   ├── csrf.go               Signed double-submit CSRF
│   │   ├── cookies.go            Session/CSRF cookie flags and redirect safety
│   │   ├── middleware.go          Request ID, access log, panic recovery
│   │   ├── middleware_auth.go    Setup gate, default-deny auth, admin check, headers
│   │   ├── errors.go             Domain-error to HTTP status mapping
│   │   ├── health.go             /healthz and /readyz handlers
│   │   └── *_test.go
│   ├── web/
│   │   ├── embed.go              Embedded template/static filesystems
│   │   ├── embed_test.go         Import guard + embed checks
│   │   ├── templates/            setup, login, settings, imports, import_detail, inventory_list, inventory_detail, admin_inventory, admin_inventory_form, admin_inventory_detail, admin_remove, admin_bulk_remove, admin_audit, error, partials
│   │   └── static/               app.css, setup.js and imports.js
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

Pure specification types shared across layers. Defines `SetupState`
(unknown/pending/complete) per D-02 plus `User`, `Session`, `Setting`, logical
`Item`/`ItemReference`, and the Phase 3 `ImportBatch`/`ImportRow`/`ImportLocation`/
`ImportWarning`/commit contracts, sentinel errors and field validation errors. No
I/O, no imports of other application packages. Public/manual inventory and general
audit types arrive with their phases.

### `internal/service`

Business logic and orchestration. Phase 1 contains the health aggregation,
setup validation/commit, bcrypt password helpers, authentication and session
lifecycle, the D-12 login limiter and typed settings. Phase 2 adds
repository-independent class/race decoders and `ItemMetadataService`, which
resolves an imported `EQUID` against Items and generates `GuildBankItems`
metadata. Phase 3 adds the streaming import parser, location/Hoard normalization
(PD3-01), natural sort keys, metadata-aware staging, full-replacement selection,
unit-aware summaries, spell aggregation and atomic commit planning
(PD3-03/PD3-04/PD3-14). The package never imports a repository: concrete stores
are injected by `platform/app` through the interfaces declared here.

### `internal/repository/sqlite`

Owns `/config/guildbank.db` (application data: Users, Settings, Sessions). It
opens the file, creates the parent directory, applies `busy_timeout(5000)`,
`journal_mode(WAL)`, `synchronous(NORMAL)` and `foreign_keys(1)`, caps the pool
at one connection to avoid writer contention (IC-08), runs forward-only
migrations under `PRAGMA user_version`, exposes `SetupStatus` and checkpoints
the WAL on `Close`.

`0001_phase1.sql` creates the Phase 1 tables and seeds the documented settings.
`CompleteSetup` inserts the first administrator and every wizard setting in one
transaction; the administrator row is the D-02 switch. A missing `Users` table
after migration is treated as corruption: `SetupStatus` fails closed instead of
offering setup.

### `internal/repository/mariadb`

Owns the guild store (`GuildBank`: Items, GuildBankItems, ImportBatch,
ImportList, Audit). It builds a DSN with `go-sql-driver/mysql` (UTF-8,
`parseTime`, UTC), sets explicit pool limits (20 open / 10 idle, 30-minute
lifetime, 5-minute idle timeout), and pings with a five-second bound. The
password exists only in the driver configuration and is never logged.
`Items` remains externally owned and read-only; `items.go` is the only file
that knows the physical lowercase table and its `id`/`name`/`classes`/`races`
columns (PD-19). It provides single, batch, count and keyset-page reads and no
write method. Production grants must still enforce `SELECT` only (E-01).

`migrate.go` applies embedded forward-only migrations under a per-database
`GET_LOCK` and records them in `schema_migrations`; `0001_guild_store.sql`
creates `ImportBatch`, `ImportList`, `GuildBankItems` and `Audit`, and
`0002_public_inventory_indexes.sql` adds the four approved public order indexes
without changing columns or data. `imports.go` owns durable batch/staging writes,
paged review reads and full-replacement selection. `imports_commit.go` owns the
advisory-locked atomic commit, item-unit expansion, spell merge into the lowest
active target and transactional import audit. `inventory.go` owns the explicit
public projection, active count/page snapshot and active-only detail; it has no
write method. `admin_backfill.go` repairs missing derived locations at startup;
`spell_metadata_backfill.go` repairs the class/race strings of spell rows written
before the DEFECT-01 commit fix, under its own advisory lock. `errors.go`
classifies deadlocks and unavailable
store errors for the service layer.

### `internal/api`

HTTP entry layer. It depends on `service` types and must not import
`repository` — a rule enforced by `importguard_test.go`. Middleware order is
recovery → request ID → access log → security headers → setup gate, then
public CSRF or, for admin routes, session → CSRF → administrator check. JSON
errors use `{"error":{"code","message","fields","request_id"}}`. Setup and
login HTML forms post to the same REST handlers as JSON clients; POST settings
is a thin HTML adapter for PUT. Phase 3 adds the admin-only import routes for
upload, history, detail, locations, rows, row IDs, selection, summary and commit.
The multipart upload uses the path-aware body limit and multipart CSRF support;
`imports.go` maps parse, selection, terminal-state, duplicate-commit and
unavailable-store errors to `409`/`413`/`422`/`503`. Phase 4 adds the public
inventory group: `/`, `/inventory/:id`, `/api/v1/items` and
`/api/v1/items/:id` are registered after the setup gate without `RequireAuth`.
`inventory.go` owns query parsing, explicit JSON DTOs, HTML view models,
pagination/sort link rebuilding, a five-second read deadline and 404/422/503
mapping. Authentication may change only navigation; it never changes the item
payload.

### `internal/web`

Server-side rendering assets, embedded with `embed.FS` so the container needs
no asset files on disk. Phase 1 adds setup, login, settings and error templates
plus `app.css` and a small vanilla setup script. Phase 3 adds the import
history/upload page, the batch review page (location filter available in
read-only terminator states), and `imports.js` for upload state, paged rows,
location toggles, debounced selection saves and commit. Phase 4 replaces the
placeholder home page with the public inventory list and adds the item-detail
template; both are full-page, JavaScript-free renders with the same embedded
CSS. No CDN is used. Bootstrap/HTMX adoption remains a recorded decision
(PD-12); the import UI deliberately uses vanilla `fetch` (PD3-09).

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
writable, opens and migrates SQLite, attempts MariaDB when configured, wires
the services/router and the hourly session purge, serves HTTP with bounded
timeouts, and on shutdown stops accepting connections, drains for at most ten
seconds, checkpoints SQLite and closes both pools. A MariaDB failure is logged
and surfaced through `/readyz` rather than preventing startup. A configured
MariaDB store is migrated at startup (fatal on migration failure) and interrupted
`Processing` batches are recovered; the pool is not Settings-backed or
hot-swappable in Phase 3 (PD-18).

## 3. Request and startup flow

```text
main
 └─ config.Load(defaults -> env -> flags, validate)
 └─ logging.New(slog, JSON)
 └─ app.Run
     ├─ ensureWritableDir(/config)
     ├─ sqlite.Open  ── pragmas, ping, single writer
     ├─ sqlite.Migrate ── PRAGMA user_version = 1 (Users, Settings, Sessions)
     ├─ mariadb.Open ── OPTIONAL: pool + 5s ping (failure = degraded)
     ├─ mariadb.Migrate ── 0001…0006 guild-store migrations (fatal on migration error)
     ├─ mariadb.RecoverInterrupted ── fail stranded Processing batches
     ├─ mariadb.BackfillInventoryLocations ── fill missing LocationGroup/LocationSort
     ├─ mariadb.BackfillSpellMetadata ── repair pre-DEFECT-01 spell class/race strings
     ├─ [optional -consolidate-items] mariadb.ConsolidateItemStacks ── QTY-09 then exit
     ├─ service.NewHealthService(sqlite, setup, mariadb)
     ├─ setup/auth/settings/import services + hourly session purge
     ├─ api.NewRouter(setup gate -> public CSRF or session -> CSRF -> admin)
     └─ http.Server.ListenAndServe
         └─ on SIGTERM: Shutdown(10s) -> sqlite wal_checkpoint(TRUNCATE) -> close
```

`GET /healthz` is liveness only and always returns `200`. `GET /readyz`
returns `200` only when SQLite responds, an administrator exists and MariaDB
is configured and reachable; otherwise it returns `503` with one entry per
check so operators can distinguish setup-pending from store-unreachable.
Once an administrator exists, `/setup` and all setup routes return `410`;
while setup is pending, other HTML routes redirect to `/setup` and other API
routes return `503 setup_required`.

## 4. Implementation decisions

### 4.1 Phase 0 decisions

| # | Decision | Rationale | Status |
|---|---|---|---|
| PD-01 | Module path `github.com/Panazule/guildbank` (from the git remote, lowercase) | Stable import paths; easy to change before publishing | **Approved with Phase 0**; used by Phases 1–3 |
| PD-02 | SQLite driver `modernc.org/sqlite` | Pure Go; satisfies the static, CGO-free, non-root container requirement | Adopted |
| PD-03 | One SQLite connection plus WAL and `busy_timeout(5000)` | Removes writer contention now; dedicated read/write pools can follow with measured need (IC-08) | Adopted |
| PD-04 | MariaDB optional at startup; failure is logged and reported by `/readyz` | SQLite-backed login/settings must survive a guild-store outage | Adopted |
| PD-05 | `GUILDBANK_MARIADB_*` environment bootstrap, superseded by Settings | Exercises the connection layer before Phase 3; E-11 is unresolved, so this must not become the permanent model | **Approved 2026-11-21 as temporary bootstrap**; the wizard persists values, but Phase 3 deliberately did not replace the runtime pool |
| PD-06 | Setup readiness is a real `Users`-table probe | Implements D-02; disabled admins still count so setup cannot reopen | Superseded by PD-14 (missing table fails closed) |
| PD-07 | `/readyz` returns `503` until an administrator exists and MariaDB is reachable | Follows IP acceptance and §7.5; container health uses `/healthz` | Adopted |
| PD-08 | Gin release mode, custom recovery/request-ID/access-log middleware | Avoids Gin's unstructured defaults; matches IC-20/IC-24 | Adopted |
| PD-09 | Schema migrations deferred from Phase 0 | Required DD DDL approval before inventing tables | Closed in Phase 1 |

### 4.2 Phase 1 decisions (approved 2026-11-21)

| # | Decision | Rationale | Status |
|---|---|---|---|
| PD-10 | Migration `0001` creates only `Users`, `Settings`, `Sessions`; no `AuditOutbox` | Owner Phase 1 scope forbids audit; outbox arrives with the audit recorder | **Approved 2026-11-21** |
| PD-11 | bcrypt cost 10; password policy 12–72 bytes, not equal to username; usernames 3–64 of `[A-Za-z0-9._-]` | Closes the minimum for MR-11; composition rules and admin reset flow still need E-07 approval | **Approved 2026-11-21** |
| PD-12 | Custom embedded CSS and vanilla JS instead of Bootstrap 5/HTMX in Phase 1 | Required screens are small plain forms; no CDN; framework adoption can follow with inventory UI (E-16) | **Approved 2026-11-21** |
| PD-13 | Signed double-submit CSRF cookie (HMAC, process-local secret); `SameSite=Lax`, `HttpOnly`, `Secure` via explicit config | Covers login/setup and all writes without a pre-session token store; restart invalidates outstanding forms | **Approved 2026-11-21** |
| PD-14 | Missing `Users` table after migration fails closed; setup is pending only when the table exists and has zero admins | DD §3.5/DC-24; prevents corruption from reopening unauthenticated setup | **Approved 2026-11-21** |
| PD-15 | Session lifecycle 30-minute idle / 8-hour absolute, sliding refresh throttled to once per minute, hourly purge | Implements E-08 defaults; no refresh-token or rotation beyond login | **Approved 2026-11-21** |
| PD-16 | In-process login limiter keyed IP+username: 5 failures → 15-minute lockout; fifth failure returns `429` with `Retry-After` | Implements D-12 defaults for a single instance; multi-instance needs a shared store | **Approved 2026-11-21** |
| PD-17 | `GUILDBANK_SECURE_COOKIES` (flag/env) explicitly enables `Secure`; no automatic `X-Forwarded-Proto` inference | Avoids trusting spoofable headers; operator must enable behind TLS. Public exposure without it needs approval | **Approved 2026-11-21** |
| PD-18 | Wizard persists MariaDB settings, but the runtime pool still uses `GUILDBANK_MARIADB_*` | Keeps Phase 1 free of MariaDB business wiring; the wizard requires a successful test before committing the admin (PD-05 remains runtime bootstrap) | **Approved 2026-11-21; still in force** — Phase 3 uses the bootstrap pool and does not hot-swap or adopt Settings-backed connections |

The owner approved PD-10–PD-20 on 2026-11-21. The approved `ClassMaskDecoder`
and `RaceMaskDecoder` are part of the Phase 2 baseline; Blade of Carnage and
Flayed Skin Cap are permanent validation fixtures. Phase 3 was subsequently
approved (PD3-01–PD3-14 and all six plan defaults) and implemented from
`Phase3ImplementationPlan.md`.

### 4.3 Pre-Phase 2 data-source decisions

These decisions arise from the development `Items` copy and are separate from
the Phase 1 approval set. Both were approved by the owner on 2026-11-21.

| # | Decision | Rationale | Status |
|---|---|---|---|
| PD-19 | Map `GuildBank.items` physical columns `id`, `name`, `classes`, `races` to the logical `EQUID`, `Name`, `ClassList`, `RaceList` inside the read-only item repository; do not rename, alter or view-wrap the external table | The actual source is lowercase with different column names; one repository must isolate that contract (`Docs/ItemsDataProfile.md` §2) | **Approved and implemented 2026-11-21**; `internal/repository/mariadb/items.go` provides read-only single, batch, count and keyset-page reads behind `domain.Item` |
| PD-20 | Decode only defined low 16 bits; treat `65535` as `ALL`, `0` as blank, and preserve the row with a non-fatal warning for undefined high bits; do not reuse `MetadataMissingFg` | 570 class and 628 race values exceed the specified 16-bit masks; rejecting them would fail otherwise valid metadata, while silently dropping high bits needs explicit approval | **Approved and implemented 2026-11-21**; repository-independent decoders and `ItemMetadataService`; import warnings persist through PD3-02 |

### 4.4 Phase 3 decisions (approved 2026-11-21)

PD3-01 through PD3-14 and all six `Phase3ImplementationPlan.md` §14.1 defaults
were approved and implemented. The authoritative rationale and impact analysis
remain in `Phase3ImplementationPlan.md` §2 and §14.1.

| # | Decision | Status |
|---|---|---|
| PD3-01 | Equipment/blank/unparseable locations → `Unspecified`; `Hoard<n>` → `Hoard` | **Approved and implemented** |
| PD3-02 | Row warnings stored as structured JSON in `ImportList.WarningText` | **Approved and implemented** |
| PD3-03 | Merge spells only into active (`DeletedFg=0`) rows; insert otherwise | **Approved and implemented** |
| PD3-04 | Aggregate staged spells by EQUID; lowest active id on anomaly; advisory lock | **Approved and implemented** |
| PD3-05 | SourceRowCount is physical lines; AcceptedRowCount is staged rows; summary reports units and records (QTY-04: records count rows) | **Approved and implemented** |
| PD3-06 | 10 MiB body, 50k lines, 64 KiB/line, 100k inserted rows, 10k count/row (QTY-04) | **Approved and implemented** |
| PD3-07 | Canonical item names with missing-metadata fallback; spells keep imported names | **Approved and implemented** |
| PD3-08 | Upload/commit audited; selection changes are not audited | **Approved and implemented** |
| PD3-09 | Vanilla `imports.js` and JSON endpoints; no HTMX vendor file | **Approved and implemented** |
| PD3-10 | Guild migration failure is fatal; unreachable store remains degraded | **Approved and implemented** |
| PD3-11 | Persist `LocationSort` and `NameSort` with the import sort index | **Approved and implemented** |
| PD3-12 | Golden sample counts 548/197/348/2/1, 30 groups, 723 item units, 3 spells; QTY-04: 163 reviewable item rows create 163 records | **Approved and asserted** |
| PD3-13 | 120-second HTTP and MariaDB DSN read/write timeouts | **Approved and implemented** |
| PD3-14 | Upload/commit audit summary carries units and records; one `ITEM_UPDATED` per merged spell, not per unit (QTY-05) | **Approved and implemented** |

## 5. Scope boundaries

Implemented: module, layout, configuration, logging, both connection layers,
startup/shutdown, Gin server, security middleware, health endpoints, SQLite
migration runner and Phase 1 schema, four-step setup wizard, administrator
creation with bcrypt, login/logout, server-side sessions, CSRF, login lockout,
default-deny admin routes, typed settings read/update, embedded UI, the
read-only Items repository, class/race decoders and service-layer item-metadata
generation.

Phase 3 adds `domain/import.go`, parser/normalizer/sort/upload/commit service
files, `repository/mariadb/{migrate,imports,imports_commit,errors}.go` with the
embedded `0001_guild_store.sql`, `api/imports.go`, the import templates,
`imports.js` and import handler/parser/commit tests. It writes `ImportBatch`,
`ImportList`, `GuildBankItems` and import-scoped `Audit` rows, and is implemented
and live-migrated in Compose.

Phase 4 adds `domain/inventory.go`, `service/inventory.go`,
`repository/mariadb/inventory.go`, `api/inventory.go`, the two public templates,
`0002_public_inventory_indexes.sql`, the opt-in 250k/1M performance test and
public contract/security tests. It exposes active `GuildBankItems` records
anonymously without changing the Phase 3 write model.

Deliberately absent: user management, general audit/outbox, manual inventory
CRUD, admin inventory grid/DTO, dashboard, location/availability filtering,
HTTP item-metadata routes, in-app anonymous rate limiting, Phase 5 date-window
write validation, and Settings-backed MariaDB pool adoption.

Remaining limitations: if MariaDB is configured but unreachable at startup,
`/readyz` keeps reporting the startup error until the process is restarted;
Phase 3 does not add reconnect or hot-swap. The wizard persists MariaDB settings
but the running process still uses the environment bootstrap (PD-18). The login
limiter is in-process, so multiple replicas do not share lockouts (PD-16).
`ItemMetadataService` is consumed by the import pipeline but has no separate
metadata route. Import warnings are persisted in `ImportList.WarningText`; the
authenticated Compose upload → selection → commit workflow passed on
2026-11-22 UTC (`ProjectStatus.md` §1.5.1). The image build and `go test -race`
run in Linux CI; a local Windows race run requires `CGO_ENABLED=1` and a C
toolchain.

## 6. Validation performed

| Check | Result |
|---|---|
| `go build ./...` | Pass |
| `go vet ./...` | Pass, no findings |
| `go test ./...` | Pass (config, logging, app, sqlite, mariadb, service, api, web; the live integration package is skipped without opt-in settings) |
| Migration tests | Fresh DB reaches `user_version=1`; tables/seeds present; rerun idempotent; newer schema refused; failed setup transaction rolls back |
| Service tests | bcrypt round trip/policy, setup atomicity, DB-failure no-write, login parity, fifth-failure `429`, idle expiry, disabled revocation, logout/revoke-all, settings validation, class/race decoder fixtures and undefined-high-bit warnings |
| Item metadata tests | Found EQUIDs use source `Name` and decoded strings; absent EQUIDs preserve imported name, blank class/race, set `MetadataMissing`, and emit one `metadata_missing` warning; batch order, validation, source errors and warning propagation covered |
| Repository mapping tests | `items.id/name/classes/races` → `EQUID/Name/ClassList/RaceList`, blank masks → 0, malformed masks rejected, missing row → `domain.ErrNotFound` |
| Repository batch/page tests | Duplicate EQUIDs collapsed; invalid page sizes rejected; repository source contains no write statement |
| API tests | Setup wizard and 410 closure, CSRF 403, default-deny 401/redirect, cookie flags, raw token absent from SQLite, secret redaction, settings persistence; import multipart CSRF, upload, selection, `LocationGroup` locations and rendered pages; public JSON/HTML contracts, anonymous/auth projection parity, forbidden-field absence, XSS escaping, 404/405/422/503 and invalid-cookie handling |
| Public inventory tests | Filter normalization, 128-rune search boundary, UTC availability boundaries/inverted windows, EQResource validation, pagination math, repository predicate/order allowlists, explicit projection and no `SELECT *` |
| Public performance gates | 250k active + 25k deleted and 1M active + 100k deleted disposable MariaDB runs; all list/detail p95 gates passed; natural order `1, 2, 10, 11`; EXPLAIN captured. See `Phase4Validation.md` |
| Guild migration (live Compose) | Rebuilt image; startup logged `guild store schema ready schema_version=0002_public_inventory_indexes`; all five guild-store tables, the new public indexes and both version rows exist; `/healthz` healthy |
| Public live paths | Anonymous `/` and `/api/v1/items` `200`; search/class/race/type counts, natural/reverse sorts, page metadata, detail/list parity and EQResource attributes verified; invalid `422`, absent `404`; restart preserved 706 records |
| Public outage/recovery | MariaDB stop: item API `503 dependency_unavailable`, `/readyz` `503`; MariaDB start: `/readyz` and item API `200` |
| Import parser/commit tests | Golden sample 548/197/348/2/1, 30 groups with one `Hoard`, 723 item units and 3 spells; natural sort and normalization cases; item expansion, spell aggregation, lowest active target, missing metadata and warning cap |
| Runtime `/healthz` | `200` |
| Runtime `/readyz` before setup | `503` with sqlite `ok`, setup `pending`, mariadb `ok` |
| Runtime `/readyz` after setup | `200` with sqlite/setup/mariadb all `ok` |
| Live setup | Four-step payload completed against Compose MariaDB; one `Admin` row, 60-byte bcrypt hash, settings persisted, session issued |
| Live auth | Login `200`; `HttpOnly` `SameSite=Lax` session cookie; `/api/v1/auth/session` `200`; logout `204`; session afterward `401` |
| Live setup closure | `GET /setup` after completion returns `410` with only the closure body |
| Live settings | `PUT /api/v1/settings` `200`; GuildName/open-days persisted; secret value returned empty with `isSecret=true` |
| SQLite state (copied from volume with WAL) | `user_version=1`; `Users`, `Settings`, `Sessions`; session `TokenHash` length 64; raw cookie absent |
| Unknown route | `404` JSON envelope with request ID |
| Docker/Compose | Image built; app and MariaDB healthy; restart retained administrator/settings and returned ready `200` |
| Development `Items` profile (read-only) | `GuildBank.items`: 144,712 rows, 144,712 distinct `id`; both published fixtures (`Blade of Carnage`, `Flayed Skin Cap`) decode as specified; 570/628 masks exceed 16 bits and now produce non-fatal warnings under approved PD-20 |
| Live Items integration (opt-in) | Static Linux test binary run on the Compose network: `CountItems` = 144,712, keyset scan covered every row exactly once, class/race high-bit counts = 570/628, fixture metadata and a missing EQUID matched D-08; `PASS` |
| Live import commit | Pass. Fresh-volume setup and admin login, canonical sample upload (548/197/348), 30 `LocationGroup` options with one `Hoard`, 8-group subset selection (77 rows, 471 item units, 3 spells) and atomic commit (474 records); `ImportBatch`/`ImportList`/`GuildBankItems`/`Audit` verified, re-commit `409`, and a second batch produced 2 spell merges plus `ITEM_UPDATED` audit rows. Evidence in `ProjectStatus.md` §1.5.1 |

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
   authorization matrix.
4. Migrations are forward-only and versioned with `PRAGMA user_version`
   (SQLite) / `schema_migrations` (MariaDB), never edited after release.
5. This document and `ProjectStatus.md` are updated at the end of every phase.