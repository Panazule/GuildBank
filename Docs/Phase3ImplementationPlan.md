# Phase 3 Implementation Plan — Import Batches, Lists, Normalization, Selection and Commit

**Status:** Approved 2026-11-21 (PD3-01–PD3-14 and all six §14.1 defaults) and implemented. The authenticated Compose workflow passed on 2026-11-22 UTC; see `ProjectStatus.md` §1.5 and §1.5.1 for the implementation and validation record.
**Date:** 2026-11-21
**Scope owner:** single developer
**Implementation-plan mapping:** this is the user's “Phase 3” (import pipeline). It corresponds to `Docs/ImplementationPlan.md` Phase 6, and it depends on the Phase 1 foundation (settings, SQLite, authentication) and the Phase 3 metadata service (`ItemMetadataService`). The phase numbering is called out so no one mistakes this document for the earlier Phase 3 dependency.

**Normative inputs**

- `Design.md` L266–L374 (GuildBankItems / ImportBatch / ImportList), L699–L728 (natural sorting), L969–L1045 (REST).
- `Docs/DatabaseDesign.md` §4.2–§4.5 (guild store schema), §5–§7 (relationships, integrity, transactions), §8 (migrations) and §9 (query design; §9.5 is the natural sort key).
- `Docs/ImplementationPlan.md` Phase 6 and Appendix B rules MR-01–MR-20.
- `Docs/ItemsDataProfile.md` and `Docs/Samples/InventoryExport-Sample.txt` for field-profile evidence.

Where this plan originally proposed something not yet in a normative document, it was marked **PROPOSED** and listed in §2 for explicit approval. The owner approved all §2 decisions and the six §14.1 defaults on 2026-11-21; the implementation is complete. Everything marked **SPEC** already exists in the documents above.

---

## 1. Outcome and acceptance criteria

When Phase 3 is complete an administrator can:

1. Upload a tab-delimited `InventoryExport.txt` from `/admin/imports`.
2. See a durable batch record even when parsing fails, including a clear line-numbered reason.
3. Review all accepted staged rows grouped by normalized location, in natural location order.
4. Select/deselect individual rows or whole locations; selections are persisted per row and survive a server restart.
5. See a summary that distinguishes **staged rows**, **inventory units** and **resulting inventory records**, and reports spells that will merge rather than insert.
6. Commit the selection atomically to MariaDB:
   - items expand one inventory row per unit;
   - spells merge quantity into an existing active spell when one exists, otherwise insert one row;
   - metadata comes from `Items` when present, with imported name and `MetadataMissingFg=1` as the fallback;
   - audit summary rows for the batch, the created-record summary and each merged spell are written in the same MariaDB transaction (PD3-14; not one audit row per expanded unit).
7. Re-open a completed batch as read-only; re-commit returns `409`.
8. Use the same operations through the documented REST endpoints.

Acceptance is verified by the test matrix in §12, including a golden fixture derived from the supplied sample export.

### 1.1 Non-goals

- No inventory list/edit/delete UI (later phase). Phase 3 only writes `GuildBankItems`.
- No import undo. Batches are history; committed inventory is changed only by a future inventory feature.
- No scheduler, watching a folder, drag-and-drop folders, ZIP support, or non-`InventoryExport` formats.
- No new external runtime dependency. The UI uses server-rendered Go templates plus vanilla `fetch`, matching `setup.js`; HTMX is not currently vendored.
- No automatic creation or migration of the external `Items` table (SR-13).
---

## 2. Approved decisions

The owner approved PD3-01 through PD3-14 on 2026-11-21. PD3-15 and PD3-16 were
approved as follow-ups on 2026-11-22 (IMP-C and DEFECT-01); PD3-07's spell clause
is superseded by PD3-16. The defaults below are the approved implementation
contract, not pending proposals.

| ID | Decision | Default / rationale | Impact if changed |
|---|---|---|---|
| PD3-01 | Location fallback for unrecognized patterns | SPEC/D-05 stays authoritative. Equipment sections (`Equipment`, `Possessions`, `KeyRing`, …), blanks and handout/ground rows are not bank slots. They normalize to `Unspecified`; blank locations also add warning `location_blank`. Reserved prefixes (`Bank`, `SharedBank`, `General`, `Hoard`) that fail to parse normalize to `Unspecified` and add `location_pattern_invalid`. | A per-section fallback such as `Equipment` would require a D-05 amendment and a different normalization table. |
| PD3-02 | Where row warnings live | `ImportList.WarningText` stores a compact JSON array of `ImportWarning` values (`code`, optional `field`/`equid`/`mask`/`highBits`, `detail`). `GuildBankItems` keeps only the normative `MetadataMissingFg`, not warning JSON. | A separate warning table is more normalized but not in the schema. |
| PD3-03 | Spell merge target | Only existing rows with `DeletedFg=0` are merge targets (DD §7.5 and D-06). Soft-deleted spells are left untouched and a new active row is inserted. | Merging into deleted rows requires a restore policy that does not exist. |
| PD3-04 | Spell merge multiplicity and concurrency | Staged rows are aggregated by `EQUID` before write. If more than one active spell row matches (data corruption), merge into the lowest `GuildBankItemID` and report the anomaly. All commits serialize on a MariaDB advisory lock so the “no duplicate active spell” invariant survives concurrent batches. | Opening the lock widens the race window; row locks alone cannot protect a non-existent-row gap reliably across all MariaDB versions. |
| PD3-05 | What “counts” mean | `ImportBatch.SourceRowCount` counts physical lines, including blank and header lines, so it reconciles to the file. `AcceptedRowCount` is staged rows. Summary separately reports staged rows, item units, spell quantity and resulting records. **Amended 2026-11-22 by QTY-04:** `InventoryRecordsToCreate` and `TotalRecordsToImport` count item rows (`ItemRows`), not item units; `ItemUnits` stays informational. | Counting only data rows makes discarded rows invisible. |
| PD3-06 | Limits | Body 10 MiB; 50,000 physical lines; 64 KiB per line; 100,000 inserted inventory rows; 10,000 `Count` per row; field lengths per schema. Parser violations are parse failures with code `limit_exceeded`; the HTTP body cap returns `413 file_too_large` before parsing, and the commit row guard returns `422 validation_failed`. **Amended 2026-11-22 by QTY-04:** the guard counts committed records (one item row per line), not expanded units. | Limits are implementation guardrails, not spec constants; they can be raised before implementation. |
| PD3-07 | Canonical item name and missing-metadata flag | For every staged row, set `MetadataMissingFg=1` when the EQUID is absent from `Items`. For both `ItemType='Item'` and `ItemType='Spell'`, use `Items.name` and the decoded `ClassString`/`RaceString` when the EQUID is present; when absent use the imported name and blank class/race. The original imported name always remains on `ImportList`. **The spell clause was amended by PD3-16 on 2026-11-22; the original text kept the imported name and blank class/race for spells and caused DEFECT-01.** | Using the imported name for items always would preserve user formatting but diverge from D-01 canonical names; skipping the flag for spells would violate AM-02. |
| PD3-08 | Selection and upload audit | Upload and commit are audited. Selection changes are operational staging state and are not audited, because an audit row per checkbox would swamp the log; the selected set remains recoverable from `ImportList.SelectedFg` and the commit audit records the resulting counts. | Auditing selection requires a revision column and event-per-PUT design. |
| PD3-09 | UI transport | Vanilla `imports.js` + JSON endpoints, following `setup.js`. No HTMX vendor file. | HTMX would add a new vendored JS file and HTML fragment endpoints; the service/API contract is unchanged. |
| PD3-10 | Migration failure behavior | If MariaDB opens but migration fails, startup is fatal (exit non-zero) rather than serving a partially migrated guild store. If MariaDB cannot be opened while unconfigured, the existing degraded mode remains. | Continuing after a failed migration risks committing against a wrong schema. |
| PD3-11 | Import grid sort columns | Phase 3 writes `ImportList.LocationSort` (DD-approved, DC-13) and adds **APPROVED** `ImportList.NameSort VARCHAR(160) NULL` so the import grid and location drill-down obey the natural-sort requirement for names as well as locations. Add `ix_import_batch_sort (ImportBatchID, LocationGroup, LocationSort, NameSort, ImportID)`. | Without `NameSort`, names sort by collation; location order still meets the explicit examples. |
| PD3-12 | Sample counts | The supplied sample is a golden test with 548 physical lines, 197 accepted rows, 348 discarded data rows, 2 structural rows (header + one section row), 1 blank line, 30 staging location groups (29 reviewable after PD3-15), 194 item rows → 723 item units, and 3 distinct spell rows → 3 spell quantity. These are asserted, not treated as production invariants. **Amended 2026-11-22 by QTY-04:** the 163 reviewable item rows create 163 inventory records (not 723 expanded records); 668 reviewable units remain informational; with 3 new spells the total is 166 records. | Updating the fixture requires updating the golden test. |
| PD3-13 | Timeouts | HTTP `ReadTimeout`/`WriteTimeout` become 120s and the guild-store DSN `ReadTimeout`/`WriteTimeout` become 120s so a 10 MiB upload or a 100,000-row commit is not cut off by the current 30s limits. `ReadHeaderTimeout` stays 10s and `IdleTimeout` stays 60s. | Slower clients can hold connections longer; the body, row and expansion limits remain the real guardrails. |
| PD3-14 | Import audit granularity | Follow the D-10 allowlist and the taxonomy's "one row per logical operation": upload writes `IMPORT_CREATED`; commit writes `IMPORT_COMMITTED` plus one `ITEM_CREATED` summary (`RecordID=ImportBatchID`) and one `ITEM_UPDATED` per merged spell row for provenance. Do not write one audit row per expanded item unit. **Amended 2026-11-22 by QTY-05:** the summary keeps `itemUnits` and adds `itemRecords`; `TotalRecordsAffected` counts records. | Per-row item audit gives forensic detail but creates up to 100,000 audit rows per commit, contradicts the taxonomy, and bloats the append-only table. |
| PD3-15 | Internal `Unspecified` group (IMP-C, 2026-11-22) | The `Unspecified` classification stays in `ImportList.LocationGroup` and still counts toward `AcceptedRowCount`, but the import review never lists it, never offers it as scope, never returns it from `rows`/`row-ids`/`selection`, rejects selecting one (`422`), and never commits one. `stagedRows`/`totalLocations` report the reviewable set. Admin inventory keeps its Other label. | Hiding staged rows from the review means an administrator cannot see or recover those rows through the import workflow; they remain in the staging table for forensics. |
| PD3-16 | Spell metadata parity (DEFECT-01, 2026-11-22) | A spell row is created from the same resolved `ItemMetadata` rule as an item row: authoritative `Items.name` and decoded `ClassString`/`RaceString`; imported name and blanks only when the EQUID is unresolved. Existing rows written under PD3-07's old spell clause are repaired once by the startup `BackfillSpellMetadata`; merges still never rewrite stored metadata or dates. | Reverting to blank spell metadata would restore the `Unknown` display defect; backfilling on every start would add startup writes for rows that should already be correct. |
---

## 3. Current-state facts the implementation builds on

- Guild-store schema was documented but not implemented when this plan was written. It is now implemented by embedded migration `0001_guild_store.sql`; `ImportBatch`, `ImportList`, `GuildBankItems`, `Audit` and `schema_migrations` exist in the Compose guild store.
- `internal/platform/app/app.go` creates a MariaDB `GuildStore` only when guild database settings exist, keeps `guildErr`, and exposes it as unavailable instead of failing startup.
- `internal/api/router.go` groups `/api/v1` with `RequireAuth` (and `RequireAdmin` for settings), applies CSRF middleware to unsafe methods, and `errors.go` maps `domain.ValidationError`, `ErrConflict`, `ErrNotFound`, `ErrUnavailable`, etc.
- `internal/service/item_metadata.go` resolves canonical item metadata and mask warnings. It does not persist warnings; the import service owns that sink.
- Templates are `html/template` files embedded from `internal/web/templates`; pages render through `views.go`; CSS is one custom stylesheet; JS is plain ES5-compatible `setup.js`.
- `GuildBankItems` fields, constraints and indexes are specified in `Docs/DatabaseDesign.md` §4.2. `ImportBatch`/`ImportList` are specified in §4.3/§4.4.

### 3.1 Field profile of the sample export

| Observation | Consequence |
|---|---|
| Columns are `Location`, `Name`, `ID`, `Count`, `Slots` (tab-delimited). | Parser maps by position; it does not trust a single fixed field count. |
| Most rows have 5 fields; some have 3 or 4; trailing empty fields collapse under PowerShell `-split` but not under a byte-level TSV reader. | Split on tabs preserving empties. A 4-field row has a slot marker in field 4 (not a count), so `Quantity=1`; a 3-field row also means `Quantity=1`. |
| Header `Location Name ID Count Slots`; section rows such as `Bank 1<TAB>Name<TAB>ID`; one blank line near the end (line 539), not the final newline. | Skip exactly one canonical header at first non-blank; skip section rows that have `Name` in field 2 and a field 3 that is not a positive integer; blank lines are not data. |
| Rows with empty name and/or `ID=0` exist (348 in the sample). | Discard, do not fail the batch. |
| Locations include `General 1…N`, `Bank 1…N`, `SharedBank 1…N`, `Hoard…`, `General 1-Slot1`, `Bank 1-Slot1`, and non-bank sections (`Equipment`, `Possessions`, spell-family names). | Normalizer strips `-SlotN`; collapses `Hoard<n>`; unknown/equipment/blank → `Unspecified` per PD3-01. |
| Names start with `Spell:`, `Song:` and `Ancient:`; item names can also contain “Ancient” without a colon. | Classifier uses a case-insensitive prefix followed by `:` only. `Ancient: …` is a spell. |
| Quantities are positive integers; item rows are mostly 1 but can be larger; spell rows are spells/songs with quantity. | Items expand; spells aggregate. `Quantity=0`/non-numeric is a fatal parse error. |
| Duplicate IDs occur within locations and across locations. | Staging accepts duplicates; only spells merge at commit. Items remain separate instances. |
| File may contain CRLF, a UTF-8 BOM, and non-ASCII names. | Strip BOM on the first line, trim `\r`, validate UTF-8, preserve names. |
---

## 4. Data model

