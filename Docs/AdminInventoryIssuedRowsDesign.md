# Admin Inventory: Movement-Derived Issued Rows (Design)

**Status:** Design proposal. No code, schema, or data changes are included in this document.
**Supersedes:** the visibility model described in `PartialIssuanceImplementationPlan.md` section 13.4.
**Scope:** Admin Inventory page only. Public inventory, imports, and totals are unchanged.
**Revision 2:** rewritten around the owner's confirmed requirement that **every** issue,
including a partial issue, is a discrete, returnable `Issued` row. Legacy lots are no
longer a design constraint (see section 8).
**Revision 3:** all six owner decisions approved; see section 14. The design is now
frozen for implementation.

## Normative requirement

The Admin Inventory page is the officer management surface. Every inventory
disposition must be visible and actionable there:

| State | Meaning | Action offered |
| --- | --- | --- |
| `Active` | inventory currently in the guild bank | `Issue`, `Remove` |
| `Issued` | inventory issued to a recipient | `Return` |
| `Removed` | inventory administratively removed | `Restore` |

The requirement applies to **all** issues, not only whole-stack issues:

- Issue 1 of 5 → an `Issued` row for 1 unit with a `Return` action.
- Issue 4 of 5 → an `Issued` row for 4 units with a `Return` action.
- Issue 5 of 5 → an `Issued` row for 5 units with a `Return` action.

Worked example from the owner:

```text
Lot: Ant's Potion, Qty 5
Issue 1 to Joe

ACTIVE   Ant's Potion            Qty 4
ISSUED   Ant's Potion  Joe       Qty 1   [Return]

Return 1

ACTIVE   Ant's Potion            Qty 5
         (the issued row disappears: nothing remains issued)
```

The inventory arithmetic is already correct. This design changes **visibility and
workflow only**; it does not redesign quantities, issue math, return math, remove
math, or restore math.

---

## 1. Problem statement

The Admin Inventory page renders one row per `GuildBankItems` row and derives the
lifecycle state from the persisted lot flags:

- `internal/api/admin_pages.go:59-65` sets the label to `Issued` only when
  `DeletedFg = 1` **and** `GivenTo` is non-empty.
- `internal/repository/mariadb/admin_inventory.go:109-120` applies the same rule as
  the `status=issued` predicate:
  `DeletedFg = 1 AND GivenTo IS NOT NULL AND GivenTo <> ''`.
- `internal/service/admin_movement.go:220-246` leaves the lot row **active** when
  the issue is partial (`in.Quantity < current.Quantity`); only a whole-stack
  issue sets `DeletedFg = 1` and stores `GivenTo`.

Consequences:

1. Issuing 1 of 5 units leaves the lot `Active` with 4 units. The 1 issued unit
   appears nowhere in the `Issued` view, because the lot row is still active.
2. The `Issued` view therefore only shows whole-stack issues. Partial issues are
   invisible as dispositions.
3. No `Return` action is exposed for a partial issue: the Action column in
   `internal/web/templates/admin_inventory.html:132` renders `Return` only when
   `Deleted` is true and `GivenTo` is set, and renders `Restore` from
   `Restorable` (`admin_pages.go:76`), which is also false for an active row.
4. The owner's requirement — "any issuance, including a partial one, shows as a
   discrete Issued row with a Return action" — cannot be met by the current
   lot-status model without corrupting lot quantities.

The issue/return arithmetic itself is correct. Only the **visibility and workflow**
model is wrong.

### 1.1 Where each requested answer lives

| Requested answer | Section |
| --- | --- |
| How a partial issue becomes a visible `Issued` row | 3, 5.1, 5.2 |
| How that row maps back to its originating lot | 3.3, 4 |
| How `Return` works from that row | 6.1, 7.1, 7.2 |
| Exact schema/model changes | 3.2, 5.1, 6 |
| Exact UI behaviour before and after Issue/Return | 6.2 |

## 2. Goals and non-goals

### 2.1 Goals

- Every issuance (partial or whole-stack) appears as its own `Issued` row on the
  Admin Inventory page, with its own `Return` action.
- The row shows the recipient, the quantity currently outstanding, and the
  issuance time/actor, without adding a separate report, ledger, or movement
  screen.
- `Return` from that row targets that specific issuance, not "the lot".
- Existing `Active` and `Removed` rows, their actions, and all on-hand totals keep
  working exactly as they do today.
- No aggregation-based display (`SUM(ISSUE) - SUM(RETURN)` per lot) — the owner
  explicitly rejected that; each issuance must be individually addressable.
- The partial issue leaves an `Active` row for the remainder **and** an `Issued`
  row for the issued quantity, simultaneously. Neither hides the other.
