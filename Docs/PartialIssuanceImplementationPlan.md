# Partial issuance - implementation plan

**Status:** plan only. No application code, route, template, migration or runtime behavior is changed by this document.
**Date:** 2026-11-22
**Baseline:** `Docs/PartialIssuanceApprovalPackage.md` and the owner decisions recorded for D-ISS-01 through D-ISS-15 and R-ISS-01 through R-ISS-16.
**Implementation gate:** do not start code until this plan is approved.

## 1. Approved design baseline

| Decision | Implementation consequence |
|---|---|
| Lot-plus-movement model | `GuildBankItems` remains the current-state lot table. A new append-only movement ledger explains every quantity or location change. |
| Partial issuance | The selected active lot's `Quantity` is reduced. The lot ID, location, dates and provenance stay unchanged. No second active row is created. |
| Full-stack issuance | The lot's `GivenTo` is set to the recipient, one `ISSUE` movement is written, and the lot is soft-removed. `Quantity` is not set to zero because `ck_gbi_quantity` requires `Quantity >= 1`. |
| `GivenTo` meaning | `GivenTo` is a recipient snapshot for a completed whole-record issue or disposition only. It is never a partial-issue, reservation or note field. |
| Partial return after whole-stack issue | A new lot is created for the returned quantity. The historical issued row is not partially resurrected. |
| Public totals | Public totals represent units on hand, not record counts. |

**Invariant:** on hand = `SUM(Quantity)` for active lots (`DeletedFg = 0`). Movement quantities are reporting history and are never added to on-hand stock.

## 2. Scope and non-goals

**In scope**

- Additive movement ledger schema and indexes.
- Atomic issue, return, receive, move, adjust, write-off and merge operations.
- Service-layer enforcement of the active-row `GivenTo` invariant.
- Admin issue/return forms and movement history.
- Audit events for every movement, in the same transaction.
- Unit totals for public and admin inventory.
- Migration, rollback and validation evidence.

**Out of scope for this plan**

- Splitting an active lot on partial issue.
- Automatically converting or clearing legacy active `GivenTo` rows.
- Backfilling guessed movement history for pre-deployment lots.
- Exposing recipients, exact locations or movement history on public pages.
- Physically deleting inventory rows.

## 3. Traceability matrix

| ID | Planned implementation |
|---|---|
| D-ISS-01 | Keep `GuildBankItems` as the lot table. No lot-table rewrite. Add only a movement ledger that references the lot. |
| D-ISS-02 | Add `InventoryMovement` as an immutable ledger. Every write path writes a movement row. |
| D-ISS-03 | Enforce `GivenTo = NULL` on active lots in create, update and restore paths. Allow `GivenTo` only on completed whole-record issue/disposition. |
| D-ISS-04 | Partial issue reduces the same lot. Full issue sets `GivenTo`, writes `ISSUE`, and soft-removes the lot without zeroing quantity. |
| D-ISS-05 | Partial move reduces the source lot and creates a destination lot. Whole move updates the lot location. Destination inherits source dates. |
| D-ISS-06 | Receives increase an explicitly chosen lot or create a new lot. No automatic merge during daily work. Spell auto-merge must be exempted or made movement-aware. |
| D-ISS-07 | Removal is a whole-lot action. Partial removal is a write-off or adjustment. Bulk actions show record count and unit count. |
| D-ISS-08 | Restore of a whole-issued lot clears `GivenTo` and writes `RETURN`. Partial return after a full issue creates a new lot. |
| D-ISS-09 | Restore preserves lot dates by default. A fresh availability window requires receiving as a new lot. |
| D-ISS-10 | Quantity editing is a correction path only. It requires a reason, writes `ADJUST`, and never sets `GivenTo`. |
| D-ISS-11 | Public inventory shows active on-hand quantity only. It never exposes `GivenTo`, exact location or removed lots. |
| D-ISS-12 | H-02 and H-05 use one definition: on hand = sum of active lot quantities. |
| D-ISS-13 | Stackability is `EQUID`-level metadata. The source of truth must be approved before partial issue is enabled for ambiguous items. |
| D-ISS-14 | Audit gains distinct movement event names or a structured movement type. A generic `GivenTo`-only entry is insufficient. |
| D-ISS-15 | Legacy active rows with `GivenTo` are an owner-reviewed data-quality list. No automatic conversion. |
| R-ISS-01 | On-hand queries use `SUM(Quantity)` for active lots only. |
| R-ISS-02 | Active lot must have `GivenTo = NULL`. |
| R-ISS-03 | Partial issue reduces the selected active lot's quantity and keeps the same lot ID. |
| R-ISS-04 | Every issue writes one immutable movement with lot ID, quantity, recipient, timestamp, actor, source location, remaining quantity and correlation ID. |
| R-ISS-05 | Full-stack issue sets `GivenTo`, writes `ISSUE`, soft-removes the row and leaves `Quantity` unchanged. |
| R-ISS-06 | Partial issue may not reduce a lot to zero. Equal quantity routes to the full-stack path. |
| R-ISS-07 | Non-stackable lot has quantity 1 and can only be issued whole. A non-stackable lot with quantity greater than 1 is a data-quality error. |
| R-ISS-08 | Stackability is read from `EQUID` metadata, never inferred from name, type or quantity. Ambiguous items cannot use partial issue. |
| R-ISS-09 | Receives increase a chosen lot or create a new lot. Any merge writes `MERGE` or `ADJUST` and preserves absorbed lot IDs. |
| R-ISS-10 | Partial move creates a destination lot. Whole move updates location. Destination inherits source lifecycle dates. |
| R-ISS-11 | Removal is whole-lot. Bulk actions show affected record count and unit count. |
| R-ISS-12 | Returns are receipts. Partial return to an active lot increases that lot. Full reversal of a whole issue restores the row. Partial return after a full issue creates a new lot. |
| R-ISS-13 | Quantity edits are corrections only. They require a reason, write `ADJUST`, and never set `GivenTo`. |
| R-ISS-14 | Public output shows active on-hand quantity only. It never shows recipient, exact location, actor, movement history or removed rows. |
| R-ISS-15 | H-02 and H-05 use the same on-hand definition and never aggregate movement rows into stock. |
| R-ISS-16 | Issue, return and adjustment are atomic: lock the lot, validate expected state, update the lot, write movement and audit in one transaction. Stale state returns `409`. |