### 4.1 Domain structs (`internal/domain/import.go`)

```go
type ImportStatus string

// Actor is the authenticated administrator snapshot used for import writes.
// It is built from the session user and deliberately excludes password data.
type Actor struct {
    UserID   int64
    UserName string
    IsAdmin  bool
}

const (
    ImportStatusProcessing ImportStatus = "Processing"
    ImportStatusUploaded   ImportStatus = "Uploaded"
    ImportStatusFailed     ImportStatus = "Failed"
    ImportStatusCompleted  ImportStatus = "Completed"
)

type ImportBatch struct {
    ImportBatchID    int64
    FileName         string
    FileSha256       *string // nil while parsing or when the hash was never computed
    SourceRowCount   *int    // physical lines; nil before parsing completes
    AcceptedRowCount *int    // staged rows; nil before parsing completes
    Status           ImportStatus
    FailureReason    *string
    ImportedBy       string
    ImportedByUserID *int64
    ImportedAt       time.Time
}

type ImportLocation struct {
    LocationGroup string
    LocationSort  string
    RowCount      int
    SelectedCount int
}

type ImportRow struct {
    ImportID      int64
    ImportBatchID int64
    SourceLine    int // 0 for a legacy row with no stored source line
    ItemType      ItemType
    EQUID         uint64
    Name          string
    Quantity      uint32
    LocationRaw   string
    LocationGroup string
    LocationSort  string
    NameSort      string // APPROVED column; always written by Phase 3
    SelectedFg    bool
    Warnings      []ImportWarning
}

type ImportWarning struct {
    Code     string `json:"code"`
    Field    string `json:"field,omitempty"`
    EQUID    uint64 `json:"equid,omitempty"`
    Mask     uint64 `json:"mask,omitempty"`
    HighBits uint64 `json:"highBits,omitempty"`
    Detail   string `json:"detail,omitempty"`
}

// ImportRowFilter and ImportRowPage belong to the domain so the MariaDB
// implementation of ImportStore never imports the service package.
type ImportRowFilter struct {
    LocationGroup string
    Selected      string // "all", "selected" or "unselected"
    Search        string
    Page          int
    PageSize      int
}

type ImportRowPage struct {
    Rows     []ImportRow
    Total    int
    Page     int
    PageSize int
}

// ImportTotals aggregates the persisted selection. ItemUnits and SpellQuantity
// are sums, so they may exceed the per-row Quantity width.
type ImportTotals struct {
    SelectedRows   int
    ItemRows       int
    ItemUnits      uint64
    SpellRows      int
    SpellQuantity  uint64
    DistinctSpells int
    WarningRows    int // selected rows with at least one stored warning
}

// ImportSummary is the unit-aware GET /summary payload. It is composed from
// ImportTotals, ListLocations, SpellPreview and the batch, not persisted.
type ImportSummary struct {
    BatchID                  int64
    Status                   ImportStatus
    SelectedRows             int
    StagedRows               int
    SelectedLocations        int
    TotalLocations           int
    ItemRows                 int
    ItemUnits                uint64
    SpellRows                int
    SpellQuantity            uint64
    DistinctSpells           int
    NewSpells                int
    MergedSpells             int
    InventoryRecordsToCreate uint64
    WarningRows              int
    TotalRecordsToImport     uint64
}

// SpellPreview is one selected spell EQUID and its current inventory target.
type SpellPreview struct {
    EQUID          uint64
    StagedRows     int
    StagedQuantity uint64
    ActiveRecords  int
    ActiveQuantity uint64
    MergeTargetID  *int64 // lowest active GuildBankItemID; nil when no active row exists
}

// UploadResult is returned by the upload service. DuplicateOf is a previous
// Uploaded/Completed batch with the same SHA-256, or nil.
type UploadResult struct {
    Batch          ImportBatch
    StructuralRows int
    DiscardedRows  int
    BlankRows      int
    DuplicateOf    *int64
    WarningCounts  map[string]int
}

// ImportFailure attaches the durable batch ID to a parse/staging failure.
type ImportFailure struct {
    BatchID int64
    Err     error
}

func (f *ImportFailure) Error() string { return f.Err.Error() }
func (f *ImportFailure) Unwrap() error { return f.Err }

// DuplicateSpellTarget records a corruption anomaly: merged into the lowest ID.
type DuplicateSpellTarget struct {
    EQUID     uint64 `json:"equid"`
    MergedInto int64 `json:"mergedInto"`
}

// CommitResult is the §7.4 commit response body.
type CommitResult struct {
    BatchID                int64
    Status                 ImportStatus
    ItemRecordsCreated     int
    SpellRecordsCreated    int
    SpellRecordsMerged     int
    SpellQuantityAdded     uint64
    TotalRecordsAffected   int
    DuplicateSpellTargets  []DuplicateSpellTarget
    Warnings               []ImportWarning
    DurationMS             int64
}

// GuildBankItem is the import-owned projection of one GuildBankItems row.
// Dates are calendar dates at 00:00 UTC; the repository writes them as DATE.
type GuildBankItem struct {
    GuildBankItemID   int64
    ItemType          ItemType
    EQUID             uint64
    MetadataMissingFg bool
    Name              string
    ClassString       string
    RaceString        string
    DateAdded         time.Time
    DateOpen          time.Time
    DateBazaar        time.Time
    Quantity          uint32
    SourceLocation    string // nullable in the table; empty is written as NULL
    GivenTo           string // nullable in the table; empty is written as NULL
    DeletedFg         bool
    ModAt             time.Time
    ModBy             string
    ModByUserID       *int64
    NameSort          string
}

// AuditEntry maps the D-10 Audit columns. OldValues/NewValues are encoded JSON
// objects. SourceIP is optional and only populated when the handler supplies it.
type AuditEntry struct {
    EventDate     time.Time
    UserID        int64
    UserName      string
    EventType     string
    TableName     string
    RecordID      string
    OldValues     json.RawMessage
    NewValues     json.RawMessage
    Notes         string
    CorrelationID string
    SourceIP      string
}
```

`domain.ItemType` and its `ItemTypeItem`/`ItemTypeSpell` constants do not exist yet: add them to `internal/domain/item.go` in step 1, next to `Item`. Do not introduce a second item-type enum. `AuditEntry` is import-scoped for this phase; the later audit phase may promote it unchanged.

### 4.2 MariaDB migration `0001_guild_store.sql`

The migration runner creates `schema_migrations` itself, takes `GET_LOCK(CONCAT('guildbank:migrate:', LEFT(SHA2(DATABASE(), 256), 32)), 30)`, applies each embedded file in lexical order once, records the version after all statements succeed, and releases the lock. Hashing the database name keeps the lock name inside MariaDB's 64-character limit while remaining database-specific. MariaDB DDL is not transactional; the runner records a version only after the whole file succeeds, and a re-run is safe because every statement is `CREATE TABLE IF NOT EXISTS` and guarded by the version check.

```sql
-- 0001_guild_store.sql
-- Import history and normalized staging rows. Column and constraint names
-- follow Docs/DatabaseDesign.md §4.3 and §4.4.

CREATE TABLE IF NOT EXISTS ImportBatch (
    ImportBatchID  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    FileName       VARCHAR(255) NOT NULL,              -- SPEC
    ImportedBy     VARCHAR(64)  NOT NULL,              -- SPEC (username snapshot, DC-07)
    ImportedAt     DATETIME(6)  NOT NULL,              -- SPEC (UTC)
    Status         ENUM('Uploaded','Processing','Completed','Failed')
                                NOT NULL DEFAULT 'Uploaded',  -- SPEC
    ImportedByUserID INT NULL,                          -- APPROVED
    FailureReason  TEXT NULL,                           -- APPROVED (MR-12)
    SourceRowCount INT UNSIGNED NULL,                   -- APPROVED
    AcceptedRowCount INT UNSIGNED NULL,                 -- APPROVED
    FileSha256     CHAR(64) NULL,                       -- APPROVED (duplicate detection)
    PRIMARY KEY (ImportBatchID),
    KEY ix_batch_status (Status, ImportedAt),
    KEY ix_batch_sha (FileSha256),
    CONSTRAINT ck_batch_status CHECK (Status IN ('Uploaded','Processing','Completed','Failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS ImportList (
    ImportID       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ImportBatchID  BIGINT UNSIGNED NOT NULL,            -- SPEC
    EQUID          INT UNSIGNED NOT NULL,               -- SPEC
    Name           VARCHAR(128) NOT NULL,               -- SPEC
    Quantity       INT UNSIGNED NOT NULL DEFAULT 1,     -- SPEC
    LocationRaw    VARCHAR(128) NOT NULL,               -- SPEC
    LocationGroup  VARCHAR(64)  NOT NULL,               -- SPEC (normalized)
    SelectedFg     TINYINT(1) NOT NULL DEFAULT 1,       -- SPEC
    ItemType       ENUM('Item','Spell') NOT NULL,       -- APPROVED (DC-18)
    WarningText    TEXT NULL,                           -- APPROVED (PD3-02, D-08)
    SourceLine     INT UNSIGNED NULL,                   -- APPROVED (troubleshooting)
    LocationSort   VARCHAR(80) NULL,                    -- APPROVED (DC-13)
    NameSort       VARCHAR(160) NULL,                   -- APPROVED (PD3-11)
    PRIMARY KEY (ImportID),
    KEY ix_import_batch_selected (ImportBatchID, SelectedFg),
    KEY ix_import_batch_location (ImportBatchID, LocationGroup),
    KEY ix_import_batch_equid    (ImportBatchID, EQUID),
    KEY ix_import_batch_sort     (ImportBatchID, LocationGroup, LocationSort, NameSort, ImportID), -- PD3-11
    CONSTRAINT fk_import_batch FOREIGN KEY (ImportBatchID)
        REFERENCES ImportBatch (ImportBatchID)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT ck_import_quantity CHECK (Quantity >= 1),
    CONSTRAINT ck_import_selected CHECK (SelectedFg IN (0,1)),
    CONSTRAINT ck_import_location CHECK (LocationGroup <> '')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

`GuildBankItems` DDL is copied verbatim from `Docs/DatabaseDesign.md` §4.2, including the approved `NameSort`, `MetadataMissingFg` and `ModByUserID` columns and all named checks/indexes. The `ImportList.NameSort` column and `ix_import_batch_sort` index were added to `DatabaseDesign.md` when PD3-11 was approved. `ix_import_batch_sort` is ordered `(ImportBatchID, LocationGroup, LocationSort, NameSort, ImportID)` so the location-filtered review query both filters and orders by index; it subsumes the shorter `ix_import_batch_location`, which remains in the baseline DDL for compatibility. `SourceRowCount`/`AcceptedRowCount` are nullable although Phase 3 always writes them; the app enforces `AcceptedRowCount <= SourceRowCount` because the DD table has no such CHECK.

### 4.3 `Audit`

`0001_guild_store.sql` creates `Audit` exactly as designed in `Docs/DatabaseDesign.md` §4.5, including `CorrelationID` and `SourceIP` and its indexes. Audit rows are appended inside the import transaction; there is no second audit-only transaction. `AuditOutbox` is a SQLite table and remains deferred with the audit phase, so no outbox row is written for guild-store imports. If Phase 3 audit is deliberately deferred, commit must stay disabled — it must never silently skip audit (DC-16). **Recommendation:** include `Audit` in this migration, because the commit/audit pairing is an acceptance criterion.

### 4.4 Status transitions

| From | Event | To | Guard |
|---|---|---|---|
| — | upload accepted | `Processing` | batch row inserted with `Status=Processing`; no audit row yet because the upload is not complete |
| `Processing` | staging succeeds | `Uploaded` | staged rows + counts/hash + upload-success audit in one transaction |
| `Processing` | parse/staging failure or crash | `Failed` | `FailureReason` set by guarded `FailBatch`; the recovery sweep only touches `Processing` |
| `Uploaded` | commit starts | `Processing` (uncommitted) | commit advisory lock + `SELECT ... FOR UPDATE`; the status update is inside the commit transaction |
| `Processing` | commit succeeds | `Completed` | inventory + audit + status in the same transaction |
| `Processing` | commit rolls back | `Uploaded` (visible again) | the `Processing` update rolls back with the transaction; retry allowed |
| `Failed` / `Completed` | any mutation | unchanged | `409 batch_not_editable` |

The commit-time `Processing` value is never committed unless the whole commit succeeds, so rollback is not a durable `Processing → Uploaded` transition. `Uploaded` is the only editable/committable state. Reads of `Completed` and `Failed` batches are allowed for history.

---

## 5. Parsing, normalization and classification

### 5.1 Parser contract (`internal/service/import_parser.go`)

```go
type ParsedRow struct {
    SourceLine int
    LocationRaw string
    Name        string
    EQUID       uint64
    Quantity    uint32
}

type ParseResult struct {
    Rows           []ParsedRow
    SourceLines    int // physical lines, including blank and structural rows
    StructuralRows int // canonical header plus section rows
    DiscardedRows  int
    IgnoredRows    int // nested-slot rows ignored by IMP-01
    BlankRows      int // empty lines; a trailing newline is not an extra line
}

type ParseError struct {
    Line    int
    Code    string
    Message string
}

