# Project Status — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-001 |
| As-of date | 2026-11-22 |
| Source specification | `Design.md` v1.6.1 (1,274 lines: body L1–L854, decisions D-01–D-13 at L855–L1161, amendments AM-01–AM-05 at L1163–L1263, informational implementation status at L1265–L1274) |
| Repository state | Phase 1 identity/settings layer, Phase 2 item-metadata integration, Phase 3 import pipeline, Phase 4 public inventory read path and Phase 5 administrative inventory management are implemented. Administrative add/edit/remove/restore, the audit viewer, the location backfill, the owner-reviewed UI amendments, the post-Phase-5 import/admin usability improvements, the follow-up import-review adjustments (IMP-A/IMP-B/IMP-C), the DEFECT-01 spell metadata correction and the item quantity model (QTY-01–QTY-12) are implemented and live-validated on the Compose development topology (`Phase5Validation.md`, `ImportAdminImprovements.md`, `ItemQuantityModelImplementation.md`). Partial-issuance Phase 1 schema (migration `0006_inventory_movement.sql`), Phase 2 domain/repository support, Phase 3 service layer, Phase 4 JSON API layer and Phase 5 officer UI (Issue, Return and movement-history HTML pages) are implemented; the Phase 5 UI reuses the Phase 3/4 service and API layer, adds no new business rules and does not touch the public read path |
| Documents issued | `ArchitectureReview.md` (AR-001), `DatabaseDesign.md` (DD-001), `ImplementationPlan.md` (IP-001), `ProjectStructure.md` (PS-002), `ItemsDataProfile.md` (DP-001), `Phase3ImplementationPlan.md`, `Phase4ImplementationPlan.md`, `Phase4Validation.md`, `Phase5ImplementationPlan.md`, `Phase5ApprovalReview.md`, `Phase5Validation.md`, `PublicInventoryAPI.md`, `ItemQuantityModelReview.md`, `ItemQuantityModelImplementation.md`, `PartialIssuanceImplementationPlan.md`, `PartialIssuanceApprovalPackage.md`, this status report |
| Overall status | **v1.6.1 published; Phases 0–2 approved and complete; Phase 3 approved, implemented and live-validated; Phase 4 approved, implemented and live-validated; Phase 5 approved, implemented and live-validated, awaiting owner review of the implementation and the one open availability-boundary wording question; partial-issuance Phase 1 schema, Phase 2 domain/repository layer, Phase 3 service layer, Phase 4 JSON API layer and Phase 5 officer UI (issue/return/movement-history pages) implemented** |

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
| Inventory administration | Partial — dev metadata loaded; loader procedure/freshness open | **Implemented** — manual create (one row with the entered quantity under QTY-07, spell merge, duplicate warning, metadata fallback), edit including item quantity through the HTML form (QTY-08; the JSON generic update contract no longer accepts quantity under partial-issuance Phase 4), remove, restore, filters/sorts, admin HTML/JSON routes and audit viewer; rows absorbed by QTY-09 consolidation cannot be restored; officer issue/return forms and a per-record movement-history page implement the partial-issuance Phase 5 UI | E-01 (loader/freshness) remains operational |
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
- `internal/repository/mariadb/stackability.go` implements the OD-01 EQUID
  stackability read contract. From 2026-11-23 this classification is advisory
  display only and never gates a write (see §1.15); the read is retained for
  display and may be removed when no longer useful.
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

### 1.12 Partial-issuance Phase 3 service layer (implemented 2026-11-22)

The service layer implements the approved movement operations without adding
API, UI or public-inventory changes:

- `Issue` supports partial and full-stack issue. A partial issue reduces the
  active lot's quantity in place and keeps the same row; a full-stack issue
  records the recipient in `GivenTo` and soft-deletes the row. Both write one
  `ISSUE` movement and one audit row in the same transaction.
- `Return` increases an active lot, restores a whole-issued row, or creates a
  new active lot for a partial return. It writes one `RETURN` movement and one
  audit row.
- The active-row `GivenTo` invariant, optimistic concurrency, idempotency-key
  persistence and duplicate rejection are enforced in the service. Partial-issue
  eligibility is decided by the active lot quantity currently on hand, not by
  stackability metadata (OD-01 revised 2026-11-23, §1.15).
- `OnHand`, `ListMovements` and `GetMovement` expose the approved on-hand total
  and bounded newest-first ledger reads. Bulk removal writes `WRITE_OFF`
  movements and unit counts.

Validation: service/fake tests cover partial and full-stack issue, return
branches, restore branching, quantity-based partial-issue eligibility,
idempotency duplicates, rollback and on-hand totals. `gofmt`, `go build ./...`,
`go vet ./...` and `go test ./...` are clean.

### 1.13 Partial-issuance Phase 4 JSON API (implemented 2026-11-22)

Phase 4 is JSON-only. The approved owner decisions are applied as follows:

- `POST /api/v1/items/:id/issue` and `POST /api/v1/items/:id/return` accept a
  JSON body with `quantity`, `recipient`, `note` and `expectedModAt`, plus the
  optional `Idempotency-Key` header. Responses return the lot, the movement and
  `currentOnHand`.
- `GET /api/v1/admin/items/:id/movements` returns bounded, newest-first history
  with the documented filters `movementType`, `recipient`, `correlationId`,
  `from`, `to`, `page` and `pageSize`. An unknown lot returns `404`.
- `GET /api/v1/admin/movements/:movementID` returns one movement; an unknown ID
  returns `404`.
- The generic `PUT /api/v1/items/:id` contract no longer accepts `quantity` and
  returns `422 validation_failed` naming the field. `givenTo` may be omitted on
  ordinary edits while the active-row invariant is preserved.
- A partial issue of an active lot with `Quantity > 1` succeeds regardless of
  stackability metadata; a lot with quantity `1` routes to the full-stack path.
  Stale versions and duplicate idempotency keys return `409`.

No HTML issue/return pages or forms were added; those remain Phase 5. API tests
cover partial/full issue, the Ant's Potion example (quantity 10, issue 5 to
Joe, same active lot, `GivenTo` NULL, one `ISSUE` movement), unknown lots,
quantity-based partial-issue eligibility, idempotency duplicates, return,
bounded history, movement detail and the updated generic update contract.
`gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` are clean.

### 1.14 Partial-issuance Phase 5 officer UI (implemented 2026-11-23)

Phase 5 adds the officer-facing workflows over the existing Phase 3/4 service
and API layer. No business rule changes; no public-inventory (Phase 6) or
legacy-cleanup (Phase 7) work:

- `GET/POST /admin/inventory/:id/issue` — Issue form and submit. A partial
  issue reduces the active lot in place and keeps the same row with `GivenTo`
  NULL; issuing the full quantity records the recipient and soft-removes the
  row. The form shows on hand, the resulting quantity and whether partial issue
  is supported, and states which path will run.
- `GET/POST /admin/inventory/:id/return` — Return form and submit. A return to
  an active lot adds to it; the full return of a whole-issued row restores it
  and clears `GivenTo`; a partial return creates a new active lot and leaves the
  issued row in history. The form states which branch will run.
- `GET /admin/inventory/:id/movements` — per-record Movement history,
  newest-first, filterable by `movementType`, `recipient`, `correlationId` and
  `pageSize`, with pagination and an opening-balance note when no movements
  exist. Correlation IDs link to the matching audit view.
- The admin detail page and list add Issue, Return and Movement history actions;
  the detail page keeps item-level stock visibility (quantity, status,
  recipient).
- HTML forms carry the CSRF token, `expectedModAt` and a per-render hidden
  `idempotencyKey` (OD-06); a validation re-render keeps the officer's key so a
  retry stays idempotent. The service re-checks the lot quantity and version
  under the row lock, so the forms are not authoritative.
- The Issue-form hint is derived from the on-hand quantity (`Quantity > 1`
  supports partial issue). A read-only `Stackability(ctx, equid)` helper remains
  available for display only; the write path never consults it (OD-01 revised
  2026-11-23, §1.15).

Validation: `internal/api/admin_movement_pages_test.go` covers the Issue form
render, partial issue (quantity 10, issue 5 to Joe, one active lot, `GivenTo`
NULL, one `ISSUE` movement), full-stack issue, validation re-render,
quantity-based partial-issue eligibility, the Return full/partial branches, the
administratively-removed return guard, the history page (rows, filters,
opening-balance empty state, unknown-lot `404`), the idempotency-key format and
the mode-text helpers; `internal/web/inventory_ui_test.go` asserts the new
templates use the shared nav, carry CSRF and expose the movement access links.
`gofmt -l`, `go build ./...`, `go vet ./...` and `go test ./...` are clean. Phase
6 and Phase 7 remain out of scope.

### 1.15 Partial-issuance stackability gate removed (Option A, implemented 2026-11-23)

The owner approved **Option A**: partial issuance is permitted for any active
lot with `Quantity > 1`, and the `EQUID` stackability gate was removed from the
write path. `EQUID` stackability is now advisory metadata used for display only.

- `service.Issue` no longer rejects a partial issue based on stackability. The
  partial/full decision is `in.Quantity < current.Quantity`; a quantity `1` lot
  therefore takes the whole-stack path.
- `domain.Stackability.AllowsPartialIssue` was removed so the classification
  cannot gate a write.
- The API issue/return forms and handlers no longer look up stackability. The
  issue view data exposes `PartialSupported: item.Quantity > 1`, and the issue
  form label reads "Partial issue" (formerly "Stackability").
- The read-only `Stackability` helper and repository classification are retained
  for display only and may be removed when no longer useful.

This change also fixed a latent bug in the service fake store: its transaction
rollback restored a snapshot of `movements` only when the movement slice had
been rewritten, so a rolled-back issue could leak a movement row. The restore is
now unconditional inside the transaction. The old stackability gate had masked
the bug because it rejected the issue before the movement write.

Validation (2026-11-23): service, domain, API and page tests were updated so a
non-stackable-equivalent lot with `Quantity > 1` issues partially, the lot stays
active, `GivenTo` stays empty and one `ISSUE` movement with the recipient is
recorded. `gofmt -l`, `go build ./...`, `go vet ./...` and `go test -count=1
./...` are clean. Bundled with the already-implemented Phase 5 UI; no new
migration or live-database write.

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

