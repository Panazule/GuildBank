# Implementation Plan — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | IP-001 |
| Source specification | `Design.md` v1.6.1 (authoritative; decisions D-01–D-13 at L855–L1161, amendments AM-01–AM-05 at L1163–L1263) |
| Review date | 2026-11-20 |
| Revision | 1.2 — 2026-11-20, re-baselined on v1.6.1 |
| Companion documents | `ArchitectureReview.md`, `DatabaseDesign.md`, `ProjectStatus.md` |
| Current repository state | Greenfield — only `Design.md` and the `Docs/` set; no code, no build files |

This plan assumes the specification is amended per the decisions in `ArchitectureReview.md` §9 and the v1.6/v1.6.1 decision set. Estimates are ranges in engineer-days for one experienced Go developer familiar with Gin, SQLite and MariaDB; they exclude product-decision time and the external inputs in §8.1.

---

## 1. Delivery principles

1. **Decisions before code.** D-01–D-13 landed in v1.6 and AM-01–AM-05 in v1.6.1; Phase 0 now closes only the remaining contract gaps (E-14 warning sink, E-15 selection payload, E-09 audit taxonomy). Building around an unresolved contract creates rework in schema, API and UI simultaneously.
2. **Vertical slices, not horizontal layers.** Each phase produces a runnable application with a demonstrable workflow, not a layer with no consumer.
3. **Schema first within a slice.** Migration → repository → service → API → UI, in that order, so the UI never invents data shapes.
4. **Security controls land with the feature they protect.** D-12's CSRF, cookie and rate-limit controls arrive with the first form, not in a later hardening phase.
5. **Fixtures are part of the deliverable.** Blade of Carnage decoding, the D-04 sample, D-05 location cases, merge and instance cases, and import files are committed test data.
6. **No external network in tests.** EQResource is never called; its base URL is a stored string (L784–L795).
7. **Every phase ends deployable.** `main` always builds, migrates and serves something; no long-lived feature branches.

---

## 2. Phase overview

| Phase | Name | Primary outcomes | Depends on | Estimate |
|---|---|---|---|---|
| 0 | Specification closure | D-01–D-13 and AM-01–AM-05 adopted; remaining E-09/E-14/E-15 closed; two real exports added | — | 2–4 |
| 1 | Platform skeleton | Buildable Go service, package boundaries, config, logging, Docker, CI | Phase 0 (partial) | 3–5 |
| 2 | Setup, identity, sessions | First-run wizard, login/logout, admin session, CSRF, rate limiting | Phase 1, D-02, D-09, D-12 | 6–9 |
| 3 | Guild store and audit | MariaDB connection, migrations, settings admin, audit recorder + outbox | Phase 2, D-10, D-13 | 6–8 |
| 4 | Public inventory read | Item decoding, natural sort keys, public list/detail, filters, pagination | Phase 3, D-01, D-12 | 7–10 |
| 5 | Inventory administration | Manual add/update/soft delete, audit events, admin grid | Phase 4, D-06, D-07 | 5–7 |
| 6 | Import pipeline | Upload, parse, normalize, stage, review/selection, atomic commit | Phase 5, D-04, D-05, D-06, D-07, D-08, D-11 | 10–14 |
| 7 | Audit, users, dashboard | Audit UI/API, user management, dashboard tiles, hardening | Phase 6 | 5–7 |
| 8 | Release readiness | Load/security tests, backups, Unraid template, release checklist | Phase 7 | 4–6 |
| **Total** | | | | **48–70** |

Critical path: Phase 0 → 2 → 3 → 4 → 6 → 8. Phases 5 and 7 have slack.

---

## 3. Phase plans

### Phase 0 — Specification closure

**Goal:** eliminate ambiguity that would otherwise be resolved differently in schema, API and UI.

**Tasks**
1. Record the v1.6 decisions (D-01–D-13) and v1.6.1 amendments (AM-01–AM-05) as adopted ADRs, and the residual decisions E-14–E-16 as short ADRs under `Docs/decisions/`.
2. Resolve the three residual contracts: warning sink (E-14/MR-22), selection payload (E-15/DC-22), audit taxonomy closure (E-09/DC-25).
3. Add two real collector exports alongside the canonical sample at `Docs/Samples/InventoryExport-Sample.txt`, whose format, encoding and row rules are fixed by AM-03; commit the repository (currently no commits) so the sample and fixtures are tracked.
4. Confirm `Design.md` v1.6.1 is published: title/version corrected, `Hoard` standardized, AM-01–AM-05 appended, change log present.
5. Re-derive `DatabaseDesign.md` §12 against the approved decisions and amendments.

**Deliverables:** ADR set; two real export fixtures plus the documented format; corrected specification.

**Exit criteria**
- No open item from `ArchitectureReview.md` §1.2 or §12 remains without a recorded resolution.
- Every **PROPOSED** column in `DatabaseDesign.md` is either approved or removed; `ItemType` is promoted.
- Import parser requirements are testable from the documented sample alone (no tribal knowledge).
- The missing-metadata rule is the signed AM-01/AM-02 rule (preserved `EQUID`, `MetadataMissingFg`, no sentinel).

