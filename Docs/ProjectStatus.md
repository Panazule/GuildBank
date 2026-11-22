# Project Status — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-001 |
| As-of date | 2026-11-22 |
| Source specification | `Design.md` v1.6.1 (1,274 lines: body L1–L854, decisions D-01–D-13 at L855–L1161, amendments AM-01–AM-05 at L1163–L1263, informational implementation status at L1265–L1274) |
| Repository state | Phase 1 identity/settings layer, Phase 2 item-metadata integration, Phase 3 import pipeline and Phase 4 public inventory read path. Public browsing, item detail and the public API are implemented; inventory administration, the general audit UI and user management remain later phases |
| Documents issued | `ArchitectureReview.md` (AR-001), `DatabaseDesign.md` (DD-001), `ImplementationPlan.md` (IP-001), `ProjectStructure.md` (PS-002), `ItemsDataProfile.md` (DP-001), `Phase3ImplementationPlan.md`, `Phase4ImplementationPlan.md`, `Phase4Validation.md`, `PublicInventoryAPI.md`, this status report |
| Overall status | **v1.6.1 published; Phases 0–2 approved and complete; Phase 3 approved, implemented and live-validated; Phase 4 approved, implemented and live-validated; Phase 5 is not started** |

---

## 1. As-is snapshot

| Area | State | Evidence |
|---|---|---|
| Requirements | Written; v1.6.1 amendments appended; canonical import sample at its declared path | `Design.md` v1.6.1 |
| Architecture review | Complete, revised | AR-001 rev 1.3: 22 missing requirements, 25 conflicts, 22 security risks, 28 implementation concerns, D-01–D-13 and AM-01–AM-05 adopted, E-01–E-16 registered, Phase 3 dispositions recorded |
| Data design | Complete and approved for the implemented scope | DD-001: DDL for 4 SQLite tables (`AuditOutbox` remains a later-phase addition), 5 MariaDB tables (`Audit` specified by D-10, `MetadataMissingFg` by AM-02); the guild-store tables match migrations `0001_guild_store` and the index-only `0002_public_inventory_indexes` |
| Delivery plan | Complete | IP-001: 9 phases, estimates, test and deployment strategy |
| Source code | Phase 1, Phase 2, Phase 3 and Phase 4 are present | `cmd/guildbank`, `internal/{domain,service,repository,api,web,platform}`; setup, auth, sessions, settings, read-only item metadata, import parser/staging/selection/commit, public inventory list/detail/API, MariaDB migrations and import/public UI |
| Build/CI | Complete for the current scope | `go.mod`/`go.sum` (Go 1.27.3), `Makefile`, `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml` |
| Environments | Development topology defined; item metadata provisioned | Compose app + MariaDB 11.4; 144,712-row read-only `items` copy in `GuildBank`; disposable credentials; no production environment |
| Test assets | Canonical import sample, development item metadata plus Phase 0/1/2/3 tests | `Docs/Samples/InventoryExport-Sample.txt` (548 physical lines); `GuildBank.items` profile in DP-001; tests for config, logging, app, sqlite migrations/repositories, mariadb item mapping/batch/page reads, mask decoders, metadata resolution, parser/natural sort/location normalization, commit planning, multipart CSRF and import handlers; opt-in full-table Items test |
| Deployment | Configured, not deployed | Multi-stage static image, non-root UID 10001, `/config` volume, `EXPOSE 8080`, healthcheck `/healthz` |
| Operations | Health/logs and session purge implemented; backup/support pending | `/healthz`, `/readyz`, JSON logs with request IDs, hourly session purge, graceful shutdown with WAL checkpoint |

**Maturity by capability** (specification and implementation are tracked separately, because a well-specified feature can still be unimplemented):

| Capability | Specification | Implementation | Blocker |
|---|---|---|---|
| Setup wizard | Partial — gate/order implemented; recovery undefined | **Implemented** for the four-step flow | E-02 offline recovery |
| Authentication/sessions | Defined — lifecycle approved as PD-15 | **Implemented** (login/logout, hashed sessions, expiry, CSRF, lockout) | — |
| Public inventory browsing | Defined; PD4-01–PD4-17 approved | **Implemented** — public list/detail/API, search, class/race/type filters, eight sorts, pagination, UTC availability, EQResource links and public DTO | Proxy-level anonymous rate limiting remains operational; no in-app limiter |
| Class/race decoding | Defined with permanent fixtures; PD-19/PD-20 approved | **Implemented** repository-independent decoders plus service metadata generation; import warnings persist to `ImportList.WarningText` | — |
| Inventory administration | Partial — dev metadata loaded; loader procedure/freshness open | Read-only item lookup, metadata resolution and import-driven inventory creation implemented; manual inventory CRUD not started | E-01 |
| Import pipeline | Approved by `Phase3ImplementationPlan.md`; D-04/AM-03 contract, D-08/AM-01/AM-02 metadata rule | **Implemented** — upload, durable failures, normalized staging, `LocationGroup` selector, persisted selection, unit-aware summary, atomic commit, `409` terminal state and admin UI/API | Production grants and real collector exports remain operational follow-ups |
| Spell merge/lifecycle dates | Defined by D-06; import provenance resolved by PD3-03/PD3-04/PD3-14 | **Implemented for imports** — merge by active EQUID into the lowest active row, staged quantity sum, `ITEM_UPDATED` audit provenance, dates frozen at import | — for imports; manual-edit provenance remains a later-phase concern |
| Settings | Partial — typed/secrets implemented; policy review open | **Implemented** (read/update/redaction) | E-11 |
| User management | Partial — policy/last-admin rules | Not started | E-07 |
| Audit | Schema defined by D-10; taxonomy open | Import-scoped audit writes implemented transactionally (`IMPORT_CREATED`, `IMPORT_COMMITTED`, `ITEM_CREATED`, `ITEM_UPDATED`); general audit UI/outbox not started | DC-25, E-09 |
| Deployment/operations | Partial — no backup/TLS rules | Not started | — |

### 1.1 Phase 0 implementation record (2026-11-21)

Per the owner's Phase 0 directive, the deliverable was the platform skeleton
and infrastructure. `ImplementationPlan.md` classifies this scope as Phase 1;
the documentation-closure tasks remain in Phase 0 as written. This report
tracks both so neither is reported as complete when it is not.