- The `Issued` row disappears as soon as its outstanding quantity reaches zero, so
  "Return 1" on a 1-of-5 issue restores the lot to 5 and removes the row.
- No data backfill and no mutation of existing lot rows or `GivenTo` values.

### 2.2 Non-goals

- Changing issue/return arithmetic, date copying, or the movement ledger's
  append-only contract. Quantities, issue math, return math, remove math, and
  restore math are all out of scope and stay byte-for-byte as they are.
- Introducing `MOVE`, `ADJUST`, `WRITE_OFF`, or `MERGE` workflows (still deferred
  by OD-04).
- Changing public JSON, public totals, or import behavior.
- Adding a dedicated "Movements" or "Issued" report screen.
- Optimizing for legacy lots. Pre-ledger issued lots can be deleted and
  re-imported, so the design does not carry a compatibility `UNION ALL` for them;
  section 8 records the accepted consequence.

## 3. Chosen model: issuance rows derived from the movement ledger

The movement ledger already records every issue:

- `internal/repository/mariadb/migrations/0006_inventory_movement.sql:6-35`
  defines `InventoryMovement` with `GuildBankItemID`, `MovementType`,
  `Quantity`, `Recipient`, `ActorName`, `OccurredAt`, `RemainingQuantity`,
  `RelatedItemID`, `CorrelationID`, `IdempotencyKey`.
- `internal/domain/movement.go:76-93` mirrors those fields in
  `domain.InventoryMovement`.
- `internal/service/admin_movement.go:248-250` writes an `ISSUE` movement for
  **both** partial and whole-stack issues, with `Quantity` = issued units and
  `RemainingQuantity` = units left in the lot.

So the ledger already contains the row the owner wants. The design is to
**project the Admin Inventory `Issued` view from outstanding `ISSUE` movements**
instead of from lot status.

An issued row is defined as:

> one `ISSUE` movement whose outstanding quantity
> (`Quantity - SUM(RETURN.Quantity attributed to it)`) is greater than zero.

Each such movement becomes exactly one row. A partial issue of 1 from a lot of 5
produces one row for 1 unit while the lot remains `Active` with 4 units. A second
issue of 2 from the same lot produces a second, separate row. Returning 1 unit
against the first row reduces that row to 0 outstanding and it leaves the
`Issued` view; the second row is untouched.

### 3.1 Why a new attribution column is required

The ledger currently cannot attribute a `RETURN` to a specific `ISSUE`:

- `returnToActive` (`admin_movement.go:366-415`) writes a `RETURN` with no
  `RelatedItemID`.
- `returnWholeIssued` (`admin_movement.go:418-461`) also writes no `RelatedItemID`.
- `returnPartialIssued` (`admin_movement.go:465-509`) sets `RelatedItemID` to the
  **lot row ID** (`current.ID`), not to a movement ID.

`CorrelationID` cannot be used: it is per-request and returns are separate
requests. Recipient+lot grouping cannot be used: two partial issues to the same
recipient from the same lot would merge into one row, violating the owner's
"per-issuance" requirement.

**Design decision:** add a nullable `RelatedMovementID` to `InventoryMovement`
(additive migration `0007`), set on every `RETURN` row to the `ISSUE` movement it
closes. `RelatedItemID` keeps its current meaning (the lot row) and is not
reused. This is the only schema change in the design.

### 3.2 Design-level schema sketch (not to be applied)

```sql
-- 0007_movement_related_movement.sql (design sketch)
ALTER TABLE InventoryMovement
    ADD COLUMN RelatedMovementID BIGINT UNSIGNED NULL AFTER RelatedItemID,
    ADD KEY ix_movement_related_movement (RelatedMovementID),
    ADD CONSTRAINT fk_movement_related_movement
        FOREIGN KEY (RelatedMovementID) REFERENCES InventoryMovement (MovementID)
        ON UPDATE RESTRICT ON DELETE RESTRICT;
```

- Nullable: existing rows keep `NULL`; no backfill.
- Self-referencing FK: a `RETURN` may only point at an existing movement.
- No new table, no column on `GuildBankItems`, no change to `GivenTo`.

### 3.3 How an issued row maps back to its lot

Every `ISSUE` movement already carries `GuildBankItemID`, the primary key of the
`GuildBankItems` row it was issued from
(`0006_inventory_movement.sql:6-35`). That column is the map:

```text
InventoryMovement (ISSUE)
  MovementID        <- identity of the issued row and of the Return target
  GuildBankItemID   -> GuildBankItems.GuildBankItemID  (the originating lot)
  Quantity          <- issued quantity
  Recipient         <- "Joe"
  OccurredAt        <- issuance time
  ActorName         <- issuing officer
```

