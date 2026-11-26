# Request System (Design Proposal)

**Status:** design proposal only — nothing in this document is implemented. No code, schema or template change accompanies it.
**Scope:** the primary member workflow: request one inventory item, track it, and have an officer approve (which issues the item) or deny it.
**Builds on:** inventory, the append-only movement ledger, the movement-derived issued projection, roles, character profiles, Discord linking/login, self-registration, account approval and the password capability.
**Non-goals:** shopping carts, multi-item requests, an "Approved but not yet issued" state, fulfilment scheduling, automatic expiry, Discord DM/e-mail notifications, requests for items that are not in inventory, and officer-initiated requests on behalf of another person.

---

## 0. Summary of the recommendation

| Question | Recommendation | Section |
|---|---|---|
| Where does a request live? | New MariaDB (guild store) table `InventoryRequest`; accounts stay in SQLite and are referenced by a stored user-ID snapshot, never a cross-database FK | §2.1 |
| One request = how many items? | Exactly one lot (`GuildBankItemID`) and one quantity. No line items, no cart | §2.2 |
| Status values | `Pending`, `Completed`, `Denied`, `Cancelled`. No `Approved` | §3, §4 |
| What does Approve do? | Issues the item and completes the request in **one** transaction, writing the `ISSUE` movement, the lot update, the request transition and the audit rows together | §9.2 |
| Who may approve? | Officer and Administrator (inventory work is Officer-level today); never the requester's own request | §8.6 |
| Character attribution for members | Dropdown of the account's profile characters; no free text. Server re-validates ownership | §5.2 |
| Character attribution for members with no characters | Free-text name, same 3–15 letter rule, with a link to `/account/profile` | §5.3 |
| Character attribution for public requests | Free-text name, same rule | §5.4 |
| Public requester identity | Character name is required; one optional contact field (Discord handle); no account is created | §6.2 |
| Can the public track a request? | Not in v1. The confirmation page returns a request number and states how officers will make contact | §6.5 |
| Does a return reopen a request? | No. A return is an inventory action; the request stays `Completed` | §9.5 |
| New settings | `PublicRequestsEnabled` (boolean, default `false`) | §11.3 |
| New audit events | `REQUEST_CREATED`, `REQUEST_COMPLETED`, `REQUEST_DENIED`, `REQUEST_CANCELLED` | §10 |
| Migrations | One additive MariaDB migration (`0008_inventory_request.sql`); no SQLite migration | §11 |
| Transport | Server-rendered HTML forms plus a small JSON API, both on the same service (existing D-03 single-write-path rule) | §12 |
| Implementation phases | Five phases, each independently releasable and testable | §16 |
| Owner decisions | Twelve decisions, each with a recommendation | §17 |

---

## 1. Current state this design builds on

| Concern | Where it lives today | Consequence for this design |
|---|---|---|
| Inventory lots | `GuildBankItems` (0001; quantity model 0005). Lots are **soft-deleted only** (`DeletedFg`); consolidation and removal update rows, nothing hard-deletes | A request can hold a `FOREIGN KEY ... ON DELETE RESTRICT` to the lot and never dangle |
| Ledger | `InventoryMovement` (0006/0007), append-only. `ISSUE` requires a `Recipient`; `RemainingQuantity` is the lot's on-hand quantity after the movement | Approval reuses the ledger verbatim; recipient = the character name |
| Issue write path | `AdminInventoryService.Issue` → `issueAttempt` (`internal/service/admin_movement.go:160-284`): lock lot, check `ExpectedModAt`, update or soft-delete, insert movement, insert audit, commit | This is the **only** issue path today; approval must share it rather than clone it (§9.1) |
| Issued rows | Movement-derived projection of outstanding `ISSUE` movements; `RelatedMovementID` attributes returns | An approved request appears there automatically. Nothing to add |
| Idempotency | `InventoryMovement.IdempotencyKey` (`VARCHAR(64)`, unique). `movementIdempotencyHeader` transport | Approval can derive a deterministic key from the request ID |
| Concurrency | `AdminWriteTx.LockInventory` + `ExpectedModAt` optimistic check; `withDeadlockRetry` | Approval uses the same lock order and the same stale-form protection |
| Accounts | SQLite `Users` (0001/0002/0004): `UserID`, `UserName`, `AccessLevel`, `AccountStatus`, `DisabledFg` | No cross-database FK is possible; the request stores a user-ID **and** name snapshot |
| Roles | `domain.Role` Member/Officer/Admin; `domain.Actor` (`internal/domain/import.go:22`) with `CanManageInventory()` | Officer-level gate for approve/deny, Member for create/cancel |
| Characters | SQLite `Characters` (0003), `domain.Character` (3–15 ASCII letters, max 10 per account), `CharacterService` | The request stores a character **name snapshot**, never a character FK |
| Audit | `Audit` table (0001) + `AuditEntry` (`internal/domain/import.go:235`); viewer scoped by `OfficerAuditEventTypes` / `AdminAuditEventTypes` | Add four request event types to the Officer allowlist |
| Public surface | `inventoryHandler` public group with CSRF middleware; `/`, `/inventory/:id`, `/api/v1/items` | Public request forms reuse the same CSRF token and rate-limit pattern as registration |
| Settings | `SettingDefinitions` allowlist plus `settingViews` (definitions drive rendering even when no row exists) | A new boolean needs only a definition and a validation case |
| Route authorization | `authorization_matrix_test.go` fails any unclassified route; `navigation_role_test.go` guards nav | Every new route and nav entry must be classified in the same commit |

---

## 2. Data model

### 2.1 Placement: MariaDB guild store

The request belongs to the **guild store**, not to SQLite:

- Approval must update `GuildBankItems`, append to `InventoryMovement`, transition the request and write audit rows **atomically**. A second database cannot join that transaction.
- The officer queue filters by lot state and shows current on-hand; both live in MariaDB.
- Accounts and characters live in SQLite and are already treated as an external identity source. The request stores `RequesterUserID` / `RequesterUserName` and `CharacterName` as **immutable snapshots**, exactly as the ledger stores `ActorName` and `Recipient` rather than pointing at the account tables.
- A request must survive account deletion. A snapshot does; an FK into another database could not.

### 2.2 Proposed table (migration 0008)

