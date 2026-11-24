# Partial issuance — owner approval package

**Status:** owner decisions recorded 2026-11-22; Phase 1 schema implemented 2026-11-22. No service, API, template, route, audit or movement workflow code changed. Phases 2–7 remain gated on owner review.
**Revision (2026-11-23):** OD-01 was revised (Option A). Partial issue is governed by the on-hand lot quantity, not `EQUID` stackability: any active lot with `Quantity > 1` supports partial issue and stackability metadata is advisory display only. Wherever this package still gates partial issue on stackability or an allow-list, the quantity-based rule governs.
**Date:** 2026-11-22
**Reviewed document:** `Docs/InventoryMovementBusinessRulesReview.md`
**Owner direction recorded:** partial issuance is required. Issuing 10 units from a 55-unit stack must leave 45 on hand, keep one active row, and record an `ISSUE` movement with recipient, timestamp and actor while preserving audit history.

This package answers the eight review questions, updates the `D-ISS` decision recommendations, and lists the implementation sequence, migration impacts and audit impacts. It records decisions only; Phase 1 schema implementation is tracked separately in `ProjectStatus.md`.

## 1. Decision summary

The owner decision settles the central ambiguity in the review:

- A **stack** is one active `GuildBankItems` row with `Quantity > 1`.
- A **partial issue** reduces that row's quantity and writes one `ISSUE` movement. It does **not** create a second active row and does **not** set `GivenTo` on the active row.
- A **full-stack issue** sets `GivenTo` to the recipient, writes one `ISSUE` movement for the full on-hand quantity, and soft-removes the row. It does not reduce `Quantity` to zero, because the table forbids zero.
- `GivenTo` is a recipient snapshot for a completed whole-record issue or a completed disposition. It is never the representation of a partial issue.
- The movement ledger is the operational history. The existing `Audit` table remains the security/accountability log. Both are append-only.

For the owner's example:

| Field | Result |
|---|---|
| Starting lot | Batwing Crunchies, `Quantity = 55`, active |
| Issue | 10 units to Marc |
| Active row after | Same row, `Quantity = 45`, `GivenTo = NULL`, still active |
| New active row | None |
| Movement | `ISSUE`, quantity `10`, recipient `Marc`, timestamp, actor, source lot ID |
| Audit | New `ITEM_ISSUED` event (or equivalent movement event) with the same correlation ID |
| On hand | `45` |
| History | Original 55-unit opening state remains explainable through the lot plus the movement |

## 2. Review of `D-ISS-01`–`D-ISS-15`

### 2.1 Decisions required to support partial issuance

The core decisions are **D-ISS-01, D-ISS-02, D-ISS-03, D-ISS-04, D-ISS-10, D-ISS-11, D-ISS-12, D-ISS-13, D-ISS-14 and D-ISS-15**.

The supporting decisions for the complete workflow are **D-ISS-05, D-ISS-06, D-ISS-07, D-ISS-08 and D-ISS-09**.

### 2.2 Decisions that become unnecessary

**No `D-ISS` record is wholly unnecessary.** The owner decision removes the need for an owner choice on the following alternatives, which should be withdrawn:

- **Split-on-partial** in `D-ISS-04`: rejected. Partial issue reduces the existing lot.
- **`GivenTo` as partial-issue, reservation or note field** in `D-ISS-03`: rejected. `GivenTo` is only for completed whole-record issue/disposition.
- **Automatic conversion of legacy active `GivenTo` rows** in `D-ISS-15`: rejected. It remains an owner-reviewed data-quality list.

`D-ISS-03` and `D-ISS-04` remain required as *rules*, but they are no longer open questions. They are settled by the owner direction.

### 2.3 Disposition table

