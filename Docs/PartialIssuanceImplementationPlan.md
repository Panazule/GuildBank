# Partial issuance - implementation plan

**Status:** approved plan. Phases 1–5 are implemented: Phase 1 schema (`0006_inventory_movement.sql`, 2026-11-22), Phase 2 domain/repository, Phase 3 service layer, Phase 4 JSON API and Phase 5 officer UI (issue/return forms and per-record movement history, 2026-11-23). Phases 6 (audit taxonomy/public totals) and 7 (legacy `GivenTo` cleanup) are not started. This document remains the design baseline. Owner decisions OD-01 to OD-07 are recorded in section 12.
**Revision (2026-11-23):** OD-01 was revised (Option A). Partial issue is governed by the on-hand lot quantity, not `EQUID` stackability: any active lot with `Quantity > 1` supports partial issue, and stackability metadata is advisory display only. Wherever this document still gates partial issue on stackability or an allow-list, the quantity-based rule in section 4.3 and section 12 governs.
**Date:** 2026-11-22
**Baseline:** `Docs/PartialIssuanceApprovalPackage.md` and the owner decisions recorded for D-ISS-01 through D-ISS-15 and R-ISS-01 through R-ISS-16.
**Implementation gate:** do not start code until this plan and the readiness conditions in section 13 are accepted.

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
- Atomic issue, return, receive/restore and write-off operations.
- Move and adjust are documented for schema completeness but **deferred by OD-04**.
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
| D-ISS-05 | Partial move reduces the source lot and creates a destination lot. Whole move updates the lot location. Destination inherits source dates. **Deferred by OD-04.** |
| D-ISS-06 | Receives increase an explicitly chosen lot or create a new lot. No automatic merge during daily work. Movement-managed lots are exempt from spell-style auto-merge (OD-07). |
| D-ISS-07 | Removal is a whole-lot action. Partial removal is a write-off or adjustment. Bulk actions show record count and unit count. |
| D-ISS-08 | Restore of a whole-issued lot clears `GivenTo` and writes `RETURN`; administrative restore of a non-issued removed row writes `RECEIVE` (OD-02). Partial return after a full issue creates a new lot. |
| D-ISS-09 | Return copies the original lot's dates by default (OD-03). A fresh-date override is deferred. |
| D-ISS-10 | Quantity editing is a correction path only. It requires a reason, writes `ADJUST`, and never sets `GivenTo`. **Deferred by OD-04.** |
| D-ISS-11 | Public inventory shows active on-hand quantity only. It never exposes `GivenTo`, exact location or removed lots. |
| D-ISS-12 | H-02 and H-05 use one definition: on hand = sum of active lot quantities. |
| D-ISS-13 | Partial issue is governed by the on-hand lot quantity (OD-01, revised 2026-11-23): any active lot with `Quantity > 1` supports partial issue. `EQUID` stackability metadata is advisory display only. |
| D-ISS-14 | Audit gains distinct movement event names or a structured movement type. A generic `GivenTo`-only entry is insufficient. |
| D-ISS-15 | Legacy active rows with `GivenTo` are an owner-reviewed data-quality list. No automatic conversion. |
| R-ISS-01 | On-hand queries use `SUM(Quantity)` for active lots only. |
| R-ISS-02 | Active lot must have `GivenTo = NULL`. |
| R-ISS-03 | Partial issue reduces the selected active lot's quantity and keeps the same lot ID. |
| R-ISS-04 | Every issue writes one immutable movement with lot ID, quantity, recipient, timestamp, actor, source location, remaining quantity and correlation ID. |
| R-ISS-05 | Full-stack issue sets `GivenTo`, writes `ISSUE`, soft-removes the row and leaves `Quantity` unchanged. |
| R-ISS-06 | Partial issue may not reduce a lot to zero. Equal quantity routes to the full-stack path. |
| R-ISS-07 | Partial issue is quantity-based: any active lot with `Quantity > 1` may be issued partially. A lot with `Quantity = 1` can only be issued whole. |
| R-ISS-08 | `EQUID` stackability metadata is advisory (display only) and never gates a write. Partial-issue eligibility is decided solely by the active lot quantity currently on hand. |
| R-ISS-09 | Receives increase a chosen lot or create a new lot. Movement-managed lots are exempt from spell-style auto-merge (OD-07); no merge is attempted in the first release. |
| R-ISS-10 | Partial move creates a destination lot. Whole move updates location. Destination inherits source lifecycle dates. **Deferred by OD-04.** |
| R-ISS-11 | Removal is whole-lot. Bulk actions show affected record count and unit count. |
| R-ISS-12 | Returns are receipts. Partial return to an active lot increases that lot. Full reversal of a whole issue restores the row. Partial return after a full issue creates a new lot. Administrative restore of a non-issued row writes `RECEIVE` (OD-02). |
| R-ISS-13 | Quantity edits are corrections only. They require a reason, write `ADJUST`, and never set `GivenTo`. **Deferred by OD-04.** |
| R-ISS-14 | Public output shows active on-hand quantity only. It never shows recipient, exact location, actor, movement history or removed rows. |
| R-ISS-15 | H-02 and H-05 use the same on-hand definition and never aggregate movement rows into stock. |
| R-ISS-16 | Issue, return and adjustment are atomic: lock the lot, validate expected state, update the lot, write movement and audit in one transaction. Stale state returns `409`. |

