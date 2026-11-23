# Project Status — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-001 |
| As-of date | 2026-11-22 |
| Source specification | `Design.md` v1.6.1 (1,274 lines: body L1–L854, decisions D-01–D-13 at L855–L1161, amendments AM-01–AM-05 at L1163–L1263, informational implementation status at L1265–L1274) |
| Repository state | Phase 1 identity/settings layer, Phase 2 item-metadata integration, Phase 3 import pipeline, Phase 4 public inventory read path and Phase 5 administrative inventory management are implemented. Administrative add/edit/remove/restore, the audit viewer, the location backfill, the owner-reviewed UI amendments, the post-Phase-5 import/admin usability improvements, the follow-up import-review adjustments (IMP-A/IMP-B/IMP-C), the DEFECT-01 spell metadata correction and the item quantity model (QTY-01–QTY-12) are implemented and live-validated on the Compose development topology (`Phase5Validation.md`, `ImportAdminImprovements.md`, `ItemQuantityModelImplementation.md`). Partial-issuance Phase 1 schema (migration `0006_inventory_movement.sql`) and Phase 2 domain/repository support are implemented; Phase 2 awaits owner review before Phase 3 |
| Documents issued | `ArchitectureReview.md` (AR-001), `DatabaseDesign.md` (DD-001), `ImplementationPlan.md` (IP-001), `ProjectStructure.md` (PS-002), `ItemsDataProfile.md` (DP-001), `Phase3ImplementationPlan.md`, `Phase4ImplementationPlan.md`, `Phase4Validation.md`, `Phase5ImplementationPlan.md`, `Phase5ApprovalReview.md`, `Phase5Validation.md`, `PublicInventoryAPI.md`, `ItemQuantityModelReview.md`, `ItemQuantityModelImplementation.md`, `PartialIssuanceImplementationPlan.md`, `PartialIssuanceApprovalPackage.md`, this status report |
| Overall status | **v1.6.1 published; Phases 0–2 approved and complete; Phase 3 approved, implemented and live-validated; Phase 4 approved, implemented and live-validated; Phase 5 approved, implemented and live-validated, awaiting owner review of the implementation and the one open availability-boundary wording question; partial-issuance Phase 1 schema and Phase 2 domain/repository layer implemented and awaiting owner review before Phase 3** |

---

## 1. As-is snapshot

| Area | State | Evidence |
|---|---|---|
| Requirements | Written; v1.6.1 amendments appended; canonical import sample at its declared path | `Design.md` v1.6.1 |
| Architecture review | Complete, revised | AR-001 rev 1.3: 22 missing requirements, 25 conflicts, 22 security risks, 28 implementation concerns, D-01–D-13 and AM-01–AM-05 adopted, E-01–E-16 registered, Phase 3 dispositions recorded |
| Data design | Complete and approved for the implemented scope | DD-001: DDL for 4 SQLite tables (`AuditOutbox` remains a later-phase addition), 6 MariaDB tables (`Audit` specified by D-10, `MetadataMissingFg` by AM-02, `InventoryMovement` added by partial-issuance Phase 1); the guild-store tables match migrations `0001_guild_store` through `0006_inventory_movement` |
| Delivery plan | Complete | IP-001: 9 phases, estimates, test and deployment strategy |
| Source code | Phases 1–5 are present | `cmd/guildbank`, `internal/{domain,service,repository,api,web,platform}`; setup, auth, sessions, settings, read-only item metadata, import parser/staging/selection/commit, public inventory list/detail/API, admin inventory CRUD/audit viewer, location and spell-metadata backfills, MariaDB migrations `0001`–`0006` and the import/public/admin UI |
| Build/CI | Complete for the current scope | `go.mod`/`go.sum` (Go 1.27.3), `Makefile`, `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml` |
| Environments | Development topology defined; item metadata provisioned | Compose app + MariaDB 11.4; 144,712-row read-only `items` copy in `GuildBank`; disposable credentials; no production environment |
| Test assets | Canonical import sample, development item metadata plus Phase 0–5 tests | `Docs/Samples/InventoryExport-Sample.txt` (548 physical lines); `GuildBank.items` profile in DP-001; tests for config, logging, app, sqlite migrations/repositories, mariadb item mapping/batch/page reads, admin SQL builders and startup repair helpers, mask decoders, metadata resolution, parser/natural sort/location normalization, commit planning (items and spells), multipart CSRF, import handlers (including the Other exclusion), admin service/API/decoder contracts, admin/public templates and the migration hash manifest; opt-in full-table Items test |
| Deployment | Configured, not deployed | Multi-stage static image, non-root UID 10001, `/config` volume, `EXPOSE 8080`, healthcheck `/healthz` |
| Operations | Health/logs and session purge implemented; backup/support pending | `/healthz`, `/readyz`, JSON logs with request IDs, hourly session purge, graceful shutdown with WAL checkpoint |

**Maturity by capability** (specification and implementation are tracked separately, because a well-specified feature can still be unimplemented):