| Deliverable | State | Evidence |
|---|---|---|
| Go module and pinned dependencies | Complete | `go.mod` (Go 1.27.3), `go.sum`; Gin 1.12.0, modernc SQLite 1.62.0, MariaDB driver 1.10.2 |
| Recommended folder structure | Complete | `cmd/guildbank`; `internal/{domain,service,repository/{sqlite,mariadb},api,web,platform/{config,logging,app}}` |
| Configuration framework / environment loading | Complete | Defaults → env → flags, validation, usage, password-safe logging |
| SQLite connection layer | Complete | WAL, `busy_timeout(5000)`, foreign keys, single writer, setup probe, WAL checkpoint on close |
| MariaDB connection layer | Complete; live connectivity verified | UTF-8 DSN, UTC, 5 s bounded ping, explicit pool limits, optional at startup; `/readyz` reported `mariadb: ok` against a MariaDB 11.4 container |
| Logging framework | Complete | `slog` JSON/text, request IDs, access log, panic recovery, no secrets |
| Application startup/shutdown | Complete | Composition root, writable `/config` check, bounded 10 s graceful shutdown |
| Gin server and health endpoints | Complete | `/healthz` 200 JSON; `/readyz` 200/503 with per-check reasons; JSON 404/405 |
| Dockerfile / Compose | Complete; image, container, Compose topology and volume persistence verified | Multi-stage, static binary, UID 10001, `/config` writable, `EXPOSE 8080`, healthcheck; Compose app + MariaDB 11.4 both healthy, `/readyz` `mariadb: ok`; `/config` backed by named volume `guildbank_config` and survives container recreation |
| Build/run instructions | Complete | `README.md`, `Makefile`, CI workflow |
| Tests | Complete for skeleton | `go build`, `go vet`, `go test ./...` pass; runtime endpoint probes pass |
| ADRs for D-01–D-13, AM-01–AM-05, E-14–E-16 | **Not started** | Planned under `Docs/decisions/`; no files created |
| E-14 warning sink and E-15 selection payload | **Resolved for imports** | PD3-02 persists warnings in `ImportList.WarningText`; D-11 plus the approved full-replacement selection contract is implemented (foreign/duplicate IDs `422`) |
| E-09 general audit taxonomy closure | **Open** | The import event subset is closed by PD3-14; the general audit UI/taxonomy belongs to a later phase |
| Two real collector exports | **Open** | External input; canonical sample is tracked |

Known deliberate omissions at Phase 0 exit: no migrations, setup/auth/session
routes, import/inventory services, audit recorder or HTML UI, and no automated
MariaDB integration test. Phase 1 and the Phase 2 item-metadata integration have
since added migrations, the identity/settings layer and an opt-in live Items
test. Phase 3 has since added the writable guild tables and import pipeline; public/manual inventory, general audit UI and user management remain absent.

### 1.2 Phase 1 implementation record (2026-11-21)

The owner authorized Phase 1: SQLite schema, Users/Settings/Sessions, setup
wizard, administrator creation, bcrypt, login/logout, session management,
authentication middleware and settings persistence. Audit, inventory, imports
and MariaDB business features were explicitly excluded; `AuditOutbox` is
therefore deferred with the audit recorder.

| Deliverable | State | Evidence |
|---|---|---|
| SQLite migration framework and Phase 1 schema | Complete | `migrations/0001_phase1.sql`; `Migrate` uses `PRAGMA user_version=1`; newer schemas refused; atomic migration and setup transactions tested |
| Users table | Complete | DDL per DD §3.1; case-insensitive unique username; Admin-only access; `DisabledFg`; `LastLogin`; bcrypt hash length 60 verified live |
| Settings table | Complete | Typed/secret columns, seeded defaults, MariaDB keys, session policy; read/update validation and atomic upsert |
| Sessions table | Complete | Distinct `SessionID`/`TokenHash`, idle and absolute expiry, revocation/purge; raw tokens never stored (live `TokenHash` length 64) |
| Setup wizard | Complete | Four steps; admin/guild/database validation; connection test; single atomic commit; `410` after completion; no partial admin |
| Administrator creation | Complete | `SetupService.Complete` hashes password, commits admin + settings, issues a session |
| bcrypt password hashing | Complete | `golang.org/x/crypto/bcrypt` cost 10; round-trip and timing-equalizer tests |
| Login | Complete | Generic failure for unknown/wrong/disabled; `LastLogin`; fifth failure `429`; `HttpOnly` `SameSite=Lax` cookie |
| Logout | Complete | Revokes the row, clears cookie, CSRF-protected; live `204` then `401` |
| Session management | Complete | 32-byte token, SHA-256 hash, 30-min idle / 8-hour absolute, one-minute touch throttle, hourly purge, disable check per request |
| Authentication middleware | Complete | Default-deny admin group; API `401`, HTML redirect; admin-only access |
| Settings persistence | Complete | Admin UI + `GET/PUT` API; secrets redacted; blank MariaDB password preserves the stored secret; invalid values `422` |
| CSRF | Complete | Signed double-submit cookie; login, setup and every write; `403` without a token |
| Login rate limiting | Complete | In-process IP+username 5-failure/15-minute lockout |
| Tests and live validation | Complete | `go build`, `go vet`, `go test ./...`; Compose app + MariaDB healthy; setup, login, settings, logout and restart verified |

Phase 1 decisions PD-10–PD-18 are recorded as approved (2026-11-21) in
`Docs/ProjectStructure.md` §4.2, and PD-19/PD-20 are approved in §4.3. The most
material are: only three SQLite tables in this phase, password-policy values,
custom CSS/JS rather than Bootstrap/HTMX, explicit secure-cookie configuration,
and MariaDB runtime continuing on the environment bootstrap. Phase 3 did not
adopt a Settings-backed or hot-swappable pool; PD-05/PD-18 remain in force.

### 1.3 Items metadata provisioning record (2026-11-21)

The development MariaDB now contains the production collector copy at
`GuildBank.items` (the logical name is `Items`; the server is case-sensitive).
It is available for read-only validation and testing. The approved PD-19/PD-20
item-metadata slice now reads it through one repository mapping.

| Check | Result | Evidence |
|---|---|---|
| Row count and identity | 144,712 rows; 144,712 distinct `id`; 1,001–178,976; no non-positive ids | DP-001 §1 |
| Physical schema | Lowercase `items`; 315 columns; `id BIGINT` primary key; `name`, `classes`, `races` and 311 other nullable `LONGTEXT` fields | DP-001 §2 |
| Logical mapping gap | No `ClassList`, `RaceList` or `MetadataMissingFg` columns; repository must map `classes`/`races` (PD-19) | DP-001 §2, F1 |
| Names | No null/empty names; 139,190 distinct; 3–50 characters; none exceed `VARCHAR(128)`; names are not unique | DP-001 §3.1 |
| Masks | All values numeric text; `classes` 0–4,211,081,216; `races` 0–251,658,240; 60,948/104,340 are `65535` (`ALL`) | DP-001 §3.2 |
| Mask contract gap | 570 class masks and 628 race masks exceed the 16-bit specification; 16-bit-only decoding is insufficient (PD-20) | DP-001 §3.2, F3 |
| Published fixture | `Blade of Carnage` (`id=25210`) → `classes=157` = `WAR,PAL,RNG,SHD,BRD`; `races=65535` = `ALL` | DP-001 §4 |
| Lookup plan | Primary-key lookup by `id` uses `PRIMARY`; names are display data, not identity | DP-001 §2, §4 |
| Access | All validation was `SELECT`; the dev account still has `ALL PRIVILEGES`, so production must enforce read-only grants | DP-001 §5, F4 |
| Source artifact | `Docs/Samples/EQItemList-items.sql` (218,230,768 bytes) is local, contains `DROP TABLE`, and is ignored by Git | DP-001 §1, F5 |

The collector loader procedure, refresh cadence, ownership and production
`SELECT`-only grants remain open (E-01). PD-19 and PD-20 were approved on
2026-11-21 and the Phase 2 item-metadata implementation is recorded below.

### 1.4 Phase 2 item-metadata integration (completed 2026-11-21)

