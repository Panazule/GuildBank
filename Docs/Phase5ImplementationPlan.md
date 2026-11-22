# Phase 5 Implementation Plan — Administrative Inventory Management

**Status:** **APPROVED (owner, 2026-11-22) — ALL DECISIONS PD5-01–PD5-46 APPROVED.** The owner's 2026-11-22 implementation instruction additionally approved the administrative and public inventory UI amendments in §9.5 (`PD5-33`–`PD5-38`) and reaffirmed the lifecycle date rule of §16.6 unchanged (`PD5-39`). No Phase 5 application code existed when those amendments were recorded. On 2026-11-22 the owner approved the Phase 5 reconciliation, including **PD5-27** (amended lifecycle date rule), **PD5-31** (correlation-ID bound) and **PD5-32** (per-row date editing), approved PD5-01–PD5-26 and PD5-28–PD5-30 as reviewed, and accepted `PD-05`, `PD-18`, `E-02` and the `AM-02` refresh residual as out of scope for Phase 5 (§16). Implementation is authorized; the review package `Docs/Phase5ApprovalReview.md` records the rationale, tradeoffs and risk classification of that approval, and §16.6 lists the three date sub-rules confirmed with it. Phase 5 was then approved with one further owner decision, **PD5-40** (inclusive `Bazaar` availability), which resolved the last open wording question. On 2026-11-22 the owner also approved the post-Phase-5 review items as **PD5-41–PD5-46** (AI-01/AI-02, IMP-01/IMP-02/IMP-04/IMP-05/IMP-06); those are implemented in `Docs/ImportAdminImprovements.md` and do not reopen any earlier decision.
**Date:** 2026-11-22
**Revision:** 1.5 (adds PD5-41–PD5-46, the owner-approved post-Phase-5 review decisions; all decisions PD5-01–PD5-46 approved)
**Implementation status:** implemented in the working tree on 2026-11-22 (migration → backfill → normalization → service → repository → API → admin UI → tests) and live-validated on the Compose development topology the same day, including the PD5-40 boundary; evidence is in `Docs/Phase5Validation.md`, and `Docs/ProjectStatus.md` §1.7 records the current state and the known deviations. No open question remains. Still outstanding: the §10.1 performance gates (250k/1M fixtures), the §13.2/§13.3 opt-in database and performance suites, the CI-only `-race` run, the §13.5 manual checklist and the §12.5 downstream documentation corrections.
**Scope owner:** single developer
**Implementation-plan mapping:** this is the user's "Phase 5" (administrative inventory management). It corresponds to `Docs/ImplementationPlan.md` Phase 5 and builds on the implemented Phase 3 import pipeline and Phase 4 public read path.
**Companion documents:** `Design.md` v1.6.1, `Docs/ArchitectureReview.md`, `Docs/DatabaseDesign.md`, `Docs/ImplementationPlan.md`, `Docs/ProjectStatus.md`, `Docs/ProjectStructure.md`, `Docs/Phase3ImplementationPlan.md`, `Docs/Phase4ImplementationPlan.md`, `Docs/Phase4Validation.md`, `Docs/Phase5ApprovalReview.md` (decision rationale, tradeoffs, future-phase impact and risk classification for PD5-01–PD5-32 and R5-01–R5-16).

**Normative inputs**

- `Design.md` L19–L30 (capabilities), L261–L374 (`GuildBankItems`, `Audit`, `ImportBatch`), L519–L558 (inventory edit rules), L699–L728 (natural sorting), L736–L795 (API/EQResource), L812–L850 (admin inventory screen), D-01–D-13, AM-01–AM-05.
- `Docs/DatabaseDesign.md` §4.2 (`GuildBankItems`), §4.5 (`Audit`), §6 (integrity), §9 (query design).
- `Docs/ImplementationPlan.md` Phase 5, §4 (API surface and conventions), §6 (test strategy), §9 (phase exit gates).
- `Docs/ArchitectureReview.md` E-02, E-07, E-09, E-11, E-12, E-16, AM-02, MR-06, MR-07, MR-14, MR-19, DC-10, DC-13, DC-17.
- The implemented `internal/domain`, `internal/service`, `internal/repository/mariadb`, `internal/api`, and `internal/web` code. Where documentation and code disagree, the code is the current baseline and the difference is called out.

Where this plan proposes behavior not fixed by the specification, it is recorded in §3 with its approval status. **SPEC** means the behavior exists in the specification. **BASELINE** means it exists in the implemented code and is not redesigned here. **OPEN** means the repository contains no authoritative disposition and this plan does not silently choose one.

---

## 1. Outcome, scope and acceptance criteria

When Phase 5 is complete, an authenticated administrator can:

1. Open `/admin/inventory` and browse active and removed guild-bank records with deterministic pagination.
2. Search and filter by ID, EQUID, name, item type, location group, recipient and metadata state.
3. Sort by name, location, added/open/bazaar date or last modification.
4. Add an item by EQUID. If item metadata exists, name, class and race are resolved server-side; if metadata is absent, a required fallback name is retained and `MetadataMissingFg` is set.
5. Add a stack of an ordinary item; one row stores the entered quantity (amended 2026-11-22 by QTY-07; the original “one row per unit” step is superseded).
6. Add a stack of a spell; the quantity is merged into a single active spell row when one exists.
7. Edit quantity (both types under QTY-08: items `1..100000`, spells the column ceiling), source location, recipient and the two lifecycle dates on an existing row; identity, type and `DateAdded` remain immutable.
8. Remove a row by soft delete and restore it later.
9. See a stale-write conflict instead of silently overwriting a concurrent change.
10. See a concise audit history for one inventory record and search the administrative audit viewer by event, actor, record and correlation ID.
11. Perform the same operations through versioned JSON endpoints with admin authorization, CSRF enforcement for browser sessions, strict validation and consistent errors.
12. See normalized location groups and sort values produced by the server, never by client-supplied grouping.

Phase 5 is complete when §1.2, the contract gates in §5–§11, the test gates in §13, and live Compose validation pass; the embedded migration manifest is consistent; the location backfill is proven idempotent; and required documentation is updated.

### 1.1 Non-goals

- No user administration, role editor, permission matrix or account deletion. There is no `/api/v1/users` endpoint today, and Phase 5 does not add one.
- No change to login, session, CSRF or bootstrap behavior. PD-05 and PD-18 remain as previously recorded.
- No import upload, parser, validation, preview or commit redesign. Import commit only receives the `LocationGroup`/`LocationSort` columns and the shared row-insert helper.
- No public inventory response changes. Public DTOs continue to exclude `SourceLocation`, `GivenTo`, `DeletedFg`, `ModBy`, `ModByUserID`, `LocationSort` and audit data.
- No item-metadata refresh, reconciliation worker or automatic correction of previously imported rows (the AM-02 residual is accepted as out of scope, §16.4).
- No generic settings audit, login/logout audit or SQLite-to-MariaDB audit outbox. Phase 5 audits MariaDB inventory mutations and exposes the existing MariaDB `Audit` rows. E-09 and any Phase 7 outbox remain open.
- No hard delete, bulk delete, undo stack or recycle-bin retention policy.
- No new JavaScript framework, HTMX, CDN, Bootstrap or client-side rendering. Progressive HTML forms are the baseline.
- No database grants, backup, TLS or secret-management change (E-11 remains operational).

### 1.2 Acceptance criteria

| ID | Criterion | Verification |
|---|---|---|
| AC5-01 | `GET /admin/inventory` requires an authenticated admin and renders active rows by default. | API/UI tests; unauthenticated and non-admin redirect/403. |
| AC5-02 | Admin list supports `q`, `itemType`, `locationGroup`, `givenTo`, `equid`, `id`, `metadata`, `status`, `sort`, `page`, `pageSize` with exactly the documented defaults and limits. | Table-driven service/API tests. |
| AC5-03 | Adding an existing item with quantity `1` creates one active row with resolved name/class/race, normalized location, `DateAdded` = the UTC commit date, `DateOpen`/`DateBazaar` defaulted from settings unless an override was supplied, and one `ITEM_CREATED` audit row. | Integration test asserts row, generated columns and audit in one transaction. |
| AC5-04 | Adding quantity `N` of an item creates exactly `N` rows sharing identity, location, recipient, dates and actor; no partial stack survives an error. | Repository integration test with forced failure mid-insert. |
| AC5-05 | When an active spell row exists, adding a spell merges quantity into the lowest-ID active spell with the same EQUID, preserves that row's location, recipient and dates, and writes `ITEM_UPDATED` with old/new quantity. When none exists, it creates one spell row and writes `ITEM_CREATED`. | Integration test including a duplicate active spell. |
| AC5-06 | Adding a missing-metadata EQUID without a fallback name returns `422`; with a fallback name sets `MetadataMissingFg=1`, blank class/race, and keeps the fallback. | Service and API tests. |
| AC5-07 | A value-changing update changes only quantity (spells only; items are fixed at `1` by `ck_gbi_item_one`), `SourceLocation`, normalized location fields, `GivenTo`, `DateOpen` and `DateBazaar`; it updates `ModAt`/`ModBy`/`ModByUserID` and writes `ITEM_UPDATED`. `DateAdded` never changes. | Integration test compares identity columns and `DateAdded` before and after; API test asserts an item edit with `quantity != 1` returns `422` and changes nothing, and a date edit outside `DateAdded <= DateOpen <= DateBazaar` returns `422`. |
| AC5-08 | An update or removal whose `expectedModAt` does not equal the locked row's `ModAt` returns `409` and changes nothing. | Two-client concurrency test. |
| AC5-09 | Removal sets `DeletedFg=1` and writes `ITEM_REMOVED`; repeat removal returns `409`; restore sets `DeletedFg=0` and writes `ITEM_UPDATED` with a restore note. | Integration and API tests. |
| AC5-10 | Every successful state-changing create, update, remove or restore writes exactly one audit row in the same MariaDB transaction with actor, UTC timestamp, old/new JSON, correlation ID and source IP. A no-op update writes none. | Count audit rows before/after; force failure and assert zero. |
| AC5-11 | Audit JSON never contains secrets, session tokens, CSRF tokens, request bodies or raw SQL. Recipient is included only as `givenTo`. | Audit serialization unit test and code review. |
| AC5-12 | `/admin/audit` and `GET /api/v1/audit` require admin; filters are parameterized and wildcard-escaped where `LIKE` is used; page size is capped at 200. | Filter tests with `%`, `_`, `\`, quotes and long input. |
| AC5-13 | Admin HTML forms are server-rendered, work without JavaScript, include CSRF tokens and show field-specific errors and fixed notices. | Template tests and no-JS browser smoke test. |
| AC5-14 | Public JSON, public HTML and `/api/v1/items/{id}` never expose admin-only fields, including after add/restore. | DTO allowlist test. |
| AC5-15 | Existing imports still commit at the approved rate, and newly imported rows have non-null normalized location fields. | Import regression test and benchmark. |
| AC5-16 | The location backfill is restartable, idempotent, concurrent-safe and does not alter raw `SourceLocation`. | Kill/restart test; checksum raw values; dual-start test. |
| AC5-17 | Admin list and audit first-page p95 targets in §10.1 are measured and recorded; a miss is accepted only with the captured `EXPLAIN` and an owner-approved follow-up plan. | `go test -bench` / `benchstat` evidence in a Phase 5 validation document. |
| AC5-18 | `go test ./...`, `go test -race ./...`, `go vet ./...`, template parsing and `docker compose` smoke pass. | CI/local command transcript. |
| AC5-19 | Changing `GuildOpenDays` or `BazaarDays` in Settings never modifies an existing row's `DateOpen`/`DateBazaar`; only a new create, or an explicit per-item edit or reset, reflects the new policy. | Integration test changes settings after rows exist, asserts every stored date is unchanged, then asserts an explicit reset on one row uses the new offset. |
| AC5-20 | The public inventory page and every admin inventory/audit page expand with the desktop viewport through a responsive max-width; no page keeps the old fixed `80rem` cap, and the layout stays usable at 320 px width. | CSS assertions on `main.card.inventory-card`/admin cards; rendered-page check at 320 px and 1920 px in manual acceptance. |
| AC5-21 | The main public inventory grid shows no `GuildBankItemID` or `EQUID` value; both remain on the public detail page, the admin list, the admin detail and the admin edit form. | Template test asserts the list row carries neither identifier; detail/admin template tests assert both are present. |
| AC5-22 | Desktop filter controls use a compact multi-column grid for both the public filters (search, class, race, item type, sort, actions) and the admin filters (search, id, equid, location group, recipient, metadata, item type, status, sort, page size, actions); below 640 px each grid collapses to one column. | CSS assertions plus template tests that the filter controls are siblings in one grid container. |
| AC5-23 | The public list's `EQResource` text column is replaced by an icon-only link with `title`/`aria-label` "View on EQResource", `target="_blank"` and `rel="noopener noreferrer"`; a row without a URL renders a muted dash with an `sr-only` explanation instead of a text word. | Template test asserts the anchor carries the icon, title, `target` and `rel`; a no-URL row asserts the dash and `sr-only` text. |
| AC5-24 | The public availability value is dynamic: `Mains` before `DateOpen`, `Persona` from `DateOpen` inclusive, `Bazaar` from `DateBazaar` inclusive; the JSON `availability` enum is unchanged. | Table-driven test with a fake clock at each boundary; API contract test asserts the enum values are unchanged. |
| AC5-25 | The approved lifecycle date rule is unchanged: `DateAdded` = the UTC commit date, `DateOpen`/`DateBazaar` default from Settings, Settings changes are non-retroactive, and administrators may override both. The detail page shows the three specific dates. | Existing AC5-03/AC5-07/AC5-19 tests plus a detail-page assertion for the three date labels and ISO values. |

---

## 2. Current baseline and evidence

### 2.1 What already exists

- `GuildBankItems` is keyed by `GuildBankItemID` and is the central inventory table. It stores item identity, decoded strings, dates, quantity, raw `SourceLocation`, `GivenTo`, soft-delete, natural-sort and modification attribution.
- The public read path is implemented in `internal/service/inventory.go`, `internal/repository/mariadb`, `internal/api`, and `internal/web/templates`. Public DTOs already strip internal fields.
- `ImportService.Commit` performs a bounded, transactional import merge using an advisory lock and `domain.AuditEntry`. `insertAudit` inserts audit rows inside the same transaction. `importCommitTx.insertInventory` inserts up to 500 rows per statement and currently supplies 17 values.
- `ErrConflict`, `ErrNotFound`, `ErrForbidden`, `ValidationError`, `ErrUnavailable` and `RetryableError` already have API mappings in `internal/api/errors.go`.
- Auth middleware attaches `service.AuthenticatedSession` to the Gin context (`CurrentSession(c)`); it does **not** supply a `domain.Actor`. Handlers build `domain.Actor{UserID, UserName, IsAdmin}` themselves, exactly as `internal/api/imports.go` does for upload and commit. CSRF middleware and request-ID/correlation middleware (`GetRequestID(c)`) already exist.
- `internal/domain/import.go` defines `AuditEntry`; the `Audit` table exists in `0001_guild_store.sql`.
- The migration runner embeds `internal/repository/mariadb/migrations` into `migrationFiles` with `//go:embed migrations/*.sql`. That directory is the only migration source; there is no top-level `migrations/` directory.
- `service.NormalizeLocation` exists for import staging, but `GuildBankItems` has no persisted `LocationGroup`/`LocationSort`; no admin location wrapper, admin inventory repository, audit read repository, admin inventory API or admin inventory UI exists.
- JSON decoding precedents exist but are inconsistent: `internal/api/imports.go` (selection PUT) uses `json.Decoder` with `DisallowUnknownFields` behind an `io.LimitReader`, and `internal/api/settings.go` uses `c.ShouldBindJSON`. Neither rejects duplicate object keys nor enforces a single trailing value, so Phase 5 must deliberately choose and test its decoder (PD5-18).

### 2.2 Database reality checks

- `SourceLocation` is `VARCHAR(128) NULL` with the table's case-insensitive collation. `GivenTo` is also `VARCHAR(128) NULL`; no length check exists above the database.
- `GuildBankItems.Name`, `ClassString` and `RaceString` are `NOT NULL` in `0001_guild_store.sql`; `NameSort` is nullable. Admin code must scan each with the correct nullability and must not assume every string column is non-null.
- **Amended 2026-11-22 by QTY-01/QTY-02:** `GuildBankItems` carried `CHECK (ItemType = 'Spell' OR Quantity = 1)` (`ck_gbi_item_one`); `0005_item_quantity_model.sql` drops it. `CHECK (Quantity >= 1)` (`ck_gbi_quantity`) remains, so the service still rejects zero before SQL but an `Item` row may store `1..100000`. See §3 PD5-07/PD5-10.
- `DateAdded`, `DateOpen` and `DateBazaar` are `DATE NOT NULL` with `CHECK (DateBazaar >= DateAdded)` (`ck_gbi_bazaar`) and `CHECK (DateOpen >= DateAdded)` (`ck_gbi_open`); the table has no constraint that keeps `DateOpen <= DateBazaar`, which is a design rule the service must enforce (E-10). `DateAdded` comes from the service clock, while the two later dates may be overridden per row under the approved rule (PD5-32), so every write must satisfy all three conditions before it reaches SQL.
- `ModAt` is `DATETIME(6)` and is the only optimistic-concurrency token. `ModByUserID` is `INT NULL`; the domain actor ID is `int64`.
- `Audit.OldValues`/`NewValues` are JSON columns. `insertAudit` already marshals values with `nullableJSON`. `Audit` also has nullable `UserID`, `TableName`, `RecordID`, `Notes`, `CorrelationID` and `SourceIP`; audit reads must scan them through `sql.Null*` and use the zero-value mapping in §5.5.
- The existing `ix_gbi_given (GivenTo)` index does not include `DeletedFg`; it is insufficient for a deleted-state-first admin filter.
- Import commit's multi-row insert assumes `LastInsertId()+i` for generated IDs. Phase 5 reuses this helper rather than inventing a second insert convention; §15 (R5-07) records the limitation and test requirement.

### 2.3 Terms confirmed at documentation level only

- "Open day" and "bazaar day" are used by `Design.md` and `Docs/ImplementationPlan.md` as settings-derived date offsets. The runtime setting names/values are the baseline; Phase 5 consumes them through the existing service path.
- "Location group" is the D-05 normalized value used for filtering. It has no persisted representation today. Phase 5 creates it, but does not retroactively change imported raw values.
- "Audit viewer" is the admin/ops screen implied by the audit table and ImplementationPlan. No UI or read endpoint exists today.

### 2.4 Reconciliation against the repository (2026-11-21)

Checked against the current working tree; no code was changed. The statements below were verified in source and are the anchors the rest of this plan relies on.