So the design needs **no new mapping column on the lot**. The issued row is the
movement row; its lot is reached by joining `GuildBankItems` on
`GuildBankItemID`, exactly as the projection in section 5.2 does. The lot's name,
type, class, race, location, and metadata all come from that join, so an issued
row renders with the same descriptive columns as the active row it was split
from.

Two consequences follow, and both are deliberate:

1. **The issued row survives changes to the lot.** If the lot is later renamed,
   moved, or consolidated, the issued row still points at the same
   `GuildBankItemID` and shows the lot's current descriptive fields. The movement
   row itself never changes, so the issued quantity and recipient stay accurate.
2. **The lot keeps its own row.** A partial issue leaves the lot row `Active`
   with the remaining quantity. The issued row does not replace it; the two rows
   are complementary and both appear in the `All` view (section 5.3).

## 4. Attribution rules

| Situation | `RelatedMovementID` on the `RETURN` | Resulting issued row |
| --- | --- | --- |
| Return against a partial issue | the `ISSUE` movement ID | outstanding decreases by the returned quantity |
| Return against a whole-stack issue | the `ISSUE` movement ID | outstanding decreases; at 0 the row leaves `Issued` |
| Return against an active lot (stock return, no issuance) | `NULL` | `returnToActive`, unchanged |

A return recorded against a lot that has no `ISSUE` movement (a pre-ledger lot)
keeps `RelatedMovementID = NULL`. Such a lot is not represented in the movement-
derived `Issued` view at all; see section 8.

Outstanding quantity is computed as:

```sql
m.Quantity - COALESCE((
    SELECT SUM(r.Quantity) FROM InventoryMovement r
    WHERE r.MovementType = 'RETURN' AND r.RelatedMovementID = m.MovementID
), 0)
```

Rows with outstanding `<= 0` are excluded from the `Issued` view. The movement
row itself is never mutated; the ledger stays append-only.

## 5. Row projection and query design

### 5.1 Where the projection lives

Add one repository method to the admin store interface (design name:
`ListIssuedRows`) and dispatch on `filter.Status` inside the existing admin list
entry point, so the HTTP layer and templates keep one page model:

```text
ListAdminInventory(ctx, filter)
  switch filter.Status {
  case Active:   existing lot query        (unchanged)
  case Removed:  existing lot query        (unchanged)
  case Issued:   ListIssuedRows(ctx, filter)
  case All:      union of lot rows + issued rows (see 5.3)
  }
```

Tab semantics, which are the heart of the fix:

- **`Active`** and **`Removed`** stay lot-only and unchanged. A partially issued
  lot is still `Active` and still appears there with its remaining quantity, so
  officers keep issuing from it.
- **`Issued`** is movement-only. It selects outstanding `ISSUE` movements and
  never selects lot rows, so a partial issue appears here even though its lot
  row is still active. This is the row that was missing before.
- **`All`** is the only composite view: lot rows plus movement rows, with the
  duplicate-suppression rule in section 5.3.

`ListIssuedRows` returns the same `domain.AdminInventoryPage` shape so the
template, pagination, and URL builders need no structural change. The record
type gains the fields listed in section 6.

### 5.2 Issued query sketch

```sql
SELECT m.MovementID, m.GuildBankItemID, m.EQUID, m.Quantity,
       m.Quantity - COALESCE(r.returned, 0) AS Outstanding,
       m.Recipient, m.ActorName, m.OccurredAt,
       i.Name, i.ItemType, i.ClassString, i.RaceString,
       i.MetadataMissingFg, i.SourceLocation, i.LocationGroup, i.LocationSort,
       i.DateAdded, i.DateOpen, i.DateBazaar, i.ModAt, i.ModBy, i.ModByUserID
FROM InventoryMovement m
JOIN GuildBankItems i ON i.GuildBankItemID = m.GuildBankItemID
LEFT JOIN (
    SELECT RelatedMovementID, SUM(Quantity) AS returned
    FROM InventoryMovement
    WHERE MovementType = 'RETURN' AND RelatedMovementID IS NOT NULL
    GROUP BY RelatedMovementID
) r ON r.RelatedMovementID = m.MovementID
WHERE m.MovementType = 'ISSUE'
  AND m.Quantity > COALESCE(r.returned, 0)
  /* filters: recipient, EQUID, item type, search, metadata, location group */
ORDER BY m.OccurredAt DESC, m.MovementID DESC
LIMIT ? OFFSET ?
```

The count for the page runs the same predicate with `COUNT(*)`, in the same
read-only repeatable-read transaction used by `ListAdminInventory`
(`admin_inventory.go:29-33`), so the page and its total share one snapshot.

