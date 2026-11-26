# Aeternus Guild Bank

Self-hosted EverQuest guild inventory for Docker on Unraid. The authoritative
specification is [`Design.md`](Design.md) v1.6.1; delivery is tracked in
[`Docs/ProjectStatus.md`](Docs/ProjectStatus.md) and
[`Docs/ImplementationPlan.md`](Docs/ImplementationPlan.md).

**Current scope: Phase 0 approved; Phase 1 approved and complete; Phase 2
item-metadata integration approved and complete; Phase 3 import pipeline
approved, complete and live-validated; Phase 4 public inventory read path
approved, complete and live-validated. Phase 5 is not started.**
The process builds, starts, migrates SQLite and MariaDB, serves the four-step
setup wizard, creates a bcrypt-hashed administrator, issues server-side
sessions, enforces CSRF and default-deny admin routes, rate-limits login and
persists typed settings. The MariaDB item repository is read-only and resolves
EQUIDs into decoded class/race metadata, including the approved
missing-metadata behavior. The development `GuildBank.items` copy was imported
and validated with 144,712 rows; PD-10 through PD-20 are approved. Phase 3 adds
the admin import pipeline: upload, parsing, normalized staging, selection and
atomic commit into `GuildBankItems`, with import-scoped audit writes and the
`/admin/imports` screens. PD3-01 through PD3-14 and all six Phase 3 plan
defaults are approved. Phase 4 adds the anonymous inventory list, item detail
and `/api/v1/items` API with name search, class/race/type filters, natural/date
sorts, deterministic pagination, UTC availability states and EQResource links.
The authenticated Compose upload → selection → commit workflow and the public
list/detail/API, restart, outage and recovery paths have passed
(`Docs/Phase4Validation.md`). Manual inventory administration, the general
audit UI/outbox and user management remain later phases; Phase 5 has not
started.

## Requirements

- Go 1.27+ (module `github.com/Panazule/guildbank`)
- Docker 24+ with Compose v2 (optional, for containers)
- MariaDB 11.4+ (required to complete the setup wizard; the service still
  starts if it is unavailable, but the wizard's connection test and readiness
  remain failed until a reachable configuration is supplied)

## Build and run (local)

Linux/macOS:

```bash
make run
# or
go build -o bin/guildbank ./cmd/guildbank
GUILDBANK_SQLITE_PATH=./config/guildbank.db ./bin/guildbank
```

Windows PowerShell:

```powershell
go build -o bin\guildbank.exe .\cmd\guildbank
$env:GUILDBANK_SQLITE_PATH = ".\config\guildbank.db"
.\bin\guildbank.exe -port 8080 -log-level debug
```

The SQLite parent directory is created automatically and the Phase 1 schema is
migrated on startup. On a fresh database `/readyz` returns `503` until the
setup wizard creates an administrator; `/healthz` returns `200` whenever the
process is alive. Open `http://localhost:8080/setup` to run the wizard. The
wizard requires a reachable MariaDB instance to complete.

Passwords are hashed with bcrypt and are never stored in plaintext. Session
tokens are random 32-byte values; SQLite stores only their SHA-256 hashes.
CSRF tokens are required on login, setup and every state-changing request.
The MariaDB password is stored in SQLite (there is no SQLite-native secret
protection; the `/config` volume permission boundary is the current mitigation,
E-11). Setup routes are intentionally unauthenticated while no administrator
exists, so expose a fresh instance only to a trusted network until setup
completes.

## Docker Compose (app + MariaDB)

```bash
docker compose up --build
curl http://localhost:8080/healthz
curl -i http://localhost:8080/readyz   # 503 until setup completes
# open http://localhost:8080/setup
```

Compose uses disposable development credentials and two named volumes:
`guildbank_config` for `/config` and `guildbank_mariadb` for the database.
Both survive container recreation and `docker compose down`; only
`docker compose down -v` (or deleting the volumes) removes the data. A bare
`docker run` needs `-v guildbank_config:/config` to persist `guildbank.db`
the same way. The application container runs as UID/GID 10001 and only
writes `/config` and `/tmp`.

## Configuration

Precedence: flags > environment > defaults.