func ParseInventoryExport(r io.Reader) (ParseResult, error)
```

`EQUID` is `uint64` to match `domain.Item`/`domain.ItemReference`; the parser rejects values above `math.MaxUint32` with `invalid_id` because the DB column is `INT UNSIGNED`. Quantity remains `uint32` because `Count` is capped at 10,000.

The parser is pure and has no database, clock or logger dependency. The upload service generates the hash from the raw bytes before parsing and stores the file name.

### 5.2 Line algorithm

Read with a line reader that returns the last non-terminated line and does not emit an extra empty line for a trailing newline; the sample is 548 lines, not 549. For each physical line, 1-based:

1. Remove a UTF-8 BOM on line 1 and a single trailing `\r`. Reject invalid UTF-8 with `invalid_utf8` (line).
2. Increment `SourceLines`. If the line is blank after trimming spaces/tabs, increment `BlankRows` and continue.
3. Enforce 64 KiB and 50,000-line limits (`limit_exceeded`).
4. Split on `\t` with `strings.Split`, **not** `Fields`, so interior empty fields are preserved. Drop only fields that are empty and at the end of the slice before field-count validation; a trailing tab is formatting, not data.
5. Trim each field of surrounding whitespace; preserve internal characters and case.
6. If this is the first non-blank line, require exactly the five canonical labels `Location`, `Name`, `ID`, `Count`, `Slots` (case-sensitive after trimming). Otherwise fail with `invalid_header`. Mark the header seen and increment `StructuralRows`. A later exact header is `duplicate_header`.
7. If the fields are the exact canonical header again, fail `duplicate_header`. Otherwise, a structural section row is `len(fields) >= 3`, `fields[1] == "Name"`, and `fields[2]` is not a positive integer. Increment `StructuralRows` and skip it. This tolerates `Bank 1 / Name / ID`, `Equipment / Name / ID`, and spell-family section headers.
8. Fewer than 3 fields: fail `too_few_fields` (a non-header data row cannot identify an item).
9. More than 5 fields: fail `too_many_fields`.
10. Parse field 3 (ID). An empty, non-numeric or zero ID is a **discard** (count `DiscardedRows`), not a batch failure, because the export intentionally contains blank inventory rows. If it is all digits and nonzero, it must fit `uint32`; a numeric ID above `math.MaxUint32` is fatal `invalid_id`.
11. Parse field 2 (Name): an empty name is a **discard**. Enforce ≤128 Unicode code points; longer → fatal `field_too_long` (field `Name`).
12. Quantity:
    - 5 fields: field 4 (`Count`) must match `^[0-9]+$`, fit the limit and be `>=1`; field 5 (`Slots`) is ignored.
    - 4 fields: field 4 is a slot marker, not a count. Quantity is always 1; the value is ignored. (The sample's 4-field tail is a slot marker; a malformed non-numeric value must not silently become a quantity.)
    - 3 fields: `Quantity=1`.
    - `Quantity=0`, non-numeric, or `>10,000` in a 5-field row → fatal `invalid_quantity`.
13. Location field 1 may be empty; it becomes `""` and is handled by normalization. Enforce ≤128 Unicode code points; longer → fatal `field_too_long` (field `Location`).
14. **IMP-01 nested-slot rule:** if the location begins with a reserved bank
    prefix and ends with two or more slot suffixes
    (`(?i)\s*-\s*Slot\s*\d+(?:\s*-\s*Slot\s*\d+)+$`), the row is the contents of
    a container that occupies a bank slot. Increment `IgnoredRows` and skip the
    row; it is never staged, never normalized to `Unspecified` and never a batch
    failure. The check runs after the ID/name checks, so the canonical sample's
    139 empty nested lines (`ID = 0`) remain ordinary discards.
15. Append the row.

`SourceLines` therefore equals the file's physical line count (548 in the sample; a trailing newline does not add a line), while `len(Rows)` equals accepted rows. `StructuralRows` counts the canonical header plus section rows (2 in the sample), while `DiscardedRows` and `BlankRows` explain the rest; all three are copied into the upload result. `SourceRowCount = SourceLines`; `AcceptedRowCount = len(Rows)`. Discarded/structural/blank counts are not new schema columns.

### 5.3 Parse failures

The parser stops at the first fatal row and returns a typed `*ParseError`. It does not stage partial rows. The upload service records the line-numbered message in `FailureReason`:

```
line 412: invalid_quantity — Count must be a positive whole number (got "abc")
```

Codes: `file_empty`, `invalid_utf8`, `invalid_header`, `duplicate_header`, `too_few_fields`, `too_many_fields`, `invalid_id`, `invalid_quantity`, `field_too_long`, `limit_exceeded`. `invalid_id` is reserved for a numeric ID outside `uint32`; empty/zero IDs are discarded. After EOF, no non-blank line, only the header/structural/blank rows, or only discarded rows (empty/zero ID or empty name) is `file_empty`; a batch with nothing importable is not staged.

### 5.4 Normalization contract (`internal/service/import_normalize.go`)

```go
type NormalizedLocation struct {
    Group    string
    Sort     string
    Warnings []domain.ImportWarning
}

func NormalizeLocation(raw string) NormalizedLocation
```

Rules, in order:

1. Trim whitespace and normalize internal runs of whitespace to one space.
2. Strip a trailing slot suffix case-insensitively: `(?i)\s*-\s*Slot\s*\d+$`.
3. If the remainder is empty → `Group="Unspecified"`, warning `location_blank`.
4. If it starts with a bank prefix, parse the fully anchored patterns:
   - `(?i)^General\s*(\d+)$` → `General <n>`.
   - `(?i)^Bank\s*(\d+)$` → `Bank <n>`.
   - `(?i)^SharedBank\s*(\d+)$` → `SharedBank <n>`.
   - `(?i)^Hoard(?:\s*\d+)?$` → `Hoard` (all hoard variants collapse, AM-04).
   - `<n>` is the captured digits with leading zeros removed (keeping a single `0`), so `General 01` and `General 1` share one selector entry.
   - Any other value beginning with `General`, `Bank`, `SharedBank` or `Hoard` (case-insensitive) → `Unspecified`, warning `location_pattern_invalid`.
5. Any other section (for example `Equipment`, `Possessions`, `KeyRing`, `Handout`, `Ground`, spell-family names) → `Unspecified` without `location_pattern_invalid`.
6. `Sort = NaturalSortKey(Group)`.

**Approved review clarification (2026-11-21):** `LocationGroup` is the
authoritative source for import filtering and selection. The selector is
generated from distinct `LocationGroup` values, and every `Hoard<n>` variant
normalizes to the single `Hoard` option. `LocationRaw` is preserved only for
traceability and auditing; it is never used as a filter key.

The normalizer returns a canonical group, never the raw section, so `ImportList.LocationGroup` cannot become an unbounded enumeration. `LocationRaw` keeps the input for audit/display. If the resulting group exceeds 64 code points (possible only for an over-long numeric suffix), use `Unspecified` with `location_pattern_invalid` rather than truncating; this keeps the selector stable and satisfies the `VARCHAR(64)` column. `LocationSort` is truncated to 80 characters and `NameSort` to 160 characters at a rune boundary before staging; this keeps each value inside its column even when a long name produces a long sort key.

IMP-01 removes nested-slot rows in the parser before this function sees them. `NormalizeLocation` continues to strip only one trailing slot suffix; an administrator who types a nested value into `SourceLocation` gets `Unspecified` with `location_unrecognized`, which is the intended admin-facing signal rather than a silent re-grouping.

### 5.5 Classification (`internal/service/import_classify.go`)

```go
func ClassifyItem(name string) domain.ItemType
```

- Lowercase only for the prefix check.
- `Spell`, `Song`, `Ancient` followed immediately by `:` (allowing whitespace before the colon) → `domain.ItemTypeSpell`.
- Everything else, including `Ancient Burrower` (no colon), → `domain.ItemTypeItem`.
- The staged name is unchanged.

If two rows with the same `EQUID` classify differently, fail the upload with `classification_conflict` naming both source lines. One EQUID cannot be both an item and a spell; failing early prevents a split commit.

### 5.6 Natural sort (`internal/service/import_sort.go`)

Implement `NaturalSortKey` exactly as `Docs/DatabaseDesign.md` §9.5: Unicode casefold (use `strings.ToLower` plus the documented ASCII normalization for this dataset), collapse non-`[a-z0-9]` runs to one space, split runs, zero-pad numbers to 10 digits, join with `\x01`. Unit-test the table in that section plus `General 1`, `General 2`, `General 10`, `General 11`. This function is shared by import locations, staging `NameSort` and future inventory `NameSort`; every persisted key is truncated at a rune boundary (`LocationSort` 80, `NameSort` 160) before insert.

---

## 6. Service orchestration

### 6.1 Interfaces

```go
// internal/service/import_service.go
type ImportStore interface {
    CreateBatch(ctx context.Context, b domain.ImportBatch) (int64, error)
    FailBatch(ctx context.Context, batchID int64, reason *string, sourceRows *int, sha *string) error
    StageBatch(ctx context.Context, batchID int64, rows []domain.ImportRow, sourceRows int, sha string, audit domain.AuditEntry) error
    GetBatch(ctx context.Context, batchID int64) (domain.ImportBatch, error)
    ListBatches(ctx context.Context, limit int, beforeID int64) ([]domain.ImportBatch, error)
    ListLocations(ctx context.Context, batchID int64) ([]domain.ImportLocation, error)
    ListRows(ctx context.Context, batchID int64, filter domain.ImportRowFilter) (domain.ImportRowPage, error)
    RowIDs(ctx context.Context, batchID int64, locationGroup string) ([]int64, error)
    SelectedIDs(ctx context.Context, batchID int64) ([]int64, error)
    ReplaceSelection(ctx context.Context, batchID int64, ids []int64) error
    SelectedTotals(ctx context.Context, batchID int64) (domain.ImportTotals, error)
    SpellPreview(ctx context.Context, batchID int64) ([]domain.SpellPreview, error)
    BeginCommit(ctx context.Context, batchID int64) (ImportCommitTx, error)
    RecoverInterrupted(ctx context.Context) (int, error)
    Ready() bool
}