**Risks:** product decisions stall; mitigation — timebox each residual decision to one working session using the AR §9 recommendations. The sample is already at its declared path; obtaining two real exports is the remaining external input.

---

### Phase 1 — Platform skeleton

**Goal:** a deployable service with the mandated layering and no business features.

**Tasks**
1. `go mod init` with the chosen module path; pin Go version and dependencies (Gin, SQL drivers, bcrypt, migration library, slog).
2. Package layout: `cmd/guildbank`, `internal/domain`, `internal/service`, `internal/repository/{sqlite,mariadb}`, `internal/api`, `internal/web`, `internal/platform`.
3. Configuration loader: flags/env for port, SQLite path (`/config/guildbank.db`), log level; no secrets in code.
4. Structured logging (`slog`, JSON), request IDs, panic recovery, access log middleware.
5. `/healthz` (process alive) and `/readyz` (SQLite open, MariaDB reachable once configured).
6. Embedded templates and static assets (`embed.FS`); Bootstrap 5 and HTMX served locally, not from a CDN.
7. Multi-stage Dockerfile (non-root, static binary, `EXPOSE 8080`, volume `/config`), `.dockerignore`, `Makefile`/task runner.
8. CI: `go build ./...`, `go vet`, unit tests, race tests, lint, Docker build.
9. Import-graph guard proving `internal/web` and `internal/api` cannot import `internal/repository`.

**Acceptance criteria**
- `docker run` starts and serves `/healthz` with `200` and a JSON body.
- `/readyz` reports `503` before setup and `200` after a valid configuration.
- A deliberate import of a repository package from the web layer fails CI.
- Logs are JSON, include request ID and method/path/status, and contain no secrets.
- Container runs as UID ≠ 0 and writes only to `/config` and `/tmp`.

**Tests** — table-driven config parsing; middleware access-log/request-ID; health endpoints; import-graph lint.

---

### Phase 2 — Setup, identity and sessions

**Goal:** a first-run wizard that produces a working administrator and session, with security controls in place from the first form.

**Tasks**
1. SQLite migrations for `Users`, `Settings`, `Sessions`, `AuditOutbox`; `PRAGMA user_version` handling.
2. Setup gate: serve setup only while `Users` contains no administrator (D-02); other requests redirect to `/setup`; once an administrator exists, `/setup` and all setup routes return `410`. `SetupComplete` is telemetry, not a gate.
3. Wizard Steps 1–4 (L138–L164): collect admin credentials, guild name, MariaDB connection test, then commit the administrator and DB settings together so setup cannot close before configuration succeeds (D-02, DC-24). Server-side validation, idempotent restart, no partial state.
4. Session implementation: random 32-byte token, distinct `SessionID` and `TokenHash` columns, idle + absolute expiry, `HttpOnly; Secure` when HTTPS, `SameSite=Lax`, logout, revoke-all, per-request `DisabledFg` check (D-09).
5. Auth middleware (default deny for admin routes), CSRF middleware for all state-changing routes (including setup and login), login rate limiter (IP + username).
6. Login/logout pages and handlers; generic failure messages; `LastLogin` update; audit via outbox (`UserName = 'system'` until MariaDB exists).
7. Session purge task and `LastAccessed` write throttling.
8. Setup tests: fresh volume, interrupted Step 3, replay after completion, concurrent setup attempts.

**Acceptance criteria**
- A fresh `/config` produces a working admin in four steps; killing the process after the connection test resumes without duplicate admins.
- Login with a wrong password returns the same message and status as an unknown username.
- A request without a valid session to any admin route returns `401` (API) or redirects to login (HTML).
- The cookie is `HttpOnly`, `Secure` (behind HTTPS), `SameSite=Lax`; only `TokenHash` is stored, never a raw token or a value the browser could replay.
- Disabling a user invalidates their sessions on the next request.
- CSRF rejection returns `403` and no state change, including on login and setup requests.
- The rate limiter blocks the fifth failed attempt and returns a documented `429`/lockout response.

**Tests** — setup state machine, bcrypt round trip, session expiry (clock injection), CSRF, rate limiter, default-deny route matrix.

---

### Phase 3 — Guild store, settings and audit foundation

**Goal:** a working MariaDB integration, an audited settings surface, and the audit pipeline that every later write depends on.

**Tasks**
1. MariaDB pool built from `Settings` values; connect timeout, `Ping` on startup, hot-swap on settings change (E-11). Pool configured with explicit max open/idle and connection lifetime.
2. MariaDB migrations: `GuildBankItems`, `ImportBatch`, `ImportList`, `Audit` (the D-10 field set), `schema_migrations`; `GET_LOCK` guard; no destructive steps.
3. `Items` loader contract documented and a development seed/fixture provided (not production data). Application queries only the contract columns; `SELECT *` prohibited.
4. Settings service: typed accessors, validation ranges, cache with invalidation, `GET /api/settings` (secrets redacted) and `PUT /api/settings`.
5. Audit recorder in the service layer: closed `EventType` allowlist built from D-10, `OldValues`/`NewValues` snapshots, `TableName`/`RecordID`, `UserID`/`UserName` snapshots, redaction of `PasswordHash` and `IsSecret`, correlation ID, insert-only repository.
6. `AuditOutbox` drain worker for SQLite-originated events; retry with backoff; metrics/logging of lag.
7. `/api/audit` read endpoint (paginated, filterable) — no UI yet.
8. Failure-mode tests: MariaDB unavailable at startup, dropped connection mid-request, invalid connection settings on update.

