# Item quantity model: implementation summary

**Status:** implemented and validated 2026-11-22 (QTY-01–QTY-12).
**Approval:** owner approved QTY-07, QTY-08 and QTY-09 Option C and adopted the
new quantity model. Design record: `Docs/ItemQuantityModelReview.md`.

This document is the implementation record: what changed, the migration, the
effect on counts, validation evidence, audit evidence and before/after examples.

## 1. Implementation summary

| Concern | Change |
|---|---|
| Import plan | `buildCommitPlan` (`internal/service/import_commit.go`) emits **one** `GuildBankItem` per staged item line with `Quantity = row.Quantity`; the per-unit loop is gone. `plan.itemUnits` is tracked separately from `plan.itemCount`. Spell aggregation, merge target selection, duplicate warning and `AddSpellQuantity` are untouched. |
| Import guard | Still `plan.itemCount + plan.newSpellCount > ImportMaxRows`, i.e. it now counts inserted records. The error text says “the selection would create more than 100,000 inventory records”. |
| Import summary | `InventoryRecordsToCreate = ItemRows + NewSpells`; `TotalRecordsToImport = ItemRows + DistinctSpells` (`internal/service/import_service.go`). `ItemUnits` remains in the summary for scale. |
| Import audit | `IMPORT_COMMITTED` and the summary `ITEM_CREATED` carry `itemUnits` (sum of quantities) **and** `itemRecords` (rows created); merged spell rows keep `ITEM_UPDATED`. No event vocabulary change. |
| Manual create | One row with the entered quantity for both Item and Spell; no expansion. `AdminMaxItemQuantity = 100000` now bounds one quantity value. |
| Manual edit | Item quantity is editable (`1..100000`); spell quantity remains `1..4294967295`. The `quantity` old/new audit pair is written for both types. |
| Restore gate | A row with `ConsolidatedInto IS NOT NULL` cannot be restored; the service returns `409` naming the survivor. |
| Admin projection | `ConsolidatedInto` is selected and mapped into `domain.AdminInventoryItem`. |
| UI copy | Review help says one record per selected line; the admin form always renders an editable quantity and labels the Item type “single record with quantity”; the readonly branch and `QuantityReadOnly` field are removed. |
| Consolidation | New `mariadb.Store.ConsolidateItemStacks` plus the `-consolidate-items` / `-dry-run` / `-consolidate-actor` flags. It runs after migration/backfills and exits before serving HTTP. |

Spells remain exactly as before: staged rows aggregate by `EQUID`, merge into
the lowest active spell row, accumulate quantity, preserve row dates/location
and warn on duplicate active targets.

## 2. Migration summary

`internal/repository/mariadb/migrations/0005_item_quantity_model.sql`:

```sql
ALTER TABLE GuildBankItems DROP CONSTRAINT IF EXISTS ck_gbi_item_one;
ALTER TABLE GuildBankItems ADD COLUMN ConsolidatedInto BIGINT UNSIGNED NULL AFTER DeletedFg;
```

- `ck_gbi_quantity CHECK (Quantity >= 1)` is retained and is the only quantity invariant.
- No unique index is added on `(ItemType, EQUID)`; duplicate stacks are required for “later imports do not merge”.
- `ConsolidatedInto` is the QTY-09 absorbed-row marker; it is `NULL` for every normal row and for survivors.
- The migration contains **no data mutation**. Legacy expanded rows are consolidated only by the owner-run command.
- **Validation finding:** the first draft used `DROP CHECK`, which is MySQL syntax and fails on MariaDB 11.4 with `ERROR 1064 … near 'CHECK ck_gbi_item_one'`. The shipped migration uses `DROP CONSTRAINT`, verified against the development MariaDB 11.4 container.
- `migration_files_test.go` pins the new file and its LF-normalized SHA-256; `Docs/ProjectStructure.md` and `Docs/DatabaseDesign.md` list it.

## 3. Impact on existing inventory counts

Nothing existing changes at deploy time: the migration drops a constraint and
adds a nullable column, so every current row keeps its ID, quantity, dates,
location, recipient and audit history. The visible counts change only through
new writes and through the optional consolidation run.

| Measure | Before | After |
|---|---|---|
| List/detail row count for an imported stack | N rows per N units | 1 row per imported line |
| Admin/public pagination totals | count of unit rows | count of stack rows (drops as new stacks are written) |
| Quantity column for items | always 1 | meaningful 1..100000 (imports bounded by `ImportMaxQuantity = 10000` per line) |
| Review “Item rows” | staged item rows / item units | unchanged: rows and units are still both shown |
| Review “Records to create” | item units + new spells | item rows + new spells |
| Review “Records to import” | item units + distinct spells | item rows + distinct spells |
| Audit `createdIds`/`itemRecords` | one ID per expanded unit | one ID per item row |
| Active-row totals after consolidation | all legacy unit rows | survivor rows only; absorbed rows become Removed |
| Admin sort by quantity | every item quantity 1 | stacks sort by their stored quantity |