The join to `GuildBankItems` is deliberately **not** filtered on `DeletedFg`: a
whole-stack issue soft-deletes the lot, and its issued row must still render with
the lot's descriptive columns. The `Issued` tab therefore shows issued rows for
both active and soft-deleted lots, which is exactly the owner's requirement.

### 5.3 The `All` tab

`All` is the default view and must show both lot rows and issued rows without
duplicates. One `UNION ALL` over the lot-row projection and the issued-row
projection, with a normalized sort key per source:

- Lot rows sort on their existing keys.
- Issued rows sort on `OccurredAt`, mapped to the same `sortKey` column so one
  `ORDER BY` works.
- Duplicate suppression: a lot row with `DeletedFg = 1 AND GivenTo <> ''` **and**
  an `ISSUE` movement is excluded from the lot projection, because its issued row
  now represents it. A partially issued lot is `DeletedFg = 0`, so it is **not**
  excluded and appears in `All` as an `Active` row alongside its `Issued` row —
  the two rows the owner asked for.

A partially issued lot therefore appears twice in `All`, which is correct: one
row is the remaining stock, the other is the issued quantity. The `RowKey`
(`l:<LotID>` vs `m:<MovementID>`) keeps the two rows distinct for bulk selection
and anchors.

### 5.4 Filters and sorting on the Issued tab

| Filter | Mapping on movement-derived rows |
| --- | --- |
| `givenTo` | `m.Recipient = ?` (exact, case-insensitive as today) |
| `equid` | `m.EQUID = ?` |
| `itemType` | `i.ItemType = ?` |
| `metadata` | `i.MetadataMissingFg` |
| `locationGroup` | `i.LocationGroup = ?` |
| `q` / `search` | `i.Name LIKE ?` (same normalization as today) |
| `id` | `(m.GuildBankItemID = ? OR m.MovementID = ?)` — approved decision D4 |
| `sort` | `newest` → `OccurredAt DESC`; `oldest` → `OccurredAt ASC`; name/location sorts → the lot's keys, then `OccurredAt` |

`normalizeMovementFilter` and `movementWhere`
(`internal/repository/mariadb/movement.go:140-203`) already model the allowlisted
predicate builder; the issued query reuses the same style rather than inventing a
new filter language.

### 5.5 Totals

`SumActiveQuantity` (`movement.go:122-127`) counts `GuildBankItems` rows only.
Partial issues already reduce the active lot's `Quantity`, so on-hand totals are
correct today and must not change. Movement-derived issued rows are display-only
and never contribute to on-hand. This is a hard invariant: **no query in this
design may add movement quantities to the on-hand total.**

## 6. Row shape and UI

`adminItemView` (`internal/api/views.go:114-149`, built by `adminItemViewOf` in
`internal/api/admin_pages.go:53-90`) gains the fields below. They are zero for
lot-derived rows, so the existing template branches keep working unchanged.

| Field | Source | Notes |
| --- | --- | --- |
| `RowKey` | `m:<MovementID>` or `l:<LotID>` | unique across both sources; used for bulk-selection and anchors |
| `MovementID` | `m.MovementID` | `0` for lot-derived rows |
| `IssuedQuantity` | computed outstanding | shown in the Quantity cell for movement rows |
| `IssuedAt` / `IssuedAtText` | `m.OccurredAt` | available to the template; not rendered as a new column (decision D5) |
| `IssuedBy` | `m.ActorName` | operator attribution |
| `GivenTo` | `m.Recipient` | already exists in the view; the recipient cell |
| `ReturnURL` | `/admin/inventory/movements/{MovementID}/return` | non-empty only for movement-derived rows |
| `Restorable` | unchanged | `DeletedFg && GivenTo == "" && ConsolidatedInto == nil` |

`ReturnURL` is the single new action field. It is empty for lot-derived rows, so
the template's existing `Restore` branch and the active-row `Issue`/`Remove`
branch keep working without a second field.

Template changes (`admin_inventory.html`):

- `Issued` badge and recipient link already render from `StatusLabel` and
  `GivenTo`; they now also render for movement-derived rows, whose `GivenTo`
  comes from the movement's `Recipient`.
- Action cell (`admin_inventory.html:128-134`) branches on the row source:
  - movement-derived issued row → `Return` (to `ReturnURL`), `History`,
    `Movements`;
  - active row → `Edit`, `History`, `Movements`, `Issue`, `Remove` (unchanged);
  - removed row → `Restore` when `Restorable` (unchanged).
- Empty-state hint (`admin_inventory.html:76-80`) stays as is; the issued queue
  is no longer empty for partial issues, so the hint simply stops firing.