## 4. Schema additions

### 4.1 New migration

Add `internal/repository/mariadb/migrations/0006_inventory_movement.sql`. It is additive: one new table and indexes. It does not alter `GuildBankItems`, does not rewrite quantities, does not backfill history and does not clear `GivenTo`.

### 4.2 Movement ledger table

Proposed table name: `InventoryMovement`.

| Column | Type | Notes |
|---|---|---|
| `MovementID` | `BIGINT UNSIGNED AUTO_INCREMENT` | Primary key. |
| `GuildBankItemID` | `BIGINT UNSIGNED NOT NULL` | The lot affected. FK to `GuildBankItems` is recommended; no physical deletes exist. |
| `EQUID` | `INT UNSIGNED NOT NULL` | Denormalized for reporting without a join. |
| `MovementType` | `ENUM('ISSUE','RETURN','RECEIVE','MOVE','ADJUST','WRITE_OFF','MERGE') NOT NULL` | Approved closed set. |
| `Quantity` | `INT UNSIGNED NOT NULL` | Must be greater than zero. |
| `Recipient` | `VARCHAR(128) NULL` | Required for `ISSUE`; internal-only. |
| `ActorUserID` | `INT NULL` | Snapshot actor ID. |
| `ActorName` | `VARCHAR(64) NOT NULL` | Snapshot actor name. |
| `OccurredAt` | `DATETIME(6) NOT NULL` | UTC timestamp. |
| `FromLocation` | `VARCHAR(128) NULL` | Source location for move/issue. |
| `ToLocation` | `VARCHAR(128) NULL` | Destination location for move/return. |
| `RemainingQuantity` | `INT UNSIGNED NOT NULL` | On-hand quantity after the movement on the affected lot. |
| `RelatedItemID` | `BIGINT UNSIGNED NULL` | Destination or source lot for split move/return. |
| `Note` | `VARCHAR(255) NULL` | Free-text reason or reference. |
| `CorrelationID` | `CHAR(36) NULL` | Matches the audit event. |
| `IdempotencyKey` | `VARCHAR(64) NULL` | Unique when present; protects double-click returns and issues. |

**Indexes**

