# Inventory movement and `GivenTo` business rules

**Status:** review-only; **no decision approved**, no application code, template,
route, migration or runtime behavior changed.
**Date:** 2026-11-22
**Revision (2026-11-23):** the owner approved **Option A** for the stackability question (OD-01 revised): partial issue is governed by the on-hand lot quantity, not `EQUID` stackability. Any active lot with `Quantity > 1` supports partial issue; a `Quantity = 1` lot is issued whole; stackability metadata is advisory display only and never gates a write. The stackability-gate discussion in §7 and the D-ISS-13 records below are superseded by this rule.
**Scope:** H-02 and H-05 in `Docs/OfficerUsabilityReviewV1.md`.
**Purpose:** define the intended business rules for inventory issuance, receipt,
movement, correction, removal, restoration and public visibility so H-02 (item
summary) and H-05 (issue/receive/move workflow) describe the same stock model.

This document is a decision aid, not an implementation plan. It does not reopen a
phase and does not propose a new phase. Every decision below is marked **Proposed**
until the owner accepts or rejects it.

## 1. Direct answers

1. **What is the meaning of `GivenTo` today?** It is an optional, free-text
   recipient field with no enforced business meaning. The design calls it
   “Optional text field” and gives `Marc`, `Icefear` and `Alt Bank` as examples
   (`Design.md` L351–L358). The admin form labels it “Recipient”
   (`internal/web/templates/admin_inventory_form.html` L65–L68). Setting it does
   not change quantity, availability, public visibility or removal state.
2. **What should `GivenTo` mean?** It should record the recipient of a
   **completed, whole-record issue**. It is the recipient of the entire record,
   and it remains as a historical snapshot after the record is soft-removed. It is
   **not** a reservation/allocation, **not** a free-text note, and **not** the
   distribution action itself. The action is a movement; `GivenTo` is its
   recipient snapshot.
3. **What should happen to a stack of 55 when 10 are given away?** Reduce the
   stack quantity from 55 to 45 and write a separate issuance movement for 10.
   Do **not** split the stack into a new active row. If all 55 are given away,
   set `GivenTo` and soft-remove the whole record. See §6.
4. **How should non-stackable items differ?** *(Revised 2026-11-23.)* Partial
   issue is decided by the on-hand quantity, not the stackability label: any
   active record with `Quantity > 1` may be issued partially, and a `Quantity = 1`
   record is issued whole. `EQUID` stackability is advisory display only and does
   not block a write. See §7.
5. **How should issuance interact with the rest of the system?** Every issue,
   receive, move, adjustment, removal and restoration must be auditable and must
   leave the active quantity and public projection consistent. See §8–§12.
6. **What model should H-02 and H-05 share?** A **stock-lot model with a movement
   ledger**: `GuildBankItems` remains the current stock lot; an immutable
   movement record explains each change; H-02 summarizes active lots by `EQUID`;
   H-05 writes movements against specific lots. See §5 and §13.

## 2. What the current system actually does

| Area | Current behavior | Evidence |
|---|---|---|
| `GivenTo` storage | Optional `VARCHAR(128) NULL`; indexed for admin filtering. | `Docs/DatabaseDesign.md` L272, L284 |
| `GivenTo` meaning | Design says only “Optional text field”; examples are names and “Alt Bank”. | `Design.md` L351–L358 |
| Admin edit | Quantity, source location, recipient and both dates are edited in one generic form. | `admin_inventory_form.html` L60–L75 |
| Admin detail | Shows “Recipient” and “Quantity” as separate read-only facts. | `admin_inventory_detail.html` L20–L26 |
| Public projection | Deliberately omits `SourceLocation`, `GivenTo`, `ModAt`, `ModBy`, `ModByUserID`, `DeletedFg` and `NameSort`. | `internal/domain/inventory.go` L56–L71 |
| Public rows | Only `DeletedFg = 0` rows are read. | `internal/repository/mariadb/inventory.go` L98, L103 |
| Availability | Derived from `DateOpen` and `DateBazaar`; `GivenTo` is not an input. | `internal/service/inventory.go`; `Docs/Phase4ImplementationPlan.md` L481–L485 |
| Quantity | One row stores the quantity of one imported or manually created line; ordinary items are editable `1..100000`; spells are mergeable. | `Docs/ItemQuantityModelReview.md` L18–L27 |
| Partial giveaway | Not modeled. The documented workaround is editing quantity down or a future split action. | `Docs/ItemQuantityModelReview.md` L71 |
| Removal | Whole-row soft delete; no physical deletion. | `Design.md` L359–L368; `Docs/DatabaseDesign.md` L511–L523 |
| Restore | Clears `DeletedFg`; records `ITEM_UPDATED` with note `restored`; does not change `GivenTo`. | `internal/service/admin_inventory.go` L864–L918 |
| Consolidated rows | A row absorbed by consolidation cannot be restored. | `Docs/DatabaseDesign.md` L303; `internal/service/admin_inventory.go` L916–L918 |
| Audit | Quantity, `givenTo`, location and date changes are written as `ITEM_UPDATED`; removal as `ITEM_REMOVED`; restore as `ITEM_UPDATED` with note `restored`. | `internal/service/admin_inventory.go` L648–L918; `Docs/DatabaseDesign.md` L513–L523 |
| Public totals | Count inventory records, not distinct item names or units. | `Docs/Phase4ImplementationPlan.md` L281 |