type ImportCommitTx interface {
    Batch() domain.ImportBatch // locked snapshot with Status='Uploaded'
    SelectedRows() ([]domain.ImportRow, error)
    ActiveSpells(equids []uint64) ([]domain.GuildBankItem, error)
    InsertItems(rows []domain.GuildBankItem) ([]int64, error)
    InsertSpells(rows []domain.GuildBankItem) ([]int64, error)
    AddSpellQuantity(id int64, qty uint32, at time.Time, actorName string, actorID int64) error
    ReplaceRowWarnings(warnings map[int64][]domain.ImportWarning) error
    WriteAudit(entries []domain.AuditEntry) error
    MarkCompleted() error
    Commit() error
    Rollback() error
}
```

`NewImportService(store ImportStore, metadata *ItemMetadataService, settings *SettingsService, now func() time.Time, logger *slog.Logger) *ImportService` wires the service. A nil `store` is allowed for degraded startup; `Ready()` then returns false. The handler converts the authenticated session user into `domain.Actor{UserID, UserName, IsAdmin: user.IsAdmin()}` and never passes the user record (and its password hash) into the import layer.

```go
// actor is the authenticated administrator from the session; it is never
// decoded from request JSON. correlationID/sourceIP come from the request
// context and are written to Audit; sourceIP may be empty when unavailable.
func (s *ImportService) Upload(ctx context.Context, actor domain.Actor, fileName string, body io.Reader, correlationID, sourceIP string) (domain.UploadResult, error)
func (s *ImportService) ListBatches(ctx context.Context, limit int, beforeID int64) ([]domain.ImportBatch, error)
func (s *ImportService) GetBatch(ctx context.Context, batchID int64) (domain.ImportBatch, error)
func (s *ImportService) Locations(ctx context.Context, batchID int64) ([]domain.ImportLocation, error)
func (s *ImportService) Rows(ctx context.Context, batchID int64, filter domain.ImportRowFilter) (domain.ImportRowPage, error)
func (s *ImportService) RowIDs(ctx context.Context, batchID int64, locationGroup string) ([]int64, error)
func (s *ImportService) Selection(ctx context.Context, batchID int64) ([]int64, error)
func (s *ImportService) SetSelection(ctx context.Context, batchID int64, ids []int64) (domain.ImportSummary, error)
func (s *ImportService) Summary(ctx context.Context, batchID int64) (domain.ImportSummary, error)
func (s *ImportService) Commit(ctx context.Context, batchID int64, actor domain.Actor, correlationID, sourceIP string) (domain.CommitResult, error)
func (s *ImportService) Ready() bool
```

`ImportService` holds `ImportStore`, `*ItemMetadataService`, `*SettingsService` and `now func() time.Time` (default `time.Now`; injected in tests). Audit is written by store methods that already take `domain.AuditEntry`; there is no separate recorder object to keep in sync. `Ready()` is false when the guild store was not configured/opened; handlers map that to `503 dependency_unavailable`, never a nil-pointer panic. The `Actor` comes from the authenticated session, never from request JSON. `ActiveSpells` selects all active spell rows for the given EQUIDs with `FOR UPDATE`; the advisory commit lock is taken by `BeginCommit` before this query.

### 6.2 Upload (`Upload`)

1. Validate `actor.UserID>0`, a non-empty `actor.UserName` and `actor.IsAdmin` (defense in depth; the session middleware already rejects disabled/non-admin callers), and the multipart filename is non-empty. Sanitize by replacing backslashes with `/` (`strings.ReplaceAll(name, "\\", "/")`) so a browser-sent Windows path cannot retain separators on Linux, then use `path.Base`, `strings.ToValidUTF8(name, "")`, strip C0/C1 control characters, trim, and truncate to 255 runes before storing. Reject if the result is empty, `.` or `..`.
2. The handler has already wrapped the body in `http.MaxBytesReader` (§7.2); read it through an `io.TeeReader` into a SHA-256 hash and count the bytes. Reject >10 MiB with a typed `ErrFileTooLarge` → `413` (defense for internal callers).
3. Insert `ImportBatch` with `Status=Processing`, actor snapshot and `ImportedAt=now().UTC()`. This row is the durable record even if parsing fails. Return its generated `ImportBatchID`.
4. Parse the bytes. On `*ParseError` or limit failure, call `FailBatch` with the line/code/message, a pointer to the observed `SourceRowCount` when the body was fully read (nil otherwise) and the computed SHA-256 only when the full body was read; a partial hash is never stored. Return `*ImportFailure{BatchID, Err}`. The batch remains queryable. `FailBatch` writes no audit row (PD3-14).
5. Classify every row and reject mixed classifications for one EQUID (`classification_conflict`, line numbers). On conflict, call `FailBatch` with the counts/hash and return `*ImportFailure`; no rows are staged.
6. Normalize each location, compute `LocationSort = NaturalSortKey(group)` and `NameSort = NaturalSortKey(name)`. Blank-location warnings are attached per row.
7. Resolve metadata once for all distinct EQUIDs (items and spells) via `ItemMetadataService.ResolveBatch` with `[]domain.ItemReference{{EQUID, Name}}`. For items the metadata supplies canonical name/class/race; for spells it is used only for `MetadataMissingFg` and the warning, never for the spell name/class/race. `ResolveBatch` returns `[]ItemMetadata` in the same order as the distinct reference slice; build a `map[uint64]ItemMetadata` for the staging pass (it does not return a map). Convert each `service.ItemWarning` to `domain.ImportWarning` (preserving `EQUID`, `Mask`, `HighBits`, `Detail`). A resolution error calls `FailBatch` with `metadata_unavailable` and the counts/hash, then returns `*ImportFailure`; no rows are staged.
7a. Set `SelectedFg` with the single default rule (IMP-04/IMP-05): `Unspecified` → false; `Hoard` → true; otherwise `!IsContainerLocation(raw)`. A container location is a raw value without a trailing `-Slot<n>`.
8. Call `StageBatch` once: inserts staged rows in chunks of 500 inside one transaction, updates the batch to `Uploaded` with `SourceRowCount`, `AcceptedRowCount`, `IgnoredRowCount` and `FileSha256`, and writes the upload-success audit row (including `ignoredRows`) in the same transaction. If staging fails, the transaction rolls back rows and batch update; call `FailBatch` in a second transaction so the batch is not stranded in `Processing`. Return `503` (or `422` for a validation-class failure).
9. Re-read the batch (or return it from `StageBatch`) so `UploadResult.Batch` reflects `Uploaded`, the counts and the hash. Duplicate detection: query for an earlier `Uploaded`/`Completed` batch with the same SHA-256 and return `DuplicateOf *int64` in `UploadResult` plus an `X-Import-Duplicate-Of` response header. Do **not** reject; re-uploading is legitimate.

`UploadResult` carries `Batch`, `StructuralRows`, `DiscardedRows`, `IgnoredRows`, `BlankRows`, `DuplicateOf`, and warning counts. Filenames, counts and actor may be logged; file contents and names/IDs must not be logged at Info.

### 6.3 Selection

`ReplaceSelection` is a full replacement (D-11): the client sends the complete set of `ImportID`s it wants selected.

- Reject >50,000 IDs, duplicates, non-positive IDs and IDs belonging to another batch (`422 selection_invalid` with field `selectedImportIds`).
- In a transaction, lock the batch row; only `Uploaded` accepts changes (`409 batch_not_editable`).
- Validate every ID belongs to the batch; if any is foreign, roll back the whole update.
- `UPDATE ImportList SET SelectedFg=0 WHERE ImportBatchID=?`, then chunked `UPDATE ... SET SelectedFg=1 WHERE ImportBatchID=? AND ImportID IN (...)`; assert a final `COUNT(*) WHERE SelectedFg=1` equals the requested set inside the transaction. Do not assert the chunk `RowsAffected` sum: MySQL reports changed rows, so re-selecting an already-selected row reports `0`.
- Empty selection is valid to save; commit then rejects it with `422 selection_empty`.
- Last write wins across administrator tabs; there is no selection revision column. The UI re-fetches before commit and the commit always reads the persisted set, so a stale tab cannot commit an obsolete set.

### 6.4 Review reads

- `ListLocations` implements DD §9.3, ordering by `MIN(LocationSort), LocationGroup`. Rows whose `LocationGroup` is `Unspecified` are excluded (IMP-C), so the review never receives an Other entry.
- `ListRows` supports `locationGroup`, `selected` (`all|selected|unselected`), `search` (name contains, optional), `page`, `pageSize` (default 50, max 200), order `LocationSort, NameSort, ImportID`. `Unspecified` rows are always excluded.
- `RowIDs(locationGroup)` returns every staged ID in a location so the UI can toggle a group without paging; `Unspecified` rows are always excluded.
- `SelectedIDs` returns the full selected set for UI bootstrap, excluding `Unspecified` rows even when a legacy batch still has `SelectedFg=1` on them.
- `SelectedTotals` returns selected row count, item row count, item unit sum, spell row count, spell quantity sum, distinct spell count and the number of selected rows carrying at least one warning, in one or few aggregate queries; `Unspecified` rows are excluded.
- `SpellPreview` returns staged spell aggregation and whether an active `GuildBankItems` spell exists for each EQUID. It never mutates.
- `Summary` composes `SelectedTotals`, `ListLocations`, `SpellPreview` and `GetBatch`: `StagedRows` is the sum of the reviewable `ListLocations` row counts (accepted rows minus the internal `Unspecified` rows), `SelectedLocations` counts groups with `SelectedCount>0`, `NewSpells`/`MergedSpells` split the preview on `MergeTargetID`, `InventoryRecordsToCreate=ItemUnits+NewSpells`, `TotalRecordsToImport=ItemUnits+DistinctSpells`.
- Completed/failed batches support every read. The UI hides mutation controls when status is not `Uploaded`.

### 6.5 Commit

`Commit(ctx, batchID, actor)`:

1. Validate the actor with the same `Actor` check as §6.2, confirm the batch exists, and require state `Uploaded`; reject empty selection before opening the write transaction (`422 selection_empty`), and re-check after the in-transaction re-read of selected rows so a concurrent deselection cannot commit an empty set.
2. Resolve metadata for all distinct EQUIDs in the batch (items and spells; superset of the current selection) before the transaction, so a metadata outage fails fast and the transaction stays short. Item metadata supplies name/class/race; a spell row uses the same resolved name/class/race when its EQUID resolves and only `MetadataMissingFg` plus the warning when it does not (DEFECT-01). `Items` is read-only and treated as static during a commit.
3. Read `GuildOpenDays`/`BazaarDays` from `SettingsService.Values` and parse them with the same 0–3650 range as settings validation; fall back to `SettingDefinitions` defaults only if a key is absent. Invalid values fail the commit with `settings_invalid` before the transaction. Never re-read settings inside the transaction.
4. `BeginCommit` opens a dedicated connection, acquires `GET_LOCK(CONCAT('guildbank:commit:', LEFT(SHA2(DATABASE(), 256), 32)), 30)`, then starts a MariaDB transaction and locks/re-reads the batch `FOR UPDATE`. The hashed per-database lock name stays within MariaDB's 64-character advisory-lock limit, serializes commits without colliding with another GuildBank database on the same server, protects the active-spell invariant and makes concurrent commit attempts deterministic.
5. `BeginCommit` has already rejected a non-`Uploaded` batch with `ErrConflict` and returns the locked snapshot through `Batch()`; re-read selected rows inside the transaction. If the snapshot is not `Uploaded` → `409 batch_not_editable`.
6. `ActiveSpells` selects all `ItemType='Spell' AND DeletedFg=0` rows for the staged spell EQUIDs `FOR UPDATE`, ordered by `GuildBankItemID`.
7. Build a pure `CommitPlan` (`internal/service/import_commit.go`):
   - group staged spell rows by EQUID and sum quantities;
   - for each spell with exactly one active existing row → merge;
   - for each spell with no active row → insert; use the lowest `ImportID` staged row's imported `Name` and `LocationRaw`, with `Quantity` summed across the EQUID;
   - for >1 active rows → merge into the lowest ID and add a `duplicate_spell_target` warning to the result/audit;
   - items expand by quantity into individual `GuildBankItem` values with `Quantity=1`;
   - `DateAdded=today UTC`, `DateOpen/DateBazaar` from the pre-read settings snapshot;
   - item rows get canonical metadata/name and class/race strings; missing metadata keeps the imported name, blanks class/race and sets `MetadataMissingFg=1`;
   - spells keep the imported name and blank class/race, but `MetadataMissingFg` still reflects whether the spell EQUID was present in `Items`;
   - every committed row gets `NameSort=NaturalSortKey(final Name)`; it is not copied from `ImportList.NameSort`, which sorts the imported name in the review grid.
8. Guard the expansion limit (100,000). Apply all writes through the transaction:
   - chunked inserts for item instances and new spells;
   - quantity updates for merges (`ModAt=now`, `ModBy=actor.UserName`, `ModByUserID=actor.UserID`; `DateAdded/Open/Bazaar` and location unchanged);
   - update selected staging rows' `WarningText` when commit-time metadata warnings differ;
   - audit summary events (PD3-14): one `IMPORT_COMMITTED` for the batch, one `ITEM_CREATED` with `RecordID=ImportBatchID` and new-record counts, and one `ITEM_UPDATED` per merged spell row with old/new quantity plus staged row/location provenance;
   - `MarkCompleted`.
9. `Commit`. On any error `Rollback`; batch remains `Uploaded` and retry is allowed. Map deadlock (`1213`) to one automatic retry of the whole commit; second failure is `503`. Advisory-lock timeout is `503`.
10. Return `CommitResult` with inserted item records, inserted spells, merged spells, spell quantity added, total affected records, warnings and duration.

Selection is intentionally not revalidated against a client version; the transaction's persisted set is authoritative.

### 6.6 Date and metadata rules

- `now := time.Now().UTC()` in production (the injected `now` in tests), captured once per `Commit` call and reused on the single deadlock retry so dates and `ModAt` do not change between attempts; do not use `Truncate(24h)` for the calendar date. Use `time.Date(year, month, day, 0, 0, 0, 0, time.UTC)`.
- `DateOpen = DateAdded + GuildOpenDays`; `DateBazaar = DateAdded + BazaarDays`, using the pre-read, validated settings snapshot. Never re-read or mutate settings during commit.
- A single `ModAt` timestamp is used for every row in one batch; `ModBy`/`ModByUserID` come from the authenticated session.
- `SourceLocation` on committed item instances is `ImportList.LocationRaw`, as required by D-06; an empty raw location is written as SQL `NULL`. A future edit is free to normalize it.
- `NameSort` on committed rows is always derived from the final committed `Name`; changing metadata between staging and commit therefore cannot leave a stale inventory sort key.
- Audit `NewValues` for a merge contains the previous and resulting quantity, `ModAt`, actor and batch ID — not the whole item. `CorrelationID` is the request ID.
---

## 7. REST API and handlers

### 7.1 Routes

All routes require an authenticated administrator. Add the API routes to the existing `RequireAdmin` group; add the HTML pages to the admin page group. Unsafe methods stay behind the existing CSRF middleware. Because the current `csrfManager.submittedToken` reads the header and urlencoded bodies only, extend it to accept a bounded multipart parse **only on the upload route** and read `csrf_token` from `MultipartForm.Value`; otherwise the no-JS upload form cannot pass CSRF. On a parse failure or a rejected token the middleware must `RemoveAll` any parsed temporary files before returning; after a successful parse it must `defer` cleanup across `c.Next()` so a later `RequireAdmin` rejection or handler early return is covered. The upload handler also defers a nil-checked cleanup for the header-token path where it parses the form itself. Other unsafe routes keep the header/urlencoded behavior.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/admin/imports` | HTML batch history + upload form |
| `GET` | `/admin/imports/:batchID` | HTML review page (read-only for terminal batches) |
| `POST` | `/api/v1/imports` | Upload multipart field `file`; JSON by default, `303` for HTML forms |
| `GET` | `/api/v1/imports` | Recent batches (`limit` default 50, max 200; optional `beforeId`) |
| `GET` | `/api/v1/imports/:batchID` | One batch with counts/status/failure |
| `GET` | `/api/v1/imports/:batchID/locations` | Location groups + counts, natural order |
| `GET` | `/api/v1/imports/:batchID/rows` | Paged staged rows (`locationGroup`, `selected`, `q`, `page`, `pageSize`) |
| `GET` | `/api/v1/imports/:batchID/row-ids` | All staged IDs (`locationGroup` optional), for group toggles |
| `GET` | `/api/v1/imports/:batchID/selection` | Full selected `importId` list |
| `PUT` | `/api/v1/imports/:batchID/selection` | Full selection replacement |
| `GET` | `/api/v1/imports/:batchID/summary` | Unit-aware selected totals + spell preview |
| `POST` | `/api/v1/imports/:batchID/commit` | Atomic commit of the persisted selection |

`/api/v1/imports` is deliberately not nested under `/admin`; authorization, not URL shape, protects it. The versioned prefix follows the existing `/api/v1` surface (`Docs/ArchitectureReview.md` IC-02) and the selection route is the D-11 endpoint under that prefix; the unversioned `/api/...` spelling in D-11 is not mounted because no other unversioned route exists. `GET` routes never require CSRF. There is no batch delete or selection PATCH.

### 7.2 Upload handler

```text
POST /api/v1/imports
Content-Type: multipart/form-data
file=<InventoryExport.txt>
```

- `http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)` applies only after `SecurityHeaders` has chosen the upload limit; call `ParseMultipartForm(1<<20)`, then read the `file` part with an `io.LimitReader`; never trust `FileHeader.Size`. The existing CSRF middleware must be extended to read `csrf_token` from multipart form values (it currently handles only the header and urlencoded bodies).
- Require exactly one file part named `file`. Missing/empty → `422 validation_failed`, field `file`.
- Optional `X-Filename` is ignored; the multipart `Filename` is authoritative and sanitized with the §6.2 rule (separator normalization, `path.Base`, `strings.ToValidUTF8`, control-character stripping, 255-rune truncation) because `ImportBatch.FileName` is `VARCHAR(255)`; display that stored value rather than the raw header.
- `201` JSON: `{"data":{"batch":{...},"structuralRows":2,"discardedRows":348,"blankRows":1,"duplicateOf":null}}`.
- Parse failure: `422` envelope with `code:"invalid_import_file"`, `fields.file` = line-numbered reason; the response header `X-Import-Batch-ID` contains the failed batch so API clients can open it. The batch itself is not deleted.
- Too large: `413` `file_too_large`.
- Store unready: `503` `dependency_unavailable`.
- HTML form (`Accept: text/html`): success → `303 /admin/imports/{id}?uploaded=1`; parse failure → `303 /admin/imports/{id}?failed=1` so the failure page is durable and refresh-safe. Other HTML failures render a small error page with `writeHTMLStatus`.
- Pass `c.ClientIP()` as `sourceIP` and the middleware request ID as `correlationID` to `Upload`; the service copies both into the audit entry.
- Log at Info: batch ID, actor ID, size, counts, duration, SHA prefix. Never log row names.

### 7.3 JSON shapes

Use one `{"data": ...}` envelope for success, matching the settings API style. All IDs are JSON numbers; timestamps are RFC 3339 UTC.

