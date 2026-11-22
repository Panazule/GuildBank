# Import and Administrative Inventory Improvements (post-Phase-5)

**Status:** implemented and validated in the working tree on 2026-11-22;
follow-up review adjustments **IMP-A/IMP-B/IMP-C** and the **DEFECT-01** spell
metadata correction were added the same day and live-validated.
**Scope:** the owner-approved review items **IMP-01…IMP-06**, **AI-01** and
**AI-02**, plus the follow-up review of 2026-11-22 (**IMP-A/IMP-B/IMP-C**) and
the **DEFECT-01** spell metadata fix. No phase was reopened; the Phase 3 commit
path, the advisory lock, the closed audit vocabulary and the Phase 5
single-record contracts are unchanged.

## 1. Approved decisions

| ID | Classification | Approved behaviour |
|---|---|---|
| IMP-01 | Latent defect | A row whose Location is a bank slot nested inside another slot (`General 1-Slot2-Slot1`) is the contents of a container. It is ignored at staging and counted separately (`IgnoredRowCount`); it is never staged as `Unspecified`. |
| IMP-02 | Presentation | `LocationGroup` keeps the stored value `Unspecified` internally, and admin inventory still labels it **Other (not a bank location)**. Import review no longer presents or exposes the group at all: those rows are excluded from the location selector, the staged-row list, the selection and the commit (IMP-C). |
| IMP-03 | Usability | Transparency for the default rule: the review grid shows a **Kind** column (`Slot` / `Container`) and the panel explains the container exclusion. The former Other-group banner is removed with the group itself (IMP-C). |
| IMP-04 | Usability / behaviour change | Default selection is computed at staging. Container rows in General, Bank and SharedBank are not selected; other rows are. This is non-retroactive: existing batches keep their persisted selection. **Reset to defaults** recomputes the rule on demand. |
| IMP-05 | Usability | Hoard is a special location family: the container exclusion does **not** apply, so every Hoard row is selected by default. |
| IMP-06 | Usability | The location panel is the page-local group scope. Unchecking a group hides its rows **and** clears its rows from the selection. The `Unselected` row filter ignores the scope (`ignoreGroups=1`) so no row is lost from review. The scope is never persisted. |
| IMP-A | Decision | The Scope concept (IMP-06) is retained unchanged. |
| IMP-B | Usability | Two page-local actions above the scope actions: **Check all locations** and **Uncheck all locations**. They change only the location-group checkboxes (the review scope); row selection and scope actions are untouched. |
| IMP-C | Behaviour change | `Unspecified` is internal only. Rows classified Other/Unspecified are not listed in the import review, never appear in the location selector or the staged-row location column, cannot be selected (the selection API rejects them with `422`), and are never committed. The Other banner is removed. Their staging still counts toward `AcceptedRowCount`. |
| AI-01 | Usability | Page-local bulk removal only: a checkbox column on the current inventory page (≤200 rows), a preview page, then one all-or-nothing transaction. No filter-wide or inventory-wide action. |
| AI-02 | Usability | "Save and remove" performs the editable-field update and the soft delete in one transaction, writing `ITEM_UPDATED` (only when fields changed) followed by `ITEM_REMOVED`, both under the same correlation ID. Restoring later does not undo the edits. |

## 2. Implementation map