- No new column is added (decision D5). The owner's example shows the existing
  columns: name, recipient, quantity, action. The Quantity cell renders
  `IssuedQuantity` (outstanding) for movement rows and the lot quantity for lot
  rows; the Status cell renders `Issued`. The issue date and actor are exposed as
  `IssuedAtText`/`IssuedBy` so the status cell can show them as a tooltip without
  changing the table layout.

### 6.1 Routes

| Route | Purpose | Status |
| --- | --- | --- |
| `GET /admin/inventory/movements/{movementID}/return` | Return form for one issuance | new |
| `POST /admin/inventory/movements/{movementID}/return` | Apply the return | new |
| `GET|POST /admin/inventory/{id}/return` | Existing lot-level return (active stock only after this design) | unchanged |
| `GET|POST /admin/inventory/{id}/issue` | Existing issue | unchanged |

The new routes reuse the existing movement form handler and CSRF/idempotency
plumbing in `internal/api/admin_movement_pages.go:250-322` (`returnForm` plus
`renderMovementForm`, which calls `CSRF(c)` and mints the idempotency key); only
the resolved target changes from a lot ID to a movement ID.

### 6.2 UI behaviour before and after Issue/Return

The tables below are the acceptance story for the owner's example. `Active` and
`Issued` are separate tabs; `All` shows both at once. Quantity shown for an
issued row is the outstanding quantity, not the original issued quantity.

**Step 0 — starting state.** Lot "Ant's Potion", 5 units, active.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | Ant's Potion | `Active` | — | 5 | `Edit`, `History`, `Movements`, `Issue`, `Remove` |
| `Issued` | *(empty)* | — | — | — | empty-state hint |
| `All` | Ant's Potion | `Active` | — | 5 | as `Active` |

**Step 1 — Issue 1 to Joe.** The issue form posts 1 with recipient "Joe". The
existing arithmetic runs unchanged: the lot stays active with quantity 4, and an
`ISSUE` movement for 1 is appended.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | Ant's Potion | `Active` | — | 4 | `Edit`, `History`, `Movements`, `Issue`, `Remove` |
| `Issued` | Ant's Potion | `Issued` | Joe | 1 | `Return`, `History`, `Movements` |
| `All` | Ant's Potion | `Active` | — | 4 | as `Active` |
| `All` | Ant's Potion | `Issued` | Joe | 1 | `Return`, `History`, `Movements` |

This is the row that does not exist today. The `Issued` tab is now non-empty for a
partial issue, and the `Return` action is reachable without opening movement
history.

**Step 2 — Return 1 from the issued row.** The `Return` link opens the movement
form (`GET /admin/inventory/movements/{movementID}/return`), pre-filled with the
outstanding quantity and the recipient. Posting it appends a `RETURN` movement
with `RelatedMovementID` set to the `ISSUE` movement, and runs the existing
`returnToActive` branch because the lot is still active.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | Ant's Potion | `Active` | — | 5 | `Edit`, `History`, `Movements`, `Issue`, `Remove` |
| `Issued` | *(empty)* | — | — | — | empty-state hint |
| `All` | Ant's Potion | `Active` | — | 5 | as `Active` |

The issued row disappears because its outstanding quantity is now 0. The lot is
back to 5 units. The `RETURN` movement remains in `Movements` as history.

**Step 3 — Issue 4 to Joe (a second, independent issuance).** The lot is active
with 5 units, so the officer issues 4. The lot stays active with 1 unit and a
second `ISSUE` movement is appended.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | Ant's Potion | `Active` | — | 1 | `Edit`, `History`, `Movements`, `Issue`, `Remove` |
| `Issued` | Ant's Potion | `Issued` | Joe | 4 | `Return`, `History`, `Movements` |
| `All` | Ant's Potion | `Active` | — | 1 | as `Active` |
| `All` | Ant's Potion | `Issued` | Joe | 4 | `Return`, `History`, `Movements` |

The first issuance's row is gone (fully returned); this is a distinct row keyed on
the new movement ID. Two issues to the same recipient from the same lot never
merge, because each row is a movement.

**Step 4 — Issue the last 1 unit as a whole-stack issue.** The lot's remaining
quantity equals the issued quantity, so the existing arithmetic soft-deletes the
lot (`DeletedFg = 1`, `GivenTo = "Joe"`) and appends a third `ISSUE` movement.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | *(no row for this lot)* | — | — | — | — |
| `Issued` | Ant's Potion | `Issued` | Joe | 4 | `Return`, `History`, `Movements` |
| `Issued` | Ant's Potion | `Issued` | Joe | 1 | `Return`, `History`, `Movements` |
| `All` | Ant's Potion | `Issued` | Joe | 4 | `Return`, `History`, `Movements` |
| `All` | Ant's Potion | `Issued` | Joe | 1 | `Return`, `History`, `Movements` |