Canonical sample arithmetic (PD3-12, reviewable set): **163 item rows / 668
item units** and **3 new spells** create **166 inventory records** after the
change, versus 671 expanded item records before. The full accepted set is 194
item rows / 723 units; parser-level unit counts are unchanged.

`GuildBankItems` row identity is still `GuildBankItemID`; it now identifies a
stack rather than a single unit. Availability dates remain per row, so two
imports of the same item can carry different `DateAdded`/`DateOpen`/`DateBazaar`
values.

## 4. Consolidation (QTY-09 Option C)

`mariadb.Store.ConsolidateItemStacks(ctx, batchSize, dryRun, actor)`:

1. acquires the same per-database advisory lock as import commit/admin writes;
2. selects active `ItemType='Item'` groups with `COUNT(*) > 1` by
   `(EQUID, SourceLocation, LocationGroup, GivenTo, DateAdded, DateOpen, DateBazaar)`,
   ordered by `MIN(GuildBankItemID)` in keyset batches;
3. locks the group (`FOR UPDATE`), keeps the **lowest ID**, sums quantity and
   rejects a sum above `INT UNSIGNED`;
4. updates the survivor, sets `DeletedFg=1` and `ConsolidatedInto=<survivor>` on
   every absorbed row, and inserts audit rows in the same transaction;
5. is idempotent: a second run finds no duplicate group;
6. `-dry-run` reports groups, rows that would be absorbed, units in those groups
   and units remaining, without writing; `units_absorbed` is only meaningful on
   a real run.

**Audit rows**

- Survivor: `ITEM_UPDATED`, `RecordID=<survivor>`, note
  `quantity consolidated from N rows`, old `{quantity: oldSurvivorQty,
  consolidatedFrom: N}`, new `{quantity: sum, absorbedIds: [...], modAt, modBy,
  modByUserId}`.
- Each absorbed row: `ITEM_REMOVED`, `RecordID=<absorbed>`, note
  `consolidated into <survivor>`, old `{quantity, deletedFg: false}`, new
  `{deletedFg: true, consolidatedInto: <survivor>}`.

**Operator runbook**

```text
# 1. deploy the new binary (migrations run automatically, then the process serves)
# 2. rehearse without writing
guildbank -consolidate-items -dry-run
# 3. inspect groups / absorbed_rows / units_in_groups / units_remaining in the log
# 4. apply, recording who ran it
guildbank -consolidate-items -consolidate-actor <operator>
# 5. optional: rerun the dry run; it must report groups=0
```

The command exits instead of starting the HTTP server. A row absorbed by the
run cannot be restored (`409`); this is intentional, because restoring it would
create a phantom single unit outside the consolidated stack.

## 5. Validation evidence

Commands run on 2026-11-22 (Windows, Go 1.27.3, MariaDB 11.4):

| Check | Result |
|---|---|
| `gofmt -w cmd internal` | clean |
| `go vet ./...` | clean |
| `go test ./...` | all packages pass: api, integration (skipped opt-in), platform/app, config, logging, repository/mariadb, repository/sqlite, service, web |
| `TestBuildCommitPlanCreatesOneRowPerLineAndAggregatesSpells` | 2 item lines → 2 rows, quantities 2 and 1; `itemUnits=3`; spells unchanged |
| `TestBuildCommitPlanNeverMergesRepeatedItemEQUID` | import 55 → one row qty 55; later import 8 → a separate row qty 8; no merges |
| `TestCommitAuditEntriesSeparateUnitsAndRecords` | summary has `itemUnits=63` and `itemRecords=2` for two rows totalling 63 |
| `TestAdminCreateItemWritesOneRowWithQuantity` | create qty 3 → `CreatedCount=1`, one ID, stored quantity 3 |
| `TestAdminUpdateItemQuantityIsEditableAndAudited` | 1 → 55 stored and audited as `quantity` old/new |
| `TestAdminUpdateRejectsItemQuantityAboveCap` | 100001 → `422 quantity` |
| `TestAdminRestoreRejectsConsolidatedRow` | absorbed row restore → `409`, no audit |
| `TestConsolidationAuditWritesSurvivorAndAbsorbedRows` | survivor `ITEM_UPDATED` sum/`absorbedIds`; absorbed `ITEM_REMOVED`/`ConsolidatedInto` |
| `TestMigrationManifest` | 0005 is pinned, contains `DROP CONSTRAINT IF EXISTS ck_gbi_item_one` and `ConsolidatedInto`, and keeps `ck_gbi_quantity` |
| `TestAdminTemplatesExistAndCarryCSRF` | quantity input editable, new help text present, no `readonly` |