The owner approved PD-10–PD-20, confirmed the decoder implementation and made
Blade of Carnage and Flayed Skin Cap permanent fixtures. This section records
the Phase 2 item-metadata work. At the Phase 2 checkpoint, inventory, imports
and audit were absent and Phase 3 had not started; Phase 3 was subsequently
approved and is recorded in §1.5.

| Deliverable | State | Evidence |
|---|---|---|
| Logical item contract | Complete | `domain.Item` exposes `EQUID`, `Name`, `ClassList`, `RaceList`; `domain.ItemReference` carries the imported identity/name fallback |
| Physical-to-logical mapping | Complete | `mariadb.Store.GetItemByEQUID` selects only `id, name, classes, races` from lowercase `items`; not-found maps to `domain.ErrNotFound` |
| Source mask parsing | Complete | `classes`/`races` LONGTEXT decimals parsed in Go; NULL/blank becomes 0; malformed values return an error |
| Repository completion | Complete | Read-only `GetItemsByEQUIDs` (chunked, duplicate-safe), `CountItems`, and keyset-paged `ListItems`; no write path exists |
| `ClassMaskDecoder` / `RaceMaskDecoder` | Complete and approved | Repository-independent; low 16 bits; `65535`→`ALL`; `0`→blank; stable `WAR…BER` and `Human…Drakkin` order |
| EQUID resolution | Complete | `ItemMetadataService.Resolve`/`ResolveBatch` validates identifiers before reading, preserves input order, and uses the source `Name` when found |
| `MetadataMissingFg` handling | Complete | Absent EQUID preserves imported name, leaves class/race blank, sets `MetadataMissing` (maps to `MetadataMissingFg`) and emits a non-fatal `metadata_missing` warning (D-08, AM-01/AM-02) |
| Undefined high bits | Complete | `Warnings` carries `undefined_high_bits` with EQUID, field, original mask and high-bit value; decoding does not fail |
| Published fixtures | Complete | Automated tests cover `25210 Blade of Carnage` (`157`→`WAR,PAL,RNG,SHD,BRD`; `65535`→`ALL`) and `10802 Flayed Skin Cap` (`1040`→`SHD,NEC`; `5`→`Human,Erudite`) |
| Live full-source test | Complete | Opt-in integration test ran on the Compose network against `GuildBank.items`: 144,712 rows scanned exactly once, 570 class / 628 race high-bit warnings, fixtures and a missing EQUID verified |
| Warning destination | Resolved for imports | The service returns structured warnings; the import pipeline persists them in `ImportList.WarningText` (PD3-02) and surfaces counts in review/commit |
| Read-only HTTP integration | Deferred | No metadata route is exposed; the service is ready for inventory/import consumers in later phases |

`go test ./...` and `go vet ./...` pass. The opt-in integration test compiled as
a static Linux test binary and passed inside the Compose network. PD-10–PD-20
are approved and the Phase 2 go/no-go was recorded; Phase 3 subsequently built
on this service without changing its contract.

### 1.5 Phase 3 import pipeline (completed 2026-11-21)

The owner approved PD3-01–PD3-14 and all six §14.1 defaults, and added one
clarification: `LocationGroup` is the authoritative value for filtering and
selection, the selector is generated from distinct `LocationGroup` values, and
every `Hoard<n>` variant normalizes to the single `Hoard` group. `LocationRaw`
is retained only for traceability and audit.

| Deliverable | State | Evidence |
|---|---|---|
| MariaDB migration and runner | Complete | Embedded `0001_guild_store.sql` creates `ImportBatch`, `ImportList`, `GuildBankItems` and `Audit`; `schema_migrations`, per-database `GET_LOCK`, lexical downgrade guard and fatal startup migration |
| Parser and limits | Complete | BOM/CRLF/UTF-8 handling; canonical header; 3/4/5-field rules; section-row and blank-row classification; 548/197/348/2/1 golden counts; 10 MiB, 50,000-line, 64 KiB-line guards |
| Normalization and natural sort | Complete | `General`/`Bank`/`SharedBank`/`Hoard` groups, slot stripping, leading-zero canonicalization, `Hoard<n>`→`Hoard`, unknown/equipment→`Unspecified`, `NaturalSortKey` with 10-digit numeric runs |
| Classification and metadata | Complete | `Spell`/`Song`/`Ancient` prefix classification; classification-conflict failure; batch metadata resolution; `metadata_missing` fallback for items and spells |
| Durable staging | Complete | Batch created as `Processing`; parse/stage failures retained with line-numbered reasons; rows and `Uploaded` transition plus `IMPORT_CREATED` audit commit atomically |
| Selection and summary | Complete | Full replacement selection, foreign/duplicate rejection, persisted per-row `SelectedFg`, `LocationGroup`-based group toggles, unit-aware summary and merged-spell preview |
| Atomic commit | Complete | Advisory lock and `FOR UPDATE`; item expansion to individual units; spell aggregation and merge into lowest active ID; metadata/date rules; 100,000-record guard; `Completed` terminal state and one-retry deadlock handling |
| Transactional audit | Complete | `IMPORT_CREATED`, `IMPORT_COMMITTED`, one summary `ITEM_CREATED` and one `ITEM_UPDATED` per merged spell in the same MariaDB transaction; audit failure rolls back the commit |
| REST API | Complete | Admin-only upload, history, detail, locations, rows, row IDs, selection GET/PUT, summary and commit; path-aware body limits, multipart CSRF, `409`/`413`/`422`/`503` mappings |
| Admin UI | Complete | `/admin/imports` history/upload and `/admin/imports/:id` review; `imports.js` handles upload state, summary, selector, paging, debounced selection and commit; degraded mode remains visible with upload disabled |
| Recovery and startup wiring | Complete | Fatal guild migration, 15-minute `Processing` recovery sweep, import service built in degraded mode when MariaDB is unavailable, 120-second HTTP and DSN timeouts |
| Validation | Complete | `go vet ./...` and `go test ./...` pass; parser golden and commit-plan unit tests pass; multipart CSRF, `LocationGroup` selector and import pages covered by handler tests; the authenticated Compose workflow passed in §1.5.1 |
| Live Compose migration | Complete | Rebuilt `guildbank:dev`, restarted the app, observed `guild store schema ready schema_version=0001_guild_store`; `ImportBatch`, `ImportList`, `GuildBankItems`, `Audit` and `schema_migrations` exist and `/healthz` is healthy |
| Live upload/commit run | Complete | Setup, login, upload, 30-group review, an 8-group subset selection and atomic commit passed against the running app and MariaDB; durable staging, inventory, warnings and audit were verified. Full evidence is in §1.5.1 |

### 1.5.1 Authenticated end-to-end validation (2026-11-22 UTC)

The Compose app and MariaDB 11.4 were healthy. The config volume was in
first-run state (`Users` empty, `SetupComplete=0`), so the approved four-step
setup API was completed with a disposable `admin` account; `GET /setup`
subsequently returned `410`. A real login followed a `204` logout: login and
`/api/v1/auth/session` returned `200`, `/admin/imports` and
`/admin/imports/1` rendered `200`, and `/readyz` returned `200`. SQLite then
held one enabled Admin with a 60-character bcrypt hash, `SetupComplete=1`,
and an active session with a 32-character ID and 64-character token hash.

