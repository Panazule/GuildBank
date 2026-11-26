# Admin Inventory: Movement-Derived Issued Rows (Implementation Plan)

**Status:** **Executed and delivered (2026-11-25).** The behavioural specification is
`AdminInventoryIssuedRowsDesign.md`. Owner decisions D1–D6 are frozen, were implemented as
specified and were re-confirmed at implementation review. This document is now the
as-built record: the change list is section 2, the executed checks are section 3, the
backfill status is section 4 and the deployment incident is section 5.

**Scope:** the Admin Inventory page only. Public inventory, imports, on-hand
totals and the JSON API contract **shape** are unchanged (D3); see
`AdminInventoryIssuedRowsDesign.md` section 7.4 for the one content-level consequence of
serving HTML and JSON from the same admin list dispatch.

## 1. Build sequence (as executed)

Deployment order is migrate -> attribution backfill -> projection/return route
(design section 11). The build sequence below followed that order, except that the
attribution backfill was not executed: it is an operator step over live data and remains
open (section 4). The projection and the movement-targeted return route therefore went
live first, which is safe for issuances created by the new code and leaves pre-deployment
returns under-attributed until the backfill runs.

1. Migration `0007_movement_related_movement` (additive schema).
2. Domain contract (`RelatedMovementID`, `ReturnIssuanceInput`, issued-row view
   fields).
3. Repository: movement column/insert/scan, issued projection and `All` union,
   write-transaction helpers.
4. Service: `ReturnIssuance` plus FIFO attribution on the lot-level return.
5. API/views/templates: routes, handler, view fields, template branching.
6. Documentation/status updates.

## 2. File-level change list

| # | File | Change |
| --- | --- | --- |
| 1 | `internal/repository/mariadb/migrations/0007_movement_related_movement.sql` | new: nullable `RelatedMovementID`, index `ix_movement_related_movement`, self-FK `fk_movement_related_movement` |
| 2 | `internal/repository/mariadb/migration_files_test.go` | pin the `0007` bytes and SHA-256; assert the `NNNN_name` shape, the 32-character version limit and the column/index/FK strings |
| 3 | `internal/domain/movement.go` | add `RelatedMovementID *int64` to `InventoryMovement` + validation; add `ReturnIssuanceInput` |
| 4 | `internal/domain/inventory_admin.go` | add `MovementID`, `IssuedQuantity`, `IssuedAt`, `IssuedBy` to `AdminInventoryItem` |
| 5 | `internal/repository/mariadb/movement.go` | movement projection/insert/scan gain `RelatedMovementID`; add `GetMovementOutstanding` |
| 6 | `internal/repository/mariadb/admin_inventory.go` | `ListAdminInventory` dispatches on `filter.Status`; add `ListIssuedRows` and the `All` union; add `LockMovementOutstanding` and `EarliestOutstandingIssue` write helpers |
| 7 | `internal/service/admin_inventory.go` | add `GetMovementOutstanding` to `AdminStore`; add `LockMovementOutstanding`/`EarliestOutstandingIssue` to `AdminWriteTx`; add `IssuanceForReturn` |
| 8 | `internal/service/admin_movement.go` | attribute `RelatedMovementID` on every return branch; add `ReturnIssuance` |
| 9 | `internal/api/router.go` | add `GET|POST /admin/inventory/movements/:movementID/return` |
| 10 | `internal/api/admin_movement_pages.go` | add `returnIssuancePage`/`returnIssuanceForm`; reuse `renderMovementForm` |
| 11 | `internal/api/views.go` | `adminItemView` gains `MovementID`, `MovementBacked`, `RowKey`, `IssuedQuantity`, `IssuedAtISO`/`IssuedAtText`, `IssuedBy`, `ReturnURL` |
| 12 | `internal/api/admin_pages.go` | populate the new view fields from the issued-row domain fields |
| 13 | `internal/web/templates/admin_inventory.html` | issued-row action branch (`Return`/`History`/`Movements`); suppress the bulk-selection checkbox for movement rows |
| 14 | `internal/web/templates/admin_inventory_return.html` | accept the movement-targeted form action |
| 15 | fakes/tests | extend `fakeAdmin` and `movementFakeStore` with the new interface methods; add projection and return tests |

## 3. Verification (executed 2026-11-25)

| Check | Result |
| --- | --- |
| `gofmt -l .` | no output |
| `go build ./...` | pass |
| `go vet ./...` | pass |
| `go test -count=1 ./...` | pass for every package |
| `TestMigrationManifest` | pass; `0007` bytes pinned, `NNNN_name` shape and the 32-character version limit asserted |
| Repository tests | outstanding with 0/1/many returns; zero-outstanding exclusion; `All` duplicate suppression; `id` filter matching both lot and movement ids; page/total snapshot |
| Service tests | return-by-movement for active, whole-issued, partial-issued and administratively removed lots; over-return `422`; stale token `409`; idempotency replay; FIFO attribution on the lot-level return |
| API tests | new routes require auth and CSRF; `Return` present only on issued rows; `Restore` unchanged |
| Live Compose deployment | `schema_migrations` records `0007_movement_related_movement`; column, index and FK present; `schema_version=0007_movement_related_movement` logged; `/healthz` `200` |
| Manual acceptance pass (design section 12) | **not executed** — open item (design section 15.4) |

## 4. Attribution backfill (one-time, idempotent)