| Area | Change |
|---|---|
| Parser | `nestedSlotLocation` rule in `internal/service/import_parser.go`; `ParseResult.IgnoredRows`. The rule runs after the ID/name checks and before quantity parsing, so the sample's 139 empty nested rows remain ordinary discards. |
| Defaults | `internal/service/import_selection.go`: `IsContainerLocation` and the single `DefaultImportSelection` rule used by staging and by the reset endpoint. |
| Staging | `internal/service/import_upload.go` sets `SelectedFg` from the rule, returns `UploadResult.IgnoredRows`, and writes `ignoredRows` to the `IMPORT_CREATED` audit payload. `ImportStore.StageBatch` and the MariaDB transaction persist the count. |
| Schema | `0004_import_ignored_rows.sql` adds nullable `ImportBatch.IgnoredRowCount` (NULL on legacy batches, read as 0). The migration hash manifest pins the new file. |
| Review reads | `ImportRowFilter` gains `ExcludeGroups` and `IgnoreGroups`; `RowIDs` accepts the same exclusion list. The handler parses `excludeGroups`/`ignoreGroups`; the row DTO adds `container`. IMP-C adds one repository predicate (`LocationGroup <> 'Unspecified'`) to `ListLocations`, `ListRows`, `RowIDs`, `SelectedIDs`, `SelectedTotals`, `SpellPreview` and the commit transaction's selected-row read. |
| Review API | New `GET /api/v1/imports/{id}/default-selection` returns the recomputed default IDs. Selection still uses the existing full-replacement `PUT`; IMP-C makes that endpoint reject an Other row with `422` ("Other location rows cannot be selected for import"). |
| Review UI | `imports.js`: group scope, Kind column, scoped select/clear, Reset to defaults, and the IMP-B **Check all locations** / **Uncheck all locations** scope-only actions. The Other label, Other banner and `renderOtherBanner` are removed (IMP-C). `import_detail.html` adds the two new buttons above the scope actions and no longer mentions Other. |
| Spell metadata | `buildCommitPlan` now applies the same resolved `ItemMetadata` to a new spell row that it applies to an item row: authoritative `Name`, decoded `ClassString`/`RaceString`, and `MetadataMissingFg` only when the EQUID is unresolved (DEFECT-01). `BackfillSpellMetadata` repairs rows written before the fix. |
| Admin UI | `adminGroupLabel` presents Other; the list adds a checkbox column and a page-local bulk form; `admin_bulk_remove.html` is the preview; the edit form adds **Save and remove**. |
| Admin service | `UpdateItemInput.Remove`; `Update` writes edit + removal in one transaction; `PreviewRemoveMany` and `RemoveMany` add the page-local bulk path with ascending-ID locking and one audit row per record. |
| Admin API | `remove` is an update-body key; new `POST /admin/inventory/bulk-remove` (preview) and `POST /admin/inventory/bulk-remove/apply` (apply). The update response adds `fieldsChanged`. |
## 3. Import count impact (sample)

`Docs/Samples/InventoryExport-Sample.txt`: 548 physical lines → 197 accepted
rows, 348 discards, 2 structural rows, 1 blank line.

- The 139 nested-slot lines are all `Empty | 0`; they remain **discards**, so
  `IgnoredRowCount = 0` and every existing count is unchanged.
- A populated nested row (`ID ≠ 0`) now increments `IgnoredRowCount` instead of
  being staged as `Unspecified`.
- Default selection for a newly uploaded sample changes from **197** selected
  rows to **138**: 49 General slot rows + 74 Bank + 12 SharedBank + 3 Hoard
  containers. The 60 container rows (10 General, 16 Bank, 2 SharedBank, 3 Hoard,
  29 Other) are not selected, except the 3 Hoard rows. Unspecified rows are
  never selected; the Other group's 2 slot rows are therefore also excluded.
- Existing batches are untouched; Reset to defaults is the only way an existing
  batch adopts the new rule.
- IMP-C subtracts the 31 `Unspecified` rows from the review without changing the
  batch: upload still reports **197 accepted**, but `/locations` returns **29**
  groups, `/rows` and `/row-ids` return **166** rows, and `summary.stagedRows`
  is **166**. The default selection stays **138** (no Other row was ever
  default-selected). A batch that already has `SelectedFg=1` on an Other row
  (possible on a legacy batch) no longer exposes, returns or commits it, and the
  selection `PUT` rejects such an ID.

## 4. Audit impact

- Vocabulary unchanged: `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED`,
  `IMPORT_CREATED`, `IMPORT_COMMITTED`. There is no `ITEM_BULK_REMOVED`.
- `IMPORT_CREATED` gains `ignoredRows` in its `NewValues` JSON.
- AI-01: N × `ITEM_REMOVED`, one per record, all sharing the request correlation
  ID and one `EventDate`.