| Claim | Verified source |
|---|---|
| Migrations directory holds exactly `0001_guild_store.sql`, `0002_public_inventory_indexes.sql`; `0003` is the only file this phase adds | `internal/repository/mariadb/migrations` listing, `migrate.go` (`//go:embed migrations/*.sql`, `migrationFiles`) |
| Runner sorts versions, skips applied ones, refuses an embedded version older than the maximum applied version, then records `schema_migrations` | `mariadb.Migrate` |
| Commit lock name/timeouts | `commitLockName = "guildbank:commit:"` in `imports_commit.go`, `migrateLockName` in `migrate.go`; both `GET_LOCK` 30 s with `LEFT(SHA2(DATABASE(), 256), 32)` |
| `insertInventory` inserts 17 values per row in 500-row chunks and derives IDs from `LastInsertId()+i` | `imports_commit.go` (`importChunkSize = 500` in `imports.go`) |
| Constant values reused by the admin limits table | `ImportMaxName/ImportMaxLocation = 128`, `ImportMaxRows = 100_000`, `ImportMaxQuantity = 10_000`, `PublicInventoryMaxSearchRunes = 128`, `PublicInventoryMaxPageSize = 100`, `PublicInventoryMaxPage = 10_000` |
| Persisted sort keys are bounded, not raw | `import_upload.go` writes `TruncateRunes(location.Sort, 80)` and `TruncateRunes(NaturalSortKey(name), 160)`; `import_commit.go` repeats the 160 bound; `TruncateRunes` lives in `import_sort.go` |
| Quantity rules | `0001_guild_store.sql` `ck_gbi_quantity CHECK (Quantity >= 1)`; `0005_item_quantity_model.sql` drops `ck_gbi_item_one` (QTY-02) |
| Lifecycle dates are constrained at the table and derived by one shared helper | `0001_guild_store.sql` `ck_gbi_bazaar CHECK (DateBazaar >= DateAdded)` and `ck_gbi_open CHECK (DateOpen >= DateAdded)`, all three date columns `DATE NOT NULL`; `dateFields(now, openDays, bazaarDays)` in `import_commit.go` builds `commitDates{added,open,bazaar}`, and `importDateSetting` parses the offsets from the settings map. The admin service calls the same unexported helper because it lives in the same package |
| Event types already written | `IMPORT_CREATED`, `IMPORT_COMMITTED`, `ITEM_CREATED`, `ITEM_UPDATED` (service layer); `ITEM_REMOVED` is new |
| Insert paths for audit | unexported `insertAudit(ctx, *sql.Tx, entries)` in `imports.go`, reused by `importCommitTx.WriteAudit` |
| Request body caps | `SecurityHeaders` wraps every request in `http.MaxBytesReader` at 1 MiB, except the 10 MiB import upload and the 2 MiB selection PUT |
| CSRF contract | signed double-submit cookie `guildbank_csrf`, header `X-CSRF-Token` or field `csrf_token`, unsafe methods only; missing/invalid → `403` |
| Admin route protection | existing group `RequireAuth` → CSRF → `RequireAdmin`; anonymous HTML → `303 /login?next=…`, non-admin HTML → `303 /login`, API → `401`/`403` |
| Template conventions | `table-scroll` `role="region"` + `tabindex="0"`, `caption.sr-only`, `th scope="col"`, `aria-live="polite"`, `role="status"`; nav lives only in `partials.html` |

Corrections applied to earlier drafts of this plan because they did not match the code: the middleware does not supply `domain.Actor` (handlers build it); `DisallowUnknownFields` does exist in `imports.go` and `ShouldBindJSON` exists in `settings.go`; a static `new` route does not need to be registered before `:id` on Gin v1.12; the nav partial is in `partials.html`, not `inventory_list.html`; the D-05 rules live in `import_normalize.go` and are not moved to a new `location.go`; the startup backfill cannot call the unexported `normalizeLocationValue`, so it uses the exported wrapper; derived sort keys must pass `TruncateRunes` at the 160/80 column bounds; an `Item` row's quantity is fixed at `1` by `ck_gbi_item_one`, so it is not editable.

One new defect was found while reconciling and is now covered by PD5-31 and its row in the §13 `internal/api` matrix: `Audit.CorrelationID` is `CHAR(36)` while inbound `X-Request-ID` is accepted up to 64 characters, so a long proxy header can break an otherwise valid write.

**Approval log.**

1. **(owner, 2026-11-22, first pass)** PD5-31 approved — `requestIDPattern` narrows to `^[A-Za-z0-9._-]{8,36}$`. The inventory lifecycle date rule approved — `DateAdded` is the UTC date the row is committed into the guild bank and is never editable; `DateOpen` defaults to `DateAdded + GuildOpenDays` and `DateBazaar` defaults to `DateAdded + BazaarDays`, both read from `Settings`; administrators may override `DateOpen` and `DateBazaar` per item (PD5-32); a later `Settings` change never rewrites existing rows. This amended PD5-27, whose earlier text said editing does not change dates.
2. **(owner, 2026-11-22, second pass — plan reconciliation approved)** PD5-27, PD5-31 and PD5-32 approved as recorded above. PD5-01–PD5-26 and PD5-28–PD5-30 approved after review of `Docs/Phase5ApprovalReview.md`, which states one rationale paragraph, the primary tradeoffs, the impact on later phases and a recommendation for each. `PD-05`, `PD-18`, `E-02` and the `AM-02` refresh/reconciliation residual accepted as **out of scope for Phase 5** (§16). The three application sub-rules in §16.6 are confirmed as the operative reading of the approved date rule. No decision remains `PROPOSED` or `OPEN` for Phase 5.
3. **(owner, 2026-11-22, third pass — UI amendments approved with the implementation instruction)** The administrative and public inventory UI requirements in §9.5 are approved as decisions **PD5-33–PD5-38**, and the lifecycle date rule is reaffirmed unchanged as **PD5-39** (no edit to PD5-27/PD5-32). This pass changes presentation only: responsive max-width, removal of `GuildBankItemID`/`EQUID` from the main grid, a compact multi-column filter grid, an icon-only EQResource link, reduced wrapping, and dynamic `Mains`/`Persona`/`Bazaar` availability labels with the three specific dates on the detail page. No decision in PD5-01–PD5-32 is reopened.
4. **(owner, 2026-11-22, fourth pass — Phase 5 approved with one decision)** PD5-40 approved: availability uses inclusive thresholds, so `Bazaar` begins on the `DateBazaar` day itself (`Mains` while `today < DateOpen`; `Persona` while `DateOpen <= today < DateBazaar`; `Bazaar` when `today >= DateBazaar`). This resolves the §9.5 boundary note in favour of the label semantics of `UI-07` and aligns `DateBazaar` with the inclusive `DateOpen`. The only behavioural change is the public `availability` value on the `DateBazaar` day itself, from `open` to `bazaar`; no route, field, filter, date rule or admin contract changes. PD5-37's "no public JSON change" clause is superseded on that single day by this decision. Implemented and validated the same day.

---

## 3. Decisions and approval status