| Capability | Specification | Implementation | Blocker |
|---|---|---|---|
| Setup wizard | Partial — gate/order implemented; recovery undefined | **Implemented** for the four-step flow | E-02 offline recovery |
| Authentication/sessions | Defined — lifecycle approved as PD-15 | **Implemented** (login/logout, hashed sessions, expiry, CSRF, lockout) | — |
| Public inventory browsing | Defined; PD4-01–PD4-17 approved | **Implemented** — public list/detail/API, search, class/race/type filters, eight sorts, pagination, UTC availability, EQResource links and public DTO | Proxy-level anonymous rate limiting remains operational; no in-app limiter |
| Class/race decoding | Defined with permanent fixtures; PD-19/PD-20 approved | **Implemented** repository-independent decoders plus service metadata generation; import commit stores the decoded strings for item and spell rows and import warnings persist to `ImportList.WarningText`; `BackfillSpellMetadata` repairs pre-DEFECT-01 spell rows at startup | — |
| Inventory administration | Partial — dev metadata loaded; loader procedure/freshness open | **Implemented** — manual create (one row with the entered quantity under QTY-07, spell merge, duplicate warning, metadata fallback), edit including item quantity (QTY-08), remove, restore, filters/sorts, admin HTML/JSON routes and audit viewer; rows absorbed by QTY-09 consolidation cannot be restored | E-01 (loader/freshness) remains operational |
| Import pipeline | Approved by `Phase3ImplementationPlan.md`; D-04/AM-03 contract, D-08/AM-01/AM-02 metadata rule | **Implemented** — upload, durable failures, normalized staging, `LocationGroup` selector, persisted selection, unit-aware summary, atomic commit, `409` terminal state and admin UI/API. One item row per staged line carries the line quantity; later imports of the same EQUID create separate rows (QTY-01/QTY-03). Rows classified `Unspecified` stay staged but are excluded from review, selection and commit (IMP-C) | Production grants and real collector exports remain operational follow-ups |
| Spell merge/lifecycle dates | Defined by D-06; import provenance resolved by PD3-03/PD3-04/PD3-14 | **Implemented for imports and admin writes** — merge by active EQUID into the lowest active row, staged/merged quantity sum, `ITEM_UPDATED` audit provenance; admin create/update applies the PD5-27/PD5-32 date rule (defaults from Settings, per-row override, non-retroactive) | Live `Bazaar` label branch cannot be produced by a new row and the availability boundary wording is open (see §1.7) |
| Settings | Partial — typed/secrets implemented; policy review open | **Implemented** (read/update/redaction) | E-11 |
| User management | Partial — policy/last-admin rules | Not started | E-07 |
| Audit | Schema defined by D-10; taxonomy open | Import-scoped and admin writes are transactional (`IMPORT_CREATED`, `IMPORT_COMMITTED`, `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED`, with restore recorded as `ITEM_UPDATED` plus notes) and exposed through a filterable admin audit viewer with per-record history; the `AuditOutbox` and general taxonomy remain later-phase | DC-25, E-09 |
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
| Atomic commit | Complete | Advisory lock and `FOR UPDATE`; one item row per imported line with the line quantity (QTY-01/QTY-03); spell aggregation and merge into lowest active ID; metadata/date rules; 100,000-record guard; `Completed` terminal state and one-retry deadlock handling |
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
are public fields. Phase 5 was not authorized at that checkpoint; it was
subsequently approved and implemented (§1.7), including the
`DateOpen <= DateBazaar` validation for all new writes.

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
filtering/normalization or availability-date filtering; those were delivered by
Phase 5 (§1.7). In-app anonymous rate limiting remains operational work.
`Phase3ImplementationPlan.md` remains the design record for Phase 3 choices.
Phase 4 is implemented and live-validated; Phase 5 followed and is recorded in
§1.7 and `Phase5Validation.md`.

---

### 1.7 Phase 5 administrative inventory management (implemented and live-validated 2026-11-22)

The owner approved PD5-01–PD5-40, reaffirmed the lifecycle date rule and
issued the UI amendments recorded in `Phase5ImplementationPlan.md` §9.5. PD5-40
(the final approval pass) made the availability thresholds inclusive, so the
`Bazaar` state begins on the `DateBazaar` day itself. All
code below is in the working tree and was validated end to end on the Compose
development topology; `Docs/Phase5Validation.md` records the evidence, the
screens captured and the items still outstanding.

| Deliverable | State | Evidence |
|---|---|---|
| Migration | Implemented | `internal/repository/mariadb/migrations/0003_admin_inventory.sql` adds nullable `LocationGroup VARCHAR(64)`/`LocationSort VARCHAR(80)` after `SourceLocation`, three admin indexes and `ix_audit_correlation`; it is the only migration copy and is embedded by the runner |
| Migration manifest gate | Implemented | `migration_files_test.go` pins the exact filename set, version prefixes and SHA-256 of `0001`–`0003` |
| Startup backfill | Implemented | `Store.BackfillInventoryLocations` uses a dedicated advisory lock, keyset batches of 500, one transaction per batch, fills only the derived columns, never touches `SourceLocation`/`ModAt`/audit and is restartable; wired in `app.Run` after migration/recovery and before serving |
| Location normalization | Implemented | One `normalizeLocationValue` computation behind `NormalizeLocation` (import contract unchanged) and `NormalizeAdminLocation` (adds `location_unrecognized`); unit tests cover the D-05 vectors and identical group/sort |
| Shared row insert | Implemented | `insertInventoryRows` is used by import commit and admin create with one 19-column list including `LocationGroup`/`LocationSort`; `GuildBankItem` carries both staged fields |
| Domain/service | Implemented | `internal/domain/inventory_admin.go`, `internal/domain/audit.go`; `AdminInventoryService` owns list/get/location groups, create (one row with the entered quantity under QTY-07, spell merge, duplicate-target warning, metadata-missing fallback), update (quantity for both types under QTY-08, immutable fields, date reset), remove/restore (absorbed-row restore rejected), audit list/get, deadlock retry once and one audit row per write |
| Repository | Implemented | `admin_inventory.go`, `admin_audit.go` add explicit projections, allowlisted sort/filter predicates, repeatable-read snapshot reads, the dedicated-lock write transaction and nullable-column scans |
| API | Implemented | `internal/api/admin_json.go` implements the strict 64 KiB decoder (content type, duplicate-key pre-pass, `DisallowUnknownFields`, single-value check) and `admin_inventory.go`/`admin_audit.go` implement the documented JSON routes, field-keyed `422`, `409` conflicts, `404`/`413`/`503` mappings and fixed `Location` header |
| HTML admin UI | Implemented | `/admin/inventory`, `/new`, `/{id}`, `/{id}/edit`, `/{id}/remove`, `/admin/audit` plus POST adapters; server-rendered forms with CSRF, confirmation page, fixed notices, conflict banners, editable quantity for both types and nav links |
| Audit viewer | Implemented | Admin-only newest-first viewer with `eventType`/`user`/`table`/`recordId`/`correlationId`/`from`/`to` filters, page size 50/200 cap and per-record history panel |
| Correlation-ID bound | Implemented | `requestIDPattern` narrowed to `{8,36}` so `Audit.CorrelationID CHAR(36)` cannot break a valid write (PD5-31) |
| UI amendments | Implemented | Responsive 120 rem content width, `ID`/`EQUID` removed from the public grid only, one compact multi-column filter grid per screen, icon-only EQResource link with `title`/`aria-label`/`noopener noreferrer`, nowrap density above 900 px, and `Mains`/`Persona`/`Bazaar` availability labels with the three specific dates on the detail page. The label boundary is inclusive per PD5-40: `Persona` while `DateOpen <= today < DateBazaar`, `Bazaar` from the `DateBazaar` day on |
| Unit/UI tests | Implemented | Service tests for create/merge/update/remove/restore, dates, filters and location wrappers; API tests for the strict decoder, optional date/string contract and labels; `internal/web` tests pin the UI amendments and admin template contract; migration manifest test |
| Validation commands | Pass (except `-race`) | `gofmt -l cmd internal` prints nothing, `go vet ./...` is clean and `go test -count=1 ./...` passes for every package. `go test -race ./...` cannot run in this Windows workspace (`-race` requires cgo and no C compiler is installed), so the race gate must be executed by CI |
| Live validation | **Passed 2026-11-22** | Fresh SQLite setup, `0003` applied over the existing 706-row store, backfill completed with `rows=706` and no `ModAt`/`Audit`/data change, admin create (quantity 2 → two rows), HTML/JSON edit, remove, restore, audit viewer, date-rule rejections, settings non-retroactivity, location `Unspecified` warning and the UI amendments all verified; evidence in `Docs/Phase5Validation.md` |
| Outstanding validation | Pending | §10.1 performance gates (250k/1M fixtures, backfill throughput, 100k-unit create), §13.2/§13.3 opt-in suites, the CI `-race` run and the §13.5 manual accessibility checklist |

