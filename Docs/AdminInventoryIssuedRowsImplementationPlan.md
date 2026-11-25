# Admin Inventory: Movement-Derived Issued Rows (Implementation Plan)

**Status:** Execution checklist. The behavioural specification is
`AdminInventoryIssuedRowsDesign.md`. Owner decisions D1–D6 are frozen.

**Scope:** the Admin Inventory page only. Public inventory, imports, on-hand
totals and the JSON API contract are unchanged (D3).

## 1. Build sequence

Deployment order is migrate -> attribution backfill -> projection/return route
(design section 11). The build sequence below matches that order.

1. Migration `0007` (additive schema).
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
| 2 | `internal/repository/mariadb/migration_files_test.go` | pin the `0007` filename set and SHA-256; assert the column/index/FK strings |
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

## 3. Verification

- `gofmt -l .`, `go build ./...`, `go vet ./...`, `go test -count=1 ./...` clean.
- `TestMigrationManifest` passes with the `0007` hash pinned.
- Repository tests cover outstanding computation (0/1/many returns),
  zero-outstanding exclusion, `All` duplicate suppression, the `id` filter
  matching both lot and movement ids, and the page/total snapshot.
- Service tests cover return-by-movement for active, whole-issued, partial-issued
  and administratively removed lots; over-return `422`; stale token `409`;
  idempotency replay; FIFO attribution on the lot-level return.
- API tests cover the new routes requiring auth/CSRF, the `Return` link present
  only on issued rows and the unchanged `Restore` behaviour.
- Manual: the eleven acceptance criteria in the design, against a seeded lot of
  5 units.

## 4. Attribution backfill (one-time, idempotent)

Existing `RETURN` rows carry `RelatedMovementID = NULL`, so the projection would
overstate outstanding on their `ISSUE` movements. The deployment therefore runs
the FIFO reconciliation in design section 11 before the projection goes live:
each unattributed `RETURN` is attributed to the earliest `ISSUE` movement on the
same `GuildBankItemID` whose outstanding still exceeds the returned quantity.
The application also attributes every new return at write time, so the backfill
is only needed once and is safe to re-run.