The lot row no longer appears in `All` because it is soft-deleted, has a
recipient, and has an `ISSUE` movement; its issued rows represent it. The two
issued rows stay separate because they are separate movements.

**Step 5 — Return the whole 1-unit row.** The `Return` link on the 1-unit row
posts 1, which equals the outstanding, so the existing `returnWholeIssued` branch
runs: the lot is restored to active, `GivenTo` is cleared, and the quantity
returns to 1.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | Ant's Potion | `Active` | — | 1 | `Edit`, `History`, `Movements`, `Issue`, `Remove` |
| `Issued` | Ant's Potion | `Issued` | Joe | 4 | `Return`, `History`, `Movements` |
| `All` | Ant's Potion | `Active` | — | 1 | as `Active` |
| `All` | Ant's Potion | `Issued` | Joe | 4 | `Return`, `History`, `Movements` |

The 1-unit row is gone; the 4-unit row is unaffected. The restored lot is active
again and can be issued or removed as usual.

**Step 6 — Return 2 of the 4-unit row.** The `Return` form posts 2 against the
4-unit movement. The lot is active, so `returnToActive` runs and the lot's
quantity becomes 3; the movement's outstanding becomes 2.

| Tab | Row | Status | Recipient | Qty | Actions |
| --- | --- | --- | --- | --- | --- |
| `Active` | Ant's Potion | `Active` | — | 3 | `Edit`, `History`, `Movements`, `Issue`, `Remove` |
| `Issued` | Ant's Potion | `Issued` | Joe | 2 | `Return`, `History`, `Movements` |
| `All` | Ant's Potion | `Active` | — | 3 | as `Active` |
| `All` | Ant's Potion | `Issued` | Joe | 2 | `Return`, `History`, `Movements` |

Returning part of an issuance leaves the row visible with the reduced outstanding
quantity. This is the case the current implementation cannot express at all.

## 7. Service and API changes

### 7.1 New input type

```go
// ReturnIssuanceInput closes part or all of one ISSUE movement.
type ReturnIssuanceInput struct {
    MovementID     int64
    ExpectedModAt  time.Time  // the lot's ModAt, as shown in the row
    Quantity       uint32
    Recipient      string
    Note           string
    IdempotencyKey string
    Actor          Actor
    CorrelationID  string
    SourceIP       string
}
```

`ReturnItemInput` stays for active-lot returns (stock returns with no issuance
behind them).

### 7.2 `ReturnIssuance` behaviour

1. Lock the `ISSUE` movement row (`SELECT ... FOR UPDATE`) and its lot row in one
   transaction. Validate the movement type is `ISSUE` and outstanding > 0.
2. Validate `1 <= Quantity <= outstanding`; `409` if the outstanding changed since
   the row was rendered (stale `ExpectedModAt` or stale outstanding).
3. Insert the `RETURN` movement with `RelatedMovementID = MovementID`, the
   recipient, and the correlation/idempotency fields. Reuse `newMovement` and
   `movementAudit` so the audit event stays `ITEM_RETURNED`.
4. Reuse the existing lot-side branches, selected exactly as today — by
   `in.Quantity` against the **lot's `Quantity`** (the original issued amount),
   not against the movement's outstanding. This preserves the approved OD-02 and
   OD-03 arithmetic; keying on outstanding would double-count units when a
   whole-stack issue is returned in parts.

   | Lot state | Return quantity vs lot `Quantity` | Branch |
   | --- | --- | --- |
   | active | any | `returnToActive` (quantity increases, lot stays active) |
   | soft-deleted with a recipient | equal | `returnWholeIssued` (restores the row, clears `GivenTo`) |
   | soft-deleted with a recipient | less | `returnPartialIssued` (new active lot for the returned units; the issued row stays visible with reduced outstanding) |
   | soft-deleted without a recipient | any | new active lot for the returned units (same mechanics as `returnPartialIssued`); the removed lot stays removed |

   The removed-lot case never un-removes the officer's administrative decision and
   never dead-ends the `Return` action, which the normative requirement forbids
   (decision D6).
   The partial-return-of-a-whole-stack case leaves the original lot soft-deleted
   with its recipient; section 5.3 suppresses that lot row from `All`, so it does
   not reappear as a duplicate issued row.
5. Commit the movement, lot update, and audit row atomically, as today.

This preserves every approved arithmetic rule (OD-02, OD-03) and only adds the
attribution link and the movement-targeted entry point.

### 7.3 Concurrency

Two officers returning against the same issuance serialize on the movement row
lock; the second sees the updated outstanding and either succeeds within the
remaining amount or returns `409`. Deadlock retry reuses
`withDeadlockRetry("return", ...)` (`admin_movement.go:310-312`).