**Acceptance criteria**
- With MariaDB down, login and settings reads still work; inventory screens show a clear unavailable state and `/readyz` returns `503`.
- `GET /api/settings` never returns the MariaDB password, even to an administrator.
- Invalid ranges (`GuildOpenDays = -1`, port `0`) are rejected with `422` and unchanged data.
- A settings change writes exactly one audit record and one outbox row; the outbox drains exactly once in the happy path.
- `Audit` rows contain no password hash or DB password (verified by a test that scans serialized payloads).
- A failed migration leaves the schema version unchanged and the process exits non-zero.

**Tests** — repository integration against a MariaDB container; settings validation table; redaction; outbox retry/duplication; audit taxonomy mapping.

---

### Phase 4 — Public inventory read path

**Goal:** the public inventory experience, backed by decoded data and a stable query contract.

**Tasks**
1. Domain decoder: class and race masks, `ALL`, unknown-bit handling, canonical ordering; Blade of Carnage fixture test; property tests for round-trip encoding.
2. Natural sort key function shared by inventory and locations; golden table from L699–L728, including every D-05 normalization output (`General 5`, `Bank 4`, `SharedBank 2`, `Hoard`).
3. Repository queries: active inventory list, detail by ID, filtered count, pagination; explicit column projections (no `SELECT *`).
4. `GET /api/items` and `GET /api/items/{id}` with documented query parameters (`search`, `class`, `race`, `itemtype`, `sort`, `page`, `pageSize`) and a paginated envelope.
5. Public UI: inventory list, filters, sorting, pagination, empty state, item detail with decoded class/race, dates and quantity; server-rendered HTMX fragments for filter/sort changes.
6. EQResource link construction from the validated base URL, with escaping and `target="_blank" rel="noopener noreferrer"`.
7. Availability presentation: define and test the comparison (`DateOpen <= today`, `DateBazaar >= today` per E-10) and label states consistently.
8. Public/admin projection separation so `DeletedFg`, `ModBy` and `ModByUserID` never serialize publicly.

**Acceptance criteria**
- Blade of Carnage decodes to `WAR,PAL,RNG,SHD,BRD` and `ALL` (L512–L529).
- Location selector order matches `General 1, General 2, General 3, General 10, General 11` (L699–L728).
- Filters combine correctly; `ALL` returns every row regardless of stored string.
- Pagination is deterministic across pages and consistent with the total count.
- Soft-deleted rows are absent from list, detail, search and counts.
- The API rejects unknown sort keys with `422` and caps `pageSize`.
- No request is made to EQResource during any test.

**Tests** — decoder tables, sort-key tables, query integration with seeded data, API contract tests, template rendering snapshots, accessibility smoke (labels, keyboard order).

---

### Phase 5 — Inventory administration

**Goal:** administrators can add, update, remove and restore inventory with full lifecycle rules and audit.

**Tasks**
1. `InventoryService.Create`: resolve `Items` metadata by `Items.ID` (D-01), decode masks, compute `DateAdded`/`DateOpen`/`DateBazaar`; item input expands to one row per unit (D-07); spells merge on `(ItemType='Spell', EQUID)` and never change dates (D-06).
2. `InventoryService.Update`: mutable fields only (`Quantity`, `GivenTo`, `SourceLocation`, `DeletedFg`); immutable fields rejected with a clear message.
3. `InventoryService.Remove`/`Restore`: soft delete and restore, both audited.
4. Admin inventory grid: search, filter, sort, pagination, row actions; add/edit forms with server-side validation and HTMX partial refresh.
5. `POST /api/items`, `PUT /api/items/{id}`, `DELETE /api/items/{id}` wired through the same service; no repository access in handlers.
6. Concurrency: detect lost updates with `ModAt` precondition (optimistic check) and return `409` on conflict.
7. Audit events: map to D-10's names (`ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED`), with restore recorded as an approved extension or an `ITEM_UPDATED` with `Notes`, and redacted change sets.
8. Validation fixtures: item with `ALL` restrictions, item with quantity > 1 (rejected), spell with quantity > 1 (accepted), missing `EQUID`.

**Acceptance criteria**
- Adding an ordinary item with quantity 2 creates two active rows, each `Quantity = 1`; adding a spell with quantity 2 creates or merges one row with quantity 2.
- `DateOpen = DateAdded + GuildOpenDays` and `DateBazaar = DateAdded + BazaarDays` for every new row.
- An update attempt to change `Name`/`DateAdded` is rejected without a partial write.
- Removing an item hides it from public and admin active views but preserves the row and its audit history.
- Restoring returns it to active views with a new `ModAt` and an audit entry.
- Stale `ModAt` update returns `409` and does not overwrite newer data.
- Every mutation creates exactly one audit row.

**Tests** — service unit tests with fake repositories; integration tests for transactions; handler contract tests; form validation table.

---

### Phase 6 — Import pipeline