The canonical `Docs/Samples/InventoryExport-Sample.txt` (SHA-256
`25627351d99d73de1029ed42ef455508cfc59909f60b319c272256ae16fd09a5`)
uploaded as batch 1 and matched the golden fixture:

| Check | Observed |
|---|---|
| Physical lines | 548 |
| Accepted / staged rows | 197 |
| Discarded rows | 348 |
| Structural rows (header and separator) | 2 |
| Blank rows | 1 |
| Non-fatal parser warnings | `metadata_missing` = 1; `undefined_high_bits` = 1 |
| Normalized `LocationGroup` count | 30: `Bank 1`–`Bank 16`, `General 1`–`General 10`, `Hoard`, `SharedBank 1`–`SharedBank 2`, `Unspecified` |
| Hoard normalization | Raw `Hoard 1`, `Hoard 2` and `Hoard 3` rows all selected one `Hoard` group, 3 rows total |
| Item totals | 194 item rows / 723 item units |
| Spell totals | 3 spell rows / 3 spell units; 3 distinct, 3 new |

The selected subset was `General 1`, `General 7`, `Bank 1`, `Bank 4`, `Bank 7`,
`SharedBank 1`, `SharedBank 2` and `Hoard`: 77 of 197 rows across 8 of 30
locations, containing 74 item rows, 471 item units and all 3 spells. The
selection was persisted through `PUT /selection`; the summary and commit agreed
on 474 records. Commit returned `Completed` with 471 item records, 3 new spells,
0 merged spells and 474 affected records; a second commit returned `409`.

Direct MariaDB verification after batch 1 found:

| Object | Evidence |
|---|---|
| `ImportBatch` | Batch 1 `Completed`, 548 source rows, 197 accepted rows, imported by Admin user 1 |
| `ImportList` | 197 staged, 77 selected, 120 unselected; the `metadata_missing` warning on ImportID 24 and the `undefined_high_bits` warning on ImportID 116 were persisted in `WarningText` |
| `GuildBankItems` | 471 `Item` rows plus 3 `Spell` rows (IDs 472–474), each spell quantity 1; EQUID 89338 remains named `Farseer's Frostpack` with blank class/race strings and `MetadataMissingFg=1` |
| `Audit` | 3 rows: `IMPORT_CREATED`, `IMPORT_COMMITTED` and the summary `ITEM_CREATED`, all with the authenticated user and request correlation IDs |

A second, merge-focused batch re-uploaded the same file: the service returned
`duplicateOf=1` plus a `duplicate_file` warning without rejecting the upload, as
approved. Selecting only `General 1` (35 rows, 33 item rows, 232 item units,
2 spells) produced 232 new item records and merged 2 existing spells; no spell
was created. Spell IDs 472 and 473 changed from quantity 1 to 2, and ID 474
stayed at 1. Batch 2 also completed, and the audit total rose to 8 rows:
2 `IMPORT_CREATED`, 2 `IMPORT_COMMITTED`, 2 `ITEM_CREATED` and 2
`ITEM_UPDATED` entries (record IDs 472 and 473).

No Phase 3 defects were found. Both non-fatal warnings were surfaced and
persisted as designed, metadata fallback preserved the imported identity, and
duplicate-file re-upload remained advisory rather than blocking. The only
environment observation is that this Compose volume was uninitialised, so setup
had to be completed before validation; that is expected fresh-volume behavior,
not a defect. No corrective implementation change is recommended.

### 1.6 Phase 4 public inventory read path (completed 2026-11-22 UTC)

The owner approved PD4-01–PD4-17 subject to the documented performance,
indexing and validation conditions, and authorized Phase 4 implementation.
PD4-11 is approved with UTC day boundaries; `DateAdded` and `metadataMissing`
are public fields. Phase 5 was explicitly not authorized. Phase 5 must later
validate `DateOpen <= DateBazaar` for all new writes.

| Deliverable | State | Evidence |
|---|---|---|
| Public routes | Complete | Anonymous `/`, `/inventory/:id`, `/api/v1/items` and `/api/v1/items/:id`; no authentication and no admin item route |
| Public projection | Complete | Dedicated DTO exposes `id`, `equid`, `name`, `itemType`, `quantity`, `classString`, `raceString`, `metadataMissing`, three dates, `availability` and nullable `eqResourceUrl`; forbidden internal fields are absent |
| Search/filter/sort | Complete | Trimmed 128-rune name search with literal `%`, `_`, `!`; canonical class/race membership including stored `ALL`; item type; eight-value sort allowlist with ID tie-breaker |
| Availability | Complete | One UTC date per request; inclusive open/bazaar boundaries; inverted windows never report open |
| EQResource | Complete | Server-side exact setting revalidation, positive decimal EQUID, nullable on invalid setting, no external request |
| Query/index safety | Complete | Explicit projection, parameterized predicates, enum-switch `ORDER BY`, active-only count/page/detail, repeatable-read read-only snapshot, page cap 10,000 and size cap 100 |
| Migration | Complete | `0002_public_inventory_indexes.sql` applied live; version `0002_public_inventory_indexes`; existing indexes retained |
| UI | Complete | Responsive list/detail templates, public navigation, labels/caption, availability text plus color, external-link attributes, empty/no-match/beyond-end/unavailable states, embedded CSS |
| Error contract | Complete | `422` field-keyed validation, `404` absent/deleted detail, `503` unavailable/deadline, `405` public method rejection, strict security headers and `no-store` |
| Validation | Complete | `go test ./...`, `go vet ./...`, `gofmt` and `git diff --check` pass; service/repository/API/HTML/security tests added; live Compose checks passed |
| Performance | Complete | 250k design target and 1M stress target passed after two measured optimizations (index-friendly null handling and skipping a provably redundant count on empty page 1); full results in `Phase4Validation.md` |
| Live migration/outage | Complete | Rebuilt image observed `schema_version=0002_public_inventory_indexes`; anonymous API/HTML `200`; restart preserved 706 records; MariaDB stop produced `503`/`503`; restore produced `200`/`200` |

Natural sorting is verified against MariaDB with `Phase4 Order 1, 2, 10, 11`.
Class/race and leading-wildcard search remain scan predicates; they pass the
1M gates. The documented escalation to a normalized search column or membership
tables was not required for this phase. No content fixture was left in the
development guild database. Full evidence is in `Docs/Phase4Validation.md` and
the contract is in `Docs/PublicInventoryAPI.md`.

Phase 4 does not implement admin inventory data, item mutation, location
filtering/normalization, availability-date filtering, in-app anonymous rate
limiting or the Phase 5 write validation. Those remain later-phase or
operational work.
`Phase3ImplementationPlan.md` remains the design record for Phase 3 choices.
Phase 4 is implemented and live-validated; Phase 5 was not started.

---

## 2. Documents produced in this review