### 7.4 JSON API

The JSON API contract is unchanged (decision D3). The existing
`POST /api/v1/items/{id}/return` keeps its lot-level semantics and gains no
`movementId` field in this delivery. If a later change adds one, it must be
validated against the same outstanding rule.

## 8. Legacy lots are not a design constraint

The owner's direction is explicit: do not optimize the design around legacy lots.
Legacy/test inventory can be deleted and re-imported, so the design targets correct
future behaviour only.

Consequences:

- The `Issued` projection is **movement-only**. There is no `UNION ALL` with
  pre-ledger issued lots.
- A pre-ledger issued lot (`DeletedFg = 1`, `GivenTo <> ''`, no `ISSUE` movement)
  does not appear in `Issued`. It still appears in `All` as a lot row with the
  `Issued` label (the `Removed` predicate excludes rows with a recipient), and its
  lot-level `Return` route keeps working as it does today.
- No backfill invents `ISSUE` movements for legacy lots. A backfill would have to
  fabricate quantities, timestamps, and actors that the ledger never recorded, so
  it is rejected rather than deferred.
- The only schema migration is the additive `0007` in section 3.2. No movement or
  lot row is inserted, updated, or deleted; the one-time attribution backfill in
  section 11 only fills the new column.
- If a pre-ledger lot is returned through the lot-level route, the resulting
  `RETURN` has `RelatedMovementID = NULL` and still produces no issued row. That is
  acceptable because the lot is legacy and slated for re-import.

The `All` tab therefore shows two kinds of row only: lot rows and movement-derived
issued rows. Section 5.3's duplicate-suppression rule concerns movement-derived
rows exclusively.

## 9. Alternatives considered and rejected

| Alternative | Why rejected |
| --- | --- |
| Aggregate `SUM(ISSUE) - SUM(RETURN)` per lot+recipient | Owner explicitly rejected; loses per-issuance identity and the per-row Return action. |
| Split the lot on partial issue (create a child row for the issued quantity) | Duplicates lots, complicates consolidation/merge, changes on-hand semantics, and rewrites lot history. |
| Mark the lot `DeletedFg = 1` on partial issue | Hides the remaining on-hand stock from the Active view and breaks totals. |
| Separate Issued/Movements report screen | Explicitly out of scope; the owner wants the workflow on the inventory page. |
| Reuse `CorrelationID` for attribution | Returns are separate requests; correlation IDs are per-request and not persisted for the return. |
| Recipient+lot grouping | Merges multiple partial issues to the same person; violates "one row per issuance". |
| Read-only virtual table / view | Still needs the attribution column to compute outstanding per issuance; no benefit over the projection. |

## 10. Edge cases and invariants

| Case | Behaviour |
| --- | --- |
| Partial issue, then return part of it | Row outstanding decreases; original lot quantity increases; row stays `Issued` until outstanding is 0. |
| Partial issue, then return all of it | Row leaves `Issued`; lot quantity returns to its pre-issue value. |
| Whole-stack issue, full return | Row leaves `Issued`; lot restored to active, `GivenTo` cleared (OD-02). |
| Whole-stack issue, partial return | Row outstanding decreases; a new active lot is created for the returned units (existing OD-03 behaviour); the issued row stays visible with the reduced outstanding, and the original soft-deleted lot stays suppressed from `All`. |
| Return more than outstanding | `422` on `quantity`; never negative outstanding. |
| Two issues to the same recipient from one lot | Two distinct rows, each with its own Return action. |
| Lot removed after a partial issue | The issued row stays visible and returnable. If the lot is still active, the return increases its quantity; if the lot was removed administratively, the returned units land in a new active lot and the removed lot stays removed. |
| Lot consolidated into another lot | Issued row still displays the original lot's name; `ConsolidatedInto` is surfaced as today. Return follows the existing consolidation rules. |
| Pre-ledger issued lot (no `ISSUE` movement) | Not shown in `Issued`; remains a lot row in `All` with the lot-level Return. Section 8. |
| Idempotent replay | Unique `IdempotencyKey` returns the original movement; no duplicate issued row. |
| On-hand total | Never includes movement quantities; unchanged by this design. |

## 11. Migration and rollback

- **Migration:** additive `0007` only (nullable column, index, self-FK). No row
  counts change, no change to `GuildBankItems`, and no movement is fabricated.
- **Attribution backfill (one-time, idempotent).** Existing `RETURN` rows have
  `RelatedMovementID = NULL`, so the projection would overstate outstanding on
  their `ISSUE` movements. The release therefore runs a reconciliation that
  attributes each unattributed `RETURN` to the earliest `ISSUE` movement on the
  same `GuildBankItemID` whose outstanding still exceeds the returned quantity
  (FIFO). Returns that cannot be attributed stay `NULL` and are ignored by the
  projection. This is *attribution* backfill, not *issuance* backfill: it creates
  no movements and invents no quantities, so it does not contradict section 8.