**Goal:** upload a collector export, review and adjust selections, and commit atomically with no partial inventory.

**Tasks**
1. Parser for the format fixed by D-04 and AM-03: validate against the sample at `Docs/Samples/InventoryExport-Sample.txt` (tab-delimited header `Location/Name/ID/Count/Slots`, UTF-8); the parser test fails if the fixture is absent. Skip blank lines and section headers; default `Count` to 1 when trailing fields are omitted; treat a non-numeric fourth field as `Slots`; header validation, streaming rows with a row limit, duplicate header detection, clear per-row error codes.
2. Row validation (L615–L630): `ID = 0` and `Name = empty` rejection; `Quantity` bounds; unknown/blank location handling per the approved policy.
3. Classification: spell prefixes (`Spell:`, `Song:`, `Ancient:`, plus approved additions) feed `ItemType` in staging (DC-09, DC-18).
4. Location normalization: single pure function implementing D-05 (`Area n-Slot m` → `Area n`; `Hoard n` → `Hoard`), with table-driven tests for every spec example and an explicit unknown-format policy (E-06/DC-20); `LocationSort` generated here.
5. Staging transaction for `ImportBatch` + `ImportList`; `Status = Failed` with `FailureReason` on parse errors (MR-12); optional duplicate-upload warning via hash.
6. Review UI: import list screen, batch detail with location selector, per-row and bulk selection, dynamic summary counts, HTMX updates.
7. Selection API (D-11): `PUT /api/imports/{id}/selection` accepting the full `ImportID` set for the batch (E-15); plus `GET /api/imports/{id}/summary`. Both validate batch ownership and terminal status; foreign IDs return `422`.
8. Commit service implementing `DatabaseDesign.md` §7.5: row lock, status guard, item instance expansion (D-07), spell merge on `(ItemType='Spell', EQUID)` with unchanged dates (D-06), unresolved metadata preserved with `MetadataMissingFg` and a warning, never `-1` (AM-01/AM-02), audit, single transaction, `409` on re-commit.
9. Import history screen: batches, statuses, counts, failure reasons; staging rows retained.
10. Failure injection tests: disconnect during commit, duplicate commit, parse failure mid-file, deselected rows.

**Acceptance criteria**
- A golden two-row file produces exactly the expected inventory rows and dates.
- A file violating the format (bad header, zero ID, empty name) fails with a specific `FailureReason` and zero inventory changes.
- `General 5-Slot1`, `General 5-Slot6`, `General 5-Slot8` normalize to a single `General 5` group; `Hoard1`, `Hoard2`, `Hoard37`, `Hoard145` normalize to `Hoard` (D-05; `Hoard` is the single spelling per AM-04).
- Deselecting a location excludes all its rows from commit but leaves them staged and visible; the selection endpoint stores row-level flags only.
- Two concurrent commits on the same batch produce one success and one `409`.
- A failure injected mid-commit leaves `GuildBankItems` unchanged and the batch in a recoverable state, documented per DC-16.
- Commit of a batch with 0 selected rows is rejected with a clear message.
- An unresolved `EQUID` is imported with a visible warning, its original identifier preserved and `MetadataMissingFg` set (AM-01/AM-02).

**Tests** — parser fixtures (happy/failed/encoding), normalizer tables, classifier, staging integration, commit transaction tests, concurrency test, API contract tests, UI snapshot tests.

---

### Phase 7 — Audit, user management, dashboard and hardening

**Goal:** complete the remaining admin screens and close the security gaps that only apply once the full surface exists.

**Tasks**
1. Audit UI: paginated list, filters (date range, actor, action, entity), detail view with redacted before/after, no mutation controls.
2. User management: list, create, disable/enable, password change/reset, last-admin protection, session revocation, audit events.
3. Dashboard: define tiles (active items, active spells, quantity totals, items opening/bazaar-ready windows, recent imports, recent audit). Each tile is a named query with a test; no inventing metrics outside the data model.
4. Security headers: CSP without inline scripts (HTMX uses external files), `X-Content-Type-Options`, `Referrer-Policy`, HSTS behind HTTPS, frame denial.
5. Login and setup rate limits verified under concurrency; public API rate limiting.
6. Error handling: uniform JSON error envelope for `/api`, friendly HTML error pages, no stack traces or driver errors to clients.
7. Accessibility and responsiveness pass on all screens (labels, focus order, contrast, mobile layout).
8. Documentation set: administrator guide, backup/restore, API reference generated from the route table.

**Acceptance criteria**
- Audit screen shows every event type with filters; a test asserts no secret values appear in rendered detail.
- A user cannot disable their own last-admin account; the attempt returns a clear error.
- Creating a user without a password policy-compliant password is rejected; disabling revokes sessions immediately.
- Dashboard tiles match repository counts on a seeded dataset.
- Security headers present on every HTML response; CSP violations absent in a manual browser check.
- API errors return the documented envelope and status codes; HTML errors never reveal internal paths.
- All screens usable at 375 px width without horizontal scrolling of primary content.

**Tests** — audit rendering redaction, user-management rules, dashboard query fixtures, header assertions, error-contract tests.

---

### Phase 8 — Release readiness

**Goal:** prove the system is operable on Unraid and resilient under realistic load.