- AI-02: `ITEM_UPDATED` (only when editable fields changed) then `ITEM_REMOVED`,
  same correlation ID, same `EventDate`, same transaction. The update payload
  contains only changed fields; the removal payload is the existing
  `{deletedFg:false}` → `{deletedFg:true}` pair.
- Bulk removal re-validates every `expectedModAt` under `FOR UPDATE`; a single
  stale or already-removed record rolls back the whole selection and writes no
  audit row.

## 5. Validation evidence

Commands (working tree, 2026-11-22):

- `gofmt -l cmd internal` → no output.
- `go vet ./...` → exit 0.
- `go test -count=1 ./...` → exit 0; all packages `ok`.

New targeted tests:

- `internal/service/import_selection_test.go`: container detection and the
  default rule, including the Hoard exception and the never-select Other rule.
- `internal/service/import_parser_nested_test.go`: populated nested rows are
  ignored and counted; the canonical sample still reports 0 ignored and 348
  discards; the sample default set is 138 rows and includes exactly 3 Hoard
  rows.
- `internal/service/admin_bulk_remove_test.go`: edit+remove writes two audit rows
  with one correlation ID; remove-only update writes just `ITEM_REMOVED`;
  bulk removal removes all selected rows in one commit; a stale token rolls back
  everything (no deletion, no audit); duplicate/empty selections are rejected;
  preview rejects removed and stale records.
- `internal/api/admin_bulk_test.go`: the admin list renders the bulk form and the
  `id|modAt` token; the preview renders the apply form; token parsing rejects
  empty, malformed, invalid-version and duplicate selections.
- `internal/repository/mariadb/migration_files_test.go`: the manifest now pins
  `0004_import_ignored_rows.sql` and all four hashes are LF-normalized, so the
  gate passes on both the LF repository/Linux build and a CRLF Windows checkout.
  The previous manifest was pinned to CRLF bytes while the repository stores LF,
  which would have failed the Ubuntu CI gate.
- `internal/api/imports_test.go` (IMP-C): `TestImportReviewExcludesOtherLocationRows`
  proves the locations, rows, row-ids, selection and summary endpoints omit
  `Unspecified` rows (including a legacy `SelectedFg=1` row), that
  `stagedRows`/`totalLocations` count only reviewable rows, that the selection
  `PUT` rejects an Other row with `422`, and that the detail page has the IMP-B
  buttons and no Other banner/label.
- `internal/service/import_commit_test.go` (DEFECT-01):
  `TestBuildCommitPlanAppliesSpellMetadata` proves a new spell row receives the
  resolved authoritative name plus decoded `ClassString`/`RaceString`, and the
  existing metadata-missing test still keeps the imported name with blanks.
- `internal/repository/mariadb/spell_metadata_backfill_test.go` (DEFECT-01):
  fills blank columns, never overwrites populated ones, never blanks a zero mask
  and rejects a malformed mask.
- The existing API import tests continue to pass with the extended fake store
  (group scope, ignored count and the Other exclusion).

## 6. Live Compose verification (2026-11-22)

The app image was rebuilt and the Compose stack restarted; the guild store
reported `schema_version=0004_import_ignored_rows`, so the new migration applied
to the existing database without backfill work.

Upload of the canonical sample through `POST /api/v1/imports` (batch 8):

- `acceptedRowCount = 197`, `ignoredRows = 0`, `ignoredRowCount = 0`.
- `summary.selectedRows = 138`, `default-selection.total = 138`,
  `totalRecordsToImport = 643`.
- `/rows` returned 197 rows and a `container` flag; the first row was a
  `Bank 1` container.
- `/rows?selected=unselected&ignoreGroups=1` returned 59 (the review escape
  hatch); `/rows?excludeGroups=Unspecified,Hoard` returned 163.
- The import detail page contained `Ignored rows`, the `Kind` header,
  `id="reset-defaults"` and `id="other-location-banner"` (the banner was later
  removed by IMP-C; see §7).

Admin inventory page:

- `GET /admin/inventory` returned 200 with
  `action="/admin/inventory/bulk-remove"`, an `id|modAt` checkbox token and the
  `Other (not a bank location)` filter label.

AI-02 (record 736, created for the probe):