- Primary key on `MovementID`.
- Unique `uq_movement_idempotency (IdempotencyKey)`.
- `ix_movement_lot_time (GuildBankItemID, OccurredAt)`.
- `ix_movement_equid_time (EQUID, OccurredAt)`.
- `ix_movement_type_time (MovementType, OccurredAt)`.
- `ix_movement_recipient_time (Recipient, OccurredAt)`.
- `ix_movement_correlation (CorrelationID)`.

**Constraints**

- `ck_movement_quantity CHECK (Quantity > 0)`.
- `ck_movement_remaining CHECK (RemainingQuantity >= 0)`.
- `ck_movement_issue_recipient CHECK (MovementType <> 'ISSUE' OR Recipient IS NOT NULL)`.

The application exposes insert and read only. No update or delete method is added. Corrections are new movements.

### 4.3 Optional stackability table

Only if the owner approves `EQUID`-level metadata:

```sql
CREATE TABLE ItemStackRule (
    EQUID       INT UNSIGNED NOT NULL,
    StackableFg TINYINT(1) NOT NULL,
    UpdatedAt   DATETIME(6) NOT NULL,
    UpdatedBy   VARCHAR(64) NOT NULL,
    PRIMARY KEY (EQUID),
    CONSTRAINT ck_stack_rule_fg CHECK (StackableFg IN (0,1))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

If the owner prefers no new table, use an explicit settings allow-list and mark it temporary. Until the source is approved, partial issue is disabled for ambiguous items.

### 4.4 No changes to existing columns

- `GuildBankItems.GivenTo` remains for completed whole-record issues and historical rows.
- `GuildBankItems.Quantity` remains the on-hand quantity for active lots.
- `GuildBankItems.DeletedFg` remains the soft-delete flag.
- No zero-quantity writes are introduced; the existing `ck_gbi_quantity` stays intact.
- No new public columns are added.

## 5. Domain and service changes

### 5.1 Domain types

Add to `internal/domain/inventory_admin.go` or a new `internal/domain/movement.go`:

- `MovementType` with the approved closed set.
- `InventoryMovement` with the ledger fields.
- `IssueItemInput`: lot ID, expected `ModAt`, quantity, recipient, optional note, idempotency key, actor, correlation ID, source IP.
- `ReturnItemInput`: lot ID, expected `ModAt`, quantity, optional recipient, optional note, idempotency key, actor, correlation ID, source IP.
- `AdjustItemInput`: lot ID, expected `ModAt`, new quantity, reason, actor, correlation ID, source IP.
- `MoveItemInput`: lot ID, expected `ModAt`, quantity, destination location, optional note, actor, correlation ID, source IP.
- `MovementFilter` and `MovementPage` for the admin history.
- `Stackability` enum: `stackable`, `non_stackable`, `unknown`.

### 5.2 Repository interface

Extend `AdminStore`/`AdminWriteTx` in `internal/service/admin_inventory.go`:

- `InsertMovement(ctx, movement domain.InventoryMovement) (int64, error)`.
- `ListMovements(ctx, filter domain.MovementFilter) (domain.MovementPage, error)`.
- `GetMovement(ctx, movementID int64) (domain.InventoryMovement, error)`.
- `SumActiveQuantity(ctx, filter domain.AdminInventoryFilter) (uint64, error)` for unit totals.

Repository implementation in `internal/repository/mariadb/admin_inventory.go`:

- Insert movement inside the existing `BeginAdminWrite` transaction.
- Read movement history with a bounded, allowlisted filter and newest-first order.
- Never expose update/delete statements for the ledger.
- Reuse the existing row lock (`SELECT ... FOR UPDATE`) and expected `ModAt` check.

### 5.3 Atomic issue operation

New `AdminInventoryService.Issue(ctx, input)` in `internal/service/admin_inventory.go`:

1. Begin `BeginAdminWrite`.
2. Lock the lot with `LockRow`.
3. Reject if the lot is removed or `GivenTo` is non-empty.
4. Reject if `input.Quantity` is zero or greater than on hand.
5. Reject if the item is non-stackable and quantity is greater than 1.
6. If quantity equals on hand, route to the full-stack path.
7. Partial path: `UPDATE GuildBankItems SET Quantity = Quantity - ?, ModAt = ?, ModBy = ?, ModByUserID = ? WHERE GuildBankItemID = ? AND DeletedFg = 0`.
8. Insert one `ISSUE` movement with `RemainingQuantity = onHand - quantity`.
9. Insert one audit event with the same correlation ID.
10. Commit. Any failure rolls back the lot update, movement and audit together.

Full-stack path:

1. Lock and validate the same way.
2. Set `GivenTo`, `DeletedFg = 1`, `ModAt`, `ModBy`, `ModByUserID`.
3. Leave `Quantity` unchanged.
4. Insert one `ISSUE` movement with the full on-hand quantity and `RemainingQuantity = 0`.
5. Insert one audit event.
6. Commit.

A stale `expectedModAt` returns `domain.ErrConflict` and the API maps it to `409`.

### 5.4 Return operation

New `AdminInventoryService.Return(ctx, input)`:

- Active lot: increase quantity, write `RETURN`, audit.
- Whole-issued lot (`DeletedFg = 1`, `GivenTo` set), full quantity: clear `GivenTo`, set `DeletedFg = 0`, write `RETURN`, audit.
- Whole-issued lot, partial quantity: create a new active lot for the returned quantity, copy item metadata and lifecycle dates, write `RETURN` on the new lot and a `RETURN`/reference movement on the original as needed. Do not partially resurrect the original row.
- Reject a return that would exceed the issued quantity or a return against a non-issued removed row unless the owner approves that case.

### 5.5 Receive, move, adjust and write-off

- **Receive:** manual create and import commit write a `RECEIVE` movement. If a chosen active lot is increased, write `RECEIVE` on that lot. No automatic merge.
- **Move:** whole move updates the lot's location and writes `MOVE`. Partial move reduces the source lot, creates a destination lot, copies source dates, and writes one `MOVE` row with `RelatedItemID` set to the destination.
- **Adjust:** quantity correction only. Require a reason, write `ADJUST`, and never set `GivenTo`.
- **Write-off/remove:** whole-lot soft delete writes `WRITE_OFF`. Bulk removal writes one movement per row and returns both record and unit counts.
- **Merge:** spell auto-merge must either be exempted for movement-managed lots or write `MERGE`/`ADJUST` and preserve absorbed lot IDs. The plan recommends exempting movement-managed lots first.

### 5.6 On-hand calculator

Add one shared service function, for example `OnHandQuantity(ctx, filter)`:

- Returns `SUM(Quantity)` for `DeletedFg = 0`.
- Never joins or adds `InventoryMovement`.
- Used by admin totals, public totals, H-02 and H-05.

### 5.7 Invariants enforced in the service layer

- Active lot: `GivenTo` must be empty.
- Create: `GivenTo` is rejected for active rows.
- Update: `GivenTo` is rejected unless the same request completes a whole-record disposition.
- Restore: clears `GivenTo` and writes `RETURN`.
- Partial issue: quantity must be less than on hand; equal quantity routes to full-stack.
- Full issue: quantity remains unchanged and `DeletedFg = 1`.
- No movement without an audit event and no audit event without a movement.

## 6. API changes

All routes stay inside the existing admin group in `internal/api/router.go`, which already enforces authentication, CSRF and administrator access.

### 6.1 New routes

| Method | Route | Purpose |
|---|---|---|
| `POST` | `/api/v1/items/:id/issue` | Partial or full-stack issue. |
| `POST` | `/api/v1/items/:id/return` | Return to an active lot or create a new lot after a whole-stack issue. |
| `POST` | `/api/v1/items/:id/adjust` | Quantity correction with a required reason. |
| `POST` | `/api/v1/items/:id/move` | Whole or partial location move. |
| `GET` | `/api/v1/admin/items/:id/movements` | Paginated movement history for one lot. |
| `GET` | `/api/v1/admin/movements/:movementID` | One movement detail. |
| `GET` | `/admin/inventory/:id/history` | HTML movement history page. |
| `GET` | `/admin/inventory/:id/issue` | HTML issue form. |
| `POST` | `/admin/inventory/:id/issue` | HTML issue submit. |
| `GET` | `/admin/inventory/:id/return` | HTML return form. |
| `POST` | `/admin/inventory/:id/return` | HTML return submit. |

### 6.2 Request contracts

**Issue**

```json
{
  "quantity": 10,
  "recipient": "Marc",
  "note": "Raid award",
  "expectedModAt": "2026-11-22T12:34:56.123456Z",
  "idempotencyKey": "optional-client-key"
}
```

- `quantity` is required and must be greater than zero.
- `recipient` is required and trimmed.
- `expectedModAt` is required for optimistic concurrency.
- If `quantity` equals on hand, the service uses the full-stack path.
- If `quantity` is greater than on hand, return `422`.
- A stale `expectedModAt` returns `409`.

**Return**

```json
{
  "quantity": 10,
  "recipient": "Marc",
  "note": "Partial return",
  "expectedModAt": "2026-11-22T12:34:56.123456Z",
  "idempotencyKey": "optional-client-key"
}
```

**Adjust**

```json
{
  "quantity": 45,
  "reason": "Recount correction",
  "expectedModAt": "2026-11-22T12:34:56.123456Z"
}
```

**Move**

```json
{
  "quantity": 10,
  "destinationLocation": "Guild Hall - Slot 4",
  "note": "Moved to the hall",
  "expectedModAt": "2026-11-22T12:34:56.123456Z"
}
```

### 6.3 Existing contract changes

- `POST /api/v1/items` (create): reject `givenTo` for active rows. The field may remain in the DTO only for historical/imported completed issues; active creation must not accept it.
- `PUT /api/v1/items/:id` (update): reject `givenTo` unless the same request is a whole-record disposition. Remove `givenTo` from the generic active-row edit contract.
- `DELETE /api/v1/items/:id` (remove): write a `WRITE_OFF` movement in the same transaction as the soft delete and audit.
- `POST /api/v1/items/:id/restore`: clear `GivenTo` and write `RETURN` when the row was a whole-issued lot. For a non-issued removed row, write a `RECEIVE` or `RETURN` movement according to the approved restore policy.
- Quantity changes on `PUT /api/v1/items/:id`: require a reason and write `ADJUST`, or remove quantity from the generic update and require the adjust endpoint. Recommended: remove quantity from the generic update contract and use `POST /api/v1/items/:id/adjust`.

### 6.4 Error semantics

| Condition | Status | Code |
|---|---|---|
| Invalid or missing field | `422` | `validation_error` |
| Lot not found | `404` | `not_found` |
| Lot changed since load | `409` | `conflict` |
| Lot already removed | `409` | `conflict` |
| Partial issue equals full quantity | `422` | `validation_error` (route to full-stack action) |
| Non-stackable partial issue | `422` | `validation_error` |
| Return exceeds issued quantity | `422` | `validation_error` |
| Duplicate idempotency key | `409` | `conflict` |

### 6.5 JSON response shape

Return the updated lot and the movement summary:

```json
{
  "item": { "...": "existing admin item response" },
  "movement": {
    "movementId": 101,
    "type": "ISSUE",
    "quantity": 10,
    "remainingQuantity": 45,
    "recipient": "Marc",
    "occurredAt": "2026-11-22T12:34:56.123456Z"
  }
}
```

No movement history or recipient is added to public JSON.

## 7. UI changes

### 7.1 Admin inventory detail

`internal/web/templates/admin_inventory_detail.html` and `internal/api/views.go`:

- Add an **Issue** action for active lots.
- Add a **Return** action for removed/issued lots with a recipient.
- Add an **Adjust quantity** action separate from the generic edit.
- Add a **Move** action for active lots.
- Show current on hand and the resulting quantity in the issue dialog.
- Keep the existing audit history section and add a movement history section below it.

### 7.2 New templates

- `internal/web/templates/admin_inventory_issue.html`
- `internal/web/templates/admin_inventory_return.html`
- `internal/web/templates/admin_inventory_history.html`

Each form must include the CSRF token and `expectedModAt`. The issue and return forms must show the lot name, lot ID, current on hand, requested quantity and resulting on hand. The recipient field is required for issue. The return form must state whether the action restores the original row or creates a new lot.

### 7.3 Admin inventory list

`internal/web/templates/admin_inventory.html` and `internal/api/admin_pages.go`:

- Keep the record count for pagination, but label it as records.
- Add a separate unit total: `45 units in 3 stacks`.
- Add unit totals to bulk removal previews and results.
- Add a "History" action on each row.

### 7.4 Public inventory list

`internal/web/templates/inventory_list.html` and `internal/api/inventory.go`:

- Change the summary from record count to units on hand, or show both with explicit labels.
- Recommended text: `45 units in 3 stacks`.
- Keep pagination based on records; label the page count as records.
- Do not add recipient, location, movement history or actor identity.

### 7.5 Movement history page

Columns:

- Occurred at
- Type
- Quantity
- From -> To location
- Recipient
- Actor
- Remaining on hand
- Note
- Correlation ID

Behavior:

- Newest first, paginated, filterable by type, date range and recipient.
- Read-only. No edit or delete.
- A movement detail drawer links to the matching audit entry.
- Pre-deployment lots show a labelled **opening balance** derived from the lot's creation state. No fabricated movement row.

### 7.6 Admin audit page

`internal/web/templates/admin_audit.html` and `internal/api/admin_audit.go`:

- Add the new movement event types to the event filter.
- Show movement ID and movement type in structured details.
- Keep legacy event types valid.

### 7.7 Form behavior changes

- Remove `GivenTo` from the generic create form.
- Remove `GivenTo` from the generic active-row edit form.
- Add recipient to the issue form and to the whole-record disposition/remove form.
- Add a required reason to the adjust form.
- Keep the "Save and remove" path only for whole-record disposition, not partial issue.

## 8. Audit impacts

### 8.1 Event taxonomy

Add distinct event types to `AdminAuditEventTypes` in `internal/service/admin_inventory.go`:

- `ITEM_ISSUED`
- `ITEM_RETURNED`
- `ITEM_RECEIVED`
- `ITEM_MOVED`
- `ITEM_ADJUSTED`
- `ITEM_WRITTEN_OFF`
- `ITEM_MERGED`

The existing `ITEM_CREATED`, `ITEM_UPDATED` and `ITEM_REMOVED` remain valid for legacy rows. Reports must handle both vocabularies.

### 8.2 Structured fields

Every movement audit event must include:

- `movementId`
- `movementType`
- `guildBankItemId`
- `equid`
- `quantity`
- `remainingQuantity`
- `recipient` when applicable
- `fromLocation` and `toLocation` when applicable
- `correlationId`
- `relatedGuildBankItemId` when applicable

Do not put the movement type only in free text.

### 8.3 Atomicity

- Movement and audit are written in the same transaction as the lot update.
- A movement without an audit event is a defect.
- An audit event without a movement is a defect for movement-managed writes.
- Corrections are new movement and audit rows. No historical row is rewritten.

### 8.4 Access and retention

- Recipient names are internal data. They appear in admin movement history and audit only.
- Public pages never expose recipient, actor, exact location or movement history.
- Existing audit retention and access controls apply unchanged.

## 9. Migration impacts

### 9.1 Additive migration only

- Add `0006_inventory_movement.sql`.
- No column removal, no quantity rewrite, no `GivenTo` rewrite.
- Existing lots become opening balances. No movement history is fabricated.
- The migration is reversible by dropping the new table. Data already changed by partial issues is not automatically reversed; a rollback plan must state that partial issues remain applied or provide a compensating report.

### 9.2 Migration tests and docs

- Update `internal/repository/mariadb/migration_files_test.go` with the new file and hash.
- Add migration assertions for the table, indexes and constraints.
- Update `Docs/DatabaseDesign.md`, `Docs/ProjectStructure.md` and `Docs/ProjectStatus.md`.

### 9.3 Legacy `GivenTo` cleanup

- Add a read-only admin report/filter for active rows with `GivenTo`.
- Do not automatically convert or clear those rows.
- New writes reject `GivenTo` on active rows from the first release.
- The owner resolves each legacy row manually.

### 9.4 Rollback

- Dropping the ledger returns the application to Phase 5 behavior.
- Lot quantities already changed by partial issues remain changed.
- The rollback note must state this explicitly and provide a report of affected lots and movements.

## 10. Validation plan

### 10.1 Unit tests

- `internal/service/admin_inventory_test.go` and new movement tests:
  - Partial issue 55 -> 45 leaves one active row and one `ISSUE`.
  - Full-stack issue sets `GivenTo`, leaves quantity unchanged, soft-removes and writes one `ISSUE`.
  - Partial issue equal to full quantity routes to full-stack.
  - Partial issue greater than on hand is rejected.
  - Active row with `GivenTo` is rejected.
  - Non-stackable partial issue is rejected.
  - Return to active lot increases quantity and writes `RETURN`.
  - Full return of a whole-issued row restores the row and clears `GivenTo`.
  - Partial return after a whole-stack issue creates a new lot and leaves the original as history.
  - Adjust requires a reason and writes `ADJUST`.
  - Move whole and partial paths write `MOVE` and preserve destination dates.
  - Remove writes `WRITE_OFF`; bulk remove returns record and unit counts.
  - Stale `expectedModAt` returns `ErrConflict`.
  - Duplicate idempotency key returns `ErrConflict`.

### 10.2 Repository tests

- `internal/repository/mariadb/inventory_test.go` and new movement tests:
  - Movement insert/read round trip.
  - Movement and audit commit atomically; rollback leaves neither.
  - Ledger has no update/delete path.
  - `SUM(Quantity)` for active lots excludes removed lots and movement rows.
  - Movement history filters and pagination.
  - Migration applies and rolls back cleanly.

### 10.3 Integration tests

- `internal/integration/items_test.go` and new partial-issuance tests:
  - Two concurrent issues from 55: one succeeds, one returns `409`.
  - Import commit writes `RECEIVE` movements without double counting.
  - Public totals show units, not record counts.
  - Admin totals show units and records with explicit labels.
  - Public pages never expose `GivenTo`, location or movement history.

### 10.4 API tests

- `internal/api/inventory_test.go` and new admin movement tests:
  - New routes require authentication and CSRF.
  - Issue/return/adjust/move request validation.
  - `409` on stale state; `422` on invalid quantities.
  - Movement history pagination and filters.
  - Public JSON does not include movement data.

### 10.5 Manual acceptance

1. Batwing Crunchies, quantity 55, issue 10 to Marc -> quantity 45, one active row, one `ISSUE`, one audit event.
2. Issue 45 from the same row -> full-stack path sets `GivenTo`, soft-removes, quantity remains 45, one `ISSUE`.
3. Return 10 against the whole-issued row -> new active lot of 10; original remains history.
4. Return all 45 against the whole-issued row -> original row restored and `GivenTo` cleared.
5. Remove an active lot -> `WRITE_OFF` movement and audit.
6. Adjust quantity with a reason -> `ADJUST` movement and audit.
7. Move 10 units to another location -> destination lot created and `MOVE` recorded.
8. Confirm public totals show units and no internal fields.

## 11. Implementation sequence

1. **Approval gate** - approve this plan and the remaining open decisions in section 12.
2. **Schema** - add `0006_inventory_movement.sql`, update migration manifest and docs. No lot data mutation.
3. **Domain and repository** - movement types, ledger reads/writes, on-hand calculator, transaction helpers.
4. **Service** - issue, return, receive, move, adjust, write-off and merge paths with invariants.
5. **API** - new routes and contract changes; error semantics.
6. **Admin UI** - issue/return forms, movement history, unit totals, `GivenTo` removal from generic forms.
7. **Audit** - event taxonomy and structured fields; legacy compatibility.
8. **Public read path** - unit totals only; no new exposure.
9. **Legacy cleanup** - owner-reviewed `GivenTo` report and dry-run.
10. **Validation** - unit, repository, integration, API, migration and manual acceptance.

Dependencies: schema -> repository -> service -> API/UI. Audit and totals can proceed once the service contract is fixed. Legacy cleanup must not block the invariant enforcement for new writes.

## 12. Open decisions and approval

The following items still need an explicit owner answer before implementation:

| # | Decision | Recommendation |
|---|---|---|
| 1 | Stackability source of truth (D-ISS-13) | `EQUID`-level metadata; until approved, partial issue only for explicitly allow-listed items. |
| 2 | Restore of a non-issued removed row | Write `RECEIVE` or `RETURN`; recommend `RETURN` only for whole-issued rows and `RECEIVE` for administrative restore. |
| 3 | Return date policy after partial return | Copy the original lot's dates; allow an explicit fresh date on the return form. |
| 4 | First-release scope | Ship issue and return first with the full ledger schema; add move/adjust UI next. |
| 5 | Import receive movements | Write one `RECEIVE` movement per created row in the import transaction. |
| 6 | Idempotency key transport | `Idempotency-Key` request header or JSON field; recommend header. |
| 7 | Spell auto-merge | Exempt movement-managed lots first, or write `MERGE` and preserve absorbed lot IDs. |

**Approval:** __________________________  **Date:** ______________

## 13. No-code statement

This document is a plan only. No application code, route, template, stylesheet, migration or runtime behavior was changed. No implementation should begin until this plan and the open decisions in section 12 are approved.