| Document | Contents | Key outputs |
|---|---|---|
| `Docs/ArchitectureReview.md` | Conformance assessment, findings, decisions, v1.6 disposition | MR-01–MR-22, DC-01–DC-25, SR-01–SR-22, IC-01–IC-28, A-01–A-12, E-01–E-16, D-01–D-13 mapping |
| `Docs/DatabaseDesign.md` | Physical schema, constraints, transactions, grants, retention | SQLite DDL (Users, Settings, Sessions, AuditOutbox), MariaDB DDL (Items, GuildBankItems, ImportBatch, ImportList, Audit), 9 workflow specifications, D-01–D-13 conformance |
| `Docs/ImplementationPlan.md` | Phased delivery, API surface, audit taxonomy, testing, deployment | 9 phases (58–84 engineer-days with contingency), route table, D-10 event taxonomy, fixtures, DoD, risk register |
| `Docs/ItemsDataProfile.md` | Read-only profile of the development item metadata | 144,712-row inventory, physical/logical mapping, mask distribution, decoding fixture, PD-19/PD-20 |
| `Docs/ProjectStatus.md` | This report | Traceability, blockers, next actions |

No application functionality beyond the platform skeleton and Phase 1
identity/settings layer exists. Phase 0 created the buildable service,
configuration, logging, both connection layers, health endpoints, deployment
files and tests. Phase 1 added the SQLite schema, setup, authentication,
sessions, CSRF, rate limiting, settings and the first screens; the full map is
in `Docs/ProjectStructure.md`.

---

## 3. Blocking items

| ID | Blocker | Decision / amendment | Status | Consequence / next action |
|---|---|---|---|---|
| B-1 | `Audit` has no schema | D-10 | **Closed** | Taxonomy/retention still open (DC-25) |
| B-2 | Import file format undefined | D-04, AM-03 | **Closed** | Sample at `Docs/Samples/InventoryExport-Sample.txt`; delimiter, header, mapping, UTF-8, variable-width rows and row rejection fixed |
| B-3 | No endpoint to persist `SelectedFg` | D-11 | **Closed** | Implemented as a full-replacement selection endpoint; foreign/duplicate IDs return `422` |
| B-4 | Location normalization is examples only | D-05, PD3-01 | **Closed** | `Hoard<n>` collapses to `Hoard` (AM-04); equipment, blank and unparseable reserved patterns normalize to `Unspecified` with warnings |
| B-5 | `Items` provisioning; `EQUID` vs `Items.ID` | D-01, PD-19 | **Closed for implementation; operational follow-up** | Identity fixed; the 144,712-row development table is loaded and validated; loader ownership/freshness and production `SELECT`-only grants remain E-01 |
| B-6 | Setup mode has no explicit state | D-02 | **Closed for implementation** | Explicit pending/complete/unknown state; missing `Users` fails closed; disabled admins still count; admin + settings commit atomically; offline recovery command remains E-02 |
| B-7 | HTMX/SSR ↔ JSON REST model undecided | D-03, PD3-09 | **Closed** | The implemented admin UI uses server-rendered templates plus vanilla `fetch` against JSON REST endpoints; HTMX fragments are not used |
| B-8 | Spell merge key and date behavior undefined | D-06, PD3-03/PD3-04/PD3-14 | **Closed** | Merge targets active rows only, aggregates staged spells, serializes commits with an advisory lock and records `ITEM_UPDATED` provenance |
| B-9 | D-08 wrote a placeholder identifier for unresolved items | D-08, AM-01/AM-02 | **Closed** | Original `EQUID` preserved, `MetadataMissingFg` added, no sentinel, FK enforcement relaxed |
| B-10 | Specification packaging incomplete | AM-05 | **Closed** | v1.6.1 title, `Hoard`, amendments and change log published |

Supporting engineering decisions still required before their relevant later phase: secrets at rest (E-11), authorization matrix/public projection (E-07), public API pagination conventions (E-12), general audit taxonomy (E-09) and HTMX error semantics if HTMX is later adopted (E-16). E-08 is approved as PD-15; E-13/AM-01/AM-02, the import warning sink (E-14), the import selection payload (E-15) and import spell-merge provenance are resolved.

---

## 4. Requirement traceability

Status legend: **Defined** = implementable as written; **Partial** = exists but lacks parameters/contract; **Missing** = required behavior absent.

### 4.1 Functional requirements

| ID | Requirement | Spec refs | Spec status | Implementation | Covered in |
|---|---|---|---|---|---|
| FR-01 | Public inventory browsing without login | L20–L22 | Defined | **Implemented** | IP Phase 4, `Docs/PublicInventoryAPI.md` |
| FR-02 | Display item details incl. stats/restrictions | L24–L27 | Defined | **Implemented** for stored restrictions, type, quantity and dates; raw item-stat fields are not stored in `GuildBankItems` | IP Phase 4, DD §4.2 |
| FR-03 | Display availability dates | L28 | Resolved by PD4-11 | **Implemented** with UTC states | DD §4.2, `Phase4Validation.md` |
| FR-04 | Class and race filtering | L25–L26, L821–L824 | Resolved by PD4-07/08 | **Implemented** | DD §9.1, §9.6, `Docs/PublicInventoryAPI.md` |
| FR-05 | Search, sort, natural sort, pagination | L699–L728, L817–L824 | Resolved by PD4-05/06/10/13 | **Implemented**; 250k/1M gates passed | DD §9.5, `Phase4Validation.md` |
| FR-06 | EQResource links in new tab | L30, L784–L795 | Resolved by PD4-12 | **Implemented** with server-side URL revalidation | AR SR-08, `Phase4Validation.md` |
| FR-07 | Admin add inventory | L37–L39, L741–L745 | Partial (validation) | Not started | DD §7.2, IP Phase 5 |
| FR-08 | Admin update inventory | L37, L744 | Partial (mutable fields) | Not started | DD §7.3, IP Phase 5 |
| FR-09 | Admin remove inventory (soft delete) | L38, L359–L368 | Defined | Not started | DD §7.3, IP Phase 5 |
| FR-10 | Import inventory upload | L41, L607–L630; D-04, AM-03 | Defined | **Implemented** | AR MR-02, IP Phase 6, `Phase3ImplementationPlan.md` |
| FR-11 | Review import, deselect rows/locations | L646–L670; D-11 | Defined | **Implemented** | DD §7.9, `Phase3ImplementationPlan.md` |
| FR-12 | Commit import with quantity expansion | L673–L698; D-06, D-07 | Defined | **Implemented** | DD §7.5, `Phase3ImplementationPlan.md` |
| FR-13 | Retain import history | L549–L556 | Defined | **Implemented** (batches, staging and terminal states retained) | DD §4.3–4.4, §10.4, `Phase3ImplementationPlan.md` |
| FR-14 | Manage settings | L35, L190–L214 | Partial (types/secrets) | **Implemented** (read/update/redaction) | DD §3.2, IP Phases 2–3 |
| FR-15 | Manage users | L43, L756–L763 | Partial (policy/delete) | Not started | DD §3.1, §7.7, IP Phase 7 |
| FR-16 | Audit trail (items, imports, users, settings, auth) | L773–L783; D-10 | Partial (general taxonomy open) | **Import-scoped implemented**; general audit UI/outbox not started | DD §4.5, IP §5, PD3-14 |
| FR-17 | First-run wizard (4 steps) | L130–L164; D-02 | Partial (recovery) | **Implemented** | DD §7.1, IP Phase 2 |
| FR-18 | Public + six admin screens | L805–L825 | Partial (dashboard contents) | Not started | IP §4, Phases 4–7 |
| FR-19 | Future external API/tool support | L729–L734 | Defined (intent only) | Not started | AR IC-02, IP §4 |

### 4.2 Data and non-functional requirements