The decisive ambiguity is this: an officer can set `GivenTo = Marc` on an active
row of 55, save, and the row remains active and publicly visible with quantity
55. The system has no rule saying whether Marc received the whole stack, is
expected to receive it, has borrowed part of it, or is only a note. H-05 exists
because that ambiguity is being used to represent a real stock movement.

## 3. The core problem with overloading `GivenTo`

`GivenTo` cannot simultaneously be all of the following:

- a **reservation** (“hold this for Marc”),
- a **completed handout** (“Marc has it now”),
- a **partial distribution** (“Marc has 10 of the 55”),
- a **location** (“Alt Bank”),
- a **note** (“Reserved for the next raid”).

If it is a reservation, the item is still on hand and should remain public. If it
is a completed handout, the item has left guild stock and should not remain
public. If it is a partial handout, one row-level text field cannot represent the
remaining and issued portions without also changing quantity. If it is a
location, it duplicates `SourceLocation`. If it is a note, it should not be
called “Recipient.”

The recommendation is to give `GivenTo` one narrow meaning and represent every
other concept elsewhere.

## 4. Recommended meaning of `GivenTo`

**Recommended definition:** `GivenTo` is the recipient snapshot of a completed,
whole-record issue. It is set when the entire quantity of one record leaves guild
stock for a named member or custody label. The record is soft-removed in the same
transaction. The field remains on the removed row as history.

This means:

| Question | Answer |
|---|---|
| Recipient of the entire record? | **Yes**, but only when the whole record is issued. A partial issue leaves the record active and does not set `GivenTo`. |
| Historical recipient? | **Yes, as a consequence.** The recipient is preserved on the soft-removed record. It is not a live “current holder” field for active stock. |
| Distribution action? | **No.** The action is an `Issue` movement. `GivenTo` is the recipient value carried by that movement and, for whole-record issues, copied onto the removed record. |
| Reservation or allocation? | **No.** If the guild needs reservations, add a separate `ReservedFor`/`AllocatedTo` concept with explicit visibility rules. Do not overload `GivenTo`. |
| Free-text note? | **No.** Notes belong in audit notes or a dedicated note field. |
| Location? | **No.** “Alt Bank” is a location/custody destination; use `SourceLocation` or a move action. |

**Invariant proposed for new writes:** an active record must not have a non-empty
`GivenTo`. `GivenTo` is populated only on a record that is being or has been
issued as a whole. Legacy active rows that already contain a recipient are data to
review, not a model to preserve.

**Why not make `GivenTo` a live allocation?** An allocation does not remove stock.
If `GivenTo` means “allocated,” the public inventory would still show the item,
and the officer would need a second action to issue it. That is two meanings for
one field. If the guild wants reservations, the reservation should be a separate
state with its own rule for whether reserved stock is public. The recommended V1
model does not add reservations.

## 5. Recommended stock model

The model has four layers. It deliberately keeps `GuildBankItems` as the current
stock table rather than replacing it.

| Layer | Entity | Responsibility |
|---|---|---|
| Item definition | `Items` | `EQUID`, name, class, race and item metadata. Read-only to the application. |
| Stock lot | `GuildBankItems` row | Current quantity of one `EQUID` at one location, with lifecycle dates and active/removed state. This is the on-hand record. |
| Movement | Proposed `InventoryMovement` record | Immutable event describing an issue, receive, move, adjustment, write-off or return. It names the lot, quantity, recipient, location, actor and time. |
| Audit | Existing `Audit` | Security and accountability log. It records the same event in the existing audit vocabulary or an approved extension. |

The movement ledger is the recommended operational history. It is not a second
stock table: it must not be used to compute on-hand quantity unless it reconciles
with the lot. The lot is the current state; the movement ledger explains how the
lot reached that state.

### 5.1 Invariants

1. **One lot, one quantity.** A lot's `Quantity` is the number of identical units
   currently on hand in that lot. `Quantity >= 1` for every stored row.
2. **No zero-quantity rows.** A lot that reaches zero is soft-removed; it is not
   stored with quantity zero.
3. **Active means on hand.** An active lot's quantity is public on-hand stock.
   Removed lots are not on hand and are not public.
4. **`GivenTo` is not a stock effect.** Setting or clearing the field never
   changes quantity or availability by itself. Only an issue/removal does.
5. **Every quantity change has a movement.** No workflow may change quantity
   without a movement record and an audit entry. A correction is an `ADJUST`
   movement, not a silent edit.
6. **No automatic merging.** Receiving or moving stock does not silently merge
   into an existing lot unless the officer explicitly selects that lot. Multiple
   lots per `EQUID` are legal and expected.
7. **Dates are lot-level.** `DateAdded`, `DateOpen` and `DateBazaar` belong to the
   lot. A partial issue or partial move does not rewrite them. A new receive gets
   new dates from the settings.