```sql
CREATE TABLE IF NOT EXISTS InventoryRequest (
    RequestID           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    GuildBankItemID     BIGINT UNSIGNED NOT NULL,
    EQUID               INT UNSIGNED NOT NULL,
    ItemName            VARCHAR(128) NOT NULL,
    RequestedQuantity   INT UNSIGNED NOT NULL,
    OnHandAtCreate      INT UNSIGNED NOT NULL,
    CharacterName       VARCHAR(64) NOT NULL,
    RequesterKind       ENUM('Member','Public') NOT NULL,
    RequesterUserID     BIGINT NULL,
    RequesterUserName   VARCHAR(64) NULL,
    ContactDiscord      VARCHAR(64) NULL,
    Note                VARCHAR(255) NULL,
    Status              ENUM('Pending','Completed','Denied','Cancelled') NOT NULL DEFAULT 'Pending',
    DecisionNote        VARCHAR(255) NULL,
    IssuedQuantity      INT UNSIGNED NULL,
    CompletedMovementID BIGINT UNSIGNED NULL,
    CorrelationID       CHAR(36) NOT NULL,
    IdempotencyKey      VARCHAR(64) NULL,
    CreatedAt           DATETIME(6) NOT NULL,
    CreatedByUserID     BIGINT NULL,
    CreatedByName       VARCHAR(64) NOT NULL,
    DecidedAt           DATETIME(6) NULL,
    DecidedByUserID     BIGINT NULL,
    DecidedByName       VARCHAR(64) NULL,
    PRIMARY KEY (RequestID),
    -- MariaDB has no partial indexes. A STORED generated column collapses to NULL
    -- for every non-pending row, and a UNIQUE index tolerates repeated NULLs, so
    -- this enforces "at most one Pending request per member per lot" in the engine.
    PendingUserKey      BIGINT UNSIGNED
                        AS (CASE WHEN Status = 'Pending' THEN RequesterUserID ELSE NULL END) STORED,
    UNIQUE KEY uq_request_movement (CompletedMovementID),
    UNIQUE KEY uq_request_pending (GuildBankItemID, PendingUserKey),
    UNIQUE KEY uq_request_idempotency (IdempotencyKey),
    KEY ix_request_status_time (Status, CreatedAt),
    KEY ix_request_requester_time (RequesterUserID, CreatedAt),
    KEY ix_request_kind_time (RequesterKind, Status, CreatedAt),
    KEY ix_request_lot_time (GuildBankItemID, CreatedAt),
    KEY ix_request_character_time (CharacterName, CreatedAt),
    CONSTRAINT fk_request_lot FOREIGN KEY (GuildBankItemID)
        REFERENCES GuildBankItems (GuildBankItemID) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT fk_request_movement FOREIGN KEY (CompletedMovementID)
        REFERENCES InventoryMovement (MovementID) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT ck_request_quantity CHECK (RequestedQuantity > 0),
    CONSTRAINT ck_request_issued CHECK (IssuedQuantity IS NULL OR IssuedQuantity > 0),
    CONSTRAINT ck_request_completed CHECK (
        Status <> 'Completed' OR (CompletedMovementID IS NOT NULL AND IssuedQuantity IS NOT NULL)),
    CONSTRAINT ck_request_decided CHECK (
        Status = 'Pending' OR (DecidedAt IS NOT NULL AND DecidedByName IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

### 2.3 Column rationale

| Column | Why it exists |
|---|---|
| `GuildBankItemID` | The single inventory item the request is for. Presentational identity comes from the lot; the FK makes an orphan impossible |
| `EQUID`, `ItemName`, `OnHandAtCreate` | Immutable snapshots so the request still reads correctly after a rename, a consolidation, or a lot that has since been issued. `OnHandAtCreate` lets an officer judge staleness at a glance |
| `RequestedQuantity` | What the member asked for; never rewritten |
| `IssuedQuantity` | What was actually issued. Equal to `RequestedQuantity` under the recommended strict rule (§9.3); kept separate so a future partial-approval decision needs no migration |
| `CharacterName` | Attribution. A name, not a FK: characters live in SQLite and can be deleted |
| `RequesterKind` | `Member` vs `Public`. Derivable from `RequesterUserID` in principle, but stored so the queue can index and filter it and so a deleted account never changes the historical classification |
| `RequesterUserID`, `RequesterUserName` | Account snapshot. No FK (different database). `NULL` for public |
| `ContactDiscord` | Public only; how an officer reaches a visitor. Never rendered on a public page |
| `Note` | The requester's free text, ≤255 runes — the same bound as a movement note, so approval can carry it into the ledger without truncation |
| `Status` | The four-state lifecycle (§4) |
| `PendingUserKey` | Generated `Status='Pending' ? RequesterUserID : NULL`. Exists only so a unique index can express "one Pending request per member per lot" without partial indexes. Never read by application code |
| `DecisionNote` | Denial reason, or the officer's completion remark. Visible to the requester (§17, OD-R8) |
| `CompletedMovementID` | The `ISSUE` movement produced by approval. Unique, so one movement can never complete two requests |
| `CorrelationID` | The creation request's correlation ID, reused by the approval's movement so the audit viewer's correlation filter shows the whole lifecycle in one list |
| `IdempotencyKey` | Hidden per-form key on creation; a duplicated POST returns the original request instead of queuing a second one |
| `CreatedBy*`, `DecidedBy*` | Actor retention even if the account is later deleted or renamed |

### 2.4 Proposed domain types

```go
// internal/domain/request.go (proposed)

// RequestStatus is the closed lifecycle set. Approval is not a state: it is the
// transition that completes the request and issues the item.
type RequestStatus string

const (
    RequestPending   RequestStatus = "Pending"
    RequestCompleted RequestStatus = "Completed"
    RequestDenied    RequestStatus = "Denied"
    RequestCancelled RequestStatus = "Cancelled"
)

func (s RequestStatus) Valid() bool
func (s RequestStatus) Terminal() bool // every value except Pending

// RequesterKind separates an authenticated member from an anonymous visitor.
type RequesterKind string

const (
    RequesterMember RequesterKind = "Member"
    RequesterPublic RequesterKind = "Public"
)

type InventoryRequest struct {
    RequestID           int64
    GuildBankItemID     int64
    EQUID               uint64
    ItemName            string
    RequestedQuantity   uint32
    OnHandAtCreate      uint32
    CharacterName       string
    RequesterKind       RequesterKind
    RequesterUserID     *int64
    RequesterUserName   string
    ContactDiscord      string
    Note                string
    Status              RequestStatus
    DecisionNote        string
    IssuedQuantity      *uint32
    CompletedMovementID *int64
    CorrelationID       string
    IdempotencyKey      string
    CreatedAt           time.Time
    CreatedByUserID     *int64
    CreatedByName       string
    DecidedAt           *time.Time
    DecidedByUserID     *int64
    DecidedByName       string
    // Read-only projection fields, filled by the queue query.
    CurrentOnHand uint32
    LotActive     bool
}

type CreateRequestInput struct {
    GuildBankItemID int64
    Quantity        uint32
    CharacterName   string
    Note            string
    ContactDiscord  string
    Kind            RequesterKind
    Actor           Actor // zero value for a public request
    IdempotencyKey  string
    CorrelationID   string
    SourceIP        string
}

type ApproveRequestInput struct {
    RequestID     int64
    ExpectedModAt time.Time // the lot version shown on the approve page
    Note          string
    Actor         Actor
    CorrelationID string
    SourceIP      string
}

type DenyRequestInput struct {
    RequestID     int64
    Reason        string // required
    Actor         Actor
    CorrelationID string
    SourceIP      string
}

type CancelRequestInput struct {
    RequestID     int64
    Actor         Actor // must equal RequesterUserID
    CorrelationID string
    SourceIP      string
}

type RequestFilter struct {
    Status          RequestStatus
    RequesterKind   RequesterKind
    RequesterUserID int64 // member "my requests" scope; 0 = not scoped
    GuildBankItemID int64
    CharacterName   string
    ItemName        string
    From, To        time.Time
    Page, PageSize  int
}
```

Validation lives in the domain, following `InventoryMovement.Validate` and `ValidateCharacterName`: quantities `> 0` and `<= OnHandAtCreate`; note and reasons ≤255 runes with no control characters; character name 3–15 ASCII letters; `ContactDiscord` trimmed, ≤64 runes, no control characters; `RequesterKind` must be `Public` when `Actor.UserID == 0` and `Member` otherwise.

### 2.5 Read projections

- **Member list/detail** (`RequestFilter.RequesterUserID = actor.UserID`): display needs no lot join, but `CurrentOnHand` is joined so a member can see the request is still fulfilable. If the lot is inactive the row says "No longer available".
- **Officer queue/detail**: joins `GuildBankItems` for `Quantity`, `DeletedFg` and `GivenTo`, so one page answers "can I approve this right now?".
- **Audit**: `Audit.TableName = 'InventoryRequest'`, `Audit.RecordID = <RequestID>`; the existing audit viewer already filters on both.

### 2.6 What is deliberately not modelled

- No `RequestLine` table, no cart, no "items per request" collection.
- No `Approved` status, no `IssuedAt` separate from the movement, no `ApprovedBy` separate from `DecidedBy`.
- No request-level attachment of a movement other than the one `ISSUE` that completed it. Partial issues from the admin screen are not requests.
- No soft-delete on requests: they are never deleted, only cancelled/denied (audit history is the point).

---

## 3. Request lifecycle

### 3.1 States and transitions

```
                        ┌───────────────────────────────┐
                        │            Pending            │
                        └───────────────────────────────┘
                              │        │          │
        officer approves ─────┘        │          └───── requester cancels
        (issues item)                  │                (own Pending only)
                              │        │          │
                              ▼        ▼          ▼
                        Completed   Denied    Cancelled
                        (terminal)  (terminal) (terminal)
```

| From | Action | Actor | To | Side effects |
|---|---|---|---|---|
| — | Create (member) | any authenticated Member+ | `Pending` | Row inserted; `REQUEST_CREATED` audit; item page shows "Your request is pending" |
| — | Create (public) | anonymous, CSRF-validated | `Pending` | Row inserted with `RequesterKind='Public'`; `REQUEST_CREATED` audit |
| `Pending` | Approve | Officer/Admin, not the requester | `Completed` | `ISSUE` movement + lot quantity reduction in the **same** transaction; `REQUEST_COMPLETED` audit; request/movement linked |
| `Pending` | Deny | Officer/Admin, not the requester | `Denied` | `DecisionNote` required; no inventory touch; `REQUEST_DENIED` audit |
| `Pending` | Cancel | the requester | `Cancelled` | Optional reason; no inventory touch; `REQUEST_CANCELLED` audit |
| any terminal | *nothing* | — | — | Terminal states are immutable. A member who wants the item again creates a new request |

### 3.2 Explicitly absent transitions

- **No `Pending → Approved`.** Approval that does not issue is not a state the guild wants; it would create an unbounded liability (approved items that nobody issued).
- **No `Cancelled → Pending`** (un-cancel) and **no `Denied → Pending`** (re-open). Re-requesting is a new row, which keeps the queue and the audit history honest.
- **No `Completed → …`** at all. A return is an inventory movement (§9.5), not a request transition.
- **No automatic expiry.** A `Pending` request never becomes `Cancelled` by a timer; an officer denies it (with a reason) or the member cancels it (§17, OD-R7).

### 3.3 Transition enforcement

Every transition is a **guarded UPDATE**, the same pattern the ledger uses:

```sql
UPDATE InventoryRequest
   SET Status = ?, DecidedAt = ?, DecidedByUserID = ?, DecidedByName = ?,
       DecisionNote = ?, IssuedQuantity = ?, CompletedMovementID = ?
 WHERE RequestID = ? AND Status = 'Pending';