Known deviations and open items:

- The per-10,000-row backfill progress log named in the plan is not emitted; the
  startup logs record start and completion with the updated row count. The
  batch size, lock, resume behavior and idempotence are as planned.
- `mapStoreError` now maps `1213` deadlock to the retryable error and `1205`
  lock-wait timeout to `ErrUnavailable`, so only a deadlock is retried once
  (§6.6); import commit shares the mapping.
- The admin route handlers are covered by the existing admin-group
  authorization middleware; a dedicated route-by-route anonymous/member/admin
  matrix test is not yet added.
- Database integration and performance tests (`GUILDBANK_INTEGRATION`,
  `GUILDBANK_PHASE5_PERF`) are planned but not written in this change.
- **Availability boundary (ruled by the owner, PD5-40, 2026-11-22):**
  `UI-07`'s "when the open-to-bazaar date is reached" is normative, so
  `service.Availability` now returns `bazaar` on the `DateBazaar` day itself.
  `Mains` while `today < DateOpen`, `Persona` while
  `DateOpen <= today < DateBazaar`, `Bazaar` when `today >= DateBazaar`; equal
  `DateOpen`/`DateBazaar` values leave no `Persona` day, and an inverted window
  is still never `open`. The public JSON `availability` value therefore moves
  from `open` to `bazaar` on that one day, superseding `PD5-37`'s
  "no public JSON change" clause for that day; nothing else in the contract
  changed. Implemented, unit-tested and live-verified in
  `Phase5Validation.md` §11.
- The downstream documentation corrections of §12.5 and the owner review of the
  implementation remain outstanding.

### 1.8 Post-Phase-5 UI defect corrections (2026-11-22)

Two defects found by owner review of the running Compose deployment were fixed
in a contained change; no new phase or feature work was started.

| Defect | Root cause | Correction |
|---|---|---|
| `/admin/settings` returned `200` with a zero-byte body (blank page) | `settings.html` assembled one `<input>` tag with a `{{if}}`/`{{else if}}` chain *inside the tag* to choose `type`/`min`/`max`. `html/template`'s contextual escaper rejects an action in attribute-name position, and because the error surfaces at execute time — after `views.render` had already written the status and content type — the response was a `200` with an empty body and no visible error. No test rendered the settings page, so the gate never caught it | Each input variant is now a complete static tag selected by an `{{else if eq .Key …}}` branch; no dynamic attribute names remain. `views.render` additionally buffers the template output and answers `500 internal_error` (logged through Gin's error channel) if execution fails, so a future template error cannot produce a blank page |
| Anonymous visitors saw `Inventory` but no way to sign in | The `nav` partial only rendered authenticated entries and the sign-out form, so the login page was reachable only by typing its URL | The partial renders `Login` (`/login`) when `.User` is nil and keeps `Inventory`, `Settings`, `Admin inventory`, `Imports`, `Audit` and `Sign out` for authenticated sessions; `Audit`/`Imports` are now ordered as documented |

Authorization is unchanged: `/admin/*` is still `RequireAuth → CSRF →
RequireAdmin`, anonymous admin requests still answer `303 /login?next=…`, and
the nav only *links* to routes the middleware guards.

Validation (2026-11-22): `gofmt -l cmd internal` prints nothing, `go vet ./...`
is clean, `go test -count=1 ./...` passes including the new
`internal/api/navigation_test.go` regression tests (anonymous nav exposes only
`Inventory`/`Login`, administrator nav order and `Sign out`, settings page body
non-empty with all eleven inputs, `?saved=1` notice). Live on the rebuilt
Compose app: anonymous `GET /` `200` with `Inventory`+`Login` and no `/admin`
links; `GET /login` `200`; `POST /api/v1/auth/login` as `phase5admin` `200`;
`GET /admin/settings` `200` with a full body (heading, CSRF field, eleven
inputs, `Configured — leave blank to keep` on the MariaDB password);
`GET /admin/inventory`, `/admin/imports` and `/admin/audit` `200`, and
`/admin` `303 /admin/settings`, as designed for an administrator; anonymous
`GET /admin/settings` and `/admin/inventory` still `303` to
`/login?next=…`.

### 1.9 Post-Phase-5 import and admin usability improvements (2026-11-22)

The owner approved the review of eight post-Phase-5 items and implementation was
carried out in a contained change; the Phase 3 commit path, the import advisory
lock, the Phase 5 single-record contracts and the closed audit vocabulary are
unchanged. Full evidence is in `Docs/ImportAdminImprovements.md`.

- **IMP-01 (latent defect)** — a populated row at a nested bank slot
  (`General 1-Slot2-Slot1`) is no longer staged as `Unspecified`; it is ignored
  and counted in the new nullable `ImportBatch.IgnoredRowCount`
  (migration `0004_import_ignored_rows.sql`), the upload result and the
  `IMPORT_CREATED` audit payload. The canonical sample is unchanged: its 139
  nested rows are empty (`ID 0`) discards, so ignored = 0 and accepted = 197.