| ID | Requirement | Spec refs | Spec status | Implementation | Covered in |
|---|---|---|---|---|---|
| DR-01 | SQLite for users/settings/sessions | L69–L107, L166–L231 | Defined | **Implemented** | DD §3 |
| DR-02 | MariaDB for guild data | L69–L107, L233–L605 | Defined | **Implemented** for the guild store and import writes; public/manual inventory reads and writes remain later phases | DD §4 |
| DR-03 | `Items` read-only | L242–L244 | Defined (production grant unenforced) | **Implemented** in code (read-only repository); production `SELECT`-only grant remains an operational check (E-01) | DD §4.1, §10.1 |
| DR-04 | Decoded class/race strings stored | L285–L309, L846–L848 | Defined | **Implemented** for imported inventory | DD §9.6 |
| DR-05 | Lifecycle dates stored permanently | L314–L330 | Defined (conflicts with edits) | **Implemented** in import commit | AR DC-06, DD §4.2 |
| DR-06 | Soft delete only | L359–L368 | Defined | **Implemented at schema level** (no delete path); inventory soft-delete API/UI remain later phases | DD §4.2, §7.3 |
| DR-07 | Cross-store actor identity | L370–L374, L538–L542 | Defined | **Implemented** for import upload/commit snapshots | DD §5–§6 |
| NR-01 | Layered architecture enforcement | L109–L129 | Defined | **Implemented** (import guards) | AR §3, IP Phase 1 |
| NR-02 | bcrypt password storage | L181–L183, L798 | Partial (cost/policy) | **Implemented** (approved PD-11) | AR SR-12 |
| NR-03 | Session expiration | L216–L227, L799–L802 | Partial (timeouts) | **Implemented** (approved PD-15) | AR SR-03, DD §3.3 |
| NR-04 | CSRF protection | L801 | Partial (mechanism) | **Implemented** (approved PD-13) | AR SR-05, IP Phase 2 |
| NR-05 | Login rate limiting | L802 | Partial (thresholds) | **Implemented** (approved PD-16) | AR SR-06, IP Phase 2 |
| NR-06 | Secure cookies / HTTPS | L800 | Partial (transport) | **Implemented** explicit flag (approved PD-17); TLS deployment pending | AR SR-11 |
| NR-07 | Administrator-only routes | L43, L804 | Partial (matrix) | **Implemented** default-deny admin group | AR SR-04, IP §4 |
| NR-08 | HTTPS-ready Docker/Unraid deployment | L63–L67 | Partial (health/ops) | Not started | IP §7 |
| NR-09 | Performance suitability | L843 | Partial (no targets) | Not started | AR MR-18, IP Phase 8 |

### 4.3 Coverage summary

| Category | Defined | Partial | Missing | Total |
|---|---|---|---|---|
| Functional | 7 | 12 | 0 | 19 |
| Data/NFR | 8 | 8 | 0 | 16 |
| **Total** | **15 (43 %)** | **20 (57 %)** | **0 (0 %)** | **35** |

No requirement is classed **Missing** after v1.6.1. The former contradictions are resolved: FR-10 is implementable against the documented sample, and the D-08 missing-metadata rule is implementable under AM-01/AM-02. The residual risk is behavioural (location edge cases) and operational (`Items` provisioning).

---

## 5. Risk register (project level)

| Risk | Likelihood | Impact | Score | Mitigation | Owner |
|---|---|---|---|---|---|
| Residual general contracts not closed (E-09/E-11/E-16) | Medium | High | **High** | Import warning and selection contracts are resolved; close the remaining general audit, secret-at-rest and any future HTMX contracts before their phases | Product owner |
| Import format diverges from real exports (D-04/AM-03) | Medium | High | **High** | Add two real exports before Phase 6; keep the parser behind an interface | Product/ops |
| Unresolved metadata never revalidated (AM-01/AM-02) | Medium | Medium | Medium | Implement the `MetadataMissingFg` refresh job and audit resolution | Dev |
| `Items` table unavailable or stale in target environment | Medium | High | **High** | Read-only integration test passes against the 144,712-row dev copy; loader procedure, freshness and production grants remain open (E-01) | Operator |
| Setup/bootstrap lockout or ordering flaw (D-02) | Medium | High | **High** | Fail closed on unreadable `Users`, admin-commit-last wizard, CSRF, tests | Dev |
| Secret leakage (DB password, hashes) | Medium | High | **High** | Redaction tests, `IsSecret`, volume permissions | Dev/ops |
| SQLite lock contention under admin load | Medium | Medium | Medium | WAL, single writer, load test | Dev |
| MariaDB outage blocks operational writes | Medium | Medium | Medium | Outbox + degraded mode + clear UX | Dev |
| Scope creep from future enhancements | Medium | Medium | Medium | Non-goals list; decision gate per new table/route | Product owner |
| Single-maintainer knowledge concentration | High | Medium | Medium | ADRs, docs, tests, runbooks | Project lead |
| Performance below expectation at scale | Low | Medium | Low | Indexes, persisted sort keys, Phase 8 measurement | Dev |

---

## 6. Decision log

### 6.1 v1.6 specification decisions (adopted)

| Decision | Status | Effect |
|---|---|---|
| D-01 item identity | **Adopted** | `Items.ID` = `EQUID`; app ids distinct |
| D-02 setup mode | **Adopted** | Gate is "no administrator exists"; ordering caveat (DC-24) |
| D-03 HTMX/REST | **Adopted** | UI→HTMX→REST→Service→Repository→DB |
| D-04 import contract | **Adopted** | Sample at `Docs/Samples/`; AM-03 fixes delimiter, header, mapping, UTF-8 and row rejection |
| D-05 location normalization | **Adopted** | `Area n-Slot m`→`Area n`; `Hoard n`→`Hoard` |
| D-06 spell merge | **Adopted** | `(Spell, EQUID)`; quantity adds; dates unchanged |
| D-07 item import | **Adopted** | Never merge; one row per unit |
| D-08 missing metadata | **Adopted** | AM-01/AM-02: `EQUID` preserved, `MetadataMissingFg`, no sentinel, FK relaxed |
| D-09 session security | **Adopted** | `TokenHash` stored; no plaintext tokens |
| D-10 audit table | **Adopted** | Ten fields; append-only; example event types |
| D-11 selection endpoint | **Adopted** | `PUT /api/imports/{batchId}/selection` |
| D-12 additional security | **Adopted** | CSRF (authenticated writes), cookie flags, 5/15 lockout, SQL allowlist |
| D-13 store separation | **Adopted** | SQLite identity/config/sessions; MariaDB guild data |

### 6.2 Engineering decision register