| ID | Decision | Rationale | Status |
|---|---|---|---|
| PD5-01 | **Persist `LocationGroup VARCHAR(64) NULL` and `LocationSort VARCHAR(80) NULL` on `GuildBankItems`** in forward migration `0003_admin_inventory.sql`, after `SourceLocation`. `SourceLocation` remains the user-visible raw value and is never rewritten by backfill. | D-05 requires filtering by location; storing the derived fields avoids re-normalizing every row at query time and makes pagination/index use possible. Keeping both derived fields nullable permits a restartable backfill and safe rollback. | **APPROVED (owner, 2026-11-22)** |
| PD5-02 | **Run an idempotent location backfill during startup, after migrations and before the HTTP server starts.** Use keyset batches of 500, a dedicated advisory lock, and a transaction per batch. Log progress every 10,000 rows. Fail startup on error; a rerun resumes at the next null row. | Startup ordering prevents requests/imports from observing partially populated derived fields. Keyset iteration avoids a full-table rescan per batch. A separate lock prevents two replicas from racing, while idempotence makes a crash harmless. | **APPROVED (owner, 2026-11-22)** |
| PD5-03 | **Normalization is centralized in one `normalizeLocationValue` computation behind two wrappers: `service.NormalizeLocation(raw string) NormalizedLocation` (the existing import signature and behavior, unchanged) and `service.NormalizeAdminLocation(raw string) NormalizedLocation`.** Both return the same group/sort; the admin wrapper maps any non-blank raw value whose final group is `Unspecified` (including a malformed reserved prefix or an overlong generated group) to that group and adds one `location_unrecognized` warning with `Field='sourceLocation'`, while the import wrapper keeps its existing `location_blank`/`location_pattern_invalid`/no-warning contract. The shared rules implement exactly D-05, not a general title-caser: trim Unicode whitespace, strip an optional trailing `-Slot<digits>`, then apply the existing case-insensitive anchored rules for `General<digits>`, `Bank<digits>`, `SharedBank<digits>`, `Hoard`/`Hoard<digits>`, and blank → `Unspecified`. `Sort` is `service.NaturalSortKey(Group)` (lowercase runs, zero-padded digit runs joined by ASCII `0x01`), the same key used by import staging. No abbreviations, aliases or spelling variants are invented. | `Design.md` D-05 and AM-04 define the actual rules and canonical `Hoard` spelling; a generic title-caser would create non-spec groups such as "The Bazaar" or "Pok". Mapping unknown input to `Unspecified` is conservative and visible, and the warning tells the admin the raw value was retained but not grouped. | **APPROVED (owner, 2026-11-22)** |
| PD5-04 | **All administrative JSON writes reuse the public resource paths with method-level authorization:** `POST /api/v1/items`, `PUT /api/v1/items/{id}`, `DELETE /api/v1/items/{id}`, `POST /api/v1/items/{id}/restore`. Admin-only reads use `/api/v1/admin/items` and `/api/v1/admin/items/{id}`; audit uses `/api/v1/audit`. | This follows `Docs/ImplementationPlan.md` §4 and D-03 while adding the established `/v1` prefix. Public `GET /api/v1/items` remains unchanged. Separate admin read routes avoid varying a public representation by authentication, which would violate E-07 and PD4. | **APPROVED (owner, 2026-11-22)** |
| PD5-05 | **HTML routes are `/admin/inventory`, `/admin/inventory/new`, `/admin/inventory/{id}`, `/admin/inventory/{id}/edit`, `/admin/inventory/{id}/remove`, and `/admin/audit`.** Mutating forms post to thin adapters: `POST /admin/inventory`, `POST /admin/inventory/{id}`, `POST /admin/inventory/{id}/remove`, `POST /admin/inventory/{id}/restore`. Adapters enforce admin/CSRF, call the same service method and return `303 See Other` with a fixed notice code. | Full-page forms remain usable without JavaScript and avoid duplicating business rules. Fixed notice codes prevent reflected text in redirects. The API remains the canonical machine contract. | **APPROVED (owner, 2026-11-22)** |
| PD5-06 | **A manual add is one atomic operation for one EQUID and one quantity.** Item metadata is resolved before the write transaction. The server derives identity, decoded strings, `NameSort` (bounded to 160 runes by `TruncateRunes`, as import does), `DateAdded` (current UTC date), `DateOpen`, `DateBazaar`, `ModAt`, actor fields and both location fields. The client supplies only EQUID, quantity, optional source location, optional recipient, an optional fallback name used only when metadata is missing, and optional `dateOpen`/`dateBazaar` overrides. **Amended 2026-11-22 by QTY-07:** the write creates one row carrying the entered quantity for both Item and Spell; ordinary items are no longer expanded per unit. | This mirrors Phase 3 import semantics and prevents the browser from becoming authoritative for identity or sort keys. Deriving `DateAdded` now preserves "date entered this guild bank"; the two later dates stay derived by default and are the only date values an administrator may set (PD5-32). | **APPROVED (owner, 2026-11-22)** |
| PD5-07 | **Quantity caps are type- and operation-specific.** Ordinary-item create and update: `1..100,000`; spell create and update: `1..4,294,967,295`. Both types store the quantity on one row; the ordinary-item cap bounds one column value, not a row count. Overflow of `existing + added` is rejected before update. **Amended 2026-11-22 by QTY-07/QTY-08.** | An unbounded ordinary stack was previously a row-expansion risk; with one row per line the cap bounds the stored quantity and keeps the manual and import models identical. A spell's quantity is a column update, so the database `INT UNSIGNED` limit is the correct upper bound. Rejecting zero forces removal/restore to be explicit. | **APPROVED (owner, 2026-11-22)** |
| PD5-08 | **Adding a spell merges into the lowest-ID active row with the same EQUID and `ItemType='Spell'`; that row's source location and recipient are preserved and only quantity/modification fields change.** If several active duplicate spell rows exist, the lowest ID is the target and a `duplicate_spell_target` warning is returned; other duplicates are untouched. If none exists, a single new spell row is created with the requested quantity. | Matches the Phase 3 commit merge policy and D-09. Preserving the target's location avoids silently moving an existing stack. The warning exposes legacy duplicates without destructive automatic consolidation. | **APPROVED (owner, 2026-11-22)** |
| PD5-09 | **Missing metadata requires a fallback name.** If the EQUID is absent from `Items` and the fallback name is blank/whitespace, return `422 field=name`. Otherwise store the trimmed fallback, blank class/race strings, `MetadataMissingFg=1`, and return a non-fatal `metadata_missing` warning. If metadata exists, any submitted name is ignored (not an error). | Preserves AM-01/AM-02 and D-08. Ignoring a submitted name avoids a field that appears editable but has no effect. The warning lets the admin understand why class/race are blank. | **APPROVED (owner, 2026-11-22)** |
| PD5-10 | **Identity and lifecycle fields are immutable through update.** `EQUID`, `ItemType`, `Name`, `ClassString`, `RaceString`, `NameSort`, `MetadataMissingFg` and `DateAdded` cannot change. Editable fields are `Quantity` (both types; item range `1..100,000`, spell range `1..4,294,967,295`), `SourceLocation` (and derived location fields), `GivenTo`, `DateOpen` and `DateBazaar` (PD5-32). **Amended 2026-11-22 by QTY-08:** item quantity is editable and audited through the existing `quantity` pair; `ck_gbi_item_one` no longer exists. Rows absorbed by the QTY-09 consolidation cannot be restored. Unknown JSON fields are rejected; immutable fields present in the update body are rejected with `422`. | D-06/D-07 make identity date-bound; editing history would invalidate audit and public filtering. Rejecting rather than ignoring immutable fields prevents false expectations. Corrections require remove plus a new add. | **APPROVED (owner, 2026-11-22)** |
| PD5-11 | **Optimistic concurrency uses `expectedModAt` from the admin detail/list response.** `PUT` requires it in the JSON body; `DELETE` and `restore` require it as a query parameter (or hidden HTML field). The write locks the row, compares `ModAt` at microsecond precision, and returns `409` with a generic conflict message on mismatch. A successful write returns the new `modAt`. | Avoids lost updates without adding an ETag/If-Match framework. `<input type=hidden>` works in no-JS forms. Microsecond equality matches `DATETIME(6)`. A generic conflict avoids leaking another user's values. | **APPROVED (owner, 2026-11-22)** |
| PD5-12 | **Removal is a reversible soft delete.** `DELETE` sets `DeletedFg=1`, updates modification attribution and writes `ITEM_REMOVED`. `POST /restore` sets `DeletedFg=0`, updates attribution and writes `ITEM_UPDATED` with note `restored`. Removing an already-removed row or restoring an active row returns `409`. A missing ID returns `404`. | Matches D-04/AM-03 and preserves an audit trail/history. Explicit state errors are safer than silently succeeding and producing meaningless audit rows. No hard-delete path is exposed. | **APPROVED (owner, 2026-11-22)** |
| PD5-13 | **All admin writes acquire the existing import commit advisory lock on a dedicated `sql.Conn`, begin a transaction, lock affected rows with `SELECT ... FOR UPDATE`, write inventory and exactly one audit row, then commit.** Release the lock and close the connection on every path. Deadlocks may retry once with the same captured `now`; lock timeout maps to `503`. | Reusing one global mutation lock serializes import commit with manual edits, preventing import/admin deadlocks and duplicate spell targets. Audit inside the transaction guarantees no mutation without history. Dedicated connections are required because `GET_LOCK` is connection-scoped. | **APPROVED (owner, 2026-11-22)** |
| PD5-14 | **Audit event codes are `ITEM_CREATED`, `ITEM_UPDATED` and `ITEM_REMOVED`, with `TableName='GuildBankItems'` and `RecordID` as the decimal primary key.** Spell merge and restore use `ITEM_UPDATED`; removal uses only `ITEM_REMOVED`. Audit `Notes` contains only fixed machine-authored text (`spell quantity merged`, `restored`) or NULL, never user input. | Names match the ImplementationPlan vocabulary and are queryable by the viewer. A small closed set keeps filtering stable. User input in `Notes` would duplicate fields and risk unsafe rendering. | **APPROVED (owner, 2026-11-22)** |
| PD5-15 | **Audit old/new JSON is a redacted, explicit allowlist written by the service.** Create: `equid`, `itemType`, `name`, `quantity`, `sourceLocation`, `givenTo`, `metadataMissing`, `createdIds` (first 100 plus count if larger). Update/restore: only changed fields plus the new `quantity` when applicable. Remove: `{deletedFg:true}` old/new. No passwords, tokens, headers, cookies, SQL, stack traces or full request bodies. | Meets AC5-10/AC5-11, keeps rows small for a 100,000-row add, and avoids leaking bearer/session material. Bounding `createdIds` prevents audit JSON from duplicating the entire response. | **APPROVED (owner, 2026-11-22)** |
| PD5-16 | **The audit viewer is admin-only, newest-first, offset-paginated and filterable by `eventType`, `user`, `table`, `recordId`, `correlationId`, `from` and `to`.** `from`/`to` are inclusive UTC dates (`YYYY-MM-DD`); a `to` day becomes `< next day`. Page size defaults to 50, maximum 200; page maximum 10,000. The one-record history panel uses the same read path with `table=GuildBankItems&recordId={id}&pageSize=50`. | Provides E-09-adjacent operational evidence without inventing a generic audit platform. Offset pagination and the same page/pageSize bounds match the public list. Exact-value filters avoid exposing wildcard behavior; if free-text search is added, `LIKE` escaping is mandatory. | **APPROVED (owner, 2026-11-22)** |
| PD5-17 | **Admin list defaults are `status=active`, `sort=modified_newest`, `page=1`, `pageSize=50` (max 200).** `status=all` and `status=removed` are allowed. Only `sort` values in the allowlist are accepted. Unknown enum values are `422`. Empty result sets return `200` with an empty array and valid meta. | Matches the public pagination contract while fitting an ops screen. A deleted-first default would hide current inventory; `all` is explicit. Server-side allowlists prevent SQL fragment injection. | **APPROVED (owner, 2026-11-22)** |
| PD5-18 | **JSON request decoding uses a buffered body capped by `MaxBytesReader` (64 KiB), a token pre-pass that rejects duplicate object keys, `json.Decoder` with `DisallowUnknownFields`, and a single-value check; `Content-Type` must be `application/json`.** The pre-pass runs before struct decoding so a repeated key cannot silently overwrite an earlier value or defeat presence checks. Missing body, trailing data, duplicate fields, wrong types and unknown fields produce `400`; oversized bodies produce `413`. HTML form parsing uses Gin form binding only in adapters, not JSON handlers. | No existing handler establishes the full contract: the selection PUT sets `DisallowUnknownFields` but has no duplicate-key or trailing-value check, and settings decodes with `ShouldBindJSON`. Phase 5 chooses the strict option for admin writes. Rejecting unknown/duplicate fields prevents clients from assuming immutable fields are editable and blocks accidental mass assignment. | **APPROVED (owner, 2026-11-22)** |
| PD5-19 | **Input validation is rune-length based and runs before SQL.** Trim leading/trailing whitespace for `name`, `sourceLocation` and `givenTo`. Name is 0–128 runes when metadata exists and 1–128 when a fallback is required; source location 0–128; recipient 0–128. Reject any rune for which `unicode.IsControl(r)` is true (C0 including NUL, `U+007F`, and `U+0080–U+009F`). Store empty optional strings as SQL NULL. `SourceLocation` is stored after trimming but before normalization; derived group/sort are always server-generated. | Database lengths are characters, but Go byte-length checks would reject valid multibyte names or accept control characters. NULL preserves "not provided" and matches Phase 3's `nullableString` behavior. No user value is ever used as a grouping key. | **APPROVED (owner, 2026-11-22)** |
| PD5-20 | **Errors use the existing API envelope and mappings.** `ValidationError` → `422` with `fields`; malformed JSON/CSRF → `400`/`403`; invalid state or stale token → `409`; missing → `404`; unauthenticated admin route → `401`; non-admin → `403`; unavailable/lock timeout → `503`; unexpected → `500` with correlation ID only. | Reuses `internal/api/errors.go` and avoids a second error vocabulary. UI adapters render field errors inline; API clients get machine-readable fields. No internal SQL or driver text is returned. | **APPROVED (owner, 2026-11-22)** |
| PD5-21 | **The admin UI is full-page server rendering with plain forms, no JavaScript requirement, no HTMX and no CDN.** A single `app.css` may add table/field states. Names and all user values are rendered through `html/template`. Remove actions are a confirmation page, not `confirm()`; destructive buttons are not the default focus. | Consistent with Phase 3/4. The confirmation page works with CSP and without script, and avoids accidental removal. Contextual escaping protects stored names, locations and recipients. | **APPROVED (owner, 2026-11-22)** |
| PD5-22 | **`internal/repository/mariadb/migrations/0003_admin_inventory.sql` is the single source of truth and is the only migration file added.** The runner embeds that directory with `//go:embed migrations/*.sql`. A repository test asserts the exact embedded filename set, the version prefix, and each file's SHA-256. | The repository has no top-level `migrations/` directory, so a second copy would invent one, create a new drift risk and be invisible to the runner. The embedded copy is the only file the runner can execute, so a hash manifest plus filename assertion is the real gate. Changing a migration then requires an explicit test update and owner review. | **APPROVED (owner, 2026-11-22)** |
| PD5-23 | **Import commit writes the persisted `LocationGroup`/`LocationSort` already staged by `NormalizeLocation` (no re-normalization), and the row insert is extracted into a shared helper used by imports and admin creation.** `buildCommitPlan` copies the staged `ImportList.LocationGroup`/`LocationSort` into `domain.GuildBankItem`, and the existing 500-row chunking and `LastInsertId()+i` convention are retained. | Import staging already computes the canonical fields; recomputing at commit would add work and risk drift. Sharing the insert avoids two SQL column lists drifting while preserving the Phase 3 performance envelope. | **APPROVED (owner, 2026-11-22)** |
| PD5-24 | **Add only these Phase 5 indexes in `0003`:** `(DeletedFg, LocationGroup, LocationSort, NameSort, GuildBankItemID)`, `(DeletedFg, ModAt, GuildBankItemID)`, `(DeletedFg, ItemType, ModAt, GuildBankItemID)`, and `Audit (CorrelationID, EventDate)`. Name search remains `LIKE`, frequency-limited; extra covering indexes are added only if the §10.4 measurements fail and the owner approves a follow-up migration. | Covers the default modified sort, status/type filtering, location grouping and correlation lookup without multiplying import write cost. The exact plan must be confirmed with `EXPLAIN ANALYZE`, not assumed from column order alone. | **APPROVED (owner, 2026-11-22)** |
| PD5-25 | **Admin DTOs are explicit allowlists; public DTOs are unchanged.** Admin adds `id`, `sourceLocation`, `locationGroup`, `locationSort`, `givenTo`, `deleted`, `modAt`, `modBy`, `modByUserId`, `metadataMissing` and `nameSort`. Public continues to omit them. No `SELECT *` is used in either path. | Prevents accidental leakage through shared structs and makes AC5-14 testable. Explicit column lists also keep scans stable when columns are added. | **APPROVED (owner, 2026-11-22)** |
| PD5-26 | **Operational logging records route, status, duration, request/correlation ID, actor ID and mutation type/count, but not item name, recipient, source location, request body or audit JSON.** The audit table carries business detail; logs carry correlation. DB errors are logged once with the correlation ID and returned generically. | Satisfies E-07/ops requirements while minimizing PII in logs. Reuses the existing request-ID middleware rather than adding a second identifier. | **APPROVED (owner, 2026-11-22)** |
| PD5-27 | **Manual creation derives lifecycle dates as in import, and administrators may override `DateOpen`/`DateBazaar` per row (amended by the owner decision of 2026-11-22).** `DateAdded` is the UTC date on which the row is committed into the guild bank: it is derived, never client-supplied and never editable. `DateOpen` defaults to `DateAdded + GuildOpenDays` and `DateBazaar` defaults to `DateAdded + BazaarDays`, with both offsets read once from `Settings`. On create, an optional `dateOpen`/`dateBazaar` value replaces the corresponding default. The service rejects `DateOpen < DateAdded`, `DateBazaar < DateAdded` (the table CHECKs `ck_gbi_open`/`ck_gbi_bazaar`) and `DateOpen > DateBazaar` (E-10) with `422` before any SQL; settings failure or an invalid offset aborts a create that needs a default. A later `Settings` change never rewrites an existing row. | Keeps manually added and imported rows semantically identical and avoids a second date rule. The owner approved a per-item override so one stack can be opened or sent to the bazaar earlier or later without changing global settings, while deriving `DateAdded` from the commit keeps "date entered this guild bank" true. Validating both CHECKs in the service prevents a database constraint violation from surfacing as `500`. Frozen stored dates mean a policy change cannot silently re-date existing stock (this closes the DC-06 concern for new writes). | **APPROVED (owner, 2026-11-22)** |
| PD5-28 | **There is no request idempotency key in Phase 5.** Stale-write protection prevents lost updates, but a retried create can duplicate a stack. The UI disables submit only as a convenience; the API contract documents at-least-once client retry risk. | A durable idempotency key would require a new table/cleanup policy not in scope. Silently pretending creates are idempotent would be false. Ops guidance is to reconcile a suspected duplicate through search and soft delete. | **APPROVED (owner, 2026-11-22)** |
| PD5-29 | **Audit is append-only from the application.** Phase 5 adds no audit update/delete service, route, UI action or retention purge. MariaDB grants that deny `UPDATE`/`DELETE` on `Audit` remain an operational recommendation (E-11), not a migration. | Preserves evidence and avoids a hidden cleanup policy. SQLite triggers with compound statements are unsafe with the current simple migration splitter, so enforcement is not forced into `0003`. Retention/archival is a separate owner decision. | **APPROVED (owner, 2026-11-22)** |
| PD5-30 | **The unresolved dispositions `PD-05`, `PD-18`, `E-02`, and the open refresh/reconciliation residual of `AM-02` remain outside Phase 5 scope.** Phase 5 does not alter bootstrap, offline recovery/data repair, or metadata refresh/reconciliation. Manual creation validates metadata at write time, but previously imported `MetadataMissingFg` rows are not repaired. | The repository contains no later authoritative disposition for these items; the `AM-02` representation rule itself is approved and implemented, but no refresh design exists. Choosing behavior here would silently expand the phase and could conflict with a future owner decision. They are repeated in §16 with the impact of leaving them open. | **APPROVED (owner, 2026-11-22) — residuals accepted as out of scope** |
| PD5-31 | **Inbound `X-Request-ID` is bounded to the audit column width.** `requestIDPattern` in `internal/api/middleware.go` narrows from `^[A-Za-z0-9._-]{8,64}$` to `{8,36}`; an over-long or malformed header is replaced by a generated 32-hex ID, exactly as an invalid one is today. | `Audit.CorrelationID` is `CHAR(36)` and `insertAudit` passes the request ID through unmodified, so a trusted proxy sending 37–64 characters would fail the audit insert in strict mode and roll back an upload commit or admin write that is otherwise valid. Bounding at the middleware is one line, covers every writer, and keeps the existing fallback behavior. | **APPROVED (owner, 2026-11-22)** |
| PD5-32 | **`DateOpen` and `DateBazaar` are editable per row; `DateAdded` never is (owner decision of 2026-11-22).** Update accepts `dateOpen` and `dateBazaar` as required keys with `YYYY-MM-DD` values; an explicit `null` resets that date to the default computed from the stored `DateAdded` and the offset currently in `Settings`, because the offsets that produced a row are not stored. Values must satisfy `DateAdded <= DateOpen <= DateBazaar` (`ck_gbi_open`, `ck_gbi_bazaar`, E-10), otherwise `422` naming the field. A changed date is written in the same `ITEM_UPDATED` transaction and audited as `dateOpen`/`dateBazaar`. Adding quantity to an existing spell row still never changes its dates (Design.md); a create that supplies an override while a merge target exists is rejected with `422 dateOpen`/`dateBazaar` so the override is never silently dropped, and the admin edits the spell row instead. | The approved rule lets an administrator correct a single row's lifecycle timing without touching global settings, while keeping `DateAdded` as the immutable commit record. Storing an override as a plain value needs no schema change because the columns already exist. Rejecting rather than dropping an override on a spell merge avoids a control that appears to work but does not. Date fields are `DATE`, so a strict `YYYY-MM-DD` parse plus a `1000-01-01..9999-12-31` range check keeps every accepted value inside the column type without relying on server strict mode. | **APPROVED (owner, 2026-11-22); the three application sub-rules in §16.6 confirmed with it** |
| PD5-33 | **Inventory content uses a responsive max-width instead of the fixed `80rem` card.** `main.card.inventory-card` and the new admin inventory/audit cards use `width: min(120rem, calc(100% - 2rem))`; page padding continues to scale through the existing `clamp()`. Auth and setup cards keep their narrow widths. The table remains inside the scrollable `table-scroll` region, so a 320 px viewport still scrolls horizontally instead of breaking the page. | A fixed 80 rem (1280 px) cap wastes a 1440p or 4K desktop and forces avoidable wrapping; a responsive max-width lets the grid use the space while keeping a readable measure. Narrow cards stay narrow because a login form benefits from a short line length, not because a table does. | **APPROVED (owner, 2026-11-22, §9.5 UI-01)** |
| PD5-34 | **`GuildBankItemID` and `EQUID` are removed from the main public inventory grid.** The row identity sub-line that printed `ID {id} · EQUID {equid}` is deleted from `inventory_list.html`. Both values remain on the public detail page, the admin list, the admin detail page and the admin edit form, and in every JSON contract. The `inventoryItemView` type keeps both fields, so no handler or API change is needed. | The two identifiers are operator and support values, not browsing information; printing them doubled every row's height and caused the name cell to wrap. Keeping them on detail and admin views preserves every diagnostic path, and AC5-21 pins the separation so a future template edit cannot reintroduce them. | **APPROVED (owner, 2026-11-22, §9.5 UI-02)** |
| PD5-35 | **Filter controls use one compact multi-column grid on desktop for both public and admin lists.** Public: `search` (wider) followed by class, race, item type, sort and an actions cell in a single grid. Admin: `q`, `id`, `equid`, `locationGroup`, `givenTo`, `metadata`, `itemType`, `status`, `sort`, `pageSize` and actions in one `auto-fit` grid (`minmax(11rem, 1fr)`). At ≤1100 px the public grid drops to two columns, and at ≤640 px both grids become one column with the actions stacked. Future filters join the same grid without a new full row. | The task bar is horizontal, so a full row per filter pushes the table off-screen on a laptop. A single grid keeps every control visible, keeps labels adjacent to inputs, and preserves the markup-only, no-JavaScript contract. Mobile keeps the single-column fallback that already exists for the import filters. | **APPROVED (owner, 2026-11-22, §9.5 UI-03)** |
| PD5-36 | **The `EQResource` text column becomes an icon-only external-link control.** The cell renders an inline `<svg>` external-link glyph (no CDN, no icon font) inside an `<a>` with `title="View on EQResource"`, `aria-label="View {name} on EQResource (opens in a new tab)"`, `target="_blank"` and `rel="noopener noreferrer"`; the column header becomes `Link`. A row without a URL renders a muted `—` plus `sr-only` text `No EQResource link`, not the word "Unavailable". Focus styling uses the existing accent outline. | Repeating the word "EQResource" on every row is the widest, least informative cell; the external-link icon is the industry-standard affordance and the title/label keep it usable with a screen reader and keyboard. `noopener noreferrer` preserves the Phase 4 protections, and the dash avoids a fake disabled control. | **APPROVED (owner, 2026-11-22, §9.5 UI-04)** |
| PD5-37 | **The public availability value is dynamic: `Mains`, then `Persona`, then `Bazaar`.** The badge shows `Mains` while `today < DateOpen`, `Persona` from `DateOpen` up to the day before `DateBazaar`, and `Bazaar` from `DateBazaar` on, using the existing inclusive-boundary `service.Availability` function and one captured UTC date. The internal `domain.AvailabilityState` values (`upcoming`, `open`, `bazaar`) and the public JSON field are unchanged; only the rendered label changes, in `inventoryItemViewOf`. The detail page keeps the badge and shows `Date added`, `Open to personas` and `Open to bazaar` as specific dates, ISO in the `datetime` attribute and `Jan 2, 2006` as text. Admin and public JSON contracts are untouched. | The old labels ("Available now", "Opens <date>", "Bazaar eligible") described the mechanism rather than the guild rule and repeated a date already shown on the detail page. Naming the audience states exactly who may receive the item now. Keeping the enum stable avoids breaking `PublicInventoryAPI.md` consumers whose only contract is the JSON value. | **APPROVED (owner, 2026-11-22, §9.5 UI-05/UI-07); the state boundary this label maps is now inclusive per PD5-40** |
| PD5-38 | **Table density favours one line per cell where space exists.** At viewport widths ≥900 px the public inventory table applies `white-space: nowrap` to the name, type, class, race, quantity, availability and link cells; the table keeps a minimum width and the `table-scroll` region absorbs overflow. Below 900 px the nowrap rule is dropped so cells wrap inside the scroll region instead of forcing an unusable horizontal strip. Badges are vertically aligned so a long class list does not push the availability cell out of line. | Wrapping inside a wide viewport made the list hard to scan and inflated row height. Nowrap plus horizontal scroll is the standard dense-table pattern, and relaxing it below 900 px preserves mobile readability. The minimum width is unchanged at 58 rem, so the scroll behavior is the same one Phase 4 already tested. | **APPROVED (owner, 2026-11-22, §9.5 UI-06)** |
| PD5-39 | **The approved lifecycle date rule is reaffirmed unchanged.** `DateAdded` is the UTC date the row is committed into the guild bank and is never editable; `DateOpen` defaults to `DateAdded + GuildOpenDays` and `DateBazaar` to `DateAdded + BazaarDays` from `Settings`; a `Settings` change never rewrites an existing row; administrators may override `DateOpen` and `DateBazaar` per row (PD5-32). The only new consequence is presentation: PD5-37 derives the badge from those stored dates, so no date value, default, override or non-retroactivity rule changes. | The owner restated the rule with the UI amendments to make sure the UI work did not loosen it. Recording the reaffirmation as its own decision keeps traceability intact and confirms that §16.6's three sub-rules still govern the implementation. Nothing in PD5-27, PD5-32, §6.3, §6.4 or the §13 date tests changes. | **APPROVED (owner, 2026-11-22, §9.5 UI-07)** |
| PD5-40 | **Availability thresholds are inclusive, so `Bazaar` starts on the `DateBazaar` day.** `service.Availability` returns `upcoming` while `today < DateOpen`, `open` while `DateOpen <= today < DateBazaar`, and `bazaar` when `today >= DateBazaar` (implemented as `!today.Before(dateBazaar)` after the open-date check). The three `domain.AvailabilityState` values, every JSON field, every route, filter and stored date are unchanged; the state changes one day earlier than under PD4-11's original comparison, and the rendered `Mains`/`Persona`/`Bazaar` labels follow the state. An inverted stored window is still never `open`: it is `upcoming` until its open date, then `bazaar`. | Aligns `DateBazaar` with the already-inclusive `DateOpen` and with the meaning of "open to the bazaar": on that date the persona window has closed and bazaar eligibility has begun. Keeping `Persona` through that day would invite members to hand an item to a persona at the moment the guild treats it as bazaar stock. Equal `DateOpen`/`DateBazaar` values therefore have no `Persona` day, which is the direct consequence of the owner's formula rather than a special case. | **APPROVED (owner, 2026-11-22, fourth pass)** |
| PD5-41 | **AI-01: bulk removal is page-local only.** The admin inventory list adds one checkbox per active row carrying the opaque `id|modAt` token; a POST renders a preview page and a second POST applies the selection. At most 200 records (one maximum-size page) may be selected. `RemoveMany` locks the selected rows in ascending `GuildBankItemID` order inside the existing admin write transaction, re-validates every token, and writes one `ITEM_REMOVED` audit row per record sharing the request correlation ID. One stale, missing or already-removed record rolls back the whole selection. No filter-wide or inventory-wide action, no new event type, no cross-page selection state. | Keeps the single-record contract and the existing audit vocabulary while making repetitive removals practical. Page-local scope bounds the transaction and prevents a "remove everything matching a filter" mistake. Preview mirroring the single remove page keeps the no-JavaScript contract. All-or-nothing plus per-row version tokens prevents partial destructive writes. | **APPROVED (owner, 2026-11-22, post-Phase-5 review)** |
| PD5-42 | **AI-02: edit and remove may commit together.** `UpdateItemInput.Remove` / JSON `remove` / the edit form's "Save and remove" button request a field update and a soft delete in one transaction. `ITEM_UPDATED` is written only when editable fields changed; `ITEM_REMOVED` is always written; both share `EventDate` and the correlation ID. The response reports `fieldsChanged` and the item as deleted. Restoring later does not undo the edits and the edit form states that. | Saves a round trip without weakening the audit trail or inventing a combined event. Two entries keep per-record history filters meaningful. Restore semantics stay the documented `DeletedFg=0` operation. | **APPROVED (owner, 2026-11-22, post-Phase-5 review)** |
| PD5-43 | **IMP-02: `Unspecified` is presented as "Other (not a bank location)".** The stored `LocationGroup` value, the API/FK values and the selector value remain `Unspecified`; only rendered labels change, in the import review and the admin inventory list/detail/filter. The group is never selected by default. | Removing or renaming the persistent value would break filters, history and the D-05 contract. A display-only label is reversible and keeps the database self-describing. | **APPROVED (owner, 2026-11-22, post-Phase-5 review)** |
| PD5-44 | **IMP-01: nested-slot rows are ignored and counted.** A well-formed row whose reserved bank location ends in two or more slot suffixes is the contents of a container, not a bank location. It is skipped at parse time, counted in `ParseResult.IgnoredRows` and the nullable `ImportBatch.IgnoredRowCount` (`0004_import_ignored_rows.sql`), included in `IMPORT_CREATED`, and never staged as `Unspecified`. Empty nested rows (`ID=0`) remain ordinary discards. | Closes the populated-bag hole deterministically while leaving the canonical sample's counts unchanged. Persisting the count makes the decision auditable without the original file. | **APPROVED (owner, 2026-11-22, post-Phase-5 review)** |
| PD5-45 | **IMP-04/IMP-05: default selection is container-aware, Hoard-exempt and non-retroactive.** Staging selects Hoard rows, selects slot contents in every other bank group, and excludes container rows in General, Bank and SharedBank; `Unspecified` is never selected. Existing batches keep their persisted selection. `GET /api/v1/imports/{id}/default-selection` plus the review page's "Reset to defaults" button is the explicit opt-in for an old batch. | The bags occupying slots are infrastructure, not guild property to import; Hoard has no slots and represents stock as container rows, so it must stay selected. Applying the rule at staging only avoids silently changing historical batches under review. | **APPROVED (owner, 2026-11-22, post-Phase-5 review)** |
| PD5-46 | **IMP-06: an unchecked group is hidden and deselected, and `Unselected` ignores the scope.** The review page keeps a memory-only group scope; unchecking a group clears its rows from the persisted selection and hides them from `/rows` through `excludeGroups`. The `Unselected` filter sends `ignoreGroups=1` so rows in a hidden group remain reviewable. Scope resets to every group on reload. | Makes the location checkbox mean one thing instead of two inconsistent ones. The escape hatch preserves reviewability of hidden rows, and a transient scope avoids adding a second persisted selection concept. | **APPROVED (owner, 2026-11-22, post-Phase-5 review)** |

---

## 4. Schema, location normalization and backfill

### 4.1 D-05 normalization contract

The canonical D-05 grouping rule is pure and already lives in `internal/service/import_normalize.go` (`NormalizeLocation`, `normalizeDigits`, the regexes). To keep import behavior frozen while giving manual writes a clearer warning, that file gains one unexported value computation, `normalizeLocationValue(raw)`, and two thin wrappers. Nothing is moved to a new file, so `import_normalize.go` stays the single home for location rules.

- `NormalizeLocation(raw)` remains the import-facing function with its current signature and behavior. It keeps the exact existing warning codes: `location_blank` for a blank value, `location_pattern_invalid` for a reserved prefix that fails its pattern or an overlong group, and no warning for other unrecognized sections.
- `NormalizeAdminLocation(raw)` is used by manual create/update. It returns the same group/sort but has admin-specific warnings: blank → no warning; any non-blank raw whose final group is `Unspecified` (including a malformed reserved prefix or an overlong generated group) → one `location_unrecognized` warning.