8. **Public privacy is unchanged.** Public output never includes `GivenTo`,
   `SourceLocation`, moderator identity or removed rows.
9. **Non-stackable means one unit.** A non-stackable lot has `Quantity = 1`.
10. **Whole-record issue is the only way `GivenTo` is set.** Partial issue uses
    the movement ledger and leaves the active lot's `GivenTo` empty.

### 5.2 What “stack” means

A stack is a **lot**, not a separate entity. “Batwing Crunchies Qty 55” is one lot
with quantity 55. Two imports of the same `EQUID` create two lots; H-02 groups
them by `EQUID` for the item summary. The stack drill-down shows each lot and its
quantity, location, dates, availability and status. This is why H-02's “multiple
stacks” indicator is a lot-count indicator, not a duplicate-name warning.

### 5.3 Movement types

The following vocabulary is proposed. Names are not final until the owner approves
the audit/event taxonomy.

| Type | Meaning | Quantity effect |
|---|---|---|
| `RECEIVE` | Stock added to a lot, or a new lot created. | Increase |
| `ISSUE` | Stock given to a member or custody recipient. | Decrease or whole-record removal |
| `MOVE` | Stock moved between locations without changing total on hand. | Decrease source, increase destination |
| `ADJUST` | Correction after a count, loss or data fix. | Increase or decrease |
| `WRITE_OFF` | Stock discarded, destroyed or lost. | Decrease |
| `RETURN` | Previously issued stock comes back. | Increase, or restore a removed lot |

`ISSUE` and `WRITE_OFF` both reduce on-hand stock, but they are not the same
business event. A handout has a recipient; a write-off does not. Reports and audit
must distinguish them.

## 6. Stacked records: the Batwing Crunchies case

**Scenario:** one active lot, `Batwing Crunchies`, `Quantity = 55`. The officer
gives 10 to Marc.

### 6.1 Recommended outcome

| Step | Result |
|---|---|
| 1. Lock and validate the lot | Read the lot with its `ModAt`/expected version and confirm it is active and still has 55. |
| 2. Calculate the result | `55 - 10 = 45`. The result is greater than zero, so the lot stays active. |
| 3. Update the lot | Set `Quantity = 45`. Do not change `GivenTo`; the lot is still on hand. |
| 4. Write the movement | Insert one `ISSUE` movement: lot ID, `EQUID`, quantity 10, recipient `Marc`, timestamp, actor, correlation ID and optional note. |
| 5. Write the audit | Write one `ITEM_UPDATED` for the quantity change `55 -> 45`, linked to the movement. The movement carries the recipient; the audit does not need to overload `GivenTo`. |
| 6. Publish | Public list shows 45. Admin item summary shows 45 on hand. Admin history shows “10 issued to Marc.” |

This is **quantity reduction plus a separate issuance record**. The issuance
record is not an active inventory row. It is the movement ledger entry. The
original lot remains the single on-hand lot.

### 6.2 Why not split into a new active row?

A split into a new active row would create a second active lot of 10. H-02 would
then show 55 on hand unless the original lot is also reduced. If the original lot
is reduced to 45 and the new row is active, the total is still 55—but the 10 are
now shown as guild stock, which is wrong for a completed handout. If the new row
is removed, it is just a history row with extra inventory-table overhead. The
split also duplicates lifecycle dates, location and provenance for a transaction
that did not change location or provenance.

A split is appropriate when the **location** changes, because location is a
row-level attribute. It is not appropriate when only the **recipient** changes,
because recipient is not a row-level stock attribute in the recommended model.

### 6.3 Rejected options

| Option | Verdict | Reason |
|---|---|---|
| Reduce quantity only, no history | Rejected | The stock total would be right, but the guild could not answer “who got the 10?” except by reading a generic audit payload. |
| Split into a new active row | Rejected for issue | Double-counts public stock or creates an active row that is not really on hand. |
| Split into a new removed row | Rejected for issue | Preserves the recipient but adds an inventory row that is not a lot. Use the movement ledger instead. |
| Change only `GivenTo` to `Marc` | Rejected | Leaves quantity 55 and the row active/public. The system would claim Marc has the item while still showing all 55 on hand. |
| Separate issuance record with no quantity reduction | Rejected | The movement says 10 left, but the lot still says 55. The two records disagree. |

### 6.4 If all 55 are given away

The result would be zero, which the schema does not allow. The recommended action
is a **whole-record issue**:

1. Set `GivenTo = Marc`.
2. Soft-remove the lot (`DeletedFg = 1`) in the same transaction.
3. Write one `ISSUE` movement for quantity 55.
4. Write `ITEM_REMOVED` (and `ITEM_UPDATED` for the recipient change if the
   implementation records `GivenTo` as an edit).
5. Public list hides the lot. Admin history shows the issue and the recipient.

The removed row remains available for audit and restoration. It is not physically
deleted. This preserves the existing soft-delete model.

### 6.5 Multiple lots for the same item

If H-02 shows two active lots of the same `EQUID`, the issue action must name the
lot it is drawing from. The recommended V1 rule is:

- Issue from **one selected lot** by default.
- If the requested quantity exceeds that lot, show the shortfall and require the
  officer to select another lot; do not silently draw across lots.