### 6.8 Officer usability improvements (Track A, 2026-11-24)

The owner accepted `OfficerUsabilityReviewV1.md` and authorized **Track A only** — six presentation/flow improvements with no Phase 6, ledger-integrity, aggregation, status-model, reporting or movement-rule change.

| ID | Change | Status |
|---|---|---|
| A-1 | Distinct `Issued` status badge derived from `DeletedFg = true AND GivenTo <> ''`, rendered wherever status appears (list, detail, movement history, return form). Presentation only: no filter, predicate or API change, and `Removed` still means an administrative removal (`GivenTo = ''`). | Implemented |
| A-2 | Return form pre-populates the recipient from the issued lot's `GivenTo`; the field stays editable and clearable, and no business rule changed. | Implemented |
| A-3 | Issue, Return, Remove, Restore and Bulk Remove carry the originating filtered list URL (`returnTo`) and return the officer to that view; untrusted/absolute values are rejected and fall back to the record page (the only accepted destination is `/admin/inventory`). | Implemented |
| A-4 | Movement-history action banner driven by lot state (active → Issue, issued → Return, administratively removed → Restore) offering one canonical action; ledger rows carry no per-row action. | Implemented |
| A-5 | Create-form label `Fallback name` renamed `Name`, with help text stating that the metadata name takes precedence when metadata exists. | Implemented |
| A-6 | Non-blocking advisory when a typed name differs from the resolved metadata name; the metadata name is still stored. New `service.ItemWarningNameMismatch` (`name_metadata_mismatch`) is additive, and the existing JSON response already forwarded warnings. | Implemented |

Explicitly **not** implemented at the time of §6.8: issued status filter (implemented in §6.10), return caps / remaining-returnable calculations, EQUID reverse lookup, AJAX/HTMX lookup, item-centric cross-lot history, and MOVE / ADJUST / MERGE workflows.

Evidence: `internal/web/templates/admin_inventory*.html`, `admin_remove.html`, `admin_bulk_remove.html`, `internal/web/static/app.css`; `internal/api/{views,admin_pages,admin_navigation,admin_pages_handlers,admin_movement_pages,admin_inventory}.go`; `internal/service/admin_inventory.go`; new tests `internal/api/admin_usability_test.go` and `internal/service/admin_name_mismatch_test.go`; `gofmt -l`, `go build ./...`, `go vet ./...` and `go test ./...` clean.

### 6.9 Restore from the Admin Inventory list (Enhancement B, 2026-11-24)

The owner approved **Enhancement B only** from the officer-usability review and deferred **Enhancement A** (partial-issue visibility) to Phase 6. The approved scope adds a list-level Restore action without changing the restore workflow, business rules, permissions, validation or movement-writing behavior.

| ID | Change | Status |
|---|---|---|
| B-1 | The Admin Inventory list offers **Restore** only when `DeletedFg = true`, `GivenTo` is empty and `ConsolidatedInto` is null. The eligibility is computed as presentation-only `adminItemView.Restorable`; the service still re-checks the same rules under lock. | Implemented |
| B-2 | A Restore confirmation page follows the existing Remove-page pattern (`GET /admin/inventory/:id/restore`), shows the record summary and posts to the unchanged `POST /admin/inventory/:id/restore` handler with CSRF, `expectedModAt` and `returnTo`. | Implemented |
| B-3 | The GET page refuses ineligible rows with `409`: active rows, issued rows (`GivenTo` set) and consolidated rows (`ConsolidatedInto` set). The existing restore service remains the authority and still returns `ErrConflict` for stale versions or consolidated rows. | Implemented |

The restore workflow itself is unchanged: `AdminInventoryService.Restore` clears the soft delete, writes `ITEM_UPDATED` and a `RECEIVE` movement (or `RETURN` for a whole-issued row), and preserves the optimistic `ModAt` check. Issued rows keep the existing Return action rather than Restore.

**Deferred:** Enhancement A (issued-quantity visibility on partially-issued Active lots) is recorded for Phase 6, where the design must evaluate the Active badge, an issued-quantity indicator, net issued quantity, opening-balance considerations and the aggregation approach without changing the meaning of the existing `Issued` status badge.

Evidence: `internal/web/templates/admin_inventory.html`, new `admin_restore.html`; `internal/api/{views,admin_pages,admin_pages_handlers,router}.go`; new tests `internal/api/admin_restore_test.go`; `gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` clean.

### 6.10 Issued queue and recipient navigation (Phases 0–3, 2026-11-24)

The owner approved the actionable-state design with **D1 Option A** (mutually exclusive Active / Issued / Removed / All), **D2** (show quantity), **D3** (filtered view, not a separate page) and **D5** (recipient sort). D4 (recipient ledger view) and D6 (over-return work) remain deferred. Phases 0–3 were implemented; Phase 4 was not started.

| ID | Change | Status |
|---|---|---|
| E-1 | `domain.AdminStatusIssued` (`status=issued`) joins the status allowlist. The four states are mutually exclusive and exhaustive: Active = `DeletedFg=0`; Issued = `DeletedFg=1 AND GivenTo` non-empty; Removed = `DeletedFg=1 AND GivenTo` empty; All = no predicate. `removed` was tightened so issued lots no longer appear as administrative removals. | Implemented |
| E-2 | The admin list gains `recipient` and `recipient_desc` sorts. Empty recipients sort last so the recipient queues read top to bottom. | Implemented |
| E-3 | The admin list renders lifecycle tabs (Active / Issued / Removed / All) that keep every other filter and reset to page 1. The Status select and the JSON `status` query parameter accept the same four values. | Implemented |
| E-4 | An issued row links its recipient and its `Issued` badge to `/admin/inventory?status=issued&givenTo=<recipient>`, which is the recipient's issued queue on the existing filtered view. No new route is introduced, so the `returnTo` allowlist (`/admin/inventory`) still applies. | Implemented |
| E-5 | `Active` + recipient cannot match, so the empty result explains that active inventory carries no recipient and links to the same recipient's issued queue. | Implemented |
| E-6 | State-driven actions: the detail page offers Return (not Restore) for issued rows and Restore only for plain administrative removals; the movement-history banner offers exactly one canonical action per state. | Implemented |
| E-7 | `returnTo` is carried consistently from the list and detail pages into the movement history, its pager and its action banner. Previously the Movements link dropped the context. | Implemented |

The issued row's `Quantity` remains the original lot quantity and is **not** a recipient balance; no issued-quantity, outstanding, `SUM(ISSUE) − SUM(RETURN)`, return-cap or aggregation value is computed or displayed. Phase 4 (recipient ledger view) and D6 (over-return exposure) are explicitly not implemented.

Evidence: `internal/domain/inventory_admin.go`, `internal/service/admin_inventory.go`, `internal/repository/mariadb/admin_inventory.go`, `internal/api/{admin_pages,admin_navigation,admin_movement_pages,views}.go`, `internal/web/templates/{admin_inventory,admin_inventory_detail,admin_inventory_history}.html`, `internal/web/static/app.css`; new tests `internal/api/admin_issued_filter_test.go` and `internal/repository/mariadb/admin_inventory_test.go`; `gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` clean.

**Superseded in part (2026-11-25):** §6.11 replaces the lot-derived `Issued` rows with movement-derived rows. An issued row now shows the outstanding quantity for its issuance, so the statement above that no issued-quantity or outstanding value is displayed no longer applies, and D6 (return against an administratively removed lot) is delivered. The D1–D5 contract of this section otherwise stands.

### 6.11 Movement-derived issued rows (delivered 2026-11-25)

The owner approved the design (`AdminInventoryIssuedRowsDesign.md`) and accepted the delivered behaviour at implementation review on 2026-11-25, re-confirming the six decisions: returned units rejoin the same active lot (Q1/D1), a fully returned issuance leaves `Issued` while its `RETURN` stays in `Movements` (Q2/D2), the JSON API is unchanged for this delivery (Q3/D3), the `id` filter matches both lot and movement ids (Q4/D4), no additional column is required on the table (Q5/D5), and a return against an administratively removed lot creates a new active lot (Q6/D6).

The original requirement is met: partial issues are visible, full issues are visible, every outstanding issuance appears in the Admin Inventory experience, each outstanding issuance can be returned directly, inventory arithmetic is unchanged, and Issue / Return / Remove / Restore behaviour is intact.

| Area | Delivered |
|---|---|
| Schema | `0007_movement_related_movement.sql`: nullable `InventoryMovement.RelatedMovementID`, `ix_movement_related_movement`, self-FK `fk_movement_related_movement` |
| `Issued` projection | movement-only: `Quantity − SUM(attributed RETURN.Quantity) > 0`; a fully returned row is hidden |
| `All` projection | `UNION ALL` of lot rows and issued rows, suppressing a whole-stack lot row that an `ISSUE` movement represents |
| Row identity | `RowKey` = `l:<LotID>` or `m:<MovementID>`; movement rows carry `MovementID`, outstanding `IssuedQuantity`, `IssuedAt` and `IssuedBy` |
| Return by movement | `GET\|POST /admin/inventory/movements/:movementID/return` with CSRF, idempotency key, optimistic `ModAt` and over-return `422` |
| Lot-level return | attributes the `RETURN` FIFO to the earliest outstanding `ISSUE` |
| JSON | `adminItemResponse` unchanged; `status=issued` and `status=all` content is movement-derived (design §7.4) |
| On-hand totals | unchanged; movement quantities never enter the total |

Deviations from the design text are recorded in `AdminInventoryIssuedRowsDesign.md` §15.2: three separate `ALTER TABLE` statements, the admin read interface left unwidened with the dispatch inside `ListAdminInventory`, and the shortened migration file name.

Open items: the one-time FIFO attribution backfill has **not** been executed — 5 pre-deployment `RETURN` rows totalling 19 units (lots 891 ×2, 970 ×1 of 15 units, 1041 ×2) are still `NULL`, so `Issued` overstates outstanding for those lots — and the manual eleven-criterion acceptance pass has not been recorded. Both are tracked in the design §15.4. The migration incident and its recovery are recorded in §6.11.1.

