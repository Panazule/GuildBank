# Production Deployment Design — Unraid + Docker + GHCR

| Field | Value |
|---|---|
| Document ID | DEP-001 |
| Status | **Design only — nothing in this document is implemented.** No code, Dockerfile, workflow, template or schema change accompanies it. |
| Revision | 1 (2026-11-27) |
| Scope | Production deployment to Unraid, public Docker distribution, first-run experience, guild-database initialization, EQ item seeding, configuration, upgrades, backups |
| Builds on | `Dockerfile`, `docker-compose.yml`, `.github/workflows/ci.yml`, `internal/platform/config/config.go`, `internal/platform/app/app.go`, `internal/repository/mariadb/*`, `internal/repository/sqlite/*`, `Docs/DatabaseDesign.md`, `Docs/ItemsDataProfile.md`, `README.md` |
| Non-goals | Implementing the container, the release workflow, the Unraid template, the item seeder, PUID/PGID support, TLS to MariaDB, or any schema change. Multi-tenant hosting, Kubernetes, Helm, in-app self-update. |

This document answers the deployment questions in order: what exists today and what is
missing (§1), the production architecture (§2), the image and release pipeline (§3), the
first-run workflow (§4), schema initialization (§5), MariaDB requirements and privileges (§6),
item data (§7), the configuration model (§8), the Unraid template (§9), upgrades (§10),
backups (§11), operations (§12), the implementation backlog (§13), the assumptions behind it
(§14), and the decisions the owner must make (§15).

---

## 0. Summary of recommendations

| # | Question | Recommendation | Section |
|---|---|---|---|
| 1 | Deployment shape | **One container** (`guildbank`), no sidecar, no bundled MariaDB, no init container. `/config` is the only application volume. | §2 |
| 2 | Reverse proxy | Existing proxy terminates TLS and forwards to `http://<unraid-host>:8080`; the container never terminates TLS. Run with `GUILDBANK_SECURE_COOKIES=true` and an explicit `GUILDBANK_TRUSTED_PROXIES`. | §2, §8 |
| 3 | Image registry | `ghcr.io/panazule/guildbank`, public, multi-arch `linux/amd64` + `linux/arm64`, built by a tag-triggered GitHub Actions workflow, signed and attested. | §3 |
| 4 | Tag policy | Immutable `X.Y.Z`, moving `X.Y`, `X`, `latest`; plus `edge` from `main` and `sha-<short>`. Unraid pins `X.Y` (patches auto-update, minor is deliberate). | §3, §10 |
| 5 | Source of the MariaDB connection | **Container environment is the single source of truth.** The wizard pre-fills from the environment, tests the connection, and refuses to complete on a mismatch. Hot adoption from Settings (PD-18) stays deferred. | §8, §15 OD-D1 |
| 6 | Does GuildBank create the database? | **No.** The operator creates `GuildBank` with the right charset/collation and grants. The app creates *schema objects only*, inside an existing database. | §5, §6 |
| 7 | Schema initialization | Keep migrate-on-start: idempotent, advisory-locked (`GET_LOCK`), forward-only, version-guarded, fatal after `GUILDBANK_MARIADB_INIT_ATTEMPTS`. Add a read-only preflight/diagnostic mode. Never auto-adopt an unknown schema. | §5 |
| 8 | Item data | **Never implicit at startup.** Ship a slim, versioned `items` seed (id/name/classes/races) applied only by an explicit one-shot `-seed-items` run, gated on "table absent or seed older". Skip when a populated `items` table already exists. | §7 |
| 9 | Upgrade | Pull new image → stop → **backup** → start → migrations before readiness → verify `/readyz` + smoke checklist. Downgrade is restore-from-backup, not schema rollback. | §10 |
| 10 | Backup | Two targets: `/config/guildbank.db` (WAL-checkpointed) and the MariaDB guild schema dumped **excluding `items`**. Pre-upgrade snapshot mandatory; rehearsed restore is an acceptance gate. | §11 |
| 11 | Unraid ownership | Add `PUID`/`PGID` with a root entrypoint that chowns `/config` then drops privileges; otherwise a non-root container cannot write a freshly created appdata folder. | §3, §9, OD-D3 |
| 12 | Owner decisions | 16 decisions (`OD-D1`…`OD-D16`), of which 4 block officer testing: `OD-D1`, `OD-D2`, `OD-D3`, `OD-D7`. | §15 |


---

## 1. Current state, and the gaps this design must close

Everything below is measured from the repository as it stands today.

| Fact | Source |
|---|---|
| One static, CGO-free Go binary; templates and static assets are `go:embed`-ed, so the runtime needs no files besides the binary. | `internal/web/embed.go`, `Dockerfile` |
| Container already runs as UID/GID `10001`, declares `VOLUME ["/config"]`, exposes `8080`, and has a liveness `HEALTHCHECK` on `/healthz`. | `Dockerfile` |
| Two data stores: SQLite at `GUILDBANK_SQLITE_PATH` (default `/config/guildbank.db`) for accounts, settings and sessions; MariaDB for guild inventory, imports, audit and the read-only item reference. | `internal/repository/sqlite/store.go`, `internal/repository/mariadb/store.go` |
| SQLite migrates on startup to `PRAGMA user_version` 5 (`0001`–`0005`). | `internal/platform/app/app.go`, `internal/repository/sqlite/migrate.go` |
| MariaDB migrations `0001`–`0008` are embedded, applied under `GET_LOCK('guildbank:migrate:<sha of db name>')`, recorded in `schema_migrations`, and rejected if an embedded migration is older than the newest applied one. | `internal/repository/mariadb/migrate.go` |
| A supervisor retries connectivity forever (1 s → 30 s backoff, 5 s probe) and treats migration/schema failure as fatal after `InitAttempts` (default 3). Readiness is published only after `Initialize` succeeds. | `internal/repository/mariadb/supervisor.go`, `internal/platform/app/app.go` |
| `Initialize` also runs startup repairs: interrupted-import recovery, `LocationGroup`/`LocationSort` backfill (500 rows), spell metadata backfill (500 rows). | `internal/repository/mariadb/supervisor.go` |
| `/readyz` is `503` while setup is pending, and also when MariaDB is `not_configured`, `initializing` or `error`. | `internal/service/health.go` |
| Setup is gated on the SQLite admin row, not on file existence: no admin → `/setup`; one admin → `410 Gone`. | `internal/repository/sqlite/store.go`, `README.md` |
| Setup completion is one SQLite transaction: administrator + all wizard settings + `SetupComplete=1`; the MariaDB connection is tested read-only beforehand. | `internal/service/setup.go`, `internal/repository/sqlite/setup.go` |
| MariaDB connection settings are **environment bootstrap only**; the wizard persists them to `Settings` but the live pool keeps using the environment (PD-18 / E-11 still in force). | `internal/platform/config/config.go:161-186`, `internal/platform/app/app.go:67-84`, `README.md:108-112` |
| Pool: 20 max open / 10 idle, `ParseTime=true`, `Loc=UTC`, collation `utf8mb4_unicode_ci`, 5 s dial timeout, 120 s read/write timeouts. No TLS parameter is set. | `internal/repository/mariadb/store.go:100-130` |
| `items` is externally owned and read-only; a unit test fails the build if `items.go` contains any write verb. | `internal/repository/mariadb/items.go`, `items_readonly_test.go` |
| The item dataset is 144,712 rows / 315 columns in a 218,230,768-byte dump at `Docs/Samples/EQItemList-items.sql`, which is `.gitignore`d and excluded from the build context (`.dockerignore` excludes `Docs`). | `Docs/ItemsDataProfile.md`, `.gitignore:21`, `.dockerignore:16` |
| CI runs format, vet, race tests, build and a Docker build, but **no image is published** and there is no release workflow. | `.github/workflows/ci.yml` |
| `docker-compose.yml` is a development topology with a bundled MariaDB and disposable credentials. | `docker-compose.yml` |

### 1.1 Gaps that make "deploy as-is" unsafe

| ID | Gap | Consequence today | Closed by |
|---|---|---|---|
| G1 | The wizard stores MariaDB credentials the process never uses. | The operator finishes setup, restarts, and the app connects with different credentials — or with none, in which case **no guild store exists** and every inventory page is unavailable. | §8.1, OD-D1 |
| G2 | `/config` is created by Docker as `root:root`; the container is UID 10001. | On a fresh Unraid install SQLite open fails with a permission error and the container crash-loops. | §3.4, OD-D3 |
| G3 | No image is published; no immutable tags. | Unraid cannot pull, cannot detect updates, has no rollback target. | §3 |
| G4 | `items` must be pre-loaded out-of-band, yet the requirement includes importing baseline item data. | Every officer-tester hand-runs MySQL against a 218 MB dump, or the app violates SR-13. | §7 |
| G5 | No preflight diagnostics. A wrong collation, a case-sensitive `Items` table, or missing grants surface as a fatal supervisor exit with a raw driver error. | Slow, opaque first-run support burden. | §5.4 |
| G6 | Migrations are forward-only and unguarded mid-file. | A downgrade cannot be expressed; a failed migration needs manual repair. Deployment docs must say so before anyone needs it at 02:00. | §10.4 |
| G7 | No stated backup/restore procedure for the SQLite + MariaDB pair. | Two-store inconsistency after a restore (accounts without inventory, or the reverse). | §11 |
| G8 | No separation between the runtime DML principal and the migration DDL principal. | SR-13's read-only `items` rule stays a code convention in production too. | §6.4, OD-D2 |
| G9 | `items`-dependent features fail closed but silently from the browser's point of view when the reference table is empty. | Imports commit with "metadata missing" on every row and blank class/race strings. | §7.6 |

---

## 2. Production deployment architecture

### 2.1 Topology

```text
                     Internet / guild LAN
                              |
                    (existing reverse proxy)
                 TLS termination, HSTS, host routing
                 e.g. https://guildbank.example.com
                              |
                              |  http://<unraid-host>:8080
                              v
        +-----------------------------------------------+
        |  Unraid host                                  |
        |                                               |
        |  +-----------------------------------------+  |
        |  | container: guildbank                    |  |
        |  | image: ghcr.io/panazule/guildbank:X.Y   |  |
        |  | uid: PUID/PGID (default 10001)          |  |
        |  | port: 8080 (bridge, published)          |  |
        |  | read-only rootfs + tmpfs /tmp           |  |
        |  |                                         |  |
        |  |  /config  ------------------------+     |  |
        |  +-----------------------------------|-----+  |
        |                                      |        |
        |  appdata/guildbank/guildbank.db  <---+        |
        |  appdata/guildbank/guildbank.db-wal           |
        |  appdata/guildbank/guildbank.db-shm           |
        |                                               |
        |  +-----------------------------------------+  |
        |  | container: MariaDB (pre-existing)       |  |
        |  | database: GuildBank                     |  |
        |  | user: guildbank_app                     |  |
        |  | tables: items (static), ImportBatch,    |  |
        |  |   ImportList, GuildBankItems, Audit,    |  |
        |  |   InventoryMovement, InventoryRequest,  |  |
        |  |   schema_migrations                     |  |
        |  +-----------------------------------------+  |
        +-----------------------------------------------+
```

Nothing new is introduced: the deployment is the existing binary, the existing
MariaDB container, the existing proxy, and one named appdata folder.

### 2.2 Component responsibilities

| Component | Owner | Responsibility |
|---|---|---|
| Reverse proxy | Operator (existing) | TLS, hostname, HSTS, optional basic-auth in front of `/setup` during the exposure window |
| `guildbank` container | This project | HTTP, sessions, SQLite store, guild-store migrations, all business logic |
| MariaDB container | Operator (existing) | Server availability, `GuildBank` database existence, credentials, grants, its own backups |
| `/config` volume | Operator | Persistence of accounts/settings/sessions; must survive image replacement |
| `items` table | Operator + seed job (§7) | Static reference metadata; read-only at runtime |

### 2.3 Trust boundaries and network rules