- If the officer intentionally draws across lots, write one movement per lot so
  each lot's quantity and history stay correct.
- Do not merge lots during issue. Merging is a separate owner-run consolidation
  action, not an issue side effect.

This keeps H-05 predictable and makes H-02's drill-down meaningful.

## 7. Non-stackable items

### 7.1 The missing model fact

The current schema has no stackability flag. `ItemType` is `Item` or `Spell`; it
does not distinguish a stack of arrows from a single sword. The quantity model
also allows an `Item` row to have quantity greater than 1. Therefore the system
cannot currently tell whether “quantity 5” means five identical stackable units or
one row that should have been five rows.

This is a business-model gap, not a UI gap. The owner must choose the source of
truth for stackability:

- an item-metadata flag on `Items` (recommended), or
- an explicit per-lot/import attribute, or
- an operational rule that the owner accepts as a proxy.

**Recommended:** add stackability as item metadata (`EQUID`-level), not as a
per-row guess. The application should not infer stackability from the item name,
the `ItemType`, or the current quantity. A stackable item can have quantity 1; a
non-stackable item must never have quantity greater than 1.

### 7.2 Rules for non-stackable items

| Rule | Behavior |
|---|---|
| Representation | One physical unit per lot. `Quantity = 1`. |
| Issue | The whole lot is issued. There is no partial quantity. |
| Recipient | `GivenTo` is set on the whole-record issue. |
| Removal | The lot is soft-removed in the same transaction as the issue. |
| Movement | One `ISSUE` movement with quantity 1. |
| Public result | The lot disappears from public inventory. |
| Bulk issue | Select multiple non-stackable lots; each lot gets its own movement and audit entry. |
| Invalid quantity | If a non-stackable lot has `Quantity > 1`, block the issue and require correction or splitting. |

### 7.3 How issuance differs from a stackable lot

The action name is the same—**Issue**—but the allowed quantity differs:

- **Stackable lot:** allowed issue quantity is `1..Quantity`; partial issue is
  permitted and reduces the lot.
- **Non-stackable lot:** allowed issue quantity is exactly `Quantity`, which must
  be 1; the whole lot is removed.

The UI should not show a quantity field for a non-stackable lot. It should show
“Issue this item” with a fixed quantity of 1. If the owner wants to issue several
identical non-stackable items, the officer selects several lots and the UI shows
the total number of lots/items.

### 7.4 If a non-stackable lot has quantity greater than 1

This is a data-quality condition. The recommended handling is:

1. Do not partially issue it.
2. Show the officer that the lot is non-stackable but has quantity greater than 1.
3. Offer a correction path: split it into quantity-1 lots, or adjust the quantity
   if the metadata/quantity is wrong.
4. Record the correction as an `ADJUST` movement or a controlled split, with
   audit.

A split into quantity-1 lots is acceptable here because the physical model really
is one unit per row. This is the opposite of the stackable issue case, where
splitting is rejected.

### 7.5 Resolution (owner-approved Option A, 2026-11-23)

The owner settled this open model choice by making partial issuance
quantity-based, so no stackability source of truth is required for the write
path:

- Treat `Quantity = 1` as a single unit: only whole-lot issue.
- Treat `Quantity > 1` as a stack: partial issue is allowed.
- `EQUID` stackability is advisory metadata used for display only. It is never
  read on a write path and never blocks an issue.

This replaces the earlier temporary proxy and the proposed `EQUID` metadata
gate. The D-ISS-13 record below is updated to match.

## 8. Movement workflows and rules

### 8.1 Receive / add stock

**Purpose:** record stock entering the guild bank.

- If the officer selects an existing lot, increase that lot's quantity and write a
  `RECEIVE` movement. The lot keeps its original `DateAdded`, `DateOpen` and
  `DateBazaar`.
- If the officer creates a new lot, compute `DateAdded = today` (UTC) and the two
  lifecycle dates from the current settings, as manual create does today.
- Do not silently merge into an existing lot. If more than one matching lot
  exists, require a choice.
- A receive does not set `GivenTo`.
- Public on-hand quantity increases by the received quantity.

### 8.2 Move / transfer location

**Purpose:** move stock between bank tabs, houses, alts or other locations without
changing total guild stock.

- **Move all:** update the lot's `SourceLocation`/`LocationGroup`/`LocationSort`
  and write a `MOVE` movement. Quantity is unchanged.
- **Move part:** reduce the source lot by the moved quantity and create a new lot
  at the destination with the moved quantity. The new lot inherits the source
  lot's `DateAdded`, `DateOpen` and `DateBazaar`, because the units are the same
  units and did not re-enter the guild. Write one `MOVE` movement with source and
  destination lot IDs.
- If the moved quantity equals the source quantity, use the move-all path rather
  than creating a zero-quantity source lot.
- Moving does not set `GivenTo` and does not change the total on-hand quantity.
- H-02 should still show one item total after a move; the stack count and location
  mix change.

### 8.3 Adjust / correct quantity

**Purpose:** correct a count or fix a data error without pretending the change was
a handout or a receipt.