- **IMP-02/IMP-03** — `Unspecified` is presented as **Other (not a bank
  location)** in admin inventory and is never selected by default; the review
  grid adds a **Kind** column (`Slot`/`Container`). The import review itself no
  longer exposes the group or its banner (superseded by IMP-C, §1.10).
- **IMP-04/IMP-05** — staging now applies one default rule: container rows (a bag
  occupying a slot) are not selected in General, Bank and SharedBank; all Hoard
  rows are selected; Unspecified is never selected. A new
  `GET /api/v1/imports/{id}/default-selection` plus **Reset to defaults** lets an
  existing batch adopt the rule explicitly. Existing batches are not rewritten.
  For the sample the default selection is 138 rows, down from 197. After IMP-C
  the review exposes 166 rows (197 accepted minus 31 Other) in 29 groups.
- **IMP-06** — the location panel is the page-local group scope: unchecking a
  group hides its rows and clears them from the selection, and the `Unselected`
  filter ignores the scope so those rows remain reviewable.
- **AI-01** — page-local bulk removal only: a checkbox column (≤200 rows), a
  preview page, then `RemoveMany` in one all-or-nothing transaction with
  ascending-ID `FOR UPDATE` locks and one `ITEM_REMOVED` audit row per record
  sharing the request correlation ID. No filter-wide or inventory-wide action,
  and no new event type.
- **AI-02** — **Save and remove** performs the edit and the soft delete in one
  transaction, writing `ITEM_UPDATED` only when fields changed plus
  `ITEM_REMOVED`, both under one correlation ID; restore does not undo the edits.

Validation (2026-11-22): `gofmt -l cmd internal` prints nothing, `go vet ./...` is
clean and `go test -count=1 ./...` passes, including new parser/selection, admin
bulk, template and migration-manifest tests. The rebuilt Compose app applied
`schema_version=0004_import_ignored_rows` and the live probe passed: sample upload
197 accepted / 0 ignored / 138 default-selected, group-scope counts of 59 and 163,
the new review/admin markup present, AI-02 returning `deleted + fieldsChanged`
with `ITEM_REMOVED`+`ITEM_UPDATED` sharing one correlation ID, AI-01 preview/apply
removing both records under one correlation ID, and the `2 records were removed.`
notice. Probe records are soft-removed; no batch was committed, so inventory is
unchanged. Evidence is in `ImportAdminImprovements.md` §6.

### 1.10 Post-Phase-5 follow-up review and DEFECT-01 (2026-11-22)

The owner accepted the Scope concept (IMP-A) and requested two changes, and
reported a high-priority spell-metadata defect. All work is contained; no phase
was reopened.

- **IMP-B** — the import-review location panel adds **Check all locations** and
  **Uncheck all locations** above the existing scope actions. They change only
  the page-local location-group checkboxes; row selection, **Select all in
  scope**, **Clear scope** and **Reset to defaults** are unchanged.
- **IMP-C** — the `Unspecified` classification is internal only. A single
  repository predicate excludes it from the location selector, the staged-row
  list, `row-ids`, the persisted selection, the summary and the commit; the
  selection API rejects an Other row with `422`; the Other banner and label are
  removed from the import review. Admin inventory keeps its Other label.
  Staging counts are unchanged: the sample is still 197 accepted, but the review
  exposes 166 rows in 29 groups and the default selection remains 138.
- **DEFECT-01 (spell metadata)** — root cause: `buildCommitPlan` resolved
  `ItemMetadata` for spell rows but its new-spell insert copied only the staged
  name, never `ClassString`/`RaceString`. Metadata lookup is by EQUID (never by
  name) and the `Spell:`/`Song:`/`Ancient:` prefixes are classification-only and
  never stripped. With blank stored strings the DTO/template layer rendered
  **Unknown**; the data was missing, not the decode. The commit plan now applies
  the same metadata rule as items (authoritative Items name, decoded class/race,
  blanks only when the EQUID is unresolved), and the startup
  `BackfillSpellMetadata` repairs existing spell rows. Merges still never
  rewrite an existing row's stored metadata or dates.

Validation (2026-11-22): `gofmt -l cmd internal` prints nothing, `go vet ./...`
is clean and `go test -count=1 ./...` passes, including new tests for the Other
exclusion, the IMP-B markup and spell metadata planning/backfill. The rebuilt
Compose app logged `spell metadata backfill complete rows=7`; `Ancient: Lcea's
Lament` (739) now renders `BRD` / `All races` instead of Unknown, `Spell:
Transon's Phantasmal Protection` (730) renders `MAG` / `All races`, and a fresh
two-row commit created `Spell: Warder's Protection` (`RNG`) and `Song: Magical
Monologue` (`BRD`) with `ALL` races; the two probe records were then soft-removed,
so the active inventory count is net-neutral (712 active / 29 soft-deleted).
Sample batch 12 returned 29 groups (no `Unspecified`), 166 rows, `stagedRows`
166 and 138 default-selected, and the review page contained both IMP-B buttons
with no Other banner. Evidence is in `ImportAdminImprovements.md` §7–§8.

### 1.11 Partial-issuance Phase 2 domain and repository (implemented 2026-11-22)

The owner approved Phase 1 and directed Phase 2 to proceed without service,
API, UI, audit-workflow or business-operation changes. The implemented layer is:

- `internal/domain/movement.go` defines the approved `MovementType` closed set,
  `InventoryMovement`, bounded `MovementFilter`/`MovementPage`, `Stackability`
  and the approved issue/return/adjust/move input shapes. No service operation
  consumes those inputs yet.
- `internal/repository/mariadb/movement.go` provides the append-only ledger
  read and write surface: explicit projection, bounded newest-first list, get by
  ID, and insert through the existing `BeginAdminWrite` transaction. The
  repository exposes no update or delete statement for `InventoryMovement`.
- `SumActiveQuantity` implements the approved on-hand definition as
  `SUM(Quantity)` over active `GuildBankItems` rows; it never reads or adds the
  movement ledger.
- `internal/repository/mariadb/stackability.go` implements the temporary OD-01
  EQUID allow-list contract. An absent EQUID is `unknown`; no partial issue is
  permitted until metadata coverage or the allow-list supplies `stackable`.