## 4. Schema additions

### 4.1 New migration

Add `internal/repository/mariadb/migrations/0006_inventory_movement.sql`. It is additive: one new table and indexes. It does not alter `GuildBankItems`, does not rewrite quantities, does not backfill history and does not clear `GivenTo`.

### 4.2 Movement ledger table

Table name: `InventoryMovement` (implemented in `0006_inventory_movement.sql`).

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
| `IdempotencyKey` | `VARCHAR(64) NULL` | Unique when present; protects double-click returns and issues. Sourced from the `Idempotency-Key` header for API calls or the hidden form field for HTML (OD-06). |

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

### 4.3 Stackability metadata (advisory)

OD-01 was **revised on 2026-11-23**: partial issuance is now governed by the
on-hand lot quantity, not by `EQUID` stackability. The `ItemStackRule` table
below is no longer required and is retained only as a target design if a future
release decides to surface an EQUID classification. Stackability metadata is
advisory and display only; no write path consults it, so the table is not part
of the first release. The existing read helper and allow-list remain in the
repository for display and can be removed when no longer useful.

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

The `ItemStackRule` table above is **not** part of any migration and is not required: OD-01 was revised on 2026-11-23 so partial issue is decided by the on-hand lot quantity. No allow-list governs partial issue; the read helper and its classification are advisory display only. The table is retained purely as a possible future display design.

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
- `ReturnItemInput`: lot ID, expected `ModAt`, quantity, optional recipient, optional note, idempotency key, actor, correlation ID, source IP. Dates are copied from the source lot (OD-03).
- `AdjustItemInput` and `MoveItemInput`: defined for schema completeness but **deferred by OD-04**.
- `MovementFilter` and `MovementPage` for the admin history.
- `Stackability` enum: `stackable`, `non_stackable`, `unknown` (advisory display only since OD-01 was revised on 2026-11-23; never gates a write).

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
5. Decide partial vs whole-stack from the quantity comparison; `EQUID` stackability metadata does not gate the issue (OD-01 revised 2026-11-23).
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

- Active lot: increase quantity, write `RETURN`, audit. Copy the lot's existing dates (OD-03).
- Whole-issued lot (`DeletedFg = 1`, `GivenTo` set), full quantity: clear `GivenTo`, set `DeletedFg = 0`, write `RETURN`, audit. Original dates are preserved.
- Whole-issued lot, partial quantity: create a new active lot for the returned quantity, copy item metadata and lifecycle dates, write `RETURN` on the new lot and a `RETURN`/reference movement on the original as needed. Do not partially resurrect the original row. Original dates are copied (OD-03).
- Reject a return that would exceed the issued quantity or a return against a non-issued removed row. Restore of a non-issued removed row is handled as `RECEIVE` (OD-02), not as a return.

### 5.5 Receive, move, adjust and write-off

- **Receive:** manual create and administrative restore write a `RECEIVE` movement. If a chosen active lot is increased, write `RECEIVE` on that lot. No automatic merge. Import commits do not write movements in the first release (OD-05).
- **Move (deferred):** whole move updates the lot's location and writes `MOVE`. Partial move reduces the source lot, creates a destination lot, copies source dates, and writes one `MOVE` row with `RelatedItemID` set to the destination. Deferred by OD-04.
- **Adjust (deferred):** quantity correction only. Require a reason, write `ADJUST`, and never set `GivenTo`. Deferred by OD-04.
- **Write-off/remove:** whole-lot soft delete writes `WRITE_OFF`. Bulk removal writes one movement per row and returns both record and unit counts.
- **Merge:** movement-managed lots are exempt from spell-style auto-merge (OD-07). Auto-merge must not absorb or alter a movement-managed lot. Movement-aware merge rules are a later decision.