- An adjustment may increase or decrease quantity.
- It must have a reason and an audit entry.
- If the adjustment would reduce a lot to zero, use a whole-lot removal or a
  write-off path instead of storing zero.
- An adjustment does not set `GivenTo`.
- The generic edit form must not be the primary way to perform a stock movement.
  If the form remains, it should be labelled as a correction and should require a
  reason. The H-05 actions should be the normal path.

### 8.4 Remove / write off

**Purpose:** remove stock from the guild bank without giving it to a member.

- **Remove whole lot:** soft-delete the lot. This removes all units in the lot.
  The UI must show the total quantity and ask for confirmation.
- **Write off part of a lot:** reduce the lot quantity and write a `WRITE_OFF`
  movement. This is not a whole-record removal. If the result is zero, remove the
  lot.
- Removal does not set `GivenTo`; a write-off has no recipient.
- Bulk removal should show both the number of records and the total number of
  units affected. “2 records” is not enough when one record contains 55 units.
- Removal is not the same as issuance. An issued lot has a recipient and an
  `ISSUE` movement; a removed/written-off lot has no recipient and a removal or
  `WRITE_OFF` movement.

### 8.5 Restore / return

**Purpose:** undo a removal or record returned stock.

- **Restore a removed lot:** clear `DeletedFg`, keep the lot's quantity and dates,
  and write a `RETURN` movement. If the lot was issued as a whole, also clear
  `GivenTo` so the active lot no longer claims a recipient. If the owner wants to
  keep the prior recipient as history, the movement and audit retain it; the
  active row should not.
- **Restore a lot removed for loss/write-off:** clear `DeletedFg` and write a
  `RETURN` movement; there is no recipient to clear.
- **Consolidated rows:** restore remains blocked. The consolidation marker means
  the row was absorbed into a survivor; restoring it would duplicate stock.
- **Returned stock dates:** recommended default is to preserve the original
  `DateAdded`, `DateOpen` and `DateBazaar`. The units never became new stock; they
  are returning. If the owner wants a returned item to receive a fresh
  availability window, the correct action is to receive it as a new lot and leave
  the old row removed. That is an owner decision, not an automatic restore rule.
- Restoration must not resurrect a lot that was partially issued and then removed
  with a different remaining quantity without preserving the prior movement
  history. The movement ledger should show the original issue, the removal and the
  return.

### 8.6 Quantity editing

The current generic edit form lets an officer change quantity, recipient, location
and dates in one save. That is the source of the H-05 ambiguity. Recommended rule:

- **Do not use direct quantity editing for normal stock movement.** Use Receive,
  Issue, Move, Adjust, Write off or Remove.
- **Keep a correction path** for data repair. It must be labelled as a correction,
  require a reason, and write an `ADJUST` movement plus audit.
- **Do not allow quantity and recipient to be changed together in one ambiguous
  save.** A whole-record issue is a named action; a partial issue is a movement.
- **Do not allow quantity to be changed to zero.** Use removal.
- The quantity-correction guard is quantity-based: a correction must not set a
  lot's quantity above what the officer can justify, and partial issue is
  allowed for any active lot with `Quantity > 1` regardless of stackability
  metadata (OD-01 revised 2026-11-23).

If the owner decides to keep the generic edit form for V1, the minimum safe change
is to separate “correct quantity” from “issue to recipient” and to show the
before/after quantity and the public effect.

## 9. Interaction rules

### 9.1 Audit history

Issuance must be visible in history without requiring the officer to interpret a
generic JSON diff. The recommended rules are:

- Write one movement record for each issue, receive, move, adjustment, write-off
  or return. The record carries the lot ID, `EQUID`, quantity, recipient (for
  issues), source and destination location (for moves), reason, actor, timestamp
  and correlation ID.
- Write one audit entry for the same event. Use the existing `ITEM_UPDATED`,
  `ITEM_REMOVED` and `ITEM_CREATED` vocabulary where it fits. If the owner
  approves a richer taxonomy, use distinct movement event names; otherwise put a
  clear movement type in the audit notes and structured fields.
- The audit entry must show the old and new quantity for a partial issue. For a
  whole-record issue, it must show the removal and the recipient. “Restored” is
  not enough; a return should say what was returned and from whom.
- The movement ledger is the operational history H-02 can query by `EQUID`. The
  audit table remains the security record. Do not rely on the audit table as the
  only item-level history if H-02 needs to summarize movements across lots.
- Do not write a `GivenTo`-only audit entry and call it an issue. That is the
  current ambiguity in a different form.

### 9.2 Removal

- Removal is a whole-lot action. It removes all units in the lot.
- A partial removal is a write-off or adjustment, not a removal.
- Removal of an issued lot is the second half of a whole-record issue. The first
  half is setting the recipient and writing the issue movement.
- Removal of a lost/discarded lot has no recipient. It should not populate
  `GivenTo`.
- The UI must show the unit count, not only the record count. A bulk removal of
  three lots could remove 3 units or 300 units.
- Removal remains soft delete. The row stays available for audit and restoration
  unless it was consolidated.

### 9.3 Restoration