- `AdminStore`/`AdminWriteTx` gained the movement, on-hand and stackability
  contracts. Existing inventory workflows and their interfaces are otherwise
  unchanged.

Validation (2026-11-22): `gofmt` is applied to all touched Go files, `go vet
./...` is clean and `go test ./...` passes. New tests cover the movement type
allowlist, row validation, stackability classification, bounded filter
normalization, parameterized ledger predicates, append-only repository source
assertions and the temporary allow-list. No live database write was performed
because the development container remains at migration `0005`; repository SQL
will be exercised when the approved `0006` migration is deployed.

---

## 2. Documents produced in this review

| Document | Contents | Key outputs |
|---|---|---|
| `Docs/ArchitectureReview.md` | Conformance assessment, findings, decisions, v1.6 disposition | MR-01–MR-22, DC-01–DC-25, SR-01–SR-22, IC-01–IC-28, A-01–A-12, E-01–E-16, D-01–D-13 mapping |
| `Docs/DatabaseDesign.md` | Physical schema, constraints, transactions, grants, retention | SQLite DDL (Users, Settings, Sessions, AuditOutbox), MariaDB DDL (Items, GuildBankItems, ImportBatch, ImportList, Audit), 9 workflow specifications, D-01–D-13 conformance |
| `Docs/ImplementationPlan.md` | Phased delivery, API surface, audit taxonomy, testing, deployment | 9 phases (58–84 engineer-days with contingency), route table, D-10 event taxonomy, fixtures, DoD, risk register |
| `Docs/ItemsDataProfile.md` | Read-only profile of the development item metadata | 144,712-row inventory, physical/logical mapping, mask distribution, decoding fixture, PD-19/PD-20 |
| `Docs/ProjectStatus.md` | This report | Traceability, blockers, next actions |
| `Docs/Phase3ImplementationPlan.md` | Approved import-pipeline plan | PD3-01–PD3-14, staging/commit design, import contracts, test gates |
| `Docs/Phase4ImplementationPlan.md` | Approved public read-path plan | PD4-01–PD4-17, public DTO/query contract, index plan, perf gates |
| `Docs/Phase4Validation.md` | Phase 4 evidence | Live Compose validation, `EXPLAIN` proof, 250k/1M performance results |
| `Docs/Phase5ImplementationPlan.md` | Approved administrative-inventory plan | PD5-01–PD5-39, §9.5 owner UI amendments, schema/backfill/API/service/audit design, §13 test gates, §16 residual dispositions |
| `Docs/Phase5ApprovalReview.md` | Phase 5 decision rationale | Per-decision rationale, tradeoffs, future-phase impact, risk classification, §1.2 batch justification for PD5-33–PD5-39 |
| `Docs/Phase5Validation.md` | Phase 5 evidence | Migration/backfill proof, schema and `EXPLAIN` evidence, live admin CRUD/audit/date-rule/UI results, latency observations, open boundary question |
| `Docs/PublicInventoryAPI.md` | Public read-path contract | Routes, filters, sorts, DTO fields, availability values, error envelope |
| `Docs/ImportAdminImprovements.md` | Post-Phase-5 improvement record (IMP-01–06, AI-01–02) | Decisions, implementation map, import count changes, audit impact, validation evidence |