- `PUT /api/v1/items/736` with a changed `givenTo` and `"remove": true` returned
  200; `deleted=true`, `changed=true`, `fieldsChanged=true`.
- History had three entries (creation plus the two writes); the newest two were
  `ITEM_REMOVED` then `ITEM_UPDATED` with the same correlation ID
  `25ebfb0665a24bbaaf8709f54882d2e4`.

AI-01 (records 737 and 738):

- The preview POST returned 200 with the apply form and both tokens.
- The apply POST returned `303 /admin/inventory?notice=bulk-removed&count=2`.
- Both records then reported `deleted=true`; each history's newest entry was
  `ITEM_REMOVED`, and both shared correlation ID
  `a2bda8d0804e5526fa5430b665b95561`.
- The notice page returned 200 and rendered `2 records were removed.`

The probe records were created only for this validation and are soft-removed;
the active inventory total was 713 before and after the run. No import batch was
committed, so inventory content is unchanged.

## 7. Follow-up review adjustments (IMP-A/IMP-B/IMP-C, 2026-11-22)

The owner accepted the Scope concept (IMP-A) and asked for two adjustments. No
phase was reopened and no schema change was needed.

- **IMP-B** — `import_detail.html` renders **Check all locations** and **Uncheck
  all locations** in their own action row above the existing scope actions.
  `imports.js` implements `setAllLocationsScope`: it sets every location-group
  checkbox in or out of the page-local scope and reloads the grid. It never
  touches the persisted row selection; **Select all in scope**, **Clear scope**
  and **Reset to defaults** keep their existing behaviour.
- **IMP-C** — the `Unspecified` classification stays internal. One repository
  predicate excludes it from `ListLocations`, `ListRows`, `RowIDs`,
  `SelectedIDs`, `SelectedTotals`, `SpellPreview` and the commit transaction's
  selected-row read, so the group cannot appear in the location selector, the
  staged-row list, the summary, the selection or a commit. `ReplaceSelection`
  rejects an Other row with `422 selectedImportIds: Other location rows cannot
  be selected for import`. The `#other-location-banner` element, its JS renderer,
  the Other label mapping and the template copy were deleted. Admin inventory
  keeps its **Other (not a bank location)** filter label for existing rows.

Live evidence (rebuilt Compose app, sample batch 12):

- Upload: `acceptedRowCount = 197`, `ignoredRowCount = 0`.
- `GET /locations` → **29** groups and no `Unspecified`.
- `GET /rows` and `GET /row-ids` → **166** rows; `GET /summary` →
  `stagedRows = 166`, `totalLocations = 29`, `selectedRows = 138`.
- `GET /admin/imports/12` contained `id="check-all-locations"` and
  `id="uncheck-all-locations"` above `id="select-all"`, `id="clear-all"` and
  `id="reset-defaults"`; it contained neither `other-location-banner` nor
  `Other (not a bank location)`.

## 8. DEFECT-01 spell metadata (2026-11-22)

**Symptoms.** `Ancient: Lcea's Lament` (EQUID 26622; `items.classes = 128`,
`items.races = 65535`) rendered as `Class: Unknown` / `Race: Unknown`.

**Answers to the investigation questions.**

1. Lookup is performed **by EQUID**, never by name: `ItemMetadataService`
   resolves every distinct staged EQUID in one batch through
   `GetItemsByEQUIDs` → `SELECT id, name, classes, races FROM items WHERE id IN
   (...)` (batches of 500). The staged name is only the fallback identity.
2. The `Spell:`, `Song:` and `Ancient:` prefixes are **not stripped**; they are
   used only by `ClassifyItem` to set `ItemType`, and an unresolved EQUID falls
   back to the staged name. Stripping is irrelevant to the lookup.
3. Spells did **not** intentionally bypass decoding. The defect was an omission
   in the commit plan, not a rule.
4. Layer: `buildCommitPlan` (`internal/service/import_commit.go`) looked up the
   spell's `ItemMetadata` and set `MetadataMissingFg`, but the new-spell insert
   copied only the staged name and left `ClassString`/`RaceString` at their zero
   values. GuildBankItems therefore stored `''`, `''` with
   `MetadataMissingFg=0`. The DTO/template layers were correct; they render a
   blank restriction string as **Unknown** (`inventoryRestrictionText`,
   `adminClassText`).
