# Aeternus Guild Bank

Self-hosted EverQuest guild inventory for Docker on Unraid. The authoritative
specification is [`Design.md`](Design.md) v1.6.1; delivery is tracked in
[`Docs/ProjectStatus.md`](Docs/ProjectStatus.md) and
[`Docs/ImplementationPlan.md`](Docs/ImplementationPlan.md).

**Current scope: Phase 0 skeleton only.** The process builds, starts, logs in
JSON, serves health endpoints and opens both databases. Authentication,
setup, imports, inventory, audit and UI behaviour are intentionally absent, as
are schema migrations.

## Requirements

- Go 1.27+ (module `github.com/Panazule/guildbank`)
- Docker 24+ with Compose v2 (optional, for containers)
- MariaDB 11.4+ (optional for Phase 0; the service runs degraded without it)

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

The SQLite parent directory is created automatically. On a fresh database
`/readyz` returns `503` because no administrator exists and MariaDB is not
configured; that is the specified pre-setup state. `/healthz` returns `200`
whenever the process is alive.

## Docker Compose (app + MariaDB)

```bash
docker compose up --build
curl http://localhost:8080/healthz
curl -i http://localhost:8080/readyz   # 503 until setup exists (Phase 2)
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
| — | `GUILDBANK_TRUSTED_PROXIES` | empty | Comma-separated IPs/CIDRs allowed to set forwarded headers; empty trusts none |
| — | `GUILDBANK_MARIADB_HOST` | empty | **Development bootstrap only**; Phase 3 reads Settings |
| — | `GUILDBANK_MARIADB_PORT` | `3306` | Bootstrap guild store port |
| — | `GUILDBANK_MARIADB_DATABASE` | `GuildBank` | Bootstrap guild store database |
| — | `GUILDBANK_MARIADB_USERNAME` | empty | Bootstrap guild store user |
| — | `GUILDBANK_MARIADB_PASSWORD` | empty | Bootstrap guild store password (never logged) |

`GUILDBANK_MARIADB_*` exists so the connection layer can be exercised before
the setup wizard stores credentials in SQLite; it is flagged as provisional
until E-11 is decided.

## Endpoints

| Method | Path | Behaviour |
|---|---|---|
| GET | `/healthz` | Always `200` while the process runs; JSON with version and uptime |
| GET | `/readyz` | `200` only when SQLite, setup state and MariaDB are all healthy; `503` otherwise with per-check reasons |

Every response carries `X-Request-ID`; logs include the same ID.

## Development

```bash
make test        # unit tests
make test-race   # race detector (requires CGO_ENABLED=1 and a C toolchain; CI does this)
make vet
make lint        # gofmt check + go vet
make build
docker build -t guildbank:dev .
docker compose config
```

Layer boundaries are enforced by tests: `internal/api` and `internal/web`
fail the build if they import `internal/repository`.

The directory map and design decisions are documented in
[`Docs/ProjectStructure.md`](Docs/ProjectStructure.md).