Evidence: the migration and manifest above; `internal/domain/{movement,inventory_admin}.go`; `internal/repository/mariadb/{movement,admin_inventory}.go`; `internal/service/{admin_inventory,admin_movement}.go`; `internal/api/{router,views,admin_pages,admin_movement_pages}.go`; `internal/web/templates/{admin_inventory,admin_inventory_detail,admin_inventory_return}.html`; new tests `internal/repository/mariadb/admin_issued_test.go`, `internal/service/admin_issuance_return_test.go` and `internal/api/{admin_issuance_return,admin_issued_filter}_test.go`; live Compose restart logged `schema_version=0007_movement_related_movement` with `/healthz` `200`; `gofmt`/`go vet`/`go test -count=1 ./...` clean.

#### 6.11.1 Migration incident and recovery (2026-11-25)

The migration version is the file name without `.sql`, recorded in `schema_migrations.Version VARCHAR(32)`. The first `0007` name was 40 characters, so its three `ALTER TABLE` statements committed while the version `INSERT` failed with `1406 Data too long for column 'Version'`; the next start re-ran the file and failed with `1060 Duplicate column name 'RelatedMovementID'`.

Recovery, with the app stopped and after confirming `0` non-NULL values: drop `fk_movement_related_movement`, then `ix_movement_related_movement`, then the `RelatedMovementID` column; rename the file to `0007_movement_related_movement.sql`; rebuild so the runner applies and records the migration. Hand-writing the version row cannot work because the version is recomputed from the file name on every start, and widening `Version` was rejected because it contradicts `DatabaseDesign.md` §8 and is not retrofitted by `CREATE TABLE IF NOT EXISTS`.

Guardrails: `migrate.go` now rejects an over-long version before executing any statement (`schemaMigrationsVersionMaxLen`), and `TestMigrationManifest` fails the build when any embedded migration name exceeds 32 characters. Exact SQL, inspection queries and evidence are in `AdminInventoryIssuedRowsImplementationPlan.md` §5.

### 6.12 Guild-store startup order and automatic reconnect (2026-11-25)

The owner decided that **connectivity failures recover automatically while migration failures remain fatal**, with `/healthz` kept as the liveness probe and `/readyz` as the operational readiness signal. This section records that change.

Root cause of the old "503 until the container is restarted" behaviour: `mariadb.Open` pinged once and, on failure, **closed the pool and returned `nil`**. Every guild service was then built against a `nil` store, `Ready()` was a pointer check, and the readiness service cached the startup error, so a MariaDB that started later could never be adopted and nothing retried. (A live pool already reconnected on its own; only the startup path was broken.)

| Area | Delivered |
|---|---|
| Pool | `mariadb.OpenPool` builds the bounded pool without dialing and keeps it for the process lifetime; `Open` keeps its eager ping for the setup wizard and integration tests |
| Supervisor | `Store.Run` (`internal/repository/mariadb/supervisor.go`) retries connectivity with bounded backoff (1s → 2s → 4s → 8s → 15s → 30s cap), then initializes |
| Initialization | `Store.Initialize` runs `Migrate`, `RecoverInterrupted` and both backfills; it is idempotent and **re-runs after every reconnect** before readiness is republished |
| Readiness | published only after a successful `Initialize`; withdrawn when the 5s keepalive probe fails (2s timeout) |
| Migration / schema failures | logged at ERROR, surfaced through `/readyz`, retried `MariaDB.InitAttempts` times (default 3) and then **fatal** — the process exits non-zero |
| Config | `-mariadb-init-attempts` / `GUILDBANK_MARIADB_INIT_ATTEMPTS` (default 3, valid 1–100) |
| `/readyz` | reports `initializing`, `error` with the live reason, or `ok`; a startup failure is no longer cached |
| Services | keep one stable store reference; `Ready()` reads the supervisor's published state (no per-request ping) |
| `-consolidate-items` | waits up to 60s for readiness instead of failing immediately |

Not changed: PD-18 (Settings-backed hot-swap), the schema, issue/return arithmetic, on-hand totals and the 503 wording.

Validation (live Compose, 2026-11-26): with MariaDB stopped the app started and served `/healthz` 200, `/readyz` 503 with a live reason (`dial tcp: lookup mariadb: i/o timeout`) and inventory 503; starting MariaDB then logged `guild store schema ready` → `guild store ready` and `/readyz` returned 200 **in the same process** (`RestartCount=0`), with one `unreachable; retrying` warning rather than a retry storm.

### 6.13 Name-first item identity (P0, 2026-11-26)

The first delivered slice of the roles/requests workstream: an officer can add inventory by item name, and an unmatched name no longer blocks entry. Approved by the owner on 2026-11-26 with the `UnresolvedEQUID` strategy; P1–P5 (roles, user administration, request intake and approval) remain design-only.

| Input | Behaviour |
|---|---|
| EQUID supplied | Unchanged: metadata by EQUID, entered name is only a fallback identity |
| Name only, unique exact match | EQUID filled from metadata and the metadata name stored; no name-mismatch warning |
| Name only, several matches | `422` with a bounded candidate list (`{error:{fields:{equid},candidates:[…]}}` for JSON, radio list for the HTML form); **no row is created**; the server never guesses |
| Name only, no match | Row created with `EQUID = 4294967295`, `MetadataMissingFg = 1`, the entered name stored verbatim, and the `item_identity_unresolved` warning |
| Neither supplied | `422` on both `equid` and `name` |
| Client sends `4294967295` | `422`: the placeholder is server-assigned only |

Implementation: `domain.UnresolvedEQUID` (with the AM-01 addendum in `Design.md`), `domain.ItemNameAmbiguousError`, `mariadb.Store.FindItemsByName` (`SELECT id, name, classes, races FROM items WHERE name = ? ORDER BY id LIMIT ?`, read-only), `service.ItemNameResolver` (normalization, exact-then-collation matching, 2,000-entry / 10-minute cache, unique/ambiguous/not-found), name resolution inside `AdminInventoryService.prepareCreate`, the `GET /api/v1/admin/item-names` lookup endpoint, a name-first form with candidate radios and `static/admin_inventory.js`, and an **Unresolved identity** badge plus explanatory note on the admin list and detail pages.

Safety guards, all tested: spell merge is skipped for the placeholder (one identifier must not merge unrelated unknown spells); `consolidationGroups` excludes `EQUID = 4294967295` (it groups by EQUID); `BackfillSpellMetadata` and the location backfill never match the placeholder; no schema migration was needed.

Validation: `gofmt -l .` empty, `go vet ./...` and `go test -count=1 ./...` clean (new tests cover the resolver, the create paths, the request decoder and the error envelope). Live checks against the Compose dataset: unique and ambiguous names exist in `items` (`Tattered Note` has 82 rows), `EXPLAIN` uses `idx_name` (`type=ref`, `key_len=1023`, 82 rows, filesort for the bounded `ORDER BY id`), and after rebuilding the image `/readyz` and `/api/v1/items` stayed 200 while the new lookup route answered `401` without a session and the new static asset answered `200`.

### 6.14 P1 role foundation (2026-11-26)

The approved `Member < Officer < Admin` hierarchy is now enforced end to end. P1 deliberately stops before user administration (P2) and the request workflow (P3/P4).

**Two defects this closed.** Session issuance rejected every non-administrator (`service/auth.go`), so an Officer or Member account could not sign in at all; and the import service checked `actor.IsAdmin`, so an Officer would have failed at the service layer even once the route allowed it. Both are fixed, the second by the `domain.Actor` capability model (`Role` + `CanManageInventory`, with a backward-compatible fallback to `IsAdmin`).

| Area | Delivered |
|---|---|
| Role model | `domain.Role` with `ParseRole` (fails closed to Member), `Rank`, `AtLeast`; `User.Role/IsOfficer/CanAdministerInventory/CanViewAudit/CanManageUsers/CanManageSettings`. `AccessLevel` remains the single source of truth |
| Login | Every enabled role may authenticate; authorization moved entirely to the route/service layer |
| Middleware | `RequireRole(min)` (Member < Officer < Admin) with JSON `403` and an HTML 403 page; `RequireAdmin()` retained as an alias |
| Route groups | Deny-by-default `member` → `officer` → `admin` groups, ordered authentication → role → CSRF. `GET /api/v1/auth/session` and `POST /api/v1/auth/logout` **moved to `member`** (non-admins could not log out before) |
| Officer scope | Imports, all inventory administration (add/edit/issue/return/remove/restore/bulk), audit and movement history, and the P0 name lookup |
| Administrator scope | Settings only in this release (`/admin/settings`, `/api/v1/settings`) |
| `/admin` landing | Administrator → `/admin/settings`, Officer → `/admin/inventory` |
| Navigation | Permission-driven: admin links only for inventory-capable roles, `Settings` only for Administrators. Today's "Settings for everyone" defect is gone; the login copy no longer implies administrator-only |
| SQLite | `0002_user_roles.sql` rebuilds `Users` (CHECK widened, default `Member`), preserves every column, recreates `ux_users_username` and normalises `sqlite_sequence`. The cascade from `Sessions` clears all sessions, which is approved |
| Last-admin guards | `CountEnabledAdmins`, `SetUserAccessLevel` and `SetUserDisabled` (transactional, Administrator-only, refuse to leave zero enabled Admins, revoke sessions on disable); role changes are visible on the next request because the session join re-reads `Users` |

**Tests.** `authorization_matrix_test.go` holds the authoritative route→role table and fails when a registered route is unclassified. `authorization_enforcement_test.go` logs in as anonymous/Member/Officer/Admin and sweeps every route, asserting rejection below the requirement and never `401`/`403` at or above it. `role_access_test.go` proves every role can use the session and logout, that a disabled account cannot sign in, and the role-aware `/admin` landing with the Officer settings `403`. `navigation_role_test.go` asserts nav visibility per role from the template itself. `users_roles_migration_test.go` replays the v1 schema on populated data (accounts preserved, sessions cleared, CHECK widened, sequence continuous, unique index intact); `users_roles_guard_test.go` covers the last-administrator and session-revocation rules plus next-request role visibility. `domain/role_test.go` covers fail-closed parsing, ordering and capabilities.

**Validation.** `gofmt -l .` empty, `go vet ./...` and `go test -count=1 ./...` clean. Live Compose deployment of the populated database logged `sqlite schema ready … user_version: 2` on first start; `/readyz` and `/api/v1/items` stayed 200, `/api/v1/auth/session` answered `401` unauthenticated (route present in the member group), and both `/admin/settings` and `/admin/inventory` answered `303` to `/login` for anonymous callers. Every existing account signs in again exactly once because of the session cascade.