Phases 2–5 are implemented and validated, and each has a plan and (from Phase 3 on) a validation record. Phase 0 created the buildable service, configuration, logging, both connection layers, health endpoints, deployment files and tests. Phase 1 added the SQLite schema, setup, authentication, sessions, CSRF, login lockout, settings and the first screens; the full file map is in `Docs/ProjectStructure.md`.

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
| FR-07 | Admin add inventory | L37–L39, L741–L745 | Resolved by PD5-01–PD5-32; QTY-07 | **Implemented** (one row with the entered quantity, spell merge, metadata fallback, dates) | DD §7.2, `Phase5ImplementationPlan.md`, `ItemQuantityModelImplementation.md` |
| FR-08 | Admin update inventory | L37, L744 | Resolved by PD5-27/PD5-32; QTY-08 | **Implemented** (immutable identity, editable quantity for both types, per-row dates, `422` field errors) | DD §7.3, `Phase5ImplementationPlan.md`, `ItemQuantityModelImplementation.md` |
| FR-09 | Admin remove inventory (soft delete) | L38, L359–L368 | Resolved by PD5 (remove/restore) | **Implemented** (soft delete, restore, audit row per write, public exclusion) | DD §7.3, `Phase5ImplementationPlan.md`, `Phase5Validation.md` |
| FR-10 | Import inventory upload | L41, L607–L630; D-04, AM-03 | Defined | **Implemented** | AR MR-02, IP Phase 6, `Phase3ImplementationPlan.md` |
| FR-11 | Review import, deselect rows/locations | L646–L670; D-11 | Defined | **Implemented** | DD §7.9, `Phase3ImplementationPlan.md` |
| FR-12 | Commit import with quantity preservation | L673–L698; D-06, D-07; QTY-01/QTY-03 | Defined | **Implemented** (one row per item line, spells merged) | DD §7.5, `Phase3ImplementationPlan.md`, `ItemQuantityModelImplementation.md` |
| FR-13 | Retain import history | L549–L556 | Defined | **Implemented** (batches, staging and terminal states retained) | DD §4.3–4.4, §10.4, `Phase3ImplementationPlan.md` |
| FR-14 | Manage settings | L35, L190–L214 | Partial (types/secrets) | **Implemented** (read/update/redaction) | DD §3.2, IP Phases 2–3 |
| FR-15 | Manage users | L43, L756–L763 | Partial (policy/delete) | Not started | DD §3.1, §7.7, IP Phase 7 |
| FR-16 | Audit trail (items, imports, users, settings, auth) | L773–L783; D-10 | Partial (general taxonomy open) | **Import-scoped and inventory admin audit implemented with a filterable viewer and per-record history**; `AuditOutbox` and the general taxonomy are not started | DD §4.5, IP §5, PD3-14, `Phase5Validation.md` |
| FR-17 | First-run wizard (4 steps) | L130–L164; D-02 | Partial (recovery) | **Implemented** | DD §7.1, IP Phase 2 |
| FR-18 | Public + six admin screens | L805–L825 | Partial (dashboard contents) | **Implemented** for public inventory, admin inventory, admin audit, settings and imports; the dashboard screen is not started | IP §4, Phases 4–7, `Phase5Validation.md` |
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
| Performance below expectation at scale | Medium | Medium | Medium | Indexes, persisted sort keys, Phase 4 gates passed at 1M; Phase 5 §10.1 admin/audit gates and the backfill benchmark are still to be measured | Dev |

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
| D-07 item import | **Adopted; amended 2026-11-22 by QTY-01/QTY-02** | Never merge; one row per imported line with that line's quantity. Manual create follows the same rule (QTY-07) |
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
| E-06 | Unknown location patterns/vocabulary | **Resolved for imports and admin writes** | PD3-01: equipment/blank/unknown → `Unspecified` with warnings; all `Hoard<n>` → `Hoard` (AM-04); PD5 shared normalization adds `location_unrecognized` for admin writes; IMP-01 ignores nested-slot contents before staging; IMP-02 presents `Unspecified` as "Other (not a bank location)" without changing the stored value |
| E-07 | Authorization matrix/projections | Partial | Admin-only routes enforced (setup/auth/settings/imports/inventory/audit); public inventory projection implemented in Phase 4; the user-management routes remain a later phase |
| E-08 | Session lifecycle | **Implemented; approved as PD-15** | 8 h absolute, 30 min idle, sliding refresh, purge; lifecycle review may refine values |
| E-09 | Audit taxonomy closure | Partial | Import and inventory-admin subsets closed by PD3-14/PD5 (`IMPORT_CREATED`, `IMPORT_COMMITTED`, `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED`) with a filterable viewer; IMP-01 adds `ignoredRows` to `IMPORT_CREATED`; AI-01/AI-02 reuse the existing event types (one `ITEM_REMOVED` per bulk record; `ITEM_UPDATED` + `ITEM_REMOVED` for a combined edit/remove); general taxonomy/`AuditOutbox` open |
| E-10 | Date comparison semantics | **Resolved by PD-11/PD4-11/PD5-27/PD5-40** | UTC date boundaries; `DateOpen <= today < DateBazaar` means open and `today >= DateBazaar` means bazaar (both thresholds inclusive, PD5-40); `DateOpen <= DateBazaar` enforced for all new admin writes |
| E-11 | Settings validation/secrets | **Implemented; PD-11/PD-18 approved** | Typed settings, redaction, volume-permission reliance; secret-at-rest review remains |
| E-12 | API versioning/pagination | **Resolved** | `/api/v1`, import paging and the public inventory pagination contract (Phase 4) are implemented; admin list/audit paging caps at 200 |
| E-13 | Missing-metadata strategy | **Resolved** | AM-01/AM-02: preserve id, set `MetadataMissingFg`, relax FK |
| E-14 | Warning sink | **Resolved for imports** | PD3-02: `ImportList.WarningText` stores structured warnings; batch counts surface them |
| E-15 | Selection payload contract | **Resolved for imports** | Full replacement, batch-scoped, `422 selection_invalid` for foreign/duplicate/non-positive ids |
| E-16 | HTMX error/auth semantics | Deferred | Implemented UI uses plain forms and vanilla `fetch`; revisit only if HTMX is adopted for inventory UI |

D-01–D-13 are specification decisions and do not require engineering approval. PD-10–PD-20 were approved on 2026-11-21; PD-10–PD-18 are recorded in `Docs/ProjectStructure.md` §4.2 and PD-19/PD-20 in §4.3. PD3-01–PD3-14 and all six Phase 3 plan defaults are also approved; the decoders, the two named fixtures and the import pipeline are part of the approved baseline. PD4-01–PD4-17 and PD5-01–PD5-39 are approved and implemented; PD5-27 (lifecycle date rule), PD5-31 (correlation-ID bound) and PD5-32 (per-row date editing) supersede nothing earlier and are recorded in `Docs/Phase5ImplementationPlan.md` §3, with the rationale in `Docs/Phase5ApprovalReview.md`. Phase 3 did not adopt a Settings-backed MariaDB pool, so PD-18 remains in force as an explicit later change.

### 6.7 Item quantity model (QTY-01–QTY-12, approved and implemented 2026-11-22)

| ID | Decision | Status |
|---|---|---|
| QTY-01 | One `GuildBankItems` row per imported item line with that line's quantity | **Adopted** |
| QTY-02 | Migration `0005` drops `ck_gbi_item_one`, keeps `ck_gbi_quantity`, adds no unique `EQUID` index and adds `ConsolidatedInto` | **Adopted** |
| QTY-03 | `buildCommitPlan` emits one item row per staged line; spell planning unchanged | **Adopted** |
| QTY-04 | Import guard and `InventoryRecordsToCreate`/`TotalRecordsToImport` count item rows; `ItemUnits` stays informational | **Adopted** |
| QTY-05 | Import audit keeps `itemUnits`, adds `itemRecords`; `TotalRecordsAffected` counts records | **Adopted** |
| QTY-06 | Review/help/form copy describes one record with a stored quantity | **Adopted** |
| QTY-07 | Manual item create writes one row with the entered quantity (1..100000) | **Adopted** |
| QTY-08 | Item quantity is editable (1..100000) and audited; the HTML form is editable | **Adopted** |
| QTY-09 | Option C: owner-run, dry-run-first `-consolidate-items`; lowest ID survives, quantity summed, absorbed rows soft deleted and audited, restore rejected; no data mutation in a migration | **Adopted** |
| QTY-10 | D-07/§One Row Per Imported Item Line, PD3-05/06/12/14, PD5-06/07/10, DatabaseDesign, ProjectStructure, R5-14 and this report amended | **Adopted** |
| QTY-11 | Spells, read projections, sorts, indexes and audit vocabulary unchanged | **Adopted** |
| QTY-12 | Migration and code ship together; consolidation only after a clean dry run; rollback is forward-fix only | **Adopted** |

Evidence and before/after examples are in `ItemQuantityModelImplementation.md`.