```json
GET /api/v1/imports/12
{"data":{"batch":{
  "id":12,"fileName":"InventoryExport.txt","sha256":"…","status":"Uploaded",
  "sourceRowCount":548,"acceptedRowCount":197,"failureReason":null,
  "importedBy":"admin","importedByUserId":1,"importedAt":"2026-11-21T14:02:03Z"
}}}

GET /api/v1/imports/12/locations
{"data":{"locations":[
  {"group":"Unspecified","sort":"unspecified","rowCount":31,"selectedCount":31},
  {"group":"General 1","sort":"general\u00010000000001","rowCount":35,"selectedCount":35}
]}}

GET /api/v1/imports/12/rows?locationGroup=General%201&page=1&pageSize=50
{"data":{"rows":[{
  "importId":91,"sourceLine":33,"itemType":"Item","equid":12345,
  "name":"Sword of Testing","quantity":1,"locationRaw":"General 1-Slot1",
  "locationGroup":"General 1","selected":true,"warnings":[]
}],"page":1,"pageSize":50,"total":35}}

GET /api/v1/imports?limit=50
{"data":{"batches":[{...}],"nextBeforeId":11}}

GET /api/v1/imports/12/selection
{"data":{"batchId":12,"selectedImportIds":[1,2,3,91]}}

GET /api/v1/imports/12/row-ids?locationGroup=General%201
{"data":{"importIds":[91,92,93],"total":35}}

GET /api/v1/imports/12/summary
{"data":{
  "batchId":12,"status":"Uploaded",
  "selectedRows":166,"stagedRows":166,"selectedLocations":29,"totalLocations":29,
  "itemRows":163,"itemUnits":668,"spellRows":3,"spellQuantity":3,"distinctSpells":3,
  "newSpells":3,"mergedSpells":0,"inventoryRecordsToCreate":671,
  "warningRows":0,"totalRecordsToImport":671
}}
```

`nextBeforeId` is the last returned batch ID when another page may exist and `null` otherwise. `row-ids` returns `total` for the filtered group even though `importIds` is the complete list. `stagedRows` and `selectedRows` are row counts of the reviewable set: accepted rows minus the internal `Unspecified` rows, which are never listed, selectable or committed (IMP-C). The example above uses the full reviewable selection (166 of 197 accepted rows; the 31 Other rows are excluded). `itemUnits` is the expanded inventory-row count for items. `inventoryRecordsToCreate` is itemUnits + newSpells (not + merged spells). `totalRecordsToImport` is itemUnits + distinct spells (created + merged). In the commit result, `spellQuantityAdded` is the total staged spell quantity applied, whether inserted or merged, and `totalRecordsAffected` is `itemRecordsCreated + spellRecordsCreated + spellRecordsMerged`. The UI labels each number with its unit rather than showing a single ambiguous “items” count (DC-18).

### 7.4 Selection and commit

```json
PUT /api/v1/imports/12/selection
{"selectedImportIds":[1,2,3,91]}
→ 200 {"data":{"batchId":12,"selectedRows":4,"summary":{...}}}
```

- Body limit 2 MiB and 50,000 IDs.
- Unknown fields rejected with `422`.
- Foreign/duplicate/non-positive ID → `422 selection_invalid`, field `selectedImportIds`.
- Terminal batch → `409 batch_not_editable`.
- Save empty list → `200`; commit after that → `422 selection_empty` (field `selection`).

```json
POST /api/v1/imports/12/commit
→ 200 {"data":{"batchId":12,"status":"Completed",
  "itemRecordsCreated":723,"spellRecordsCreated":3,"spellRecordsMerged":0,
  "spellQuantityAdded":3,"totalRecordsAffected":726,
  "duplicateSpellTargets":[],
  "warnings":[],
  "durationMs":812}}
```

### 7.5 Error mapping

| Condition | Status | Code |
|---|---|---|
| Validation (field, malformed page/limit, empty selection) | 422 | `validation_failed` / `selection_invalid` / `selection_empty` |
| Invalid commit-time settings snapshot | 422 | `settings_invalid` |
| File too large | 413 | `file_too_large` |
| Parse/classification failure | 422 | `invalid_import_file` |
| Batch not found | 404 | `not_found` |
| Editable-state violation or duplicate commit | 409 | `batch_not_editable` |
| Guild store/metadata/advisory lock unavailable | 503 | `dependency_unavailable` |
| Internal failure (rolled back) | 500 | `internal_error` |

Use typed service errors and handler mapping; do not leak MariaDB errors. The existing `respondError` is reused for domain errors, with explicit handler cases before it for import-specific codes. Include `request_id` in every error envelope (existing helper).

### 7.6 Handler structure

- `internal/api/imports.go`: `importsHandler` with `Imports *service.ImportService`, logger, views, readiness.
- Handlers: `uploadImport`, `listImports`, `getImport`, `listImportLocations`, `listImportRows`, `listImportRowIDs`, `getImportSelection`, `putImportSelection`, `getImportSummary`, `commitImport`, plus `importsPage` / `importDetailPage`.
- Parse `batchID` with `strconv.ParseInt`, reject `<=0` as `404`.
- `page` defaults 1; `pageSize` defaults 50, max 200. `selected` accepts only `all|selected|unselected`. `q` is trimmed, empty means no filter, and >128 code points is `422`. An unknown `locationGroup` on `/rows` or `/row-ids` is an empty result, not `404`; only a missing batch or referenced row is `404`. Invalid values are `422`, not silently ignored.
- Store readiness is checked once per handler before calling the service; `503` if not ready.
- HTML handlers render `imports.html` / `import_detail.html` through the same layout/partials mechanism as settings. The detail page embeds `data-batch-id` and `data-status`; no row data is rendered server-side beyond the batch header, so the grid can page without full-page reloads.
- `views.go` gains `RenderImports` and `RenderImportDetail`; template data carries `User`, `CSRF`, batches/batch, `Ready`, and page metadata.
- `batch` JSON includes `failureReason` (null unless failed), `sha256` (null before staging completes), and `importedAt` from `domain.ImportBatch.ImportedAt`; there is no `importDate` field.

### 7.7 Commit handler

- No body is required; a body, if present, is ignored only if empty. Do not accept a selection or item data in the commit request.
- `409` for already `Completed`, `Processing` or `Failed`; `422 selection_empty`; `503` for dependency/lock timeout; `500` only after rollback and with a logged request ID.
- A commit retry after `503` is safe: a successful prior commit is `Completed` and subsequent attempts are `409`.
- Pass `c.ClientIP()` and the request ID to `Commit` for audit provenance.
- The handler returns the `CommitResult` shape from §7.4 and sets no success `Location`; JSON clients re-fetch batch state.
---

## 8. Web UI

### 8.1 Pages

**`/admin/imports` — history and upload**

- Upload card: `<form method="post" action="/api/v1/imports" enctype="multipart/form-data">` with the hidden `csrf_token` and a file picker (`accept=".txt,text/plain"`), submit button, help text describing the expected tab-delimited columns and that the default is “all rows selected”.
- A live “uploading…” disabled state via progressive enhancement; without JS the form posts normally.
- Recent batches table: date, file name, status badge, source/accepted counts, imported by, link to detail. Failed rows show a truncated failure reason; completed rows are labeled read-only.
- Empty state explains that no imports exist and no guild database is configured if `Ready=false`.
- A `duplicateOf` query parameter renders a non-blocking banner linking to the earlier batch.

**`/admin/imports/:id` — review**

- Header: file name, status, source/accepted counts, imported by/date, SHA prefix, failure reason (full text in a `<pre>` for failed batches).
- Batch meta also shows the IMP-01 ignored nested-slot count. The review grid adds a Kind column (`Slot`/`Container`), and the `Unspecified` group is labelled **Other (not a bank location)**.
- For `Uploaded`: summary cards, location panel, row grid, “Commit selected” button.
- For `Completed`/`Failed`: the same read-only grid and summary, no checkboxes, no commit. Completed shows a “committed” banner; failed shows the parse reason.
- `data-batch-id`, `data-status`, `data-page-size` on `<main>` are the only values the JS needs.

### 8.2 Interaction rules

- Load `/selection` and `/summary` in parallel on page load. Maintain `selected = Set(selectedImportIds)`.
- Load location groups and the first row page. The location selector is built only from distinct `LocationGroup` values; all `Hoard<n>` variants appear as one `Hoard` entry. `LocationRaw` is shown as row provenance, never as a filter option. Rows show checkbox state from the local set, not from the row response alone.
- Toggle a row: update the set immediately, recalculate the visible checkbox/counters optimistically, debounce 300 ms, then `PUT /selection` with the complete set. Disable commit while a PUT is in flight; apply the canonical `summary` from the response; on failure revert the set and show an inline error.
- Toggle a location:
  1. `GET /row-ids?locationGroup=…` once and cache the IDs.
  2. Add/remove all IDs from the set and update the page-local group scope.
  3. PUT as above; reload `/rows` with `excludeGroups` so a hidden group's rows disappear.
  The location checkbox is the **group scope** control (IMP-06): checked when the
  group is in scope and fully selected, `indeterminate` while in scope and not
  fully selected. Unchecking clears the group's rows from the selection and hides
  them; the group stays listed as `hidden` so it can be re-included. The scope is
  memory-only and resets to every group on reload.
- The `Unselected` row filter sets `ignoreGroups=1` and drops `excludeGroups`, so
  rows in a hidden group remain reviewable and can be re-selected individually.
- “Select all” and “Clear” apply only to the groups in scope: they call
  `GET /row-ids?excludeGroups=…`. The scope list is ordered by `/locations`.
- The location panel presents `Unspecified` as **Other (not a bank location)**
  (IMP-02) and shows a banner with its row count, explaining that it is never
  selected automatically. The grid adds a **Kind** column (`Slot` / `Container`)
  so the default exclusion of container rows is visible (IMP-03).
- “Reset to defaults” (IMP-04) calls `GET /default-selection`, replaces the
  selection with the returned IDs, re-includes every group and PUTs the set. It
  is the only way an existing batch adopts a changed default rule; staging never
  rewrites an existing batch.
- Row filters (`locationGroup`, selected state, search) re-request `/rows` with `page=1`; page links keep the current set in memory and do not re-fetch selection. The location dropdown lists only in-scope groups; removing the active filter group resets the dropdown to “All locations in scope”.
- Commit: confirm with the summary (“Import 726 inventory records?”), POST, disable the page, then reload the detail page on success. `409` shows “This batch has already been committed or is no longer editable”; `422 selection_empty` highlights the summary; `503` offers retry.
- All fetch calls send `X-CSRF-Token` from the page meta tag on unsafe methods and send `Accept: application/json`.
- Errors are rendered in an `aria-live="polite"` status area; no `alert()`.

### 8.3 Templates, CSS and JS

- New templates: `internal/web/templates/imports.html` (history/upload) and `import_detail.html` (review). Add “Imports” to the admin navigation partial only when `User.IsAdmin`.
- New static file `internal/web/static/imports.js`, registered in `embed.go`; plain ES5 syntax and no build step, matching `setup.js`. Guard on `document.getElementById("import-detail")` so it is inert elsewhere.
- CSS additions in `app.css`: `.import-table`, `.status-badge` (Uploaded/Processing/Completed/Failed variants using existing `--accent`, `--ok`, `--danger`), `.import-layout` (sidebar + grid, single column below 900px), `.location-list`, `.summary-grid`, `.pager`, `.warnings`.
- The grid is a real `<table>` with `<th scope="col">`, checkbox labels, and a caption. On narrow screens the location panel stacks above the table; the table may scroll horizontally rather than hiding columns.
- No inline scripts except the existing CSRF meta value; no third-party CDN.

### 8.4 Accessibility and UX edge cases

- Checkbox changes have text labels or `aria-label` including name and location.
- The indeterminate location state is set in JS (`checkbox.indeterminate`), with a text count so it is not color-only.
- Failed uploads are always navigable; the upload form is never replaced by a dead-end error page.
- A batch with no accepted rows shows an explicit empty state and disables commit.
- A batch with warnings shows the warning count and lets the user inspect warnings per row; warnings never block commit.
- During a long commit, show “Committing…” and prevent duplicate submits. The server remains the concurrency authority.
---

## 9. Repository and SQL implementation

### 9.1 Files and ownership

- `internal/repository/mariadb/migrate.go` — embedded `migrations/*.sql`, `Migrate(ctx) (string, error)`, `schema_migrations`, advisory lock.
- `internal/repository/mariadb/imports.go` — `ImportBatch`/`ImportList` reads, staging writes, selection replacement, totals, preview, commit transaction.
- `internal/repository/mariadb/guilditems.go` — item insert and spell merge/lock helpers used by the import transaction. Inventory CRUD beyond this is out of scope.
- `internal/repository/mariadb/audit.go` — unexported `insertAudit(ctx, executor, entries)` helper that writes the DD §4.5 columns. Store methods and `ImportCommitTx.WriteAudit` call it inside their existing transaction; there is no exported recorder interface. No outbox delivery worker is required in this phase, and no outbox row is written for guild-store imports.
- `internal/repository/sqlite` gains nothing; import state is guild-store state and never belongs in SQLite. The split stays exactly as DC-11 describes.

All SQL uses `?` placeholders and `context.Context`. No string interpolation of user values. Table/column names here are static identifiers from the design.

### 9.2 Migration runner

1. `CREATE TABLE IF NOT EXISTS schema_migrations (Version VARCHAR(32) PRIMARY KEY, AppliedAt DATETIME(6) NOT NULL)`.
2. Acquire `SELECT GET_LOCK(CONCAT('guildbank:migrate:', LEFT(SHA2(DATABASE(), 256), 32)), 30)` on a dedicated `*sql.Conn`. If it returns `0`/`NULL`, fail `ErrUnavailable`; release in `defer`.
3. Read applied versions into a set. If any embedded version is lexically lower than the highest applied but absent, that is a downgrade/edited-history error; fail and log both versions. Never auto-repair.
4. For each embedded file in lexical order not applied: split statements safely (statements contain no `;` inside literals; add a documented splitter) or keep one statement per `-- statement` marker. Execute; insert the version only after all statements complete. Log `migration applied`, version, duration.
5. Release the lock and return the latest version.
6. A second startup is a no-op. A failed migration leaves no version row; the next startup retries from that file. Because DDL is non-transactional, `IF NOT EXISTS`/guarded indexes make retries safe.