| ID | Disposition | Recommended rule after the owner decision |
|---|---|---|
| D-ISS-01 | **Required** | Keep `GuildBankItems` as the stock-lot table. A row is one lot with one quantity, one location and its own dates. Multiple lots per `EQUID` remain legal. |
| D-ISS-02 | **Required** | Add an immutable movement ledger for issue, receive, move, adjust, write-off and return. The lot is current state; the ledger explains changes. |
| D-ISS-03 | **Required; owner direction recorded** | `GivenTo` means the recipient of a completed whole-record issue or disposition. It is not a reservation, note, partial-issue field or location. An active row must not carry a recipient. |
| D-ISS-04 | **Required; owner direction recorded** | A partial issue reduces the same lot and writes one `ISSUE` movement. No new active row. A full issue sets `GivenTo`, writes `ISSUE`, and soft-removes the lot without reducing quantity to zero. |
| D-ISS-05 | **Required supporting** | A partial move reduces the source lot and creates a destination lot; a whole move updates the lot's location. Destination inherits source dates. |
| D-ISS-06 | **Required supporting; approved (OD-07)** | Receives increase an explicitly chosen lot or create a new lot. Movement-managed lots are exempt from spell-style auto-merge; auto-merge must not absorb or alter a movement-managed lot. |
| D-ISS-07 | **Required supporting** | Removal is a whole-lot action. A partial removal is a write-off or adjustment. Bulk removal must show record count and unit count. |
| D-ISS-08 | **Required supporting; approved (OD-02)** | Restoring a fully returned whole-issued lot clears `GivenTo` and writes a `RETURN` movement. Administrative restore of a non-issued removed row writes `RECEIVE`. A partial return after a full issue creates a new lot instead of restoring the old row. |
| D-ISS-09 | **Required supporting; approved (OD-03)** | Return copies the original lot's dates by default. A fresh-date override is deferred to a later enhancement. |
| D-ISS-10 | **Required** | Quantity editing is a correction path only. It requires a reason, writes an `ADJUST` movement and audit, and must not set `GivenTo`. |
| D-ISS-11 | **Required** | Public inventory shows active on-hand quantity only. It never exposes `GivenTo`, exact source location or removed lots. Availability remains date-based. |
| D-ISS-12 | **Required** | H-02 totals and H-05 actions use one definition: on hand = sum of active lot quantities. Issued/removed lots are history, not stock. |
| D-ISS-13 | **Required; revised (OD-01, 2026-11-23)** | Partial-issue eligibility is governed by the active lot quantity on hand, not `EQUID` stackability. Any active lot with `Quantity > 1` may be issued partially; a lot with `Quantity = 1` can only be issued whole. `EQUID` stackability metadata is advisory display only and no longer gates a write. |
| D-ISS-14 | **Required** | Audit must gain distinct movement event names or an explicit movement type in structured fields. A generic `GivenTo`-only entry is insufficient. |
| D-ISS-15 | **Required; automatic conversion withdrawn** | Legacy active rows with `GivenTo` are an owner-reviewed data-quality list. They are not automatically converted to issued or cleared. |

## 3. Recommended business rules

| ID | Rule |
|---|---|
| R-ISS-01 | **On hand** = sum of `Quantity` for active lots (`DeletedFg = 0`). Movement rows are never added to stock. |
| R-ISS-02 | An **active** lot must have `GivenTo = NULL`. `GivenTo` is populated only when a whole-record issue or disposition completes. |
| R-ISS-03 | **Partial issue** reduces the selected active lot's `Quantity` by the issued quantity. The lot remains active with the same ID, location, dates and provenance. No new active row is created. |
| R-ISS-04 | Every issue writes one immutable `ISSUE` movement with: movement ID, lot ID, quantity, recipient, timestamp, actor, source location, remaining quantity and correlation ID. |
| R-ISS-05 | **Full-stack issue** sets `GivenTo` to the recipient, writes one `ISSUE` movement for the full current on-hand quantity, and sets `DeletedFg = 1`. The row's `Quantity` is left unchanged; zero is not written. |
| R-ISS-06 | A partial issue may not reduce a lot to zero. If the requested quantity equals the full on-hand quantity, the action must use the full-stack issue path. |
| R-ISS-07 | Partial issue is **quantity-based**: any active lot with `Quantity > 1` may be issued partially. A lot with `Quantity = 1` can only be issued whole. |
| R-ISS-08 | `EQUID` stackability metadata is **advisory (display only)** and never gates a write. Partial-issue eligibility is decided solely by the active lot quantity currently on hand. |
| R-ISS-09 | Receives either increase an explicitly chosen active lot or create a new lot. No automatic merge during daily work. Any merge that remains must write a `MERGE` or `ADJUST` movement and preserve the absorbed lot IDs. |
| R-ISS-10 | A partial move reduces the source lot and creates a destination lot. A whole move updates the lot's location. The destination inherits the source lifecycle dates. |
| R-ISS-11 | Removal is whole-lot. A partial removal is a write-off or adjustment. Bulk actions must show both affected record count and affected unit count. |
| R-ISS-12 | Returns are receipts. A partial return to an active lot increases that lot's quantity and writes `RETURN`. A full reversal of a whole issue restores the original row and clears `GivenTo`. A partial return after a full issue creates a new lot for the returned quantity. |
| R-ISS-13 | Quantity edits are corrections only. They require a reason, write an `ADJUST` movement and audit, and never set `GivenTo`. |
| R-ISS-14 | Public output shows active on-hand quantity only. It never shows `GivenTo`, exact source location, actor identity, movement history or removed rows. |
| R-ISS-15 | H-02 and H-05 use the same on-hand definition. H-02 must not aggregate movement rows or count removed/issued lots. |
| R-ISS-16 | Issue, return and adjustment are atomic: lock the lot, validate expected state, update the lot, write the movement and write the audit in one transaction. A stale expected state returns `409`; no partial write occurs. |