1. **Browser → proxy**: HTTPS only. The app must never be published directly to the internet; it speaks plain HTTP.
2. **Proxy → container**: HTTP on the published port. Because the proxy sets `X-Forwarded-*`, `GUILDBANK_TRUSTED_PROXIES` must list the proxy address (or the Docker bridge gateway); otherwise Gin ignores the headers and audit/rate-limiting record the proxy IP.
3. **Container → MariaDB**: over a shared Docker network by container name **only if both containers are on that network**. Unraid's default `bridge` network gives the app the host IP (`192.168.x.x`), *not* `127.0.0.1` and not the MariaDB container name. This is the most common Unraid misconfiguration; the template description must say so (§9).
4. **Container → DNS/HTTP out**: only the Discord endpoints (OAuth token and user/guild lookups). No other egress is required.

### 2.4 Runtime unit list

| Unit | Count | Notes |
|---|---|---|
| `guildbank` service container | 1 | Always running. |
| `guildbank-seed` one-shot container | 0 or 1 | Same image, different command (`-seed-items`), run on install and after a deliberate item refresh (§7). Never part of normal startup. |
| Migration job | 0 | Migrations run inside the app at startup under `GET_LOCK`; a separate job is unnecessary at this scale. |

### 2.5 Availability expectations

The design is single-instance, deliberately: SQLite is a single-writer store, the
MariaDB migration lock is per-database, and sessions live in SQLite. Two replicas
sharing one `/config` would corrupt the SQLite store. Horizontal scaling is out of
scope and must not be attempted by adding replicas.

### 2.6 What the operator must already have

| Prerequisite | Why |
|---|---|
| Unraid with Docker and an appdata share | Container host |
| A reverse proxy with a certificate for the chosen hostname | Secure cookies, Discord OAuth redirect |
| MariaDB 11.4+ reachable from the container | Guild store |
| A Discord application (client ID/secret) with a redirect URI registered | Optional login; required for Discord sign-in |
| ~1 GB free for the image, ~50 MB for `/config`, plus DB growth | Capacity |

---

## 3. Docker image and release design

### 3.1 Decision: extend the existing image, publish it, do not restructure it

The current multi-stage build is already correct for this workload: `golang:1.25-alpine`
builder → static `CGO_ENABLED=0` binary → `alpine` runtime with `ca-certificates`, `tzdata`,
`wget`, UID/GID 10001, `/config` volume, `8080`, and a `/healthz` healthcheck. The design
changes are additive:

1. publish the image from CI (today nothing is pushed);
2. let the entrypoint tolerate Unraid's root-owned appdata folder (`PUID`/`PGID`);
3. add an explicit seed mode to the same binary;
4. harden (read-only rootfs, `--cap-drop ALL`, OCI labels, SBOM, provenance).

No distroless/scratch migration is proposed: `wget` is required by the healthcheck and
`tzdata` by log timestamps, and the maintenance cost of a scratch image with a bundled
healthcheck binary is not justified here.

### 3.2 Image contract

| Property | Value | Notes |
|---|---|---|
| Registry | `ghcr.io/panazule/guildbank` | Namespace is owner-configurable (`OD-D4`) |
| Platforms | `linux/amd64`, `linux/arm64` | Unraid is amd64; arm64 covers dev/test on Apple silicon |
| Base | `alpine:3.22`, pinned by digest in the release build | Same as today |
| Size target | ≤ 45 MB compressed | `Docs/Samples` is excluded from the context today and must stay excluded |
| User | `10001:10001`, overridable at start via `PUID`/`PGID` | §3.4 |
| Writable paths | `/config` (volume), `/tmp` (tmpfs) | Root filesystem read-only when supported |
| Ports | `8080/tcp` | Proxy maps 443 → 8080 |
| Signals | `SIGTERM` → graceful shutdown (`run.go` already handles it) | Give `stop_grace_period: 20s` |
| Healthcheck | `GET /healthz` every 30 s, 3 s timeout, 3 retries, `start-period` 20 s | `/healthz` is liveness only; `/readyz` is for the proxy |
| Contents | `/app/guildbank`, CA bundle, tzdata, busybox only | No source, no docs, no sample SQL |
| Labels | `org.opencontainers.image.{title,description,url,source,version,revision,created,licenses}`, `…base.digest` | GHCR metadata and `docker inspect` triage |

### 3.3 Filesystem and volume layout

| Path | Type | Contents | Backed up |
|---|---|---|---|
| `/config` | volume | `guildbank.db`, `-wal`, `-shm` | Yes (§11) |
| `/tmp` | tmpfs, 64 MB | transient | No |
| `/app` | image, read-only | binary | No |
| `/data` | **not used** | Any older note referencing `/data` is superseded; the SQLite default is `/config/guildbank.db` | — |

`GUILDBANK_SQLITE_PATH` may point anywhere, but the design treats
`/config/guildbank.db` as the only supported production value so that one volume
covers all state.

### 3.4 Entrypoint and ownership (`PUID`/`PGID`)

Unraid creates an appdata folder as `root:root` before first start. A container that
starts as UID 10001 cannot write there, so the current image crash-loops on a fresh
install (G2). The design adds a small entrypoint script to the runtime stage:

```text
if [ "$(id -u)" = "0" ]; then
  PUID=${PUID:-10001}; PGID=${PGID:-10001}
  chown -R "$PUID:$PGID" /config        # one directory, one file: fast and bounded
  exec su-exec "$PUID:$PGID" /app/guildbank "$@"
fi
exec /app/guildbank "$@"
```

| Rule | Rationale |
|---|---|
| Privilege drop is unconditional when started as root | The process never serves traffic as root |
| `chown` is limited to `/config` | No writes to arbitrary host paths |
| `PUID=99`, `PGID=100` on Unraid | Matches `nobody:users`, so appdata stays browsable from the host |
| Started with an explicit `--user` | Entrypoint skips `chown` and `su-exec`; host ownership must then be correct, which is documented |
| A failed `chown` is fatal with an actionable message | Better than a later, opaque SQLite "permission denied" |

This changes the current `USER 10001:10001` *default*: the image leaves `USER` unset so
root can fix ownership, and the entrypoint guarantees the drop. `docker run --user 10001`
remains supported and equally safe.

### 3.5 Build pipeline

| Stage | Today | Proposed |
|---|---|---|
| Context | `.dockerignore` excludes `.git`, `data`, `Docs`, `bin`, coverage/test artifacts | Add `.github`, `tmp`, root `*.md` |
| Compile | `go build -trimpath -ldflags "-s -w"` | Add `-buildvcs=false`; inject version/commit/date via `-X` |
| Multi-arch | Single-arch local build | `docker/build-push-action`, `platforms: linux/amd64,linux/arm64`; QEMU for the arm64 leg only |
| Cache | none | `cache-from/to: type=gha` |
| Attestations | none | `provenance: mode=max`, `sbom: true` |
| Signing | none | `cosign sign` keyless (optional; `OD-D12`) |
| Verification | `docker build` only | `docker run --rm image --version` plus a container smoke test of `/healthz` |

### 3.6 Release workflow (new `.github/workflows/release.yml`)

Triggered by `push: tags: ['v*.*.*']`, plus `workflow_dispatch` to re-run an existing
tag. It stays separate from `ci.yml`, which keeps running on pull requests and `main`.

| Job | Steps |
|---|---|
| `verify` | `go vet`, `go test ./... -race`, embedded-asset check — reuse the existing CI definitions instead of duplicating them |
| `build` (matrix, one job per platform) | Build, push by digest |
| `merge` | `docker buildx imagetools create` to publish the tag set; record the digest in the release notes |
| `attest` | Provenance, SBOM and signature on the index digest |
| `release` | GitHub Release: changelog, image digest, upgrade notes, link to the backup procedure (§11) |

Permissions: `contents: write`, `packages: write`, `id-token: write` (keyless signing),
`attestations: write`. The workflow holds no deployment secrets.

### 3.7 Tagging scheme

| Tag | Moves? | Source | Intended use |
|---|---|---|---|
| `X.Y.Z` | Never | Release tag | Rollback target, reproducibility |
| `X.Y` | On patch releases | Release tag | **Unraid default** — patches auto-update, minors are deliberate |
| `X` | On minor releases | Release tag | "Always newest minor" |
| `latest` | On stable releases only | Release tag | Manual `docker pull` users |
| `edge` | Every green `main` build | `main` | Testers only; never the template default |
| `sha-<7>` | Never | Any build | Bisecting a regression |

A pre-release (`v1.2.0-rc.1`) publishes only `1.2.0-rc.1` and `sha-…`; it must not
move `latest`, `X` or `X.Y`.

### 3.8 Supply chain and reproducibility

| Control | Implementation |
|---|---|
| Base-image pinning | `FROM alpine:3.22@sha256:…` in the release build; Dependabot or a scheduled job refreshes the digest |
| Dependency integrity | `go.sum` verification is on by default; `GOFLAGS=-mod=readonly` in the builder |
| Provenance | `SLSA` build provenance attached by `build-push-action` |
| Vulnerability scan | `govulncheck` in CI; `trivy image --severity HIGH,CRITICAL --ignore-unfixed` on the release index |
| Signature verification | Documented `cosign verify` command in the release notes |
| Deterministic binary | `-trimpath`, `-buildvcs=false`, no `CGO` |

### 3.9 Acceptance gates for the Docker deliverable

1. `docker run --rm ghcr.io/panazule/guildbank:X.Y.Z --version` prints the release version.
2. `docker run` with an empty, `root:root` directory mounted at `/config` and `PUID=99` starts and serves `/healthz` within 20 s.
3. The same with `--read-only --tmpfs /tmp` behaves identically.
4. `docker run --user 10001` against a correctly owned folder behaves identically.
5. Two-arch build completes; both digests carry provenance.
6. Image size and layer count do not regress by more than 10% release over release.

---

## 4. First-run experience

### 4.1 Principles

1. **Fail fast on misconfiguration, never half-start.** Invalid environment → exit code 2 with a message naming the variable. Unwritable `/config` → fatal at bind time.
2. **The container is usable before MariaDB is.** The HTTP server starts, `/healthz` is green, and `/setup` explains the guild-store state instead of returning a 500.
3. **Setup is idempotent and one-way.** A completed install cannot re-enter the wizard; a failed attempt changes nothing.
4. **No secrets in logs.** Passwords and secrets are never echoed, including in MariaDB driver errors (SR-10).
5. **Everything the wizard asks for must also be settable by the operator in advance**, so an unattended/reproducible install is possible.

### 4.2 State machine

```text
[container start]
      |
      v
(1) env validation -------- invalid ---------> exit 2, message names the variable
      |
      v
(2) /config writable? ------ no --------------> exit 2, chown/PUID guidance
      |
      v
(3) SQLite open + migrate (0001..0005, user_version=5)
      |
      +--> fresh DB ---------------------------+
      |                                        |
      v                                        v
(4) admin row exists? -- no --> SETUP PENDING: /setup served, /readyz 503 (setup pending)
      |                                        |
      | yes                                    | wizard completes (single tx)
      v                                        v
(5) SETUP COMPLETE: /setup -> 410      SETUP COMPLETE
      |
      v
(6) MariaDB supervisor (background, retrying)
      configured? -- no --> status not_configured; /readyz 503 "guild store not configured"
      |
      yes
      v
(7) connect -> migrate (GET_LOCK) -> init repairs -> READY, /readyz 200
      |
      x--> connectivity lost --> backoff retry, /readyz 503, recovers automatically
      |
      x--> migration/schema failure x InitAttempts --> FATAL: process exits 1 (restart-or-fix)
```

Steps 3–5 are synchronous and fast (seconds). Step 6–7 run concurrently so the wizard
is reachable while MariaDB is still being fixed — which matters, because the wizard is
where the operator learns the MariaDB credentials are wrong.

### 4.3 Phase detail