Startup in `app.go`:

```go
if guildStore != nil {
    if _, err := guildStore.Migrate(ctx); err != nil {
        return fmt.Errorf("app: migrate guild store: %w", err)
    }
    if _, err := guildStore.RecoverInterrupted(ctx); err != nil {
        logger.Warn("import recovery failed", "error", err)
    }
}
```

With PD3-10 this is fatal after a successful `Open`; it does not replace the existing degraded behavior when the store cannot be opened.

### 9.3 Staging transaction

`StageBatch` uses one `*sql.Tx`:

1. Insert `ImportList` rows in chunks of 500 using a multi-row `INSERT` with 12 placeholders per row (all columns except auto-increment `ImportID`, including nil `WarningText`).
2. `UPDATE ImportBatch SET Status='Uploaded', SourceRowCount=?, AcceptedRowCount=?, IgnoredRowCount=?, FileSha256=? WHERE ImportBatchID=? AND Status='Processing'`; require exactly one affected row.
3. Insert the upload audit entry.
4. Commit. On error roll back all inserts/status; the service then calls `FailBatch` in a separate transaction. No cleanup `DELETE` is needed because the staging transaction rolled back.

`FailBatch` sets status in a single UPDATE guarded by `Status='Processing'`; it is idempotent so the recovery sweep cannot overwrite a completed batch.

### 9.4 Selection replacement SQL

Validation first (`SELECT COUNT(*) FROM ImportList WHERE ImportBatchID=? AND ImportID IN (...)`, chunked, plus total membership count), then:

```sql
START TRANSACTION;
SELECT Status FROM ImportBatch WHERE ImportBatchID=? FOR UPDATE;
UPDATE ImportList SET SelectedFg=0 WHERE ImportBatchID=?;
-- chunk 1,000:
UPDATE ImportList SET SelectedFg=1
 WHERE ImportBatchID=? AND ImportID IN (?,?,...);
-- after all chunks:
SELECT COUNT(*) FROM ImportList WHERE ImportBatchID=? AND SelectedFg=1;
-- must equal len(ids); do not sum changed-row counts from the chunks
COMMIT;
```

No audit row is written (PD3-08). If `len(ids)==0`, the second step is skipped. The final selected count is asserted, because a silent partial update would change what the user commits; changed-row counts are not used because MySQL returns `0` for a row already holding the target value.

### 9.5 Review queries

Locations (DD §9.3, extended with stored sort values):

```sql
SELECT LocationGroup, MIN(LocationSort) AS LocationSort,
       COUNT(*) AS RowCount, SUM(SelectedFg) AS SelectedCount
  FROM ImportList
 WHERE ImportBatchID = ?
 GROUP BY LocationGroup
 ORDER BY LocationSort, LocationGroup;
```

Rows/count use identical predicates:

```sql
SELECT ImportID, SourceLine, ItemType, EQUID, Name, Quantity,
       LocationRaw, LocationGroup, SelectedFg, WarningText
  FROM ImportList
 WHERE ImportBatchID = ?
   -- AND LocationGroup = ?          (only when locationGroup is set)
   -- AND Name LIKE ? ESCAPE '!'      (only when q is set; pattern escaped)
   -- AND SelectedFg = ?             (only for selected/unselected)
 ORDER BY LocationSort, NameSort, ImportID
 LIMIT ? OFFSET ?;

SELECT COUNT(*) FROM ImportList WHERE /* same assembled predicates */;
```

`selected=selected` maps to `SelectedFg=1`; `unselected` maps to `0`. Keep every optional predicate as an explicit Go branch (the SQL above shows the shape after branch expansion) rather than `(? = '' OR ...)` or a `CASE`, either of which defeats the index. Escape `!`, `%` and `_` in `q` (prefix each with `!`) so it is a literal substring search; `!` avoids dependence on the server's `NO_BACKSLASH_ESCAPES` mode.

Selected totals:

```sql
SELECT COUNT(*) AS SelectedRows,
       COALESCE(SUM(ItemType='Item'),0) AS ItemRows,
       COALESCE(SUM(CASE WHEN ItemType='Item' THEN Quantity ELSE 0 END),0) AS ItemUnits,
       COALESCE(SUM(ItemType='Spell'),0) AS SpellRows,
       COALESCE(SUM(CASE WHEN ItemType='Spell' THEN Quantity ELSE 0 END),0) AS SpellQuantity,
       COUNT(DISTINCT CASE WHEN ItemType='Spell' THEN EQUID END) AS DistinctSpells,
       COALESCE(SUM(CASE WHEN WarningText IS NOT NULL AND WarningText <> ''
                         AND WarningText <> '[]' THEN 1 ELSE 0 END),0) AS WarningRows
  FROM ImportList
 WHERE ImportBatchID = ? AND SelectedFg = 1;
```

Spell preview:

```sql
SELECT EQUID, SUM(Quantity) AS StagedQuantity, COUNT(*) AS StagedRows
  FROM ImportList
 WHERE ImportBatchID = ? AND SelectedFg = 1 AND ItemType = 'Spell'
 GROUP BY EQUID;

SELECT GuildBankItemID, EQUID, Quantity
  FROM GuildBankItems
 WHERE DeletedFg = 0 AND ItemType = 'Spell' AND EQUID IN (...)
 ORDER BY GuildBankItemID;
```

`newSpells = distinct staged spells whose EQUID is absent from the second query`; `mergedSpells` is the remainder. The preview and commit use the same predicates so the numbers cannot drift except for inventory changes between requests. Chunk both `IN (...)` lists at 1,000; a 50,000-spell selection must not build one giant placeholder list.

### 9.6 Commit transaction

`BeginCommit` must hold one physical connection because `GET_LOCK` and the transaction are connection-scoped:

```go
conn, _ := s.db.Conn(ctx)
// acquired, _ := conn.QueryRowContext(ctx,
//     "SELECT GET_LOCK(CONCAT('guildbank:commit:', LEFT(SHA2(DATABASE(), 256), 32)), 30)").Scan(&result)
// result 1 = acquired; 0 = timeout; NULL = error.
// On 0/NULL: release the lock best-effort, close conn and return ErrUnavailable
// before starting a transaction; release/close on every later path too.
tx, _ := conn.BeginTx(ctx, nil)
// SELECT ... FROM ImportBatch WHERE ImportBatchID=? FOR UPDATE
// if Status <> 'Uploaded': return ErrConflict (tx.Batch() exposes the snapshot)
// UPDATE ImportBatch SET Status='Processing' WHERE ImportBatchID=? AND Status='Uploaded'
// require RowsAffected=1; this status change rolls back with the transaction
```

`ImportCommitTx` stores `conn`, `tx`, the locked batch snapshot, and a `finished` guard so `Commit/Rollback` always release the lock and close the connection exactly once. `Batch()` returns the snapshot taken before the `Processing` update. `Commit`:

1. `tx.Commit()`.
2. `SELECT RELEASE_LOCK(CONCAT('guildbank:commit:', LEFT(SHA2(DATABASE(), 256), 32)))` (best effort; log failure).
3. `conn.Close()`.

`Rollback` does the same in reverse. If the process dies, MariaDB releases the lock when the connection drops.

Selected rows are read in the transaction; the batch status is rechecked. Existing spells are locked with `FOR UPDATE`. Writes:

```sql
INSERT INTO GuildBankItems
 (ItemType, EQUID, MetadataMissingFg, Name, ClassString, RaceString,
  DateAdded, DateOpen, DateBazaar, Quantity, SourceLocation, GivenTo,
  DeletedFg, ModAt, ModBy, ModByUserID, NameSort)
VALUES (?,?,?,?,?,?,?,?,?,?,?,NULL,0,?,?,?,?);
-- execute as one multi-row INSERT per chunk; the row above shows the column shape

UPDATE GuildBankItems
   SET Quantity = Quantity + ?, ModAt = ?, ModBy = ?, ModByUserID = ?
 WHERE GuildBankItemID = ? AND DeletedFg = 0 AND ItemType = 'Spell';
```

Inserted item instances use `Quantity=1`. New spells use the aggregated `Quantity`. `GivenTo` and `DeletedFg` are explicit `NULL`/`0`. The update's `RowsAffected=1` is required; a concurrent soft-delete between lock and update cannot occur inside the transaction, but the guard makes the invariant explicit.

`WriteAudit` uses a multi-row insert into `Audit`; every entry carries `CorrelationID` (request ID), `EventDate`, actor snapshots, `EventType`, `TableName`, `RecordID`, and `OldValues`/`NewValues` JSON. If the audit insert fails, the whole commit rolls back (DC-16).

| Moment | `EventType` | `TableName` | `RecordID` | JSON content |
|---|---|---|---|---|
| Upload staged (in the `StageBatch` transaction) | `IMPORT_CREATED` | `ImportBatch` | batch ID | new: file name, size, source rows, hash |
| Commit (in the commit transaction) | `IMPORT_COMMITTED` | `ImportBatch` | batch ID | old: `{status:"Uploaded"}`; new: status/counts/duration |
| New item units / one new spell | `ITEM_CREATED` | `GuildBankItems` | batch ID | new: summary counts (`itemUnits`, `spellsCreated`, `spellsMerged`), not every generated row |
| Each merged spell | `ITEM_UPDATED` | `GuildBankItems` | spell `GuildBankItemID` | old: quantity/location; new: quantity plus staged row/location provenance |

`FailBatch` writes no `Audit` row: D-10's allowlist has no import-failed event, and inventing one would reopen the taxonomy (DC-25). The failure is recorded in `ImportBatch.FailureReason` and the application log. The upload-success row is written only after staging commits, so a failed upload has no misleading `IMPORT_CREATED` event.

### 9.7 Recovery sweep

At startup, after migration:

```sql
SELECT ImportBatchID FROM ImportBatch
 WHERE Status='Processing' AND ImportedAt < ?; -- now - 15m
```

For each, run the equivalent of `FailBatch(batchID, reason='interrupted before completion', nil, nil)` and log a warning. Do this only after the migration succeeds and before the router serves traffic. It never touches `Uploaded`/`Completed` batches and never deletes their rows. Apply a grace period (for example, only batches with `ImportedAt < now-15m`) so a second instance starting while the first is still uploading cannot fail an in-flight `Processing` batch; the upload body/line limits keep a legitimate staging pass far below that window.

### 9.8 Error classification

Map driver errors by type/message only in the repository:

- `context.DeadlineExceeded` / canceled → pass through; handler maps to `503`/client-closed.
- `mysql.MySQLError.Number` `1213` (deadlock) → `domain.ErrConflict` or a typed `RetryableError` consumed by one service retry.
- duplicate key → `domain.ErrConflict` with a generic message; never expose the SQL.
- `sql.ErrNoRows` → `domain.ErrNotFound`.
- connection failures → `domain.ErrUnavailable`.
- anything else → wrapped with operation context for logs, but `respondError` returns generic `internal_error`.
---

## 10. Warning and error taxonomy

### 10.1 Non-fatal warnings

Warnings are stored as JSON on the staged row. They are visible in the grid and commit result, never block commit, and are not copied to inventory except through `MetadataMissingFg`.

| Code | Source | Field | Meaning / action |
|---|---|---|---|
| `metadata_missing` | metadata service | `EQUID` | EQUID absent from `Items`; imported name retained, class/race blank, `MetadataMissingFg=1` (items and spells alike). |
| `undefined_high_bits` | mask decoder | `ClassList` / `RaceList` | Low 16 bits decoded; high bits ignored (PD-20). Preserve `Mask`/`HighBits` in the staging JSON. |
| `location_blank` | normalize | `Location` | Raw location empty; group `Unspecified`. |
| `location_pattern_invalid` | normalize | `Location` | Value starts with a reserved bank prefix but does not parse; group `Unspecified`. |
| `duplicate_spell_target` | commit | `EQUID` | More than one active spell row matched; merged into the lowest ID. Returned in commit result/audit. |
| `duplicate_file` | upload service | `FileSha256` | A prior batch has the same hash; advisory only, not stored per row. |
| `warnings_truncated` | warning serializer | — | The 1,000-character cap omitted later warnings; `metadata_missing` is retained first. |

The JSON serializer caps `WarningText` at 1,000 characters. If the full array would exceed the cap, keep the first warnings by severity (`metadata_missing` first) and append `{"code":"warnings_truncated"}`. Parsing unreadable/legacy `WarningText` returns `[]`; it never fails a read.

### 10.2 Fatal batch errors

These are parser/upload failures; the batch is created and marked `Failed` with a line-numbered message:

| Code | Cause |
|---|---|
| `file_empty` | no importable rows (only header, structural, blank or discarded rows) |
| `invalid_utf8` | byte sequence is not UTF-8 |
| `invalid_header` | first non-blank line is not the canonical five-column header |
| `duplicate_header` | canonical header appears again |
| `too_few_fields` | data row has <3 fields |
| `too_many_fields` | data row has >5 fields |
| `invalid_id` | numeric ID > `uint32` |
| `invalid_quantity` | 5-field `Count` is 0, non-numeric, or >10,000 |
| `field_too_long` | name >128 or raw location >128 code points |
| `limit_exceeded` | parser body/line/row guard exceeded (the 100,000 commit-expansion guard is a separate `422 validation_failed`) |
| `classification_conflict` | one EQUID appears as both item and spell |
| `metadata_unavailable` | `Items` could not be read during upload |

Empty/zero IDs and empty names are **not** fatal; they are discarded and counted, because the real export contains those rows.

### 10.3 Message style

- Failure reasons are lowercase, line-prefixed, and name the field: `line 87: invalid_quantity — Count must be a positive whole number (got "x")`.
- Never echo a whole input line. Strip control characters and cap the quoted value at 40 runes before including it in a reason.
- No Go type names, SQL, file paths, or stack traces.
- Batch detail shows the full stored reason (max 1,000 chars); history truncates to 160 chars.
- Warnings show a short label in the grid and full detail on row expansion/`title`.

---

## 11. Application wiring, configuration and operations

### 11.1 Constructing the import service

`internal/platform/app/app.go`, after SQLite migration and a successful MariaDB `Open`/`Migrate`:

```go
metadata := service.NewItemMetadataService(guildStore)
imports := service.NewImportService(guildStore, metadata, settingsService, time.Now, logger)
```