- Restoration is allowed only for removed rows that were not consolidated.
- Restoring an issued whole lot clears `GivenTo` on the active row and writes a
  `RETURN` movement. The historical recipient remains in the movement and audit.
- Restoring a lost/discarded lot writes a `RETURN` movement with no recipient.
- Restoring a row does not undo earlier quantity edits. The current quantity is
  restored as it was at removal time. If the owner needs to reverse the original
  issue and restore the pre-issue quantity, that is a separate correction, not a
  plain restore.
- Restoration preserves the lot's dates by default. A returned item does not get
  a new availability window unless the owner explicitly receives it as new stock.
- The existing restore gate for consolidated rows remains unchanged.

### 9.4 Quantity editing

- Quantity editing is a correction tool, not a movement tool.
- Every quantity change must be audited and should have a movement record.
- A quantity edit must not also set `GivenTo`; that would recreate the ambiguity.
- A quantity edit to zero is a removal, not an edit.
- Partial issue is allowed for any active lot with `Quantity > 1`; stackability
  metadata is advisory and does not gate a quantity correction (OD-01 revised
  2026-11-23).
- The generic edit form can remain for corrections, but the normal daily actions
  should be named actions with before/after quantities and explicit effects.

### 9.5 Dates

- `DateAdded` is immutable. It is the date the lot entered guild inventory.
- `DateOpen` and `DateBazaar` are computed at creation from the settings and can be
  edited on the lot. They are not changed by an issue.
- A partial issue does not change the lot's dates. The remaining units keep the
  original availability window.
- A partial move inherits the source lot's dates. The units did not re-enter the
  guild.
- A receive into a new lot gets new dates. A receive into an existing lot keeps
  that lot's dates.
- An issue's business date is the movement timestamp, not a change to the lot's
  lifecycle dates.
- A restore preserves the dates by default. If the owner wants returned stock to
  have a fresh window, use a receive-as-new-lot path instead of restore.

### 9.6 Public inventory visibility

- Public inventory shows active lots only. Removed lots are hidden.
- Public inventory shows the lot's on-hand quantity. A partial issue reduces that
  quantity; a whole issue removes the lot.
- Public inventory does not show `GivenTo`, `SourceLocation`, moderator identity or
  audit details. The existing public projection already omits these fields and
  should not be widened.
- Public availability remains date-based. `GivenTo` must never change availability.
  An active lot with a legacy recipient is still active and still public unless it
  is removed.
- If H-02 is applied to the public list, the public total must be the sum of
  on-hand quantities for active lots, not a count of records. If the public list
  stays record-level, each row's quantity already reflects partial issues. The
  public contract must state which total it shows.
- Public totals must not include issued/removed lots. Otherwise the public sees
  stock the guild no longer has.

### 9.7 Legacy `GivenTo` rows

Existing active rows may already have a recipient. Recommended handling:

1. Do not silently reinterpret them as issued; that would hide stock.
2. Report them as a data-quality list for owner review.
3. For each row, the owner decides whether it is a real whole-record issue (set
   recipient, remove), a reservation (clear recipient or move to a future
   reservation field), or a note (clear recipient).
4. New writes should enforce the invariant that an active row has no recipient.

This is an owner-run cleanup, not an automatic migration. It mirrors the existing
owner-run consolidation approach and avoids guessing intent from free text.

## 10. Reviewing H-02 and H-05 together

H-02 is the **read model**: one row per item, total quantity, stack count,
availability mix and location groups. H-05 is the **write model**: issue, receive,
move and adjust. They must agree on one definition of “on hand.”

### 10.1 Shared definitions

| Term | Definition for H-02 and H-05 |
|---|---|
| Item | One `EQUID` and its name metadata. Multiple lots can share an item. |
| Lot / stack | One active or removed `GuildBankItems` row with its own quantity, location and dates. |
| On hand | Sum of `Quantity` for active lots of the item. Removed lots are excluded. |
| Stack count | Number of active lots for the item. |
| Availability mix | Counts of active lots by `upcoming`, `open` and `bazaar` using the row dates. |
| Location mix | Distinct location groups across active lots. |
| Issued | Quantity recorded in `ISSUE` movements. It is history, not on hand. |
| Received | Quantity recorded in `RECEIVE` movements. It is history. |
| Movement | One issue/receive/move/adjust/write-off/return record against a lot. |

### 10.2 H-02 read rules

- Group active lots by `EQUID` and name. Sum quantity for the total.
- Count active lots for the stack count. Show a “multiple stacks” indicator when
  the count is greater than one.
- Show the availability mix from the lot dates. Do not derive availability from
  `GivenTo`.
- Show location groups, not exact source locations, in the public-facing summary.
  The admin drill-down can show exact locations.
- Expand a row to the individual lots. Each lot shows its ID, quantity, location,
  dates, availability and status. A removed/issued lot appears only in history,
  not in the active total.
- Do not merge lots automatically. H-02 is a read grouping, not a write merge.
- If the owner applies H-02 to the public list, the total must be the sum of
  active quantities. The existing public total counts records; that definition
  must be changed or the public row must clearly say “records,” not “items.”

### 10.3 H-05 write rules