```

`RowsAffected == 0` means someone else already decided the request → `409 request_not_pending`. This makes double-approval impossible even if two officers submit at the same instant, and it needs no optimistic version column.

---

## 4. Status model

### 4.1 Values

| Status | Meaning | Terminal | Inventory effect |
|---|---|---|---|
| `Pending` | Awaiting an officer decision | No | None |
| `Completed` | Approved and **issued** | Yes | Lot quantity reduced; `ISSUE` movement written |
| `Denied` | Refused by an officer, with a reason | Yes | None |
| `Cancelled` | Withdrawn by the requester | Yes | None |

### 4.2 Why there is no `Approved`

An `Approved` status would have to mean one of two things, and both are wrong here:

1. *"Approved, item not yet issued"* — a promise against stock that can be issued to somebody else in the meantime; the queue would need a second, invisible backlog and a resolution flow.
2. *"Approved and issued"* — identical to `Completed`, so it is a synonym, not a state.

The single fact that matters to an officer reading the queue is whether the stock still exists. Making approval atomic with issuance means the answer is always yes at the moment of the transition. If the item is gone, approval fails with `409 insufficient_stock` and the officer denies the request with a reason instead — the honest outcome.

Should a partial-fulfilment need appear later, `IssuedQuantity` already exists, so the extension is a new status value rather than a redesign (§17, OD-R4).

### 4.3 Derived display state

The UI shows a badge from `Status` plus the **availability** of the lot (a projection field, never stored as status):

| Status | Lot available | Badge |
|---|---|---|
| Pending | yes | `Pending` |
| Pending | no | `Pending · item unavailable` |
| Completed | — | `Completed` |
| Denied | — | `Denied` |
| Cancelled | — | `Cancelled` |

---

## 5. Character handling model

### 5.1 Principle

Character attribution is recorded as a **name snapshot** on the request. Whether that name came from a dropdown or a text box is a *presentation* decision, not a data decision, so the storage model is identical for members and public visitors and every historical request renders the same way.

### 5.2 Authenticated member with profile characters

- The request form contains a `<select>` populated from the account's character list, ordered as the profile page orders them, with the account's **main character preselected** if the profile has one (otherwise the first).
- No free-text input is rendered at all. The dropdown is the only way to supply a name.
- The server **re-validates ownership**: the posted `CharacterName` must appear in the actor's current character list (§14.4). A stale form whose character was deleted since the page loaded fails with `422 character_invalid` and the form re-renders with the current list.
- The submitted value is normalised exactly as the character profile stores it, so the ledger `Recipient` matches the profile character string.

### 5.3 Authenticated member with no profile characters

- The form renders a single text input, `Character Name [__________]`, with the profile rule applied (3–15 ASCII letters).
- Alongside it, one line of help: "Add characters to your profile so you can pick them from a list next time." linking to `/account/profile`.
- This is not a free-for-all: the entered name is validated with the *same* rule the profile uses, so the character vocabulary stays consistent across the product.
- Deliberately **not** auto-creating a profile character from the entered name. Creating identity records as a side effect of a request would make the profile page's contents depend on unrelated activity, and the owner has already chosen explicit character management (OD-R6 asks for confirmation).

### 5.4 Public visitor

- Same text input, same 3–15 letter validation, required.
- No character list exists to validate against, so the value is accepted as authoritative. The officer sees `Public` on the queue row and can deny a request with an implausible character name.

### 5.5 Server-side resolution order (single code path)

```
createRequest:
  if actor.UserID != 0:                 # authenticated
      kind = Member
      chars = characterService.List(actor.UserID)
      if len(chars) > 0:
          require posted name ∈ chars   # dropdown path
      else:
          validate as profile name      # text path, no list to check
  else:                                 # anonymous
      kind = Public
      validate as profile name          # text path
  store CharacterName as submitted (normalised)