- `guildStore` implements `ImportStore` and `ItemMetadataReader`; audit writes go through the store methods and `ImportCommitTx.WriteAudit`.
- If MariaDB is not configured or `Open` failed, build the same service with nil dependencies. `Ready()` is false; every import method returns `domain.ErrUnavailable`, and the handler renders the database-unavailable state rather than a 500. This keeps the nav/page stable in degraded mode.
- Production passes `time.Now`; tests inject a fixed `func() time.Time`. Do not add a second time abstraction.
- `settingsService` is used directly; do not add a separate settings cache in this phase.

Add `Imports *service.ImportService` to `api.Deps`. `NewRouter` requires it to be non-nil. Register:

```go
admin.GET("/admin/imports", importHandler.page)
admin.GET("/admin/imports/:batchID", importHandler.detailPage)
admin.GET("/api/v1/imports", importHandler.list)
admin.POST("/api/v1/imports", importHandler.upload)
admin.GET("/api/v1/imports/:batchID", importHandler.get)
admin.GET("/api/v1/imports/:batchID/locations", importHandler.locations)
admin.GET("/api/v1/imports/:batchID/rows", importHandler.rows)
admin.GET("/api/v1/imports/:batchID/row-ids", importHandler.rowIDs)
admin.GET("/api/v1/imports/:batchID/selection", importHandler.selection)
admin.PUT("/api/v1/imports/:batchID/selection", importHandler.putSelection)
admin.GET("/api/v1/imports/:batchID/summary", importHandler.summary)
admin.POST("/api/v1/imports/:batchID/commit", importHandler.commit)
```

Gin's wildcard parameters within one group are compatible because every route uses the same `:batchID` name. `POST /api/v1/imports` remains inside the CSRF and admin middleware groups; there is no public upload route.

### 11.2 Startup order and failure behavior

1. SQLite open/migrate (unchanged).
2. MariaDB open when configured (unchanged).
3. MariaDB `Migrate`; **fatal on error** (PD3-10). A database user without DDL rights therefore prevents startup with a clear log line, not a half-working import page.
4. `RecoverInterrupted`; log-only on error (it must not prevent serving reads).
5. Build services, router, server.

When MariaDB is unreachable, the existing degraded behavior is unchanged: `/readyz` reports it, import pages explain that the guild database is unavailable, and existing pages keep working. `Ready()` derives from the store pointer and never performs a ping per request.

The setup/settings flow currently persists new MariaDB credentials without rebuilding the process pool (`Docs/ProjectStructure.md` PD-18). Phase 3 must not pretend otherwise: the import page shows the active target from the running config and a note that connection changes take effect after restart. Hot-swapping the pool (`Docs/ArchitectureReview.md` IC-19) remains a separate, explicitly approved change; the import service depends only on interfaces, so a later swapper can replace `guildStore` without touching the import feature.

### 11.3 HTTP limits and timeouts

- Upload body: 10 MiB via `http.MaxBytesReader`. The existing `SecurityHeaders` middleware currently wraps every body at 1 MiB, so make its limit path-aware, using `c.FullPath()` (10 MiB for `POST /api/v1/imports`, 2 MiB for `PUT /api/v1/imports/:batchID/selection`, 1 MiB otherwise), before the handler runs; an outer 1 MiB cap cannot be raised by a handler-level reader. The upload route remains behind `RequireAuth`, which runs before CSRF or any body read, so the larger cap is not reachable unauthenticated.
- Multipart part headers are bounded by the 10 MiB body cap and Go's `mime/multipart` parser; require exactly one `file` part and do not claim a separate per-header limit that the standard parser does not expose.
- Selection body: 2 MiB, 50,000 IDs.
- JSON bodies: 1 MiB.
- Raise `http.Server.ReadTimeout` from 30s to 120s so a 10 MiB upload over a slow LAN can finish; keep `ReadHeaderTimeout=10s`, set `WriteTimeout=120s` and `IdleTimeout=60s`. These remain bounded and are documented in the deployment notes.
- Raise the guild-store driver settings `mysql.Config.ReadTimeout`/`WriteTimeout` from 30s to 120s in `internal/repository/mariadb/store.go` for the same reason; a commit transaction may legitimately run for many seconds. The HTTP and driver limits are aligned so the server does not cut off a request that MariaDB is still processing.
- `c.Request.MultipartForm.RemoveAll()` must be guaranteed for every successfully parsed form. The upload handler installs `defer func(){ if c.Request.MultipartForm != nil { c.Request.MultipartForm.RemoveAll() } }()` at entry for the header-token path, while the CSRF middleware `defer`s cleanup across `c.Next()` after its own successful parse; this covers a later `RequireAdmin` rejection and every handler early return. The middleware removes the form immediately only when it rejects the request before `c.Next()`. Both paths use `errors.As` for `*http.MaxBytesError`; the CSRF middleware maps that parse error to `413 file_too_large` rather than a generic `403`, so an oversized upload is not misreported.
- File names are sanitized (separator normalization, `path.Base`, `strings.ToValidUTF8`, control-character stripping, 255-rune truncation) before storing; the template still escapes the stored value.

### 11.4 Security review items

- Authorization is the existing admin session plus admin flag; upload is not available to authenticated non-admins or unauthenticated callers.
- CSRF applies to upload, selection and commit. JSON clients must send the header; HTML forms receive a token from the shared CSRF manager. The multipart upload path requires the middleware extension described in §7.1 because a hidden `csrf_token` field is not visible to the current header/urlencoded-only parser.
- SQL uses placeholders; no identifier interpolation. Failure reasons never include SQL or credentials.
- Imported names/locations are untrusted text: templates escape them; JSON encodes them; logs omit row-level text.
- File content is never executed, templated, or written to disk except Go's bounded multipart temp file, which is removed.
- Only `Items` metadata is read. No writes to `Items`, no schema changes to it, no outbound EQResource call.
- The commit advisory lock and transaction prevent two batches from concurrently creating duplicate active spells. A second server instance obeys the same lock.
- Audit `CorrelationID` uses the middleware request ID, preserving traceability across logs and the DB.
- Error responses keep the existing no-detail `internal_error` body; detailed errors go to the server log with `request_id`.

### 11.5 Operational notes

- Migration/add-index on a large `GuildBankItems` table can block writes; run the first deployment in a maintenance window and record duration.
- Log a structured `import batch uploaded` (counts, sha prefix) and `import batch committed` (counts, duration, correlation ID); log failures at Warn with batch ID and reason.
- No import rows are logged, because item names may be guild-private.
- `/readyz` remains the only readiness signal; an import-specific probe is not added.
- Restart during `Processing` is repaired by the sweep. Restart during commit cannot leave half-applied state: the MariaDB transaction commits or rolls back; the lock dies with the connection.
- Backup expectation: `GuildBankItems` and the import tables live in the same MariaDB database, so one database backup covers both. The plan adds no files under `/config` for imports.
---

## 12. Test matrix

Tests follow the existing split: pure unit tests with table cases, service tests with in-memory fakes, handler tests with `httptest` against the assembled router, and opt-in integration tests against MariaDB. No test calls EQResource.

### 12.1 Parser and normalizer

| Area | File | Cases | Level |
|---|---|---|---|
| Header | `internal/service/import_parser_test.go` | canonical header counts in `StructuralRows`; missing header; wrong label; header after data; duplicate header; leading blank lines | unit |
| Line handling | same | BOM; CRLF; LF; blank lines count in `SourceLines`; a trailing newline does not add a line; whitespace trimming; tabs preserved inside names; non-ASCII names | unit |
| Field counts | same | 3 fields → quantity 1; 4 fields → quantity 1; 5 fields → Count; 2 fields fatal; 6 fields fatal | unit |
| Discards | same | empty ID, `0` ID, non-numeric ID, empty name, name of spaces; all counted as discarded, not fatal | unit |
| Fatal parsing | same | invalid UTF-8; `Count=0`; non-numeric Count; Count >10,000; ID > `uint32`; name/location >128 code points; 64 KiB line; 50,000 lines; >10 MiB handled at handler level | unit |
| Structural rows | same | `Bank 1 / Name / ID`; spell-family section; section row not mistaken for data; `Name` field in a real item row | unit |
| Golden sample | `TestParseSampleExportGolden` | `Docs/Samples/InventoryExport-Sample.txt` (copied to `internal/service/testdata`): 548 physical lines, 197 accepted, 348 discarded, 2 structural, 1 blank, exact first/last accepted rows, 30 normalized groups, 723 item units, 3 spells | unit |
| Normalization | `import_normalize_test.go` | `General 1-Slot1`→`General 1`; `General 01`→`General 1`; `Bank 12-Slot3`; `SharedBank 2`; `Hoard 3`/`Hoard3`→`Hoard`; blank; `Equipment`; `Possessions`; spell section; malformed reserved prefix; trailing whitespace; case sensitivity | unit |
| Natural sort | `import_sort_test.go` | `General 1`, `General 2`, `General 10`; numeric runs, leading zeros, mixed case, accents, empty string, long keys ≤160 | unit |
| Classification | `import_normalize_test.go` | `Spell:`, `Song:`, `Ancient:`, case variants; `Ancient Sword` (item); names containing colons later | unit |
| Warning JSON | `import_warning_test.go` | round-trip; unknown code preserved; truncation at 1,000 chars; malformed stored JSON reads as empty | unit |

### 12.2 Service orchestration (fakes)

| Test | Asserts |
|---|---|
| `TestUploadStagesAllRowsSelected` | one `Processing` batch, parser counts, every staged row `SelectedFg=1`, `Uploaded` only after staging |
| `TestUploadParseFailureLeavesFailedBatch` | batch created, no rows, `Failed`, line-numbered reason and source line visible; structural/discarded/blank counts exist only in the upload response, while `SourceRowCount`/`AcceptedRowCount` are persisted columns |
| `TestUploadClassificationConflict` | same EQUID as item and spell → `Failed`, `classification_conflict` names both source lines, no rows staged |
| `TestUploadMetadataUnavailable` | fails before staging, `Failed`, `metadata_unavailable`, no partial batch |
| `TestUploadDuplicateHashIsAdvisory` | second batch still succeeds, result/warning includes `duplicate_file` |
| `TestSelectionRejectForeignAndDuplicateIDs` | foreign/duplicate/zero IDs → `422`-class error, no store mutation |
| `TestSelectionEmptyIsPersistable` | empty PUT succeeds, commit returns `selection_empty` |
| `TestSelectionTerminalBatch` | completed/failed batch → `batch_not_editable`, store unchanged |
| `TestSelectionChunkBoundary` | 1,001 / 50,000 IDs produce the right chunks; final selected count equals the requested set, including when every row was already selected |
| `TestSummaryCounts` | 197 rows → 194 item rows, 723 units, 3 spells/quantity, 3 new, 0 merged, 30 groups, 197 records (QTY-04) |
| `TestSummaryMergedSpell` | existing active spell → `mergedSpells=1`, `inventoryRecordsToCreate = itemRows + newSpells`, `totalRecordsToImport = itemRows + distinct` (QTY-04) |
| `TestCommitPlanCreatesOneRowPerLineAndAggregatesSpells` | two item lines (quantity 2 and 1) → two rows with those quantities, `SourceLocation=LocationRaw`, same batch timestamp (QTY-03) |
| `TestBuildCommitPlanNeverMergesRepeatedItemEQUID` | two imports of one EQUID → separate rows, no merge (QTY-01) |
| `TestCommitPlanAggregatesSpells` | duplicate spell rows summed into one insert; no per-row inserts; `Name`/`SourceLocation` come from the lowest `ImportID` row |
| `TestCommitPlanDuplicateActiveSpellTargets` | two active rows → merge into lowest `GuildBankItemID`, warning recorded |
| `TestCommitPlanMetadataFallback` | missing item EQUID → imported name, blank class/race, `MetadataMissingFg=1`, warning |
| `TestCommitPlanSpellMissingMetadata` | missing spell EQUID → imported name, blank class/race, `MetadataMissingFg=1`, warning; a present spell EQUID → flag 0 |
| `TestCommitNameSortUsesFinalName` | canonical item name differs from imported name → committed `NameSort` matches the canonical name; staged `ImportList.NameSort` still matches the imported name |
| `TestCommitDates` | UTC calendar date; open/bazaar offsets from settings; no local-time drift at midnight |
| `TestCommitExpansionLimit` | >100,000 expanded records → `422 validation_failed` before writes |
| `TestCommitRollbackKeepsBatchEditable` | write error → batch `Uploaded`, no inventory/audit changes, retry succeeds |
| `TestCommitAuditFailureRollsBack` | failing audit insert → whole commit rolls back |
| `TestCommitDeadlockRetriesOnce` | first attempt `1213`, second succeeds; second failure surfaces 503-class |
| `TestCommitLockTimeout` | `GET_LOCK` returns 0 → 503-class, no transaction |
| `TestCommitDuplicateRequest` | second concurrent commit → `409`, exactly one winner |

### 12.3 API and handler tests

`internal/api/imports_test.go` builds the router with a real `ImportService` over a fake `ImportStore`; this exercises middleware, CSRF and error mapping without a database.