**Deferred at the time (closed by P2 in §6.15):** user administration and password workflows, `MustChangePassword` enforcement, and — as a hard gate — the officer audit event-scope filter had to ship in the same release as the first `USER_*` audit writer. P2 shipped all four together.

### 6.15 P2 user administration (2026-11-26)

Account management is now available to Administrators, and the forced-password-change workflow is enforced for every role. The approved design (Q1–Q8) was followed: temporary passwords are always required to change, are never echoed back, and current-password verification is mandatory even during a forced change.

| Area | Delivered |
|---|---|
| Service | `service.UserAdminService` (list/filter, create, role change, enable/disable, reset, delete, own password change). The last-enabled-administrator guards stay in the store, so every caller passes through them |
| Password policy | `HashUserPassword(username, password)` applies the 12–72 byte policy **and** the "must not equal the username" rule, which `HashPassword` could never enforce without a name |
| Creation | Role defaults to `Member`; `MustChangePassword` is always set; duplicate usernames are case-insensitive and return `409` with a `userName` field message |
| `MustChangePassword` | `RequirePasswordChanged()` runs after authentication and before the role gate. A flagged account may reach only `/account/password`, `POST /api/v1/auth/password`, logout and session inspection; HTML callers are redirected (`303 /account/password`), API callers receive `403 password_change_required`. Login redirects a flagged account to the change page before honouring `next` |
| Own password change | Current password required and verified; new password must differ; success clears the flag and revokes every other session of that account |
| Reset | Administrator reset sets a temporary password, requires a change, and revokes **all** sessions for the account (including the administrator's own, per Q4) |
| Deletion | Hard delete behind a typed-username confirmation and the last-admin guard; the UI offers Disable as the preferred action |
| Self-protection | An administrator cannot disable or delete their own account; the store still refuses to remove the last enabled Administrator |
| Audit | `USER_CREATED`, `USER_ROLE_CHANGED`, `USER_DISABLED`, `USER_ENABLED`, `USER_PASSWORD_RESET`, `USER_PASSWORD_CHANGED`, `USER_DELETED` on `TableName = 'Users'`, best-effort after the SQLite commit (Q5) |
| Officer audit scope | The hard gate is closed: `AuditEventTypesFor(role)` restricts non-administrators to the inventory/import allowlist, `domain.AuditFilter.EventTypes` adds an `EventType IN (...)` predicate, the viewer's event-type options are scoped, and `GET /api/v1/audit/:id` returns `404` to an Officer for a `USER_*` row |
| Routes | `member`: `GET /account/password`, `POST /api/v1/auth/password`; `admin`: `/admin/users`, `/admin/users/new`, `/admin/users/:id`, form POSTs (`/admin/users`, `:id`, `:id/password`, `:id/delete`) and the JSON API (`GET/POST /api/v1/users`, `PUT|POST /api/v1/users/:id`, `POST /api/v1/users/:id/password`, `DELETE /api/v1/users/:id`) |
| UI | `/admin/users` list with a username filter and last-admin/disabled badges, create form, detail page with role+status, reset and delete cards, and `/account/password`. Navigation gains **Accounts** for Administrators; `Settings` stays last |
| Landing page | HTML login lands on **inventory (`/`)** for every role; a saved `next` page is returned to only when the signed-in role may use it, otherwise inventory. Settings is never a default destination |

**Schema.** No migration: P2 reuses the `Users` columns created by `0001` and widened by `0002`, so the schema version remains 2.

**Tests.** `service/user_admin_test.go` covers creation, the username-as-password rule, duplicate names, non-admin refusal, self-disable/self-delete refusal, reset semantics, own-password verification and session retention, and that an audit failure never fails the account change. `service/audit_scope_test.go` proves the Officer scope excludes every `USER_*` event from filters and from single-row reads while the Administrator scope is unrestricted. `api/users_admin_test.go` proves Members and Officers receive `403` on every account route; drives create → promote → disable → enable → reset → delete over the API; proves the last-administrator and self-protection guards through HTTP; proves a flagged account is confined, that the wrong current password is `422`, and that the change clears the flag while keeping the current session; and exercises every HTML page and form, including the duplicate-name re-render. `api/authorization_matrix_test.go` gained the 14 new routes, and the enforcement sweep exercises them for all four identities.

**Validation.** `gofmt -l .` empty, `go vet ./...` and `go test -count=1 ./...` clean (all 10 packages).

**Deferred:** password history/complexity beyond length, account lockout policy changes, and the `AuditOutbox` that would make account audit atomic with the SQLite commit.

### 6.16 P3 Phase A — profile character names (2026-11-26)

The request workflow needs a trustworthy requester identity per account, so every signed-in user can now keep a list of EverQuest character names. This is the first slice of the approved P3 profile design (D1–D4); Discord identity, requests and self-registration remain design-only.

| Area | Delivered |
|---|---|
| Schema | `0003_user_profiles.sql` adds the three nullable Discord columns (inert until Phase B) and the `UserCharacters` table with a global case-insensitive unique index on the name and a cascade from `Users`. `ALTER TABLE ADD COLUMN` is metadata-only here, so **nobody is signed out** |
| Validation | `domain.ValidateCharacterName`: 3–15 ASCII letters, trimmed, no spaces, digits or control characters; `AddCharacterInput` reports field-level messages |
| Storage | `ListCharacters`, `CountCharacters`, `AddCharacter` (cap of 10 enforced **inside** the insert transaction) and `DeleteCharacter` (ownership is part of the `WHERE`, so another account's ID is `404`) |
| Service | `CharacterService`: list/add/remove for the owner and `RemoveForUser` for an Administrator (D4). Administrators can delete a name but the store refuses any account mismatch, so ownership can never be moved |
| UI | `/account/profile` lists names with Remove actions, an add form and an empty state that explains the request use case; the nav gains **Profile** for every signed-in user; the administrator's user detail page shows the account's names with a Remove action |
| API | `GET/POST /api/v1/me/characters`, `DELETE /api/v1/me/characters/:id`, `POST /api/v1/me/characters/:id/delete`, plus the administrator pair `POST /admin/users/:id/characters/:characterID/delete` and `DELETE /api/v1/users/:id/characters/:characterID` |
| Audit | `CHARACTER_ADDED` and `CHARACTER_REMOVED` on `TableName = 'UserCharacters'`, best-effort after commit, added to the Administrator-only event scope so Officers cannot read them |

**Schema version.** 3. The `latestSchemaVersion` helper derives the expected value from the embedded migration count, so every migration test follows automatically.

**Tests.** `domain/character_test.go` covers the name rules (length, charset, trimming, non-ASCII, control characters). `repository/sqlite/characters_test.go` covers ordering, global case-insensitive uniqueness, the 10-name cap, ownership-scoped delete, name reuse after delete and the cascade from user deletion. `service/characters_test.go` covers audit shape, ownership refusal, the administrator path (including the mismatched-owner refusal) and that an audit failure never fails the change. `api/profile_test.go` covers anonymous rejection, add/list/duplicate/invalid/delete over JSON and forms, cross-account name collision, the profile being blocked by `MustChangePassword`, Officer refusal on the administrator routes, and an Administrator removing a name from the user detail page. The route matrix gained the seven new routes and the enforcement sweep now substitutes `:characterID`.

**Validation.** `gofmt -l .` empty, `go vet ./...` and `go test -count=1 ./...` clean (all 10 packages).

### 6.17 P3 Phase B — Discord account linking (2026-11-26)

Members can attach their Discord identity to the account they already own. Linking is strictly additive: it creates no account, grants no session, changes no password, and the password login path is untouched. Discord *sign-in*, self-registration and requests remain out of scope.

| Area | Delivered |
|---|---|
| Configuration | Five Settings keys: `PublicBaseUrl` (origin only), `DiscordEnabled`, `DiscordClientId`, `DiscordClientSecret` (secret, blank keeps the stored value), optional `DiscordGuildId`. Validation rejects a bad origin, non-numeric IDs and an unparseable flag, and the settings page renders the flag as a select |
| Provider client | `internal/platform/discord`: token exchange with HTTP Basic client credentials, `GET /users/@me`, and `GET /users/@me/guilds` for optional membership checks. Every call is bounded (10 s), fail-closed and logs only a status plus a 512-byte body excerpt |
| Storage | The `0003` columns are now used. `DiscordIdentity`, `DiscordUserIDOwner`, `SetDiscordIdentity` and `ClearDiscordIdentity`, with the partial unique index as the authority on a shared identity |
| Link workflow | `POST /account/discord/link` mints a signed state value bound to the account **and** session, stores it in a `SameSite=Lax`, `HttpOnly` cookie, then redirects to Discord's consent screen with `scope=identify` (never `email`) |
| Callback | `GET /account/discord/callback` requires a live session, a matching state cookie and a valid signature/expiry. It clears the cookie before doing anything else, so the state is single use. Every outcome is `303 /account/profile?notice=<code>`; the codes are mapped to fixed text |
| Unlink | `DELETE /api/v1/me/discord` and the `POST /account/discord/unlink` form adapter. Session ownership is sufficient (approved D8); unlinking NULLs the three columns and keeps the account |
| Account takeover guards | Linking requires an authenticated session; a Discord identity already owned by another account is refused (`409` / `notice=discord-taken`); the state is unguessable, session-bound and expires in 10 minutes; a provider failure writes nothing; the profile page never renders unlinked state as linked |
| Guild verification | Optional plumbing only: when `DiscordGuildId` is set the callback asks whether the linked Discord user is a member and records `guildVerified` in the audit. It never blocks linking, and a failed check is logged and recorded as unverified. **Known defect (confirmed 2026-11-26):** the consent requests `scope=identify`, but `GET /users/@me/guilds` requires the `guilds` scope, so the check is expected to fail with `403` and every attempt is recorded unverified. See `SelfRegistrationDesign.md` §6.5 for the documented requirement and the fix |
| Audit | `DISCORD_LINKED` / `DISCORD_UNLINKED` on `TableName = 'Users'`, written best-effort after the commit and added to the Administrator-only event scope. No token, secret or code is ever audited |
| UI | A Discord card on `/account/profile`: "not enabled" copy, a link button, or the linked name with a since-date and an unlink action |

**No schema change.** Phase B reuses migration `0003`, so the schema stays at version 3 and nobody is signed out.

**Deliberately absent.** No `/api/v1/auth/discord*` route, no Discord button on the login page, no account creation, no `MustChangePassword` bypass (a flagged account is still sent to `/account/password` before it can reach the link form).

**Tests.** `platform/discord/client_test.go` covers credentials, form fields, basic auth, profile parsing and guild membership against `httptest`. `service/discord_test.go` covers configuration readiness, the authorize URL, successful linking with audit shape, guild verification success/failure, refusal of a shared identity, the three provider-failure paths, disabled configuration, unlink and unavailable dependencies. `repository/sqlite/discord_test.go` covers the round trip, the unique index, relinking, releasing an identity for another account and unknown accounts. `api/discord_test.go` covers the profile card across settings changes, the full link flow, single-use state, foreign state, cross-account refusal, provider failure, unlink (API and form), disabled configuration and `MustChangePassword` confinement. The route matrix gained five member routes.

**Validation.** `gofmt -l .` empty, `go vet ./...` clean, and the suite passes package by package — `api` (by test-name range, because the package now exceeds the terminal's single-command output window), `service`, `repository/sqlite`, `platform/...` (11 packages including the new `platform/discord`), `web`, `integration` and `domain`.

### 6.18 P3 Phase C — Discord sign-in (2026-11-26)

A linked account may now sign in with either its username and password **or** Discord. Both credentials resolve to the same row; Discord is never a second account, never a registration path and never a replacement. Self-registration, Discord-only accounts and the request workflow remain out of scope.

| Area | Delivered |
|---|---|
| Login page | A "Sign in with Discord" link appears only when Discord sign-in is fully configured. It carries `next` through the round trip; the password form is unchanged and always present |
| Start endpoint | `GET /auth/discord` is public, mints a state scoped `login`, and redirects to Discord with the **separate** redirect URI `…/auth/discord/callback`. An existing valid session short-circuits to its normal destination instead of starting a second credential flow |
| Callback | `GET /auth/discord/callback` verifies the state cookie (double submit), the signature, the `login` scope, the 10-minute expiry and the embedded `next`. Every outcome is a `303` with fixed text; no provider message is reflected |
| Account resolution | `UserByDiscordID` looks up the owning account. `NotFound` and `DisabledFg` both return the single neutral `ErrUnauthorized`, so an attacker cannot tell "not linked" from "disabled", and no row is created or modified |
| Session creation | `DiscordService` holds a `SessionIssuer` (`AuthService.IssueSession`), so the session comes from the same code path as a password login: same idle/absolute policy, same `DisabledFg` re-check, same `LastLogin` and audit-time semantics. The cookie and CSRF rotation mirror `authHandler.establish` |
| Redirect handling | `next` is signed inside the state, re-validated with `safeNext`, and still passes `authorizedNext` (so a Member asking for `/admin/users` lands on the inventory page). Failure notices preserve `next` for a retry |
| MustChangePassword | Checked immediately after the session is written, before any destination: a flagged account goes to `/account/password` and stays confined there |
| Rate limiting | A dedicated limiter keyed by client address, 10 failures per 15 minutes, returning `notice=discord-rate`. Successes reset it; a failure never issues a session. This bounds outbound provider calls without affecting the password limiter |
| Audit | `DISCORD_LOGIN` on `TableName = 'Users'` with `userName`, `discordUserId`, `discordUserName` and `method=discord`. No token, code or secret is written. Failed attempts are logged through `slog` only, so anonymous traffic cannot inflate the audit table |
| Scope separation | A Phase B link state is rejected by the sign-in callback and vice versa, because the flow name is inside the signed payload and each flow has its own cookie |

**Password login is untouched.** `AuthService.Login`, the login form, the CSRF flow and the session middleware are unchanged; the only edit in that file is the optional Discord button on the login page.

**Deliberately absent.** No registration, no account matching by email or username, no Discord-only account, no role escalation, and no bypass of the password-change or disabled-account gates.

**Tests.** `service/discord_test.go` adds sign-in coverage: session issued for a linked account with the login redirect URI and audit shape, unlinked refusal that writes nothing, disabled-account refusal, disabled configuration and missing issuer, provider failure, and the per-address rate limit. `repository/sqlite/discord_test.go` covers `UserByDiscordID` including unknown and empty identifiers. `api/discord_login_test.go` covers the button's visibility, a full end-to-end sign-in after a real Phase B link (plus password login for the same account and single-use state), the neutral unlinked response with `next` preserved (and an unchanged account count), `MustChangePassword`, a disabled account, forged state, link-state replay at the sign-in callback, the already-signed-in shortcut, and the unconfigured case. The route matrix gained the two public sign-in routes.

**Validation.** `gofmt -l .` empty, `go build ./...` and `go vet ./...` clean, and the suite passes per package: `service`, `repository/sqlite`, `platform/...`, `web`, `domain`, `integration`, and `api` by test-name range (`[A-C]`, `[D]`, `[E-F]`, `[H-M]`, `[N-R]`, `[S-U]`, plus the Discord groups).

### 6.19 Defect: the Discord link button never left the profile page (2026-11-26)

**Symptom.** Clicking "Link Discord Account" issued `POST /account/discord/link`, which correctly answered `303 Location: https://discord.com/oauth2/authorize…`, but the browser stayed on `/account/profile` and no Discord consent screen appeared. Pasting the returned URL worked and completed the link.

**Root cause.** `SecurityHeaders` sends `Content-Security-Policy: … form-action 'self'`. A form submission's redirect chain is checked against `form-action` by the browser, so the browser cancelled the cross-origin `303` before it became a navigation. The state cookie had already been stored, which is why a manual paste then succeeded. Link navigation (the "Sign in with Discord" anchor) and same-origin `303`s (character add/remove, unlink) were unaffected, and the automated suite could not see it because Go's HTTP client does not implement CSP.

**Fix.** The profile card now uses `<a href="/account/discord/link">` instead of a POST form, and the router serves `GET /account/discord/link` alongside the existing `POST` (kept for non-browser clients that want the `authorizeUrl` JSON). The handler, state signing, session binding, TTL, callback validation and identity logic are unchanged; a GET start only mints the short-lived state cookie and redirects, exactly like `/auth/discord`.

**Tests.** `TestProfileDiscordSectionFollowsSettings` now asserts the anchor is rendered and that no form targets the link path (the CSP regression guard). `startDiscordLink` drives the GET navigation with no CSRF token. `TestDiscordLinkStartJsonContract` keeps the POST/JSON contract covered, and `TestDiscordRoutesRequireEnabledConfiguration` covers both the GET and POST unavailable paths. The route matrix classifies `GET /account/discord/link` as `classMember`; the enforcement sweep passes.

### 6.20 P4-SR — Self-service registration via Discord (2026-11-26)

A visitor can now create a Member account with Discord, with no email, no SMTP and no password. Registration is the **only** flow that requests `scope=identify guilds` (owner decision D-SR-13): linking and sign-in keep `identify`, and `authorizeURL` takes the scope as an explicit parameter, so no flow can inherit another's permissions by default. Design: `SelfRegistrationDesign.md`.

| Area | Delivered |
|---|---|
| Schema | `0004_self_registration.sql`: `AccountStatus` (`NOT NULL DEFAULT 'Approved'`, `CHECK (Approved, PendingApproval)`), nullable `Origin` and `GuildVerifiedAt`, `ix_users_status`. Additive only — no rebuild, no session cascade; every existing account reads `Approved`. `insertUser` writes all three columns, and `userColumns`/`scanUser`/`sessionSelect`/`scanSession` were extended together so a session read cannot see a zero status |
| Status model | `domain.AccountStatus` with `ParseAccountStatus` (blank → Approved, unknown → PendingApproval) and `User.Status/IsApproved/IsPending`. **`IsEnabled()` is now `IsApproved() && !Disabled`**, so `Login`, `issueSession`, `Authenticate` and `DiscordService.Login` refuse a pending account without a single change to those functions |
| Account creation | `CreateRegisteredUser` writes the account and its Discord identity in one INSERT (atomicity without a two-step window); `ux_users_username` and `ux_users_discord` decide every race and both surface as `ErrAlreadyExists`. Self-registered accounts store a discarded bcrypt hash of 32 random bytes, so `PasswordHash` stays `NOT NULL` and no password can ever verify. `Origin = SelfRegistration`, role always `Member`, `MustChangePassword = 0` |
| Flow | `GET /register` (landing, states the approval outcome before consent), `GET /register/discord` (state scoped `register`, own cookie), `GET /register/discord/callback` (double submit, expiry, code exchange on the third redirect URI, guild check, ownership check), `GET /register/complete` + `POST /register/complete` (username form, CSRF, signed ticket cookie). Every start is a plain `<a>` navigation, so CSP `form-action 'self'` cannot cancel it |
| Ticket | `api/register_ticket.go`: `base64url(payload).base64url(HMAC-SHA256)`, payload `v1|discordUserID|guildCheck|guildID|issuedAt|expiry|nonce|displayName` (free-form values base64url'd), 10-minute TTL, HttpOnly/SameSite cookie, process-local secret. The Discord identity never travels in a form field, so the browser cannot choose which identity is registered |
| Fail-closed approval | Only `(true, nil)` from `GuildMember` approves. A provider error is recorded as `error` (never "not a member"), and an empty `DiscordGuildId` is `not-configured` — both create `PendingApproval`, no session, and the account appears in the queue |
| Session rule | `RegistrationService` holds no `SessionIssuer`. The API issues a session through `AuthService.IssueSession` only when the created account reports `IsEnabled()`, so a pending account cannot obtain one even if the check were reordered |
| Approval administration | `UserAdminService.Approve` / `WithdrawApproval` over `SetUserAccountStatus` (Administrator-guarded in the store, sessions deleted in the same transaction on withdrawal, idempotent when unchanged). Routes: `POST /admin/users/:id/approve`, `POST /admin/users/:id/withdraw-approval`, `POST /api/v1/users/:id/approve`, `POST /api/v1/users/:id/withdraw-approval`. Audit: `USER_APPROVED`, `USER_APPROVAL_WITHDRAWN`, plus `USER_SELF_REGISTERED` |
| Guards | A role change is refused while the account is pending (`409`, "approve the account before assigning a role"), so approval and promotion stay separately audited acts. An operator cannot withdraw their own approval (it would end their own session mid-request), mirroring self-disable/self-delete. `CharacterService.Add` also refuses a non-enabled account, so a pending user cannot claim a globally unique name even if a session ever leaked through |
| UI | Login page gains **Create account** (only when the switch is on and Discord is ready); new `/register` and `/register/complete` pages; `/admin/users` gains a status filter, a `N account(s) await approval` banner linking to `?status=pending`, and the derived status text (`Active` / `Pending approval` / `Disabled` / `Pending approval · disabled`); the detail page adds an Approval section with Approve or Withdraw, shows the Discord name and whether membership was verified, and disables the role select while pending |
| API | `userResponse` gains `accountStatus`, `pending`, `origin`, `guildVerifiedAt` (additive); `GET /api/v1/users?status=pending\|approved\|disabled\|all`; `createUserRequest` still has no status field |
| Settings | `SelfRegistrationEnabled` (boolean, default `false`) with the warning that enabling it without a guild ID makes every registration pending |

**Deployment checklist.** Add `PublicBaseUrl + /register/discord/callback` to the Discord developer portal redirect URIs, alongside `/account/discord/callback` and `/auth/discord/callback`. A missing portal entry fails at Discord with `invalid_redirect_uri` before any code reaches GuildBank. Registration is the first consent that asks for `guilds`, so an existing user who consented to `identify` earlier will see a second consent prompt.

**Known limitation kept on purpose.** The Phase B guild check in the link flow still requests `identify` and is therefore expected to fail with `403` (§6.17, `SelfRegistrationDesign.md` §6.5). D-SR-13 accepts that limitation: the shipped link implementation is unchanged, and registration — not linking — supplies the guild signal.

**Deliberately absent.** No email, no SMTP, no self-service password recovery, no captcha, no Discord role mapping, no re-verification or expiry of approval, no automatic matching or merging with an existing account, and no request system.

**Tests.** `domain/account_status_test.go` (parse rules, `IsEnabled` requires approval). `repository/sqlite/migration_0004_test.go` (0004 over a populated v3 database: row reads `Approved`, the session survives, `CHECK` rejects a corrupt value, `user_version` follows the embedded migration count). `repository/sqlite/registration_test.go` (single-statement account+identity write, both unique indexes, withdrawal revokes sessions, idempotency, role change refused while pending, store-level administrator guard). `service/scope_test.go` (`TestAuthorizeScopeIsPerFlow`: link and sign-in `identify`, registration `identify guilds`, separate redirect URIs). `service/registration_test.go` (the §6.2 decision table, the registration redirect URI, disabled configuration, no writes during `Verify`, Approved vs Pending completion, unusable stored hash, claim of an existing account). `api/register_test.go` (landing page, full approved flow with automatic sign-in, pending flow with no session and a listed queue entry, duplicate-username `409` with the ticket kept, unavailable until enabled). `api/user_approval_test.go` (approval API, HTML approve form, withdrawal revoking sessions and blocking the next login). The route matrix gained five public and four Administrator routes.

**Validation.** `gofmt -l .` empty, `go build ./...` and `go vet ./...` clean, and the suite passes package by package (`domain`, `repository/sqlite`, `service`, `web`, `platform/...`), with `api` run by test-name range because its single-command output exceeds the terminal window.

### 6.21 Discord sign-in distinguishes pending and disabled accounts (2026-11-26)

**Defect.** Every refusal that happened *after* a linked Discord identity was resolved produced the same fixed notice, *"No GuildBank account is linked to that Discord account."* A self-registered member waiting for approval, and a suspended account, were both told that nothing was linked — the one diagnosis that is wrong for them, and the one that sends them to a password form they cannot use.

**Fix.** `DiscordService.Login` already holds the `domain.User` returned by `UserByDiscordID`, so the reason travels the last hop with no extra query: the `!user.IsEnabled()` branch now returns `*domain.AccountStateError{Status, Disabled}` instead of a bare `ErrUnauthorized`. The type unwraps to `ErrUnauthorized`, so every caller that only knows "not allowed" — including the existing `errors.Is` assertions — is unaffected. `discordHandler.loginCallback` checks it **before** the `ErrUnauthorized` branch (a `switch` takes the first match and the wrapped error satisfies both) and maps it to the fixed codes `discord-pending` or `discord-disabled`. `Disabled` wins when both apply.

| Aspect | Behaviour |
|---|---|
| Lookup path | Unchanged — one `UserByDiscordID`, no additional query, no schema change |
| Unlinked identity | Unchanged: plain `ErrUnauthorized` → `discord-unlinked`, same text |
| Rate limiter | Unchanged: the refusal still goes through `fail()`, so a pending account cannot drive unbounded provider calls (10 failures / 15 min per address) |
| Session | None in any branch; `IssueSession` is never reached |
| Audit | No row for a refusal (a retrying pending member must not flood it); one `Warn` line with the status and client address and no Discord ID |
| Password login | Untouched and still neutral; a pending account has no usable password anyway |
| Enumeration | **Accepted, owner decision D-SR-14.** The caller must complete a Discord consent as that identity, so only accounts the caller already controls can be probed, and `/register` already distinguishes `registration-pending` from `registration-exists` for the same identity |

**Tests.** `TestLoginRefusesPendingAccount` and the rewritten `TestLoginRefusesDisabledAccount` assert the typed error, that it still wraps `ErrUnauthorized`, that no session or audit row is written, and that a pending account is rate-limited after ten failures. `TestDiscordLoginTellsAPendingAccountItIsPending` drives the real callback and the rendered page; `TestDiscordLoginRefusesDisabledAccount` now expects `notice=discord-disabled` and asserts the page no longer claims nothing is linked. `TestDiscordLoginIsNeutralForAnUnlinkedDiscordAccount` still pins the unlinked text.

**Validation.** `gofmt -l` on the five touched files empty, `go build ./...` and `go vet ./...` clean, and `go test` passes for `internal/api`, `internal/service` and `internal/domain`.

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
| 2.10 | 2026-11-22 | Partial-issuance Phase 3 service layer implemented: `Issue` and `Return` operations with atomic lot updates, movement and audit writes; partial/full-stack issue rules; active-row `GivenTo` invariant; stackability enforcement; approved RECEIVE vs RETURN restore integration; on-hand totals; movement history reads; bulk-removal movements and unit counts; idempotency-key persistence and duplicate rejection. Service/fake tests added and updated; `gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` clean. API routes, UI, public inventory and Phase 4 work remain out of scope |
| 2.11 | 2026-11-22 | Partial-issuance Phase 4 JSON API implemented: `POST /api/v1/items/:id/issue`, `POST /api/v1/items/:id/return`, `GET /api/v1/admin/items/:id/movements` and `GET /api/v1/admin/movements/:movementID` under the existing admin group. Issue/return responses include the lot and `currentOnHand`; movement history is bounded with documented filters (`movementType`, `recipient`, `correlationId`, `from`, `to`, `page`, `pageSize`) and an unknown lot returns `404`. The generic `PUT /api/v1/items/:id` contract now rejects `quantity` by name (quantity changes only through movement operations) and permits `givenTo` omission while preserving the active-row invariant. `Idempotency-Key` header transport, `422 validation_failed` for non-stackable partial issues and `409` for stale/duplicate requests are covered by API tests. `gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` clean. HTML issue/return pages and forms remain Phase 5 and were not added |
| 2.12 | 2026-11-23 | Partial-issuance Phase 5 officer UI implemented: HTML Issue (`GET/POST /admin/inventory/:id/issue`), Return (`GET/POST /admin/inventory/:id/return`) and per-record Movement history (`GET /admin/inventory/:id/movements`) pages over the existing Phase 3/4 service and API layer, plus Issue/Return/Movement-history actions and stock visibility on the admin detail and list pages. Forms carry CSRF, `expectedModAt` and a per-render hidden `idempotencyKey` (OD-06); a read-only `Stackability` helper backs the issue-form hint. Handler tests (`admin_movement_pages_test.go`) and template assertions (`inventory_ui_test.go`) were added; `gofmt -l`, `go build ./...`, `go vet ./...` and `go test ./...` clean. No business-rule change; Phase 6 public-inventory changes and Phase 7 cleanup remain out of scope |
| 2.13 | 2026-11-23 | Partial-issuance Option A implemented: partial issue is permitted for any active lot with `Quantity > 1` and the `EQUID` stackability gate was removed from the write path. `service.Issue` decides partial vs whole-stack solely on quantity; `domain.Stackability.AllowsPartialIssue` was removed so classification cannot gate a write; API issue/return handlers no longer look up stackability (`PartialSupported: item.Quantity > 1`) and the issue form label reads "Partial issue". A latent service-fake rollback bug was fixed while in the transaction (the movement snapshot restore is now unconditional); the old stackability gate had masked it. Service, domain, API and page tests updated so a non-stackable-equivalent lot with `Quantity > 1` issues partially, keeps the lot active, leaves `GivenTo` empty and records one `ISSUE` movement with the recipient. No migration or live-database write; `gofmt -l`, `go build ./...`, `go vet ./...` and `go test -count=1 ./...` clean |
| 2.14 | 2026-11-24 | Owner-accepted **Track A** officer-usability improvements implemented (no Phase 6, ledger-integrity, aggregation, status-model, reporting or movement-rule change): a distinct presentation-only `Issued` status badge (`DeletedFg = true AND GivenTo <> ''`) wherever status renders; return-recipient prefill from `GivenTo` (editable); navigation-context preservation (`returnTo`) across Issue/Return/Remove/Restore/Bulk Remove with a single allowed destination and untrusted-value fallback; a state-driven movement-history action banner offering one canonical action; create-form label `Fallback name` → `Name` with metadata-precedence help text; and a non-blocking `name_metadata_mismatch` advisory when a typed name differs from the resolved metadata name (metadata name still stored). Explicitly excluded: issued status filter, return caps/remaining-returnable, EQUID reverse lookup, AJAX/HTMX lookup, item-centric cross-lot history and MOVE/ADJUST/MERGE. `gofmt -l`, `go build ./...`, `go vet ./...` and `go test ./...` clean. Recorded in §6.8 |
| 2.15 | 2026-11-24 | Owner-approved **Enhancement B** implemented: the Admin Inventory list now offers a Restore action only for `DeletedFg = true AND GivenTo IS NULL AND ConsolidatedInto IS NULL` rows, with a new Remove-pattern confirmation page (`GET /admin/inventory/:id/restore`) that posts to the unchanged `POST /admin/inventory/:id/restore` handler. The existing restore service, validation, permissions, movement-writing and optimistic `ModAt` rules are unchanged; the GET page rejects active, issued and consolidated rows with `409`. Enhancement A (partial-issue visibility) was deferred to Phase 6. `gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` clean. Recorded in §6.9 |
| 2.16 | 2026-11-24 | Owner-approved actionable-state design implemented as Phases 0–3: `AdminStatusIssued` added to the status contract so Active/Issued/Removed/All are mutually exclusive (`removed` tightened to `DeletedFg=1 AND GivenTo` empty); `recipient`/`recipient_desc` admin sorts; lifecycle tabs on the filtered admin list; issued-row recipient and badge links to `/admin/inventory?status=issued&givenTo=…`; explicit Active+recipient empty-state hint; state-driven detail actions (Return for issued, Restore only for plain removals); and `returnTo` carried into movement history, its pager and its action banner. No issued-quantity, outstanding, return-cap or aggregation value is computed. D4 (recipient ledger view) and D6 (over-return work) remain deferred; Phase 4 was not started. `gofmt`, `go build ./...`, `go vet ./...` and `go test ./...` clean. Recorded in §6.10 |
| 2.17 | 2026-11-24 | Owner-approved **movement-derived issued rows** (design `AdminInventoryIssuedRowsDesign.md` Revisions 3–4, decisions D1–D6) implemented, reviewed and accepted on 2026-11-25: additive migration `0007_movement_related_movement.sql` adds nullable `InventoryMovement.RelatedMovementID` with an index and a self-referencing FK; the `Issued` tab is projected from outstanding `ISSUE` movements (`Quantity − SUM(attributed RETURN.Quantity) > 0`, so a fully returned issuance leaves the tab while its `RETURN` history stays in `Movements`) and `All` is the UNION of lot rows plus issued rows with whole-stack lot-row suppression; the new `GET|POST /admin/inventory/movements/:movementID/return` route returns one issuance and attributes the `RETURN` to the addressed movement, while the lot-level return attributes FIFO to the earliest outstanding issue; returned units rejoin the same active lot (D1) and a return against an administratively removed lot lands the units in a new active lot (D6). The `Issued` `id` filter matches both the lot id and the movement id (D4). The JSON response shape, on-hand totals and issue/return arithmetic are unchanged (D3); no issued-quantity column was added to the table (D5). `gofmt`, `go build ./...`, `go vet ./...` and `go test -count=1 ./...` clean. Recorded in §6.11 and `AdminInventoryIssuedRowsImplementationPlan.md` |
| 2.18 | 2026-11-25 | Startup failure after 2.17 diagnosed and recovered. Root cause: the migration file name is the version recorded in `schema_migrations.Version VARCHAR(32)`, and `0007_inventory_movement_related_movement` is 40 characters, so the file's DDL applied and `INSERT INTO schema_migrations` failed with `1406 (22001) Data too long for column 'Version'`; with no version row, the next start re-ran the file and failed with `1060 (42S21) Duplicate column name 'RelatedMovementID'`. Recovery with the app stopped: inspected the tracking table and the three objects, confirmed `0` non-NULL `RelatedMovementID` values across 28 movement rows, dropped the self-FK, then the index, then the column, renamed the file to `0007_movement_related_movement.sql` (30 characters), and rebuilt so the runner applied and recorded it. Recording the version by hand was rejected (the version is derived from the file name, so the file would re-run and fail again) and widening the column was rejected (undocumented, and `CREATE TABLE IF NOT EXISTS` does not retrofit existing databases). Guardrails added: a pre-flight version-length check in `migrate.go` that fails before executing any statement, and a 32-character assertion in `TestMigrationManifest`. Live result: `schema_migrations` records `0007_movement_related_movement`, column/index/FK present, `/healthz` `200`. Recorded in §6.11.1 and `AdminInventoryIssuedRowsImplementationPlan.md` §5 |
| 2.19 | 2026-11-25 | Guild-store startup-order recovery implemented per owner decision (connectivity recovers automatically, migrations stay fatal): `mariadb.OpenPool` keeps the bounded pool instead of discarding it on a failed ping; `Store.Run` supervises connectivity with bounded backoff (1s → 30s cap) and publishes readiness only after `Store.Initialize` (migrate + interrupted-import recovery + location/spell backfills, re-run after every reconnect); a failed keepalive probe (5s interval) withdraws readiness. Migration/schema failures log at ERROR, surface through `/readyz` and are fatal after `MariaDB.InitAttempts` attempts (`-mariadb-init-attempts`, `GUILDBANK_MARIADB_INIT_ATTEMPTS`, default 3). `HealthService` reports the supervisor's live state rather than a cached startup error, so `/readyz` turns green without a restart; services keep one stable store reference, and `-consolidate-items` waits up to 60s for readiness. `/healthz` remains the liveness probe. Live Compose validation: with MariaDB stopped the app started degraded (`/readyz` 503 with a live reason), then reached `guild store ready` and `/readyz` 200 in the same process (`RestartCount=0`) once MariaDB came up. `gofmt`, `go build ./...`, `go vet ./...` and `go test -count=1 ./...` clean. Recorded in §6.12 |
| 2.20 | 2026-11-26 | **P0 name-first item identity** delivered (first slice of the approved roles/requests workstream; P1–P5 remain design-only). `domain.UnresolvedEQUID = 4294967295` with the AM-01 manual-entry addendum in `Design.md`; `mariadb.Store.FindItemsByName` (read-only, bounded); `service.ItemNameResolver` (normalize, collation-aware exact match, cached, unique/ambiguous/not-found); `AdminInventoryService.prepareCreate` resolves an omitted EQUID from the name and stores the placeholder with `MetadataMissingFg = 1` and the entered name authoritative when nothing matches; ambiguous names return `422` with a candidate list and create nothing; a client-supplied placeholder is rejected. Guards: spell merge and item consolidation exclude the placeholder; both are tested. API: `GET /api/v1/admin/item-names` plus `unresolvedIdentity` in the item projection. UI: name-first form, candidate radios, `static/admin_inventory.js`, Unresolved identity badge and note. No schema migration. `gofmt`, `go vet ./...`, `go test -count=1 ./...` clean; live `EXPLAIN` uses `idx_name`. Recorded in §6.13 |
| 2.21 | 2026-11-26 | **P1 role foundation** delivered (P2–P4 remain design-only). `domain.Role` hierarchy `Member < Officer < Admin` with fail-closed parsing and `User`/`Actor` capability helpers; **every enabled role can now log in** (session issuance previously required Admin) and the import service uses `Actor.CanManageInventory` instead of `IsAdmin`. `RequireRole(min)` replaces the single admin gate; routes are deny-by-default in `member`/`officer`/`admin` groups (authentication → role → CSRF), with `session`/`logout` moved to `member` so non-admins can sign out. Officers receive imports, all inventory administration and audit/movement history; Administrators keep settings. `/admin` lands each role on its own workspace and navigation is permission-driven. SQLite `0002_user_roles.sql` rebuilds `Users` (CHECK widened, default `Member`, columns preserved, index recreated, `sqlite_sequence` normalised) and clears sessions through the approved cascade. Last-admin primitives (`CountEnabledAdmins`, `SetUserAccessLevel`, `SetUserDisabled`) refuse to leave zero enabled Admins and revoke sessions on disable. New complete route→role matrix plus behavioural enforcement, per-role login/nav and migration/guard tests. `gofmt`, `go vet ./...`, `go test -count=1 ./...` clean; live deployment logged `user_version: 2`. Recorded in §6.14 |
| 2.22 | 2026-11-26 | **P2 user administration** delivered (P3/P4 remain design-only). `domain` gains the account input models (`CreateUserInput`, `ResetPasswordInput`, `ChangePasswordInput`, `DeleteUserInput`) with username rules (1–64 runes, no control characters) and a role parser that rejects unknown input instead of downgrading it. `service.HashUserPassword` applies the 12–72 byte policy **and** the username-equality rule the setup-only hasher could not. New `service.UserAdminService` covers list/filter, create, role change, enable/disable, reset, delete and the owner's own password change; the last-enabled-administrator guards stay in the store. `RequirePasswordChanged()` confines a flagged account to `/account/password`, the password endpoint, logout and session inspection, and login redirects there before honouring `next`. Administrator reset revokes **all** sessions of the target; an own-password change keeps only the caller's; neither an administrator's own disable nor self-delete is allowed. `USER_*` audit events are written best-effort after the SQLite commit, and the P1 gate is closed: `AuditEventTypesFor(role)` scopes non-administrators to inventory/import events in both list and single-row reads. UI adds the account list, create and detail pages plus `/account/password`, and the nav gains **Accounts**. No migration (schema stays at 2). `gofmt`, `go vet ./...`, `go test -count=1 ./...` clean. Recorded in §6.15 |
| 2.23 | 2026-11-26 | **Post-login landing fixed.** HTML login redirected every account to `/admin/settings`; it now lands on the public inventory page (`/`) for Members, Officers and Administrators alike. `redirectAfterLogin` takes the signed-in account and only honours a `next` destination the role may use (`minimumRoleForPage`: `/admin/settings` and `/admin/users` require Admin, `/admin`, `/admin/inventory`, `/admin/imports`, `/admin/audit` require Officer); anything else, including a stale or forbidden saved page, falls back to `/` instead of rendering a Forbidden page. Setup completion also lands on `/`. Open-redirect rejection is unchanged. New `api/login_redirect_test.go` covers the default per role, eleven `next` cases including forbidden and open-redirect values, the anonymous → `/login?next=…` → login loop, and the already-signed-in `/login` path. `gofmt`, `go vet ./...`, `go test -count=1 ./...` clean. Recorded in §6.15 and in the F-01 status note of `OfficerUsabilityReviewV1.md` |
| 2.24 | 2026-11-26 | **P3 Phase A profile characters** delivered (Discord phases B–C, requests P3 Phase D and self-registration Phase E remain design-only). `0003_user_profiles.sql` adds the three nullable Discord columns via `ALTER TABLE ADD COLUMN` (metadata-only: no rebuild, nobody signed out) plus `UserCharacters` with a global `COLLATE NOCASE` unique name index and a cascade from `Users`. Name rules are 3–15 ASCII letters with a cap of 10 per account; the cap is checked inside the insert transaction and duplicates return `409` with a `characterName` field message. Storage is ownership-scoped in every query, so another account's character ID is `404`. `CharacterService` adds self-service list/add/remove and an Administrator-only removal that can never move ownership (D4). UI: `/account/profile` with list, add form and empty state, a **Profile** nav link for every signed-in user, and the account's names on the administrator user detail page. API: `/api/v1/me/characters` (GET/POST/DELETE plus a POST form adapter) and the administrator pair under `/api/v1/users/:id/characters/:characterID`. `CHARACTER_ADDED`/`CHARACTER_REMOVED` audit events are written best-effort and scoped to Administrators. Schema version 3. `gofmt`, `go vet ./...`, `go test -count=1 ./...` clean. Recorded in §6.16 |
| 2.25 | 2026-11-26 | **P3 Phase B Discord account linking** delivered (Discord sign-in, self-registration and requests remain out of scope). Settings gain `PublicBaseUrl` (origin only), `DiscordEnabled`, `DiscordClientId`, `DiscordClientSecret` (secret, blank keeps the stored value) and optional `DiscordGuildId`; the settings page renders the flag as a select and validates origins, snowflakes and booleans. `internal/platform/discord` implements the token exchange with Basic client credentials plus `/users/@me` and `/users/@me/guilds`, bounded to 10 s and logging only a status and a bounded body excerpt. Linking uses the `0003` columns through `DiscordIdentity`, `DiscordUserIDOwner`, `SetDiscordIdentity` and `ClearDiscordIdentity`; `ux_users_discord` keeps one Discord identity per account. `POST /account/discord/link` issues a signed state bound to the account **and** session in a SameSite/HttpOnly cookie and requests `scope=identify` only; `GET /account/discord/callback` requires the matching cookie, verifies signature and expiry, clears the cookie first (single use) and redirects to `/account/profile` with a fixed notice code. A Discord identity owned by another account is refused; a provider failure writes nothing; unlinking needs only the session (D8) and leaves the account intact. Optional guild membership is checked at link time and recorded as `guildVerified` in the `DISCORD_LINKED` audit row, which (with `DISCORD_UNLINKED`) is Administrator-scoped and never contains a token or secret. The profile page gained a Discord card. Password login is unchanged and `MustChangePassword` still confines the account. No schema change: version 3. `gofmt`, `go vet ./...` and the package-by-package suite pass. Recorded in §6.17 |
| 2.26 | 2026-11-26 | **P3 Phase C Discord sign-in** delivered (self-registration, Discord-only accounts and requests remain out of scope). A linked account may authenticate with either its password or Discord; both resolve to the same row and Discord is never a registration path. `GET /auth/discord` is public, issues a state scoped `login` (flow, expiry, `next` and nonce inside the HMAC payload) in its own SameSite/HttpOnly cookie, and redirects to Discord with the separate portal registration `/auth/discord/callback`; an existing valid session short-circuits to its usual destination. `GET /auth/discord/callback` enforces double submit, signature, scope, expiry and `safeNext`+`authorizedNext`, then resolves the account through `UserByDiscordID`. An unlinked or disabled account produces the identical neutral `ErrUnauthorized`, creates nothing and modifies nothing. Sessions come from `AuthService.IssueSession`, so idle/absolute policy and the `DisabledFg` re-check are the password path's own; the cookie and CSRF rotation mirror `authHandler.establish`, `MustChangePassword` still wins over `next`, and a dedicated per-address limiter (10 failures / 15 min) bounds provider calls. `DISCORD_LOGIN` is audited with `method=discord` and no credentials. A Phase B link state is refused by the sign-in callback and vice versa. The login page shows the Discord link only when sign-in is fully configured; the password form is unchanged. No schema change: version 3. `gofmt`, `go build`, `go vet ./...` and the package-by-package suite pass. Recorded in §6.18 |
| 2.27 | 2026-11-26 | **Defect fix: Discord link button did not navigate.** `POST /account/discord/link` returned the correct `303` to `https://discord.com/oauth2/authorize…`, but the browser cancelled the cross-origin navigation because the page's `Content-Security-Policy` contains `form-action 'self'`, which browsers apply to a form submission's whole redirect chain; the state cookie was stored, so pasting the URL completed the link. The profile card now renders an anchor to `GET /account/discord/link` (served alongside `POST`, which stays for API clients returning `authorizeUrl` JSON). State signing, session binding, TTL, callback validation and identity logic are untouched. `TestProfileDiscordSectionFollowsSettings` guards against a form reappearing, `startDiscordLink` exercises the GET navigation without CSRF, `TestDiscordLinkStartJsonContract` keeps the JSON contract, and the unavailable paths are covered for both methods. `gofmt`, `go build`, `go vet ./...` and the `api` suite pass. Recorded in §6.19 |
| 2.28 | 2026-11-26 | **P4-SR self-service registration** delivered (requests and request approval remain future workstreams). `0004_self_registration.sql` adds `AccountStatus` (`Approved`/`PendingApproval`, `CHECK`ed) plus nullable `Origin` and `GuildVerifiedAt` and the `ix_users_status` index — additive, no rebuild, nobody signed out. `domain.AccountStatus` parses blank as Approved and anything unknown as PendingApproval, and `User.IsEnabled()` is now `IsApproved() && !Disabled`, so every existing authentication gate refuses a pending account without being touched. `CreateRegisteredUser` writes the account and its Discord identity in one statement (`ux_users_username`/`ux_users_discord` remain the authorities); `SetUserAccountStatus` withdraws approval and deletes sessions in one transaction. New public flow `/register`, `/register/discord`, `/register/discord/callback`, `GET|POST /register/complete` with a registration-scoped state, a dedicated signed ticket cookie and `scope=identify guilds` **only** there (D-SR-13: link and sign-in keep `identify`, and `authorizeURL` now takes the scope explicitly). A verified guild member is created `Approved` and signed in through `AuthService.IssueSession`; anything else (not a member, provider error, no guild ID) is created `PendingApproval` with no session. Administrators gain `POST /admin/users/:id/approve`, `POST /api/v1/users/:id/approve` and the withdraw pair, the `?status=` queue filter, the pending banner and the `accountStatus`/`pending`/`origin`/`guildVerifiedAt` response fields; a role change is refused while pending and an operator cannot withdraw their own approval. `SelfRegistrationEnabled` (default off) gates the flow and the login-page link. Schema version 4. `gofmt -l`, `go build ./...`, `go vet ./...` and the package-by-package suite pass. Recorded in §6.20 |
| 2.29 | 2026-11-26 | **Discord sign-in diagnoses the refusal** (owner decision D-SR-14). Every refusal after a linked identity was resolved used to produce the one message *"No GuildBank account is linked to that Discord account"*, which is wrong for a self-registered member awaiting approval and for a suspended account, and sends both to a password form they cannot use. `DiscordService.Login` now returns `*domain.AccountStateError{Status, Disabled}` from the `domain.User` it already holds — same single `UserByDiscordID` lookup, no new query, no schema change — and the type unwraps to `ErrUnauthorized`, so callers that only know "not allowed" and their `errors.Is` tests are unaffected. `discordHandler.loginCallback` matches it **before** the `ErrUnauthorized` branch and redirects with the fixed codes `discord-pending` / `discord-disabled` (`Disabled` wins when both apply); only fixed text is shown, never a username or Discord ID. `discord-unlinked`, the neutral `ErrUnauthorized` for an unknown identity, the 10/15-minute limiter (the refusal still counts as a failure), the no-session/no-audit rule and the unchanged neutral password login are all preserved. `AccountStateError` is new in `domain/identity.go`, with rewritten `TestLoginRefusesDisabledAccount`, new `TestLoginRefusesPendingAccount` (typed error, `ErrUnauthorized` unwrap, no session or audit row, rate-limited after ten failures), new `api` `TestDiscordLoginTellsAPendingAccountItIsPending`, and the disabled `api` test now asserts `notice=discord-disabled` and the rendered text. `gofmt`, `go build`, `go vet ./...` and the `domain`/`service`/`api` suites pass. Recorded in §6.21 and `SelfRegistrationDesign.md` §14/§17 |



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
one-row-per-line model; migration `0006` creates the append-only
`InventoryMovement` ledger and `0007_movement_related_movement` adds the nullable
self-referencing `RelatedMovementID` that attributes a `RETURN` to the `ISSUE` it
closes. Phase 3 supports upload,
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
general audit outbox, user management and dashboard remain later phases;
movement-derived issued rows on the Admin Inventory page (§6.11) are the latest
completed change, and the one-time return-attribution backfill (design §11.1) is the
one outstanding data step for it. The guild store now also survives a MariaDB
startup race and a mid-life outage without a container restart (§6.12): the pool is
retained, initialization is retried, readiness is published only after a successful
initialize, and only migration failures remain fatal. Name-first item entry (§6.13)
is the first delivered slice of the approved roles/requests workstream, and the P1
role foundation (§6.14) is the second: Member/Officer/Admin accounts can sign in,
routes are classified by role, and the last enabled Administrator is protected.
P2 user administration (§6.15) is the third: Administrators create, promote,
demote, disable, re-enable, reset and delete accounts, temporary passwords are
forced to change at the next sign-in, and Officers cannot read `USER_*` audit
rows. P3 Phase A (§6.16) is the fourth: every account keeps up to ten globally
unique character names and Administrators can remove a name they do not own.
P3 Phase B (§6.17) adds Discord account linking as an additive identity, gated
by Administrator settings. P3 Phase C (§6.18) lets a linked account sign in with
Discord as an alternative to its password, through the same session service and
the same role, disabled-account and password-change gates. Self-registration, the
request workflow (P3 Phase D/P4) and Discord-only accounts remain design-only.

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