**Tasks**
1. Performance test on a seeded dataset (100k inventory rows, 50k staging rows): list, filter, search, count, audit query, import commit of a 5k-row file. Record p50/p95 and slow-query logs.
2. Load test concurrent sessions (e.g., 50 users browsing, 3 admins importing) and verify SQLite `busy_timeout` behavior.
3. Security review against `ArchitectureReview.md` §6: verify each SR has an implemented control or an accepted, documented risk.
4. Backup/restore rehearsal: dump both stores, restore into a clean volume, verify login, inventory, imports, audit.
5. Migration rehearsal: install previous release, upgrade, confirm idempotent startup and no data loss.
6. Unraid template: port, `/config` mapping, MariaDB dependency notes, icon, description; sample `docker-compose.yml` for non-Unraid users.
7. Image hygiene: non-root, read-only root FS compatibility, pinned base image digest, vulnerability scan with no unresolved criticals.
8. Release checklist, version tag, changelog, and known-limitations section.

**Acceptance criteria**
- p95 for the public list under 300 ms on the seeded dataset on reference hardware; import commit of 5k rows under 10 s; both recorded.
- Restore rehearsal succeeds with zero manual SQL.
- Upgrade rehearsal preserves users, sessions (or cleanly invalidates them), inventory, imports and audit.
- No unresolved critical vulnerability in the image scan.
- A new operator can deploy from the template and complete setup using only the documentation.
- Every SR finding has a control reference or a signed risk acceptance.

**Tests** — load scripts, migration tests against a copy of the previous schema, restore verification script, container smoke test.

---

## 4. API surface

The specification lists the resource endpoints (L736–L771) but not their contracts (IC-01). This is the complete route table the implementation should build. Versioned paths are recommended (E-12); the specification's unversioned paths can be aliased during the transition.

| Method | Path | Access | Purpose | Notes |
|---|---|---|---|---|
| GET | `/healthz` | public | Liveness | Always `200` while the process runs |
| GET | `/readyz` | public | Readiness | `503` until setup complete and MariaDB reachable |
| GET | `/setup` | setup-only | Wizard entry | `410` once an administrator exists (D-02) |
| POST | `/setup/admin` | setup-only | Create first administrator | Idempotent by username |
| POST | `/setup/guild` | setup-only | Guild name | Stored in SQLite |
| POST | `/setup/database/test` | setup-only | Validate MariaDB connection | No persistence on failure |
| POST | `/setup/complete` | setup-only | Persist DB settings, set complete | Single SQLite transaction |
| POST | `/api/v1/auth/login` | public (rate-limited) | Authenticate | Sets session cookie; generic failures |
| POST | `/api/v1/auth/logout` | authenticated | End session | Deletes/revokes row, clears cookie |
| GET | `/api/v1/auth/session` | authenticated | Current user and expiry | Used by HTMX bootstrap |
| GET | `/api/v1/items` | public | List inventory | `search`, `class`, `race`, `itemType`, `sort`, `page`, `pageSize` |
| GET | `/api/v1/items/{id}` | public | Item detail | Public projection; admin projection on admin route |
| POST | `/api/v1/items` | admin | Add inventory | Service validates `EQUID` and lifecycle |
| PUT | `/api/v1/items/{id}` | admin | Update mutable fields | `ModAt` precondition |
| DELETE | `/api/v1/items/{id}` | admin | Soft delete | Never SQL `DELETE` |
| GET | `/api/v1/imports` | admin | Import history | Paginated, status filter |
| POST | `/api/v1/imports` | admin | Upload and stage | Multipart file; size/type limits |
| GET | `/api/v1/imports/{id}` | admin | Batch detail with staging rows | Paginated; location filter |
| PUT | `/api/v1/imports/{id}/selection` | admin | Persist `SelectedFg` for the batch | **D-11**; full `ImportID` set; `422` for foreign IDs |
| GET | `/api/v1/imports/{id}/summary` | admin | Counts by location, items, spells | **Proposed** (MR-03) |
| POST | `/api/v1/imports/{id}/commit` | admin | Commit selected rows | Atomic; `409` if terminal/in progress |
| GET | `/api/v1/users` | admin | List users | Never returns hashes |
| POST | `/api/v1/users` | admin | Create user | Password policy applied |
| PUT | `/api/v1/users/{id}` | admin | Update role/status/password | Last-admin protection |
| DELETE | `/api/v1/users/{id}` | admin | Disable user | Documented soft behaviour (DC-11) |
| GET | `/api/v1/settings` | admin | Read settings | Secrets redacted |
| PUT | `/api/v1/settings` | admin | Update settings | Validation + audit + cache invalidation |
| GET | `/api/v1/audit` | admin | Audit search | Paginated; filters; read-only |
| GET | `/` | public | Public inventory screen | Server-rendered |
| GET | `/admin/{screen}` | admin | Six admin screens (L805–L825) | Dashboard, inventory, import, settings, users, audit |

**Response conventions**