| Flag | Environment | Default | Purpose |
|---|---|---|---|
| `-port` | `GUILDBANK_PORT` | `8080` | HTTP listen port |
| `-sqlite-path` | `GUILDBANK_SQLITE_PATH` | `/config/guildbank.db` | Application database |
| `-log-level` | `GUILDBANK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `-log-format` | `GUILDBANK_LOG_FORMAT` | `json` | `json` or `text` |
| `-secure-cookies` | `GUILDBANK_SECURE_COOKIES` | `false` | Set `Secure` on session and CSRF cookies; enable when served over HTTPS |
| — | `GUILDBANK_TRUSTED_PROXIES` | empty | Comma-separated IPs/CIDRs allowed to set forwarded headers; empty trusts none |
| — | `GUILDBANK_MARIADB_HOST` | empty | **Development bootstrap only**; the wizard persists Settings but the live pool still uses this value |
| — | `GUILDBANK_MARIADB_PORT` | `3306` | Bootstrap guild store port |
| — | `GUILDBANK_MARIADB_DATABASE` | `GuildBank` | Bootstrap guild store database |
| — | `GUILDBANK_MARIADB_USERNAME` | empty | Bootstrap guild store user |
| — | `GUILDBANK_MARIADB_PASSWORD` | empty | Bootstrap guild store password (never logged) |

`GUILDBANK_MARIADB_*` exists so the connection layer can be exercised before
the setup wizard stores credentials in SQLite. The wizard persists those
credentials in `Settings`, but the running pool still uses this environment
bootstrap. Phase 3 deliberately did not hot-swap the pool; changing connection
settings takes effect after a restart (PD-05/PD-18 remain in force).

## Endpoints

| Method | Path | Behaviour |
|---|---|---|
| GET | `/healthz` | Always `200` while the process runs; JSON with version and uptime |
| GET | `/readyz` | `200` only when SQLite, setup state and MariaDB are all healthy; `503` otherwise with per-check reasons |
| GET | `/setup` | Four-step wizard while no administrator exists; `410` afterwards |
| POST | `/setup/admin`, `/setup/guild`, `/setup/database/test` | Validate a wizard step; no persistence except the connection test's transient probe |
| POST | `/setup/complete` | Atomically create the administrator and settings, then start a session |
| GET | `/` | Public inventory list; search, class/race/type filters, sort and pagination |
| GET | `/inventory/:id` | Public item detail for one active `GuildBankItemID`; absent/deleted `404` |
| GET | `/api/v1/items` | Public paginated inventory JSON with the same filters and sort contract |
| GET | `/api/v1/items/:id` | Public item-detail JSON; absent/deleted `404` |
| GET | `/login` | Login page (redirects to setup while setup is pending) |
| POST | `/api/v1/auth/login` | Verify credentials, set the session cookie, return JSON or, for HTML, redirect to the requested page or the inventory page (`/`) |
| POST | `/api/v1/auth/logout` | Revoke the session; requires authentication and CSRF |
| GET | `/api/v1/auth/session` | Current account, role and expiry; any signed-in role |
| GET | `/api/v1/auth/csrf` | Issue a signed CSRF token for API clients |
| GET | `/account/profile` | Profile page: the account's character names with add and remove |
| GET | `/api/v1/me/characters` | The signed-in account's character names (future requester dropdown) |
| POST | `/api/v1/me/characters` | Add a character name; JSON or `303` for HTML |
| DELETE | `/api/v1/me/characters/:id` | Remove one of the account's own names; another account's ID is `404` |
| POST | `/api/v1/me/characters/:id/delete` | HTML form adapter for DELETE |
| GET | `/account/discord/link` | Start Discord account linking (browser navigation); `303` to Discord's consent screen |
| POST | `/account/discord/link` | Same start for API clients; returns `{"authorizeUrl": …}` |
| GET | `/account/discord/callback` | OAuth callback; `303 /account/profile?notice=…` on every outcome |
| GET | `/auth/discord` | Discord sign-in start; `303` to Discord, or `/login?notice=…` when unavailable |
| GET | `/auth/discord/callback` | Discord sign-in callback; sets a session or redirects to `/login?notice=…` |
| POST | `/account/discord/unlink` | HTML form adapter that unlinks Discord |
| GET | `/api/v1/me/discord` | The account's linked Discord identity (`linked`, name, date) |
| DELETE | `/api/v1/me/discord` | Unlink Discord from the signed-in account |
| GET | `/admin/settings` | Administrator settings screen |
| GET | `/api/v1/settings` | Redacted settings JSON; secrets never returned |
| PUT/POST | `/api/v1/settings` | Validate and persist settings; POST is the HTML form adapter for PUT |
| GET | `/admin/imports` | Import history and upload form |
| GET | `/admin/imports/:batchID` | Batch review page; read-only for terminal batches |
| GET | `/api/v1/imports` | Recent batches (`limit`, `beforeId`) |
| POST | `/api/v1/imports` | Multipart upload of `InventoryExport.txt`; JSON by default or `303` for HTML |
| GET | `/api/v1/imports/:batchID` | Batch status, counts, hash and failure reason |
| GET | `/api/v1/imports/:batchID/locations` | Selector generated from distinct normalized `LocationGroup` values |
| GET | `/api/v1/imports/:batchID/rows` | Paged staged rows (`locationGroup`, `selected`, `q`, `page`, `pageSize`) |
| GET | `/api/v1/imports/:batchID/row-ids` | All staged IDs, optionally for one `LocationGroup` |
| GET/PUT | `/api/v1/imports/:batchID/selection` | Read or fully replace the persisted selection |
| GET | `/api/v1/imports/:batchID/summary` | Unit-aware selected totals and spell preview |
| POST | `/api/v1/imports/:batchID/commit` | Atomically write the persisted selection to `GuildBankItems` |

Every response carries `X-Request-ID`; logs include the same ID. JSON errors
use `{"error":{"code","message","fields","request_id"}}`. Admin API requests
without a session return `401`; admin HTML requests redirect to `/login`.

## Development

```bash
make test        # unit and API tests, including migrations and security flows
make test-race   # race detector (requires CGO_ENABLED=1 and a C toolchain; CI does this)
make vet
make lint        # gofmt check + go vet
make build
docker build -t guildbank:dev .
docker compose config
```

The Phase 1 tests cover schema migration and rollback, setup atomicity, bcrypt,
session hashing/expiry/disabled accounts, login failure parity and lockout,
CSRF rejection, default-deny routes, secure cookie flags, secret redaction and
settings validation. The four-step flow was also exercised against the Compose
topology with a live MariaDB 11.4 instance.

Phase 3 adds the parser golden test for the 548-line sample, natural-sort and
`LocationGroup` normalization tests, commit-plan quantity/merge tests, and
handler tests for multipart CSRF, upload, selection and rendered import pages.
Phase 4 adds filter/availability/EQResource unit tests, repository predicate and
allowlist tests, public JSON/HTML contract tests, forbidden-field and XSS tests,
and the live Compose public list/detail/restart/outage/recovery checks. Phase 3
and Phase 4 workflows have been executed against Compose MariaDB 11.4; see
`Docs/ProjectStatus.md` §1.5.1 and `Docs/Phase4Validation.md`.

The full Items metadata test is opt-in because it requires the provisioned
144,712-row source. Set `GUILDBANK_INTEGRATION=1` and the
`GUILDBANK_TEST_MARIADB_HOST`, `_PORT`, `_DATABASE`, `_USERNAME` and `_PASSWORD`
values, then run `go test ./internal/integration -run ItemMetadataIntegration -v`.
Normal `go test ./...` skips it. The test is read-only.

The Phase 4 inventory benchmark is also opt-in and refuses to run unless the
database is the disposable `GuildBankPhase4Perf` schema. Provision that schema,
set `GUILDBANK_PHASE4_PERF=1` plus the same `GUILDBANK_TEST_MARIADB_*` values,
then run `go test ./internal/integration -run Phase4PublicInventoryPerformance -v`.
Set `GUILDBANK_PHASE4_PERF_ROWS=1000000` for the stress run; the default is
250,000 active rows plus 10% deleted noise. Results are recorded in
`Docs/Phase4Validation.md`.

Legacy item rows written before the QTY-01 change can be folded into one stack
with the owner-run maintenance command. Always rehearse with `-dry-run` first;
the command migrates and backfills as usual, prints the group/absorbed/unit
counts in the log, and exits without serving HTTP:

```sh
guildbank -consolidate-items -dry-run
guildbank -consolidate-items -consolidate-actor <operator>
```

The survivor is the lowest ID, quantities are summed, absorbed rows are soft
deleted with `ConsolidatedInto` and audit rows, and a second run is a no-op.
See `Docs/ItemQuantityModelImplementation.md`.

Layer boundaries are enforced by tests: `internal/api` and `internal/web`
fail the build if they import `internal/repository`.

The directory map and design decisions are documented in
[`Docs/ProjectStructure.md`](Docs/ProjectStructure.md).