### 5.6 On-hand calculator

Add one shared service function, for example `OnHandQuantity(ctx, filter)`:

- Returns `SUM(Quantity)` for `DeletedFg = 0`.
- Never joins or adds `InventoryMovement`.
- Used by admin totals, public totals, H-02 and H-05.

### 5.7 Invariants enforced in the service layer

- Active lot: `GivenTo` must be empty.
- Create: `GivenTo` is rejected for active rows.
- Update: `GivenTo` is rejected unless the same request completes a whole-record disposition.
- Restore: a whole-issued row clears `GivenTo` and writes `RETURN`; an administratively removed row writes `RECEIVE` (OD-02).
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
| `POST` | `/api/v1/items/:id/adjust` | Quantity correction with a required reason. **Deferred by OD-04.** |
| `POST` | `/api/v1/items/:id/move` | Whole or partial location move. **Deferred by OD-04.** |
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
  "expectedModAt": "2026-11-22T12:34:56.123456Z"
}
```

- `quantity` is required and must be greater than zero.
- `recipient` is required and trimmed.
- `expectedModAt` is required for optimistic concurrency.
- If `quantity` equals on hand, the service uses the full-stack path.
- If `quantity` is greater than on hand, return `422`.
- A stale `expectedModAt` returns `409`.
- The idempotency key is sent in the `Idempotency-Key` header, not in the JSON body (OD-06). A duplicate key returns `409`.

**Return**

```json
{
  "quantity": 10,
  "recipient": "Marc",
  "note": "Partial return",
  "expectedModAt": "2026-11-22T12:34:56.123456Z"
}
```

- Return copies the original lot's dates (OD-03). No date fields are accepted in the first release.
- The idempotency key is sent in the `Idempotency-Key` header (OD-06).

**Adjust (deferred by OD-04)**

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

The Adjust and Move contracts are documented for schema completeness but are **deferred by OD-04**. They are not exposed in the first release.

### 6.3 Existing contract changes

- `POST /api/v1/items` (create): reject `givenTo` for active rows. The field may remain in the DTO only for historical/imported completed issues; active creation must not accept it.
- `PUT /api/v1/items/:id` (update): reject `givenTo` unless the same request is a whole-record disposition. Remove `givenTo` from the generic active-row edit contract.
- `DELETE /api/v1/items/:id` (remove): write a `WRITE_OFF` movement in the same transaction as the soft delete and audit.
- `POST /api/v1/items/:id/restore`: clear `GivenTo` and write `RETURN` when the row was a whole-issued lot. For a non-issued removed row, write `RECEIVE` (OD-02 approved).
- Quantity changes on `PUT /api/v1/items/:id`: require a reason and write `ADJUST`, or remove quantity from the generic update and require the adjust endpoint. Recommended: remove quantity from the generic update contract and use `POST /api/v1/items/:id/adjust`.

### 6.4 Error semantics

| Condition | Status | Code |
|---|---|---|
| Invalid or missing field | `422` | `validation_error` |
| Lot not found | `404` | `not_found` |
| Lot changed since load | `409` | `conflict` |
| Lot already removed | `409` | `conflict` |
| Partial issue equals full quantity | `200` | Routes to the full-stack path automatically; no error |
| Partial issue with `Quantity > 1` (any stackability) | `201` | Allowed; stackability metadata is not consulted |
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
- Add an **Adjust quantity** action separate from the generic edit. **Deferred by OD-04.**
- Add a **Move** action for active lots. **Deferred by OD-04.**
- Show current on hand and the resulting quantity in the issue dialog.
- Keep the existing audit history section and add a movement history section below it.

### 7.2 New templates

- `internal/web/templates/admin_inventory_issue.html`
- `internal/web/templates/admin_inventory_return.html`
- `internal/web/templates/admin_inventory_history.html`

Each form must include the CSRF token and `expectedModAt`. The issue and return forms must also include a hidden `idempotencyKey` field generated per form render (OD-06); the same duplicate-key semantics apply as for the API header. The issue and return forms must show the lot name, lot ID, current on hand, requested quantity and resulting on hand. The recipient field is required for issue. The return form must state whether the action restores the original row or creates a new lot, and must show that the original lot's dates are copied.

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
- Add a required reason to the adjust form. **Deferred by OD-04.**
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

### 9.5 Import movements deferred

- OD-05 defers import-generated `RECEIVE` movements. Imports continue to create inventory state only.
- Imported lots appear as opening balances with no receipt history. No import transaction changes are required in the first release.
- Revisit when import receipt history is required; the ledger schema already supports `RECEIVE`.

## 10. Validation plan

### 10.1 Unit tests

- `internal/service/admin_inventory_test.go` and new movement tests:
  - Partial issue 55 -> 45 leaves one active row and one `ISSUE`.
  - Full-stack issue sets `GivenTo`, leaves quantity unchanged, soft-removes and writes one `ISSUE`.
  - Partial issue equal to full quantity routes to full-stack.
  - Partial issue greater than on hand is rejected.
  - Active row with `GivenTo` is rejected.
  - Partial issue with `Quantity > 1` succeeds regardless of `EQUID` stackability; a quantity-1 lot routes to the full-stack path.
  - Return to active lot increases quantity and writes `RETURN`.
  - Full return of a whole-issued row restores the row and clears `GivenTo`.
  - Partial return after a whole-stack issue creates a new lot and leaves the original as history.
  - Return copies the original lot's dates (OD-03).
  - Restore of a whole-issued row writes `RETURN` and clears `GivenTo`; administrative restore writes `RECEIVE` (OD-02).
  - Remove writes `WRITE_OFF`; bulk remove returns record and unit counts.
  - Stale `expectedModAt` returns `ErrConflict`.
  - Duplicate idempotency key returns `ErrConflict`.
  - Adjust and move tests are **deferred by OD-04**.

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
  - Import commit creates no movements and does not double count (OD-05).
  - Public totals show units, not record counts.
  - Admin totals show units and records with explicit labels.
  - Public pages never expose `GivenTo`, location or movement history.

### 10.4 API tests

- `internal/api/inventory_test.go` and new admin movement tests:
  - New routes require authentication and CSRF.
  - Issue/return request validation; adjust/move validation is deferred by OD-04.
  - `409` on stale state; `422` on invalid quantities.
  - `Idempotency-Key` header and hidden form field share duplicate-key semantics.
  - Movement history pagination and filters.
  - Public JSON does not include movement data.

### 10.5 Manual acceptance

1. Batwing Crunchies, quantity 55, issue 10 to Marc -> quantity 45, one active row, one `ISSUE`, one audit event.
2. Issue 45 from the same row -> full-stack path sets `GivenTo`, soft-removes, quantity remains 45, one `ISSUE`.
3. Return 10 against the whole-issued row -> new active lot of 10; original remains history.
4. Return all 45 against the whole-issued row -> original row restored and `GivenTo` cleared.
5. Remove an active lot -> `WRITE_OFF` movement and audit.
6. Restore a whole-issued row -> `RETURN`, `GivenTo` cleared; restore an administratively removed row -> `RECEIVE` (OD-02).
7. Partial-issue a lot with `Quantity > 1` regardless of `EQUID` stackability metadata -> allowed; the lot quantity decreases and the same active lot remains.
8. Submit the same issue twice with one idempotency key -> second request returns `409` and no duplicate movement.
9. Confirm public totals show units and no internal fields.
10. Adjust and move manual checks are **deferred by OD-04**.

## 11. Implementation sequence

1. **Approval gate** - approve this plan. Owner decisions in section 12 are recorded; no further decision is outstanding for the first release.
2. **Schema** - add `0006_inventory_movement.sql`, update migration manifest and docs. No lot data mutation.
3. **Domain and repository** - movement types, ledger reads/writes, on-hand calculator, advisory stackability read and transaction helpers.
4. **Service** - issue, full-stack issue, return, receive/restore and write-off paths with invariants. Move and adjust are deferred by OD-04.
5. **API** - new routes and contract changes; error semantics.
6. **Admin UI** - issue/return forms, movement history, unit totals, `GivenTo` removal from generic forms.
7. **Audit** - event taxonomy and structured fields; legacy compatibility.
8. **Public read path** - unit totals only; no new exposure.
9. **Legacy cleanup** - owner-reviewed `GivenTo` report and dry-run.
10. **Validation** - unit, repository, integration, API, migration and manual acceptance.

Dependencies: schema -> repository -> service -> API/UI. Audit and totals can proceed once the service contract is fixed. Legacy cleanup must not block the invariant enforcement for new writes.

## 12. Owner decisions (recorded)

The owner reviewed the decision matrix on 2026-11-22. The following decisions are **recorded** and govern the implementation; OD-05 is deferred rather than approved.

| ID | Decision | Owner disposition | Implementation consequence |
|---|---|---|---|
| **OD-01** | Partial-issue eligibility | **Revised 2026-11-23 (Option A)** - partial issue is governed by the active lot quantity currently on hand, not by `EQUID` stackability metadata. | Any active lot with `Quantity > 1` supports partial issue; stackability is not consulted on the write path and is advisory display only. The earlier `EQUID` allow-list gate is removed. |
| **OD-02** | Restore semantics | **Approved** - administrative restore writes `RECEIVE`; return from a recipient writes `RETURN`. | Restore branches on prior issue state: a row issued to a recipient restores as `RETURN` and clears `GivenTo`; a row removed administratively restores as `RECEIVE`. |
| **OD-03** | Return date policy | **Approved** - copy the original lot's dates by default; fresh-date override deferred. | Return copies the source lot's open/bazaar dates. No fresh-date override in the first release. |
| **OD-04** | First-release scope | **Approved** - `ISSUE` and `RETURN` only; `MOVE` and `ADJUST` deferred. | The ledger schema supports all movement types, but only issue and return are exposed in the first release. Move and adjust routes, forms and tests are deferred. |
| **OD-05** | Import receive movements | **Deferred** - no import-generated `RECEIVE` movements in the first release. | Imports continue to create inventory state only. Imported lots appear as opening balances with no receipt history. |
| **OD-06** | Idempotency transport | **Approved** - `Idempotency-Key` header for API, hidden form field for HTML, shared semantics. | One idempotency key per request; duplicate keys return `409 conflict`. HTML forms carry the key in a hidden field; API clients use the header. |
| **OD-07** | Spell auto-merge | **Approved** - movement-managed lots are exempt from spell-style auto-merge. | Auto-merge must not absorb or alter a movement-managed lot. Ledger integrity is preserved; movement-aware merge rules are a later decision. |

**Approval:** Owner decisions recorded 2026-11-22. Implementation may begin subject to the readiness conditions in section 13.

## 13. Implementation readiness summary

### 13.1 Remaining required work

1. **Schema** - add `0006_inventory_movement.sql`, indexes, constraints and migration manifest/hash updates. No lot data mutation.
2. **Domain and repository** - movement types, append-only ledger reads/writes, on-hand calculator, advisory stackability read and transaction helpers.
3. **Service** - atomic issue, full-stack issue, return, receive/restore and write-off paths with row locking, expected-state checks and the active-row `GivenTo` invariant.
4. **API** - `POST /api/v1/items/:id/issue`, `POST /api/v1/items/:id/return`, movement history reads, and the create/update/delete/restore contract changes. `Idempotency-Key` header parsing.
5. **Admin UI** - issue and return forms, movement history page, unit totals and `GivenTo` removal from generic active-row forms. Hidden idempotency field in HTML forms.
6. **Audit** - distinct movement event types and structured fields, written in the same transaction as the lot update and movement.
7. **Public read path** - unit totals only; no recipient, location or movement exposure.
8. **Legacy `GivenTo` cleanup** - read-only report and owner dry-run; no automatic conversion.
9. **Validation** - the test and acceptance coverage in section 10.

### 13.2 Deferred work

| Item | Reason | Revisit trigger |
|---|---|---|
| `MOVE` and `ADJUST` API/UI | OD-04 limits first release to issue and return. | After issue/return is stable; schema already supports both types. |
| Import-generated `RECEIVE` movements | OD-05 deferred; imports remain inventory state only. | When receipt history for imports is required. |
| Fresh-date return override | OD-03 deferred. | When returned stock genuinely has new dates. |
| Movement-aware spell merge (`MERGE`) | OD-07 exempts movement-managed lots. | When merge rules can preserve ledger integrity. |
| Stackability metadata coverage | OD-01 (revised) no longer gates partial issue on metadata; the advisory classification is display only. | Only if a richer EQUID metadata source is added for display. |

### 13.3 Estimated implementation phases

| Phase | Content | Exit criteria |
|---|---|---|
| 0 - Approval and baseline | Owner decisions recorded; plan approved. | This document approved. |
| 1 - Schema | Movement ledger migration, indexes, constraints, migration tests and docs. | Migration applies and rolls back cleanly; no lot data changed. |
| 2 - Domain and repository | Movement types, ledger reads/writes, on-hand calculator, advisory stackability read. | Repository tests pass; ledger is append-only. |
| 3 - Service | Issue, full-stack issue, return, restore and write-off with invariants and idempotency. | Service tests pass; stale state returns `409`; no partial writes. |
| 4 - API | Issue/return routes, history reads, contract changes, error semantics. | API tests pass; authentication and CSRF enforced. |
| 5 - Admin UI | Issue/return forms, history page, unit totals, `GivenTo` removal. | Manual acceptance for issue and return passes. |
| 6 - Audit and public totals | Movement events, structured fields, public unit totals. | Audit atomicity tests pass; public output exposes no internal data. |
| 7 - Legacy cleanup and release | `GivenTo` report, dry-run, documentation and release validation. | Owner resolves or accepts each legacy row; release checklist complete. |

Phases 1-3 are sequential. Phase 4 depends on 3. Phase 5 depends on 4. Phase 6 can start once the service contract is fixed. Phase 7 closes the release.

### 13.4 Migration impact

- **Additive only.** One new table (`InventoryMovement`) with indexes and constraints. No column removal, no quantity rewrite, no `GivenTo` rewrite.
- **Indexes** on `(GuildBankItemID, OccurredAt)`, `(EQUID, OccurredAt)`, `(MovementType, OccurredAt)`, `(Recipient, OccurredAt)` and `(CorrelationID)`; unique `IdempotencyKey`.
- **Constraints** - quantity greater than zero; remaining quantity zero or greater; recipient required for `ISSUE`; movement type restricted to the approved set; no application update/delete path.
- **No backfill.** Existing lots become opening balances; pre-deployment history is not invented. The UI labels the baseline.
- **No automatic `GivenTo` cleanup.** A read-only report lists active rows with recipients; the owner resolves each row. New writes cannot set `GivenTo` on active rows.
- **Rollback.** Dropping the ledger returns the application to Phase 5 behavior. Partial issues already applied remain applied; the rollback note must say so and provide a report of affected lots and movements.
- **Import behaviour unchanged.** No `RECEIVE` movements are written by imports in the first release (OD-05).

### 13.5 Validation approach

- **Unit** - service invariants: partial vs full-stack issue, quantity-based partial-issue eligibility (stackability never gates), return to active lot, return after whole-stack issue, restore branching (`RECEIVE` vs `RETURN`), return date copying, idempotency conflicts, stale `expectedModAt`.
- **Repository** - movement round trip, atomic movement plus audit, append-only enforcement, on-hand `SUM(Quantity)` excludes removed lots and movement rows, migration apply/rollback.
- **Integration** - concurrent issues from one lot (one succeeds, one `409`), import creates no movements, public totals show units, public pages expose no internal fields.
- **API** - authentication and CSRF on new routes, request validation, `409`/`422` semantics, `Idempotency-Key` header and hidden form field share duplicate-key behavior, history pagination and filters, no movement data in public JSON.
- **Migration** - applies and rolls back cleanly; no lot data mutation; opening-balance labeling.
- **Manual acceptance** - issue 10 from 55 leaves 45 in one active row; issuing the remainder uses the full-stack path; return restores correctly; restore writes the approved movement type; public totals show units only.
- **Deferred coverage** - move and adjust tests are out of scope for the first release (OD-04); import movement tests are out of scope (OD-05).

## 14. Implementation status

This document was a plan when approved. Phase 1 schema is now implemented: `internal/repository/mariadb/migrations/0006_inventory_movement.sql` adds the append-only `InventoryMovement` ledger with the approved indexes and constraints. The migration manifest and migration assertions were updated. No service, API, UI, audit or movement workflow code was changed; no existing lot data, quantity or `GivenTo` value was mutated. Owner decisions OD-01 to OD-07 are recorded in section 12. Phases 2–5 are now also implemented: the domain/repository layer, the `Issue`/`Return` service operations, the JSON API and the Phase 5 officer UI (issue and return forms plus per-record movement history). Phase 6 (movement audit taxonomy and public unit totals) and Phase 7 (legacy `GivenTo` cleanup and release) remain and are the only outstanding phases.
