# Item quantity model: one row per imported line

**Status:** **approved and implemented** (QTY-01–QTY-12, 2026-11-22).
**Date:** 2026-11-22
**Request:** an imported item line whose `Quantity` is greater than 1 creates
**one** `GuildBankItems` row carrying that quantity instead of being expanded
into `Quantity` rows of 1. Spells keep merge-by-`EQUID`. A later import of the
same non-spell item does **not** merge into an existing row. Manual create
follows the same model, item quantity becomes editable and audited, and
existing expanded rows are consolidated by an owner-run, dry-run-first command.

Implementation evidence, validation commands and before/after examples are in
`Docs/ItemQuantityModelImplementation.md`. The decision record below is the
approved baseline; the review that preceded it is summarised in §2–§4.

## 1. Approved model

| Area | Behaviour |
|---|---|
| Imported item line | One `GuildBankItems` row, `Quantity = line quantity`. No expansion. |
| Repeated `EQUID` in one batch | One row per staged line, each with its own quantity. |
| Later import of the same item | A new row; no lookup, no merge, no update of the existing row. |
| Spells | Unchanged: aggregate by `EQUID`, merge into the lowest active spell row, accumulate quantity, keep dates, warn on duplicate active targets. |
| Manual create | One row with the entered quantity (`1..100000` for Item, column limit for Spell). Spell merge unchanged. |
| Edit | `Quantity` editable for both types: Item `1..100000`, Spell `1..4294967295`, with the existing `quantity` old/new audit pair. |
| Remove / restore | Whole-row soft delete unchanged. Restore of a row absorbed by consolidation is rejected. |
| Schema | `ck_gbi_item_one` dropped; `ck_gbi_quantity (Quantity >= 1)` stays; **no** unique index on `EQUID`; `ConsolidatedInto` marks absorbed rows. |
| Legacy data | Owner-run `-consolidate-items` command, dry-run first; no data mutation in a migration. |

## 2. Why the change was safe

| Fact | Evidence at review time |
|---|---|
| The column was already wide enough | `Quantity INT UNSIGNED NOT NULL` in `0001_guild_store.sql`; domain and admin types use `uint32`. |
| The only blocker was one CHECK | `ck_gbi_item_one CHECK (ItemType = 'Spell' OR Quantity = 1)`; MariaDB 11.4 removes it with `ALTER TABLE … DROP CONSTRAINT` (the MySQL-only `DROP CHECK` form is a syntax error on MariaDB, caught by the SQL validation run). |
| Duplicates were already legal | Only non-unique `ix_gbi_equid` exists, so “no merge on later import” needs no new constraint. |
| Reads do not aggregate | Public and admin projections select `Quantity` per row; totals are `COUNT(*)`. |
| Audit vocabulary was sufficient | `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED` and the `quantity` field are already in the D-10/PD5-14/PD5-15 allowlists. |
| The repository update already supported it | `UpdateInventory` uses `Quantity = COALESCE(?, Quantity)`; only the service gate and form were item-specific. |

## 3. Adopted decision record

| ID | Decision | Status |
|---|---|---|
| QTY-01 | One `GuildBankItems` row per imported item line with that line's quantity; no expansion | **Adopted** |
| QTY-02 | `0005_item_quantity_model.sql` drops `ck_gbi_item_one`, keeps `ck_gbi_quantity`, adds no unique `EQUID` index, adds `ConsolidatedInto BIGINT UNSIGNED NULL` | **Adopted** |
| QTY-03 | `buildCommitPlan` emits one item per staged line; `plan.itemCount` counts rows; spell planning unchanged | **Adopted** |
| QTY-04 | `ImportMaxRows` guards inserted records; `InventoryRecordsToCreate = ItemRows + NewSpells`; `ItemUnits` stays informational | **Adopted** |
| QTY-05 | Import summaries keep `itemUnits` and add `itemRecords`; `createdIds` has one ID per row; `TotalRecordsAffected` uses records | **Adopted** |
| QTY-06 | Review screen, commit help and inventory form describe one record with a stored quantity | **Adopted** |
| QTY-07 | Manual create writes one row with the entered quantity; no expansion | **Adopted** |
| QTY-08 | Item `Quantity` editable (`1..100000`) and audited; form input editable | **Adopted** |
| QTY-09 | Option C consolidation command: dry-run, lowest ID survives, quantity summed, absorbed rows soft deleted, per-row audit, restore rejected, no data mutation in a migration | **Adopted** |
| QTY-10 | Amend D-07/§One Row Per Imported Item Line, PD3-05/06/12/14, PD5-06/07/10, DatabaseDesign, ProjectStructure, R5-14 and ProjectStatus | **Adopted** |
| QTY-11 | Spells, merge-by-`EQUID`, read projections, sorts, indexes and audit vocabulary unchanged | **Adopted** |
| QTY-12 | Migration and code ship together; consolidation only after a clean dry run; rollback is forward-fix only | **Adopted** |

## 4. Before / after examples

| Scenario | Before | After |
|---|---|---|
| Import `Batwing Crunchies` quantity 55 | 55 rows, each `Quantity=1` | 1 row, `Quantity=55` |
| Second import, same `EQUID`, quantity 8 | 8 more single rows; no merge | A second row, `Quantity=8`; the first row is untouched |
| Manual create quantity 55 | 55 rows | 1 row, `Quantity=55` |
| Edit 55 → 52 | impossible for an Item (read-only, `ck_gbi_item_one`) | one `ITEM_UPDATED` with `quantity: 55 → 52` |
| Review screen “Records to create” | sum of item units + new spells | item rows + new spells; units shown separately |
| Legacy 55 single rows, same provenance | 55 list lines | consolidation leaves one row with quantity 55; 54 absorbed rows become Removed with audit |

## 5. Residual risks accepted at approval

1. **Partial give-away.** Removing a stack removes all units. The workaround is editing the quantity down (audited) or a future explicit split action. This gap is documented, not silently mitigated.
2. **Rollback is forward-fix only.** The previous binary rejects item quantity ≠ 1 in `Update` and renders the form read-only, so a stack row is uneditable under a rollback. Deploy migration and code together (QTY-12).
3. **Absorbed rows remain visible as Removed.** D-11 and PD5-13 forbid physical deletion; the admin list keeps them, marked with `ConsolidatedInto`.
4. **Consolidation is an operator action.** It never runs at startup. A skipped consolidation leaves legacy single rows next to new stack rows, which is the documented Option A fallback, not a correctness failure.
5. **Availability dates remain row-level.** A stack row carries the lifecycle dates of the import that produced it; a later import of the same item has its own dates.

## 6. Where the implementation lives

| Concern | File |
|---|---|
| Schema | `internal/repository/mariadb/migrations/0005_item_quantity_model.sql` |
| Import plan/audit | `internal/service/import_commit.go` |
| Summary counts | `internal/service/import_service.go` |
| Manual create/edit/restore gate | `internal/service/admin_inventory.go` |
| Admin projection | `internal/repository/mariadb/admin_inventory.go` |
| Consolidation | `internal/repository/mariadb/item_consolidation.go`, `cmd/guildbank` + `internal/platform/app/app.go` |
| UI copy | `internal/web/templates/import_detail.html`, `internal/web/templates/admin_inventory_form.html` |
| Tests | `import_commit_test.go`, `admin_inventory_test.go`, `admin_lifecycle_test.go`, `migration_files_test.go`, `item_consolidation_test.go`, `internal/web/inventory_ui_test.go` |

Detailed evidence: `Docs/ItemQuantityModelImplementation.md`.