- Success: `{"data": ..., "meta": {"page": 1, "pageSize": 50, "total": 123}}`.
- Error: `{"error": {"code": "validation_failed", "message": "...", "fields": {"quantity": "must be >= 1"}}}`.
- Status mapping: `400` malformed, `401` unauthenticated, `403` unauthorized/CSRF, `404` missing, `409` conflict/terminal state, `422` validation, `429` rate limited, `500` unexpected, `503` dependency unavailable.
- HTML routes redirect to `/login?next=…` on `401` and render an error page on `5xx`; HTMX requests receive the same status with a fragment when appropriate (IC-20).

---

## 5. Audit event taxonomy (D-10)

D-10 fixes ten event types and calls them examples. Until E-09 closes the allowlist, the implementation treats these as the **minimum mandatory set** and records any extension as a decision; the repository must expose no free-text `EventType`.

| `EventType` | `TableName` | Trigger | Notes |
|---|---|---|---|
| `LOGIN` | `Users` (logical) | Successful authentication | `UserID`/`UserName` snapshot; never store password material |
| `LOGOUT` | `Users` (logical) | Explicit logout | Logout is state-changing and must be CSRF-protected |
| `IMPORT_CREATED` | `ImportBatch` | Upload staged with `Status='Uploaded'` | Row counts in `NewValues` |
| `IMPORT_COMMITTED` | `ImportBatch` | Successful commit | Accepted counts and resulting inventory counts |
| `ITEM_CREATED` | `GuildBankItems` | Manual add or import commit | One event per logical operation; `RecordID` may reference a batch summary |
| `ITEM_UPDATED` | `GuildBankItems` | Mutable-field change or restore | `OldValues`/`NewValues` hold changed fields only |
| `ITEM_REMOVED` | `GuildBankItems` | Soft delete | `DeletedFg` transition only |
| `USER_CREATED` | `Users` (logical) | Administrator creates a user | `TableName` is a label, not a MariaDB FK |
| `USER_UPDATED` | `Users` (logical) | Role/status/password change | bcrypt hash never serialized |
| `USER_DISABLED` | `Users` (logical) | Deactivation | Session revocation recorded in `Notes` |
| `SETTINGS_UPDATED` | `Settings` (logical) | Any settings change | Secret values redacted |

Extension candidates that require E-09 approval before use: `LOGIN_FAILED`, `IMPORT_FAILED`, `IMPORT_SELECTION_UPDATED`, `ITEM_RESTORED`, `SYSTEM_SETUP_COMPLETE`.

Rules: one row per logical operation; `EventDate` in UTC written by the application; `OldValues`/`NewValues` contain only changed fields and never `PasswordHash`, `MariaDbPassword` or file contents; every event carries a correlation ID when available; audit is insert-only (D-10).

---

## 6. Test strategy

### 6.1 Layers and ownership

| Layer | Scope | Tooling | Gate |
|---|---|---|---|
| Unit | Decoders, sort keys, location normalization, date math, validators, settings types | `go test`, table-driven tests | Every commit |
| Repository integration | SQL, constraints, indexes, transactions, soft-delete scoping | Real SQLite temp file; MariaDB via container (`testcontainers` or Docker Compose in CI) | Every commit |
| Service | Orchestration, audit emission, cross-store ordering, idempotency | Fakes for repositories + integration variants | Every commit |
| API contract | Route table, status codes, error envelope, auth matrix, pagination | `httptest` against the assembled router | Every commit |
| UI | Rendered HTML, HTMX fragments, form validation display, accessibility basics | `httptest` golden files; optional headless browser for flows | Every commit (fast), nightly (browser) |
| Security | CSRF, session fixation/expiry, rate limits, redaction, headers, SQL injection attempts | Go tests + targeted scripts | Every commit |
| Performance | Seed 100k rows; list/search/filter/commit timings | Benchmark scripts; nightly job | Before release |
| Recovery | Backup/restore, migration upgrade, MariaDB outage | Scripted rehearsal | Before release |

### 6.2 Mandatory fixtures (committed, not generated at test time)

1. **Decoder:** `ClassList=157` → `WAR,PAL,RNG,SHD,BRD`, `RaceList=65535` → `ALL` (Blade of Carnage, L512–L529); `3` → `WAR,CLR`; `5` → `Human,Erudite`; `0`; unknown-bit mask.
2. **Location normalization:** `General 5-Slot1/6/8` → `General 5`; `Bank4-Slot1/6` → `Bank 4`; `SharedBank2-Slot6` → `SharedBank 2`; `Hoard1/2/37/145` → `Hoard`; unknown format; mixed case with extra spaces (D-05; body L569–L605).
3. **Import files:** the canonical sample (`Docs/Samples/InventoryExport-Sample.txt`; tab-delimited header `Location/Name/ID/Count/Slots`, 548 rows) as the golden parse; a valid items-only file, a valid spells-only file, a mixed file, a malformed header, a zero-`ID` row, an empty-`Name` row, a row whose `EQUID` is absent from `Items`, a non-UTF-8 encoding sample, an oversized-file test descriptor.
4. **Merge and instance rules (D-06/D-07):** an import with two units of one item → two rows; a spell with quantity 5 then 3 → one row quantity 8 with unchanged dates; a spell row whose `EQUID` is staged twice in one file.
5. **Selection payloads (D-11):** full set, empty set, an `ImportID` from another batch, and a terminal batch.
6. **Settings:** valid, boundary (`0`, max), invalid (negative, non-integer, invalid URL) payloads.
7. **Codec/state:** seeded dataset (100k inventory rows, 50k staging rows) generated by a deterministic script.