SQL validation against a disposable `GuildBankQtyCheck` schema on the running
MariaDB 11.4 container (created, exercised, then dropped):

| Step | Result |
|---|---|
| Create the 0001 `GuildBankItems` shape with `ck_gbi_item_one` | table created |
| `ALTER TABLE … DROP CHECK ck_gbi_item_one` (first draft) | **failed** `ERROR 1064`; migration corrected to `DROP CONSTRAINT` |
| `DROP CONSTRAINT` (without `IF EXISTS`) after the table already lacked the constraint | **failed**; the shipped form is `DROP CONSTRAINT IF EXISTS` |
| `DROP CONSTRAINT IF EXISTS ck_gbi_item_one` + `ADD COLUMN ConsolidatedInto` | success; `ConsolidatedInto bigint(20) unsigned YES NULL`; a second `DROP CONSTRAINT IF EXISTS` is also a no-op |
| Insert `Item` row with `Quantity=55` | success (row 6, quantity 55) |
| Insert `Item` row with `Quantity=0` | rejected `ERROR 4025 … CONSTRAINT ck_gbi_quantity failed` |
| Duplicate-group query on 5 seeded rows | one group: survivor 1, count 2, sum 2, recipient `Guild Bank`; the NULL-recipient row, removed row and different-`DateAdded` row stayed separate |
| Consolidation transaction (survivor quantity 2; absorbed row soft deleted with `ConsolidatedInto=1`) | rows: 1 qty 2 active; 2 qty 1 removed into 1; others unchanged |
| Re-run duplicate-group query | `duplicate_groups 0` (idempotent) |
| Audit inserts | `ITEM_UPDATED` record 1 note `quantity consolidated from 2 rows`; `ITEM_REMOVED` record 2 note `consolidated into 1` |

Live Compose validation on 2026-11-22 (rebuilt `guildbank:dev`, MariaDB 11.4):

| Step | Result |
|---|---|
| `docker compose build guildbank` | image built (`BUILD_EXIT=0`) |
| `docker compose run --rm -T --no-deps guildbank -consolidate-items -dry-run` | exit 0; startup reported `schema_version=0005_item_quantity_model`; location backfill wrote 0 rows |
| Dry-run log | `groups=39`, `absorbed_rows=795`, `units_in_groups=834`, `units_absorbed=0`, `units_remaining=834`, `dry_run=true` |
| Statement before the dry run | 874 active item rows / 874 units; 39 duplicate groups. Projected post-run state: 79 active rows, 874 units |
| Schema after deployment | `0005` recorded in `schema_migrations`; `ConsolidatedInto` present; only `ck_gbi_quantity`, `ck_gbi_deleted`, `ck_gbi_meta_missing`, `ck_gbi_bazaar` and `ck_gbi_open` remain as CHECK constraints |
| Audit after the dry run | 0 rows with `Notes LIKE '%consolidat%'`, proving the rehearsal wrote nothing |
| Restart on the new image | container healthy; `/healthz` returned `200` |

The dry run was **not** followed by a real run: applying consolidation is the
owner's separate, actor-recorded step (QTY-09 Option C). The development store
still holds the 874 legacy rows until that run is authorized.

The opt-in full-table Items integration test and the Phase 4 benchmarks were not
re-run; this change does not touch the Items repository or the public query
paths.

## 6. Audit evidence

| Operation | Event | Payload (redacted allowlist) |
|---|---|---|
| Import commit | `IMPORT_COMMITTED` | old `{status: Uploaded}` → new `{status: Completed, selectedRows, itemUnits: 668, itemRecords: 163, spellsCreated: 3, spellsMerged, spellQuantity, duplicateSpellRecords, durationMs}` |
| Import commit | `ITEM_CREATED` (batch `RecordID`) | `{itemUnits: 668, itemRecords: 163, spellsCreated: 3, spellsMerged, batchId}`; per-row IDs are not written for items (PD3-14 unchanged) |
| Merged spell | `ITEM_UPDATED` | unchanged: old quantity, new summed quantity, batch/import/location provenance |
| Manual create | `ITEM_CREATED` | one row: `{quantity, equid, itemType, name, sourceLocation, givenTo, metadataMissing, dateOpen, dateBazaar, createdIds: [id]}` |
| Manual item quantity edit | `ITEM_UPDATED` | `{quantity: 1}` → `{quantity: 55}` (only changed fields) |
| Remove / restore | `ITEM_REMOVED` / `ITEM_UPDATED` | unchanged whole-row soft-delete payload |
| Consolidation survivor | `ITEM_UPDATED` | `{quantity: old, consolidatedFrom: N}` → `{quantity: sum, absorbedIds: [...], modAt, modBy}` |
| Consolidation absorbed | `ITEM_REMOVED` | `{quantity, deletedFg: false}` → `{deletedFg: true, consolidatedInto: survivor}`, note `consolidated into <id>` |