## 4. How stack (partial) issuance works

**Trigger:** the officer opens an active lot with `Quantity > 1` and chooses **Issue**. The dialog shows the item name, lot ID, current on hand, requested quantity, resulting on hand and recipient.

**Required input:** quantity (`1..onHand-1` for the partial path), recipient (required, trimmed), optional note/reason.

**Validation before write:**

1. The lot is active (`DeletedFg = 0`) and `GivenTo IS NULL`.
2. The lot is active. Partial issue is allowed whenever the on-hand quantity is greater than `1`; `EQUID` stackability metadata is not consulted.
3. The requested quantity is greater than zero and **less than** the current on hand. Equal quantity routes to the full-stack path (§5).
4. The expected quantity/version supplied by the dialog still matches the database.

**Transaction:**

1. `SELECT … FOR UPDATE` the lot.
2. Re-check the expected quantity and active state.
3. `UPDATE` the same row: `Quantity = Quantity - requested`.
4. Insert one `ISSUE` movement.
5. Insert one audit event with the same correlation ID.
6. Commit.

**Result:** same lot ID, same location, same lifecycle dates, same provenance, `Quantity = 45`, `GivenTo = NULL`, still active. No second active row. No split. No consolidation.

**Concurrency:** two officers issuing 10 from 55 — the second transaction sees the expected quantity changed, returns `409`, and the UI refreshes to 45. It must not silently issue from the new balance.

**Edge cases:**

- Requested quantity greater than on hand → `422` with the current on hand.
- Requested quantity equal to on hand → full-stack path, not partial.
- Lot with quantity `1` → whole-lot path, not partial.
- Lot with `Quantity > 1` and non-stackable or unclassified `EQUID` → partial issue proceeds; stackability metadata does not gate the write.
- Legacy active lot with `GivenTo` → blocked until the owner clears or resolves the row (§11).

## 5. How full-stack issuance works

**Trigger:** requested quantity equals the full on hand, or the officer explicitly chooses **Issue all**.

**Transaction:**

1. Lock the lot and re-validate active state and expected quantity.
2. Set `GivenTo = recipient`.
3. Write one `ISSUE` movement for the full current on-hand quantity, with the recipient, actor, timestamp, source location and remaining quantity `0`.
4. Set `DeletedFg = 1` (soft remove) using the existing removal fields.
5. **Leave `Quantity` unchanged.** The table forbids zero; the row is history, not stock.
6. Write the audit event in the same transaction.

**Result:** the row disappears from active totals and public inventory, remains queryable in history with its recipient, and can be restored (§6). No zero-quantity active row exists.

**Why not "partial issue all":** reducing to zero would violate the quantity constraint and would leave an active row with no recipient, losing the whole-record recipient snapshot. The full-stack path is the only path that records a completed whole-record issue.

## 6. How returns work

Returns are **receipts**, not edits. Three cases:

| Case | Behavior |
|---|---|
| Return to an active lot (for example, stock issued partially and returned) | Increase the same lot's `Quantity` by the returned quantity and write a `RETURN` movement. `GivenTo` stays `NULL`. |
| Full reversal of a whole-stack issue | Restore the original row (`DeletedFg = 0`), clear `GivenTo`, leave `Quantity` unchanged, and write a `RETURN` movement referencing the original `ISSUE` movement. The original recipient remains in the movement and audit history. |
| Partial return after a whole-stack issue | **Recommended:** create a new lot for the returned quantity and write a `RETURN` movement referencing the original issue. The original row stays issued/removed as history. Do not reopen the row with a partial quantity and a stale recipient. |

Additional rules:

- A return without a matching prior issue is a **receive**, not a return; it must not carry a recipient.
- A consolidated/absorbed row cannot be restored; the existing consolidation gate remains.
- Returned stock keeps its original dates on restore. A fresh availability window requires receiving as a new lot.
- Returns must be idempotent per request key to prevent double-click double receipts.
- Owner decision required: accept the "new lot on partial return" recommendation, or define a different loan/return model.

## 7. How movement history appears in the UI

**Location:** admin item summary → expand to lots → **History** action on the lot (and an item-level history that merges lots).

**List columns:** timestamp, type (`ISSUE`, `RETURN`, `RECEIVE`, `MOVE`, `ADJUST`, `WRITE_OFF`), quantity, from → to location, recipient, actor, remaining on hand, note, correlation ID.

**Behavior:**

- Newest first, paginated, filterable by type, date range and recipient.
- Read-only. No edit or delete. Corrections are new movements.
- A movement detail drawer shows the full record and a link to the matching audit entry.
- Lot rows show current on hand and a compact "last movement" hint; the expanded history explains how the balance was reached.
- Whole-issued lots remain visible in history with their recipient and final quantity, clearly marked as issued/removed.
- **Pre-deployment rows have no movement history.** Show a labelled **opening balance** line derived from the lot's creation state. Do not fabricate a movement row and do not backfill guessed history.
- Public inventory never shows movement history, recipient, actor or exact location.

## 8. How inventory totals are calculated

| Measure | Definition |
|---|---|
| On hand | `SUM(Quantity)` for active lots (`DeletedFg = 0`). This is the only stock total. |
| Stack count | `COUNT(*)` of active lots for the item. |
| Availability mix | Counts of active lots by `upcoming`, `open`, `bazaar` using lot dates. |
| Location mix | Distinct location groups across active lots. |
| Issued (reporting) | `SUM(quantity)` of `ISSUE` movements. History only; never added to stock. |
| Returned (reporting) | `SUM(quantity)` of `RETURN` movements. History only. |
| Removed / written off | `SUM(quantity)` of `WRITE_OFF` movements or removed-lot quantities. History only. |

Rules:

- H-02, H-05, the admin list and the public list must use the same on-hand definition.
- A partial issue reduces on hand by 10; the movement row records 10 but is not subtracted again.
- A whole-stack issue removes the lot from on hand; its quantity remains on the historical row and in the `ISSUE` movement.
- Public totals show **units**, not record counts, and say so ("45 units in 1 stack"). If the existing record-level list is retained, it must label its number as records and add a separate unit total.
- Bulk actions show both affected record count and affected unit count.
- No report may add movement quantities to active quantities; that would double count.

## 9. Phase 5 conflicts

Phase 5 shipped administrative CRUD and audit; H-02/H-05 are still design items. The conflicts are therefore with the generic admin flows, not with shipped H-02/H-05 screens.