### 6.3 Coverage and quality gates

- Domain package: ≥ 90 % statement coverage, including every bit position and every normalization branch.
- Service package: ≥ 80 %; every audit action has at least one test asserting the emitted event.
- No test may call EQResource or any external network.
- Race detector on all packages in CI (`go test -race ./...`).
- Flaky tests are quarantined and fixed; retries are not permitted in the main gate.
- Golden-file updates require an explicit review comment.

### 6.4 Manual QA checklist (per release)

- Fresh install on a clean volume; setup with wrong DB credentials then correct ones.
- Login/logout, expired session, disabled user, second browser session.
- Add item, edit given-to, remove, restore, verify audit trail.
- Import valid file, deselect a location, commit, verify counts and dates; re-commit blocked.
- Public list: search, class/race filters, sort, pagination, deep link, EQResource link opens in a new tab.
- Mobile viewport check on public and admin screens.
- Backup, restore into a clean volume, verify data and login.

---

## 7. Deployment plan

### 7.1 Runtime topology

```
Unraid host
├── guildbank container (Go binary, non-root, port 8080)
│   └── /config  →  guildbank.db (+ -wal/-shm)
└── mariadb container (existing or managed)
    └── database: GuildBank
```

The application is stateless apart from `/config` and MariaDB; it can be recreated at any time. TLS terminates at the Unraid reverse proxy; the application must not assume `X-Forwarded-Proto` unless the proxy is explicitly trusted (SR-11).

### 7.2 Configuration