5. Valid metadata was not displayed because it was never persisted for spell
   rows. Item rows already used the resolved name and decoded strings; spells
   used a separate insert branch that skipped them.

**Correction.** `buildCommitPlan` now applies the single metadata rule to both
kinds: on a resolved EQUID the Items name is authoritative and
`ClassString`/`RaceString` come from `ItemMetadata`; an unresolved EQUID keeps
the imported name with blank strings and `MetadataMissingFg=1` (AM-01/AM-02).
Merges still never rewrite an existing row's stored metadata or dates (D-06).

**Existing rows.** `Store.BackfillSpellMetadata` runs at startup after the
location backfill. It joins `items` by EQUID for spell rows that resolved
metadata but have a blank class/race string, decodes with the same decoders,
fills only blank columns, never overwrites a populated value, never blanks a
zero mask, holds a dedicated advisory lock and is restartable. It does not
touch `Name`, dates, `ModAt`, `ModBy` or audit. The Compose restart logged
`spell metadata backfill complete rows=7`; all seven spell rows, active and
soft-removed, now carry their decoded strings.

**Validation.**

- Unit: the two tests listed in §5.
- Live rows repaired by the backfill: `739` (`Ancient: Lcea's Lament`) →
  `BRD` / `ALL`; `730` (`Spell: Transon's Phantasmal Protection`) → `MAG` /
  `ALL`; `731`/`474` (`Spell: Foliage Shield`) → `DRU` / `ALL`.
  `/inventory/739` and `/inventory/730` rendered those values with no
  `class="unknown"` marker.
- Commit path: a two-row probe batch (11) with `Spell: Warder's Protection`
  (7700) and `Song: Magical Monologue` (7701) committed as two new spells.
  Records 740 and 741 returned `RNG`/`ALL` and `BRD`/`ALL` from
  `/api/v1/items/{id}` and rendered correctly on `/inventory/{id}`. Both probe
  records were then soft-removed through the AI-02 route (audit IDs 78–79), so
  the active inventory count net change is zero.

## 9. Residuals

**Later amendment (2026-11-22, QTY-01–QTY-12).** The item quantity model
changed after this record: imported and manually created ordinary items now
write one row carrying the line/entered quantity, later imports do not merge,
item quantity is editable and audited, and legacy expanded rows can be
consolidated by the owner-run `-consolidate-items` command. The IMP/AI history
above is unchanged; only its expansion-based counts are superseded. See
`ItemQuantityModelImplementation.md`.

- Live Compose verification of the new pages (bulk preview/apply, import scope and
  reset) passed as recorded in §6.
- IMP-03 is implemented as the transparency described in §1; IMP-C later removed
  its Other banner. If the owner meant a different usability change under that
  identifier, it should be recorded as a follow-up rather than folded into this
  change.
- After the follow-up probes the stored inventory is 741 rows: 712 active and 29
  soft-deleted. The two DEFECT-01 probe records (740, 741) were created and
  removed, so this change is net-neutral for the active count.
- `BackfillSpellMetadata` repairs only spell rows whose `MetadataMissingFg=0`
  and whose Items row still exists and decodes to a non-empty string. Rows
  flagged missing remain missing until the AM-01/AM-02 refresh work exists; the
  backfill does not clear that flag.
- Restoring a record removed through "Save and remove" restores the row with the
  edits it was removed with; it does not revert them (documented in the edit
  form).
- **Pre-existing API contract defect discovered while probing:** an explicit JSON
  `null` in `givenTo`/`dateOpen`/`dateBazaar` decodes to an absent
  `*json.RawMessage`, so the API returns `422 ... is required (use null to
  reset)` instead of clearing the field. The HTML form path is unaffected and
  clears correctly (the probe used `""` for `givenTo`). Fixing the JSON DTO needs
  a presence-preserving representation; it is outside the approved AI-02 scope
  and is recorded here for a separate decision.