| ID | Subject | Status | Default |
|---|---|---|---|
| E-01 | `Items` provisioning/freshness | Partial | Dev `items` copy loaded 2026-11-21; loader procedure, refresh cadence and production `SELECT`-only grants remain open |
| E-05 | Spell merge provenance/concurrency | **Resolved for imports; later phases TBD** | PD3-03/PD3-04: active targets only, aggregate by EQUID, lowest active ID, advisory lock; PD3-14 writes `ITEM_UPDATED` provenance |
| E-06 | Unknown location patterns/vocabulary | **Resolved for imports** | PD3-01: equipment/blank/unknown → `Unspecified` with warnings; all `Hoard<n>` → `Hoard` (AM-04) |
| E-07 | Authorization matrix/projections | Partial | Admin-only routes enforced; public inventory projection still open |
| E-08 | Session lifecycle | **Implemented; approved as PD-15** | 8 h absolute, 30 min idle, sliding refresh, purge; lifecycle review may refine values |
| E-09 | Audit taxonomy closure | Partial | Import subset closed by PD3-14 (`IMPORT_CREATED`, `IMPORT_COMMITTED`, `ITEM_CREATED`, `ITEM_UPDATED`); general taxonomy/UI open |
| E-10 | Date comparison semantics | Open | `DateOpen <= today` means open; UTC |
| E-11 | Settings validation/secrets | **Implemented; PD-11/PD-18 approved** | Typed settings, redaction, volume-permission reliance; secret-at-rest review remains |
| E-12 | API versioning/pagination | Partial | `/api/v1` and import paging implemented; public inventory pagination contract pending |
| E-13 | Missing-metadata strategy | **Resolved** | AM-01/AM-02: preserve id, set `MetadataMissingFg`, relax FK |
| E-14 | Warning sink | **Resolved for imports** | PD3-02: `ImportList.WarningText` stores structured warnings; batch counts surface them |
| E-15 | Selection payload contract | **Resolved for imports** | Full replacement, batch-scoped, `422 selection_invalid` for foreign/duplicate/non-positive ids |
| E-16 | HTMX error/auth semantics | Deferred | Implemented UI uses plain forms and vanilla `fetch`; revisit only if HTMX is adopted for inventory UI |

D-01–D-13 are specification decisions and do not require engineering approval. PD-10–PD-20 were approved on 2026-11-21; PD-10–PD-18 are recorded in `Docs/ProjectStructure.md` §4.2 and PD-19/PD-20 in §4.3. PD3-01–PD3-14 and all six Phase 3 plan defaults are also approved; the decoders, the two named fixtures and the import pipeline are part of the approved baseline. Phase 3 did not adopt a Settings-backed MariaDB pool, so PD-18 remains in force as an explicit later change.

---

## 7. Next actions

| # | Action | Owner | Effort | Depends on | Output |
|---|---|---|---|---|---|
| 1 | Add two real collector exports beside the canonical sample | Guild admin + dev | external | — | Import fixtures (closes E-04) |
| 2 | Turn the loaded development `items` copy into a documented loader/freshness procedure and production `SELECT`-only grants | Operator + dev | 0.5–1 d | — | Loader runbook, verified least-privilege access (E-01) |
| 3 | Close the general audit taxonomy (E-09) and secret-at-rest review (E-11) before their phases; the public API/pagination contract (E-12) is closed by Phase 4 | Dev + product | 0.5–1 d | — | Approved records |
| 4 | Decide when Settings become the authoritative MariaDB configuration, including whether to hot-swap the pool (PD-18) | Product + dev | 0.5 d | — | Separate approved change; PD-05/PD-18 remain runtime behavior until then |
| 5 | Write ADRs for D-01–D-13, AM-01–AM-05, PD-10–PD-20, PD3-01–PD3-14 and PD4-01–PD4-17 | Dev | 1 d | — | `Docs/decisions/` records |
| 6 | Provision the target MariaDB and Unraid template | Operator + ops | 0.5 d | #2 | Deployment artifacts |