| # | Phase 5 behavior | Conflict | Resolution |
|---|---|---|---|
| 1 | Generic edit allows direct quantity changes with no reason or movement | Conflicts with D-ISS-10 / R-ISS-13 | Restrict quantity edits to a correction path that requires a reason and writes an `ADJUST` movement, or remove quantity editing from the generic form and route it to the named action. |
| 2 | Create/edit accept `GivenTo` on active rows | Conflicts with the active-row invariant (R-ISS-02) | Reject `GivenTo` on create and on active-row edits. Keep the field only for historical/imported completed issues; provide the owner cleanup report first. |
| 3 | Restore clears `DeletedFg` but leaves `GivenTo` | Conflicts with D-ISS-08 | Restore must clear `GivenTo` and write a `RETURN` movement. |
| 4 | Remove is a whole-row soft delete with audit only | Partially conflicts with D-ISS-02 | Keep whole-row removal, but write a `WRITE_OFF`/`REMOVE` movement so the ledger is complete. Audit alone is not queryable history. |
| 5 | Audit vocabulary is limited to `ITEM_CREATED` / `ITEM_UPDATED` / `ITEM_REMOVED` | Conflicts with D-ISS-14 | Add distinct movement events or a structured `movement_type` field. Do not rely on free-text notes. |
| 6 | Admin list totals count records | Conflicts with D-ISS-12 | Add unit totals and label record counts explicitly. |
| 7 | Spell merge auto-merges same-`EQUID` rows | Conflicts with D-ISS-06's no-auto-merge rule | Exempt movement-managed lots explicitly, or make the spell write a `MERGE` movement and preserve absorbed lot IDs. |
| 8 | Public projection already omits `GivenTo`, location and removed rows | No conflict | Keep as is; do not widen the projection. |
| 9 | Quantity model (QTY-07/QTY-08) allows item quantity greater than 1 and dropped `ck_gbi_item_one` | No conflict; it enables stacks | Reuse it. Partial issue reduces quantity; full issue leaves quantity and soft-removes. |
| 10 | Bulk add with quantity N creates one row | No conflict | It is a stack creation path; the new row is the opening lot. |
| 11 | Restore of a consolidated row is blocked | No conflict | Returns must respect the same gate. |

**Highest-risk conflict:** #2. If active rows may still be created or edited with `GivenTo`, officers can recreate the exact ambiguity this decision removes. The invariant must be enforced in the service layer, not only in the UI.

## 10. Implementation sequencing

No code is authorized by this package. Owner decisions OD-01 to OD-07 are recorded in the plan (§12) and this package (§13). Recommended order:

1. **Approval gate** — accept the updated plan and readiness conditions. No further first-release decision is outstanding.
2. **Schema** — add the movement ledger table and indexes; no lot data mutation. Stackability is advisory `EQUID`-level metadata and needs no gate configuration (OD-01 revised 2026-11-23).
3. **Domain/service** — movement writer, on-hand calculator, advisory stackability read and atomic issue/return/restore/write-off operations with row locking and expected-state checks. Move and adjust are deferred (OD-04).
4. **API** — issue and return endpoints; `Idempotency-Key` header; update edit/restore contracts; define `409`/`422` semantics.
5. **Admin UI** — issue and return forms with hidden idempotency fields, movement history, unit totals, `GivenTo` removal.
6. **Audit** — movement event names or structured type; correlation IDs; reporting compatibility for legacy events.
7. **Public read path** — unit total definition only; no recipient or history exposure.
8. **Legacy cleanup** — owner-reviewed `GivenTo` report and dry-run; enforce the active-row invariant for new writes from the start, with a clear path to clear legacy rows.
9. **Validation** — unit and integration tests, concurrency tests, migration/rollback proof, manual acceptance.

Dependencies: 2 → 3 → 4 → 5; 6 can proceed with 3; 7 depends on the total definition; 8 must not block 3–5 but must complete before the invariant is enforced on legacy rows. Import-generated `RECEIVE` movements are deferred (OD-05); imports remain inventory state only.

## 11. Migration impacts

- **New table only.** No column removal, no quantity rewrite, no recipient rewrite. The migration is additive and reversible by dropping the new table. Implemented in `0006_inventory_movement.sql`; no existing lot data was mutated.
- Suggested ledger columns: movement ID, lot ID, `EQUID`, movement type, quantity, recipient, actor, occurred-at, from/to location, remaining quantity, note, correlation ID, created-at. Add indexes on `(EQUID, occurred_at)`, `(lot_id)`, `(recipient)` and `(type)`.
- Constraints: quantity greater than zero; recipient required for `ISSUE`; type restricted to the approved set; no update/delete path in the application.
- **No backfill.** Existing lots become opening balances. Their pre-deployment history is not invented. The UI labels the baseline.
- **No automatic `GivenTo` cleanup.** Produce a read-only report of active rows with recipients; owner resolves each row. New writes are blocked from setting `GivenTo` on active rows.
- **Stackability:** `EQUID`-level stackability is advisory metadata (OD-01, revised 2026-11-23). It is available for display only and never blocks a partial issue. Partial-issue eligibility is decided by the on-hand lot quantity.
- **Rollback:** dropping the ledger returns the application to Phase 5 behavior; lot quantities changed by partial issues are not automatically reversed. A rollback plan must state that partial issues already applied remain applied, or provide a compensating report.