Live deployment and rehearsal on 2026-11-22: the rebuilt `guildbank:dev` image
applied `0005_item_quantity_model` to the development guild store and started
healthy (`/healthz` 200). The owner-run dry run
(`guildbank -consolidate-items -dry-run`) reported 39 duplicate groups, 795
absorbable rows, 834 units in those groups and 0 rows changed; the store
remained at 874 active item rows / 874 units pending the authorized real run.
The audit table gained no consolidation rows, confirming the dry run wrote
nothing. `0005` is recorded in `schema_migrations`, `ConsolidatedInto` exists,
and `ck_gbi_item_one` is gone while `ck_gbi_quantity` remains.

---

## 7. Next actions

| # | Action | Owner | Effort | Depends on | Output |
|---|---|---|---|---|---|
| 1 | Add two real collector exports beside the canonical sample | Guild admin + dev | external | — | Import fixtures (closes E-04) |
| 2 | Turn the loaded development `items` copy into a documented loader/freshness procedure and production `SELECT`-only grants | Operator + dev | 0.5–1 d | — | Loader runbook, verified least-privilege access (E-01) |
| 3 | Close the general audit taxonomy (E-09) and secret-at-rest review (E-11) before their phases; the public API/pagination contract (E-12) is closed by Phase 4 | Dev + product | 0.5–1 d | — | Approved records |
| 4 | Decide when Settings become the authoritative MariaDB configuration, including whether to hot-swap the pool (PD-18) | Product + dev | 0.5 d | — | Separate approved change; PD-05/PD-18 remain runtime behavior until then |
| 5 | Write ADRs for D-01–D-13, AM-01–AM-05, PD-10–PD-20, PD3-01–PD3-14, PD4-01–PD4-17 and PD5-01–PD5-40 | Dev | 1 d | — | `Docs/decisions/` records |
| 6 | Provision the target MariaDB and Unraid template | Operator + ops | 0.5 d | #2 | Deployment artifacts |
| 7 | ~~Decide the Phase 5 availability boundary wording~~ — **closed 2026-11-22**: the owner adopted inclusive thresholds (PD5-40), so `Bazaar` begins on the `DateBazaar` day; implemented and live-verified | Owner | 0 d | — | Recorded in `Phase5ImplementationPlan.md` §3 PD5-40/§9.5 and `Phase5Validation.md` §11 |
| 8 | Write the §13.2 database integration suite and the §13.3 performance fixture; run the §10.1 gates at 250k and 1M rows | Dev + ops | 2–3 d | — | `internal/integration` opt-in tests, recorded p95/`EXPLAIN` evidence |
| 9 | Execute the §13.5 manual acceptance checklist (keyboard, screen reader, 320 px, focus order) and the CI `-race` run | Dev + owner | 0.5 d | Phase 5 build | Completed checklist and CI run |

**Immediate next:** Phase 5 is approved, implemented and live-validated
(§1.7, `Phase5Validation.md`), including the PD5-40 boundary ruling; its
remaining items are #8–#9 plus the §12.5 documentation corrections. The owner
authorized the post-Phase-5 review items and they are implemented in §1.9
(`ImportAdminImprovements.md`), including the live Compose pass over the new
review and bulk-removal pages. Operational items #1–#6 continue independently.

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

Phase 4 is complete and live-validated. Phase 5 is approved, implemented and
live-validated (§1.7, `Phase5Validation.md`), with the §10.1 performance gates,
the §13.2/§13.3 opt-in suites, the CI `-race` run, the §13.5 manual checklist
and one availability-boundary wording ruling outstanding. No phase after Phase 5
has been authorized; the owner review of the Phase 5 implementation is the gate.

---

## 9. Assumptions and limitations of this document set