- **Rollback:** dropping the column and index restores the previous schema. The
  application then falls back to lot-derived `Issued` rows, so partial issues
  become invisible again; no lot, movement, or quantity data is lost, but the
  attribution values are discarded and must be recomputed by the backfill on
  redeploy. The rollback note must say so and list the affected movements.
- **Deployment order:** migrate → run the attribution backfill → deploy the read
  projection and the movement-targeted return route. Running the backfill before
  the projection goes live prevents phantom issued rows for issues that were
  already returned. Returns written by the old code during the window are picked
  up by the same backfill; returns written after deployment set the column
  directly.

## 12. Acceptance criteria

1. Issuing 1 of 5 units from a lot leaves the lot `Active` with 4 units and adds
   one `Issued` row for 1 unit, recipient "Joe" shown, `Return` action present.
   This is the behaviour that does not exist today.
2. Returning 1 from that row restores the lot to 5 units and removes the row from
   `Issued`; the `RETURN` movement stays in `Movements`.
3. Issuing 4 of 5 units afterwards produces a distinct `Issued` row for 4 units;
   the first row's history does not merge into it.
4. Issuing the last 1 unit as a whole-stack issue removes the lot from `Active`
   and leaves two separate `Issued` rows (4 and 1) in `All`, with no lot row.
5. Returning the whole 1-unit row restores the lot to `Active` with 1 unit and
   `GivenTo` cleared; the 4-unit row is unaffected.
6. Returning 2 of the 4-unit row leaves that row at 2 outstanding and the lot
   `Active` with 3 units.
7. `Removed` rows still show `Restore`, and restoring them still works.
8. On-hand totals before and after each operation match the pre-design values;
   movement quantities never enter the on-hand total.
9. Concurrent returns against one issuance produce one success and one `409`.
10. No new page or report is required; every action is reachable from the
    inventory list.
11. Every outstanding issued row offers a working `Return`; if its lot was removed
    administratively, the returned units land in a new active lot rather than
    failing the action.

## 13. Test plan (design level)

- **Repository:** outstanding computation with 0, 1, and many returns; exclusion of
  zero-outstanding rows; duplicate suppression on `All`; filters and sort mapping;
  page/total snapshot consistency; a partially issued lot appears in both `Active`
  and `Issued`.
- **Service:** return by movement for partial and whole-stack issues; over-return
  rejected; stale `ExpectedModAt` rejected; idempotency replay; audit event and
  movement row written atomically; deadlock retry.
- **API/UI:** new routes require auth and CSRF; `Return` link present only on
  issued rows; `Restore` unchanged; `409`/`422` semantics; empty-state hint no
  longer fires for a partially issued lot.
- **Migration:** applies and rolls back cleanly; no row counts change; FK and
  index exist.
- **Attribution backfill:** idempotent on a second run; FIFO attribution for
  multiple returns against one lot; unattributable returns stay `NULL`; no
  movement rows are created.
- **Manual:** the eleven acceptance criteria above, executed against a seeded lot
  of 5 units.

## 14. Decisions (approved by the owner)

All six questions are closed. The identifiers `D1`–`D6` are referenced from the
sections above and from the implementation plan.

| ID | Decision | Consequence in this design |
| --- | --- | --- |
| D1 | Returned units go back into the **same active lot** when that lot is still active. | `returnToActive` is the branch for active lots (section 7.2). No new lot is created for a partial issue from an active lot. |
| D2 | A fully returned issuance is **hidden** from `Issued`; it does not remain as a zero row. | The projection filters `outstanding > 0` (section 5.2). `Movements` still shows the `RETURN` history. |
| D3 | The **JSON API is unchanged**. | Section 7.4: no `movementId` field on `POST /api/v1/items/{id}/return` in this delivery. |
| D4 | The `id` filter on the `Issued` tab matches **both** the lot ID and the movement ID. | Section 5.4: `(m.GuildBankItemID = ? OR m.MovementID = ?)`. |
| D5 | **No extra column** is added to the table. | Section 6: the action cell gains `Return`; issue date and actor are exposed as view fields for the status-cell tooltip only. |
| D6 | Returning from a **removed** lot creates a **new active lot** for the returned units. | Section 7.2: the removed lot stays removed; the `Return` action never dead-ends. |

### 14.1 Implementation plan

The build sequence, file-level change list, and verification steps are in
`Docs/AdminInventoryIssuedRowsImplementationPlan.md`. This document remains the
behavioural specification; the plan document is the execution checklist.