| Phase | Trigger | Observable result | Blocking? |
|---|---|---|---|
| P1 Environment | process start | Startup log line: version, commit, listen address, SQLite path, MariaDB host/db **without password**, `secure_cookies`, trusted proxies | Yes — exit 2 |
| P2 Data directory | after P1 | `sqlite: opened` + applied migrations list | Yes — exit 2 |
| P3 SQLite schema | P2 | `user_version` 5; `Settings` defaults seeded; `SetupComplete=0` | Yes |
| P4 Setup gate | P3 | `GET /` → 303 `/setup`; `/readyz` 503 `setup=pending`; `/healthz` 200 | No |
| P5 MariaDB connect | P4 (background) | `mariadb: connected` or retry log every 1→30 s | No — retries |
| P6 MariaDB schema | P5 | `mariadb: migrations applied: 0001..0008` (first run) or `up to date` | No — fatal after N |
| P7 Startup repairs | P6 | `mariadb: recovered N interrupted imports`, backfill counters | No |
| P8 Ready | P7 + admin exists | `/readyz` 200 with all checks `ok` | — |

### 4.4 The setup wizard

Reached at `/setup`; four steps, all server-rendered, no JavaScript requirement beyond
progressive enhancement.

| Step | Fields | Validation | Writes |
|---|---|---|---|
| 1. Administrator | username (3–64), password, confirm | Username uniqueness against SQLite; password length/complexity per the auth policy; bcrypt cost ≥ 12 | Nothing (staged in session) |
| 2. Guild | guild name, time zone, open-day count, bazaar-day count, EQResource URL, public-inventory visibility | Ranges and URL shape; sane defaults pre-filled | Nothing |
| 3. Database | host, port, database, username, password | **Pre-filled from the container environment and read-only when the environment is set** (`OD-D1`); "Test connection" performs a 5 s dial + `Ping` and reports a categorised failure (DNS / refused / auth / unknown database / TLS) with no raw driver text | Nothing — `mariadb.Test` only |
| 4. Review & finish | summary of steps 1–3 + a checkbox acknowledging "migrations will run on next start" | Re-validates everything, re-tests the connection | Single SQLite transaction (§4.5) |