Existing `RETURN` rows carry `RelatedMovementID = NULL`, so the projection would
overstate outstanding on their `ISSUE` movements. The deployment therefore runs
the FIFO reconciliation in design section 11 before the projection goes live:
each unattributed `RETURN` is attributed to the earliest `ISSUE` movement on the
same `GuildBankItemID` whose outstanding still exceeds the returned quantity.
The application also attributes every new return at write time, so the backfill
is only needed once and is safe to re-run.

**Status: not executed.** No backfill entry point was added to the application, so this
remains an operator-run procedure against a live store. At review the development store
held 15 `ISSUE` and 7 `RETURN` movements: 2 returns written by the new code carry a link,
and 5 pre-deployment returns totalling 19 units remain `NULL` (lot 891 ×2, lot 970 ×1 of 15
units, lot 1041 ×2). Until the reconciliation runs, `Issued` overstates outstanding for
those three lots. Run it before exposing `Issued` to officers on a store with history, then
re-run `go test -count=1 ./...`.

## 5. Deployment incident and recovery: `0007` version overflow (2026-11-25)

### 5.1 Symptom

The first deployment of `0007` started, then exited fatally and crash-looped:

```text
app: migrate guild store: mariadb: record migration
0007_inventory_movement_related_movement: Error 1406 (22001):
Data too long for column 'Version' at row 1
```

Every later start failed earlier, inside the migration file:

```text
app: migrate guild store: mariadb: migration
0007_inventory_movement_related_movement: Error 1060 (42S21):
Duplicate column name 'RelatedMovementID'
```

### 5.2 Root cause

`migrate.go` derives the version from the embedded file path
(`strings.TrimSuffix(strings.TrimPrefix(path, "migrations/"), ".sql")`) and records it in
`schema_migrations.Version`, declared `VARCHAR(32)`. The original name
`0007_inventory_movement_related_movement` is **40 characters**, so:

1. every statement in the file executed and each DDL statement auto-committed — the
   column, the index and the self-FK all existed;
2. `INSERT INTO schema_migrations` failed with 1406, so the version was never recorded;
3. the next startup saw the file as unapplied, re-ran it, and the first `ALTER TABLE`
   failed with 1060.

MariaDB DDL is not transactional, which is why the schema and the bookkeeping diverged.
Every earlier migration name is 16–29 characters, so the limit had never been exercised.

### 5.3 Inspection (read-only)

```sql
SELECT Version, AppliedAt FROM schema_migrations ORDER BY Version;
SHOW CREATE TABLE schema_migrations;

SELECT ORDINAL_POSITION, COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'InventoryMovement'
  AND COLUMN_NAME = 'RelatedMovementID';

SELECT INDEX_NAME, COLUMN_NAME FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'InventoryMovement'
  AND INDEX_NAME = 'ix_movement_related_movement';

SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
FROM information_schema.KEY_COLUMN_USAGE
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'InventoryMovement'
  AND CONSTRAINT_NAME = 'fk_movement_related_movement';

SELECT COUNT(*) FROM InventoryMovement WHERE RelatedMovementID IS NOT NULL;
```

Observed on 2026-11-25: `0001`–`0006` recorded and `0007` absent; `Version varchar(32)`;
column, index and FK all present; 0 non-NULL values across 28 movement rows.

### 5.4 Options and decision

| Option | Verdict |
| --- | --- |
| Shorten the file name to ≤ 32 characters | **chosen** — no schema change, no divergence from the documented `VARCHAR(32)`, and the runner records the migration itself |
| Widen `schema_migrations.Version` | rejected — contradicts `DatabaseDesign.md` §8 and `Phase3ImplementationPlan.md` §9.2, and `CREATE TABLE IF NOT EXISTS` does not retrofit existing databases, so every environment would need out-of-band DDL |
| Record the version by hand | rejected — the version is derived from the file name on every run, so a hand-written (necessarily truncated) value never matches; the file re-runs and fails with 1060 again |

### 5.5 Recovery (executed)

```powershell
docker compose stop guildbank
```

```sql
-- Run only the statements whose object exists (all three existed here.)
ALTER TABLE InventoryMovement DROP FOREIGN KEY fk_movement_related_movement;
ALTER TABLE InventoryMovement DROP KEY ix_movement_related_movement;
ALTER TABLE InventoryMovement DROP COLUMN RelatedMovementID;
```

Order matters: the FK before its supporting index, the column after both. The column was
dropped only after `SELECT COUNT(*) … WHERE RelatedMovementID IS NOT NULL` returned `0`,
so the revert was lossless.

Then rename and redeploy, so the runner applies and records the migration itself:

```powershell
Rename-Item -LiteralPath 'internal\repository\mariadb\migrations\0007_inventory_movement_related_movement.sql' -NewName '0007_movement_related_movement.sql'
docker compose up -d --build guildbank
```

Result: `schema_migrations` gained `0007_movement_related_movement`; the runner re-created
the column, index and FK; the log reports
`guild store schema ready … schema_version=0007_movement_related_movement` and `/healthz`
returns `200`.

**Any other environment that already attempted the old name** holds the same partial state
and will crash-loop on this build; run the same three `DROP` statements there, after the
non-NULL check, and restart. No environment can hold the old version string, because
recording it is exactly what failed.

### 5.6 Prevention

- `migrate.go` rejects any migration whose version exceeds `schemaMigrationsVersionMaxLen`
  (32) **before executing any statement**, so an over-long name fails fast with a clear
  message instead of half-applying.
- `TestMigrationManifest` fails the build if any embedded migration name exceeds 32
  characters, alongside the existing `NNNN_name` and byte-manifest assertions.
- The `0007` manifest entry was re-pinned after the rename: the pinned hash covers the file
  bytes, which include the header comment naming the file.