Both wrappers call `normalizeLocationValue`, so a raw value cannot receive different group/sort values on the import and manual paths. The startup backfill cannot call the unexported computation (it lives in `internal/repository/mariadb`), so it calls the exported `NormalizeLocation` and discards `Warnings`. Import staging already computes these fields into `domain.ImportRow`; commit persists the staged values rather than recomputing them.

`NormalizedLocation.Warnings` keeps its existing type `[]domain.ImportWarning`; no second normalized-location type is introduced. The admin service converts the single `location_unrecognized` entry to a `service.ItemWarning` with the same code/detail when composing `AdminWriteResult`. Import warnings remain `domain.ImportWarning` and are unchanged.

The parser trims surrounding Unicode whitespace, then applies these anchored, case-insensitive rules (optional whitespace around the numeric portions is tolerated). Sort values are shown with `\u0001` standing for the ASCII `0x01` separator emitted by `NaturalSortKey`; they are persisted machine keys, not display text.

| Raw shape | Example | Group | Sort | Admin warning |
|---|---|---|---|---|
| `General<N>-Slot<M>` | `General5-Slot1` | `General 5` | `general\u00010000000005` | none |
| `Bank<N>-Slot<M>` | `Bank4-Slot6` | `Bank 4` | `bank\u00010000000004` | none |
| `SharedBank<N>-Slot<M>` | `SharedBank2-Slot6` | `SharedBank 2` | `sharedbank\u00010000000002` | none |
| `Hoard` or `Hoard<N>` | `Hoard17` | `Hoard` | `hoard` | none |
| blank / NULL | `NULL` | `Unspecified` | `unspecified` | none |
| anything else | `Guild Hall` | `Unspecified` | `unspecified` | `location_unrecognized` |

Rules:

1. Numeric components are parsed as non-negative decimal integers; zero is retained and leading zeroes do not leak into the group (`General05` → `General 5`).
2. The canonical prefixes and the `Hoard` spelling are exactly those in D-05/AM-04. `SharedBank` has no space between `Shared` and `Bank`; the instance number does have one space before it.
3. `LocationGroup` is capped at 64 runes and `LocationSort` at 80 runes. Generated values are far shorter; the caps are defensive schema limits.
4. `SourceLocation` is retained exactly as trimmed input, including `General5-Slot1`. Derived values never replace it. Normalization word-splits its input, so interior whitespace affects only the derived group and sort, never the raw value.
5. An empty or `NULL` `SourceLocation` derives group `Unspecified` and its natural sort key, matching the existing import group for blank locations. The manual admin path adds no warning; the import path keeps its existing `location_blank` warning.
6. Unknown values are not guessed. They all group as `Unspecified`, preserving a filterable "raw location not mapped" bucket, and the manual admin path receives a `location_unrecognized` warning. Import's existing staging warnings (`location_blank`, `location_pattern_invalid`, or none) remain authoritative for imported rows and are not rewritten by Phase 5.
7. Unicode normalization is not applied to raw values. Group parsing uses the existing ASCII-prefix patterns because D-05 defines ASCII source formats; a raw value that does not match those patterns is `Unspecified`.
8. Test vectors include every D-05 example, `Hoard`, `Hoard145`, lowercase prefixes, surrounding whitespace, zeroes, an overlong numeric component, invalid UTF-8 replacement, and unknown strings. Tests assert both wrappers produce identical group/sort and that each preserves its documented warning contract.

### 4.2 Migration `0003_admin_inventory.sql`

PD5-22 makes `internal/repository/mariadb/migrations/0003_admin_inventory.sql` the only copy. It is embedded by `migrate.go`; there is no top-level `migrations/` directory in the repository and Phase 5 does not create one.

```sql
-- 0003_admin_inventory.sql
-- Phase 5: persisted D-05 location groups and administrative query indexes.

ALTER TABLE GuildBankItems
    ADD COLUMN LocationGroup VARCHAR(64) NULL AFTER SourceLocation,
    ADD COLUMN LocationSort  VARCHAR(80) NULL AFTER LocationGroup,
    ADD KEY ix_gbi_admin_location
        (DeletedFg, LocationGroup, LocationSort, NameSort, GuildBankItemID),
    ADD KEY ix_gbi_admin_modified
        (DeletedFg, ModAt, GuildBankItemID),
    ADD KEY ix_gbi_admin_type_modified
        (DeletedFg, ItemType, ModAt, GuildBankItemID);

ALTER TABLE Audit
    ADD KEY ix_audit_correlation (CorrelationID, EventDate);
```

Notes:

- `LocationGroup` and `LocationSort` stay nullable. That is what makes the backfill restartable and allows rollback without reconstructing raw values. `LocationSort` uses the table's existing `utf8mb4_unicode_ci` collation, matching `NameSort`; no new collation is introduced.
- No `NOT NULL` conversion, generated column, trigger, foreign key or new table is introduced.
- Column order is explicit so an upgraded schema and a fresh install produce the same schema.
- `ix_gbi_admin_location` supports status + group filtering and grouped name sorting. `ix_gbi_admin_modified` supports the default status + modified sort. `ix_gbi_admin_type_modified` supports status + type + modified sort. Exact `EXPLAIN` plans and possible filesorts are measured in §10.4.
- `ix_audit_correlation` on `(CorrelationID, EventDate)` supports the correlation filter; existing date/table/record indexes continue to serve the other filters.
- DDL locking mode is verified against the deployed MariaDB version in the maintenance window. A plain `ALTER` may rebuild the table on older versions. No `ALGORITHM=` clause is hard-coded because the Compose and production versions are not yet pinned in this document.
- The file is added to the embedded migration manifest test. A missing, renamed or edited migration fails `go test ./internal/repository/mariadb` until the expected hash is deliberately updated.

### 4.3 Startup backfill

`mariadb.Store.BackfillInventoryLocations(ctx, batchSize)` is called from `internal/platform/app/app.go` inside the existing `if guildStore != nil` block, after `Migrate` succeeds and after the existing `RecoverInterrupted` call (which remains log-only), before the inventory service/router is started. When no guild store is configured or `mariadb.Open` fails, `guildStore` is nil, the backfill is skipped and startup continues in the existing degraded mode.

Algorithm:

1. Acquire `GET_LOCK(CONCAT('guildbank:location-backfill:', LEFT(SHA2(DATABASE(), 256), 32)), 300)` on one dedicated connection. If not acquired, fail startup with a clear error; a second replica must not write concurrently. The 32-hex-character database hash matches the existing commit and migration lock derivation and keeps the name within MariaDB's 64-character advisory-lock limit. The migration runner's own lock is already released at this point, so this is a separate lifetime lock.
2. Query the next 500 rows with the primary key cursor:
   ```sql
   SELECT GuildBankItemID, SourceLocation
   FROM GuildBankItems
   WHERE GuildBankItemID > ?
     AND (LocationGroup IS NULL OR LocationSort IS NULL)
   ORDER BY GuildBankItemID
   LIMIT 500;
   ```
   Scan `SourceLocation` into `sql.NullString`; pass `""` to the normalizer when it is NULL.
3. For each row call the exported `service.NormalizeLocation` (the empty string yields the `Unspecified` group, matching import). The packages are already wired this way: `internal/repository/mariadb/imports.go` and `imports_commit.go` import `internal/service`, so no new dependency edge is introduced and no layering rule is crossed. Bound the key with `service.TruncateRunes(sort, 80)` before writing, exactly as upload staging does. Update only the derived columns:
   ```sql
   UPDATE GuildBankItems
   SET LocationGroup = ?, LocationSort = ?
   WHERE GuildBankItemID = ?
     AND (LocationGroup IS NULL OR LocationSort IS NULL);
   ```
   Updates run in one transaction per batch. `SourceLocation`, `ModAt`, `ModBy` and `ModByUserID` are not touched; backfill is a schema repair, not a user edit.
4. Advance the cursor to the highest ID in the batch. Commit, then repeat. Progress logs every 10,000 examined rows and at completion, with counts and elapsed time, but no raw locations.
5. Unlock and close the connection in `defer`. On interruption, committed batches remain; the next startup resumes at the next null row. An uncommitted batch rolls back.
6. If a row changes concurrently (only possible if another old-version writer ignores the new columns), the `WHERE` condition leaves a null behind; the cursor has already passed that row, so the next startup run repairs it. Phase 5 writers always populate both fields.

Performance: the keyset cursor visits each table row at most once per complete run. A restart may re-walk already-filled rows until it reaches the first null; that cost is bounded and preferable to a server-side cursor holding a long transaction. The per-row update strategy is benchmarked at 250,000 and 1,000,000 rows; if it misses the gate, the same batch may be implemented with a temporary table join, but no semantics change.

Concurrency and audit: backfill emits no audit row because it changes only derived query columns and no business value visible to the public. It never changes `SourceLocation`. This exception is explicit; manual edits remain audited.

**Follow-up repair (2026-11-22, DEFECT-01).** A second startup routine,
`mariadb.Store.BackfillSpellMetadata(ctx, batchSize)`, runs immediately after the
location backfill in the same `if guildStore != nil` block. It repairs spell rows
written before the commit-plan fix: it joins `items` by `EQUID` for rows with
`ItemType='Spell'`, `MetadataMissingFg=0` and a blank `ClassString`/`RaceString`,
decodes with `service.NewClassMaskDecoder`/`NewRaceMaskDecoder`, and fills only
the blank columns. It uses its own advisory lock
(`guildbank:spell-metadata-backfill:`), the same keyset-batch and one-transaction-
per-batch pattern, never overwrites a populated string, never blanks a zero
mask, and touches no other column. A failure fails startup with
`app: backfill spell metadata: …`. It is idempotent: after the first run every
row whose masks decode to a non-empty value is repaired and no further writes
occur; a legitimate empty decoded column can still be re-read on a later start
but is never rewritten. It logs `spell metadata backfill complete` with the row
count only when it repaired something. The Compose run repaired seven rows. See
`ImportAdminImprovements.md` §8.

### 4.4 Import commit integration

`importCommitTx.insertInventory` is generalized to a package-level `insertInventoryRows(ctx context.Context, tx execer, rows []domain.GuildBankItem) ([]int64, error)` used by both import and admin transaction types, with one shared column list:

- Add `LocationGroup` and `LocationSort` to the `domain.GuildBankItem` import projection. `buildCommitPlan` copies them from each selected `domain.ImportRow` (already normalized at upload) for both expanded item rows and new spell rows.
- Add `LocationGroup` and `LocationSort` to the `INSERT` after `SourceLocation`.
- Supply `nullableString(row.LocationGroup)` and `nullableString(row.LocationSort)` from the staged row, not by re-running normalization in the repository.
- Keep 500-row chunks and the existing `LastInsertId()+i` ID assignment.
- Keep the import transaction's advisory lock, audit behavior and rollback semantics unchanged.

The admin create path calls the same helper. This prevents the column lists from drifting and ensures imports and manual adds are indistinguishable to public queries. A regression test commits an import containing `General5-Slot1` and asserts the resulting row has group `General 5`, sort `general\x010000000005`, raw `General5-Slot1`, and that source location is unchanged.

---

## 5. API and HTML contracts

### 5.1 Routes

All new routes are registered in the existing admin group (`RequireAuth` → CSRF middleware → `RequireAdmin`) except the unchanged public reads. That group is declared as `admin := r.Group("/")` (see `NewRouter` in `internal/api/router.go`), so every route is registered with its full absolute path and there is no group-level `/admin` prefix; the public inventory reads stay in the `public` group that carries only CSRF. Gin registers methods independently, so the new admin `POST /api/v1/items` coexists with the public `GET /api/v1/items`; the public route is not moved or weakened. JSON writes send `X-CSRF-Token` with the value of the signed `guildbank_csrf` cookie; HTML forms submit the same value in `csrf_token`. The existing CSRF middleware validates it before any handler decodes the body. A non-browser JSON client obtains the cookie and token from the existing `GET /api/v1/auth/csrf` response (`data.csrfToken`) and sends both on later requests.

| Method | Path | Response | Purpose |
|---|---|---|---|
| `GET` | `/api/v1/admin/items` | JSON | Admin collection, including removed rows |
| `GET` | `/api/v1/admin/items/{id}` | JSON | Admin detail, including a removed row |
| `POST` | `/api/v1/items` | JSON `201` | Create one item/spell |
| `PUT` | `/api/v1/items/{id}` | JSON `200` | Edit source/recipient, quantity for spells only |
| `DELETE` | `/api/v1/items/{id}` | JSON `200` | Soft delete |
| `POST` | `/api/v1/items/{id}/restore` | JSON `200` | Restore |
| `GET` | `/api/v1/audit` | JSON | Audit search |
| `GET` | `/api/v1/audit/{id}` | JSON | One audit entry with old/new JSON |
| `GET` | `/admin/inventory` | HTML | Admin inventory list and filters |
| `GET` | `/admin/inventory/new` | HTML | Add form |
| `GET` | `/admin/inventory/{id}` | HTML | Admin detail plus record history |
| `GET` | `/admin/inventory/{id}/edit` | HTML | Edit form |
| `GET` | `/admin/inventory/{id}/remove` | HTML | Remove confirmation page |
| `POST` | `/admin/inventory` | `303` | HTML create adapter |
| `POST` | `/admin/inventory/{id}` | `303` | HTML edit adapter |
| `POST` | `/admin/inventory/{id}/remove` | `303` | HTML remove adapter |
| `POST` | `/admin/inventory/{id}/restore` | `303` | HTML restore adapter |
| `GET` | `/admin/audit` | HTML | Audit viewer |

Gin v1.12 supports a static segment alongside a parameter sibling on the same path segment, so `/admin/inventory/new` and `/admin/inventory/:id` coexist and a literal `new` reaches the create form instead of being parsed as an ID; route tests pin both orderings so a future downgrade or tree change cannot silently turn `new` into `404`. Paths in this table use `{id}` for readability; Gin registration uses `:id` as in the existing router. Every malformed ID (non-integer, `<= 0`) returns `404` for API and HTML; it is not treated as a name search. The HTML adapters never contain business logic: they bind the form, call the same service method as the API, and map success or a field error back to a page.

### 5.2 Collection query parameters

| Parameter | Allowed / default | Validation |
|---|---|---|
| `q` | empty | Trim; 128-rune cap; case-insensitive substring over `Name` only. Literal `%`, `_`, `!`. |
| `id` | empty | Positive signed 64-bit integer; malformed or out of range → `422`; exact `GuildBankItemID` filter. |
| `equid` | empty | Decimal integer `1..4294967295`; malformed or out of range → `422`; exact `EQUID` filter. |
| `givenTo` | empty | Trim; 128-rune cap; exact case-insensitive match using the column collation; empty means no filter. |
| `metadata` | `any` default / `missing` / `present` | Exact allowlist; `missing` → `MetadataMissingFg=1`, `present` → `0`, `any` → no predicate; otherwise `422`. |
| `locationGroup` | empty / any `1..64` rune string | Exact match after trimming; `Unspecified` is a real value. Not normalized from arbitrary input; the filter UI supplies server-generated options. |
| `itemType` | empty / `Item` / `Spell` | Case-insensitive canonicalization; otherwise `422`. |
| `status` | `active` default / `removed` / `all` | Exact allowlist; otherwise `422`. |
| `sort` | `modified_newest` default / `modified_oldest` / `name` / `name_desc` / `location` / `quantity` / `date_added` / `date_added_desc` / `date_open` / `date_open_desc` / `date_bazaar` / `date_bazaar_desc` | Exact allowlist; otherwise `422`. No SQL is built from the value. |
| `page` | `1`, `1..10000` | Malformed/out of range → `422`. Beyond the result set → empty page. |
| `pageSize` | `50`, `1..200` | Malformed/out of range → `422`. |

Unknown query parameters are ignored, matching Phase 4. A repeated parameter uses its first value (`c.Query`), matching Phase 4; repetition is not a validation error, and tests pin that behavior so a later switch to `QueryArray` is a deliberate contract change. `status`, `sort` and paging are validated in the service so HTML and JSON cannot drift. The HTML list preserves every non-default filter in page/sort links and drops empty values.

Sort definitions are deterministic with an ID tiebreaker:

- `modified_newest`: `ModAt DESC, GuildBankItemID DESC`
- `modified_oldest`: `ModAt ASC, GuildBankItemID ASC`
- `name`: `NameSort ASC, GuildBankItemID ASC`
- `name_desc`: `NameSort DESC, GuildBankItemID DESC`
- `location`: `LocationSort ASC, NameSort ASC, GuildBankItemID ASC`
- `quantity`: `Quantity DESC, NameSort ASC, GuildBankItemID ASC`
- `date_added`: `DateAdded ASC, GuildBankItemID ASC`; `date_added_desc`: `DateAdded DESC, GuildBankItemID DESC`
- `date_open`: `DateOpen ASC, GuildBankItemID ASC`; `date_open_desc`: `DateOpen DESC, GuildBankItemID DESC`
- `date_bazaar`: `DateBazaar ASC, GuildBankItemID ASC`; `date_bazaar_desc`: `DateBazaar DESC, GuildBankItemID DESC`

### 5.3 Request bodies

Create:

```json
{
  "itemType": "Item",
  "equid": 28813,
  "quantity": 3,
  "sourceLocation": "General5-Slot1",
  "givenTo": "Guild Bank",
  "name": "Fallback name",
  "dateOpen": "2026-12-05",
  "dateBazaar": "2026-12-21"
}
```

- `itemType` is required on create, one of `Item` or `Spell`, and immutable on update; it selects row-per-unit expansion versus spell merge.
- `equid` is a required decimal integer in the `GuildBankItems.EQUID` range (`1..4294967295`), parsed from JSON as a number or a decimal string; malformed, fractional, negative, zero or out-of-range values are `422`.
- `quantity` is required; ordinary items `1..100000`, spells `1..4294967295`.
- `sourceLocation` is optional nullable string, max 128 runes.
- `givenTo` is optional nullable string, max 128 runes.
- `name` is optional and used only when metadata is missing; trimmed, max 128 runes.
- `dateOpen` and `dateBazaar` are optional on create and override the settings-derived defaults (PD5-27/PD5-32). Format is exactly `YYYY-MM-DD`, interpreted as a UTC calendar date to match the `DATE` columns; `null` or an absent key means "use the default". An override must be on or after the derived `DateAdded`, and the pair must satisfy `dateOpen <= dateBazaar`, otherwise `422` naming the field. A spell create that merges into an existing row rejects an override with `422` rather than dropping it.
- Unknown fields, trailing JSON and duplicate/malformed fields are `400`; present immutable fields are `422` naming the field. The strict decoder is defined by PD5-18.
- The create DTO decodes `equid` through a small decimal type that accepts a JSON number or string but rejects a sign, fraction, exponent, whitespace or non-digit; it converts to `uint64` only for the range check, so no float64 conversion is involved.

Update:

```json
{
  "expectedModAt": "2026-11-21T14:03:22.123456Z",
  "quantity": 1,
  "sourceLocation": "Bank2-Slot4",
  "givenTo": null,
  "dateOpen": "2026-12-06",
  "dateBazaar": null
}
```

All six fields are required. `expectedModAt` is RFC 3339 in UTC with an optional fraction of up to six digits, normalized to microseconds for comparison. `sourceLocation` and `givenTo` may be `null` to clear; `dateOpen` and `dateBazaar` may be `null` to reset to the settings-derived default (PD5-32). An absent key for any of them is `422`, not "unchanged", so a client must send the full editable state it read. Immutable fields (`equid`, `name`, `itemType`, `dateAdded`, class/race, `metadataMissing`, `locationGroup`, `locationSort`, `nameSort`) are rejected with `422` naming the field, not silently ignored.

`quantity` on update is editable for both types under QTY-08. An `Item` row accepts `1..100000`; a `Spell` row accepts `1..4294967295`. Zero is `422`. The service writes the `quantity` old/new audit pair for either type. `0005` drops `ck_gbi_item_one`, so the old “item must stay 1” rule no longer exists.