## 12. Audit impacts

- Every movement writes an audit event in the same transaction as the lot update. A movement without an audit event, or an audit event without a movement, is a defect.
- Preferred taxonomy: `ITEM_ISSUED`, `ITEM_RETURNED`, `ITEM_RECEIVED`, `ITEM_MOVED`, `ITEM_ADJUSTED`, `ITEM_WRITTEN_OFF`, or one `ITEM_MOVEMENT` event with a structured `movement_type`.
- Structured fields should include movement ID, lot ID, `EQUID`, type, quantity, recipient, remaining quantity, from/to location and correlation ID. Do not put the movement type only in free text.
- Audit remains append-only. Corrections are new events. No historical audit row is rewritten.
- Legacy `ITEM_UPDATED`/`ITEM_REMOVED` rows remain valid; reports must handle both old and new vocabularies.
- Recipient names are internal data. They appear in admin movement history and audit only, never in public output. Existing access controls and retention rules apply.

## 13. Owner decisions (recorded)

The owner reviewed the decision matrix on 2026-11-22. These decisions govern implementation.

| ID | Decision | Owner disposition |
|---|---|---|
| **OD-01** | Partial-issue eligibility | **Revised 2026-11-23 (Option A)** — partial issue is governed by the active lot quantity on hand, not `EQUID` stackability. Any active lot with `Quantity > 1` supports partial issue; stackability is advisory display only. |
| **OD-02** | Restore semantics | **Approved** — administrative restore writes `RECEIVE`; return from a recipient writes `RETURN`. |
| **OD-03** | Return date policy | **Approved** — copy the original lot's dates by default; fresh-date override deferred. |
| **OD-04** | First-release scope | **Approved** — `ISSUE` and `RETURN` only; `MOVE` and `ADJUST` deferred. |
| **OD-05** | Import receive movements | **Deferred** — no import-generated `RECEIVE` movements in the first release; imports remain inventory state. |
| **OD-06** | Idempotency transport | **Approved** — `Idempotency-Key` header for API, hidden form field for HTML, shared semantics. |
| **OD-07** | Spell auto-merge | **Approved** — movement-managed lots are exempt from spell-style auto-merge. |

The previously recommended business rules R-ISS-01 to R-ISS-16, the `GivenTo` rules, the public totals definition and the movement taxonomy remain as described in sections 2 to 9 and are not reopened by these decisions.

**Approval:** Owner decisions recorded 2026-11-22. Implementation may begin subject to the readiness conditions in section 14.

## 14. Implementation readiness summary

### 14.1 Remaining required work

1. **Schema** — movement ledger migration, indexes, constraints and migration manifest/hash updates. No lot data mutation.
2. **Domain and repository** — movement types, append-only ledger reads/writes, on-hand calculator, advisory stackability read and transaction helpers.
3. **Service** — atomic issue, full-stack issue, return, receive/restore and write-off paths with row locking, expected-state checks and the active-row `GivenTo` invariant.
4. **API** — issue/return routes, movement history reads, contract changes, error semantics and `Idempotency-Key` header parsing.
5. **Admin UI** — issue/return forms, hidden idempotency fields, movement history page, unit totals and `GivenTo` removal from generic active-row forms.
6. **Audit** — distinct movement event types and structured fields, written in the same transaction as the lot update and movement.
7. **Public read path** — unit totals only; no recipient, location or movement exposure.
8. **Legacy `GivenTo` cleanup** — read-only report and owner dry-run; no automatic conversion.
9. **Validation** — unit, integration, concurrency, migration/rollback and manual acceptance coverage.

### 14.2 Deferred work

| Item | Reason | Revisit trigger |
|---|---|---|
| `MOVE` and `ADJUST` API/UI | OD-04 limits first release to issue and return. | After issue/return is stable; schema already supports both types. |
| Import-generated `RECEIVE` movements | OD-05 deferred; imports remain inventory state only. | When receipt history for imports is required. |
| Fresh-date return override | OD-03 deferred. | When returned stock genuinely has new dates. |
| Movement-aware spell merge (`MERGE`) | OD-07 exempts movement-managed lots. | When merge rules can preserve ledger integrity. |
| Stackability metadata coverage | OD-01 (revised) no longer gates partial issue on metadata; the advisory classification is display only. | Only if a richer EQUID metadata source is added for display. |