- Every action starts from a specific lot or a specific item summary with a lot
  selection. The officer sees the current quantity and the resulting quantity.
- Issue reduces the selected lot or removes it if the whole quantity is issued.
- Receive increases the selected lot or creates a new lot.
- Move updates the location or splits the lot when only part moves.
- Adjust changes quantity with a reason and audit.
- Remove/write-off removes the whole lot or part of it with a clear business
  meaning.
- Every action preserves the H-02 total definition: active quantities only.
- Every action returns the officer to the same item summary or filtered list
  context, consistent with H-04.

### 10.4 What H-02 must not do

- It must not sum `GivenTo` as if it were quantity.
- It must not count removed/issued lots as on hand.
- It must not merge duplicate lots into a single row in the database.
- It must not hide the stack count; duplicate stacks are a real operational fact.
- It must not expose recipient or exact location to the public.

### 10.5 What H-05 must not do

- It must not set `GivenTo` on an active partial lot.
- It must not split a stack into a new active row for a handout.
- It must not leave a zero-quantity active row.
- It must not auto-merge lots during receive or move.
- It must not use the generic edit form as the only movement path.
- It must not change public privacy rules to make recipient visible.

### 10.6 Recommended sequencing of the decision, not a new phase

The owner should decide the `GivenTo` meaning and the stackability source before
H-05 actions are designed. H-02 can be implemented as a read-only grouping over
the existing lots without waiting for the movement ledger, but its totals must use
the same on-hand definition. H-05 should not ship an action that writes a
different total definition than H-02 reads. This is a design dependency, not a
new project phase.

## 11. Proposed decision records

All records are **Proposed** and require owner approval. None is approved by this
review.

### 11.1 Stock model and `GivenTo`

| ID | Decision | Rationale | Status |
|---|---|---|---|
| D-ISS-01 | Keep `GuildBankItems` as the stock-lot table. A row is one lot with one quantity, one location and its own lifecycle dates. Multiple lots per `EQUID` are legal. | Preserves the approved quantity model, avoids a rewrite, and lets H-02 group lots without changing storage. | Proposed |
| D-ISS-02 | Add an immutable movement ledger as the operational history for issue, receive, move, adjust, write-off and return. The lot remains the current-state record; the ledger explains changes. | Gives H-05 a structured write model and H-02 a queryable item history without parsing audit JSON. | Proposed |
| D-ISS-03 | Define `GivenTo` as the recipient of a completed whole-record issue. It is not a reservation, note, partial-issue field or location. An active row must not carry a recipient. | Removes the current ambiguity and makes the public/on-hand effect of the field explicit. | Proposed |
| D-ISS-04 | A partial issue reduces the lot quantity and writes one `ISSUE` movement. It does not split the lot into a new active row. A whole issue sets `GivenTo` and soft-removes the lot. | Keeps one on-hand lot, avoids double counting, and preserves the recipient in history. | Proposed |
| D-ISS-05 | A partial move reduces the source lot and creates a destination lot; a whole move updates the lot's location. The destination lot inherits the source dates. | Location is a lot attribute, so a physical move needs a destination lot; recipient is not a lot attribute. | Proposed |
| D-ISS-06 | Receives either increase an explicitly chosen existing lot or create a new lot. No automatic merge. A new lot gets the current settings dates. | Preserves provenance and avoids surprise consolidation during daily work. | Proposed |

### 11.2 Operations and lifecycle

| ID | Decision | Rationale | Status |
|---|---|---|---|
| D-ISS-07 | Removal is a whole-lot action; a partial removal is a write-off or adjustment. Bulk removal must show both record count and unit count. | Prevents “remove 2 records” from silently removing 55 units. | Proposed |
| D-ISS-08 | Restoring a whole-issued lot clears `GivenTo` on the active row and writes a `RETURN` movement. The historical recipient remains in the movement/audit. | An active row must not claim a recipient after the item has returned. | Proposed |
| D-ISS-09 | Restore preserves the lot's dates by default. A fresh availability window requires receiving the returned item as a new lot. | Avoids silently changing lifecycle dates during an undo action. | Proposed |
| D-ISS-10 | Quantity editing is a correction path only. It must require a reason, write an `ADJUST` movement and audit, and must not also set `GivenTo`. | Stops the generic edit form from being an ambiguous movement workflow. | Proposed |
| D-ISS-11 | Public inventory shows active on-hand quantity only. It never exposes `GivenTo`, exact source location or removed lots. Availability remains date-based. | Preserves the existing privacy boundary and makes partial issues visible correctly. | Proposed |
| D-ISS-12 | H-02 totals and H-05 actions use the same definition: on hand = sum of active lot quantities. Issued/removed lots are history, not stock. | Prevents the read model and write model from disagreeing. | Proposed |

### 11.3 Open model choices