`dateOpen` and `dateBazaar` on update are validated against the stored `DateAdded`, which is immutable: `DateAdded <= DateOpen <= DateBazaar` must hold after the edit (`ck_gbi_open`, `ck_gbi_bazaar`, E-10). A violation is `422` naming `dateOpen` or `dateBazaar` and no write occurs. `null` resets the field to `stored DateAdded + current Settings offset`, so the reset value is itself audited; the offset that originally produced a row was never stored and is not reconstructed. A merge that only increases a spell's quantity never changes dates.

API remove and restore take no JSON body. Both require `expectedModAt` as a query parameter. HTML remove/restore forms submit it as a hidden field; the adapter passes that value to the same service input, so the API query requirement does not leak into HTML.

```
DELETE /api/v1/items/474?expectedModAt=2026-11-21T14%3A03%3A22.123456Z
POST   /api/v1/items/474/restore?expectedModAt=2026-11-21T14%3A03%3A22.123456Z
```

### 5.4 Response shapes and headers

Admin item resource:

```json
{
  "id": 474,
  "equid": 28813,
  "name": "Blade of Carnage",
  "itemType": "Item",
  "quantity": 1,
  "classString": "WAR,PAL,RNG,SHD,BRD",
  "raceString": "ALL",
  "metadataMissing": false,
  "sourceLocation": "General5-Slot1",
  "locationGroup": "General 5",
  "locationSort": "general\u00010000000005",
  "givenTo": "Guild Bank",
  "deleted": false,
  "nameSort": "blade of carnage",
  "dateAdded": "2026-11-21",
  "dateOpen": "2026-12-05",
  "dateBazaar": "2026-12-21",
  "availability": "upcoming",
  "modAt": "2026-11-21T14:03:22.123456Z",
  "modBy": "admin",
  "modByUserId": 1
}
```

Collection envelope: `{"data":{"items":[...],"meta":{"page":1,"pageSize":50,"total":123,"totalPages":3,"status":"active","sort":"modified_newest"}}}`. Detail: `{"data":{"item":{...}}}`. `sourceLocation` and `givenTo` are JSON `null` when the stored column is NULL (the domain uses an empty string, matching the import projection); `locationGroup` and `locationSort` are always strings after migration/backfill. `locationSort` is the persisted machine key (`NaturalSortKey`), not display text; the example shows the JSON escape for the ASCII `0x01` separator. `modByUserId` is `null` only if the historical row never recorded an actor.

Create returns `201`, a `Location` header pointing at `/api/v1/admin/items/{item.guildBankItemId}` (for an ordinary stack that is the first created row; for a spell merge it is the existing target), and:

```json
{"data":{"item":{...},"createdIds":[474,475,476],"createdCount":3,"warnings":[{"code":"metadata_missing","field":"name"}]}}
```

The create response object has required `item` and `createdCount`, optional `createdIds`, and `warnings` (always an array, possibly empty).

A spell merge returns the target row, `createdIds` containing only that ID when a new spell row was inserted (or an empty array when merged), and `createdCount`. Update/remove/restore return `{"data":{"item":{...}}}` with the fresh `modAt`; remove/restore also return the new `deleted` value, and update includes `changed` (`false` for the no-op case in §6.4). Responses use `Cache-Control: no-store`, `X-Content-Type-Options: nosniff` and the existing request ID header. Create responses always include `createdCount`; `createdIds` is included with at most the first 10,000 IDs and is omitted only when `createdCount` is zero, while the audit remains capped per PD5-15.

### 5.5 Audit read contract

`GET /api/v1/audit` accepts `eventType` (exact allowlist: `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED`, `IMPORT_CREATED`, `IMPORT_COMMITTED`; empty means no filter; anything else is `422`), `user` (exact `UserName`), `table` (exact `TableName`), `recordId` (exact string), `correlationId`, `from` and `to` (inclusive UTC dates), `page`, `pageSize`. It returns:

```json
{"data":{"entries":[...],"meta":{"page":1,"pageSize":50,"total":1,"totalPages":1}}}
```