1. All statements are derived from `Design.md` v1.6.1; where the specification is silent, the gap is recorded rather than silently resolved.
2. Line references (`L###`) are valid for the body (L1–L854) and the decisions (L855–L1161) and were re-verified at the cited anchors; the v1.6.1 amendments are cited as `AM-xx` at L1163–L1263. The non-normative implementation-status block at L1265–L1274 does not change any requirement. Decisions D-01–D-13 have no per-decision line anchors (IC-26). Any further edit to `Design.md` requires re-checking.
3. The documents introduced **PROPOSED** schema elements and defaults. The decisions used by Phases 1–3 were approved on 2026-11-21 (PD-10–PD-20, PD3-01–PD3-14); the remaining `AuditOutbox` and general-audit additions are later-phase proposals, not current implementation requirements.
4. Application code covers the Phase 0 skeleton, Phase 1 identity/settings layer, Phase 2 item metadata, Phase 3 import pipeline, Phase 4 public inventory read path and Phase 5 administrative inventory management, including migrations, parser/commit tests, public and admin service/repository/API/template tests, security tests, the migration hash manifest and the Phase 4 performance gates. The Phase 5 §10.1 performance gates and the §13.2/§13.3 opt-in suites are not written; the general audit outbox and user-management tests do not yet exist; effort estimates remain planning ranges, not measured velocity.
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
| 2.3 | 2026-11-22 | Phase 5 approved, implemented and live-validated: PD5-01–PD5-39 (including the owner UI amendments PD5-33–PD5-39 and the reaffirmed lifecycle date rule), migration `0003_admin_inventory` applied over the existing 706-row store, startup backfill with no data/`ModAt`/audit change, admin create/edit/remove/restore with one audit row per write, filterable audit viewer with per-record history, strict 64 KiB JSON decoder, `DateOpen <= DateBazaar` enforcement, settings non-retroactivity, responsive/compact UI amendments and `Mains`/`Persona`/`Bazaar` labels; `gofmt`/`go vet`/`go test` clean; §10.1 performance gates, §13.2/§13.3 suites, CI `-race` and the §13.5 manual checklist outstanding, plus one availability-boundary wording ruling for the owner |
| 2.4 | 2026-11-22 | Phase 5 final approval with PD5-40: availability thresholds are inclusive, so `Bazaar` begins on the `DateBazaar` day (`Mains` while `today < DateOpen`; `Persona` while `DateOpen <= today < DateBazaar`; `Bazaar` when `today >= DateBazaar`). `service.Availability` changed one comparison, the service and API boundary tests were updated and extended, `PublicInventoryAPI.md`, `Phase4ImplementationPlan.md` PD4-11/§5.4/§7.3, `Phase5ImplementationPlan.md` PD5-37/PD5-40/§9.5 and `Phase5Validation.md` were aligned; three admin probe rows (718–720) verified `Bazaar`/`Persona`/`Mains` in HTML and JSON and were removed through the admin form; `gofmt`/`go vet`/`go test ./...` clean; final state 706 active rows, 29 audit rows. No post-Phase-5 work started |
| 2.5 | 2026-11-22 | Owner-approved post-Phase-5 review items implemented: IMP-01 nested-slot rows ignored and counted (`IgnoredRowCount`, migration `0004`); IMP-02/IMP-03 Other label, Kind column and banner; IMP-04/IMP-05 container-aware default selection with the Hoard exception and non-retroactive reset; IMP-06 page-local group scope with an unselected view that ignores it; AI-01 page-local bulk removal through a preview and one all-or-nothing transaction; AI-02 combined edit+remove in one transaction with `ITEM_UPDATED`+`ITEM_REMOVED` under one correlation ID. Audit vocabulary, import commit path and advisory lock unchanged; the migration hash manifest is now LF-normalized so the gate passes on both Windows and Linux; `gofmt`/`go vet`/`go test ./...` clean; live Compose verification passed. Recorded in `ImportAdminImprovements.md` and §1.9 |
| 2.6 | 2026-11-22 | Item quantity model approved and implemented (QTY-01–QTY-12): migration `0005` drops `ck_gbi_item_one` and adds `ConsolidatedInto`; import commit and manual create write one row with the line/entered quantity; later imports of the same EQUID do not merge; item quantity is editable/audited; spells keep merge-by-EQUID; owner-run `-consolidate-items` (dry-run first) folds legacy expanded rows into the lowest ID with summed quantity, soft deletes and per-row audit, and restore of absorbed rows is rejected; review, implementation summary, D-07/PD3/PD5/DatabaseDesign/ProjectStructure amendments recorded; `gofmt`/`go vet`/`go test ./...` clean. Recorded in `ItemQuantityModelReview.md` and `ItemQuantityModelImplementation.md` |
| 2.7 | 2026-11-22 | Live deployment of the item quantity model: rebuilt `guildbank:dev` applied `0005_item_quantity_model` to the development guild store and restarted healthy (`/healthz` 200); the owner-run dry run reported 39 groups / 795 absorbable rows / 834 units in groups with 0 rows changed, leaving 874 active item rows and 874 units pending the authorized real run; 0 consolidation audit rows confirm the dry run wrote nothing; `ConsolidatedInto` present, `ck_gbi_item_one` absent and `ck_gbi_quantity` retained |
| 2.8 | 2026-11-22 | Partial-issuance Phase 1 schema implemented: additive `0006_inventory_movement.sql` creates the append-only `InventoryMovement` ledger with the approved indexes and constraints; migration manifest hash and migration assertions updated; no service, API, UI, audit or movement workflow code changed; no existing lot data, quantity or `GivenTo` value mutated. Phase 2 remains gated on owner review |
| 2.9 | 2026-11-22 | Partial-issuance Phase 2 domain and repository implemented: movement types, append-only ledger read/write contracts, bounded newest-first history reads, on-hand `SUM(Quantity)` support, temporary EQUID stackability allow-list and the existing admin transaction helpers were extended; no service, API, UI, audit-workflow or business operation was added. `gofmt` applied, `go vet ./...` clean and `go test ./...` passing; no live database write because the development container remains at `0005`. Phase 3 remains gated on owner review |

---

## 11. Status summary

The GuildBank specification is **buildable as written**. v1.6 closed the audit schema, the selection route, the HTMX/REST layering model, spell merge and item identity, and defined session storage, the audit table and store separation. v1.6.1 closed the two remaining contradictions: the canonical sample lives at `Docs/Samples/InventoryExport-Sample.txt` with a normative contract (AM-03), and missing metadata is representable by preserving the imported `EQUID` and setting `MetadataMissingFg`, with no sentinel and no enforced FK for unresolved rows (AM-01/AM-02). The import location vocabulary, selection payload, warning sink and spell-merge provenance are resolved by the approved PD3 decisions. Remaining soft gaps are setup recovery (E-02), Items loader ownership/freshness and production grants (E-01), general audit taxonomy/UI (E-09), secret-at-rest review (E-11) and future HTMX semantics if that UI is adopted (E-16).

The platform skeleton, the owner-approved Phase 1 identity/settings layer, the
Phase 2 item-metadata integration, the Phase 3 import pipeline, the Phase 4
public inventory read path and the Phase 5 administrative layer are implemented
and validated: `go build`, `go vet`, `go test ./...`, SQLite migration/rollback
tests, parser golden tests against the 548-line sample, commit-plan and summary
tests, multipart CSRF and import handler tests, public and admin
service/repository/API/HTML tests, the migration hash manifest, and the live
Compose app + MariaDB identity/settings, import, public-inventory and admin
inventory flows. Users, Settings and Sessions exist under `user_version=1`;
passwords are bcrypt hashes and only session-token hashes are stored.

The guild store now has embedded migrations for `ImportBatch`, `ImportList`,
`GuildBankItems` and `Audit`, plus the approved index-only public-inventory
migration and the approved `0003_admin_inventory` migration that adds the
derived location columns and the admin/audit indexes. Migration `0005` drops
the old item quantity constraint, adds `ConsolidatedInto` and enables the
one-row-per-line model. Phase 3 supports upload,
durable parse failures, normalized staging, a selector derived from distinct
`LocationGroup` values (with every `Hoard<n>` collapsed to `Hoard`), persisted
selection, unit-aware summary, and an advisory-locked atomic commit that writes
one item row per line with its quantity and merges spells into the lowest active
EQUID target. Phase 4
exposes those records anonymously with bounded search, class/race/type filters,
natural/date sorts, pagination, UTC availability states, EQResource links and a
dedicated public DTO. Phase 5 adds the startup location backfill, manual
create (one row with the entered quantity)/edit/remove/restore with the same
advisory lock and one audit row per write, editable item quantity, the
filterable audit viewer with per-record history, the admin HTML/JSON
routes under the existing admin group, the owner UI amendments and the
owner-run item-stack consolidation command. Import and
admin audit rows are written in the same transaction as the data change. The
general audit outbox, user management and dashboard remain later phases; the
item quantity model is the latest completed change.

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