| Variable | Default | Purpose |
|---|---|---|
| `GUILDBANK_PORT` | `8080` | Listen port |
| `GUILDBANK_SQLITE_PATH` | `/config/guildbank.db` | SQLite file |
| `GUILDBANK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `GUILDBANK_TRUSTED_PROXIES` | *(empty)* | CIDRs allowed to set forwarded headers |
| `GUILDBANK_SESSION_KEY` | *(generated and stored in SQLite if absent)* | Optional override for token hashing pepper |

MariaDB credentials and guild settings are configured through the wizard and stored in SQLite (L161–L164); they are not environment variables by default (E-11 records whether that changes).

### 7.3 Startup and migration

1. Ensure `/config` exists and is writable; fail fast with a clear log if not.
2. Open SQLite, apply migrations, log `user_version`.
3. Load settings; if the `Users` table has no administrator, serve only setup routes (D-02).
4. Connect to MariaDB with a bounded timeout; apply migrations under an advisory lock.
5. Start the HTTP server, outbox drain, and session purge tasks.
6. On `SIGTERM`: stop accepting connections, finish in-flight requests (≤ 10 s), checkpoint WAL, close pools.

Rollback: redeploy the previous image. Migrations are forward-only, so a rollback that crosses a schema change requires restoring the pre-upgrade backup; this is documented in the release notes.

### 7.4 Unraid template fields

- Name/description/icon; category Tools.
- Network: bridge, port `8080 → 8080`.
- Path: `/mnt/user/appdata/guildbank/config → /config`.
- Extra: MariaDB host/port documented; no MariaDB credentials in the template.
- Healthcheck: `GET /healthz`.

### 7.5 Observability and support

- JSON logs to stdout with request ID, route, status, duration, actor (not passwords).
- `/readyz` reports SQLite and MariaDB separately with a short reason.
- Document a support bundle procedure: version, `user_version`, MariaDB schema version, redacted settings, recent log excerpt.
- No metrics endpoint in v1; structured logs are the source of truth. A Prometheus endpoint is a post-v1 enhancement.

---

## 8. Dependencies, estimates and sequencing

### 8.1 External dependencies

| Dependency | Needed by | Owner | Risk if late |
|---|---|---|---|
| v1.6 decisions and v1.6.1 amendments adopted (D-01–D-13, AM-01–AM-05) | Phase 0 | Product owner | Remaining soft gaps stay open; rework |
| Two real collector export files alongside the canonical sample at the D-04 path | Phase 6 (ideally Phase 0) | Guild administrator | Parser cannot be proven against real data; format assumptions persist |
| Populated `Items` table + loader procedure | Phase 3/4 | Operator / loader owner | Import decoding and manual add cannot run end to end |
| MariaDB instance (test + production-like) | Phase 3 | Operator | No integration testing; schema unverified |
| Unraid host or compatible Docker host | Phase 8 | Operator | Deployment rehearsal deferred, not substitutable |
| EQ rules confirmation (spell prefixes, availability meaning, races/classes) | Phase 0/4 | Domain expert | Classification and UI semantics remain guesses |

### 8.2 Estimate breakdown and contingency

| Phase | Estimate (engineer-days) | Confidence | Notes |
|---|---|---|---|
| 0 | 2–4 | High | Decisions and amendments adopted; residual contracts and two real exports remain |
| 1 | 3–5 | High | Standard scaffolding |
| 2 | 6–9 | Medium | Setup state machine is the hidden cost |
| 3 | 6–8 | Medium | MariaDB integration + audit pipeline |
| 4 | 7–10 | Medium | Decoder is small; query/UI contract is not |
| 5 | 5–7 | High | Reuses Phase 4 patterns |
| 6 | 10–14 | Low | Parser variability and normalization remain the main unknowns; real exports still absent |
| 7 | 5–7 | Medium | Breadth over depth |
| 8 | 4–6 | Low | Performance tuning and rehearsals can expand |
| **Subtotal** | **48–70** | | Add 20 % contingency → **58–84** |

### 8.3 Two-developer option

After Phase 4, split: Developer A owns Phases 5 and 6 (inventory + import server-side); Developer B owns Phase 7 (audit/users/dashboard UI) and Phase 8 tooling. Shared contracts are the reason Phase 4 must precede the split: both need the same repository projections and error envelope.

---

## 9. Definition of done

A task is complete only when all of the following are true:

1. Code reviewed and merged to `main`; no direct commits.
2. Unit and integration tests cover the happy path, boundary conditions and one failure mode.
3. Schema changes ship as a forward-only migration with a rollback note.
4. Every write path emits the correct audit event, verified by a test.
5. Authorization is enforced server-side; the route is present in the auth matrix test.
6. User-facing text is escaped; no string concatenation into SQL identifiers.
7. Documentation updated: API reference, admin guide, or decision record as applicable.
8. Logs contain no secrets, hashes or session tokens; errors use the standard envelope.
9. Accessibility basics verified (labels, focus, keyboard).
10. Changelog entry written; version impact recorded.
11. Deployment smoke test passes on a clean volume.
12. Known limitations recorded in the release notes rather than left implicit.
13. Import parsing is validated against the committed D-04 sample; a missing or changed sample fails CI.

---

## 10. Risk register

| Risk | Likelihood | Impact | Mitigation | Trigger to act |
|---|---|---|---|---|
| Residual contracts stall (E-14/E-15/E-09) | Medium | High | Timebox Phase 0; AR §9 recommendations pre-agreed | Any residual open > 1 week |
| Import format diverges from real exports (D-04/AM-03) | Medium | High | Add two real files before Phase 6; parser behind an interface | Fixture missing or parsing failure > 1 % |
| Unresolved metadata never revalidated (AM-01/AM-02) | Medium | Medium | Implement the `MetadataMissingFg` refresh job and audit the transition to resolved | Flagged rows older than one `Items` refresh cycle |
| `Items` table unpopulated or stale | Medium | High | Provision loader before Phase 3 integration; record metadata coverage | Any test needing real item data fails |
| Spell classification wrong (scrolls vs knowledge) | Medium | Medium | Confirm prefixes and semantics in Phase 0; make classifier data-driven | Domain expert disputes fixture output |
| SQLite lock contention under concurrent admins | Medium | Medium | WAL, single writer, busy timeout, touch throttling; load test | `database is locked` in logs |
| MariaDB outage blocks inventory/audit | Medium | Medium | Read-only auth/settings; outbox for SQLite-origin audit; clear 503 UX | Repeated connection failures |
| Secret handling (DB password) inadequate | Medium | High | Decide E-11 in Phase 0; restrict volume permissions; redaction tests | Secrets found in logs/API output |
| Cross-store inconsistency | Medium | Medium | Documented ordering, snapshots, outbox, no distributed claims | Audit missing for a successful write |
| Natural sort/pagination mismatch | Low | Medium | Persisted sort keys + deterministic tiebreak; golden tests | Page overlap/duplicates observed |
| Scope creep (future enhancements pulled into v1) | Medium | Medium | Explicit non-goals; changelog gate | New endpoint/table without decision record |
| Single-maintainer bus factor | High | Medium | Documentation set, ADRs, tests as specification | Any undocumented operational step |

---

## 11. Traceability to specification capabilities

| Specification capability | Lines | Delivered in |
|---|---|---|
| Public inventory browsing, filters, sort, pagination | L20–L28, L817–L824 | Phase 4 |
| Class/race restrictions display | L24–L26, L418–L529 | Phase 4 |
| Availability dates display | L28, L311–L330 | Phase 4 |
| EQResource links | L30, L784–L795 | Phase 4 |
| Admin add/update/remove inventory | L37–L39, L261–L374 | Phase 5 |
| Import inventory, review, deselect, commit | L41, L607–L698; D-04–D-08, D-11, AM-01–AM-03 | Phase 6 |
| Settings management | L35, L190–L214 | Phase 3 |
| User management | L43, L174–L187; D-12 | Phase 7 |
| Audit trail | L773–L783; D-10 (L1083–L1116) | Phase 3 (backend), Phase 7 (UI) |
| First-run experience | L130–L164; D-02 (L877–L892) | Phase 2 |
| Authentication, sessions, CSRF, rate limiting | L216–L227, L797–L804; D-09/D-12 (L1066–L1082, L1130–L1146) | Phase 2 |
| Public + six admin screens | L805–L825 | Phases 4, 5, 6, 7 |
| Docker/Unraid deployment | L63–L67 | Phases 1, 8 |
| Future API consumers | L729–L734; D-03, D-12 | Phases 3–6 (versioned contract) |
| Store separation | D-13 (L1147–L1161) | Phases 2 and 3 |