| Test | Asserts |
|---|---|
| `TestImportRoutesRequireAdmin` | unauthenticated → 401/redirect; authenticated non-admin → 403; admin → 200 |
| `TestUploadRequiresCSRF` | header token accepted; multipart hidden `csrf_token` accepted via the bounded parse; missing/incorrect token → 403 |
| `TestSecurityHeadersBodyLimits` | upload path 10 MiB, selection path 2 MiB, other paths 1 MiB; oversized body rejected at the middleware boundary |
| `TestUploadMultipartSuccess` | JSON `201` with batch shape; HTML POST → `303 Location: /admin/imports/:id` |
| `TestUploadNoFile` / `TestUploadWrongField` / `TestUploadTwoFiles` | `422 validation_failed` |
| `TestUploadFilenameSanitization` | Windows path separators, `.`/`..`, control characters and >255 runes are sanitized or rejected; the stored name is bounded |
| `TestUploadTooLarge` | valid CSRF token, 10 MiB+1 → `413 file_too_large`, no batch created |
| `TestUploadParseFailure` | `422 invalid_import_file`, body contains line/field message, batch is retrievable |
| `TestListPagination` | default 50, max 200, `beforeId`, invalid values → `422` |
| `TestBatchNotFound` | 404 with `not_found` envelope and `request_id` |
| `TestRowsPaging` | page/pageSize bounds, filters echo, total independent of page, 0-based edge cases rejected |
| `TestBatchListShape` | `beforeId` pagination, default/max limit, `nextBeforeId` |
| `TestSelectionAndRowIDsShapes` | selection replacement returns canonical summary; row-ids returns a bare ID array plus total |
| `TestRowIDsLocationFilter` | all IDs in group; empty group → empty array, not 404 |
| `TestSelectionBodyValidation` | unknown field, malformed JSON, >50,000 IDs, oversized body → `422`/413 |
| `TestSelectionTerminal` | `409 batch_not_editable` |
| `TestCommitEmptySelection` | `422 selection_empty` with field `selection` |
| `TestCommitSuccessShape` | counts, `duplicateSpellTargets`, `warnings`, `durationMs`, RFC 3339 dates |
| `TestCommitDuplicate` | `409 batch_not_editable`, only one inventory result |
| `TestImportReadyFalse` | import routes → `503 dependency_unavailable`; page renders an unavailable state |
| `TestTemplatesRender` | both pages with fixture view data, no missing field/escape issues; read-only batch hides checkboxes |

### 12.4 Repository integration (opt-in MariaDB)

`internal/integration/import_test.go` follows `integrationMariaDBConfig(t)`: skip unless `GUILDBANK_INTEGRATION=1`, fail if connection variables are missing. It must use a dedicated test database, never a production schema, and clean only rows it created (by batch ID / known EQUIDs).

| Test | Asserts |
|---|---|
| `TestGuildStoreMigrateIdempotent` | first run applies `0001`; second run no-op; version recorded; a concurrent second connection waits for the lock and then no-ops |
| `TestGuildStoreMigrateLock` | holding the migrate lock from another connection and passing a short context deadline makes `Migrate` return promptly without applying a version |
| `TestStageAndReadBack` | counts, ordering by `LocationSort,NameSort,ImportID`, warning JSON, selection defaults |
| `TestRowsSearchLiteralWildcards` | `q` values containing `%`, `_` and `!` match literally; no accidental wildcard expansion |
| `TestSelectionReplacementAtomic` | a forced error inside the replacement transaction leaves the prior selection unchanged; a valid set updates exactly; final selected count asserted, not changed-row counts |
| `TestCommitItemExpansion` | 5-quantity item → 5 rows; metadata/class/race/date fields; `SourceLocation` raw; one summary `ITEM_CREATED`, no per-unit audit |
| `TestCommitSpellMerge` | one existing spell gets summed quantity, `ModAt/ModBy` changed, no new row; audit Update old/new |
| `TestCommitNewSpell` | exactly one active spell row, quantity aggregated from staged rows |
| `TestCommitDuplicateActiveSpell` | lowest ID updated; warning/audit records target |
| `TestCommitAuditRollback` | real store transaction with a failing audit insert leaves zero inventory changes, batch `Uploaded` |
| `TestCommitConcurrent` | two goroutines: one 200, one 409; no duplicate active spells, no orphaned records |
| `TestRecoverInterrupted` | `Processing` → `Failed`; `Uploaded`/`Completed` untouched |
| `TestRollbackOnConstraint` | a forced constraint error (for example an overlong name inserted through the repository) leaves no partial audit/inventory |

### 12.5 Manual UI checklist

No browser automation framework is currently part of the repo, so the following stays a documented manual pass:

1. Upload `InventoryExport-Sample.txt`; history shows `Uploaded`, 548/197, and a link.
2. Detail shows 30 location groups, 197 selected rows, 723 item units and 3 spells.
3. Toggle one row and one whole location; reload the page and verify persistence. Reload after a server restart and verify again.
4. Location checkbox shows indeterminate for a partial group, with a readable count.
5. Filter/search/paging keep selection and summary consistent; no duplicate IDs appear across pages.
6. Commit; confirmation shows unit-aware numbers; after success the page is read-only with the commit counts.
7. Re-submit commit from a stale tab; get 409 and a clear message.
8. Upload a deliberately malformed file; batch shows `Failed` with line number, and the page remains navigable.
9. Upload with an unreachable guild store; import page explains unavailability and other admin pages still work.
10. Keyboard-only: reach upload, checkboxes, location toggle, commit; focus is visible; status announcements are read.
11. 200% zoom and 400 px width: no clipped controls, table scrolls rather than hiding columns.
12. Templates escape a name containing `<script>` and a location containing `&`.

### 12.6 Commands and quality gates

```powershell
go vet ./...
go test ./...
go test -race ./internal/service ./internal/api
go test ./... -cover
GUILDBANK_INTEGRATION=1 go test ./internal/integration -run Import -v
```

The integration command requires the `GUILDBANK_TEST_MARIADB_*` variables and a disposable database. CI without MariaDB still runs every unit, service, handler and template test; integration tests are skipped, matching the existing Items integration policy.

Phase 3 is not mergeable unless:

- every fatal parser code and every warning code has at least one test;
- the golden sample counts match PD3-12 exactly;
- commit rollback, audit failure, deadlock retry and concurrent commit tests pass;
- no new repository import appears in `internal/api` or `internal/web` (the existing import guard test enforces this);
- `go vet` and `go test ./...` are clean.

### 12.7 Acceptance traceability

| Acceptance (§1) | Primary evidence |
|---|---|
| Upload and durable failure record | 12.2 upload tests, 12.3 upload handler tests |
| Location grouping in natural order | golden sample, normalization/sort tests, `TestStageAndReadBack` |
| Selection persistence and restart | selection service/API tests, manual step 3 |
| Unit-aware summary and spell preview | `TestSummaryCounts`, `TestSummaryMergedSpell` |
| Atomic commit, merge, metadata fallback, audit | `TestCommit*`, `TestCommitAuditRollback`, `TestCommitSpellMerge` |
| Read-only completed batch and 409 | `TestSelectionTerminalBatch`, `TestCommitDuplicateRequest`, manual step 7 |
| REST parity | 12.3 route/API tests |
---

## 13. Implementation sequence and file map

No file below exists yet unless marked “modify”. Each step ends with its tests passing; do not begin commit work before the parser/normalizer golden test is green.

| Step | Files | Deliverable |
|---|---|---|
| 1 | `internal/domain/import.go` (new), `internal/domain/item.go` (modify) | statuses, batch/list/location/row/warning, selection and commit result types; `ItemType`/`ItemTypeItem`/`ItemTypeSpell` |
| 2 | `internal/service/import_parser.go`, `import_normalize.go`, `import_sort.go`, `import_classify.go` (new) + tests/fixtures | pure parsing, normalization, sort keys, classification, golden sample |
| 3 | `internal/repository/mariadb/migrations/0001_guild_store.sql`, `migrate.go` (new), `migrate_test.go` | schema, `schema_migrations`, advisory lock, idempotency |
| 4 | `internal/repository/mariadb/imports.go`, `guilditems.go`, `audit.go` (new) + tests | staging, review queries, selection, transaction commit, recovery |
| 5 | `internal/service/import_service.go`, `import_commit.go`, `import_warnings.go` (new) + tests | upload orchestration, selection, summary, commit plan and retries |
| 6 | `internal/api/imports.go` (new), `csrf.go`/`middleware_auth.go`/`router.go`/`Deps` (modify), `imports_test.go`, `csrf_test.go` | handlers, path-aware body limits, multipart CSRF, validation, error mapping, auth, upload/commit tests |
| 7 | `internal/web/templates/imports.html`, `import_detail.html`, `partials/nav.html` (modify), `internal/web/static/imports.js`, `app.css`, `embed.go` (modify) | pages, interaction, responsive styles |
| 8 | `internal/platform/app/app.go`, `internal/repository/mariadb/store.go` (modify), service time-function wiring | service construction, migration/recovery order, degraded mode, HTTP and DSN timeouts |
| 9 | `Docs/ProjectStatus.md`, `Docs/ProjectStructure.md`, `Docs/DatabaseDesign.md` status notes (modify), `README.md` | record Phase 3 complete, routes, migration, test commands |
| 10 | integration tests, manual checklist | final gate |

The dependency direction stays `api/web → service → repository`; `service` declares interfaces, `mariadb` implements them, `app` wires them. No repository import in `api` or `web`.

### 13.1 Definition of done

- Every acceptance item in §1 has an automated test except the visual/accessibility pass in §12.5.
- `ImportBatch`/`ImportList`/`Audit`/`schema_migrations` exist in a migrated test database; `Items` remains untouched.
- A completed batch is read-only and one inventory/audit result exists per committed record.
- Docs reflect the actual column names, route list, limits and operational notes; every former “PROPOSED” item now has an approved status.
- `go vet ./...` and `go test ./...` pass; integration run recorded in the PR description.
---

## 14. Risks and mitigations

| Risk | Likelihood / impact | Mitigation |
|---|---|---|
| Parser misreads rows whose trailing tabs were stripped by a third-party editor | Medium / wrong quantity | Header + 3/4/5-field rules; golden sample; per-row fatal messages; never treat `Slots` as `Count` |
| Sample is not representative of production exports | Medium / wrong grouping | Golden test is regression only; normalization warnings make fallback visible; D-05 remains authoritative |
| `CHECK` constraints are ignored on old MariaDB | Low / bad data | Document MariaDB ≥10.2.1; application validates before write; constraints are defense in depth |
| Migration on a large `GuildBankItems` blocks writes | Medium / downtime | Maintenance window, baseline duration, forward-only file, no destructive change |
| 100,000-row commit holds locks and creates a large audit/transaction | Medium / latency | Expansion limit, advisory lock timeout, one retry, progress log, read-only preview; future increase requires review |
| Metadata changes between pre-resolution and commit | Low / inconsistent class/race | Treat `Items` as static during commit; single metadata snapshot per commit; document operational assumption |
| Second app instance commits concurrently | Low / duplicate spells | DB advisory lock (`GET_LOCK`) on every commit, not a process mutex |
| Server restart during `Processing` | Medium / stranded batch | `RecoverInterrupted` sweep; guarded `FailBatch`; completed batches untouched |
| Selection PUT races a commit | Low / stale set committed | Commit reads persisted selection in the transaction; UI disables commit during PUT; terminal state returns 409 |
| Audit volume overwhelms the table | Low / storage | One `ITEM_CREATED` summary plus one `ITEM_UPDATED` per merged spell (PD3-14) keeps audit proportional to spells, not expanded units; monitoring and retention policy remain operational follow-ups |
| Upload temp files leak on error | Low / disk | `RemoveAll` on every parsed-multipart path: the CSRF middleware defers cleanup across `c.Next()` after a successful parse (covering a later admin rejection or handler early return) and removes immediately on a rejected token/parse error; the upload handler also defers a nil-checked cleanup for its own header-token parse; body cap; code review verifies both paths |
| JavaScript failure blocks review | Medium / unusable page | Upload degrades to a normal form; the detail page server-renders the batch header and failure reason, and requires JS for the summary, location list, row grid, selection and commit. Keep failed batches readable without JS and document the requirement |
| Connection settings changed without restart | Medium / confusing target | Explicit restart notice on the page; hot swap is a separate approved change (`ProjectStructure.md` PD-18 / `ArchitectureReview.md` IC-19) |
| Time-zone drift in dates | Low / off-by-one day | Use the injected time function in UTC, construct calendar dates with `time.Date`, and test at 23:59 UTC |
| `NameSort`/`LocationSort` exceed their column widths | Low / insert failure | Truncate `LocationSort` to 80 and `NameSort` to 160 characters at a rune boundary before staging; test long names, numbers and Unicode |

### 14.1 Resolved review questions

All questions below were resolved by the owner on 2026-11-21 as part of the Phase 3 approval. No Phase 3 review question remains open.

| # | Question | Resolution | Decision |
|---|---|---|---|
| 1 | Add `ImportList.NameSort` or use collation order? | `NameSort` is approved and implemented, including the sort index. | PD3-11 |
| 2 | Write audit through the store or defer to a later audit phase? | Import-scoped audit writes in the same MariaDB transaction are approved and implemented. | PD3-14 |
| 3 | Use `Unspecified` for equipment/handout rows or amend D-05? | `Unspecified` fallback is approved; D-05 remains unchanged. | PD3-01 |
| 4 | Use 120-second HTTP and DSN timeouts? | Approved and implemented. | PD3-13 |
| 5 | Use one `ITEM_CREATED` summary plus one `ITEM_UPDATED` per merged spell? | Approved and implemented. | PD3-14 |
| 6 | Show the import page in degraded mode with upload disabled, or redirect to settings? | The page remains visible and explanatory, with upload and commit disabled. | §14.1 default approved 2026-11-21 |

---

## 15. Review checklist

- [x] Phase numbering is understood: this document is Phase 3, implemented as `ImplementationPlan.md` Phase 6.
- [x] PD3-01–PD3-14 were approved on 2026-11-21 before completion.
- [x] `ImportList.NameSort` and its index/ordering are approved and implemented.
- [x] Staging schema and indexes match `DatabaseDesign.md`; `Items` is read-only.
- [x] Parser rules for 3/4/5 fields, header, section rows, empty IDs and limits are accepted.
- [x] Golden sample counts (548/197/348/2/1/30/194→723/3) are accepted as test expectations and asserted by the parser test.
- [x] Location and name sort keys are approved.
- [x] Spell aggregation/merge, lowest-ID anomaly handling and advisory lock are approved.
- [x] Metadata fallback and commit date/offset rules are approved.
- [x] Audit-in-transaction behavior is approved.
- [x] Route list, JSON envelope, status codes and 409/413/422/503 behavior are approved.
- [x] Multipart CSRF support and the path-aware body limits (10 MiB upload/2 MiB selection/1 MiB otherwise) are approved.
- [x] 120s HTTP/DSN timeouts (PD3-13) are approved.
- [x] UI flow, `imports.js`, accessibility and no-HTMX choice are approved.
- [x] Migration failure is fatal; recovery sweep behavior is approved.
- [x] Test matrix covers every acceptance criterion; integration policy is acceptable.
- [x] Signed off 2026-11-21; implementation completed without starting Phase 4.