Entry allowlist: `id`, `eventDate`, `userId`, `userName`, `eventType`, `tableName`, `recordId`, `correlationId`, `sourceIP`, `notes`, `oldValues`, `newValues`. `id` maps to `AuditID`; the existing `domain.AuditEntry` must gain an `AuditID int64` field because it currently has no primary-key field. `userId` is emitted as `null` when the stored `UserID` is NULL (the domain uses `0` as the sentinel). Nullable text columns are emitted as `""` (the domain's zero value), except `oldValues`/`newValues`, which are `{}` for SQL NULL. `GET /api/v1/audit/{id}` returns one entry; `404` if absent. Old/new JSON is returned as stored, already redacted by the writing service; the handler does not reparse or merge it. The HTML history panel calls the same repository method with `table=GuildBankItems&recordId={id}` and renders only the fields the template names.

`Audit.CorrelationID` is `CHAR(36)`, but the accepted inbound `X-Request-ID` is 8–64 characters (`requestIDPattern` in `internal/api/middleware.go`). Generated IDs are 32 hex characters and the `crypto/rand` fallback is 28, so both fit; a trusted-proxy header of 37–64 characters would exceed the column and fail the audit insert in strict SQL mode, rolling back an otherwise valid import or admin write. Phase 5 therefore narrows `requestIDPattern` to `{8,36}` (rejecting an over-long inbound ID and generating a fresh one) and adds a handler test that posts a 64-character `X-Request-ID` through an admin write and an import commit, asserting both succeed and store a 36-character-or-shorter correlation ID. `SourceIP` is `VARCHAR(45)` and comes from `c.ClientIP()`, which yields a host without a port, so it fits IPv4 and IPv6.

### 5.6 Error mapping

| Condition | API | HTML adapter |
|---|---|---|
| malformed JSON, unknown/trailing field, wrong content type | `400` `invalid_request` | Re-render form with `400` |
| CSRF missing/mismatched | `403` existing CSRF code | Existing CSRF error page |
| validation, immutable field, malformed date/query | `422` `validation_failed` + `fields` | Re-render form with field errors and preserved safe values |
| stale `ModAt` or invalid lifecycle state | `409` `conflict` | Re-render with conflict notice |
| absent item/audit | `404` `not_found` | `404` page |
| body over 64 KiB | `413` `request_too_large` | `413` page |
| DB lock timeout/unavailable | `503` `dependency_unavailable` | Unavailable page |
| unexpected | `500` `internal_error` + request ID | `500` page with request ID |

Error JSON uses the existing `internal/api/errors.go` envelope. Field keys are stable: `q`, `id`, `equid`, `quantity`, `sourceLocation`, `givenTo`, `name`, `metadata`, `locationGroup`, `itemType`, `expectedModAt`, `status`, `sort`, `page`, `pageSize`, `from`, `to`. Driver text, SQL and stack traces never appear in a response.

Every admin write handler (API and HTML adapter) wraps `c.Request.Body` in a handler-level `http.MaxBytesReader` with `AdminMaxBodySize` (64 KiB) before decoding. This narrows, and does not replace, the existing 1 MiB `SecurityHeaders` cap; nesting is safe because each reader enforces its own limit. Reading past 64 KiB yields `*http.MaxBytesError`, mapped to `413` `request_too_large` in §5.6. A valid create carries at most a few hundred bytes, so the cap is conservative and independent of `quantity`.

---

## 6. Validation and write semantics

### 6.1 Order of checks

Every write performs checks in this order so errors are deterministic and no database work occurs for invalid input:

1. Authenticate/authorize and verify CSRF (middleware).
2. Parse the route ID; non-positive or malformed → `404`.
3. Decode strictly (JSON) or bind (HTML adapter); unknown/immutable/malformed shape → `400` or `422`.
4. Validate field values (rune lengths, control characters, quantity bounds, enums, date format).
5. Resolve item metadata for create. A definitive "not found" selects metadata-missing handling; a database error aborts with `503` and is never treated as "missing".
6. Read open/bazaar day offsets when a date default is needed (create, or an update that resets a date); an invalid or unavailable setting aborts with `503`. A fully derived inverted window (`GuildOpenDays > BazaarDays`) is also `503`; an inverted explicit override is `422` on the submitted field.
7. Acquire the write transaction and row locks; re-check state and `ModAt`.
8. Insert/update rows and exactly one audit row atomically.
9. Map the fresh row and warnings to the response.

Step 5 is the one validation that depends on a database lookup: a blank `name` is rejected as `422 field=name` only after metadata resolution reports the EQUID absent (PD5-09). The name's type/length are still checked in step 4; only the "required when metadata is missing" condition waits for step 5.

### 6.2 Field rules

| Field | Rule | Failure field |
|---|---|---|
| `itemType` | Required create; exactly `Item` or `Spell`; immutable on update | `itemType` |
| `equid` | Required create; decimal integer `1..4294967295` (`GuildBankItems.EQUID` is `INT UNSIGNED`), accepted as a JSON number or decimal string; immutable on update | `equid` |
| `quantity` | Required create/update; no sign, decimal only; ordinary item create `1..100000`; spell create/update `1..4294967295`; ordinary update exactly `1`; spell merge sum must not exceed `4294967295` | `quantity` |
| `name` | Optional create fallback; trimmed; 1–128 runes when metadata is missing; 0–128 runes otherwise; no control characters; ignored when metadata exists | `name` |
| `sourceLocation` | Optional; trimmed; 0–128 runes; no control characters; empty → NULL; raw value preserved; group/sort derived | `sourceLocation` |
| `givenTo` | Optional; trimmed; 0–128 runes; no control characters; empty/`null` → NULL | `givenTo` |
| `dateOpen`, `dateBazaar` | Optional create override; required key on update (`null` resets to the Settings default). Exactly `YYYY-MM-DD` within MariaDB's `DATE` range `1000-01-01..9999-12-31`; `DateAdded <= value`, `dateOpen <= dateBazaar` after the edit | `dateOpen` / `dateBazaar` |
| `expectedModAt` | Required update/remove/restore; RFC 3339 with optional fraction up to 6 digits; parsed in UTC; exact microsecond equality | `expectedModAt` |
| `id` | Route parameter: positive signed 64-bit integer within MariaDB `BIGINT`; malformed → `404`. Query filter: same range; malformed → `422` | route: none; query: `id` |
| `status`/`sort`/`metadata`/dates/page | Allowlists and bounds from §5.2/§5.5 | matching parameter |

A text value is valid only if it is valid UTF-8 and contains no rune for which `unicode.IsControl(r)` is true. That standard predicate covers `U+0000–U+001F` (NUL included), `U+007F`, and `U+0080–U+009F`; the plan does not hand-roll a partial range. Leading/trailing whitespace is trimmed with `strings.TrimSpace`; interior whitespace is preserved. Unicode is counted in runes, not bytes. HTML forms submit empty optional fields as `""`, which the adapter converts to `nil` before calling the service. The service performs the same conversion for API JSON so both paths produce identical rows.

The existing code does **not** reject control characters in imported names or locations, and Phase 5 does not retroactively revalidate stored rows. The new check applies to manual admin input only. Imported rows keep their existing parser contract and the startup backfill only fills location columns.

Limits reuse existing constants wherever the meaning matches:

| Admin bound | Source | Value |
|---|---|---|
| `AdminMaxSearchRunes` | `service.PublicInventoryMaxSearchRunes` | 128 |
| `AdminMaxNameRunes` | `service.ImportMaxName` | 128 |
| `AdminMaxLocationRunes` | `service.ImportMaxLocation` | 128 |
| `AdminMaxGivenToRunes` | new constant, matches `GivenTo VARCHAR(128)` | 128 |
| `AdminMaxItemQuantity` | new constant, bounded by `service.ImportMaxRows` | 100000 |
| `AdminMaxSpellQuantity` | `uint32` column/storage limit | 4294967295 |
| `AdminMaxPageSize` | new admin constant | 200 |
| `AdminMaxBodySize` | new API constant in `internal/api`, handler-level `MaxBytesReader` | 65536 bytes |

`AdminMaxItemQuantity` deliberately differs from `ImportMaxQuantity` (10,000): that constant bounds one export line, while a manual item create is bounded by the total rows it may insert, which is the same 100,000-row ceiling as an import commit. Both are new/derived constants in `internal/service/admin_inventory.go`; no existing constant is redefined or widened. `AdminMaxBodySize` is an API-layer transport bound, not a service field limit, so it lives beside the admin handlers rather than in the service.

### 6.3 Create

```
normalize input
  -> resolve metadata (EQUID)
  -> read settings offsets once
  -> acquire commit lock + begin transaction
  -> if ItemType == Spell: SELECT all active spell rows FOR UPDATE in ID order
       if any: target = lowest ID; UPDATE quantity = quantity + requested (cap check)
               audit ITEM_UPDATED (note "spell quantity merged")
       else: INSERT one spell row
  -> if ItemType == Item: INSERT one row with Quantity = requested (QTY-07; no expansion)
  -> audit ITEM_CREATED for inserted rows (merge already wrote ITEM_UPDATED)
  -> commit
```

Details:

- `DateAdded` is the UTC calendar date returned by the injected service clock, captured once before the transaction; it is never client-supplied. `DateOpen` and `DateBazaar` are the submitted overrides when present, otherwise `DateAdded + GuildOpenDays` and `DateAdded + BazaarDays`. Offsets are read once from `Settings` and only when at least one default is needed. Every final date satisfies `DateOpen >= DateAdded`, `DateBazaar >= DateAdded` and `DateOpen <= DateBazaar`, otherwise `422` naming `dateOpen`/`dateBazaar` and no row is written. One exception: when both dates are settings-derived and the configured offsets themselves invert the window (`GuildOpenDays > BazaarDays`), the request fails with `503` rather than `422`, because no administrator-supplied value is at fault and the operator must correct `Settings`. Import's existing `dateFields` helper performs the offset arithmetic; it is not duplicated.
- `MetadataMissingFg=1` only when metadata is definitively absent. `Name` is then the trimmed fallback; `ClassString` and `RaceString` are `''`; a `metadata_missing` warning is returned. When metadata exists, the decoded name/class/race and `MetadataMissingFg=0` are authoritative and any submitted `name` is ignored.
- Metadata resolution uses the existing `ItemMetadataService.Resolve(ctx, domain.ItemReference{EQUID, Name})` (`internal/service/item_metadata.go`, fields `EQUID/Name/ClassString/RaceString/MetadataMissing/Warnings`). It happens before the advisory lock is acquired; a resolver error (the `Items` read failing) aborts the request with `503` and no row, audit entry or lock is written. `MetadataMissing` maps directly to `MetadataMissingFg`, so the existing `metadata_missing` warning is forwarded rather than reconstructed.
- New warning codes are ordinary `service` string constants beside the existing `ItemWarningMetadataMissing`: `ItemWarningLocationUnrecognized = "location_unrecognized"` and `ItemWarningDuplicateSpellTarget = "duplicate_spell_target"`. No new warning struct is introduced; `service.ItemWarning{Code, Field, Detail}` is reused and the API serializes only `{code, field, detail}`.
- `NameSort` uses the existing `service.NaturalSortKey` on the final name and is bounded exactly as import does: `service.TruncateRunes(NaturalSortKey(name), 160)` because `NameSort` is `VARCHAR(160)`. Digit runs are padded to ten characters, so an un-truncated key for a digit-heavy 128-rune name can exceed the column and would fail the insert; the same bound applies to `LocationSort` at 80 (`TruncateRunes(location.Sort, 80)`), matching `import_upload.go`. Both location fields come from PD5-03 and are never recomputed from client input.
- `ModAt` is one UTC `time.Time` with microsecond precision, reused for every row created by one request. `ModBy` is the session user name and `ModByUserID` the session user ID.
- **Amended 2026-11-22 by QTY-07:** an ordinary item is written as one row whose `Quantity` is the requested value, identical identity/date/location/recipient/audit values. The shared chunked helper still handles the insert; the former 100,000-unit expansion path no longer exists.
- The spell merge target is the lowest `GuildBankItemID` among active `ItemType='Spell'` rows for the EQUID; its `Name`, source location and recipient are preserved and only quantity/modification fields change. If more than one active spell row exists, the lowest ID is the merge target and a `duplicate_spell_target` warning is returned; other duplicates are not touched. If no active row exists, a spell row is created with the submitted source/recipient. A submitted `dateOpen`/`dateBazaar` override applies only when a new spell row is created; when a merge target is found the override is rejected with `422` before any `UPDATE`, because a quantity merge never changes an existing row's dates (Design.md).
- The create transaction inserts exactly one audit row. `CreatedCount` is 1 for both types; `CreatedIDs` contains that row's ID. The audit JSON uses `createdIds` with the same ID and carries the stored quantity (QTY-07).

### 6.4 Update

1. Lock the row with `SELECT` of all current columns `FOR UPDATE`. Absent → `404`.
2. `DeletedFg=1` → `409`; the admin must restore before editing.
3. Compare stored `ModAt.UTC().Truncate(time.Microsecond)` with the parsed `expectedModAt`. Mismatch → `409` without revealing current values.
4. Enforce the immutable-field rejection documented in §5.3, including rejecting a non-1 `quantity` when the stored row is an `Item`, and validate the `dateOpen`/`dateBazaar` format and ordering against the stored `DateAdded`.
5. Resolve the effective dates: the submitted value, or `DateAdded + current Settings offset` when the key is `null` (PD5-32). A `Settings` read happens only when a reset is requested; a failure there is `503` and nothing is written. The window `DateAdded <= DateOpen <= DateBazaar` is checked on the effective values, so editing a legacy row whose stored window is already inverted requires correcting both dates in the same request; such a row cannot otherwise be edited.
6. Recompute `LocationGroup`/`LocationSort` from the new raw `SourceLocation` using `NormalizeAdminLocation`; persist the sort key through `TruncateRunes(sort, 80)` exactly as import does, and never accept client-derived values.
7. Write `SourceLocation`, `LocationGroup`, `LocationSort`, `GivenTo`, `DateOpen`, `DateBazaar` (only when a date changed), `ModAt`, `ModBy`, `ModByUserID`, plus `Quantity` only for `Spell` rows (see §5.3). Identity, name sort and `DateAdded` remain untouched.
8. Write exactly one `ITEM_UPDATED` audit row containing only changed editable fields.

A request whose editable values are identical to the stored row is a success with `200`, but performs no write, does not change `ModAt` and writes no audit row. This keeps history meaningful and prevents a stale-token refresh loop from generating empty events.

### 6.5 Remove and restore

- Remove requires an active row; restore requires a removed row. The other state → `409`.
- Remove sets `DeletedFg=1`, attribution and `ModAt`, and writes one `ITEM_REMOVED`; new values are `{"deletedFg":true}` and old values `{"deletedFg":false}`.
- Restore sets `DeletedFg=0`, attribution and `ModAt`, and writes one `ITEM_UPDATED` with note `restored`.
- No physical delete exists in the service, repository, API or UI. A restored row keeps its original identity and dates.
- Removing or restoring a row does not alter its location fields, quantity or audit history.

### 6.6 Transaction and locking contract

- The lock name is the existing `guildbank:commit:` prefix plus `LEFT(SHA2(DATABASE(), 256), 32)`, acquired on a dedicated `*sql.Conn` for 30 seconds. Timeout maps to `503`. Import commit and admin writes therefore serialize, preventing spell-merge races and import/admin deadlocks.
- Inside the transaction the service locks the target row(s) with `FOR UPDATE`. For create-spell it locks all candidate active spell rows in ID order before selecting the lowest.
- The transaction writes rows and exactly one audit row, then commits. Any error rolls back both. The lock is released and the connection closed in `defer` on every return path.
- A detected deadlock (`ER_LOCK_DEADLOCK`/`1213`) may retry the whole operation once using the same captured `now` and expected `ModAt`; a second deadlock is `503`. Lock wait timeout (`1205`) is not retried.
- Reads (`GET` admin list/detail/audit) do not take the advisory lock. They observe committed state only; a multi-statement read uses a short read-only transaction so its rows and total come from the same snapshot.

---

## 7. Domain, service and repository design

### 7.1 Domain types

Add to `internal/domain/inventory_admin.go` (names may change during implementation, but the field semantics must not). The code block also shows `AdminWriteResult` for context; that one type lives in `internal/service/admin_inventory.go` because it carries `service.ItemWarning` (see below).

```go
type AdminInventoryStatus string // active | removed | all

type AdminInventorySort string
// modified_newest | modified_oldest | name | name_desc | location | quantity
// date_added | date_added_desc | date_open | date_open_desc
// date_bazaar | date_bazaar_desc

type AdminMetadataFilter string // any | missing | present

type AdminInventoryFilter struct {
    ID            int64 // 0 means no ID filter
    EQUID         uint64 // 0 means no EQUID filter
    Search        string
    GivenTo       string
    Metadata      AdminMetadataFilter
    LocationGroup string
    ItemType      ItemType
    Status        AdminInventoryStatus
    Sort          AdminInventorySort
    Page          int
    PageSize      int
}

type AdminInventoryItem struct {
    ID                int64
    EQUID             uint64
    Name              string
    ItemType          ItemType
    Quantity          uint32
    ClassString       string
    RaceString        string
    MetadataMissingFg bool
    SourceLocation    string
    LocationGroup     string
    LocationSort      string
    GivenTo           string
    DeletedFg         bool
    NameSort          string
    DateAdded         time.Time
    DateOpen          time.Time
    DateBazaar        time.Time
    ModAt             time.Time
    ModBy             string
    ModByUserID       *int64
}

type AdminInventoryPage struct {
    Records    []AdminInventoryItem
    Total      int
    Page       int
    PageSize   int
    TotalPages int
    Filter     AdminInventoryFilter
}

type CreateItemInput struct {
    ItemType       ItemType
    EQUID          uint64
    Quantity       uint32
    SourceLocation OptionalString
    GivenTo        OptionalString
    Name           string
    DateOpen       OptionalDate // optional; unset or null means "use the settings default"
    DateBazaar     OptionalDate // optional; unset or null means "use the settings default"
}

type UpdateItemInput struct {
    ID             int64
    ExpectedModAt  time.Time
    Quantity       uint32
    SourceLocation OptionalString // required key; null clears
    GivenTo        OptionalString // required key; null clears
    DateOpen       OptionalDate // required key; null resets to the settings-derived default
    DateBazaar     OptionalDate // required key; null resets to the settings-derived default
}

type AdminWriteResult struct {
    Item         AdminInventoryItem
    CreatedIDs   []int64 // ascending; at most 10,000 entries
    CreatedCount int     // total rows created; >= len(CreatedIDs)
    Warnings     []ItemWarning
    Changed      bool // false only for a no-op update
}

type AdminUpdateRow struct {
    ID             int64
    Quantity       *uint32       // nil for Item rows: Quantity is never assigned
    SourceLocation OptionalString
    GivenTo        OptionalString
    DateOpen       *time.Time // resolved value; nil leaves the column untouched
    DateBazaar     *time.Time // resolved value; nil leaves the column untouched
    ModAt          time.Time
    Actor          Actor
}

type AuditFilter struct {
    EventType     string
    User          string
    Table         string
    RecordID      string
    CorrelationID string
    From, To      *time.Time // inclusive date bounds, parsed by the service
    Page          int
    PageSize      int
}

type AuditPage struct {
    Entries    []AuditEntry
    Total      int
    Page       int
    PageSize   int
    TotalPages int
    Filter     AuditFilter
}
```

The existing `domain.AuditEntry` is reused, not redefined. It remains in `internal/domain/import.go` and gains one field, `AuditID int64`, because read contracts need the primary key; no second audit-entry type is introduced.

`OptionalString` has `Value *string` and `Set bool`. Its `UnmarshalJSON` accepts a string or `null`, sets `Set`, and rejects numbers/objects. A separate presence check rejects an absent key where `Set` is required. This is what allows §5.3 to distinguish "clear" from "unchanged".

`OptionalDate` mirrors it with `Value *time.Time` and `Set bool`: `Set && Value == nil` is an explicit `null` (reset to the settings-derived default), `Set == false` is an absent key. Its `UnmarshalJSON` accepts only a `YYYY-MM-DD` string or `null` and returns the date at midnight UTC, so no time-of-day or timezone offset can reach a `DATE` column. `time.Parse` accepts years `0000`–`9999`, while MariaDB's `DATE` range is `1000-01-01..9999-12-31`, so the service also range-checks the parsed date and rejects anything outside it as `422` instead of letting the insert fail.

`AdminWriteResult` lives in `internal/service/admin_inventory.go`, not `internal/domain`, because it carries `service.ItemWarning` (the existing warning type also used by mask/metadata decoding); the other admin types above live in `internal/domain`. Create warnings reuse `service.ItemWarning`; the API maps only `{code, field, detail}`, dropping internal numeric fields, and detail is fixed text, never raw SQL or metadata internals. The existing `domain.GuildBankItem` import projection gains `LocationGroup` and `LocationSort` so the shared insert helper can write them; see §4.4.

### 7.2 Service

Add `internal/service/admin_inventory.go` with one `AdminInventoryService`:

```go
type AdminStore interface {
    Ready() bool
    ListAdminInventory(ctx context.Context, f domain.AdminInventoryFilter) (domain.AdminInventoryPage, error)
    GetAdminInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error)
    ListLocationGroups(ctx context.Context) ([]string, error)
    BeginAdminWrite(ctx context.Context) (AdminWriteTx, error)
    ListAudit(ctx context.Context, f domain.AuditFilter) (domain.AuditPage, error)
    GetAudit(ctx context.Context, id int64) (domain.AuditEntry, error)
}

type AdminWriteTx interface {
    LockInventory(ctx context.Context, id int64) (domain.AdminInventoryItem, error)
    FindActiveSpells(ctx context.Context, equid uint64) ([]domain.AdminInventoryItem, error)
    InsertInventory(ctx context.Context, rows []domain.GuildBankItem) ([]int64, error)
    AddSpellQuantity(ctx context.Context, id int64, qty uint32, at time.Time, actor domain.Actor) error
    UpdateInventory(ctx context.Context, in AdminUpdateRow) error
    SetDeleted(ctx context.Context, id int64, deleted bool, at time.Time, actor domain.Actor) error
    InsertAudit(ctx context.Context, entry domain.AuditEntry) error
    Commit() error
    Rollback() error
}
```

`BeginAdminWrite` owns the dedicated connection, advisory lock, transaction and release/close behavior. The service never touches `*sql.Tx` or `*sql.Conn`. Auth/settings/metadata are existing narrow interfaces; `clock` is injected for tests.

Service methods: `List`, `Get`, `LocationGroups`, `Create`, `Update`, `Remove`, `Restore`, `ListAudit`, `GetAudit`. `List`/`Get` validate filters, capture one UTC date, and map records to admin DTOs (including `availability` via the existing public availability function). Writes return `AdminWriteResult`; errors are domain sentinels plus `*domain.ValidationError` with field keys. HTML adapters call these methods directly, satisfying D-03 because the UI does not reach the repository.

`Create` resolves metadata and settings. `Update` reads settings only when a date key is `null` and a reset is required (PD5-32); a settings failure at that point is `503`. Both handlers capture `now` once and pass the same `time.Time` to the row writes and the audit entry. `Remove` and `Restore` capture `now` once per attempt. Retry-on-deadlock reuses that captured value.

### 7.3 Repository

Add `internal/repository/mariadb/admin_inventory.go` and `admin_audit.go`:

- `ListAdminInventory` builds one parameterized query from a fixed column list and a fixed sort clause selected by an allowlist. It runs the count and page queries in a read-only transaction. `status=active` → `DeletedFg=0`; `removed` → `1`; `all` → no predicate. `locationGroup` is an exact `=` predicate (including `Unspecified`); `id` and `equid` are exact when non-zero; `givenTo` is exact under the column collation; `metadata` maps to `MetadataMissingFg` as in §5.2. `q` uses `Name LIKE ? ESCAPE '!'` with `%`, `_`, `!` escaped. `itemType` is exact.
- `GetAdminInventory` uses the same projection and returns `domain.ErrNotFound` for absent rows, regardless of `DeletedFg`.
- Every admin projection scan uses explicit nullability: `SourceLocation`, `GivenTo`, `NameSort`, `ModByUserID` and `LocationGroup`/`LocationSort` are scanned through `sql.NullString`/`sql.NullInt64` (or equivalent) and mapped to the domain's empty string/`nil`; a SQL NULL is never scanned directly into a `string`. `Name`, `ClassString`, `RaceString` and `ModBy` are `NOT NULL` and are scanned as plain strings. The audit projection follows the same rule: `AuditID`, `EventDate`, `UserName` and `EventType` are non-null; `UserID`, `TableName`, `RecordID`, `Notes`, `CorrelationID`, `SourceIP`, `OldValues` and `NewValues` are nullable and map NULL to `0`, `""` or an empty `json.RawMessage`. `Audit.AuditID` is `BIGINT UNSIGNED`; the scan targets `int64` and a value above `math.MaxInt64` is a mapping error, not a wrapped negative ID.
- `ListLocationGroups` returns the distinct non-null persisted groups in natural sort order: `SELECT LocationGroup FROM GuildBankItems WHERE LocationGroup IS NOT NULL GROUP BY LocationGroup ORDER BY MIN(LocationSort), LocationGroup LIMIT 200`. The 200 limit is a selector display bound, not a filter constraint: a group outside the first 200 is still valid for the `locationGroup` filter. It is a separate small query so the HTML filter offers only values that exist; it is never used to validate the `locationGroup` filter. A failure fails the page (`503`) rather than silently rendering an empty selector.
- `BeginAdminWrite` mirrors `BeginCommit`: dedicated connection, `GET_LOCK` with the same `guildbank:commit:` prefix and `LEFT(SHA2(DATABASE(), 256), 32)` suffix, transaction, and a cleanup guard that is idempotent on rollback/commit.
- `LockInventory` selects the explicit admin projection `FOR UPDATE`; `FindActiveSpells` selects all active spell rows for one EQUID ordered by ID `FOR UPDATE`; `UpdateInventory` updates only editable/attribution columns; `SetDeleted` updates only `DeletedFg`, attribution and `ModAt`.
- `InsertInventory` calls the shared `insertInventoryRows` helper, which now includes `LocationGroup` and `LocationSort` in its fixed 19-column list. It keeps 500-row chunks and the `LastInsertId()+i` convention.
- `InsertAudit` calls one shared audit insert helper reused by import commit, so column names and nullable handling cannot drift. It inserts one row; there is no update/delete method.
- The existing `importCommitTx.AddSpellQuantity` already performs the merge with `UPDATE GuildBankItems SET Quantity=Quantity+?, ModAt=?, ModBy=?, ModByUserID=? WHERE GuildBankItemID=? AND DeletedFg=0 AND ItemType='Spell'` and fails with `domain.ErrConflict` unless exactly one row was affected. Phase 5 reuses that SQL behind the admin write interface (signature gains `ctx`, `time.Time` and `domain.Actor`) and performs the `4294967295` overflow check on the locked sum in the service before calling it, because the SQL itself has no upper bound and an overflowing `INT UNSIGNED` assignment would surface as a driver error.
- `ListAudit` uses a fixed column list including `AuditID`, exact predicates for `user`, `table`, `recordId`, `correlationId` and `eventType`, inclusive date bounds (`EventDate >= from 00:00:00`, `EventDate < to+1day`), `EventDate DESC, AuditID DESC`, and `LIMIT/OFFSET`. `GetAudit` selects the same projection by primary key. Invalid date ranges are rejected by the service before SQL.
- SQL errors map through `mapStoreError`; new mapping: deadlock/lock-timeout → `domain.ErrUnavailable` (timeout) or a retryable indicator used only inside the service; absent rows → `domain.ErrNotFound`; stale conditional updates → `domain.ErrConflict`. Driver details stay in logs at Debug, never in returned errors.

No repository method accepts a raw sort string, a column name, a `WHERE` fragment or a full SQL statement. No admin read uses `SELECT *`.

### 7.4 File map

| Area | New/changed files |
|---|---|
| Migration | `internal/repository/mariadb/migrations/0003_admin_inventory.sql` (single embedded copy) |
| Domain | `internal/domain/inventory_admin.go`, `internal/domain/audit.go` (filter/page types only; existing `internal/domain/import.go::AuditEntry` is reused and gains `AuditID int64`, not redefined) |
| Service | `internal/service/admin_inventory.go`, `internal/service/admin_audit.go`, `internal/service/import_normalize.go` (extended with `normalizeLocationValue` and `NormalizeAdminLocation`; no file move) |
| Repository | `internal/repository/mariadb/admin_inventory.go`, `admin_audit.go`, `imports_commit.go` (shared helpers) |
| API | `internal/api/admin_inventory.go`, `admin_audit.go`, `router.go` |
| UI | `internal/web/templates/admin_inventory.html`, `admin_inventory_form.html`, `admin_inventory_detail.html`, `admin_remove.html`, `admin_audit.html`, `partials.html`, `app.css` |
| Wiring | `internal/platform/app/app.go` (store, backfill and service construction), `internal/api/router.go` dependencies |
| Tests | Unit/service/API/UI/migration tests listed in §13 |

No new third-party dependency is introduced.

---

## 8. Audit contract

### 8.1 Common fields

Every admin write inserts exactly one `domain.AuditEntry` inside its inventory transaction:

| Column | Value |
|---|---|
| `EventDate` | The single captured UTC `now`, microsecond precision |
| `UserID` / `UserName` | Session actor ID and name |
| `EventType` | `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED` |
| `TableName` | `GuildBankItems` |
| `RecordID` | Decimal primary key as a string (merge/update/remove/restore target; for a create that inserts rows, the first created ID) |
| `CorrelationID` | Existing middleware request ID |
| `SourceIP` | Client IP passed through the service input, never trusted from the body |
| `Notes` | Fixed text only: `spell quantity merged`, `restored` or NULL |
| `OldValues` / `NewValues` | JSON per §8.2 |

`SourceIP` is supplied by the API layer; the service does not read HTTP headers. If a trusted proxy is later introduced, its proxy-header configuration must be reviewed before the value is treated as authoritative. `AuditID` is never supplied by the writer; MariaDB assigns it and the read repository fills it.

### 8.2 Old/new payload allowlists

Create (`ITEM_CREATED`) writes `OldValues = {}` and this `NewValues` object:

```json
{
  "equid": 28813,
  "itemType": "Item",
  "name": "Blade of Carnage",
  "quantity": 3,
  "sourceLocation": "General5-Slot1",
  "givenTo": "Guild Bank",
  "metadataMissing": false,
  "dateOpen": "2026-12-05",
  "dateBazaar": "2026-12-21",
  "createdIds": [474,475,476]
}
```

`createdIds` contains at most the first 100 IDs; when `CreatedCount` is larger, `"createdIdCount"` is set to the total `CreatedCount`. This bounds a 100,000-row audit row.

Spell merge (`ITEM_UPDATED`, note `spell quantity merged`):

```json
{"old":{"quantity":2},"new":{"quantity":5,"added":3}}
```

Update (`ITEM_UPDATED`): old and new contain only fields that changed, chosen from `quantity`, `sourceLocation`, `givenTo`, `dateOpen` and `dateBazaar`; derived location fields are never stored. If only location changed, old/new carry `sourceLocation` and the viewer does not invent a group. Dates appear only when a lifecycle date actually changed, so a `null` reset records the resolved date, not `null`.

Remove (`ITEM_REMOVED`): `{"old":{"deletedFg":false},"new":{"deletedFg":true}}`. Restore (`ITEM_UPDATED`, note `restored`): the inverse.

No audit payload contains passwords, session tokens, cookies, headers, SQL, stack traces, full request bodies, class/race strings, `DateAdded` or user IDs. Old/new are JSON objects with fixed keys; a NULL column is returned as `{}` by the API, not `null`, for consistent clients.

### 8.3 Append-only and retention

The application exposes only INSERT and SELECT on `Audit` in Phase 5. There is no update, delete, purge, retention job or export endpoint. The migration adds only the correlation index. Operational grants denying `UPDATE`/`DELETE` on `Audit` remain recommended (E-11) but are not migrated, because grants are environment-specific and the current migration splitter cannot safely handle triggers.

Audit records are never cascaded by item changes. A soft delete/restore adds a row; it does not rewrite history. If an audit row cannot be inserted, the whole inventory write rolls back and the API returns `500`/`503`, never a success without evidence.

---

## 9. HTML admin UI

### 9.1 Pages and templates

New templates follow the existing full-page `{{define}}` + `head`/`nav`/`footer` pattern. No JavaScript is required for any admin flow; the UI works with forms, links, redirects and server-rendered errors.

| Route | Template | Content |
|---|---|---|
| `GET /admin/inventory` | `admin_inventory.html` | Filter form (`q`, `id`, `equid`, `givenTo`, `metadata`, `locationGroup`, `itemType`, `status`, `sort`, `pageSize`), result summary, table, pager, "Add item" link. The `locationGroup` selector is populated from `LocationGroups`; an empty database yields only the empty "Any" choice. |
| `GET /admin/inventory/new` | `admin_inventory_form.html` | Create form; `Item` or `Spell` radio, EQUID, quantity, source location, recipient, optional fallback name, and optional `dateOpen`/`dateBazaar` `type="date"` inputs that default to blank (blank means "use the guild default") |
| `GET /admin/inventory/{id}/edit` | `admin_inventory_form.html` | Edit form pre-filled from the locked read; hidden `expectedModAt`; identity and name are read-only display fields. `dateAdded` is shown read-only; `dateOpen` and `dateBazaar` are `type="date"` inputs pre-filled with the stored `YYYY-MM-DD`, each with the hint "blank resets to the guild default" — the adapter maps an empty date input to explicit `null` (PD5-32), not to `422`. Quantity is editable for both types; the item range is `1..100000` and the spell range is the column limit (QTY-08). |
| `GET /admin/inventory/{id}` | `admin_inventory_detail.html` | All fields, status badge, attribution, action links, recent 20 audit events, link to full history |
| `GET /admin/inventory/{id}/remove` | `admin_remove.html` | Confirmation page showing identity, quantity and location; POST only |
| `GET /admin/audit` | `admin_audit.html` | Read-only audit filters and table |

The shared `nav` template in `internal/web/templates/partials.html` gains conditional `Admin inventory` and `Audit` links by extending the existing inline `{{if .User.IsAdmin}}` block (line 18, which today holds only the `Imports` link); the nav is defined only there, not in `inventory_list.html`. The public `inventory_list.html` gains, for admins only, an "Edit" link on each row (the row view type already carries `ID`, so no new field is required). Public queries never include removed rows. The admin list is a separate page because it adds status, location and attribution columns that the public view must not expose.

### 9.2 Forms and feedback

- Every state-changing form is `method="post"` with `csrf_token` from `.CSRF`; remove and restore are POST to their routes, never GET.
- Successful writes redirect `303` to the detail page with a one-time flash message (`created`, `updated`, `removed`, `restored`, or `unchanged`). A POST refresh therefore does not repeat the write.
- Validation failures re-render the same form with `422`, the submitted values and field-level messages adjacent to inputs, linked by `aria-describedby`. Passwords and CSRF tokens are never echoed.
- `409` conflicts render a banner: "This record changed since the form was loaded" with a link to reload the edit page. The stale token is not hidden behind a generic error.
- `404` renders the existing not-found page; `403` renders the existing forbidden page. Admin routes never reveal whether another user exists.
- On an `Item` row the edit form's quantity input uses `readonly` (not `disabled`), so the stored `1` is still posted as `quantity` and satisfies the required-field rule in §5.3. A test asserts the item edit form marks the input `readonly` and does not mark it `disabled`.
- The remove page requires an explicit "Remove record" button; there is no destructive action without a confirmation step.
- Every flash message and error banner uses `role="status"` or `role="alert"` and is announced on page load.

### 9.3 Presentation rules

- Null source location or recipient renders as an em dash with an accessible label, never as the literal `null`.
- Derived group displays as a badge next to the raw location; `Unspecified` is shown only when the raw value is empty or unrecognized.
- Removed rows use a muted style plus a "Removed" badge; they are never hidden from the admin `all`/`removed` views.
- Tables include a `<caption class="sr-only">`, `<th scope="col">`, and a scrollable region with `tabindex="0"`, matching existing templates.
- Pagination links are built with `url.Values`, preserve every active filter, and use `rel="prev"`/`rel="next"`.
- All interpolation uses `html/template` auto-escaping. No `template.HTML`, inline event handlers or inline `<script>` is added. A test asserts generated admin templates contain no `template.HTML`.
- Existing CSS classes are reused; new classes are additive in `app.css`. The layout remains usable at 320 px width and with keyboard only.

### 9.4 Navigation and access

- The nav shows `Admin inventory` and `Audit` only when `.User.IsAdmin` is true. This is presentation only; every route still enforces admin authorization server-side.
- The public detail page does not expose audit, attribution user IDs or removed records.
- Create/edit forms show the server-computed location group preview only after a successful save; the UI never submits `locationGroup`/`locationSort`.
- `dateAdded` is displayed read-only in ISO and human formats because it is the immutable commit record. `dateOpen` and `dateBazaar` use native `type="date"` inputs on the edit form (server-rendered, no JavaScript required) and a blank input means "reset to the guild default"; the settings ceiling (`0..3650` days) means a derived default is at most ten years after `DateAdded`, but an override may be any valid `DATE`. The create form accepts the same two optional date inputs.

### 9.5 Public and administrative inventory UI requirements (owner, 2026-11-22)

The owner reviewed the implemented Phase 4 public inventory presentation and issued seven UI requirements with the Phase 5 implementation instruction. They are recorded here as the normative text and implemented as `PD5-33`–`PD5-39`. They change presentation only; no public JSON shape, route, filter value, date rule or admin contract changes.

| ID | Requirement (owner wording, abridged) | Implementation |
|---|---|---|
| UI-01 | Responsive content width. Main inventory content shall expand with available desktop viewport width; avoid narrow fixed widths that force excessive wrapping; prefer a responsive max-width approach suitable for large monitors. | `PD5-33`: `min(120rem, calc(100% - 2rem))` for `main.card.inventory-card` and the admin cards; `clamp()` padding retained; 320 px usable. |
| UI-02 | Inventory list columns. Remove `GuildBankItemID` and `EQUID` from the main inventory grid; both remain available on the item detail page and in administrative views. | `PD5-34`: the row identity sub-line is removed from `inventory_list.html` only. Detail, admin list, admin detail, admin edit and both JSON contracts keep both values. |
| UI-03 | Filter layout. Desktop filter controls use a compact multi-column layout; class, race, item type, sort and future filters do not each consume a full row; mobile usability preserved. | `PD5-35`: one grid for public filters and one `auto-fit` grid for admin filters; ≤1100 px two columns, ≤640 px one column. |
| UI-04 | EQResource link presentation. Replace the repeated text column with a compact icon-only external-link control; accessible tooltip/title "View on EQResource"; continue opening in a new tab with the existing safety protections. | `PD5-36`: inline SVG icon inside an anchor with `title`, `aria-label`, `target="_blank"` and `rel="noopener noreferrer"`; a muted dash with `sr-only` text when no URL exists. |
| UI-05 | Minimize unnecessary wrapping. Optimize `Name`, `Class`, `Race` and `Availability` presentation to reduce wrapping where horizontal space exists; favour readability and density on desktop. | `PD5-38`: nowrap cells at ≥900 px with the existing `table-scroll` overflow region; wrapping stays available below that width. |
| UI-06 | Lifecycle date behavior (reaffirmed). `DateAdded` = date committed into GuildBank; `DateOpen` defaults to `DateAdded + GuildOpenDays`; `DateBazaar` defaults to `DateAdded + BazaarDays`; Settings changes never retroactively alter existing records; administrators may override `DateOpen` and `DateBazaar`. | `PD5-39`: unchanged from PD5-27/PD5-32. §6.3, §6.4 and the §13 date tests remain the operative contract; no code path in this pass touches stored dates. |
| UI-07 | Dynamic availability. When the item is added the text says `Mains`; when the "open to personas" date is reached, `Persona`; when the "open to the bazaar" date is reached, `Bazaar`. The detail page contains the date added, the date open to personas and the date open to the bazaar as specific dates. | `PD5-37`: labels `Mains`/`Persona`/`Bazaar` from the stored dates and one captured UTC date; detail page labels `Date added`, `Open to personas`, `Open to bazaar` with ISO `datetime` and human text. `PD5-40` makes the boundary inclusive: `Mains` while `today < DateOpen`, `Persona` while `DateOpen <= today < DateBazaar`, `Bazaar` from the `DateBazaar` day on. |

Implementation notes that keep the amendment inside the approved architecture:

- The availability **state** is not renamed. `service.Availability` keeps returning `domain.AvailabilityUpcoming`/`AvailabilityOpen`/`AvailabilityBazaar`, the JSON field keeps those three values, and only `inventoryItemViewOf` maps a state to the display label. This preserves `PublicInventoryAPI.md` and the Phase 4 contract tests.
- The badge CSS classes stay `availability-upcoming`/`availability-open`/`availability-bazaar` so the color semantics do not depend on the label text, and a future wording change does not require a stylesheet change.
- The icon is inline SVG inside the template, so the existing `Content-Security-Policy` (`img-src 'self' data:`, no external origins) and the no-CDN rule in PD5-21 continue to hold. No icon font, sprite sheet or external stylesheet is added.
- Filter markup stays a plain `GET` form. The admin filter form posts `GET` to `/admin/inventory`; the create/edit/remove/restore forms remain `POST` with `csrf_token`.
- The admin list keeps `ID` and `EQUID` as its first columns because the owner's requirement is explicit that administrative views retain both identifiers; the admin list adds a `Location` column showing the raw value with the derived group as a badge.
- The public list link column is header `Link`; the admin list keeps a `History` link to the audit viewer instead, so the two tables do not drift into the same layout.
- **Boundary note (resolved 2026-11-22 by PD5-40).** During validation the implementation still derived the label from the pre-decision `service.Availability`, which returned `AvailabilityBazaar` only for `today.After(dateBazaar)`, so the `DateBazaar` day rendered `Persona`. The owner reviewed the wording conflict with `UI-07` and ruled for inclusive thresholds: `Mains` while `today < DateOpen`, `Persona` while `DateOpen <= today < DateBazaar`, `Bazaar` when `today >= DateBazaar`. The change is one comparison (`!today.Before(dateBazaar)`) plus the unit tests that pin the boundary; the state names, JSON field, routes, filters, stored dates and label mapping are unchanged. The single behavioural consequence is that the public `availability` value on the `DateBazaar` day itself moves from `open` to `bazaar`, which is what `UI-07` describes and what `PD5-37`'s "no public JSON change" clause was protecting against; PD5-40 supersedes that clause on that one day. Equal `DateOpen`/`DateBazaar` values now have no `Persona` day. Evidence is in `Docs/Phase5Validation.md` §7 and §11.

---

### 9.6 Post-Phase-5 admin and import additions (PD5-41–PD5-46)

- `/admin/inventory` adds a Select column (active rows only) and one page-local
  bulk form. Every checkbox value is the opaque `id|modAt` token. Submitting the
  form (plain POST, no JavaScript) renders `admin_bulk_remove.html`, which
  re-posts the same tokens to `/admin/inventory/bulk-remove/apply`. The apply
  handler calls `RemoveMany`, redirects to the list with a bounded
  `notice=bulk-removed&count=N`, and never performs a filter-wide action.
- `/admin/inventory/{id}/edit` adds a **Save and remove** submit button
  (`name="action" value="save-remove"`). It sends the same fields plus the
  removal request; the update service commits both, and the notice distinguishes
  `updated-removed` from a removal with no field changes.
- The admin list/detail/filter present `Unspecified` as
  **Other (not a bank location)**.
- The import review page adds the group scope (IMP-06), the Kind column
  (IMP-03), the Other banner/label (IMP-02) and **Reset to defaults** (IMP-04);
  the batch meta shows the ignored nested-slot count (IMP-01).

## 10. Performance

### 10.1 Targets

| Operation | Target (p95, warm pool) |
|---|---|
| Admin list, no filter, 250k rows | ≤ 250 ms |
| Admin list, location-group filter, 1M rows | ≤ 400 ms |
| Admin list, name search (`LIKE '%…%'`), 1M rows | ≤ 1200 ms, documented as scan |
| Admin list, unindexed sort (`date_added`, `date_open`, `quantity`), 1M rows | ≤ 1200 ms, documented as filesort |
| Detail read | ≤ 50 ms |
| Create/update/remove/restore | ≤ 300 ms excluding lock wait; lock wait ≤ 30 s |
| Audit list, 1M audit rows | ≤ 400 ms |
| Create 100,000 ordinary units | ≤ 30 s, one transaction |

Targets are measured in the Phase 5 acceptance environment, not production, and are goals rather than contractual SLOs. A miss is recorded with the `EXPLAIN` output before changing SQL.

### 10.2 Index and query evidence

The migration adds `(DeletedFg, LocationGroup, LocationSort, NameSort, GuildBankItemID)`, `(DeletedFg, ModAt, GuildBankItemID)` and `(DeletedFg, ItemType, ModAt, GuildBankItemID)`. Verification must show:

- `EXPLAIN` for default active sort uses `ix_gbi_admin_modified` and reports no filesort.
- `EXPLAIN` for a location-group filter plus grouped name sort uses `ix_gbi_admin_location`.
- `EXPLAIN` for a location-group filter with the default modified sort may use the same index and filesort within the group; the measured cost is recorded and compared with the target. An additional index is not added unless the benchmark misses and the owner approves a follow-up migration.
- `EXPLAIN` for an item-type filter with modified sort uses `ix_gbi_admin_type_modified`.
- `EXPLAIN` for an exact `givenTo` filter uses `ix_gbi_given` or records why the optimizer chose another plan; it is not a substring search.
- `EXPLAIN` for `date_bazaar` sorts may use `ix_gbi_dates`; `date_added`, `date_open` and `quantity` sorts have no covering index in this phase and must record their filesort cost. If any misses the 1M target, the validation document records the miss and a proposed follow-up index for owner approval rather than adding one silently.
- `EXPLAIN` for audit filters uses `PRIMARY`, `ix_audit_correlation`, or a documented scan for broad date-only queries.
- `SHOW INDEX` confirms index names and column order.

`LIKE '%term%'` cannot use a normal index. If the target is missed at 1M rows, the plan is not to add a full-text index silently; it is to record the miss in §16 and propose a scoped follow-up. Count queries use the same predicates and are measured with the page query.

### 10.3 Bounded work

- `pageSize ≤ 200`, `page ≤ 10000`, so `OFFSET ≤ 2,000,000`; deeper requests are `422`.
- Create writes one row for either type (QTY-07); the 64 KiB admin write body cap (§5.6), inside the existing 1 MiB global middleware cap, bounds input, while `quantity` bounds the stored value.
- Audit list `pageSize ≤ 200`; old/new JSON is truncated only at display time, never in storage.
- No admin `GET` performs writes, metadata lookups or settings reads.
- Slow query logging at `200 ms` is temporary in the acceptance environment and removed before release unless the owner keeps it.

### 10.4 Measurement procedure

1. Restore a copy of the acceptance database; record row counts for `GuildBankItems` and `Audit`.
2. Run each target query 20 times via `ab`/`hey`-equivalent or a Go benchmark against a warm pool; report p50/p95.
3. Capture `EXPLAIN` and `SHOW INDEX` for each distinct predicate/sort combination.
4. Record results in the phase evidence file with date, row counts and database version.
5. Re-run after migration verification and after the startup backfill completes.

These checks do not require production access and use no real guild data beyond what the owner already approved for acceptance.

---

## 11. Security

### 11.1 Threat and control matrix

| Threat | Control | Verification |
|---|---|---|
| Unauthenticated or non-admin access | Every `/admin/*` route sits behind the existing session middleware plus an admin check. HTML preserves the existing middleware behavior (anonymous → `303` to `/login?next=…`, authenticated non-admin → `303` to `/login`); API routes return `401 unauthenticated` or `403 forbidden` with the envelope. | Route-table test for all methods, with anonymous, member and admin sessions |
| IDOR / record enumeration | Admin routes require admin; public routes never expose removed rows or audit; detail by ID always checks `DeletedFg` only for the public path | API tests request another user's/nonexistent IDs; responses are `404`/`403`, not data |
| CSRF | Every state-changing request uses an unsafe method (`POST`, `PUT` or `DELETE`) and is rejected by the existing CSRF middleware without a valid token. HTML forms send `csrf_token`; JSON clients send `X-CSRF-Token`. `SameSite` cookie settings are unchanged and GET has no state change. | Test rejects missing/invalid token on every write route with `403`; GET remove page has no side effect |
| SQL injection | Fixed SQL text, parameterized values, allowlisted sort/where fragments, no string interpolation of user input; `LIKE` escape helper | Repository tests with `' OR 1=1 --`, `%`, `_`, `!`; static search for `fmt.Sprintf` near SQL |
| Mass assignment | DTOs contain only documented fields; strict decoder rejects unknown and immutable keys; server derives location, `DateAdded` and identity, and validates any submitted lifecycle date | Table-driven JSON tests for every forbidden field |
| Stored XSS | `html/template` auto-escaping, no `template.HTML`, no inline JS, existing CSP; JSON responses use `application/json` | Template test and rendered-page test with `<script>` in name/location/recipient |
| Audit tampering | No update/delete path; append-only helper; mutation and audit share one transaction | Test that a failed insert leaves no inventory change and vice versa; static absence of `UPDATE Audit`/`DELETE FROM Audit` |
| Information disclosure | Public DTOs exclude attribution, audit and removed rows; errors contain fields, not SQL; `no-store` on admin HTML/JSON | Response-shape tests; grep for driver error strings |
| Resource exhaustion | Quantity cap 100,000; page size/offset caps; 64 KiB admin write body cap; 30 s lock wait; one transaction per write; no unbounded audit payload | Boundary tests at cap and cap+1; lock-timeout test |
| Credential leakage | Admin forms never pre-fill or echo passwords; audit allowlist excludes tokens, cookies and headers; logs use correlation IDs | Audit payload test; log-capture test |
| Clickjacking / MIME sniffing | Existing security headers are retained; admin pages add no iframe or external script | Header assertions on admin responses |
| Open redirect | Redirect targets are fixed server-built paths, never user-supplied URLs | Test a malicious `returnTo` if any is ever added |

### 11.2 Authorization model

- Admin is a single existing role; Phase 5 adds no delegation, per-guild scoping or moderator role. The owner-approved requirement is "the admin can manage inventory", not a granular permission system.
- Authorization is checked before body decoding, so an unauthorized caller cannot probe validation behavior.
- `ModBy` is always the authenticated session actor, never a submitted name. A request that supplies `modBy` is rejected as unknown.
- The audit viewer requires admin. It is intentionally not public even though read-only.
- A degraded guild store returns `503` for admin pages; it must not render an empty inventory that looks like data loss.

### 11.3 Secrets and configuration

- No new secret, key, token or environment variable is introduced.
- The advisory lock name is derived from the first 32 hex characters of `SHA2(DATABASE(), 256)`, matching the existing commit and migration locks; it is not a secret, so exposing it in logs is acceptable.
- Source IP is logged for audit only and is not used for authorization.
- CSRF and session behavior are unchanged; this phase must not weaken cookie flags or the CSP to support forms.

---

## 12. Migration reconciliation

### 12.1 Source of truth and migration manifest

`internal/repository/mariadb/migrations/0003_admin_inventory.sql` is embedded by `migrate.go` and is the only migration copy. The repository has no top-level `migrations/` directory; PD5-22 does not create one.

Implementation adds `internal/repository/mariadb/migration_files_test.go` which:

1. Globs `migrations/*.sql` from the same embedded `migrationFiles` FS used by the runner.
2. Asserts the exact filename set: `0001_guild_store.sql`, `0002_public_inventory_indexes.sql`, `0003_admin_inventory.sql`.
3. Asserts every filename matches `^\d{4}_[a-z0-9_]+\.sql$` and that the four-digit prefixes are unique and ascending.
4. Compares each file's SHA-256 with a checked-in expected hash and prints the actual hash on failure.
5. Asserts `0003` contains exactly the two expected `ALTER TABLE` statements and no `DELIMITER`, trigger or procedure text.

The test runs in the normal `go test ./internal/repository/mariadb` job and needs no database. It catches an accidental edit, rename, deletion or addition without duplicating the file or changing the runner.

### 12.2 Apply behavior

- `Migrate` sorts versions, skips applied versions, refuses an embedded version older than the maximum applied version, executes statements in order and then inserts `schema_migrations`.
- `0003` executes two statements: the `GuildBankItems` column/index `ALTER`, then the `Audit` index `ALTER`. There are no `DELIMITER`, procedures, triggers or comments containing semicolons, because the current splitter cannot handle them.
- Because MariaDB DDL auto-commits, a crash between the first `ALTER` and the `schema_migrations` insert can leave a partially applied version that fails on retry with "duplicate column/index". The runbook is therefore: take a backup immediately before applying, record the pre-migration `schema_migrations` maximum, and on partial failure restore that backup rather than hand-editing schema. No automatic repair is promised.
- A fresh database applies `0001`, `0002`, `0003` in order and needs no backfill because the table is empty. An existing database applies only `0003`, then runs the startup backfill before serving.
- The application database account must retain `ALTER` and `INDEX` privileges for this release. If it does not, startup fails with a clear migration error and the service stays unavailable rather than running on the old schema.

### 12.3 Verification

1. Run `go test ./internal/repository/mariadb` (hash and schema tests).
2. Apply to a restored acceptance copy; confirm version `0003_admin_inventory` is recorded exactly once.
3. `SHOW COLUMNS FROM GuildBankItems` confirms nullable `LocationGroup VARCHAR(64)` and `LocationSort VARCHAR(80)` after `SourceLocation`.
4. `SHOW INDEX FROM GuildBankItems` confirms exactly one of each new index name; `SHOW INDEX FROM Audit` confirms `ix_audit_correlation`.
5. `SELECT COUNT(*) FROM GuildBankItems WHERE LocationGroup IS NULL OR LocationSort IS NULL` is zero after the startup backfill completes.
6. Compare `SHOW CREATE TABLE` from the acceptance database with a fresh migrated database; the only permitted differences are data-independent table options if the environments differ.

### 12.4 Rollback and coexistence

- There is no down migration. Dropping the columns would discard derived data, and no Phase 5 code path requires it.
- Rolling back to the previous binary is safe: it ignores the new nullable columns and indexes. Public reads are unchanged. Imports and old-code admin writes stop populating locations, so after rolling forward again the backfill fills the gaps.
- The Phase 5 binary must not run against an un-migrated database. Startup order is migrate → backfill → serve; failure at either step is fail-closed.
- A rollback must not delete audit rows or rewrite `schema_migrations`. Restoring the pre-migration backup is the only supported way to remove `0003`.

### 12.5 Documentation updates after approval only

`Docs/DatabaseDesign.md` §4.2 and §7.3, `Docs/ProjectStructure.md`, `README.md` and `Docs/ProjectStatus.md` are updated after implementation and live validation, not as part of this plan. They must record column names, index names, the version string, the single embedded migration source and the backfill behavior, and they must correct the current statement that `DateOpen`/`DateBazaar` are immutable after creation: `DateAdded` remains immutable, while the two lifecycle dates become editable per row under PD5-32 and are never rewritten by a `Settings` change. `Docs/ImplementationPlan.md` §(mutable fields) is corrected the same way.

---

## 13. Test gates

### 13.1 Unit tests

| Package | New tests |
|---|---|
| `internal/service` | New `import_normalize_admin_test.go`: every D-05 vector, blank/NULL, import warning compatibility, admin `location_unrecognized`, rune cap, no title-casing, exact escaped `NaturalSortKey` sort values (including `0x01` and zero padding), 80/160 `TruncateRunes` bounds, and identical group/sort across both wrappers. `admin_inventory_test.go`: create ordinary/spell/merge/duplicate spell, quantity caps including the item-edit `quantity != 1` rejection, metadata missing vs error, settings-offset failure, date default composition (`DateAdded + offset`), accepted overrides, override rejected when a spell merge target exists, `dateOpen`/`dateBazaar` format and `1000-01-01..9999-12-31` range errors, `DateOpen < DateAdded` and `DateOpen > DateBazaar` rejection, a fully derived inverted window returning `503` while an inverted override returns `422`, editing a legacy row with an inverted stored window only succeeding when both dates are corrected, `null` reset using the current offset plus its settings failure, date change counted as a change (not a no-op), field validation order, immutable fields, no-op update, stale `ModAt`, remove/restore state errors, lock timeout, single audit per write, retry once on deadlock. `admin_inventory_test.go` also covers `LocationGroups` passthrough and failure propagation, and every list filter (`q`, `id`, `equid`, `givenTo`, `metadata`, `locationGroup`, `itemType`, `status`) plus sort/paging allowlists. |
| `internal/service` (audit) | Filter validation, inclusive date bounds, page caps, `{}` for NULL old/new, payload allowlists. |
| `internal/api` | Strict decode: unknown field, duplicate field, immutable field, trailing JSON, body over limit, malformed ID, absent required key, `null` required key, `expectedModAt` formats, `dateOpen`/`dateBazaar` absent vs `null` vs `""` vs `2026-12-05` vs `2026-12-05T00:00:00Z` vs `999-01-01` vs `1000-01-01`. Content-type handling. Error envelope fields and status codes. Collection query parsing for every filter, including malformed `id`/`equid`, `metadata` and first-value behavior for repeated parameters. Audit `id`, nullable `userId` and text fields, and `{}` old/new mapping. |
| `internal/api` | Route authorization matrix for every `/admin/inventory` and `/admin/audit` method with anonymous/member/admin sessions. |
| `internal/api` | Correlation-ID bound (PD5-31): valid 8–36 character header is echoed and stored; 37-character and 64-character headers are replaced by a generated ID; both an admin write and an import commit complete and persist a `CorrelationID` no longer than 36 characters. |
| `internal/web` | Template parse for all new templates; no `template.HTML`; rendered pages escape a `<script>` payload; admin nav hidden for members; forms contain CSRF; remove page has no side effect; the item edit form's quantity input is `readonly` and not `disabled`, and the posted `quantity=1` is accepted; the edit form pre-fills both `type="date"` inputs with the stored `YYYY-MM-DD`, shows `dateAdded` read-only, and a blank date input is persisted as a reset to the settings default rather than rejected. |
| `internal/repository/mariadb` | Migration hash/filename test; SQL builder tests for filter/sort allowlists and `LIKE` escaping; audit query builder tests; audit scan tests for `AuditID` and nullable columns; no `SELECT *`. These do not require a database where the query text is built separately. |

Tests use the existing fake clock, fake store and `httptest` conventions. They do not sleep for lock behavior; the fake tx returns the sentinel error.

### 13.2 Database integration tests

Opt-in, matching the existing `GUILDBANK_INTEGRATION=1` and `GUILDBANK_TEST_MARIADB_*` policy, against a disposable schema:

1. Apply all migrations; assert version and schema.
2. Insert a mix of known/unknown/blank locations; run `BackfillInventoryLocations`; assert every row populated, `ModAt` unchanged and `Audit` count unchanged.
3. Interrupt/restart simulation: leave nulls, rerun, assert idempotence and no duplicate updates.
4. Concurrent backfill lock: second caller fails to acquire and does not write.
5. Create 1,000 ordinary units in one transaction; assert 1,000 rows and one audit row.
6. Spell merge with two active spell rows returns the `duplicate_spell_target` warning, merges only the lowest-ID row and leaves the other duplicate untouched.
7. Update/remove/restore audit rows match old/new expectations.
8. Import commit and admin create concurrently for the same spell EQUID; assert serialization by the shared lock and a single merged quantity.
9. Create the maximum-length digit-heavy name and a maximum-length raw location from §13.1; assert the stored `NameSort` is at most 160 characters, `LocationSort` at most 80 and `LocationGroup` at most 64, so no write relies on the database rejecting an overlong key.
10. Attempt an admin update that sets `quantity=2` on an `Item` row; assert the service rejects it with `422` for field `quantity` and that no row, audit entry or `ModAt` change is written.
11. Forced audit-insert failure rolls back the inventory change.
12. Offset pagination under a concurrent modification: assert the documented behavior that each request is a consistent snapshot, while a row whose sort key changes between requests may move between pages; no duplicate rows appear within one response.
13. Lifecycle date override round-trip: create rows with `dateOpen`/`dateBazaar` overrides and assert the stored `DATE` values exactly; update one date and assert the audit `old`/`new` contain only that date and that `DateAdded` is unchanged; reset with `null` and assert the value equals `DateAdded + current settings offset`. Also assert an update violating `DateAdded <= DateOpen <= DateBazaar` returns `422` and writes nothing, so neither `ck_gbi_open`, `ck_gbi_bazaar` nor E-10 is ever exercised by the database.
14. Settings non-retroactivity: after rows exist, change `GuildOpenDays`/`BazaarDays` through the settings service; assert every existing `DateOpen`/`DateBazaar` is unchanged and no audit row was written, then reset one row and assert only that row adopts the new offset.

These tests skip cleanly when the variables are absent. CI without MariaDB still runs every unit and handler test.

### 13.3 Performance tests

Add `internal/integration/admin_inventory_perf_test.go`, guarded by `GUILDBANK_PHASE5_PERF=1`, requiring a disposable schema named `GuildBankPhase5Perf` and refusing any other database. It seeds 250,000 and 1,000,000 rows, runs the §10 queries, captures `EXPLAIN`, and fails only on a hard timeout or incorrect results; target misses are reported as logged evidence rather than flaky CI failures.

The backfill benchmark runs at both sizes and records rows/second. The 100,000-unit create benchmark records total duration and chunk count.

### 13.4 Commands and gates

```bash
gofmt -l cmd internal          # must print nothing
go vet ./...
go test ./...
go test -race ./...
GUILDBANK_INTEGRATION=1 go test ./internal/integration -v
```

A change is not mergeable unless:

- All new unit/API/UI tests pass with no skips other than documented opt-ins.
- `go vet` and `go test -race ./...` pass.
- The migration hash test passes and the embedded migration manifest contains exactly `0001`, `0002` and `0003` with no second copy of `0003`.
- The API contract tests cover every status code in §5.6.
- No test depends on wall-clock sleeps, production data or a live MariaDB unless explicitly opt-in.
- Every AC5 item has at least one named test; the mapping is added to the implementation notes, not this plan.

### 13.5 Manual acceptance

A short checklist covers what automation cannot: keyboard-only create/edit/remove/restore, screen-reader labels on error messages, 320 px layout, confirmation page focus order, and the visible difference between `Unspecified` and a recognized group. Results are recorded in `Docs/Phase5Validation.md` after implementation.

---

## 14. Rollout, observability and operations

### 14.1 Pre-deployment checklist

1. Confirm the owner has approved the remaining PD5 items (`PD5-01`–`PD5-26`, `PD5-28`–`PD5-30`; `PD5-27`, `PD5-31` and `PD5-32` are already approved) and resolved or accepted the §16 items, including the two residual date confirmations in §16.6.
2. Back up the MariaDB schema and record `SELECT MAX(Version) FROM schema_migrations` and row counts for `GuildBankItems` and `Audit`.
3. Confirm the application account can `ALTER` and add indexes; confirm there is enough disk for a table rebuild if the deployed MariaDB cannot do the `ALTER` in place.
4. Confirm the maintenance window covers the `ALTER` duration at the production row count. The backfill can resume, but a DDL lock wait blocks writes.
5. Build and run the full test suite from the release commit; archive the commands and results.
6. Record the expected migration version `0003_admin_inventory` and the previous binary image tag.

### 14.2 Deployment sequence

1. Stop the application container (do not stop MariaDB).
2. Start the new binary; `Migrate` applies `0003` under the migration lock.
3. Startup calls `BackfillInventoryLocations` before serving. It logs start, batches, progress every 10,000 rows and completion with counts and elapsed time.
4. Readiness remains `503` until migration, backfill and the existing checks pass. Admin routes cannot be reached through a partially prepared schema.
5. Smoke test: admin list, create an item into a test location, verify its audit row, update it, remove it, restore it, then soft-delete the test row and confirm the public list is unchanged.
6. Re-enable normal traffic.

### 14.3 Failure handling

| Failure | Behavior | Operator action |
|---|---|---|
| Migration lock timeout | Startup fails, no schema change | Check for another instance/migration; retry after it exits |
| Partial DDL after crash | Startup fails on duplicate column/index | Restore the pre-migration backup, then retry once |
| Backfill lock not acquired | Startup fails before serving | Stop the other instance; restart |
| Backfill error mid-run | Startup fails; committed batches remain | Fix the cause; restart; it resumes at the first null |
| Lock timeout on a write | API returns `503` | Retry after the import/admin transaction finishes |
| Deadlock twice | API returns `503` | Retry the request; no partial write exists |
| Audit insert failure | Whole write rolls back, returns `500`/`503` | Check DB health and audit table/indexes |
| Settings unavailable or invalid during a create or date reset | API returns `503`, nothing written | Restore the setting; `GuildOpenDays`/`BazaarDays` must be whole numbers `0..3650` and must not invert the window |
| Store unavailable | Admin routes return `503`, public path follows existing degraded behavior | Restore connectivity; no admin write is queued |

### 14.4 Observability

- Logs: route, method, status, duration, correlation ID, actor ID, operation (`create`, `update`, `remove`, `restore`) and row count. No item name, recipient, raw location, body or audit JSON.
- Metrics/log counters (existing mechanism only): admin request count by status, write duration, lock-wait duration, lock timeout count, deadlock retries, audit insert failures, backfill rows processed and backfill duration.
- The log line for a failed write carries the correlation ID that also appears in the audit row for successful writes, so an operator can correlate a report to an event without exposing payloads.
- No new external telemetry, tracing service or dashboard dependency is added.

### 14.5 Post-deployment validation

- Confirm `schema_migrations` has one `0003_admin_inventory` row.
- Confirm null location count is zero and the process logged backfill completion.
- Confirm one manual create produced one row (or N rows for quantity N), one audit row, correct dates and correct derived location.
- Confirm a manual edit of an `Item` row succeeds with the form's posted `quantity=1`, and that `SELECT MAX(CHAR_LENGTH(NameSort)), MAX(CHAR_LENGTH(LocationSort)) FROM GuildBankItems` stays within 160 and 80.
- Confirm the approved date rule live: a create with no override stores `DateAdded + GuildOpenDays`/`+ BazaarDays`; an explicit override is stored verbatim; an edit that would place `DateOpen` before `DateAdded` or after `DateBazaar` is rejected with `422`; and changing `GuildOpenDays`/`BazaarDays` in Settings leaves every existing row's dates unchanged (`SELECT` before/after comparison).
- Confirm `EXPLAIN` on the live schema still matches §10.2.
- Record the results in `Docs/Phase5Validation.md` and update `Docs/ProjectStatus.md` only after success.

---

## 15. Risk register

| ID | Risk | Impact | Mitigation |
|---|---|---|---|
| R5-01 | Partial DDL cannot be re-run automatically | Extended outage | Backup-first deployment checklist (§14.1) and apply behavior (§12.2); no manual schema edits |
| R5-02 | Backfill time on 1M rows delays startup | Longer maintenance window | Batched keyset updates, progress logs, resumability, measured benchmark |
| R5-03 | `LIKE '%term%'` name search is slow at 1M rows | Admin latency | Documented scan; target measured; follow-up decision instead of a silent index |
| R5-04 | Location-group + modified sort may filesort | Slower filtered page | `EXPLAIN` evidence; acceptable unless benchmark misses; owner-approved follow-up only |
| R5-05 | Advisory lock serializes imports and admin writes | Write latency during large import commit | Existing 500-row chunks and 30 s timeout; operations guidance to avoid large import commits during bulk admin work |
| R5-06 | Duplicate active spell rows exist from earlier data | Manual spell merge is ambiguous | Deterministic lowest-ID target plus `duplicate_spell_target` warning; other duplicates untouched; no automatic cleanup |
| R5-07 | `LastInsertId()+i` assumes contiguous IDs under concurrency | Wrong IDs could be returned/audited | Shared helper under the global commit lock; integration test asserts returned IDs; no other writer runs concurrently |
| R5-08 | No create idempotency key | Duplicate stack on client retry | Documented; search + soft delete reconciliation; no false idempotency claim |
| R5-09 | Metadata changes after a row is created | Stale class/race/name on existing rows | Out of scope (AM-02 residual); creation reads current metadata; §16 records the gap |
| R5-10 | Audit JSON grows with large operations | Large audit rows/page payloads | API `createdCount` is always present and `createdIds` is capped at 10,000 entries (§5.4); audit `createdIds` is capped at 100 plus `createdIdCount` (§8.2); display truncation; no full request bodies |
| R5-11 | Lock wait stacks behind a slow import commit | Admin `503` under load | Timeout, clear `503`, retry guidance, metrics |
| R5-12 | Template regressions leak admin fields publicly | Information disclosure | Explicit DTO allowlists; §13 public-shape tests; separate admin templates |
| R5-13 | Derived sort keys exceed their columns (`NameSort VARCHAR(160)`, `LocationSort VARCHAR(80)`) because `NaturalSortKey` zero-pads every digit run | Insert/update fails with a data-too-long error surfaced as `500` | Write paths persist through `TruncateRunes` at exactly the import bounds (160/80) in create (§6.3), update (§6.4), and backfill (§4.3); unit tests cover a digit-heavy 128-rune name and a max-length raw location |
| R5-14 | Admin edit sets a quantity that violates the old item constraint | MySQL check-constraint error surfaced as `500` | **Closed 2026-11-22 by QTY-08:** `0005_item_quantity_model.sql` drops `ck_gbi_item_one`; the service validates `1..100000` for items, allows quantity edits for both types, and writes the `quantity` audit pair. Service tests cover the edit and the cap rejection |
| R5-15 | A date override is outside MariaDB's `DATE` range or violates `ck_gbi_open`/`ck_gbi_bazaar` | Insert/update fails and is surfaced as `500` | `OptionalDate` accepts only `YYYY-MM-DD`, the service range-checks `1000-01-01..9999-12-31` and enforces `DateAdded <= DateOpen <= DateBazaar` before any SQL; §13.1/§13.2 cover the errors and the no-write guarantee |
| R5-16 | `Settings` can hold `GuildOpenDays > BazaarDays`, and import's `dateFields` never checks ordering, so a default-derived create would be inverted and legacy imported rows may already carry `DateOpen > DateBazaar` | Admin create fails confusingly, or a row cannot be edited at all | A fully derived inverted window returns `503` (fix `Settings`), not `422`; an explicit override is `422` on the submitted field; the ordering check runs on effective values, so an inverted legacy row is editable only when both dates are corrected in the same request. Rewriting existing dates is out of scope because a `Settings` change must stay non-retroactive (AC5-19) |

---

## 16. Unresolved dispositions

These four items were **accepted as out of scope for Phase 5 by the owner on 2026-11-22** (approval log, §2.4). Phase 5 does not choose behavior for them and must not be read as resolving them. Each section keeps the original analysis so the gap stays documented; if the owner later wants any of them closed as part of Phase 5, that is a scope change requiring a new decision and a revised plan.

### 16.1 PD-05 — MariaDB runtime bootstrap

**Status:** OPEN — **accepted as out of scope for Phase 5 (owner, 2026-11-22)**. The repository records PD-05 as an approved *temporary* bootstrap: `GUILDBANK_MARIADB_*` configures the runtime pool even though the wizard persists settings.

**Impact on Phase 5:** Admin inventory always uses the configured runtime pool. If settings and environment diverge, the admin screen reads and writes the environment-configured database. Phase 5 adds no pool hot-swap, no settings-based reconnect and no warning banner beyond the existing configuration behavior. Operators must change the environment and restart, as today.

**What would close it:** An approved decision making `Settings` authoritative for the runtime pool, including migration of existing deployments and a tested reconnect path. That is a separate change.

### 16.2 PD-18 — Settings are persisted but not authoritative

**Status:** OPEN — **accepted as out of scope for Phase 5 (owner, 2026-11-22)**. Closely related to PD-05; the wizard writes settings, the pool uses the environment.

**Impact on Phase 5:** Same as PD-05. The admin UI cannot repair a wrong runtime connection by editing settings. No new code path depends on settings-backed MariaDB connection values. Date offsets are read through the existing settings service, which is already the source for import and public behavior.

**What would close it:** The PD-18 decision described in `Docs/ProjectStatus.md` §"remaining work", including hot-swap semantics, failure behavior and rollback.

### 16.3 E-02 — Offline recovery and data repair

**Status:** OPEN — **accepted as out of scope for Phase 5 (owner, 2026-11-22)**. The setup path is implemented and fails closed, but the repository contains no approved offline recovery command for unreadable `Users`, disabled administrators, or corrupted local data.

**Impact on Phase 5:** Phase 5 adds no recovery command, no forced admin reset and no direct SQLite repair UI. If the only administrator cannot log in, the admin inventory routes are unreachable exactly as they are today; restoring a backup is the only documented path. Phase 5 also adds no data-repair tool for corrupted guild inventory; the backfill repairs only null derived location columns.

**What would close it:** An approved, documented offline command or procedure with its own security review and tests.

### 16.4 AM-02 — Missing-metadata representation versus refresh

**Status:** PARTIAL / OPEN residual — **the residual is accepted as out of scope for Phase 5 (owner, 2026-11-22)**. The representation rule is implemented (preserve `EQUID`, set `MetadataMissingFg`, blank class/race, no sentinel). There is no approved disposition for revalidating or repairing rows after metadata becomes available.

**Impact on Phase 5:** Manual creation checks metadata at write time and sets the flag correctly. Existing rows with `MetadataMissingFg=1` are not rechecked, renamed, backfilled or linked to newly available metadata. The admin UI shows the missing flag and permits manual edit of location/recipient, plus quantity for spell rows only; it does not offer a metadata refresh action. A reconciled item therefore stays missing until a separate process updates it.

**What would close it:** An approved refresh/reconciliation design (trigger, schedule, ownership, audit and conflict rules). None exists in the repository; PD5-30 leaves it open, and PD5-31 does not address it.

### 16.5 Effect on approval

The owner approved this plan **with these four items accepted as out of scope** on 2026-11-22. That approval covers Phase 5 as specified above; it does not approve a bootstrap change, a recovery tool, or a metadata reconciliation job, and nothing in Phase 5 may quietly implement one. If the owner later requires any of them, implementation pauses for a revised scope rather than adding them implicitly.

### 16.6 Residual confirmations for the approved lifecycle date rule

The owner approved the rule on 2026-11-22 (PD5-27 amended, PD5-32 added). Three consequences were not stated in the decision and are recorded here as the **operative sub-rules, confirmed with the reconciliation approval of 2026-11-22**. They do not change the approved rule; they fix how it is applied.

1. **`null` resets use the current offsets.** The offsets that produced a row are not stored, so "reset to default" means `stored DateAdded + the offset in Settings at edit time`. A policy change followed by a reset therefore adopts the new policy for that row only. The alternative — storing the original offsets in a new column — is a schema change and is not planned.
2. **A date override is rejected when a spell create merges.** `Design.md` states that increasing a spell quantity never changes its dates, so an override supplied with a merge has nowhere to go. The plan rejects it with `422` instead of silently ignoring it. The alternatives are to apply the override to the merge target (which would make a quantity merge change dates, contradicting `Design.md`) or to ignore it with a warning (which presents a control that does nothing).
3. **An inverted settings window is a `503`, and legacy inverted rows need a date correction to be edited.** `GuildOpenDays` and `BazaarDays` are independently bounded at `0..3650`, so settings can describe `DateOpen > DateBazaar`; import never checked that. Phase 5 therefore fails a fully derived inverted create with `503` (an operator fix), attributes an inverted explicit override to the submitted field with `422`, and checks ordering on effective values, which means a legacy row with an inverted window can only be edited once its dates are corrected. The alternative — exempting defaults from the E-10 rule — would write windows the design forbids, and the alternative of silently rewriting legacy rows would break the approved non-retroactivity rule.

If any of them is decided differently, only PD5-32, §5.3, §6.3 and the §13 cases change; no other section depends on them.