Design requirement on step 3 (changes today's behaviour): the environment is the source
of truth, so a mismatch between the typed values and `GUILDBANK_MARIADB_*` **blocks
completion** with an explicit message, rather than silently storing unusable settings
(G1).

### 4.5 Completion transaction

Order of operations at "Finish", all before any redirect:

1. Re-run validation for all steps (server-side, from the session, not the form).
2. `mariadb.Test` with the effective configuration (5 s dial, 120 s read timeout). Failure → stay on step 4 with a categorised error; **no partial writes**.
3. Begin SQLite transaction:
   - `INSERT INTO Users` (administrator, `IsAdmin=1`, `Disabled=0`, bcrypt hash, UTC timestamps);
   - `UPSERT` every wizard setting into `Settings`;
   - `SetupComplete = 1`.
4. Commit, then create the session cookie and `303` to `/`.
5. Log one structured line: setup completed, admin username, guild name, guild-store target host/db — never the password.

Invariants:

- A crash between 3 and 4 rolls the whole thing back; the wizard is still available.
- A crash after 4 is harmless: the admin exists, so the wizard is closed and login works.
- The wizard never writes to MariaDB. Schema creation is exclusively the startup migrator's job, which keeps DDL in one place and under the lock.
- `SetupComplete` is advisory UI state; the gate is the presence of an enabled admin row (existing behaviour, preserved).

### 4.6 The exposure window

Between first start and setup completion, `/setup` is reachable by anyone who can reach
the port, including a bot on a misconfigured forward. Mitigations, in order of preference:

| Mitigation | Detail |
|---|---|
| Bind to LAN only until complete | Do not publish the proxy route until setup is done |
| Proxy-level basic auth on `/setup` | Two lines in the existing proxy config |
| Complete setup immediately | The window should be minutes, not days |
| `SETUP_TOKEN` gate (proposed) | Optional env var; when set, `/setup` requires `?token=` or a cookie set once. Cheap to implement, and the design recommends it (`OD-D7`) |

The design also requires the readiness/liveness split to be honest: `/readyz` must be
`503` while setup is pending so an uptime monitor does not report a half-installed
system as healthy — which is already the behaviour.

### 4.7 First-run failure matrix

| Symptom | Cause | Diagnosis | Fix |
|---|---|---|---|
| Container exits 2 immediately | Missing/invalid env (e.g. `GUILDBANK_MARIADB_PORT=abc`, empty username with host set) | Log names the variable | Correct the env, restart |
| Exits 2 with "permission denied" on `/config` | Root-owned appdata, no `PUID` | `ls -ln` on the host folder | Set `PUID`/`PGID`; or `chown -R 99:100` |
| `/healthz` 200 but `/readyz` 503 `setup=pending` | Wizard not completed | Expected | Complete setup |
| `/readyz` 503 `guild store not configured` | `GUILDBANK_MARIADB_HOST` empty | `/readyz` body | Set the env vars, restart |
| `/readyz` 503 `initializing`, logs show `dial tcp … connection refused` | Wrong host/port for the Unraid topology | `docker exec guildbank getent hosts <name>` | Use the host IP, or put both containers on one network |
| Exits 1 after 3 attempts, `Access denied for user` | Wrong credentials or no grant from the container's source IP | MariaDB error log | Grant `'guildbank_app'@'192.168.%'` or `'%'` |
| Exits 1, `Unknown database 'GuildBank'` | Database not created | — | Create it (§6.2) |
| Exits 1, `Table 'GuildBank.items' doesn't exist` | Item seed never run | `SHOW TABLES` | Run the seed (§7) |
| Exits 1, `Duplicate key name` / `already exists` during migration | Migration partially applied earlier, or `schema_migrations` lost | Compare `schema_migrations` with the embedded list | Repair manually (§5.6) |
| Exits 1, collation error | Database created with the wrong collation | `SHOW CREATE DATABASE` | Recreate with `utf8mb4_unicode_ci` |
| Setup page 500 | SQLite write failure (disk full / read-only volume) | Host `df`, container logs | Fix the volume |

### 4.8 Acceptance tests for first run

| # | Scenario | Pass condition |
|---|---|---|
| FR-1 | Fresh install, MariaDB up, seed run | Wizard completes; `/readyz` 200 within 30 s of finishing; dashboard lists inventory |
| FR-2 | Fresh install, MariaDB **down** | Wizard still reachable; `/readyz` 503 `initializing`; starting MariaDB later turns `ready` green with no restart |
| FR-3 | Wizard completed with wrong DB credentials | Finish is blocked with a categorised error; `guildbank.db` contains no admin |
| FR-4 | Existing `/config` with an admin | Wizard is 410; login page served |
| FR-5 | Existing `/config` copied to a second container | Same accounts, same settings; wizard stays closed |
| FR-6 | Interrupted migration | Container exits non-zero with a repair hint; no silent half-schema (documented, not hidden) |

---

## 5. Guild database initialization and migration

### 5.1 Ownership split

| Object | Created by | Altered by app | Notes |
|---|---|---|---|
| MariaDB server, users, grants, `GuildBank` database | **Operator** | Never | The app must not need `CREATE DATABASE` or `CREATE USER` |
| `schema_migrations` | App, every startup (`CREATE TABLE IF NOT EXISTS`) | App inserts rows | The only bookkeeping table |
| `ImportBatch`, `ImportList`, `GuildBankItems`, `Audit`, `InventoryMovement`, `InventoryRequest` and their indexes/constraints | App migrations `0001`–`0008` | App migrations only | Application-owned schema |
| `items` | Item seed (§7) | **Never** — read-only, enforced by test and by grants (`OD-D2`) | Static reference data |

### 5.2 Execution semantics (as built — the design does not change them)

| Property | Behaviour |
|---|---|
| Trigger | Every process start, after a successful connect, inside the supervisor's `Initialize` |
| Ordering | Lexicographic over the embedded filenames, sorted before comparison |
| Bookkeeping | `schema_migrations(Version VARCHAR(32) PK, AppliedAt DATETIME(6))`; `Version` is the filename without `.sql` |
| Concurrency | `GET_LOCK('guildbank:migrate:<first 32 chars of SHA2(DATABASE())>', 30)` on a dedicated connection, released in a defer; a timeout returns `ErrUnavailable`, which the supervisor retries |
| Atomicity | **None.** Statements run one at a time on that connection, no transaction; the version is recorded only after all statements succeed. MariaDB DDL implicitly commits, so a mid-file failure leaves a partial schema |
| Out-of-order protection | A pending embedded version older than the newest applied version is a hard error (protects against replaying an old branch's migration) |
| Filename limit | Versions longer than 32 characters are rejected **before** any statement runs (error 1406 → 1060 trap, already learned once) |
| Statement splitting | `--` line comments are stripped, then the file is split on `;`. Consequence: **no `;` and no `--` inside string literals or identifiers in migration files** |
| Idempotency | All `CREATE TABLE` use `IF NOT EXISTS`; `0005` uses `DROP CONSTRAINT IF EXISTS`; an applied version never re-runs |
| Failure handling | The error returns to the supervisor; a schema failure is fatal after `InitAttempts` (default 3) and the process exits 1 |
| Empty-database case | Identical to any other case: `0001`–`0008` run in order in a few seconds |

### 5.3 First-run sequence against an empty database

```text
supervisor: ping ok
  -> Migrate
       CREATE TABLE IF NOT EXISTS schema_migrations
       GET_LOCK(...)                             [30 s wait budget]
       applied = {} (empty)
       0001_guild_store            -> ImportBatch, ImportList, GuildBankItems, Audit
       0002_public_inventory_indexes
       0003_admin_inventory
       0004_import_ignored_rows
       0005_item_quantity_model    -> ALTER ... DROP CONSTRAINT IF EXISTS
       0006_inventory_movement
       0007_movement_related_movement
       0008_inventory_request
       RELEASE_LOCK
  -> Initialize
       recover interrupted imports (none)
       backfill LocationGroup/LocationSort (bounded batches)
       backfill spell metadata from items (0 rows when items is empty)
  -> markReady
```

Expected first-run duration is well under 5 seconds. If it is longer, something is wrong:
lock contention, an `ALTER` over a huge pre-existing table, or a slow array. The
supervisor's 120 s read timeout is the per-statement ceiling.

### 5.4 Preflight diagnostics (new, read-only)

Today a misconfiguration surfaces as a fatal exit with a driver string (G5). The design
adds a read-only preflight executed after connect and before `Migrate`:

| Check | Query | Failure message |
|---|---|---|
| Server version | `SELECT VERSION()` | Warn below 10.6; fail below 10.2 (generated column in `0008`) |
| Database collation | `information_schema.SCHEMATA` for `DATABASE()` | Require `utf8mb4` / `utf8mb4_unicode_ci`; warn if different |
| Case sensitivity | `SELECT @@lower_case_table_names` | If `0` and an `Items`/`ITEMS` table exists, fail with "rename to `items`" |
| Object inventory | `information_schema.TABLES` for the six app tables | App tables present but `schema_migrations` empty → **refuse to run**: "existing schema not managed by this app" |
| Privileges | `SHOW GRANTS FOR CURRENT_USER()` | Warn when `CREATE`/`ALTER`/`INDEX` are absent (migrations will fail) |
| `items` presence | `information_schema.TABLES` for `items` | Warn (not fail): "item metadata missing; run the seed" (§7) |
| Time zone | `SELECT @@system_time_zone, @@global.time_zone` | Informational; the driver uses `loc=UTC` |

The same report is exposed on `/readyz` as a `guild_store` sub-object (`version`,
`database`, `collation`, `migrations_applied`, `migrations_pending`, `items_rows`), so
support can diagnose an install with one `curl`.

### 5.5 Guard rails the design adds

| Guard | Reason |
|---|---|
| Refuse to touch a non-empty schema with no `schema_migrations` | Prevents `CREATE TABLE IF NOT EXISTS` from "adopting" an unrelated schema and then failing on the first `ALTER` |
| `-migrate-only` mode | connect → preflight → migrate → exit; lets the operator apply schema in a maintenance window and inspect the result without starting the server |
| `-check-db` mode | connect → preflight → print the report → exit 0/1; used by the troubleshooting docs |
| Migration checksum (future, optional) | `schema_migrations.Checksum` would detect an edited migration; today the filename is the only identity. Cheaper before the first public release than after |
| Documented failure classes | Connectivity retries forever, schema failures exit. Operators must know which is "wait" and which is "fix" |

### 5.6 Recovery from a partial migration

There is no automatic repair, by design — silently guessing is worse than stopping. The
documented procedure (mirroring the recovery already executed in
`Docs/AdminInventoryIssuedRowsImplementationPlan.md` §5.5):

1. Read the failing statement and error (`error 1060` duplicate column, `1061` duplicate key, `1826` duplicate constraint).
2. Read `SELECT Version, AppliedAt FROM schema_migrations ORDER BY Version` — the failing version is **absent**.
3. Inspect the actual object with `SHOW CREATE TABLE`.
4. Either (a) finish the migration by hand and `INSERT` the version row, or (b) undo the partial objects and restart so it re-runs cleanly.
5. Never edit an already-applied migration file: the runner will not re-apply it, and a future environment will diverge.
6. Take a dump before step 4.

### 5.7 Acceptance tests for database initialization

| # | Scenario | Pass condition |
|---|---|---|
| DB-1 | Empty `GuildBank` database | All 8 migrations applied; `schema_migrations` has 8 rows; second start applies 0 |
| DB-2 | Second container starts concurrently | One migrates, the other waits on `GET_LOCK` and then reports "up to date" |
| DB-3 | `items` pre-populated with 144k rows | Migrations still apply; no write to `items`; `Audit` unaffected |
| DB-4 | User granted only `SELECT` | Preflight warns; migration fails with a clear "ALTER privilege required" hint |
| DB-5 | Existing unrelated tables, no `schema_migrations` | Preflight refuses; nothing is created |
| DB-6 | Old image started after a new one ran `0009` | Runner fails with "embedded migration 0008 is older than applied version 0009"; rollback procedure applies (§10.4) |
| DB-7 | Database created with `utf8mb3`/`latin1` | Preflight fails with the exact `ALTER DATABASE` command to run |

---

## 6. MariaDB requirements

### 6.1 Server requirements

| Requirement | Value | Rationale |
|---|---|---|
| Version | **11.4 LTS recommended**, minimum 10.6 (the repo tests against 11.4.13) | `0008` uses a generated column (10.2+); 11.4 is the LTS the project standardises on |
| Engine | InnoDB | FKs, row-level locking, crash recovery |
| Database charset/collation | `utf8mb4` / `utf8mb4_unicode_ci` | The driver hard-codes `Collation: utf8mb4_unicode_ci`; a `utf8mb3` database yields `1267 Illegal mix of collations` at join time |
| `lower_case_table_names` | Consistent; recommended `0` with all-lowercase schema objects | The app queries `items` lowercase; a dump restored as `Items` on a case-sensitive server is invisible |
| `max_connections` | ≥ 40 free for GuildBank (pool is 20 open / 10 idle) | Two instances plus MariaDB's own sessions must not exhaust the server |
| `max_allowed_packet` | ≥ 64 MB during item seeding | Item rows are wide (315 columns); seed batches stay well under this |
| `wait_timeout` | ≥ 600 s (default 28800 is fine) | Longer than the pool's 5 min idle / 30 min lifetime |
| `sql_mode` | `STRICT_TRANS_TABLES` (default), **no** `ANSI_QUOTES` | Some SQL uses backticks; `ANSI_QUOTES` would break quoting |
| Time zone | Anything; connections use `loc=UTC` | All datetimes are written as UTC by Go |
| Network | TCP 3306 reachable from the container | Unraid's default bridge requires the host IP |
| TLS | **Not supported by the current driver configuration** | No `tls` DSN parameter is set; keep MariaDB on the same host/LAN or gate on `OD-D5` |

### 6.2 Database creation (operator, once)

```sql
CREATE DATABASE IF NOT EXISTS GuildBank
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;
```

- **Do not** let the app create the database. It never needs `CREATE DATABASE`, and the
  operator keeps control of the collation — the single most common cause of a failed
  first run.
- If the database exists with the wrong collation,
  `ALTER DATABASE GuildBank CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;` fixes only
  the default for **new** tables; existing tables need
  `ALTER TABLE … CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;` or a
  recreate before first use.
- The database may be shared in principle, but this design assumes it is dedicated:
  sharing risks name collisions with the six app tables and makes the
  "refuse to adopt unknown schema" preflight fire.

### 6.3 Privileges: what the app actually needs

Derived from the code, not guessed:

| Privilege | Needed for |
|---|---|
| `SELECT` | All reads; `items` lookups; readiness pings |
| `INSERT` | Imports, `GuildBankItems`, `Audit`, `InventoryMovement`, `InventoryRequest`, `schema_migrations` rows |
| `UPDATE` | Inventory edits, request transitions, backfills, import batch status |
| `DELETE` | Row-deletion paths (soft-delete columns exist, but hard deletes are reachable) |
| `CREATE` | `CREATE TABLE` in `0001`–`0008` and `schema_migrations` |
| `ALTER` | `0003`, `0005`, `0007`, `0008`, including `DROP CONSTRAINT` |
| `INDEX` | `CREATE INDEX` in `0001`–`0008` and `DROP INDEX` (index changes need `INDEX`, not `DROP`) |
| `REFERENCES` | Foreign keys in `0006`, `0007`, `0008` |

Deliberately **not** granted: `DROP` (no migration drops a table), `CREATE DATABASE`,
`CREATE USER`, `GRANT OPTION`, `FILE`, `SUPER`, `PROCESS`, `TRIGGER`, `EVENT`,
`CREATE ROUTINE`, `EXECUTE`, `LOCK TABLES`, `SHOW VIEW`, `RELOAD`. Nothing in the code
requires them; `GET_LOCK`/`RELEASE_LOCK` need no privilege.

### 6.4 Grant profiles

**Profile A — enforced read-only `items` (recommended; satisfies SR-13).** Table-level DML
grants are required, because a database-level `INSERT/UPDATE/DELETE` grant would hand the
runtime user write access to `items` (grants are additive; there is no "everything except"
form):

```sql
CREATE USER 'guildbank_app'@'192.168.1.%' IDENTIFIED BY '<strong-password>';

GRANT SELECT                         ON `GuildBank`.`items`             TO 'guildbank_app'@'192.168.1.%';
-- items_meta is created by `-seed-items`; a GRANT on a missing table fails, so apply this
-- line after the first seed run (or create the table first).
GRANT SELECT                         ON `GuildBank`.`items_meta`        TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT, UPDATE, DELETE ON `GuildBank`.`GuildBankItems`    TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT, UPDATE, DELETE ON `GuildBank`.`ImportBatch`       TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT, UPDATE, DELETE ON `GuildBank`.`ImportList`        TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT, UPDATE, DELETE ON `GuildBank`.`Audit`             TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT, UPDATE, DELETE ON `GuildBank`.`InventoryMovement` TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT, UPDATE, DELETE ON `GuildBank`.`InventoryRequest`  TO 'guildbank_app'@'192.168.1.%';
GRANT SELECT, INSERT                 ON `GuildBank`.`schema_migrations` TO 'guildbank_app'@'192.168.1.%';

-- only if this principal also runs migrations (A1):
GRANT CREATE, ALTER, INDEX, REFERENCES ON `GuildBank`.* TO 'guildbank_app'@'192.168.1.%';
```

Two consequences must be documented, not hidden:

1. A release whose migration **adds a table** needs a matching `GRANT` before the app can
   write to it. Mitigation: every release note carries a copy-paste grant block, and the
   preflight reports "table `X` exists but the current user lacks INSERT".
2. The `ON GuildBank.*` DDL grant also permits `ALTER`/`CREATE`/`INDEX` on `items`. `DROP`
   is still absent, so a table cannot be destroyed, but `items` is not fully immutable.
   Closing that completely requires Profile B.

**Profile B — separate migration principal (strongest, post-v1).** `guildbank_migrate`
holds `CREATE, ALTER, INDEX, REFERENCES, SELECT, INSERT` on `GuildBank` and is used only by
`-migrate-only`; `guildbank_app` holds table-level DML and no DDL. Requires a second DSN in
the configuration — a code change, therefore out of scope here (`OD-D2`).

**Profile C — single database-level grant (simplest, weakest).**

```sql
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, INDEX, REFERENCES
  ON `GuildBank`.* TO 'guildbank_app'@'192.168.1.%';
```

Acceptable for officer testing only. It makes `items` writable by the app user, which is
precisely the gap SR-13 describes.

### 6.5 Host pattern and connection source

| Deployment | `GRANT … TO 'guildbank_app'@…` |
|---|---|
| Unraid bridge network, MariaDB on the same host | `'192.168.1.%'` (the container's traffic appears to come from the host IP) or `'%'` |
| Both containers on a user-defined Docker network | `'%'` (source addresses are inside the bridge subnet) |
| Separate DB host | That host's client subnet, never `'%'` |

`'%'` is acceptable only when MariaDB itself is unreachable from outside the LAN; the
design prefers subnet-scoped accounts, and the readiness report prints the effective
`CURRENT_USER()` so the mismatch is visible.

### 6.6 Backup principal and seed principal

| Principal | Grants | Used by |
|---|---|---|
| `guildbank_backup` | `SELECT, LOCK TABLES, SHOW VIEW, TRIGGER, EVENT` on `GuildBank.*` | `mariadb-dump` (§11). No DML, no DDL |
| `guildbank_seed` | `SELECT, INSERT, UPDATE, DELETE, CREATE` on `GuildBank.items` and `GuildBank.items_meta` only | The one-shot item seed (§7) and manual item refreshes. This is the only principal that may write `items` |

Splitting these means a compromised application credential cannot rewrite the item
catalogue and cannot read the whole schema beyond its own tables.

### 6.7 Acceptance tests for the database requirements

| # | Scenario | Pass condition |
|---|---|---|
| MD-1 | Profile A grants applied to an empty DB | All migrations succeed; app writes inventory; `INSERT INTO items` is denied |
| MD-2 | Profile C grants | Works identically, but `INSERT INTO items` succeeds (documents the weaker posture) |
| MD-3 | Grant from wrong host pattern | Startup fails with `Access denied`, and the preflight names `CURRENT_USER()` |
| MD-4 | `max_connections` exhausted | App retries; `/readyz` degrades rather than panics; recovers |
| MD-5 | Database collation `latin1` | Preflight fails before any DDL |
| MD-6 | `items` renamed to `Items` on a case-sensitive server | Preflight fails with the rename instruction |

---

## 7. Item data strategy

### 7.1 The constraint, stated precisely

| Fact | Consequence |
|---|---|
| The full EQItemList dump is 315 columns, 144,712 rows, 218,230,768 bytes, and is `.gitignore`d and excluded from the Docker context | It cannot be shipped in the image, in the repo, or in CI |
| The application reads **exactly four columns**: `id, name, classes, races` (`itemColumns` in `items.go`) | A four-column seed is functionally complete for every current feature |
| `items` is read-only to the application (`items_readonly_test.go`, SR-13) | The seed must not run inside the request-serving process's normal path |
| Setup already requires an administrator before anything else | The seed needs a credential the app does not hold, so it is an operator action |
| `classes`/`races` are `LONGTEXT` decimal strings, `name` is not unique | The seed must preserve column types and never resolve inventory by name |

**Decision: ship a slim, versioned item seed (four columns) applied by an explicit
one-shot mode of the same image. Never seed implicitly at startup.**

A four-column, PK-ordered seed of 144,712 rows is roughly 4–6 MB raw and 1–2 MB gzipped —
small enough to embed in the image, which keeps the install self-contained and
air-gap-friendly, and small enough that `INSERT … ON DUPLICATE KEY UPDATE` in batches
finishes in seconds.

### 7.2 Two supported datasets

| Dataset | Columns | Source | Who provides it | Seed behaviour |
|---|---|---|---|---|
| **Slim (default)** | 4 (`id`, `name`, `classes`, `races`) | Generated from the EQItemList export at release time | Ships embedded in the image | Creates `items`, upserts, records `items_meta` |
| **Full (optional)** | 315 | Operator's own EQItemList import | Operator | **Detected and left untouched** — the seed refuses to modify a table that has more than the four managed columns |

Detection rule: `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'items'`.
`> 4` → full dataset, skip with a clear log line. `0` → create. `1–4` → validate the four
column names and types; anything unexpected → abort without writing.

### 7.3 Seed artifact and versioning

| Property | Design |
|---|---|
| File name | `items_seed_v<YYYYMMDD>_<sha7>.sql.gz` (e.g. `items_seed_v20261127_4f2c1ab.sql.gz`) |
| Version string | `v<YYYYMMDD>_<sha7>` — the date of the source snapshot plus a hash of the seed content; both are printed by `-seed-items --dry-run` |
| Build | A small generator (not part of this design) reads the EQItemList dump, selects the four columns, writes batched `REPLACE`/upsert statements, gzips them, and computes the hash. It runs on a maintainer machine or a manual workflow, never in CI (the dump is not in the repo). |
| Storage | Embedded with `go:embed` in a `seed` package **and** attached to the GitHub Release as a verifiable artifact |
| Integrity | `SHA256` of the artifact is recorded in the release notes and asserted by a unit test against the embedded copy |
| Marker | Table `items_meta(Version VARCHAR(64) PK, SourceDate DATE NOT NULL, Rows INT NOT NULL, MaxID BIGINT NOT NULL, AppliedAt DATETIME(6) NOT NULL)` created by the seed, read by the app |
| Why not `schema_migrations` | That table's max applied version drives the out-of-order guard for *migrations*; a `v2026…` row would sort above `0008` and make every future migration fail the guard. Explicitly rejected. |

Because the seed owns `items_meta`, the app's Profile A grant list gains
`GRANT SELECT ON GuildBank.items_meta`, and the seed principal gains
`SELECT, INSERT, UPDATE, DELETE, CREATE` on `GuildBank.items` and `GuildBank.items_meta`.

### 7.4 Seed algorithm (`-seed-items`)

```text
1. load config (same env as the server: host, port, db, user, password)
2. preflight: connect, verify collation, verify CURRENT_USER() can CREATE/INSERT
3. GET_LOCK('guildbank:seed-items:<sha32(db)>', 60)   -- one seeder at a time
4. inspect items:
     absent                     -> create with the four columns and PK(id)
     exactly 4 managed columns  -> continue
     anything else              -> EXIT 0 "full/foreign items table detected; untouched"
5. compare items_meta version with the embedded seed version
     already applied and Rows/MaxID match -> EXIT 0 "up to date"
6. stream the gz seed in batches of 1,000 rows:
     INSERT INTO items (id, name, classes, races) VALUES (...),(...)
       ON DUPLICATE KEY UPDATE name=VALUES(name), classes=VALUES(classes), races=VALUES(races)
     commit per batch (never one 144k-row transaction)
     log progress every 25,000 rows
7. verify: COUNT(*), MIN(id), MAX(id) vs the seed manifest
8. UPSERT items_meta(version, source_date, rows, max_id, applied_at)
9. RELEASE_LOCK; EXIT 0 with a summary line
```

| Property | Value |
|---|---|
| Idempotent | Yes: upsert by PK plus the version short-circuit in step 5 |
| Resumable | Yes: re-running after a crash re-upserts the same rows |
| Concurrency-safe | Yes: advisory lock; the second run exits "another seed is running" |
| Memory | Bounded: one batch at a time, streamed from the gzip reader |
| Duration | Seconds to low minutes depending on the array; a 1,000-row batch never approaches the 120 s write timeout |
| Writes | `items` and `items_meta` only — never `GuildBankItems`, `Audit`, or anything else |
| Failure | Non-zero exit, nothing recorded in `items_meta`; partial upserts are harmless and re-run cleanly |

The seed is **not** a migration: it does not touch `schema_migrations`, is not required for
`/readyz`, and its absence degrades features rather than breaking the server (§7.6).

### 7.5 When the seed runs

| Situation | Action | Rationale |
|---|---|---|
| Fresh install (empty DB) | Operator runs the seed **after** the first successful start (schema exists) or after `-migrate-only` | Avoids racing `0001`–`0008` |
| Existing install with the full 315-column table | Seed detects and skips | Never overwrite an operator's richer dataset |
| New release with a newer seed version | Operator runs the seed during the upgrade window, after the new image has started once | Explicit, auditable, reversible via backup |
| Normal container start | **Nothing** | Startup must never write to `items` |
| `items` dropped by mistake | Re-run the seed | Recovery path |
| Air-gapped install | Same image, seed embedded | No download |

Optional automation (`OD-D16`): a one-shot `guildbank-seed` service in `docker-compose.yml`
gated by a Compose profile, so `docker compose --profile seed up` is one command. Always
explicit, never on container start.

### 7.6 Degradation when item metadata is missing or stale

| Condition | Behaviour |
|---|---|
| `items` absent | Imports still commit; every row is flagged metadata-missing; class/race display is blank; the picker reports "metadata unavailable". `/readyz` stays `200` |
| `items` present but a specific EQUID is absent | Only that row is flagged (existing AM-01/AM-02 rule, unchanged) |
| `items_meta` older than the image's seed version | Admin banner: "item metadata v… available (currently v…)" plus the exact command |
| `items` present with a foreign schema | Seed refuses; the banner explains both options |

New admin-facing surface (design only): a read-only `Settings → Item data` panel showing
`items_meta`, the row count, and the seed version bundled with the running image.

### 7.7 Data refresh cadence

| Question | Recommendation |
|---|---|
| How often does the seed change? | Only when a maintainer regenerates it from a newer EQItemList (typically with an EverQuest patch). Each release either bumps the seed or does not |
| Does a seed bump force an upgrade? | No — the app works with any `items` contents; the version only drives the "update available" banner |
| Can the operator use a newer seed than the image ships? | Yes, by running a newer image's `-seed-items`, or by importing the full dataset. The image is a distribution channel, not a gate |
| Old seed, new item requested by an officer | The picker reports "not in metadata"; the officer can still record the item by id/name where the UI allows it (existing behaviour) |

### 7.8 Alternatives considered and rejected

| Alternative | Why rejected |
|---|---|
| Auto-seed at startup | Violates the read-only contract with `items` (SR-13), races migrations, makes startup data-dependent, and lets an app bug overwrite an operator's dataset |
| Ship the 218 MB dump in the image | Bloat, redistribution question, and 311 columns the app never reads |
| Require the operator to import the full dump | Today's situation, and the gap this design closes; kept as an *option*, not a requirement |
| Download the seed at first run | Breaks air-gapped installs and adds a download to the boot path |
| Record the seed in `schema_migrations` | Breaks the out-of-order guard (§7.3) |
| Keep item metadata in SQLite | Duplicates 144k rows into the single-writer store, bloats `/config` backups, diverges from the table the queries already use |

### 7.9 Acceptance tests for item data

| # | Scenario | Pass condition |
|---|---|---|
| IT-1 | Empty DB, run `-seed-items` | `items` has 144,712 rows; `items_meta` has one row; second run exits "up to date" in < 2 s |
| IT-2 | Two concurrent seed runs | One proceeds, one exits "another seed is running"; final counts correct |
| IT-3 | Full 315-column table present | Seed exits 0 "untouched"; row count unchanged |
| IT-4 | Kill the seed mid-run, then re-run | Final counts match the manifest; no duplicates |
| IT-5 | App started with `items` absent | `/readyz` 200; an import still commits with metadata-missing flags; banner shows the seed command |
| IT-6 | Seed credential used to write `GuildBankItems` | Denied by grants |
| IT-7 | App credential attempts `INSERT INTO items` | Denied by grants (Profile A) |

---

## 8. Configuration model

### 8.1 Existing process configuration (authoritative, from `config.go`)

Precedence, highest first: **command-line flags → environment → built-in defaults.**
Verified against `Load`, `applyEnv` and `Validate`:

| Variable | Flag | Default | Validation |
|---|---|---|---|
| `GUILDBANK_PORT` | `-port` | `8080` | 1–65535 |
| `GUILDBANK_SQLITE_PATH` | `-sqlite-path` | `/config/guildbank.db` | Non-empty, no NUL byte |
| `GUILDBANK_LOG_LEVEL` | `-log-level` | `info` | `debug`/`info`/`warn`/`error` |
| `GUILDBANK_LOG_FORMAT` | `-log-format` | `json` | `json`/`text` |
| `GUILDBANK_TRUSTED_PROXIES` | — (env only) | empty | Comma-separated IPs or CIDRs |
| `GUILDBANK_SECURE_COOKIES` | `-secure-cookies` | `false` | Bool |
| `GUILDBANK_MARIADB_HOST` | — | empty | Empty = no guild store |
| `GUILDBANK_MARIADB_PORT` | — | `3306` | 1–65535 |
| `GUILDBANK_MARIADB_DATABASE` | — | `GuildBank` | Non-empty |
| `GUILDBANK_MARIADB_USERNAME` | — | empty | Non-empty when host is set |
| `GUILDBANK_MARIADB_PASSWORD` | — | empty | Not validated (may be empty; discouraged) |
| `GUILDBANK_MARIADB_INIT_ATTEMPTS` | `-mariadb-init-attempts` | `3` | 1–100 |

There is no `GUILDBANK_LISTEN_ADDR`, no `GUILDBANK_BASE_URL`, no
`GUILDBANK_COOKIE_NAME` and no Discord variables — the public URL, the Discord
application and the session timeouts are **Settings rows in SQLite**, not environment
(§8.2). The port is a plain integer, not a `host:port` address, and the server always
binds all interfaces on that port.

**Bootstrap caveat carried from the source** (`config.go`, PD-18/E-11): the environment's
MariaDB values are the *live* pool configuration. The setup wizard persists a copy into
Settings, but the running pool still uses the environment. Consequences this design
adopts:

1. Production **must** set `GUILDBANK_MARIADB_HOST` … `_PASSWORD` as environment
   variables; the wizard alone cannot connect the store.
2. The wizard compares the values it is given with the effective environment and
   **refuses to complete** when they disagree (host, port, database, username), because
   the app would otherwise run against a database the operator never configured.
3. Changing the MariaDB target requires editing the environment and restarting — not an
   in-app setting. This is a known limitation, recorded as `OD-D8`.

Also existing: `-consolidate-items`, `-dry-run`, `-consolidate-actor` (one-shot
maintenance mode, QTY-09). It requires a configured guild store, exits after the pass, and
is the precedent for the one-shot `-seed-items` mode in §7.

### 8.2 Proposed additions (design only — not implemented)

Naming rules kept from the existing code: `GUILDBANK_` prefix, flat (no nesting), upper
snake case, no secrets as defaults, flags override environment. `PUID`/`PGID` are the one
deliberate exception because those names are the Unraid/linuxserver convention and users
expect them (§3.4).

| Addition | Kind | Default | Purpose |
|---|---|---|---|
| `PUID` / `PGID` | env | `10001` / `10001` | Ownership of `/config`; Unraid sets `99`/`100` |
| `GUILDBANK_SETUP_TOKEN` | env | empty | When set, `/setup` requires the token (`OD-D7`) |
| `GUILDBANK_TRUSTED_PROXY_HEADER` | env | `X-Forwarded-For` | Documents the assumed header; only consulted for proxies in `GUILDBANK_TRUSTED_PROXIES` |
| `GUILDBANK_ITEMS_SEED_URL` | env | empty | Optional operator-supplied seed artifact path/URL; only read by the explicit seed mode, never at boot |
| `-seed-items` | flag | `false` | Applies the embedded seed and exits (§7.4) |
| `-seed-dry-run` | flag | `false` | Prints the seed version, target and planned row count; writes nothing |

No environment variable is added for anything already owned by Settings: adding
`GUILDBANK_DISCORD_*` or `GUILDBANK_BASE_URL` would create a second source of truth for
OAuth identity and invite exactly the drift this design is trying to avoid.

### 8.3 Application settings (SQLite, not environment)

Persisted in `/config/guildbank.db` and edited in the wizard and admin UI. Verified keys
from `service/settings.go`:

| Key | Type | Default | Production guidance |
|---|---|---|---|
| `GuildName` | string | `Aeternus` | Set to the real guild name (1–64 chars) |
| `GuildOpenDays` | int | `14` | Guild-bank open window; keep the guild's rule |
| `BazaarDays` | int | `30` | Bazaar window |
| `EQResourceBaseUrl` | URL | `https://items.eqresource.com/items.php?id=` | Leave default unless EQResource moves |
| `MariaDbHost` | string | empty | **Must equal** `GUILDBANK_MARIADB_HOST` |
| `MariaDbPort` | int | `3306` | **Must equal** `GUILDBANK_MARIADB_PORT` |
| `MariaDbDatabase` | string | `GuildBank` | **Must equal** `GUILDBANK_MARIADB_DATABASE` |
| `MariaDbUsername` | string | empty | **Must equal** `GUILDBANK_MARIADB_USERNAME` |
| `MariaDbPassword` | secret | empty | Must authenticate; blank in the form keeps the stored value |
| `SessionIdleMinutes` | int | `30` | 5–1440 |
| `SessionAbsoluteHours` | int | `8` | days; 1–730 (key name is historical) |
| `PublicBaseUrl` | URL | empty | Required for Discord: the exact public origin, e.g. `https://guildbank.example.com` |
| `DiscordEnabled` | bool | `false` | Enable only after ID, secret and public URL are set |
| `DiscordClientId` | string | empty | From the Discord developer portal |
| `DiscordClientSecret` | secret | empty | Never logged; blank keeps the stored value |
| `DiscordGuildId` | string | empty | Optional; enables membership checks for linking and self-registration |
| `SelfRegistrationEnabled` | bool | `false` | Leave off unless Discord guild verification is configured |

Because these live in SQLite, **`/config` is a secret**: it holds the Discord client secret
and a copy of the MariaDB password. Backups of `/config` must be treated as credentials
(§11).

### 8.4 Effective production configuration

| Concern | Source | Unraid template value |
|---|---|---|
| Listen port | `GUILDBANK_PORT` | `8080` |
| Data directory | `GUILDBANK_SQLITE_PATH` | `/config/guildbank.db` (inside the `/config` volume) |
| Log | `GUILDBANK_LOG_LEVEL` / `_FORMAT` | `info` / `json` (Unraid log view handles JSON lines) |
| Reverse proxy | `GUILDBANK_TRUSTED_PROXIES` | The proxy's IP/CIDR; also depends on `OD-D11` (network mode) |
| Cookies | `GUILDBANK_SECURE_COOKIES` | `true` whenever the public URL is HTTPS |
| Public URL, Discord, guild windows, session timeouts | Settings | Entered in the wizard |
| MariaDB target | env **and** mirrored in Settings | Both must agree; env wins |
| MariaDB credentials | env `GUILDBANK_MARIADB_USERNAME`/`_PASSWORD` | Profile A/B account from §6.4 |
| Item seed | Release artifact, applied by `-seed-items` | No configuration needed in the common case |

### 8.5 Configuration diagnostics

New preflight checks the design requires (all read-only, all run before readiness):

| Check | Failure message |
|---|---|
| `/config` writable by the process uid | "cannot write /config — check PUID/PGID and the appdata share" |
| SQLite opens and `PRAGMA quick_check` passes | "guildbank.db is corrupt; restore from backup" |
| `GUILDBANK_MARIADB_HOST` set but port/database/username invalid | Existing `Validate` error, exit 2 (already implemented) |
| Settings `MariaDb*` disagree with the environment | "Settings say host X, environment says Y — the environment is authoritative; update the wizard values" |
| `GUILDBANK_SECURE_COOKIES=true` while `PublicBaseUrl` is `http://…` | Warning: cookies will not be sent; fix one of the two |
| `DiscordEnabled=true` without ID/secret/public URL | Refuse to enable (existing Settings validation) |
| `GUILDBANK_TRUSTED_PROXIES` empty while a proxy is in front | Warning: client IPs will be the proxy's (affects audit and rate limits) |

---

## 9. Unraid template design

No Unraid template exists in the repository today (the root holds only `Dockerfile` and
`docker-compose.yml`). The design adds **one file, `unraid/guildbank.xml`**, plus an icon,
and treats it as a release artifact kept in step with the image.

### 9.1 Template identity

| Element | Value | Note |
|---|---|---|
| `<Name>` | `GuildBank` | Shown in the Docker tab |
| `<Repository>` | `ghcr.io/panazule/guildbank` | Without a tag Unraid's "Check for Updates" follows `latest`, which is too loose — the shipped template pins `X.Y` (§9.4) |
| `<Network>` | `bridge` | See §9.5 for the MariaDB host consequence |
| `<Privileged>` | `false` | Never required |
| `<Shell>` | `sh` | The runtime image is Alpine |
| `<Icon>` | `https://raw.githubusercontent.com/panazule/GuildBank/main/Docs/icon.png` | **The icon must be added to `Docs/` before publication**; this path is a placeholder requirement, not an existing file |
| `<WebUI>` | `http://[IP]:[PORT:8080]/` | Lets Unraid offer a click-through |
| `<Category>` | `Tools: Game Servers:` (or the closest category agreed with the maintainer) | Only matters for Community Applications |
| `<Support>` / `<Project>` | Issues URL / repository URL | Required for a CA submission |
| `<Overview>` | Single container, requires an existing MariaDB, requires a reverse proxy for TLS, one `/config` volume | Mirrors §2 and §8 |
| `<ExtraParams>` | Empty by default | Operators add `--label`/`--log-opt` here |

### 9.2 Paths and ports

| `<Config>` | Type | Container | Host default | Mode | Required |
|---|---|---|---|---|---|
| Appdata | `Path` | `/config` | `/mnt/user/appdata/guildbank` | `rw` | Yes |
| Web UI | `Port` | `8080` | `8080` | `tcp` | Yes |

Help text that belongs in the template:

- `/config` holds SQLite (`guildbank.db`), the Settings secrets and the only mutable state.
  Put it on a pool/share with snapshots where available.
- Host port `8080` is contested on many Unraid boxes. The **host** port may change (e.g.
  `8081`); the **container** port must stay `8080` unless `GUILDBANK_PORT` is changed to
  match, and the reverse proxy must target whichever host port is chosen.
- Do **not** add a second path for database dumps: dumps belong to the MariaDB container's
  share, not to GuildBank's appdata.
- A read-only root filesystem is not offered yet; it would need a writable `/tmp` mount and
  is recorded as future hardening (`OD-D13`).

### 9.3 Environment variables exposed by the template

| `<Config>` label | Variable | Default | Masked |
|---|---|---|---|
| User ID | `PUID` | `99` | No |
| Group ID | `PGID` | `100` | No |
| MariaDB host | `GUILDBANK_MARIADB_HOST` | empty (must be filled) | No |
| MariaDB port | `GUILDBANK_MARIADB_PORT` | `3306` | No |
| MariaDB database | `GUILDBANK_MARIADB_DATABASE` | `GuildBank` | No |
| MariaDB user | `GUILDBANK_MARIADB_USERNAME` | empty | No |
| MariaDB password | `GUILDBANK_MARIADB_PASSWORD` | empty | **Yes** (`Display="password"`) |
| Log level | `GUILDBANK_LOG_LEVEL` | `info` | No |
| Log format | `GUILDBANK_LOG_FORMAT` | `json` | No |
| Secure cookies | `GUILDBANK_SECURE_COOKIES` | `true` | No |
| Trusted proxies | `GUILDBANK_TRUSTED_PROXIES` | empty | No |
| Setup token (optional) | `GUILDBANK_SETUP_TOKEN` | empty | **Yes** |

Deliberately **not** in the template: `GUILDBANK_PORT` (fixed at 8080 by the image
default), `GUILDBANK_SQLITE_PATH` (fixed at `/config/guildbank.db`),
`GUILDBANK_MARIADB_INIT_ATTEMPTS` (the default suits production), the QTY-09 consolidation
flags (CLI actions, not template fields), and every Settings-owned value — public URL,
Discord credentials, guild name, windows, session timeouts. Keeping Settings out of the
template is what prevents a second source of truth for OAuth identity.

### 9.4 Tag, update and lifecycle

| Concern | Design |
|---|---|
| Default tag | `X.Y` (e.g. `1.4`), so patch releases arrive through "Check for Updates" and a minor bump is an explicit operator action (`OD-D9`) |
| Release notes | `<Overview>` links to the GitHub release; the changelog is not embedded |
| Auto-update | Permitted, but the help text states the §11 pre-upgrade backup requirement — Unraid auto-update does not take one |
| Downgrade | Not supported by the UI; the runbook is §10.5 (pin the older `X.Y.Z`, restore the backup) |
| Removal | Deleting the container must not delete `/config`; the help text says so explicitly |
| Beta | `<Beta>false</Beta>`; `edge` and `sha-*` tags are documented for testers but never defaulted |

### 9.5 Network-mode note (the one that bites)

With `bridge`, the MariaDB container is reachable **by container name only when both
containers share a user-defined network**. Unraid's Docker tab puts containers on the
default `bridge`, where name resolution does not work, so:

| Option | MariaDB host value | Trade-off |
|---|---|---|
| MariaDB on the same Unraid host, default bridge | The Unraid host IP (e.g. `192.168.1.10`) with `3306` published | Simple, but exposes MariaDB on the LAN; the host grant pattern in §6.5 applies |
| Both containers on a user-defined network | The MariaDB container name | Requires `docker network create` plus template cooperation; not achievable purely in the template UI |
| `host` network mode | `127.0.0.1` or the host IP | Simplest DNS story, loses port isolation, collides with Unraid services |

**Recommendation:** default the template to `bridge`, document "use the Unraid host IP",
and recommend restricting `3306` (MariaDB `bind-address` and/or host firewall) to the Unraid
host and the proxy. Recorded as `OD-D11`.

### 9.6 Template acceptance checks

| # | Check | Pass condition |
|---|---|---|
| UT-1 | Install from the template's XML on a clean box | Wizard reachable; `/config` created `99:100` |
| UT-2 | Install with an empty MariaDB host | Container starts, `/readyz` is 503 with `not_configured`, log names the missing variable |
| UT-3 | Host port changed to 8081 | Only the proxy target changes; the app still listens on 8080 |
| UT-4 | Remove and re-add the container | Existing `/config` is picked up; no re-setup, no data loss |
| UT-5 | `X.Y` update after a patch release | Image pulled, container recreated, schema unchanged, `/readyz` 200 |
| UT-6 | Template XML validated | No unknown elements; every `Path`/`Port`/`Variable` has `Target`, `Default`, `Mode`, `Description` |

---

## 10. Upgrade strategy

### 10.1 Principles

1. **Backup precedes upgrade.** No image is started against data that has not been
   snapshotted (§11). This is the only genuine safety net, because:
2. **There is no schema rollback.** Migrations are forward-only and are not wrapped in
   transactions; MariaDB commits DDL implicitly. Un-applying a migration would require
   hand-written SQL that nobody rehearsed.
3. **An older image still starts against a newer schema.** The runner only considers the
   migrations it embeds: versions already in `schema_migrations` are skipped and an unknown
   newer row merely raises `maxApplied`. A downgrade is therefore *silently accepted* and can
   fail later at query time — which is why the documented rollback is "previous image **plus**
   restore", never "previous image alone".
4. **Migrations complete before readiness.** Readiness is published only after `Initialize`
   succeeds, so neither an orchestrator nor a human watching `/readyz` sees traffic admitted
   to a half-migrated database.
5. **One minor at a time.** Skipping minors is untested, because a later minor may depend on
   schema written by an earlier one.

### 10.2 The upgrade runbook

| # | Step | Command / action | Abort condition |
|---|---|---|---|
| 1 | Read the release notes | Schema changes, seed version, minimum MariaDB | Notes missing → stop |
| 2 | Record the current state | Running tag, `SELECT MAX(Version) FROM schema_migrations`, SQLite `PRAGMA user_version` | Cannot read them → stop |
| 3 | Back up both stores | §11.2; verify both dump files exist and are non-zero | Any failure → stop |
| 4 | Pull the new image | `docker pull ghcr.io/panazule/guildbank:X.Y.Z` | Pull fails → nothing changed |
| 5 | Stop the container | Unraid: stop (graceful SIGTERM, requests drain) | Not stopped in 60 s → investigate first |
| 6 | Start the new image | Unraid: change the tag or "Force Update"; env and `/config` unchanged | — |
| 7 | Watch startup | `docker logs -f guildbank`: config → SQLite → MariaDB → migrations → repairs → "ready" | Migration error → §10.5 |
| 8 | Verify readiness | `curl -fsS http://<host>:8080/readyz` → 200 | 503 for > 5 min → §10.5 |
| 9 | Verify the schema | `MAX(Version)` equals the version named in the release notes | Lower than expected → migration did not run |
| 10 | Apply a new seed if the notes bump it | `docker run --rm <same env> …:X.Y.Z -seed-items` (§7.4) | Non-zero exit → metadata stays stale, app keeps working |
| 11 | Smoke test | Sign in, open inventory, open audit, confirm `/healthz`, scan the log for new warnings | Any functional break → §10.5 |
| 12 | Prune | Remove the old tag once satisfied; keep the previous `X.Y.Z` for one cycle | — |

Step 5 exists because a clean stop checkpoints SQLite and lets in-flight requests finish. A
restart mid-import is survivable (interrupted-import recovery runs at startup) but muddies
the upgrade's log.

### 10.3 What happens during the first start of a new version

| Phase | Action | Fatal? |
|---|---|---|
| Config | Environment parsed and validated | Yes → exit 2 |
| SQLite | Open, `PRAGMA user_version` migration `0001`–`0005` | Yes |
| Provisioning | Setup state read; `/readyz` stays 503 while setup is pending | No |
| MariaDB | Connect with retry (1 s → 30 s backoff; connectivity retries are unlimited) | Waits, never exits |
| Migration | Under `GET_LOCK`, apply embedded migrations newer than `maxApplied` | Yes, after `InitAttempts` |
| Repairs | Interrupted-import recovery; `LocationGroup`/`LocationSort` backfill; spell metadata backfill (500 rows each) | Yes (part of `Initialize`) |
| Ready | `/readyz` 200, `guild_store: ready` | — |

Startup time is data-dependent: the repair passes are bounded to 500 rows each and finish in
seconds on a healthy database, but a large interrupted import stretches the MariaDB phase. The
operator expectation to document is a **10-minute readiness window**; the template help text
should say so.

### 10.4 Version compatibility

| Combination | Supported | Behaviour |
|---|---|---|
| New image, older schema | Yes | Migrations apply — the normal path |
| Same image, same schema | Yes | Nothing to apply; repairs still run |
| New image, newer schema (from `edge`) | Warned | Known versions skipped, higher `maxApplied` accepted; correctness not guaranteed — testers only |
| Old image, newer schema | **No** | Starts (§10.1: no version gate) and may fail at query time; recover by restoring the pre-upgrade backup |
| New app, same `/config` | Yes | SQLite migrations are additive in the current range; the file is not downgrade-safe |

`edge` builds carry no compatibility promise; `X.Y.Z` releases do, within the pairs above.

### 10.5 Rollback and recovery

| Situation | Action |
|---|---|
| New image fails before migrating | Stop, re-tag the previous `X.Y.Z`, start, verify `/readyz`. No restore needed |
| Migration failed mid-file (partial schema) | Stop. Inspect per §5.6, restore the MariaDB dump from step 3, then retry the new image or run the previous one |
| Post-upgrade functional break | Stop, re-tag the previous `X.Y.Z`, **restore both** the SQLite file and the MariaDB dump, start, verify |
| Corruption found days later | Restore is the only path; everything after the backup point is lost. This is why step 3 is non-negotiable |
| Item seed went wrong | `items`/`items_meta` are independent of the upgrade: re-run `-seed-items`, or restore the two tables from a full (unfiltered) dump |

### 10.6 Upgrade acceptance tests

| # | Scenario | Pass condition |
|---|---|---|
| UP-1 | Patch upgrade `X.Y.1` → `X.Y.2`, no migrations | Container recreated, `/readyz` 200 in < 2 min, schema version unchanged |
| UP-2 | Minor upgrade with one new migration | Migration applied once; a second restart applies nothing |
| UP-3 | Upgrade while MariaDB is stopped | Container stays up, `/readyz` 503, no exit; starting MariaDB finishes the upgrade without a restart |
| UP-4 | Migration failure injected | Container exits non-zero after `InitAttempts`; the log names the failing version; data intact |
| UP-5 | Rollback rehearsal | Previous image + restored dumps reproduce the pre-upgrade state exactly (row counts on every table) |
| UP-6 | Upgrade while an import is in flight | The startup repair recovers the import; no duplicate or orphaned rows |

---

## 11. Backup strategy

### 11.1 What is backed up, what is deliberately not

| Store | Contents | Backed up? | Method | Cadence |
|---|---|---|---|---|
| SQLite `/config` | Accounts, sessions, Settings (including the Discord secret and a copy of the MariaDB password), setup state | **Yes — mandatory** | Container stopped, directory copy (the file is WAL-mode; a graceful stop checkpoints and truncates the WAL via `PRAGMA wal_checkpoint(TRUNCATE)`, `sqlite/store.go:122`) | Weekly + before every upgrade |
| MariaDB application tables | `ImportBatch`, `ImportList`, `GuildBankItems`, `InventoryMovement`, `InventoryRequest`, `Audit`, `schema_migrations` | **Yes — mandatory** | `mariadb-dump --single-transaction` | Daily + before every upgrade |
| `items` (slim seed) | 144,712 reference rows | **No** | Reproducible from the release artifact; re-running `-seed-items` restores it exactly | — |
| `items_meta` | Seed version record | **No** | Recreated by `-seed-items` | — |
| `items` (operator's full 315-column dataset) | Third-party dataset | **Yes, if present** | Same dump with the `--ignore-table` arguments removed | Whatever the operator already does |
| Image and env | Container tag, environment, template XML | Documented in the runbook, not dumped | Note the tag and export the template XML | Each upgrade |
| MariaDB server itself | `my.cnf`, datadir, grants | Out of scope — belongs to the MariaDB container's owner | `SHOW GRANTS` output captured in the runbook | Once, on change |

The `items` exclusion is the single biggest backup-size win: the dump drops from ~218 MB of
reference data to the guild's own transactions, which is small. It also makes restores fast
enough to rehearse.

### 11.2 Pre-upgrade backup commands

```bash
# 1. SQLite: stop first so the WAL is checkpointed and the copy is consistent.
docker stop guildbank
cp -a /mnt/user/appdata/guildbank "/mnt/user/backups/guildbank/config-$(date +%F-%H%M)"
docker start guildbank

# 2. MariaDB: consistent snapshot without blocking writers (InnoDB).
docker exec mariadb mariadb-dump \
  --single-transaction --quick --routines --triggers --events \
  --default-character-set=utf8mb4 \
  --ignore-table=GuildBank.items \
  --ignore-table=GuildBank.items_meta \
  -u guildbank_backup -p GuildBank | gzip > "/mnt/user/backups/guildbank/db-$(date +%F-%H%M).sql.gz"

# 3. Verify before touching the image.
ls -l /mnt/user/backups/guildbank/ && gzip -t /mnt/user/backups/guildbank/db-*.sql.gz
docker exec mariadb mariadb -u guildbank_backup -p -e 'SELECT MAX(Version) FROM GuildBank.schema_migrations'
```

| Rule | Reason |
|---|---|
| Stop the app before copying `/config` | A live copy can miss a WAL page; the app checkpoints on close, not continuously |
| `--single-transaction` | Gives a consistent InnoDB snapshot without `LOCK TABLES`, so officers are not blocked |
| `--ignore-table` exactly as above | Keeps the dump small and avoids overwriting a foreign `items` on restore |
| Verify (`gzip -t`, non-zero size, readable `MAX(Version)`) | An unverified backup is indistinguishable from no backup |
| Record the values from step 3 with the backup | The rollback test compares against them (UP-5) |

The backup principal and its grants are defined in §6.6 (`guildbank_backup`: `SELECT`,
`LOCK TABLES`, `SHOW VIEW`, `TRIGGER`, `EVENT` — no write privilege on any table).

### 11.3 Restore procedure

| # | Step | Command |
|---|---|---|
| 1 | Stop the app | `docker stop guildbank` |
| 2 | Restore MariaDB into an empty `GuildBank` | `gunzip -c db-*.sql.gz \| docker exec -i mariadb mariadb -u root -p GuildBank` |
| 3 | Restore SQLite | `rm -f /mnt/user/appdata/guildbank/guildbank.db*` then `cp -a config-<stamp>/. /mnt/user/appdata/guildbank/` |
| 4 | Re-seed item metadata | `docker run --rm <env> …:X.Y.Z -seed-items` |
| 5 | Start and verify | `docker start guildbank`; `/readyz` → 200; row counts per table equal the recorded values |
| 6 | Sign in and smoke test | Admin login, inventory list, one audit page |

Restoring SQLite alone loses nothing in MariaDB and vice versa, but the two are linked by the
setup state: a `/config` restored from before a MariaDB re-point will make the app talk to the
wrong database, which is exactly why the wizard refuses mismatches (§8.1). Restore both
together whenever possible.

### 11.4 Retention and scheduling

| Layer | Frequency | Keep | Where |
|---|---|---|---|
| MariaDB dump | Daily (cron on Unraid, or the MariaDB container's own tooling) | 14 daily, 8 weekly, 12 monthly | `/mnt/user/backups/guildbank/`, ideally copied off the box |
| `/config` copy | Weekly | 8 | Same |
| Pre-upgrade pair | Every upgrade | Until the next successful upgrade | Same, named with the tag being left |
| Restore rehearsal | Once per quarter, and once before the first production release | Latest rehearsal result recorded in the runbook | — |

Unraid's appdata backup plugin can take the `/config` copy correctly because it stops
containers first; it does **not** cover MariaDB, so the daily dump is a separate job. Neither
job may run during a scheduled import window if the guild cares about import timing — the
dump is non-blocking, so this is a preference, not a requirement.

### 11.5 Backup confidentiality

`/config` contains the Discord client secret, the MariaDB password copy, session data and the
password hashes. The MariaDB dump contains guild members' data and the full audit trail.

| Requirement | Implementation |
|---|---|
| Encryption at rest | Unraid array/pool encryption, or encrypt the archives (`age`/`gpg`) before they leave the box |
| Off-box copy | At least one copy on a different device (Unraid backup target, NAS, or cloud) |
| Access | Only administrators; backups are not served by GuildBank and must never be placed under a web-served share |
| Git | Never. Backups are not source artifacts |
| Restore to a test box | Use a throwaway copy; the restored data is production-equivalent |

### 11.6 What the backup does not cover

| Gap | Mitigation |
|---|---|
| The MariaDB server's own configuration and grants | Capture `SHOW GRANTS FOR 'guildbank'@'…'` and the `CREATE DATABASE` statement in the runbook (§6.2, §6.4) |
| The container/template definition | Export the Unraid template XML with each upgrade; it is small and text |
| The image itself | Tags are immutable in GHCR, so re-pulling `X.Y.Z` reproduces it; `sha-<7>` pins the commit |
| The item seed artifact | Embedded in the image and attached to the release; both are immutable |
| Data written after the backup point | Nothing — that window is genuinely lost. Communicate the backup timestamp when restoring |

### 11.7 Backup acceptance tests

| # | Scenario | Pass condition |
|---|---|---|
| BK-1 | Take the pre-upgrade pair | Both artifacts exist, are non-zero, `gzip -t` passes, `MAX(Version)` readable |
| BK-2 | Restore onto a clean box | `/readyz` 200; per-table row counts match the source; admin can sign in |
| BK-3 | Restore without re-seeding items | App still functions; item metadata is reported missing and the banner offers `-seed-items` |
| BK-4 | Dump excludes `items`/`items_meta` | Dump size is orders of magnitude below 218 MB and contains no `INSERT INTO \`items\`` |
| BK-5 | Backup while an officer is working | Dump completes with `--single-transaction`; no lock-wait errors in the app log |
| BK-6 | Restore a pre-upgrade backup after a failed upgrade | The previous image starts against the restored schema and serves `/readyz` 200 |
| BK-7 | Backup taken while the container is running (negative test) | Reviewer confirms the runbook's stop-first rule is what prevents this; a live copy is *not* accepted as a valid backup |

---

## 12. Operations and troubleshooting

### 12.1 Symptom → cause → action

| Symptom | Likely cause | Action |
|---|---|---|
| Container restarts in a loop, log says `permission denied` on `/config/guildbank.db` | `/config` owned by root while the process is UID 10001 (G2) | Fix ownership (`chown -R 99:100`), or ship `PUID`/`PGID` (`OD-D3`) |
| `/readyz` 503, `guild_store: not_configured` | `GUILDBANK_MARIADB_HOST` empty | Set the four MariaDB variables and restart |
| `/readyz` 503, `guild_store: initializing` forever | MariaDB unreachable; connectivity retries are unlimited | Check host/port from inside the container (`docker exec guildbank wget -qO- http://host:3306` is not valid — use the app log's dial error), network mode `OD-D11` |
| Log: `mariadb: acquire migration lock` timeout | Another instance is migrating, or a stale lock from a killed process | Confirm only one container runs; `SELECT IS_USED_LOCK('guildbank:migrate:…')` |
| Log: `embedded migration X is older than applied version Y` | A migration file was renamed or back-dated | Do not work around it: revert the file name, or ship it as a new higher version |
| Log: `1267 Illegal mix of collations` | Database/table collation is not `utf8mb4_unicode_ci` | §6.2; convert tables before first use |
| `SHOW TABLES` has `Items` instead of `items` | Dump restored on a case-sensitive server with different case | Rename to `items` (`lower_case_table_names=0` guidance, §6.1) |
| Every import row flagged metadata-missing | `items` empty or absent | Run `-seed-items` (§7.5) |
| Item picker empty, log mentions `items` permission denied | App principal lacks `SELECT` on `items` | Re-apply Profile A grants (§6.4) |
| Session lost on restart / after upgrade | `GUILDBANK_SECURE_COOKIES=true` while accessed over `http://` | Fix the proxy or the flag; both must agree with `PublicBaseUrl` |
| Discord sign-in fails with `redirect_uri_mismatch` | `PublicBaseUrl` differs from the URL registered in Discord | Make them identical, including scheme and no trailing slash |
| Audit shows the proxy's IP for every action | `GUILDBANK_TRUSTED_PROXIES` empty | Set the proxy's CIDR (§2.3) |
| Wizard completes but inventory stays unavailable | Settings MariaDB values disagree with the environment | The environment wins; correct the Settings copy (§8.1) |

### 12.2 Routine operator tasks

| Task | Cadence | Command |
|---|---|---|
| Check health | Whenever | `curl -fsS http://<host>:8080/readyz`, `/healthz` |
| Tail logs | On demand | `docker logs --tail 200 guildbank` |
| Inspect schema version | On demand | `SELECT MAX(Version) FROM GuildBank.schema_migrations` |
| Item seed status | On demand | `SELECT * FROM GuildBank.items_meta` (or the admin panel) |
| Backup | Daily/weekly | §11.2 |
| Consolidate duplicate stacks (QTY-09) | Only when the audit view reports them | `docker run --rm <env> … -consolidate-items -dry-run`, then without `-dry-run` |
| Re-seed items after a patch | When release notes say so | `docker run --rm <env> … -seed-items` |

### 12.3 Log and diagnostic expectations

| Endpoint / output | Content | Never contains |
|---|---|---|
| `/healthz` | Liveness only, always 200 while the process runs | Store details |
| `/readyz` | 200/503 plus a component summary (`guild_store`, setup state) | Credentials, host names beyond the redacted form |
| Startup log | Version, commit, build date, redacted MariaDB target (`user@host:port/db`), migration results | Passwords, the Discord secret, session tokens |
| Migration log | Each version applied once, plus the resulting `latest` | SQL bodies |

`MariaDB.Redacted()` already guarantees the password never appears in the connection log
line; the design requires the same rule for the new seed mode and preflight output.

---

## 13. Implementation backlog implied by this design

Nothing here is implemented. The list is the smallest set of changes that makes the design
true, ordered so each step is independently shippable and testable.

| # | Work item | Closes | Touches | Acceptance |
|---|---|---|---|---|
| W1 | Root entrypoint that chowns `/config` for `PUID`/`PGID` then `exec`s the binary as those ids | G2, `OD-D3` | `Dockerfile`, new `docker/entrypoint.sh` | UT-1 |
| W2 | Release workflow: tag-triggered, multi-arch, GHCR push, provenance/SBOM, `X.Y.Z`/`X.Y`/`X`/`latest`/`sha-<7>` | G5, `OD-D4`, `OD-D12` | `.github/workflows/release.yml` | §3.9 gates |
| W3 | Unraid template + icon + README deployment section | G6, `OD-D14` | `unraid/guildbank.xml`, `Docs/icon.png`, `README.md` | §9.6 UT-1…UT-6 |
| W4 | Read-only preflight/diagnostic mode (`-check-db`) reporting collation, privileges, `items` state, schema version | G7 | `internal/repository/mariadb`, `cmd/guildbank` | §5.4 |
| W5 | Setup wizard: prefill from environment, refuse mismatched completion, optional setup token | G1, `OD-D1`, `OD-D7` | `internal/service/setup.go`, templates | FR-1…FR-6 |
| W6 | Item seeder (`-seed-items`, `-seed-dry-run`), embedded artifact, `items_meta` | G9 | new `internal/seed` package, `cmd/guildbank` | IT-1…IT-7 |
| W7 | Grant-profile documentation and `items` read-only enforcement in the seed path | G8, `OD-D2` | `Docs/`, SQL runbook | MD-1…MD-6 |
| W8 | Backup/restore runbook in `README.md` + a rehearsal record | G3 | `README.md`, `Docs/` | BK-1…BK-7 |

Sequencing: W1, W2 and W3 make the image installable; W5 and W4 make the first run safe; W6
makes the app useful; W7 and W8 make it supportable. W1–W3 are prerequisites for any officer
testing.

## 14. Assumptions and limitations

| Assumption | If it does not hold |
|---|---|
| One GuildBank container per guild database | Two instances would fight over migrations only during startup; the advisory lock serialises them, but two web front-ends on one SQLite file is unsupported |
| SQLite and the container's `/config` live on the Unraid host | Network storage (NFS/SMB) for `/config` risks SQLite locking failures; not supported |
| MariaDB is on the same host or the LAN, without TLS | Cross-internet MariaDB is blocked on `OD-D5` (driver change) |
| TLS terminates at an existing reverse proxy | Without one, set `GUILDBANK_SECURE_COOKIES=false` and accept plaintext LAN-only access |
| The guild has a MariaDB administrator who can create a database and grants | Otherwise §6 cannot be executed and the app cannot start |
| The operator upgrades one minor at a time and takes backups | Nothing in the app prevents skipping, so the runbook is the only control |
| `items` is either the shipped seed or the operator's full dataset | Any other schema makes the app report metadata-missing for every row |

Known limitations carried forward, deliberately: no in-app self-update, no schema rollback, no
Settings-backed live pool (PD-18/E-11), no bundled MariaDB, no multi-tenant mode, no TLS to
MariaDB, no per-user data scoping beyond what the app already enforces.

---

## 15. Owner decisions

Each row is a decision the owner must make before the corresponding work ships. "Blocking"
means officer testing cannot start until it is settled.

| ID | Decision | Recommendation | Blocking? |
|---|---|---|---|
| `OD-D1` | Is the container environment the single source of truth for the MariaDB connection (and the wizard only mirrors it), or do we adopt Settings-backed connections now (PD-18/E-11)? | **Environment is the source of truth now**; Settings adoption stays deferred. Cheapest option that is correct, and it matches the code as built | **Yes** — without it the wizard can configure a database the process will not use |
| `OD-D2` | Do we split the runtime DML principal from the migration DDL principal (Profile B), or ship Profile A (one account with table-level grants)? | **Profile A for v1**, document its ALTER-on-`items` caveat; Profile B post-v1. Profile B is the only way to make `items` read-only at the server as well as in code | **Yes** — determines the grant runbook the tester follows |
| `OD-D3` | Accept a root entrypoint that chowns `/config` to `PUID`/`PGID` and drops privileges? | **Yes.** Without it a fresh Unraid install crash-loops (G2), and there is no operator-friendly alternative | **Yes** — nothing installs cleanly without it |
| `OD-D4` | Is `ghcr.io/panazule/guildbank` (public) the registry and namespace? | **Yes**, public read; publishing from Actions with `GITHUB_TOKEN` | No |
| `OD-D5` | Do we add TLS parameters to the MariaDB DSN (driver change) to allow a remote/cross-site database? | **No for v1**; require same host or LAN. Revisit if a guild needs an off-box database | No |
| `OD-D6` | Minimum supported MariaDB version: 10.6 floor, or 11.4 LTS only? | **11.4 LTS only** in the documented path; 10.6 remains technically compatible but untested | No |
| `OD-D7` | Add `GUILDBANK_SETUP_TOKEN` to gate `/setup` during the first-run exposure window? | **Yes.** A one-line check removes the race described in §4.6 | **Yes** — the exposure window otherwise stays open to whoever reaches the port first |
| `OD-D8` | Accept that changing the MariaDB target requires editing the environment and restarting (no in-app change)? | **Yes for v1**; it follows from `OD-D1` and is honest about the limitation | No |
| `OD-D9` | Default Unraid tag `X.Y` (patches auto, minors deliberate) rather than `latest`? | **Yes** — `latest` on Unraid can jump a minor unattended | No |
| `OD-D10` | Are Unraid auto-updates allowed, given they bypass the §10.2 backup step? | **Allowed but not enabled by default**; the help text states the backup requirement | No |
| `OD-D11` | Network mode and MariaDB host pattern: `bridge` + host IP, user-defined network + container name, or `host`? | **`bridge` + host IP**, with `3306` restricted to the host and the proxy | No |
| `OD-D12` | Sign images with keyless `cosign` in addition to provenance/SBOM? | **Yes, when the workflow exists**; optional for the first release | No |
| `OD-D13` | Is a read-only root filesystem with a writable `/tmp` required for v1? | **No** — hardening for a later release; it complicates the entrypoint | No |
| `OD-D14` | Pursue a Community Applications listing (needs icon, support URL, category), or publish the template in-repo only? | **In-repo first**, CA after the first stable minor | No |
| `OD-D15` | Is the operator's full 315-column `items` dataset a supported deployment (with its own backup obligation), or is the slim seed the only supported shape? | **Support both**, but document that the full dataset is the operator's to back up (§11.1) | No |
| `OD-D16` | Add a `guildbank-seed` one-shot service behind a Compose profile for `docker compose --profile seed up`? | **Yes** — it makes the documented seed step one command without ever running on container start | No |

### 15.1 What each blocking decision unblocks

| Blocking ID | Officer testing cannot start because… | Settling it produces |
|---|---|---|
| `OD-D1` | The wizard may configure a database the process ignores, so the first run is ambiguous | A wizard that pre-fills from the environment and refuses mismatches (W5) |
| `OD-D2` | The grant script cannot be written without knowing the principal model | A copy-pasteable SQL runbook (§6.4) the tester executes |
| `OD-D3` | A clean Unraid install cannot write `/config` | W1, after which UT-1 passes |
| `OD-D7` | Any LAN visitor can claim the administrator account during setup | W5's token gate, after which §4.6's window closes |

### 15.2 Decisions already made by this document (not owner questions)

For clarity, the following are design positions taken here rather than open questions:
single container with no bundled MariaDB; the app never creates the database; migrate-on-start
retained with `GET_LOCK`; item seeding only by explicit `-seed-items`; the item seed is slim
and embedded; `/readyz` gates readiness on both stores; tags follow `X.Y.Z`/`X.Y`/`X`/`latest`
/`edge`/`sha-<7>`; no schema rollback; backups exclude `items`.