```

Both branches converge on one validated string. There is one `CharacterName` column, one ledger `Recipient`, and no "is this a profile character?" flag — because a character deleted next month would falsify such a flag.

### 5.6 Rendering rule for the form

The template receives a small view model rather than branching on user state inline:

```go
type characterPickerView struct {
    Mode     string   // "select" | "text"
    Options  []string // populated in "select" mode
    Selected string   // populated in "select" mode
    Value    string   // populated in "text" mode
    HelpLink string   // "/account/profile" when Mode == "text" and authenticated
}
```

`Mode == "select"` iff the account has at least one profile character.

### 5.7 Interaction with the admin Issue form

The existing `/admin/inventory/:id/issue` form keeps its free-text `GivenTo`. Requests are a *separate* entry point into the same service method. An officer may still issue manually — that path produces a movement with no request, which is a legitimate outcome (e.g. a bank alt handout) and must not be forbidden.

---

## 6. Public-request workflow

### 6.1 Entry points

| Where | What appears | Gate |
|---|---|---|
| `/inventory/:id` (public item page) | "Request this item" button | `PublicRequestsEnabled == true` |
| `/` (public dashboard) | "Requesting from the bank" explainer paragraph | same |
| `/inventory` (public list) | No change — requests start from a specific item | — |

Anonymous visitors cannot reach the member flow, and authenticated members do not use the public flow (they get the richer member form, §7). The route is the same handler; the branch is `actor.UserID == 0`.

### 6.2 Required requester information

| Field | Required | Bound | Rationale |
|---|---|---|---|
| Item | implicit (route/param) | an active lot | One item per request |
| Quantity | yes | `1 ≤ qty ≤ current on-hand` | Never promises more than exists |
| Character Name | yes | 3–15 ASCII letters | Attribution; the officer needs to know whom to mail |
| Note | no | ≤255 runes | Why they want it, or a meeting time |
| Discord handle | yes (recommended) | ≤64 runes, `^[A-Za-z0-9._#-]{2,64}$` | **The only way an officer can reach a visitor.** Without it a public request cannot be reliably fulfilled |
| Real name / e-mail / phone | not collected | — | No outbound e-mail exists in this product; collecting one would be a promise the system cannot keep |

If the owner prefers not to require a Discord handle, the honest alternative is to disable public requests (OD-R2).

### 6.3 Submission steps

```
GET  /inventory/:id            → public item page, "Request this item" (if enabled)
GET  /request/new?item=<id>    → public request form (CSRF token, honeypot, time-trap)
POST /request/new              → validate → insert Pending → PRG to /request/:id/confirm
GET  /request/:id/confirm      → "Request #123 received"
```

The confirmation page shows **only** the request number, item name, quantity, character name and a sentence explaining that an officer will contact the supplied Discord handle. It shows no other request's data and no officer-only fields.

### 6.4 Anti-abuse controls

Public creation is the only write an unauthenticated visitor can reach besides registration, so it carries the same class of protection:

1. **CSRF** token on the form, exactly as registration already does.
2. **Rate limit** per source IP and globally via the existing `service/rate_limit.go` limiter: 5 creations/hour/IP and 60/hour overall. Both are constants, not settings — a knob nobody turns is a knob that rots.
3. **Honeypot** hidden field plus a minimum fill time (2 s) carried in a signed field; failing either yields the normal success page with **no** row inserted, so a bot cannot learn the rule.
4. **Server-side length and content validation**, never trusting HTML `maxlength`.
5. **On-hand snapshot check**: quantity must be ≤ on-hand at submit time, which also stops a request for 999,999 units.
6. **Per-requester pending budget**: at most 3 Pending public requests for the same character name (case-insensitive); excess gets the generic rate-limited page.
7. **Minimal PII** — a Discord handle and a note, both of which the officer needs. Nothing is written to application logs except identifiers; the audit row carries the standard nullable `SourceIP` like every other write in the app.

### 6.5 Tracking a public request (decision)

The recommendation is **no public status page** in v1:

- A guessable status URL is an enumeration leak (item, character and note of arbitrary strangers).
- A tokenised URL would need a token column, a resend flow and an e-mail — none of which exist.
- The guild's real contact channel is Discord, and the officer contacts the requester there.

The confirmation page therefore says: "Keep this number. An officer will contact you on Discord once your request has been reviewed." OD-R3 offers the tokenised alternative if the owner wants public self-service tracking.

### 6.6 Public request visibility

| Data | Member sees | Public sees | Officer sees |
|---|---|---|---|
| Own request | full, minus officer internals | confirmation summary only | full |
| Others' requests | never | never | full |
| Discord handle | — | own value echoed on the form | yes |
| Source IP | — | — | never rendered (audit only) |

---

## 7. Member-request workflow

### 7.1 Entry points

| Where | What appears |
|---|---|
| `/inventory/:id` | "Request this item" button (always on for an authenticated user) |
| `/my/requests` | The account's own requests, newest first |
| `/my/requests/:id` | Detail: item, quantity, character, note, status, decision note, and a link to the resulting movement when `Completed` |
| Item detail (authenticated) | If a Pending request by this user already exists for that lot, the button becomes "You have a pending request — view" |

### 7.2 Form contents

| Field | Control | Validation |
|---|---|---|
| Item | read-only summary (name, on-hand, location) | lot must still be active at submit |
| Quantity | number input, `min=1 max=<on-hand>` | re-checked against the locked lot |
| Character | dropdown or text (§5) | ownership or profile rule |
| Note | textarea, `maxlength=255`, optional | ≤255 runes, no control characters |
| Hidden | `item`, `expectedModAt`, `idempotencyKey` | `expectedModAt` drives the "stock changed since you looked" message |

`expectedModAt` is not cosmetic: it stops a member queuing a request for a quantity already issued to somebody else, mirroring how the admin forms protect themselves.

### 7.3 Steps

```
GET  /request/new?item=<id>    → member form (character picker from profile)
POST /request/new              → validate + idempotency → insert Pending → 303 /my/requests/:id?notice=created
GET  /my/requests              → own requests, filterable by status
GET  /my/requests/:id          → detail
POST /my/requests/:id/cancel   → guarded UPDATE → 303 detail?notice=cancelled
```

### 7.4 Post-creation behaviour

- The item page shows the pending indicator instead of the button, so the member cannot double-submit through the UI (the idempotency key covers the HTTP duplicate).
- A member may hold at most **5** Pending requests (`RequestMaxPendingPerUser`, a constant). No shopping cart, but also no queue abuse.
- A member may hold only **one** Pending request per lot (`409 request_already_pending`). Two pending requests for the same lot is either a mistake or a hoard attempt.

### 7.5 Notification

No e-mail, no DM. Status is visible on `/my/requests`, and the nav shows the member's own pending count (informational, not an alert). Discord DMs are out of scope for v1 (§17, OD-R10).

---

## 8. Officer workflow

### 8.1 Entry points and navigation

| Where | What appears |
|---|---|
| Admin nav | **Requests** item, visible to Officer and Administrator, with a Pending count badge |
| `/admin/requests` | Queue: default filter `Status=Pending`, oldest first |
| `/admin/requests/:id` | Decision page: full request, live lot state, Approve / Deny forms |
| `/admin/inventory/:id` | "Pending requests for this item" panel (bounded to 10, linked) |
| `/admin/audit` | Request events appear under the existing Officer allowlist |

The nav entry must be added to `navigation_role_test.go` expectations in the same commit, and every route classified in `authorization_matrix_test.go` as `classOfficer` except the member and public route families (§12.1).

### 8.2 Queue contents (one row per request)

Columns: `#`, Created (relative, with an exact-value tooltip), Requester (character name, plus a `Public` chip), Item, Qty, On-hand now, Age (colour banded at 2 and 7 days), Actions.

Sorting: `CreatedAt ASC` by default so the oldest request cannot starve. Filters: status, kind (Member/Public), character name, item name, date range, and an "Only available" toggle (lot active and on-hand ≥ requested). Paging follows the existing `AdminMaxPageSize` / `AdminDefaultPageSize` conventions.

### 8.3 Decision page

Shows everything needed to decide without a second click:

- The request (character, quantity, note, requester kind, created, age).
- The **live** lot: current quantity, location, given-to, last modification, and the movement that produced it.
- If on-hand is now below the requested quantity, an explicit red line: "Only 2 on hand — this request cannot be approved as-is."
- A link to the lot's movement history.
- **Approve** form: hidden `expectedModAt`, optional officer note (≤255), confirm button.
- **Deny** form: required reason (≤255), confirm button.
- History block: the `REQUEST_*` audit rows for this request with actor and timestamp.

### 8.4 Approve behaviour (the important one)

Approval issues. It is one POST, one transaction, one audit chain (§9). The officer sees the outcome immediately:

- Success → `303 /admin/requests/:id?notice=completed`; the page shows "Issued 3 × [item] to Icefear — movement #412" with links to the movement and the issued rows.
- `409 insufficient_stock` → re-rendered page: "The stock changed. Only N remained." The officer then denies with a reason.
- `409 request_not_pending` → re-rendered page: "Another officer already decided this request."
- `409 stale_item` → the lot was edited since the page loaded; re-render with the new lot state and ask for confirmation again.
- Replaying the same approve (double click, back button) does not error: the request is already `Completed` by that movement, so the officer lands on the completed page (§9.4).

### 8.5 Deny behaviour

- Reason is **required** (`DecisionNote`), because a denied member deserves to know why and the audit row should be self-explanatory.
- No inventory change; no movement is created; the request is terminal.
- `303 /admin/requests/:id?notice=denied`.

### 8.6 Self-decision prohibition

An officer may not approve or deny **their own** request:

- Enforced in the service (`Approve`/`Deny` compare `request.RequesterUserID` with `actor.UserID`) → `403 own_request`.
- Enforced in the UI (buttons hidden for own requests, with an explanatory line).
- This matters because an Officer can both request and issue: without the rule, the ledger would show a self-issued item whose approving signature proves nothing.

Administrators are subject to the same rule. If a lone Administrator needs the item, another Administrator must act (OD-R5 asks whether to allow an exception with an explicit acknowledgement checkbox; recommendation: no).

### 8.7 Historical view

`/admin/requests?status=Completed|Denied|Cancelled` gives the full history with the same filters. Because terminal rows are immutable and never deleted, this view is the authoritative operational record behind the audit log. CSV export is not in v1 (OD-R11).

### 8.8 Workload signals (optional, phase 4)

The queue header may show three small counters (Pending, oldest age, pending public) — informational only, no charts.

---

## 9. Inventory integration design

### 9.1 Single write path (no second issue implementation)

`AdminInventoryService.issueAttempt` (`internal/service/admin_movement.go:188-284`) already performs issuance correctly: lock the lot, reject a removed lot, reject an active row carrying a recipient, verify `ExpectedModAt`, bound the quantity to on-hand, choose partial vs full-stack, update or soft-delete, append the movement, append the audit row, commit. Cloning that sequence for requests would create two divergent issue paths — precisely the failure mode the ledger exists to prevent.

The recommendation is a **mechanical extraction**, not a rewrite:

```go
// internal/service/admin_movement.go (refactor, no behaviour change)

// issueLocked performs the issue sequence inside an already-open admin write
// transaction. Both the manual Issue flow and request approval call it, so the
// lock order, the stale-version check, the partial/full decision and the audit
// payload are written once.
func (s *AdminInventoryService) issueLocked(
    ctx context.Context,
    tx AdminWriteTx,
    in domain.IssueItemInput,
) (current domain.AdminInventoryItem, movement domain.InventoryMovement, err error)
```

- `issueAttempt` becomes: begin tx → `issueLocked` → commit → build `AdminWriteResult`. Behaviour identical, existing tests must pass unmodified.
- `approveAttempt` becomes: begin tx → `issueLocked` → `CompleteRequest` (§9.3) → commit.

`issueLocked` does **not** commit and does **not** own the transaction — that is what lets approval add the request transition to the same atomic unit.

### 9.2 What approval writes, in order

```
BEGIN (connection + transaction, via BeginAdminWrite)
  1. SELECT ... FOR UPDATE InventoryRequest WHERE RequestID = ?    -- LockRequest
       status must be Pending                                    -- 409 request_not_pending
       lock the request row FIRST, then the lot: stable, documented order
  2. LockInventory(current.GuildBankItemID)                        -- existing helper
       same checks as a manual issue (removed, recipient-set, ExpectedModAt)
       quantity bound: requested <= on-hand                        -- 409 insufficient_stock
  3. UpdateInventory / SetDeleted                                  -- existing
  4. InsertMovement(ISSUE, Recipient = CharacterName,
                    RequestID = ?, IdempotencyKey = "req-<RequestID>")
  5. CompleteRequest(RequestID, movementID, qty, actor, at, note)   -- guarded UPDATE
  6. InsertAudit(REQUEST_COMPLETED)                                -- TableName='InventoryRequest'
     (ITEM_ISSUED was already written inside issueLocked)
COMMIT
```

Notes:

- **Lock order is request → lot.** Every request-path writer follows it. The manual issue path touches only the lot, so there is no cycle. Deny and cancel touch only the request row and take no inventory lock.
- **Deadlocks** are already handled by `withDeadlockRetry`; `approveAttempt` is wrapped in it exactly like `issueAttempt`.
- **Two audit rows, deliberately**: `ITEM_ISSUED` on `GuildBankItems` (the ledger's own event, unchanged, so inventory history reads identically) and `REQUEST_COMPLETED` on `InventoryRequest` (the workflow event). Both share the request's `CorrelationID`, so the audit viewer shows them together; `REQUEST_COMPLETED.NewValues` carries `movementId`, `issuedQuantity` and `remainingQuantity`.
- **Recipient** is `CharacterName` verbatim, so movement-derived issued rows display the requesting character and the admin issued view needs no new join.
- **Movement note** = the officer's note when supplied, otherwise the requester's note (OD-R9 covers adding a prefix).

### 9.3 The completion update (guarded, single statement)

```sql
UPDATE InventoryRequest
   SET Status = 'Completed',
       IssuedQuantity = ?, CompletedMovementID = ?,
       DecidedAt = ?, DecidedByUserID = ?, DecidedByName = ?,
       DecisionNote = ?
 WHERE RequestID = ? AND Status = 'Pending';
```

`RowsAffected = 1` is required. After step 1 holds `FOR UPDATE` a zero-row result can only be a programming error, and the transaction rolls back (`ErrConflict`). The important property: **the item is never issued for a request that did not transition**, and a request is never completed without an issue, because they share a transaction.

### 9.4 Idempotency of approval

`IdempotencyKey = "req-" + RequestID` (≤64 chars, deterministic, unique per request).

- In practice a second approval never reaches the movement insert: step 1 holds the request row `FOR UPDATE`, and the second transaction reads `Status = 'Completed'` and returns `request_not_pending`. The key is a **backstop**, not the primary guard.
- If the key ever does fire (a future bug that bypasses the row lock), `approveAttempt` re-reads the request: `Status = Completed` **and** `CompletedMovementID` is the movement carrying that key → return success, because the caller asked for an outcome that already happened. Any other conflict shape is returned as-is.
- This gives at-most-once issuance without a client token, and two officers approving simultaneously produce one issue and one "already completed" page.

### 9.5 Relationship to returns, write-offs and manual adjustments

| Inventory action | Effect on a completed request |
|---|---|
| `RETURN` against the issue | None. The request stays `Completed`; the issued-rows projection shows the returned quantity as it does today |
| `WRITE_OFF`, `MERGE`, `RECEIVE`, `MOVE` | None |
| Manual `ISSUE` from the admin screen | None — creates a movement with `RequestID = NULL` |
| Lot soft-deleted or consolidated later | None; `CompletedMovementID` keeps provenance, `ItemName` keeps the label |

Reopening a request on return would silently double-issue the item on a second approve, which is why the design forbids it. "They got it, gave it back, and want it again" is a new request (OD-R12).

### 9.6 Repository surface to add

Extend the existing `AdminStore` / `AdminWriteTx` rather than adding a second transaction factory, so approval has one transaction for the lot, the movement, the request and the audit:

```go
// added to AdminStore
GetRequest(ctx context.Context, id int64) (domain.InventoryRequest, error)
ListRequests(ctx context.Context, f domain.RequestFilter) (domain.RequestPage, error)
CountPendingRequests(ctx context.Context, requesterUserID int64) (int, error)
PendingRequestForLot(ctx context.Context, lotID, requesterUserID int64) (int64, error)
PendingRequestsForLot(ctx context.Context, lotID int64, limit int) ([]domain.InventoryRequest, error)

// added to AdminWriteTx
LockRequest(ctx context.Context, id int64) (domain.InventoryRequest, error)
InsertRequest(ctx context.Context, in domain.CreateRequestInput) (int64, error)
CompleteRequest(ctx context.Context, p domain.CompleteRequestParams) error
DecideRequest(ctx context.Context, p domain.DecideRequestParams) error // Denied | Cancelled
```

Creation does **not** need an inventory transaction: it reads the lot without a lock, compares the form's `expectedModAt` against that read, validates the quantity against the same snapshot, and inserts the row plus `REQUEST_CREATED` in one short transaction. `expectedModAt` on create is a **courtesy** check ("the stock changed since you looked"), not a guarantee — a request is a statement of intent, so a lot that changes a second later is handled at approval time (`409 insufficient_stock`) rather than by holding a lock while a member fills in a form.

---

## 10. Audit design

### 10.1 Events

| Event type | Table | RecordID | OldValues | NewValues | When |
|---|---|---|---|---|---|
| `REQUEST_CREATED` | `InventoryRequest` | request ID | `null` | item, quantity, character, kind, note (public: handle present, IP not) | creation, member or public |
| `REQUEST_COMPLETED` | `InventoryRequest` | request ID | `{status:"Pending"}` | `{status:"Completed", movementId, issuedQuantity, remainingQuantity, note}` | approval, inside the issue transaction |
| `REQUEST_DENIED` | `InventoryRequest` | request ID | `{status:"Pending"}` | `{status:"Denied", reason}` | officer deny |
| `REQUEST_CANCELLED` | `InventoryRequest` | request ID | `{status:"Pending"}` | `{status:"Cancelled", reason}` | requester cancel |

Plus the existing `ITEM_ISSUED` row that `issueLocked` already writes for the same approval, on `GuildBankItems`.

### 10.2 Actor retention

- `UserID` / `UserName` on the audit row come from `domain.Actor`, exactly as every other admin event does (`validateActor` already guarantees a non-zero ID and name).
- A **public** request has no account: the audit row uses `UserID = 0`, `UserName = "public"`, and writes the character name plus Discord handle into `NewValues`. The request row remains the authoritative record (it keeps `CreatedByName`).
- Deleted or renamed accounts do not rewrite history: the audit row keeps the name at the time of the event, and the request keeps its own `CreatedByName` / `DecidedByName` snapshots.

### 10.3 Allowlist placement

`OfficerAuditEventTypes` (`internal/service/admin_inventory.go:62`) gains the four request events. This is correct because:

- Officers hold `CanManageInventory()`, so request decisions are within their remit.
- The audit viewer scopes by that list, so Officers see request history without gaining access to user-administration events.

`AdminAuditEventTypes` is derived by appending, so no change is needed there. Tests that assert the exact allowlist contents must be updated deliberately — a useful tripwire.

### 10.4 Correlation

`InventoryRequest.CorrelationID` is generated at creation and propagated to:

- the `REQUEST_CREATED` audit row,
- the `ISSUE` movement (`InventoryMovement.CorrelationID`),
- the `ITEM_ISSUED` audit row (already derived from the movement),
- the `REQUEST_COMPLETED` audit row.

Consequence: `/admin/audit?correlation=<id>` shows the whole lifecycle of one request — who asked, who approved, which movement it became — in one list. Deny and cancel reuse the same ID, so refused requests are equally traceable.

### 10.5 Retention and immutability

- No request row is ever deleted and no audit row is ever updated, consistent with the ledger.
- `Audit` is the compliance record; `InventoryRequest` is the operational record. Both are needed: the audit log is append-only and expensive to query, while the queue needs indexed, joinable state.

---

## 11. Required migrations

### 11.1 MariaDB (guild store) — `0008_inventory_request.sql`

Additive only; no existing table is rebuilt and no data is backfilled.

1. `CREATE TABLE InventoryRequest` (§2.2) with its indexes, FKs and check constraints.
2. `ALTER TABLE InventoryMovement ADD COLUMN RequestID BIGINT UNSIGNED NULL AFTER RelatedMovementID;`
   `CREATE UNIQUE INDEX uq_movement_request ON InventoryMovement (RequestID);`
   Safe because MariaDB permits multiple `NULL`s in a unique index. Provenance now lives in the ledger itself, so a movement history page can say "issued on request #123" without a reverse lookup.
3. `ALTER TABLE InventoryMovement ADD CONSTRAINT fk_movement_request FOREIGN KEY (RequestID) REFERENCES InventoryRequest (RequestID) ON DELETE RESTRICT ON UPDATE RESTRICT;`

Insert order inside the approval transaction is: request already exists → movement referencing it → request updated with `CompletedMovementID`. There is no circular *insert* problem, because `CompletedMovementID` starts `NULL` and is filled by the later `UPDATE`.

Two engine-level guarantees worth calling out in the migration header:

- `uq_request_movement` plus `ck_request_completed` mean one movement can complete at most one request, and a `Completed` request must name its movement.
- `uq_request_pending` on `(GuildBankItemID, PendingUserKey)` is how "one Pending request per member per lot" survives two concurrent submissions. MariaDB has no partial indexes, so the STORED generated column is the mechanism; any equivalent must be tested with two real concurrent inserts, not just sequential ones.

Migration test expectations:

- `guildbank_schema_migrations` records `0008`; a second run is a no-op (idempotent `IF NOT EXISTS` and a catalog-guarded `ALTER`, matching how 0007 guards its column).
- Inserting a request with a non-existent `GuildBankItemID` fails.
- Deleting a lot referenced by a request fails (`ON DELETE RESTRICT`).
- Two movements with the same `RequestID` fail; two with `NULL` succeed.
- Two `Pending` rows for the same `(GuildBankItemID, RequesterUserID)` fail; a second one after the first is `Cancelled` succeeds.

### 11.2 SQLite (accounts) — no migration

No new column is needed. Request history lives in MariaDB, and identity is already complete. `Users` and `Characters` are read-only from the request system's point of view.

### 11.3 Settings — no migration, one definition

Settings are rows in the existing SQLite `Settings` table, so adding a key is code, not schema:

| Key | Type | Default | Validation |
|---|---|---|---|
| `PublicRequestsEnabled` | boolean | `false` | existing boolean parser |

Add one `SettingDefinition` in `service/settings.go` plus its validation case; the settings page renders it automatically because `settingViews` iterates the definitions. Default **off** makes public submission an explicit owner decision rather than a launch accident.

Optional, only if OD-R6/R7 require them: `RequestMaxPendingPerUser` and `RequestPublicPerIPHour` as settings instead of constants. Recommendation: keep them constants for v1 — every setting is a support surface.

---

## 12. API design

### 12.1 Routes and classification

| Method | Path | Handler | Class | Notes |
|---|---|---|---|---|
| GET | `/request/new` | `requestHandler.newForm` | `classPublic` | member form when authenticated, public form otherwise |
| POST | `/request/new` | `requestHandler.create` | `classPublic` | CSRF; rate-limited; idempotency key |
| GET | `/request/:id/confirm` | `requestHandler.confirm` | `classPublic` | generic receipt; shows no other request's data |
| GET | `/my/requests` | `requestHandler.mine` | `classMember` | own requests only |
| GET | `/my/requests/:id` | `requestHandler.mineDetail` | `classMember` | 404 (not 403) for another user's ID |
| POST | `/my/requests/:id/cancel` | `requestHandler.cancel` | `classMember` | CSRF; own Pending only |
| GET | `/admin/requests` | `adminRequestHandler.queue` | `classOfficer` | filters, paging |
| GET | `/admin/requests/:id` | `adminRequestHandler.detail` | `classOfficer` | decision page |
| POST | `/admin/requests/:id/approve` | `adminRequestHandler.approve` | `classOfficer` | CSRF; issues |
| POST | `/admin/requests/:id/deny` | `adminRequestHandler.deny` | `classOfficer` | CSRF; reason required |
| GET | `/api/v1/requests` | `requestAPI.list` | `classMember` | own requests, JSON |
| GET | `/api/v1/requests/:id` | `requestAPI.get` | `classMember` | own only |
| POST | `/api/v1/requests` | `requestAPI.create` | `classPublic` | JSON create; same validation |
| POST | `/api/v1/requests/:id/cancel` | `requestAPI.cancel` | `classMember` | |
| GET | `/api/v1/admin/requests` | `adminRequestAPI.list` | `classOfficer` | |
| POST | `/api/v1/admin/requests/:id/approve` | `adminRequestAPI.approve` | `classOfficer` | |
| POST | `/api/v1/admin/requests/:id/deny` | `adminRequestAPI.deny` | `classOfficer` | |

`/request/:id/confirm` is the route that needs the most care: it is `classPublic` and renders only the item name, quantity and character name for the ID supplied, with `Cache-Control: no-store` and `X-Robots-Tag: noindex`. An enumerating visitor can therefore learn that a request number existed — acceptable, because the receipt carries nothing else (OD-R3 revisits this).

Every route must be added to `routeMatrix` in `authorization_matrix_test.go`, and nav entries to `navigation_role_test.go`, in the same commit as the handler.

### 12.2 JSON shapes

```json
// GET /api/v1/requests/:id  (member, own request)
{
  "id": 123,
  "status": "Pending",
  "item": { "lotId": 42, "equid": 12345, "name": "Cloak of Flames" },
  "quantity": 1,
  "character": "Icefear",
  "note": "for raid",
  "decisionNote": null,
  "createdAt": "2026-11-26T18:04:11Z",
  "decidedAt": null,
  "decidedBy": null,
  "movementId": null,
  "available": true
}
```

```json
// POST /api/v1/admin/requests/:id/approve  → 200
{
  "id": 123,
  "status": "Completed",
  "issuedQuantity": 1,
  "movementId": 412,
  "remainingQuantity": 3,
  "decidedAt": "2026-11-26T18:20:02Z",
  "decidedBy": "OfficerBlue"
}
```

Field rules, following the existing admin DTO discipline:

- The member/public DTO is an **explicit allowlist**; it never includes `requesterUserId`, another requester's `contactDiscord`, `createdByUserId`, `decidedByUserId` or `correlationId`.
- The officer DTO adds `requesterKind`, `requesterUserName`, `contactDiscord`, `onHandAtCreate`, `currentOnHand`, `lotActive`.
- No `SELECT *` in either path (the PD5-25 convention).

### 12.3 Error semantics

| Situation | Status | Code |
|---|---|---|
| Validation failure (quantity, note, character, handle) | 422 | `validation_error` + field map |
| Character not owned by the caller | 422 | `character_invalid` |
| Lot missing or removed | 409 | `item_unavailable` |
| Requested quantity above on-hand | 409 | `insufficient_stock` |
| Duplicate Pending for the same lot by the same user | 409 | `request_already_pending` |
| Pending budget exceeded | 429 | `too_many_pending` |
| Request already decided (by anyone, including a replay of the same approval) | 409 | `request_not_pending` — except a replay whose movement matches, which returns 303 to the completed page (§9.4) |
| Officer deciding their own request | 403 | `own_request` |
| Stale `expectedModAt` on approve | 409 | `stale_item` |
| Public rate limit / honeypot / time-trap | 429, or a silent success page | `rate_limited` / no signal |
| `PublicRequestsEnabled` off | 404 | — (the route must not advertise itself) |

### 12.4 CSRF and method conventions

All writes are POST through the existing CSRF middleware, rendered as ordinary `<form>` submissions; no `method=PUT/DELETE`, matching the rest of the app. Success is always a 303 (PRG). The JSON API is additive and intended for future tooling, not a replacement for the HTML flow.

---

## 13. UI design

### 13.1 New templates

Naming follows the existing flat, role-prefixed convention:

| File | Purpose |
|---|---|
| `request_form.html` | Shared create form: renders the member variant (character dropdown) or the public variant (text + Discord handle) from one view model |
| `request_confirm.html` | Receipt page: public variant ("Request #123 received", no status link) and member variant, from one template and two models |
| `requests_mine.html` | Member request list |
| `request_mine_detail.html` | Member request detail with the cancel form |
| `admin_requests.html` | Officer queue |
| `admin_request_detail.html` | Officer decision page |

Edits to existing templates:

- `inventory_detail.html`: add the "Request this item" button / pending indicator, and gate the public button on `PublicRequestsEnabled`.
- `admin_inventory_detail.html`: add the "Pending requests for this item" panel.
- `partials.html`: add a `requestStatusBadge` partial and extend the admin nav partial with the Requests entry.

### 13.2 Public item page

```
┌──────────────────────────────────────────────┐
│ Cloak of Flames            [Request this →]  │
│ 3 on hand · Bank Tab 2                       │
└──────────────────────────────────────────────┘
```

When `PublicRequestsEnabled` is false the button is absent and a single line reads: "Requests are currently limited to guild members. [Sign in]".

### 13.3 Public request form

```
Request: Cloak of Flames
Available: 3

Quantity         [ 1 ]      (1–3)
Character Name   [__________]   (3–15 letters)
Discord handle   [__________]   so an officer can reach you
Note (optional)  [                    ]   0/255

[ Submit request ]
```

- One column, labels above controls, `inputmode` where relevant, no client-side JS beyond the char counter (the app is server-rendered; keep it that way).
- Errors render above the form with a field-anchored message list, matching the existing registration/settings error pattern.
- The CSRF field, honeypot and time-trap are hidden inputs; the honeypot is positioned off-screen, not `type=hidden`, so naive bots fill it.

### 13.4 Member request form

Identical layout, with the character row swapping to:

```
Character        [ Icefear        ▼ ]
                 (from your profile)
```

or, when the profile is empty:

```
Character Name   [__________]
                 Add characters to your profile to pick from a list → /account/profile
```

No Discord handle field for members: the account already carries the linked Discord identity when present, and officers contact members through the guild roster.

### 13.5 Member request list and detail

`/my/requests`

```
Your requests                                  [Newest first ▼]

 #123  Cloak of Flames      1   Icefear   Pending     2 hours ago
 #118  Flask of the Titans  5   Raidshaman Completed  3 days ago → movement #389
 #110  Arcanite Bar         2   Banktoon  Denied     1 week ago  (reason…)
```

- Status uses a badge partial with distinct colours and text (never colour alone): `Pending`, `Completed`, `Denied`, `Cancelled`.
- A `Pending` row whose lot is gone shows `Pending · item unavailable`.
- Filters: status; paging at 25.

Detail page: request facts, decision block (who/when/reason), the linked movement when `Completed` (link to `/admin/inventory/:id/history` for officers; for members a read-only summary), and a **Cancel request** form with a confirm step, shown only for `Pending`.

### 13.6 Officer queue and decision page

- Queue: dense table, sticky filter bar, `Requests` nav item with a Pending badge. Age column bands at 2 days (amber) and 7 days (red).
- Row actions: **Approve** (opens the decision page rather than acting inline — an irreversible issue deserves a confirmation surface, not a table button) and **Deny**.
- Decision page: two stacked cards.
  - "Why this can be issued" — live lot state, quantity check, links.
  - Decision block — approve (optional note) / deny (required reason) as separate forms, both with hidden `expectedModAt`.
- Precedent from the existing admin pages: irreversible writes confirm on a dedicated page rather than via JS dialogs (`admin_remove.html`, `admin_inventory_issue.html`), so this matches.

### 13.7 Notices

Notice codes go through the existing message-partial mechanism, one fixed code per outcome:

`created`, `cancelled`, `completed`, `denied`, `insufficient-stock`, `already-decided`, `stale-item`, `own-request`, `public-disabled`.

No notice ever echoes user-entered text; the templates read the row instead.

### 13.8 Accessibility and copy rules

- Every status badge carries a text label, not just colour.
- The approve/deny buttons state the consequence in the label: "Approve and issue 1 × Cloak of Flames" rather than "Approve".
- Deny requires the reason field to be filled; the form says why ("Members see this reason").
- Age and timestamps use `<time datetime="…">` with a human-readable value.

---

## 14. Security design

### 14.1 CSRF and method safety

- Every state-changing route is POST with the existing CSRF token; the middleware already covers all non-GET routes, so no new mechanism is introduced.
- Approval requires the hidden `expectedModAt` field *and* the CSRF token; a replayed approval therefore fails on `request_not_pending` rather than on CSRF, which keeps the error honest.
- Public create is POST-only. A GET to `/request/new` renders the form; it never creates.

### 14.2 Rate limiting and bot defences

Reuse `service/rate_limit.go` as-is:

| Scope | Limit | Response |
|---|---|---|
| Public create, per IP | 5 / hour | 429 + generic page |
| Public create, global | 60 / hour | 429 + generic page |
| Member create, per user | 20 / hour | 429 |
| Public form GET, per IP | 60 / hour | 429 |

Plus, on the public create path only:

- **Honeypot**: a hidden-but-visible-to-bots field; a non-empty value silently returns the normal success page without writing anything. Silence is deliberate — telling a bot it was detected invites tuning.
- **Time trap**: submissions arriving under 2 seconds after the form was rendered are treated the same way.
- Neither mechanism is a substitute for the IP limit; all three are cheap and independent.

### 14.3 PII handling

- `ContactDiscord` is required for public requests and is **never** rendered on any public page or in the public receipt. It appears only in the officer queue/detail and the officer JSON DTO.
- The public request's IP and User-Agent are **not** stored on the request row. The audit row for `REQUEST_CREATED` carries the standard nullable `SourceIP` that every write in the app already records (`Audit.SourceIP`, populated from request context), and nothing else — so abuse is investigable without turning the request table into a personal-data store.
- Log lines for creation record the request ID, item ID, quantity and requester kind — never the note or the handle. Existing logging discipline is kept: identifiers, not payloads.
- `Note` and `DecisionNote` are capped (255 / 500) and rendered as text, never as HTML.

### 14.4 Character ownership re-validation (server-side)

The member character dropdown is a convenience, not an authorization decision. On **every** member create:

1. Read `Characters` for the submitting `UserID`.
2. Require an exact, case-insensitive match on `Name` (the same collation the profile page uses).
3. Reject with `422 character_invalid` when there is no match, even if the posted value "looks" like one of the user's characters.

Consequences that make this necessary:

- The form is a plain POST; the dropdown's option list is attacker-editable.
- Character names are visible on other members' profiles, so knowing a name is trivial; owning it is not.
- The rule must hold for the JSON API too, since it bypasses the dropdown entirely.

Public requests are exempt from this rule by definition (no account), which is exactly why they carry a required Discord handle and are visually distinct in the queue (`requesterKind = Public`).

### 14.5 Self-decision and privilege boundaries

- An officer may not approve, deny or otherwise decide their own request: the handler compares `decidedByUserId` against `requesterUserId` before opening any transaction and returns `403 own_request`.
- Officers hold `CanManageInventory()`, so no new permission flag is introduced. Members cannot reach `/admin/requests` at all; the route returns 404 rather than 403 for non-officers, consistent with existing admin routes.
- Members can cancel only their own `Pending` request; "own" is resolved by `requesterUserId`, never by a posted value.

### 14.6 Enumeration and disclosure

| Surface | Leak risk | Mitigation |
|---|---|---|
| `/my/requests/:id` | Another user's request | 404 for non-owners; no timing signal (same query path) |
| `/request/:id/confirm` | Existence of a request number | Only item name, quantity, character; `no-store`, `noindex` |
| `/admin/requests/:id` | Full requester data | Officer-only, 404 for members |
| Approve/deny errors | Stock levels | `insufficient_stock` says nothing about other requests; the lot page already shows on-hand publicly |

Request numbers are sequential and therefore guessable, which is why the receipt is deliberately thin. A tokenised public status page is deferred (OD-R3).

### 14.7 Idempotency and duplicate protection

- Create: `Idempotency-Key` honoured when present (existing convention), plus `uq_request_pending` on `(GuildBankItemID, PendingUserKey)` for members (§2.2). A double-submitted form returns the first request's receipt rather than creating a second row; the duplicate-key error is translated to `409 request_already_pending`. Public requests have `RequesterUserID = NULL`, so the index does not constrain them — their volume is governed by the rate limits in §14.2 instead.
- Approve: the deterministic key `req-<RequestID>` on the movement, plus the guarded `UPDATE … WHERE Status='Pending'`. Two concurrent approvals cannot both issue.
- Cancel: guarded `UPDATE … WHERE Status='Pending'`; a second cancel is a no-op with `request_not_pending`.

### 14.8 Threats explicitly out of scope

- A member requesting an item, receiving it, and trading it out of the guild: unchanged exposure versus the existing manual issue flow.
- Compromised officer accounts: out of scope, but the request → movement → audit chain means the damage is at least fully traceable.
- Denial of service beyond the rate limits above: the app already runs behind the existing reverse proxy posture.

---

## 15. Test plan

### 15.1 Unit and service tests

| Area | Cases |
|---|---|
| Creation (member) | happy path; quantity 0, negative, above on-hand, above `RequestedQuantity` cap; note length; character not owned → `character_invalid`; character owned but different case → accepted; lot removed → `item_unavailable`; Pending budget at 5 → `too_many_pending`; duplicate Pending for same lot → `request_already_pending`; idempotency key replay → same ID |
| Creation (public) | happy path; `PublicRequestsEnabled` off → 404; missing Discord handle → `validation_error`; honeypot filled → no row written, success page shown; sub-2-second submit → same; rate limit 5/hour/IP → 429 |
| Approval | happy path issues exactly `RequestedQuantity`; movement `Recipient` equals `CharacterName`; `Movement.RequestID` set; `CompletedMovementID` set; `IssuedQuantity` set; lot `OnHand` decremented by exactly the issued amount; both audit rows share `CorrelationID`; lot with fewer units than requested → `insufficient_stock` and **no** movement |
| Approval failures | already `Completed` → `request_not_pending`; `Denied`/`Cancelled` → same; missing lot → `item_unavailable`; stale `expectedModAt` → `stale_item`; self-decision → `own_request`; replay of the same approve → one movement only |
| Deny / cancel | deny writes reason and audit; deny without reason → validation error; cancel by owner; cancel by another member → 404; cancel of a `Completed` → `request_not_pending`; cancel writes no movement |
| Concurrency | two approvals of the same request in parallel → one movement; two creates for the same lot by one member in parallel → one row; approve racing a manual issue that empties the lot → one wins, the other gets `insufficient_stock` |
| Projections | `available` flag; `currentOnHand` vs `onHandAtCreate`; `lotActive` after soft delete; queue filters by status/kind/age; `PendingRequestForLot` returns the member's ID |

### 15.2 Integration and migration tests

- Migration applies and is idempotent (§11.1 expectations list).
- FK/unique/check behaviour verified by direct SQL, not through the service layer.
- `issueAttempt` and `approveAttempt` produce byte-identical movement rows for the same input — the assertion that proves the extraction is behaviour-preserving.
- Audit allowlist tests updated for the four new events.

### 15.3 Authorization and navigation

- Every route in §12.1 added to `routeMatrix` in `authorization_matrix_test.go`; the test fails if a route exists without a classification.
- Nav entries added to `navigation_role_test.go`: `Requests` appears for Officer/Admin, absent for Member; `My requests` appears for Member and above.
- Member hitting `/admin/requests` → 404 (not 403); anonymous hitting `/my/requests` → redirect to login.

### 15.4 Manual QA checklist

1. Public item page with the setting off shows no button; turn it on, button appears.
2. Submit a public request; officer sees it in the queue with the handle; member pages cannot see it.
3. Approve it; lot on-hand drops by the requested amount; movement history shows the issue; audit correlation shows both rows.
4. Approve a request for an item another officer just emptied; the error is `insufficient_stock` and nothing is issued.
5. Cancel a member request; it leaves the queue; the member sees `Cancelled`.
6. Deny with a reason; the member sees the reason on their detail page.

---

## 16. Implementation phases

Each phase is independently releasable and leaves the app in a consistent state. Later phases only add; nothing in an earlier phase is reworked.

### Phase 1 — Schema and domain (no routes)

- `0008_inventory_request.sql`, migration test, `RequestID` column on `InventoryMovement`.
- `internal/domain/request.go`: types, validation, `RequestFilter`.
- `RequestStore` / `AdminWriteTx` additions (§9.6) with their SQL, tested directly.
- Ship gate: migration applies on a copy of production data; no behavioural change to the running app.

### Phase 2 — Extraction of `issueLocked`

- Move the non-committing core of `issueAttempt` into `issueLocked(ctx, tx, in)`; `issueAttempt` becomes a thin wrapper.
- No new routes, no new UI, no request table usage yet.
- Ship gate: full existing test suite green, including the "identical movement rows" test (§15.2). This is the riskiest refactor and deserves to land alone.

### Phase 3 — Member flow

- `/my/requests`, `/my/requests/:id`, cancel; create form with the character dropdown; member receipt.
- Audit events and allowlist update.
- Ship gate: members can request and cancel; officers still issue manually; nothing references `PublicRequestsEnabled`.

### Phase 4 — Officer flow

- `/admin/requests` queue, detail, approve, deny; `approveAttempt` calling `issueLocked`.
- Pending panel on `admin_inventory_detail.html`; nav entry; badge.
- Ship gate: approval issues through the ledger exactly like a manual issue, verified against the existing history views.

### Phase 5 — Public flow

- `PublicRequestsEnabled` setting, public form, receipt, rate limits, honeypot, time-trap.
- Public item page button.
- Ship gate: setting defaults off; enabling it is a deliberate owner action; disable = button disappears and `/request/new` 404s.

### Deferred (documented, not planned)

JSON API (§12.1 lists it for completeness), tokenised public status page (OD-R3), CSV export (OD-R11), Discord DM (OD-R10), auto-expiry (OD-R7), partial fulfilment (OD-R4).

---

## 17. Owner decisions

Each decision can be answered "yes/no to the recommendation" without further reading. Recommendations follow from the constraints above; where a recommendation contradicts something an owner might reasonably want, the cost of the alternative is stated.

| ID | Question | Recommendation | Cost of the alternative |
|---|---|---|---|
| **OD-R1** | Should `PublicRequestsEnabled` default to off? | **Yes, off.** Public submission changes the guild's exposure (unknown requesters, spam surface, PII). Turning it on should be deliberate, and it is one checkbox on the existing settings page | If it defaults on, a fresh deployment exposes a public write endpoint to bots before an officer has seen the queue |
| **OD-R2** | Is the Discord handle mandatory for public requests? | **Yes, in v1.** Without it an officer cannot contact an unauthenticated requester, and a request nobody can follow up is a dead row. If owners want a public form with no contact field, the cleaner answer is to leave public requests disabled | An optional handle forces officers to guess or deny for lack of contact — worse than not accepting the request |
| **OD-R3** | Should requesters get a tokenised public status page instead of only the receipt? | **No for v1.** The receipt plus the Discord contact covers the expected volume. A token URL adds a bearer credential to protect, a second lookup path, and an outbound-link problem (people paste it into Discord) | Without it a public requester cannot self-serve status and must ask an officer. Acceptable at low volume |
| **OD-R4** | Should partial fulfilment be supported (request 5, issue 3)? | **No for v1.** `IssuedQuantity` exists so it needs no migration later, but supporting it now means a new status, a second decision surface and a rule for the remainder. Strict `issued == requested` keeps one decision, one movement, one end state | Members who ask for too many units are denied and re-request the right number — a visible, coachable outcome |
| **OD-R5** | May an Administrator approve their own request? | **No.** The rule is separation of duties, not trust in a person: the request-to-movement chain is the guild's audit story, and "I approved myself" weakens it even when honest | An exception puts an asterisk on every future audit review of that administrator's requests |
| **OD-R6** | Should a member with no profile characters have one auto-created from the typed name? | **No.** The profile is the member's own data; writing to it from a request form surprises people and lets a typo become a permanent character. The form links to `/account/profile` instead | Auto-creation saves one click, costs a confusing profile edit plus a delete flow for the mistake |
| **OD-R7** | Should stale `Pending` requests expire automatically? | **No.** A timer that silently moves requests to `Cancelled` hides work from officers and gives requesters no reason. Age banding in the queue (§13.6) makes staleness visible, and denial carries a reason | Auto-expiry avoids a slowly growing queue but produces "why was my request cancelled?" questions and untraceable outcomes |
| **OD-R8** | Is `DecisionNote` visible to the requester? | **Yes.** A denial reason is the difference between "the guild refused me" and "the guild wants one, not five". Capped at 255, required for denial, rendered as text | Hiding it turns every denial into a support ticket to an officer |
| **OD-R9** | Should the approval movement's note be prefixed with the request ID (`request #123: for raid`)? | **Yes.** `InventoryMovement.RequestID` records the link structurally, but the note is what officers read in a history list without following links. Keep the prefix short and never duplicate the requester's note into it | Without it, an issue history gives no hint which issues came from requests |
| **OD-R10** | Should approving or denying send a Discord DM (or e-mail) to the requester? | **No for v1.** No outbound messaging infrastructure exists, and adding one brings token management, retries, delivery state and a new failure mode into an inventory feature | Members must check `/my/requests`; consistent with the "no notifications" position already taken in §7.5 |
| **OD-R11** | Is a CSV export of requests needed? | **Not for v1.** Reporting is served by the audit correlation filter and the queue filters. The app's CSV patterns exist for inventory import, not reporting | Officers who want a spreadsheet must copy from the queue; revisit when someone asks |
| **OD-R12** | After a `Completed` request's item is returned, should the request reopen or allow a re-approve? | **Neither.** The return is an inventory movement (§9.5); the item goes back into the lot and the member creates a new request. One request = one issue, permanently | Reopening lets a second approve issue against a stale row and makes `CompletedMovementID` non-final |

### 17.1 Decisions that are not owner-facing

Settled by the existing codebase; recorded so implementation does not reopen them:

- Approval goes through `issueLocked` (§9.1) — no second issue path.
- Lock order is request row, then lot row (§9.2); deny and cancel take no inventory lock.
- Requests live in MariaDB with snapshots instead of cross-database FKs (§2.1).
- Four statuses, no `Approved` (§4).
- New routes are classified in the authorization and navigation tests in the same commit (§15.3).
- The public surface stays off until an owner enables it (OD-R1).

---

## 18. What this document deliberately does not contain

- No `.sql`, `.go`, `.html` or `.js` file was created or modified. This is a proposal.
- No time or effort estimates.
- No template markup beyond the low-fidelity sketches in §13, which fix field order and wording, not styling.
- No assignment of phases to people.