| ID | Decision | Rationale | Status |
|---|---|---|---|
| D-ISS-13 | Partial-issue eligibility is governed by the on-hand lot quantity, not `EQUID` stackability. Any active lot with `Quantity > 1` supports partial issue; `Quantity = 1` is issued whole. Stackability metadata is advisory display only. | The owner revised OD-01 on 2026-11-23 (Option A) to remove the metadata gate. | **Revised 2026-11-23** |
| D-ISS-14 | The audit taxonomy should either gain distinct movement event names or carry an explicit movement type in structured audit fields. A generic `GivenTo`-only audit entry is not sufficient. | Keeps issue, receive, move and write-off distinguishable in audit and reporting. | Proposed |
| D-ISS-15 | Legacy active rows with `GivenTo` are an owner-reviewed data-quality list. They are not automatically converted to issued or cleared. | Free text cannot reliably distinguish a handout, a reservation and a note. | Proposed |

## 12. Owner decisions required

The review cannot approve these choices. The owner should answer each one before
H-05 is designed.

| # | Question | Recommended answer | Alternative to consider |
|---|---|---|---|
| 1 | Is `GivenTo` a completed handout, a reservation, a note or a location? | Completed whole-record handout. | If reservations are needed, add a separate reservation field/state; do not overload `GivenTo`. |
| 2 | Should a partial issue split the stack or reduce it? | Reduce the stack and write an `ISSUE` movement. | Split only if the guild needs separate loan/consignment lots with their own status. |
| 3 | What is the source of truth for stackability? | Item metadata at `EQUID` level. | Per-import attribute or a manually maintained list; the last is error-prone. |
| 4 | Should a non-stackable row ever have quantity greater than 1? | No. Correct or split it before issue. | Allow it as a multi-item lot only if the owner explicitly defines that exception. |
| 5 | Should public totals count units or records? | Units for an item summary; state the definition explicitly. | Keep record counts for the existing record list and add a separate unit total. |
| 6 | Should returned stock keep its original dates? | Preserve dates on restore; receive as new for a fresh window. | Always reset dates on return; this changes availability semantics. |
| 7 | Should “Alt Bank” be a recipient or a location? | A location/custody destination; use Move, not `GivenTo`. | Treat it as a recipient only if the alt bank is a custody owner, not a place. |
| 8 | Should audit gain new movement event names? | Prefer distinct movement types or a structured movement type field. | Keep `ITEM_UPDATED`/`ITEM_REMOVED` and put movement type in notes/JSON. |
| 9 | Should receive auto-merge same-EQUID lots? | No; require an explicit lot choice. | Auto-merge only when the owner accepts provenance loss and updates H-02. |
| 10 | Should the generic edit form remain? | Keep it as a correction path only; move daily actions to named workflows. | Remove quantity/recipient editing from the generic form entirely. |
| 11 | Should active legacy rows with `GivenTo` be cleaned automatically? | No; owner-reviewed list. | Automatic clear or auto-issue; both risk hiding stock. |
| 12 | Should issued lots be restorable? | Yes, unless consolidated; restore clears recipient and writes a return. | Make issued lots non-restorable and require a new receive. |

## 13. Risks and limitations

1. **Movement ledger is a new model.** It is the cleanest way to answer “who got
   what?” and to give H-02 item-level history, but it is more than a form change.
   The owner may choose to use audit JSON as a temporary history; that is weaker
   for querying and reporting.
2. **Stackability is not modeled.** Until the owner chooses a source of truth,
   the application cannot reliably distinguish a stack from a single item. The
   quantity proxy in §7.5 is temporary and should not be presented as complete.
3. **Legacy `GivenTo` data is ambiguous.** Active rows with recipients may be
   reservations, notes or unrecorded handouts. Automatic conversion would guess.
4. **Public totals may change meaning.** The current public total counts records.
   An item summary that counts units is a different contract and must be stated.
5. **Returned stock dates are a policy choice.** Preserving dates is the least
   surprising default, but it may make returned stock immediately available.
6. **Bulk operations need unit counts.** A record count is not enough when one
   record can contain 55 units. H-02/M-01 must show both.
7. **The generic edit form remains a risk.** If it continues to allow quantity
   and recipient changes together, officers can recreate the ambiguity even after
   H-05 exists. The owner should either restrict it or label it as a correction
   path.
8. **Consolidation interacts with restoration.** An absorbed row cannot be
   restored. Any issue/return flow must respect that existing gate.
9. **No automatic migration is recommended.** The review does not propose a data
   mutation. Legacy cleanup should be owner-run and dry-run first, consistent with
   the existing consolidation approach.

## 14. Closing statement

H-05 is not only a missing button. It exposes a missing business definition.
`GivenTo` currently behaves as a free-text recipient while the system continues to
treat the row as active stock. That is why an officer can “give” an item to Marc
and still see all 55 publicly available.

The recommended model is deliberately narrow:

- a lot is the current stock record;
- a movement is the history;
- `GivenTo` is only the recipient of a completed whole-record issue;
- partial issues reduce the lot and write a movement;
- non-stackable items are whole-record issues;
- public inventory shows active on-hand quantity only;
- H-02 and H-05 share one definition of on hand.

If the owner accepts these rules, H-05 can be designed as a small set of named
actions over a consistent stock model rather than as a smarter edit form. If the
owner rejects the whole-record meaning of `GivenTo`, the alternative should be an
explicit reservation/loan model, not a broader free-text field.

No code, template, stylesheet, route, migration or runtime behavior was changed by
this review.