All consolidation rows are written in the same transaction as the row changes;
no mutation exists without history (D-10/PD5-13). Consolidated rows are marked
by `ConsolidatedInto`, not by parsing free text.

## 7. Before / after examples

**Import**

```text
Before: Batwing Crunchies  Qty 55
        55 GuildBankItems rows, each Quantity = 1

After:  Batwing Crunchies  Qty 55
        1 GuildBankItems row, Quantity = 55
```

**Two imports of the same item (no merge)**

```text
Import 1: Batwing Crunchies  Qty 55   -> Row A  Quantity 55
Import 2: Batwing Crunchies  Qty 8    -> Row B  Quantity 8
```

**Manual create and edit**

```text
Before: create Qty 55 -> 55 rows; edit quantity -> read-only / 422
After:  create Qty 55 -> 1 row;      edit 55 -> 52 -> one ITEM_UPDATED
```

**Legacy consolidation**

```text
Before: rows 1..55, same EQUID/location/recipient/dates, each Qty 1
After:  row 1 Qty 55 active
        rows 2..55 DeletedFg=1, ConsolidatedInto=1
        audit: ITEM_UPDATED on 1; ITEM_REMOVED on 2..55 with notes
```

**Spell (unchanged)**

```text
Staged: Spell EQUID 20 qty 1 + qty 2, existing active spell row qty 4
After:  no new row; existing row qty 7; one ITEM_UPDATED (quantity 4 -> 7)
```

## 8. Documentation updated

`Design.md` (§One Row Per Imported Item Line and D-07), `ImplementationPlan.md`,
`Docs/Phase3ImplementationPlan.md` (PD3-05/06/12/14 and the commit test list),
`Docs/Phase5ImplementationPlan.md` (PD5-06/07/10, the admin route table and
R5-14), `Docs/DatabaseDesign.md` (DDL and notes), `Docs/ProjectStructure.md`
(migration list, repository file, startup flow and decisions), `Docs/ProjectStatus.md`
(status rows, D-07, decision section 6.7 and change log 2.6),
`Docs/ItemQuantityModelReview.md` (approved decision record), `README.md`
(consolidation runbook and test wording), and this implementation record.

## 9. Residual risks

1. **Partial give-away.** Removing a stack removes every unit; the correction
   path is an audited quantity edit (or a future split action). Documented, not
   implemented.
2. **Forward-fix rollback only.** The previous binary rejects item quantity ≠ 1
   on update, so do not roll back after stack rows exist.
3. **Consolidation is explicit.** If it is not run, legacy single rows remain
   beside new stacks (Option A behaviour); no correctness impact.
4. **Absorbed rows stay visible as Removed.** Required by D-11/PD5-13.
5. **Quantity per row is bounded by the parser for imports (10,000) and by
   `AdminMaxItemQuantity` (100,000) for manual writes, while the column supports
   4,294,967,295; the consolidation sum check rejects an over-range group rather
   than truncating it.**

## 10. Requirement traceability

| Requirement | Evidence |
|---|---|
| Imported item preserves quantity | `buildCommitPlan`; `TestBuildCommitPlanCreatesOneRowPerLineAndAggregatesSpells`; SQL qty-55 insert |
| Spells unchanged | spell tests unchanged and passing; `TestBuildCommitPlanMissingMetadata`/`AppliesSpellMetadata` |
| Later imports do not merge | `TestBuildCommitPlanNeverMergesRepeatedItemEQUID`; no item lookup in `active`; no unique index |
| Manual create follows the model | `createAttempt`; `TestAdminCreateItemWritesOneRowWithQuantity` |
| Item quantity editable and audited | `updateAttempt`; `TestAdminUpdateItemQuantityIsEditableAndAudited`; audit allowlist |
| QTY-09 Option C consolidation | `item_consolidation.go`; `TestConsolidationAudit…`; SQL dry-run/group/update/idempotency evidence |
| Docs and status updated | §8 and `ProjectStatus.md` §6.7/change log 2.6 |