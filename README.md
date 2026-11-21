# Aeternus Guild Bank

Self-hosted EverQuest guild inventory for Docker on Unraid. The authoritative
specification is [`Design.md`](Design.md) v1.6.1; delivery is tracked in
[`Docs/ProjectStatus.md`](Docs/ProjectStatus.md) and
[`Docs/ImplementationPlan.md`](Docs/ImplementationPlan.md).

**Current scope: Phase 1 complete.** The process builds, starts, migrates SQLite,
serves the four-step setup wizard, creates a bcrypt-hashed administrator,
issues server-side sessions, enforces CSRF and default-deny admin routes,
rate-limits login and persists typed settings. Inventory, imports, audit,
user management and MariaDB business repositories remain later phases.

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
| — | `GUILDBANK_MARIADB_HOST` | empty | **Development bootstrap only**; Phase 3 reads Settings |
| — | `GUILDBANK_MARIADB_PORT` | `3306` | Bootstrap guild store port |
| — | `GUILDBANK_MARIADB_DATABASE` | `GuildBank` | Bootstrap guild store database |
| — | `GUILDBANK_MARIADB_USERNAME` | empty | Bootstrap guild store user |
| — | `GUILDBANK_MARIADB_PASSWORD` | empty | Bootstrap guild store password (never logged) |

`GUILDBANK_MARIADB_*` exists so the connection layer can be exercised before
the setup wizard stores credentials in SQLite. The wizard persists those
credentials in `Settings`, but the running pool still uses this environment
bootstrap until Phase 3 implements the Settings-backed pool and hot-swap
(PD-05 remains in force for the runtime connection during Phase 1).

## Endpoints

| Method | Path | Behaviour |
|---|---|---|
| GET | `/healthz` | Always `200` while the process runs; JSON with version and uptime |
| GET | `/readyz` | `200` only when SQLite, setup state and MariaDB are all healthy; `503` otherwise with per-check reasons |
| GET | `/setup` | Four-step wizard while no administrator exists; `410` afterwards |
| POST | `/setup/admin`, `/setup/guild`, `/setup/database/test` | Validate a wizard step; no persistence except the connection test's transient probe |
| POST | `/setup/complete` | Atomically create the administrator and settings, then start a session |
| GET | `/login` | Login page (redirects to setup while setup is pending) |
| POST | `/api/v1/auth/login` | Verify credentials, set the session cookie, return JSON or redirect for HTML |
| POST | `/api/v1/auth/logout` | Revoke the session; requires authentication and CSRF |
| GET | `/api/v1/auth/session` | Current administrator and expiry; admin-only |
| GET | `/api/v1/auth/csrf` | Issue a signed CSRF token for API clients |
| GET | `/admin/settings` | Administrator settings screen |
| GET | `/api/v1/settings` | Redacted settings JSON; secrets never returned |
| PUT/POST | `/api/v1/settings` | Validate and persist settings; POST is the HTML form adapter for PUT |

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

Layer boundaries are enforced by tests: `internal/api` and `internal/web`
fail the build if they import `internal/repository`.

The directory map and design decisions are documented in
[`Docs/ProjectStructure.md`](Docs/ProjectStructure.md).