### 14.3 Estimated implementation phases

| Phase | Content | Exit criteria |
|---|---|---|
| 0 — Approval and baseline | Owner decisions recorded; plan approved. | Updated plan accepted. |
| 1 — Schema | Movement ledger migration, indexes, constraints, migration tests and docs. | Migration applies and rolls back cleanly; no lot data changed. |
| 2 — Domain and repository | Movement types, ledger reads/writes, on-hand calculator, advisory stackability read. | Repository tests pass; ledger is append-only. |
| 3 — Service | Issue, full-stack issue, return, restore and write-off with invariants and idempotency. | Service tests pass; stale state returns `409`; no partial writes. |
| 4 — API | Issue/return routes, history reads, contract changes, error semantics. | API tests pass; authentication and CSRF enforced. |
| 5 — Admin UI | Issue/return forms, history page, unit totals, `GivenTo` removal. | Manual acceptance for issue and return passes. |
| 6 — Audit and public totals | Movement events, structured fields, public unit totals. | Audit atomicity tests pass; public output exposes no internal data. |
| 7 — Legacy cleanup and release | `GivenTo` report, dry-run, documentation and release validation. | Owner resolves or accepts each legacy row; release checklist complete. |

Phases 1–3 are sequential. Phase 4 depends on 3. Phase 5 depends on 4. Phase 6 can start once the service contract is fixed. Phase 7 closes the release.

### 14.4 Migration impact

- **Additive only.** One new table (`InventoryMovement`) with indexes and constraints. No column removal, no quantity rewrite, no `GivenTo` rewrite.
- **No backfill.** Existing lots become opening balances. Pre-deployment history is not invented; the UI labels the baseline.
- **Indexes** on `(GuildBankItemID, OccurredAt)`, `(EQUID, OccurredAt)`, `(MovementType, OccurredAt)`, `(Recipient, OccurredAt)` and `(CorrelationID)`; unique `IdempotencyKey`.
- **Constraints** — quantity greater than zero; remaining quantity zero or greater; recipient required for `ISSUE`; movement type restricted to the approved set; no application update/delete path.
- **Rollback** — dropping the ledger returns the application to Phase 5 behavior. Lot quantities already changed by partial issues remain changed; the rollback note must report affected lots and movements.
- **Legacy `GivenTo`** — no automatic cleanup. A read-only report is produced; the owner resolves each row. New writes cannot set `GivenTo` on active rows.
- **Import history** — no movement rows are created for imports in the first release (OD-05).

### 14.5 Validation approach

- **Unit tests** — movement type validation, on-hand calculation, advisory stackability classification (display only), return date copying, restore branching (`RECEIVE` vs `RETURN`).
- **Repository tests** — append-only ledger, indexes and constraints, no update/delete path.
- **Service tests** — atomic issue, full-stack issue, partial return, whole return, restore and write-off; row locking; stale `expectedModAt` returns `409`; duplicate idempotency key returns `409`; no partial writes on failure.
- **Concurrency tests** — two concurrent issues from 55 units: one succeeds and one returns `409`.
- **API tests** — authentication and CSRF; request validation; `409`/`422` semantics; `Idempotency-Key` header and hidden form field share duplicate-key behavior; movement history pagination and filters; public JSON exposes no movement data.
- **Migration tests** — apply and rollback cleanly; no lot data mutation; opening-balance labeling.
- **Manual acceptance** — issue 10 from 55 leaves 45 in one active row; issuing the remainder uses the full-stack path; return restores correctly; restore writes the approved movement type; public totals show units only.
- **Deferred coverage** — move and adjust tests are out of scope for the first release (OD-04); import movement tests are out of scope (OD-05).

## 15. Implementation status

This package records owner decisions only. Phase 1 schema is now implemented through `0006_inventory_movement.sql`; no service, API, template, stylesheet, route, audit or movement workflow code was changed. `Docs/InventoryMovementBusinessRulesReview.md` is unchanged; this package records the owner direction and the resulting recommendations. Phases 2–7 remain gated on owner review.