**Immediate next:** Phase 4 is approved, implemented and live-validated
(§1.6, `Phase4Validation.md`). The remaining items are operational or
later-phase work (#1–#6). Phase 5 is not authorized; when authorized, its first
validation condition is `DateOpen <= DateBazaar` for all new writes.

---

## 8. Exit criteria and next-phase gate

Phase 0, Phase 1 and Phase 2 are complete and approved. The owner-approved Phase 1 covers
the skeleton, SQLite schema, setup wizard, administrator/session/authentication
layer and settings persistence. Phase 2 covers the read-only Items repository,
approved class/race decoders and metadata resolver; the opt-in live test passed
against all 144,712 development rows. `ImplementationPlan.md` uses different
phase numbers; the owner's numbering governs this report.

Phase 3 is approved (PD3-01–PD3-14 plus all six §14.1 defaults) and complete:
upload, durable staging, `LocationGroup` selection, unit-aware summary, atomic
commit, import-scoped audit, REST API, admin UI and tests are implemented, the
`0001_guild_store` migration is applied in Compose, and the authenticated live
workflow passed (§1.5.1). PD-10–PD-20 are approved; PD-19/PD-20 and the decoder
implementation are part of the baseline.

The authenticated Compose upload → selection → commit workflow passed on
2026-11-22 UTC, including the full sample parse, subset commit, durable inventory
and audit rows, and a spell-merge batch; the evidence is in §1.5.1.

Remaining deployment and later-phase checks, none of which reopens Phase 3:

- Add the two real collector exports; the canonical sample is a regression
  fixture, not proof against production exports.
- Close the production loader/freshness and `SELECT`-only grant requirements
  under E-01.
- The general audit taxonomy (E-09), secret-at-rest review (E-11) and future
  HTMX contract (E-16) remain later-phase work. The public API/pagination
  contract (E-12) is closed by Phase 4.
- PD-18 remains in force: the wizard persists MariaDB settings, but the runtime
  pool still uses the environment bootstrap and is not hot-swapped.
- `Design.md` v1.6.1 and the D-04 sample remain the published baseline.

Phase 4 is complete and live-validated. Phase 5 has not started.

---

## 9. Assumptions and limitations of this document set

1. All statements are derived from `Design.md` v1.6.1; where the specification is silent, the gap is recorded rather than silently resolved.
2. Line references (`L###`) are valid for the body (L1–L854) and the decisions (L855–L1161) and were re-verified at the cited anchors; the v1.6.1 amendments are cited as `AM-xx` at L1163–L1263. The non-normative implementation-status block at L1265–L1274 does not change any requirement. Decisions D-01–D-13 have no per-decision line anchors (IC-26). Any further edit to `Design.md` requires re-checking.
3. The documents introduced **PROPOSED** schema elements and defaults. The decisions used by Phases 1–3 were approved on 2026-11-21 (PD-10–PD-20, PD3-01–PD3-14); the remaining `AuditOutbox` and general-audit additions are later-phase proposals, not current implementation requirements.
4. Application code covers the Phase 0 skeleton, Phase 1 identity/settings layer, Phase 2 item metadata, Phase 3 import pipeline and Phase 4 public inventory read path, including migrations, parser/commit tests, public service/repository/API/template tests, security tests and performance gates. Manual inventory administration, general audit UI/outbox and user-management tests do not yet exist; effort estimates remain planning ranges, not measured velocity.
5. The review assumes a single Go service with server-rendered UI, Gin and SQLite/MariaDB as specified. Phase 1 used plain embedded forms and vanilla JS rather than HTMX/Bootstrap (PD-12), and Phase 3 uses JSON endpoints with vanilla `fetch` (PD3-09); the layer boundaries still match the specification.
6. Security findings are design-level. A penetration test and dependency scanning are still required before production use.
7. Estimates assume one experienced developer and exclude waiting time for external data (`Items`, export files, Unraid access).

---

## 10. Change log

| Version | Date | Change |
|---|---|---|
| 1.0 | 2026-11-20 | Initial status report; four documents issued; 8 blockers, 12 open decisions |
| 1.1 | 2026-11-20 | Re-baselined on `Design.md` v1.6; D-01–D-13 adopted; engineering decisions renumbered E-xx; 4 blockers closed, 4 partial/open, 2 new (B-9, B-10) |
| 1.2 | 2026-11-20 | Re-baselined on `Design.md` v1.6.1; AM-01–AM-05 adopted; B-2, B-9, B-10 closed; E-13 resolved; 3 blockers remain partial |
| 1.3 | 2026-11-21 | Phase 0 skeleton implemented: Go module, package layout, config, logging, SQLite/MariaDB layers, health endpoints, Docker/Compose, CI, tests; `ProjectStructure.md` added; ADRs, E-14/E-15/E-09 and real exports remain open |
| 1.4 | 2026-11-21 | PD-05 approved as temporary MariaDB bootstrap configuration (Settings become authoritative after the Setup Wizard); `/config` named-volume persistence verified across container recreation |
| 1.5 | 2026-11-21 | Phase 1 implemented: SQLite migrations (Users, Settings, Sessions), four-step setup, bcrypt administrator, login/logout, hashed server-side sessions, CSRF, login lockout, default-deny admin middleware, settings persistence/redaction and embedded screens; live Compose/MariaDB flow and restart verified; PD-10–PD-18 were pending at this checkpoint and were subsequently approved |
| 1.6 | 2026-11-21 | Development `items` copy loaded and profiled read-only (144,712 rows); DP-001 added; physical lowercase/column mapping and wide-mask handling registered as PD-19/PD-20; E-01 changed to partial |
| 1.7 | 2026-11-21 | PD-19/PD-20 approved; added repository-independent `ClassMaskDecoder`/`RaceMaskDecoder`, `domain.Item`, read-only `mariadb.Store.GetItemByEQUID` mapping and fixture tests for 25210 and 10802; documented the scoped Phase 2 item-metadata slice |
| 1.8 | 2026-11-21 | PD-10–PD-20 approved; completed Phase 2 item metadata: batch/count/keyset repository reads, `ItemMetadataService` with D-08 missing-metadata handling, full 144,712-row opt-in integration test passed (570/628 high-bit warnings), decoders and fixtures declared permanent; Phase 3 was not started at this checkpoint and was subsequently approved and completed |
| 1.9 | 2026-11-21 | PD3-01–PD3-14 and all six §14.1 defaults approved; Phase 3 import pipeline implemented: guild-store migration, parser/normalization/natural sort, durable staging, `LocationGroup` selector (all `Hoard<n>`→`Hoard`), selection, unit-aware summary, advisory-locked atomic commit, import-scoped transactional audit, REST API, admin UI and tests; Phase 4 not started |
| 2.0 | 2026-11-21 | Documentation consistency audit: removed stale pending-approval language; recorded PD-10–PD-20 and PD3-01–PD3-14 as approved; closed resolved blockers and decisions; corrected Phase 3 gate/next-action text; aligned DatabaseDesign import DDL and ProjectStructure with the implemented migration and files; Phase 4 remains not started |
| 2.1 | 2026-11-21 | Authenticated live Compose validation passed: fresh-volume setup, admin login, canonical sample (548/197/348), 30 normalized groups including one `Hoard`, 8-group subset selection (77 rows, 471 item units, 3 spells), atomic commit (474 records), durable warnings and missing-metadata fallback, `409` re-commit, and a second batch proving 2 spell merges and `ITEM_UPDATED` audit; no Phase 3 defects; Phase 4 remains not started |
| 2.2 | 2026-11-22 | Phase 4 approved and completed: PD4-01–PD4-17 with UTC availability, public `DateAdded`/`metadataMissing`, public list/detail/API, search/class/race/type filters, eight sorts, pagination, EQResource revalidation, index-only `0002` migration, public templates/CSS and security tests; 250k design and 1M stress performance gates passed; live Compose restart/outage/recovery validation passed; Phase 5 not started |

---

## 11. Status summary

The GuildBank specification is **buildable as written**. v1.6 closed the audit schema, the selection route, the HTMX/REST layering model, spell merge and item identity, and defined session storage, the audit table and store separation. v1.6.1 closed the two remaining contradictions: the canonical sample lives at `Docs/Samples/InventoryExport-Sample.txt` with a normative contract (AM-03), and missing metadata is representable by preserving the imported `EQUID` and setting `MetadataMissingFg`, with no sentinel and no enforced FK for unresolved rows (AM-01/AM-02). The import location vocabulary, selection payload, warning sink and spell-merge provenance are resolved by the approved PD3 decisions. Remaining soft gaps are setup recovery (E-02), Items loader ownership/freshness and production grants (E-01), general audit taxonomy/UI (E-09), secret-at-rest review (E-11) and future HTMX semantics if that UI is adopted (E-16).

The platform skeleton, the owner-approved Phase 1 identity/settings layer, the
Phase 2 item-metadata integration, the Phase 3 import pipeline and the Phase 4
public inventory read path are implemented and validated: `go build`,
`go vet`, `go test ./...`, SQLite migration/rollback tests, parser golden tests
against the 548-line sample, commit-plan and summary tests, multipart CSRF and
import handler tests, public inventory service/repository/API/HTML security
tests, and the live Compose app + MariaDB identity/settings, import and public
inventory flows. Users, Settings and Sessions exist under `user_version=1`;
passwords are bcrypt hashes and only session-token hashes are stored.

The guild store now has embedded migrations for `ImportBatch`, `ImportList`,
`GuildBankItems` and `Audit`, plus the approved index-only public-inventory
migration. Phase 3 supports upload, durable parse failures, normalized staging,
a selector derived from distinct `LocationGroup` values (with every `Hoard<n>`
collapsed to `Hoard`), persisted selection, unit-aware summary, and an
advisory-locked atomic commit that expands item units and merges spells into
the lowest active EQUID target. Phase 4 exposes those records anonymously with
bounded search, class/race/type filters, natural/date sorts, pagination,
UTC availability states, EQResource links and a dedicated public DTO. Import
audit rows are written in the commit or staging transaction. Manual inventory
administration, the general audit UI/outbox, user management and dashboard
remain later phases; Phase 5 has not started.

A development copy of the item metadata is available as `GuildBank.items`
(144,712 rows, read-only in use). The profile in DP-001 confirms both permanent
fixtures (`Blade of Carnage` and `Flayed Skin Cap`), even though the physical
table is lowercase with `classes`/`races` text columns and contains 570 class /
628 race masks above the specified 16-bit range. PD-10 through PD-20 are
approved; PD-19 (repository mapping) and PD-20 (wide-mask policy) are
implemented. PD3-01 through PD3-14 and all six Phase 3 plan defaults are
approved, and Phase 3 is complete. The opt-in live test
scanned every row and verified those counts plus the missing-EQUID path. The
E-14 warning destination is implemented for imports as `ImportList.WarningText`.
The authenticated live Compose upload → selection → commit passed on
2026-11-22 UTC, including durable inventory, warnings, missing-metadata fallback,
spell merges and audit records (§1.5.1). Production `SELECT`-only `Items` grants
remain an operational follow-up. PD-05 remains the runtime MariaDB bootstrap;
the wizard persists connection settings but does not hot-swap the live pool.