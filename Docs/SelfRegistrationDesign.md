# Self-Service Registration via Discord (Design Proposal)

**Status:** implemented 2026-11-26 — see §17 for the implementation record and the two small additions made during the build. D-SR-13 (scope isolation) is in force: linking and sign-in keep `identify`, and only `/register/discord` requests `identify guilds`.
**Scope:** P3 follow-up ("P4-SR"). Adds self-service account creation to GuildBank using Discord identity, an approval status on `Users`, and the administration surface to work the approval queue.
**Non-goals:** the request system, email of any kind, Discord role-based authorization, automatic Officer/Administrator assignment, re-verification of guild membership over time.

---

## 0. Summary of the recommendation

| Question | Recommendation | Section |
|---|---|---|
| Where does approval live? | New `Users.AccountStatus` column (`Approved` / `PendingApproval`); `DisabledFg` stays a separate axis | §2 |
| Who approves? | Administrators only in the first release; Officer approval is an owner decision | §3.3, §14 |
| When is a registration auto-approved? | Only when `DiscordGuildId` is configured **and** the guild check returned a definitive "member" | §6 |
| Username selection: before or after OAuth? | **After** OAuth, on a signed short-lived ticket | §7 |
| Does registration ever sign in an existing account? | No. A Discord identity that already owns an account is sent to `/login?notice=registration-exists` | §5.5 |
| Password for self-registered accounts | None. A discarded random bcrypt hash is stored so `PasswordHash` stays `NOT NULL` and can never verify | §2.5 |
| New settings | `SelfRegistrationEnabled` (default off) | §8.6 |
| New callback URI in the Discord portal | Yes: `/register/discord/callback` (third registration) | §6.1 |
| MariaDB changes | None — accounts live in SQLite only | §4.5 |

---

## 1. Current state this design builds on

| Concern | Where it lives today | Consequence for this design |
|---|---|---|
| Account row | `domain.User` (`internal/domain/identity.go:125`), `Users` table (0001/0002/0003) | One additive column, no rebuild |
| "May authenticate" | `domain.User.IsEnabled()` = `!Disabled` (`identity.go:166`); enforced in `AuthService.Login` (`auth.go:103`), `issueSession` (`auth.go:131`), `Authenticate` (`auth.go:191`) and `DiscordService.Login` (`discord.go:356`) | One predicate change gates every entry point |
| Discord identity | `Users.DiscordUserID/UserName/LinkedAt` + partial unique index `ux_users_discord` | The identity is already the natural registration key |
| Guild check | `DiscordAPI.GuildMember` → `GET /users/@me/guilds`; called only from `DiscordService.Link`; result recorded in the audit payload only, never persisted | Registration needs the same call, at creation time |
| OAuth state | `discordStateManager` (`api/discord_state.go`): HMAC payload `v2|scope|userID|expiry|next|nonce`, cookie double-submit, 10-minute TTL, one cookie per flow | A third scope + third cookie follows the existing pattern |
| Route authorization | `authorization_matrix_test.go` `routeMatrix` with `classPublic/Member/Officer/Admin`; enforcement sweep in `authorization_enforcement_test.go` | Every new route must be classified or the build fails |
| Account admin | `UserAdminService` + `sqlite` store guards (last-enabled-Administrator), `admin_users.html`, `admin_user_detail.html`, `POST /admin/users/:id` (`active`/`disabled`) | Approval is a new, explicit action rather than a third value in the existing select |
| Self-registration 303 lesson | CSP `form-action 'self'` cancels a form-driven cross-origin redirect (README route table; ProjectStatus §6.19) | All new OAuth starts are plain `<a>` GET navigations |

---

## 2. Account status model

### 2.1 Two independent axes

`Users` already distinguishes *disabled* (administrative penalty; sessions revoked; reversible) from *enabled*. Approval is a third, orthogonal state: an account may be **unapproved** without being **disabled**, and vice versa. Overloading `DisabledFg` would destroy that distinction in the audit log, in the admin UI, and in the last-administrator guard.

```
                     approve
  PendingApproval ─────────────▶ Approved
        │  ▲                        │  │
        │  └────────────────────────┘  │ disable
        │        return to pending     ▼
        │                          Disabled
        └────────────── delete ────────┘  (any state; typed-name confirmation)
```

### 2.2 Values and predicates

```go
// domain/identity.go (proposed)
type AccountStatus string

const (
    AccountStatusApproved        AccountStatus = "Approved"
    AccountStatusPendingApproval AccountStatus = "PendingApproval"
)

func (s AccountStatus) Valid() bool {
    return s == AccountStatusApproved || s == AccountStatusPendingApproval
}

// ParseAccountStatus maps a stored value onto the model. Blank means Approved,
// because 0004 backfills every existing row and a zero-value domain.User in a
// test or a partial read must not be treated as unapproved. Any other
// unrecognized value fails closed to PendingApproval, so corruption can never
// authenticate.
func ParseAccountStatus(value string) AccountStatus
```

| Predicate | Meaning | Used by |
|---|---|---|
| `IsApproved() bool` | `ParseAccountStatus(raw) == Approved` | Admin UI labels, approval guard |
| `IsPending() bool` | `Status == PendingApproval` | Admin UI, `/register` notices |
| `IsEnabled() bool` | **redefined**: `IsApproved() && !Disabled` | Login, session validation, Discord sign-in, session issuance, character add |
| `CanManageUsers()`, `CanAdministerInventory()`, … | unchanged role checks | unchanged |

**Redefining `IsEnabled()` is the safety property of this design.** Every authentication gate already calls it (`auth.go:103/131/191`, `discord.go:356`), so a pending account is refused by code that was never touched, and future gates inherit the rule. Nothing may test `DisabledFg` directly for authentication; the admin UI reads `Disabled` for the *label* and `IsPending` for the *approval* label.

The `User` struct mirrors the existing `AccessLevel`/`Role()` pattern: a raw `AccountStatus string` field plus a parsed `Status() AccountStatus` accessor, so a corrupt stored value is quarantined at read time instead of failing a scan.

### 2.3 Display status (derived, not stored)

| Stored | Label in `/admin/users` | Detail page actions |
|---|---|---|
| `PendingApproval`, `DisabledFg=0` | `Pending approval` | Approve / Disable / Delete |
| `Approved`, `DisabledFg=0` | `Active` | Role / Disable / Delete |
| `Approved`, `DisabledFg=1` | `Disabled` | Role / Enable / Delete |
| `PendingApproval`, `DisabledFg=1` | `Pending approval · disabled` | Enable / Approve / Delete |

A pending account is never shown as "Active", and a disabled pending account is never approved implicitly.

### 2.4 Transitions and their invariants

| Transition | Trigger | Invariants |
|---|---|---|
| `(none) → PendingApproval` | Self-registration, guild check not a definitive member | Role is `Member`; no session exists; `MustChangePassword = 0` |
| `(none) → Approved` | Self-registration, guild check returned member | Role is `Member`; the handler issues a session afterwards |
| `PendingApproval → Approved` | Administrator approves | Role stays `Member`; audit event; no session is created |
| `Approved → PendingApproval` | Administrator withdraws approval (rare) | **Deletes every session for the account in the same transaction** (mirrors disabling) |
| `any → Disabled` | Administrator disables | Existing behaviour, unchanged (sessions revoked, last-admin guard) |
| `any → deleted` | Administrator deletes | Existing behaviour, unchanged (typed-name confirmation, self-delete refused) |

`SetUserAccessLevel` must refuse a role change while `IsPending()` — "approve the account before assigning a role" (`409`). Approval and promotion stay two separate, separately audited acts; nothing can leave a pending account holding Officer/Admin, and nothing can be promoted by a single ambiguous form submit.

### 2.5 Password representation for Discord-only accounts

`Users.PasswordHash` is `NOT NULL`, and the whole login path assumes a hash exists. Rather than relax the column (a rebuild that would cascade every session, as 0002 did), a self-registered account stores

```go
h, _ := service.HashPassword(randomSecret) // 32 bytes from crypto/rand, never logged, never returned
```

`VerifyPassword` on that hash succeeds only for a secret that no longer exists anywhere. Administrators can still issue a temporary password through the existing reset action, which turns the account into a password-capable account; the profile "change password" page refuses with the existing `currentPassword` validation until then. **Owner decision D-SR-7** covers whether self-registered users may set an initial password at registration instead.

---

## 3. Approval design

### 3.1 Automatic approval (guild member)

The Discord consent proves the identity; `GET /users/@me/guilds` proves membership. When **both** hold, the account is created `Approved` and the handler issues a session through `AuthService.IssueSession`, exactly as `DiscordService.Login` does today. The user lands on `/` (or a safe `next`) with no officer involvement.

### 3.2 Pending approval (not a member, or unverifiable)

Same creation path, `AccountStatus = PendingApproval`. No session is issued — the handler does not even call `IssueSession` for an account that reports `IsEnabled() == false`, which keeps the rule in one place. The user is redirected to `/login?notice=registration-pending`, which explains that an officer must approve the account before it can be used. A pending account can do nothing else: no session, no character names, no future requests.

### 3.3 Who may approve

**Recommendation: Administrators only**, matching `CanManageUsers()`. Rationale: approval creates an authenticated Member who can then claim globally unique character names; that is the same trust decision as account creation today, which is Administrator-only. Officer approval would require widening `CanManageUsers` or introducing a second predicate, and would make "who let this person in" ambiguous in the audit trail.

**Owner decision D-SR-2** records the alternative (Officers approve Members; Admins approve anything) and its consequences.

### 3.4 Approval does not authenticate

Approving never signs anybody in and never creates a session. The approved user signs in through `/login` or `/auth/discord`. This keeps one authentication path per credential and keeps the login limiter authoritative over every sign-in.

### 3.5 Membership can lapse (out of scope, recorded)

`GuildVerifiedAt` records *when* the check passed. Re-checking membership on every Discord sign-in, or expiring approval when a member leaves the Discord guild, is deliberately out of scope: `DiscordService.Login` performs no guild call today and must not start failing sign-ins because Discord is briefly unavailable.

### 3.6 Queue hygiene

Approval is a queue, so the design provides a way to see and clear it:

- `/admin/users?status=pending` lists pending accounts oldest first, with age.
- The accounts page shows a count banner ("3 accounts await approval") linking to that filter.
- Delete remains the rejection action; the typed-name confirmation already prevents stray clicks. A dedicated "Reject" with a stored reason is **owner decision D-SR-9** (recommended: defer; delete plus audit is sufficient for a guild-sized queue).
- Pending rows are not auto-deleted. Age is displayed so an officer can decide. An expiry setting is **owner decision D-SR-8**.

---

## 4. Required schema changes

### 4.1 Migration `0004_self_registration.sql` (SQLite)

```sql
-- 0004_self_registration.sql
-- Adds the approval axis. DisabledFg stays the administrative penalty flag; this
-- column answers a different question ("has an officer admitted this account?").
--
-- ADD COLUMN with a constant default is a metadata-only change: no table rebuild,
-- no session cascade, nobody is signed out. Existing accounts and every
-- Administrator-created account become 'Approved', which is exactly today's
-- behaviour.

ALTER TABLE Users ADD COLUMN AccountStatus TEXT NOT NULL DEFAULT 'Approved'
    CHECK (AccountStatus IN ('Approved','PendingApproval'));

CREATE INDEX ix_users_status ON Users (AccountStatus, CreatedAt);
```

**Verification requirement:** the migration must be proven by a test (mirroring `users_roles_migration_test.go`): apply 0001–0004 to a database that already holds an Approved-era row and assert (a) that row reads `Approved`, (b) a direct `UPDATE ... AccountStatus = 'Bogus'` is rejected by the CHECK, (c) `PRAGMA user_version = 4`, (d) `Sessions` was not cascaded. If the runtime SQLite build rejects an inline CHECK on `ADD COLUMN`, the fallback is to add the column **without** the CHECK and keep the allowlist in Go only — never a 0002-style table rebuild, which would sign every account out for a constraint.

### 4.2 Recommended additional columns (same migration, still additive)

```sql
ALTER TABLE Users ADD COLUMN Origin          TEXT NULL; -- 'SelfRegistration', or NULL for administrator-created
ALTER TABLE Users ADD COLUMN GuildVerifiedAt TEXT NULL; -- last successful guild check, UTC ISO-8601
```

- `Origin` lets the admin UI answer "who created this, and why is it pending?" without overloading `ModBy`, which is a *username snapshot* — a real account could be named `registration`.
- `GuildVerifiedAt` persists what is currently an audit-only boolean (`DISCORD_LINKED` newValues `guildVerified`). The pending queue is unusable without it: an officer needs to know whether the guild check ran at all.

Both are nullable, so there is no backfill and no reinterpretation of existing rows (`NULL` = unknown / administrator-created). They are optional for phase S1 but should be decided before the admin-UI phase, because retrofitting them later costs another migration.

### 4.3 Code touch points for any new `Users` column

Three places enumerate `Users` columns explicitly and will silently disagree if missed:

| Location | Why it must change |
|---|---|
| `internal/repository/sqlite/users.go:89` `userColumns` | Used by `GetUserByName/ID`, `ListUsers`, `UserByDiscordID` |
| `internal/repository/sqlite/users.go:52` `scanUser` | Scan order must match `userColumns` |
| `internal/repository/sqlite/sessions.go:13` `sessionSelect` (+ `scanSession`) | The session join re-lists every user column; a missing column means an authenticated request sees a zero `AccountStatus` |

`insertUser` (`users.go:135`) must also write the new values; omitting the column there would fall back to the default (`Approved`) and silently approve a pending registration.

### 4.4 New store operations

```go
// CreateRegisteredUser inserts the account and its Discord identity in one
// transaction. ux_users_username and ux_users_discord are the authorities; a
// duplicate is reported as domain.ErrAlreadyExists.
CreateRegisteredUser(ctx context.Context, u domain.User, identity domain.DiscordIdentity) (domain.User, error)

// SetUserAccountStatus approves (or withdraws approval of) one account. It is
// Administrator-guarded and deletes every session when the account stops being
// approved. Auditing stays in the service.
SetUserAccountStatus(ctx context.Context, userID int64, status domain.AccountStatus,
    at time.Time, actor domain.Actor) (domain.User, error)
```

`CreateUser` (administrator path) sets `AccountStatus: Approved` and leaves `Origin` empty explicitly rather than relying on column defaults, so the two creation paths differ only where they are meant to.

### 4.5 No MariaDB change

Accounts, identities and approvals all live in SQLite (D-13). The guild store only receives audit rows whose `EventType` is a free-form string, so `USER_SELF_REGISTERED` and `USER_APPROVED` need no schema or privilege change. No cross-store transaction is introduced: the SQLite commit is authoritative and the audit write stays best-effort, exactly as `writeAudit` already behaves.

---

## 5. Self-registration workflow

### 5.1 Entry points

`login.html` becomes three choices, in this order:

```
Sign in               (existing username/password form)
Sign in with Discord  (existing /auth/discord, shown when Discord is ready)
Create account        (new /register, shown when self-registration is on)
```

`Create account` must be a plain `<a href="/register">`, never a form. Every OAuth start is a GET navigation because CSP `form-action 'self'` cancels a form submission's cross-origin redirect chain (the P3B defect).

`/register` is a public page that explains the model and offers a single `Continue with Discord` anchor to `/register/discord`. When self-registration or Discord is not configured, the page still renders (so the link never 404s) but shows "Self-registration is not enabled; ask an officer for an account" and no OAuth anchor.

### 5.2 Flow

```
GET /login ──▶ Create account ──▶ GET /register ──▶ GET /register/discord
                                                          │ (state cookie, 303)
                                                          ▼
                                               Discord consent (identify)
                                                          │
GET /register/discord/callback ◀──────────────────────────┘
   │ verify state (scope=register, cookie double-submit, TTL)
   │ exchange code (register redirect URI) → GET /users/@me
   │ GET /users/@me/guilds  (only when DiscordGuildId is set)
   │ DiscordUserIDOwner? ── yes ──▶ /login?notice=registration-exists
   │ no
   │ issue signed registration ticket (cookie, 10 min)
   ▼
GET /register/complete  (username form, Discord identity shown, membership outcome)
   │
POST /register/complete (CSRF + ticket + username validation)
   │
   ├─ member  ──▶ create Approved  ──▶ IssueSession ──▶ 303 /
   └─ else    ──▶ create Pending   ──▶ 303 /login?notice=registration-pending
```

### 5.3 Routes

| Method | Path | Class | Behaviour |
|---|---|---|---|
| `GET` | `/register` | `classPublic` | Landing page; no Discord call |
| `GET` | `/register/discord` | `classPublic` | Validate `DiscordConfig.Ready()` + `SelfRegistrationEnabled`; mint state; `303` to Discord |
| `GET` | `/register/discord/callback` | `classPublic` | Verify state; exchange code; guild check; ticket; `303 /register/complete` or `303 /login?notice=…` |
| `GET` | `/register/complete` | `classPublic` | Render the username form from a valid ticket; otherwise `303 /register?notice=registration-expired` |
| `POST` | `/register/complete` | `classPublic` | Create the account; `303 /` or `303 /login?notice=registration-pending`; `409`/`422` re-render the form |
| `POST` | `/admin/users/:id/approve` | `classAdmin` | HTML form adapter |
| `POST` | `/api/v1/users/:id/approve` | `classAdmin` | JSON client adapter |

`/register*` is deliberately **not** exempt from `SetupGate`: during first run a visitor is redirected to `/setup`, and after setup the routes work. `csrf.middleware()` on the public group supplies the token for `POST /register/complete`; the three GETs stay token-free like `/auth/discord`.

Both handlers must be added to `routeMatrix` in `authorization_matrix_test.go` or the enforcement sweep fails the build.

### 5.4 Layering

| Layer | Responsibility |
|---|---|
| `api/register.go` (new) | Pages, state/ticket cookies, redirects, fixed notice codes, CSRF, session issuance for the approved path |
| `service/registration.go` (new) | Config gate, code exchange → profile → guild check, identity-ownership check, account creation, audit |
| `repository/sqlite` | `CreateRegisteredUser` (one transaction), `DiscordUserIDOwner`, `SetUserAccountStatus` |
| `domain` | `AccountStatus`, `RegistrationInput` validation, `RegistrationTicket` value type |

```go
// service/registration.go (proposed surface)
type RegistrationService struct { /* settings, DiscordAPI, store, audit, logger, limiter, now */ }

func (s *RegistrationService) Available(ctx context.Context) (bool, error)               // enabled + Discord ready
func (s *RegistrationService) Begin(ctx context.Context, state string) (string, error)   // authorize URL
func (s *RegistrationService) Verify(ctx context.Context, code, correlationID string) (domain.RegistrationTicket, error)
func (s *RegistrationService) Complete(ctx context.Context, ticket domain.RegistrationTicket,
    userName, correlationID, sourceIP string) (domain.User, error)
```

`RegistrationService` holds **no** `SessionIssuer`. Registration creates accounts, never credentials; `api/register.go` calls `deps.Auth.IssueSession` only when `created.IsEnabled()`. That is a stronger invariant than today's `DiscordService.Login`, and it is testable.

### 5.5 Outcomes, mapped to fixed text

| Condition | Result |
|---|---|
| Consent denied (`?error=`) | `303 /login?notice=registration-cancelled` |
| Bad/expired/replayed state, cookie mismatch | `303 /login?notice=registration-state` |
| Exchange or profile call fails, or profile has no ID | `303 /login?notice=registration-provider` |
| Discord identity already owns a **pending** account | `303 /login?notice=registration-pending` |
| Discord identity already owns an account in any other state | `303 /login?notice=registration-exists` — "Sign in instead; if this is not your account, contact an officer" |
| Duplicate username on submit | `409` re-render with a `userName` field message; the ticket stays valid so the user can pick another name |
| Invalid username | `422` re-render with the existing `ValidateUserName` messages |
| Ticket missing/expired on submit | `303 /register?notice=registration-expired` |
| Rate limited | `429` + `Retry-After`, fixed text |
| Self-registration disabled mid-flight | `303 /login?notice=registration-unavailable` |

Every code goes through a fixed-text mapper (mirroring `discordLoginNotice`); no query value or provider text is ever reflected.

**Registration never authenticates an existing account.** When `DiscordUserIDOwner` finds an owner, the flow stops. It does not issue a session for that account even though the identity is proven: that would create a second authentication entry point outside `/auth/discord` and its limiter, and would blur "registration" and "sign-in" in the audit trail.

### 5.6 Concurrency and idempotency

- Two tabs, same Discord identity, two usernames: `ux_users_discord` rejects the second `CreateRegisteredUser`. The loser sees `registration-exists` (the ticket is cleared), so no half-created second account and no FK repair.
- Two tabs, different identities, same username: `ux_users_username` rejects one with a `userName` field message.
- Double-submit of the same form: the ticket cookie is cleared before the redirect and the request runs in one transaction, so the second submit finds no ticket and is redirected to `/register?notice=registration-expired`.
- A crash between the code exchange and the ticket leaves no row: nothing is written until `POST /register/complete`.

### 5.7 Audit events

| Event | Table / record | newValues |
|---|---|---|
| `USER_SELF_REGISTERED` | `Users` / user ID | `userName`, `accessLevel=Member`, `accountStatus`, `discordUserId`, `discordUserName`, `guildId`, `guildCheck=member|not-member|not-configured|error` |
| `USER_APPROVED` | `Users` / user ID | `accountStatus` before/after, `origin` |
| `USER_APPROVAL_WITHDRAWN` | `Users` / user ID | `accountStatus` before/after, `sessionsRevoked` |

`USER_SELF_REGISTERED` uses the new account as the actor (`domain.Actor{UserID: newID, UserName: username, Role: Member}`), plus `sourceIP` and the request correlation ID. It is written after the SQLite commit through the existing best-effort `writeAudit`, so an audit outage can never block a registration.

---

## 6. Discord guild-verification workflow

### 6.1 Configuration and portal registrations

| Requirement | Detail |
|---|---|
| `DiscordEnabled=true`, client ID, client secret, `PublicBaseUrl` | Already required by `DiscordConfig.Ready()` |
| `DiscordGuildId` | Required for automatic approval; when empty every registration is pending |
| Third redirect URI | `PublicBaseUrl + /register/discord/callback`, added to the Discord developer portal alongside `/account/discord/callback` and `/auth/discord/callback` |
| Scope | **`guilds` is required, confirmed against Discord's documentation (§6.5).** Link and sign-in keep `identify`; registration must request `identify guilds`, because `GET /users/@me/guilds` states "For OAuth2, requires the `guilds` scope" |
| New constant | `DiscordRegisterCallbackPath = "/register/discord/callback"`, plus `DiscordConfig.RegisterRedirectURI()` and `DiscordScopeRegister = "identify guilds"` (the OAuth scope string; the signed state value that separates flows is a different `register` string) |

A missing portal registration fails at Discord with `invalid_redirect_uri` before any code reaches GuildBank; this is a deployment checklist item, not a code path. Reusing `/auth/discord/callback` instead was rejected: the existing design deliberately gives every flow its own callback so a state minted for one flow can never be replayed as another.

### 6.2 Decision table

| Guild ID | `GuildMember` result | Account status | Reason code |
|---|---|---|---|
| set | `(true, nil)` | `Approved` | `member` |
| set | `(false, nil)` | `PendingApproval` | `not-member` |
| set | `(false, err)` (401/403/5xx/timeout) | `PendingApproval` | `error` |
| empty | not called | `PendingApproval` | `not-configured` |

**Fail closed.** Three rules make this unambiguous:

1. Only a definitive `true` with no error approves.
2. Any error is `unverified`, never `not a member`; the UI and audit say "membership could not be verified", not "not in the guild".
3. An empty `DiscordGuildId` never approves, because "no check configured" is not "member". The registration page states which outcome will apply *before* the user consents, so a pending account is never a surprise.

### 6.3 What is stored

- `GuildVerifiedAt` is set only on a definitive `true` (recommended column, §4.2).
- `Users.DiscordUserID/UserName/LinkedAt` are written in the same transaction as the row.
- The guild ID and the outcome are recorded in the audit payload; the Discord *access token* is never persisted (unchanged).

### 6.4 Relationship to the existing link flow

`DiscordService.Link` keeps its current behaviour: membership is checked, the result is audited, and linking is **not** blocked by a failed check. Changing that is out of scope; a linked-but-unverified account is visible on the profile page exactly as today. The only shared code is `DiscordAPI.GuildMember`; no refactor of `Link` is required.

**Scope isolation (D-SR-13, owner decision 2026-11-26).** The existing implementation is left exactly as it ships. `DiscordScope` stays `"identify"` and remains the scope for both `/account/discord/link` and `/auth/discord`; only the registration entry point requests `DiscordScopeRegister = "identify guilds"`. Ordinary login and linking are never widened — not to fix the guild check, not as a convenience, and not as a "while we are here" change. The `403` described in §6.5 therefore stays a known, accepted limitation of the link flow; the registration flow supplies the guild signal instead. `authorizeURL` gains a `scope` parameter and each of the three callers passes its own constant explicitly, so no caller can inherit another flow's scope by default.

### 6.5 Scope requirement — confirmed (resolved 2026-11-26)

Checked against Discord's published reference rather than assumed. Two verbatim statements settle it:

- `GET /users/@me/guilds`: *"Returns a list of partial guild objects the current user is a member of. **For OAuth2, requires the `guilds` scope.**"*
- Scope table: `identify` *"allows `/users/@me` without `email`"*; `guilds` *"allows `/users/@me/guilds` to return basic information about all of a user's guilds"*. (`guilds.members.read` covers the single-member endpoint and is not needed here.)

**Answers**

| Question | Answer |
|---|---|
| Is `identify` sufficient? | **No.** It grants `/users/@me` only |
| Is `guilds` required? | **Yes**, for `GET /users/@me/guilds`. It is a standard scope and is **not** one of the approval-gated scopes |
| Does the existing plumbing work with current scopes? | **The code works; the scope does not.** See below |

**What happens today.** `DiscordScope = "identify"` and `authorizeURL` hardcodes it, so in production `Client.GuildMember` is expected to get `403` (Missing Access) — the documented consequence of the missing scope, pinned down in the live check below; `do` turns that into an error; `DiscordService.Link` logs `discord guild check failed` and audits `guildVerified=false`. Linking still succeeds — Phase B degrades to "never verified" for every user, including members, rather than failing. `client_test.go` serves 200 for `/users/@me/guilds` and never inspects the scope, so no test can catch this.

**Consequences for this design**

1. Registration must consent with `identify guilds`; parameterise `authorizeURL` per flow. Link and sign-in keep `identify`, so a re-link never asks for more than the original consent.
2. A wrong or missing `guilds` scope is indistinguishable from "not a member" in the current audit payload; the `error` reason code (§6.2) is what makes it visible.
3. No token is stored (only the identity), so existing linked accounts cannot be re-verified without a fresh consent. Out of scope.
4. `url.Values.Encode()` renders the space as `+` (`scope=identify+guilds`). Confirm Discord accepts it during the live check; if not, emit `%20`.
5. Alternative considered: `guilds.members.read` with `GET /users/@me/guilds/{guild.id}/member` answers exactly "is this user in this guild" without granting the full guild list. Rejected for now — a non-member has no member object, so the answer arrives as an error rather than a `false`, and `GuildMember` over the list is already implemented and tested.

**Live check still owed by the owner (needs the real app credentials; not possible from the repository alone)**

1. Consent once with `identify guilds`, call `GET /users/@me/guilds` with that token, confirm the configured guild ID appears.
2. Consent once with `identify` only, confirm the `403`, and confirm the link flow records `guildVerified=false` without failing.
3. Record both results here before S3.

---

## 7. Username selection workflow

### 7.1 Recommendation: **B — after Discord OAuth succeeds**

The username is chosen on `GET /register/complete`, which is reachable only with a valid signed ticket produced by a successful Discord consent.

Reasons, in order of weight:

1. **Nothing is written before identity is proven.** Option A needs either an anonymous pre-registration row or a second reservation table with its own expiry and cleanup. Both are writable by anyone on the internet, and both must be policed against squatting.
2. **Uniqueness can be enforced exactly once.** With B, the username is inserted in the same transaction as the Discord identity, so `ux_users_username` and `ux_users_discord` decide every race. With A, the name is "reserved" first and can still be lost (or squatted) between reservation and completion.
3. **The check is already rate-limited and consented.** Under B the availability check happens after OAuth, so anonymous username enumeration over the registration path is not possible at all.
4. **Better default.** The Discord display name is known at that point and can be offered as a prefilled suggestion, which is the main UX benefit option A was meant to provide.
5. **Failure costs one page, not a second consent.** If the username is taken, the ticket is still valid for its remaining TTL, so the form is simply re-rendered — no round trip to Discord.

### 7.2 The registration ticket

A dedicated signer, not the OAuth state manager: the payload is different and mixing them would make the state format do two jobs.

```go
// api/register_ticket.go (proposed)
type registrationTicket struct {
    DiscordUserID   string
    DiscordUserName string
    GuildCheck      string // "member" | "not-member" | "not-configured" | "error"
    GuildID         string
    IssuedAt        time.Time
}
```

| Property | Value |
|---|---|
| Format | `base64url(payload) + "." + base64url(HMAC-SHA256(payload))`, payload `v1|discordUserID|guildCheck|guildID|issuedAtUnix|expiryUnix|nonce|base64(displayName)` |
| Secret | 32 random bytes at process start (process-local, same lifecycle rule as `discordStateManager`: a restart abandons in-flight registrations, which is safe) |
| Cookie | `guildbank_register_ticket`, `HttpOnly`, `SameSite=Lax`, `Secure` when configured, `Path=/`, `Max-Age=600` |
| Transport | Cookie only. The ticket is never placed in a URL, form field or log |
| Consumption | Cleared on successful creation and on every invalid/expired read; a validation failure (`409`/`422`) deliberately keeps it |
| Binding | The cookie itself is the bearer; CSRF on the POST supplies the cross-site protection |

Because the identity is inside the signed cookie, the form carries no hidden identity fields at all. A user cannot change which Discord account the page is registering for by editing the DOM, and the server cannot be tricked into writing an identity the browser did not receive from Discord.

### 7.3 Validation, uniqueness and messages

| Rule | Source | Message surface |
|---|---|---|
| Trim, valid UTF-8, non-blank, ≤64 runes, no control characters | existing `domain.ValidateUserName` | `422` field `userName` |
| Case-insensitive uniqueness | `ux_users_username` (`COLLATE NOCASE`) → `ErrAlreadyExists` | `409` field `userName` — "that username is already in use" (identical text to the admin form) |
| Discord identity uniqueness | `ux_users_discord` → `ErrAlreadyExists` | redirect `registration-exists` |
| No email, no password | by design | — |

The username is the only field on the form. `accessLevel`, `accountStatus` and `origin` are server-set constants; the handler must not bind them from the request, and the service must ignore any such field if a JSON client sends one (the JSON path, if offered, uses a strict decoder like `decodeAdminJSON`).

### 7.4 Username suggestions (optional, phase S4)

A "Use <Discord display name>" helper button that fills the input client-side (no script needed: a second submit button `name="suggested" value="1"` that re-renders with the suggestion pre-filled). The stored value is never auto-derived: uniqueness and ownership decisions stay visible to the person choosing the name.

### 7.5 Why option A (before OAuth) is rejected

| Concern | Option A | Option B |
|---|---|---|
| Writable by unauthenticated visitors | Yes (reservation row/table) | No |
| Squatting surface | Usernames reserved without proving Discord identity | None |
| Cleanup | Needs TTL, sweeper, partial-index complexity | None |
| Race between reservation and completion | Two-phase, needs re-check | Single transaction |
| User effort on a taken name | Abandon and restart | Re-render the same page |
| Default suggestion | Not available | Discord display name is known |

Option A is only preferable if the product later wants to hold a username *before* Discord is configured at all. That is not this feature.

---

## 8. Administration UI changes

### 8.1 `/admin/users` (list)

| Change | Detail |
|---|---|
| Status column | `Active` / `Pending approval` / `Disabled` / `Pending approval · disabled`, plus `· must change password` and `· last administrator` where they apply |
| Pending banner | `N account(s) await approval. <a href="/admin/users?status=pending">Review</a>` in an `alert-warn` block |
| Filter | `?status=pending|approved|disabled|all` combined with the existing `?q=` name filter; pending sorts by `CreatedAt` ascending so the oldest request is first |
| Origin column (if `Origin` ships) | `Self-registered` / `Created by officer` as muted text, no new table column |
| Row action | Pending rows link to the detail page where the Approve button lives; no inline approve in the list, so the action is always one identifiable request |

`usersHandler.page` builds `userRowView` from `userRowViewOf`; that helper gains `Pending`, `GuildVerifiedAtText` and `DiscordUserName`. The pending count comes from a small `CountPending` service method (single indexed query), the same way `EnabledAdmins` is computed today.

### 8.2 `/admin/users/:id` (detail)

Insert above "Role and status" when `.Row.Pending`:

```html
<h2>Approval</h2>
<p class="muted">
  Self-registered {{.Row.CreatedText}}. Discord: {{.Row.DiscordUserName}}.
  {{if .Row.GuildVerifiedAtText}}Guild membership verified {{.Row.GuildVerifiedAtText}}.
  {{else}}Guild membership was not verified at registration.{{end}}
</p>
<form method="post" action="/admin/users/{{.Row.ID}}/approve" class="inline-form">
  <input type="hidden" name="csrf_token" value="{{.CSRF}}">
  <button type="submit" class="primary">Approve account</button>
</form>
```

The existing "Role and status" form keeps only `active` / `disabled`; for a pending account the role select is disabled with the help text "Approve the account before assigning a role." Withdrawing approval from an approved account is a separate, clearly labelled action ("Withdraw approval"), never a silent side effect of the status select.

### 8.3 Approve handler

```go
// usersHandler.approve (HTML) and .approveAPI (JSON)
func (h *usersHandler) approve(c *gin.Context) {
    id, ok := parseAdminID(c, false); if !ok { return }
    actor, ok := adminActor(c); if !ok { return }
    if _, err := h.users.Approve(ctx, id, actor, GetRequestID(c), c.ClientIP()); err != nil {
        h.fail(c, err); return
    }
    if wantsHTML(c) {
        c.Redirect(http.StatusSeeOther,
            "/admin/users/"+strconv.FormatInt(id, 10)+"?notice=approved")
        return
    }
    // JSON: 200 {"data":{"user": {...}}}
}
```

`UserAdminService.Approve` validates `actor.IsAdmin`, loads the target, returns the row unchanged when already approved (idempotent), calls `SetUserAccountStatus`, and records `USER_APPROVED`. New detail notice: `approved` → "The account was approved. The user can now sign in."

### 8.4 JSON API contract

`userResponse` (`internal/api/users.go:27`) gains:

```go
AccountStatus   string     `json:"accountStatus"` // Approved | PendingApproval
Pending         bool       `json:"pending"`
Origin          string     `json:"origin,omitempty"`
GuildVerifiedAt *time.Time `json:"guildVerifiedAt,omitempty"`
```

Additive fields keep existing clients working. `GET /api/v1/users?status=pending` filters; `POST /api/v1/users/:id/approve` is preferred over `PUT` with `accountStatus`, because the action is explicit and each call maps to exactly one audit event. `Create` continues to reject any client-supplied status: `createUserRequest` has no such field and the service hardcodes `Approved`.

### 8.5 Navigation

`partials.html` renders the Administration menu for Administrators. If the pending count is cheaply available (one indexed `COUNT`), append a small badge: `Accounts (2 pending)`. This is optional; the banner on `/admin/users` is the required surface.

### 8.6 Settings

New key in `service/settings.go` (the canonical registry that drives validation and the settings page):

```go
{Key: "SelfRegistrationEnabled", Label: "Allow self-registration", Type: domain.SettingBoolean,
 Default: "false", Help: "Lets visitors create a Member account with Discord. Accounts are approved automatically only when Discord guild membership is verified."}
```

| Aspect | Decision |
|---|---|
| Type | Existing `SettingBoolean`, rendered as a select like `DiscordEnabled` |
| Default | `false` — an existing deployment does not silently open registration on upgrade |
| Availability rule | `SelfRegistrationEnabled && DiscordConfig.Ready()`; both must hold or `/register` shows the unavailable text |
| Validation | Existing boolean parsing; no new rules |
| Guild dependency | Enabling the flag with an empty `DiscordGuildId` is **allowed but warned about** ("every self-registration will be pending until a guild ID is set"), because the safe outcome is still correct |

---

## 9. Security considerations

| # | Threat | Control |
|---|---|---|
| 1 | Cross-site request forgery on registration | `csrf.middleware()` on the public group; `POST /register/complete` requires the token rendered into the form. GET starts are side-effect-free apart from a state cookie, exactly like `/auth/discord` |
| 2 | OAuth login/registration confusion | Separate scope (`register`), separate cookie (`guildbank_discord_register_state`), separate callback path, scope inside the HMAC payload. A state minted for link or sign-in cannot be replayed here, and vice versa |
| 3 | Code replay / state tampering | HMAC-SHA256 over the payload, cookie double-submit, 10-minute TTL, single use (cookie cleared), process-local secret |
| 4 | Client-supplied identity or privilege | The Discord identity travels only inside the signed ticket; `accessLevel`, `accountStatus` and `origin` are server constants and are never bound from the request |
| 5 | Two accounts for one Discord user | `ux_users_discord` partial unique index is the authority; the pre-check only produces a nicer message |
| 6 | Username squatting / duplicate names | Case-insensitive unique index, validated before insert, `409` with a field message; nothing is reserved before OAuth |
| 7 | Pending account authenticating | `IsEnabled()` now includes approval and is already called by `Login`, `issueSession`, `Authenticate` and `DiscordService.Login`; `Approved → PendingApproval` deletes sessions in the same transaction |
| 8 | Privilege escalation through approval | Registration hardcodes `Member`; role changes are refused while pending; approval never assigns a role; no automatic Officer/Admin exists anywhere in the flow |
| 9 | Fail-open guild check | Only `(true, nil)` approves; errors and a missing `DiscordGuildId` produce pending. No retry loop that could be abused into approving |
| 10 | Mass account creation / storage abuse | Per-IP limits on `/register/discord` and `POST /register/complete` (reusing `LoginLimiter` semantics, e.g. 20 starts/hour and 10 completed registrations/hour); optional global pending cap (D-SR-6) |
| 11 | Username or by-identity enumeration | Availability is revealed only after Discord consent; `registration-exists` is shown only to the person who just proved control of that Discord account; no anonymous lookup endpoint |
| 12 | Account takeover by unlink/relink | Registration never links to an existing account. Unlinking stays a signed-in, session-bound action; an account with neither a password nor Discord can only be recovered by an Administrator reset |
| 13 | Credential leakage in logs | Codes and tokens are never logged (existing convention); the ticket is cookie-only; audit records the Discord user ID/name, which are already stored |
| 14 | Open redirect | `next` continues to pass `safeNext`/`authorizedNext`; the registration flow carries no `next` at all in v1 |
| 15 | CSP regressions | Every OAuth start is an `<a>`; the form posts only to same-origin `/register/complete`; mirror `TestProfileDiscordSectionFollowsSettings` with a guard asserting no form targets `/register/discord` |
| 16 | Session fixation | The session is created after the account write by `IssueSession`, which mints a fresh id and token; registration sets no pre-authentication session |

**Deliberate non-goals:** no captcha (rate limits plus Discord consent already cost real work), no email, no self-service password recovery, no Discord role mapping.

---

## 10. Migration requirements

### 10.1 Database

| Item | Detail |
|---|---|
| SQLite | `0004_self_registration.sql` (§4.1) plus the optional columns in §4.2. Forward-only, one transaction per file (`migrate.go` semantics), `PRAGMA user_version` becomes 4 |
| Compatibility | Additive only; no rebuild, no session cascade, no downtime beyond the normal restart |
| Existing data | Every existing account reads `Approved`; no backfill statement and no data transformation |
| Rollback | There is no down-migration. Operational rollback is restoring the pre-upgrade SQLite file from backup; the previous binary refuses `user_version > 3`, which is the intended forward-only guard. Losing the `AccountStatus` history on rollback is acceptable because it defaults to `Approved` |
| MariaDB | No change (§4.5) |
| Tests | New `0004` migration test (§4.1 verification list) plus a `store` test proving `CreateRegisteredUser` is atomic and that the two unique indexes produce `ErrAlreadyExists` |

### 10.2 Configuration

1. Add `/register/discord/callback` to the Discord developer portal redirect URIs (deployment checklist, documented in README and ProjectStatus).
2. Registration consent must request `identify guilds` (§6.5). `guilds` needs no Discord approval, so no application request is required — but the consent screen gains "access your server list", which is why link and sign-in keep `identify` alone.
3. Set `DiscordGuildId` before enabling `SelfRegistrationEnabled`, so new registrations are approved automatically.
4. Confirm `PublicBaseUrl` is the canonical origin; the third callback is built from it.

### 10.3 Documentation updates (after implementation, not in this proposal)

| Document | Change |
|---|---|
| `Docs/DatabaseDesign.md` | §3.1 `Users` columns; §7.7 creation path; new §7.10 "Self-registration and approval" |
| `README.md` | Route table rows for `/register*`, `/admin/users/:id/approve`, `/api/v1/users/:id/approve` |
| `Docs/ProjectStatus.md` | New section for this phase + changelog row; note the third portal registration |
| `Docs/ProjectStructure.md` | `api/register.go`, `service/registration.go`, `0004_self_registration.sql` |

---

## 11. Future request-system integration

| Requirement | How this design supports it |
|---|---|
| Requests pick from the user's own character names | `UserCharacters` already exists and is unchanged; approval gates whether an account may populate it |
| Character names must identify a real, admitted member | Pending accounts cannot add names. `CharacterService.Add` already receives `domain.User`; add an explicit `if !user.IsEnabled() { return domain.ErrForbidden }` so the rule is enforced in the service, not only by the absence of a session. The profile page hides the add form for an unapproved account |
| No name squatting before admission | Because names require approval, a pending registrant cannot reserve a globally unique character name that a real requester needs |
| Requests need an authenticated requester | `RequireAuth` + `RequireRole(Member)` already refuse a pending account once `IsEnabled()` includes approval |
| Audit/history must survive account changes | Account creation and approval are audited with the username snapshot; request rows will keep their own `RequesterName` snapshot, so a later rename or deletion does not break history |
| Discord identity as a future notification channel | `DiscordUserID` is stored at registration, so a future "notify the requester on Discord" feature needs no new identity plumbing |
| Re-verification or membership expiry | Not implemented here; `GuildVerifiedAt` is the field a future job would read. A future job must only *flag for review*, never delete accounts, because requests will reference them |

Two consequences worth stating explicitly:

1. **Approval is the moment a person becomes a requester.** The officer queue is therefore also the "who may request" queue; the admin UI text should say so ("Approving lets this account add character names and raise requests").
2. **Pending accounts hold no unique names.** This keeps the globally unique character-name space clean (D1 decision) and avoids officer work cleaning up squatted names.

---

## 12. Recommended implementation phases

| Phase | Scope | Exit criteria |
|---|---|---|
| **S0 — decisions** | Owner answers §14 (at minimum D-SR-1, 2, 3, 7). No code | Decisions recorded in this document |
| **S1 — schema and status model** | `0004` migration (+ optional columns), `AccountStatus`, `IsEnabled` redefinition, `userColumns`/`scanUser`/`sessionSelect`, `CreateUser` sets Approved, `SetUserAccountStatus`, pending refuses role change | Migration test green; every existing auth test still passes; a pending account cannot sign in by password or Discord; no session survives a transition to pending |
| **S2 — settings and gating** | `SelfRegistrationEnabled` key, validation, settings page copy; `DiscordRegisterCallbackPath`, `RegisterRedirectURI()`, `DiscordScopeRegister` | Settings round-trip test; disabling the flag makes every registration route answer the unavailable path |
| **S3 — registration flow** | `api/register.go`, `api/register_ticket.go`, `service/registration.go`, `sqlite.CreateRegisteredUser`, login-page "Create account" anchor, `/register` page | End-to-end: member → approved + signed in; non-member → pending + no session; existing identity → `registration-exists`; duplicate username → `409` and form re-render; state/ticket misuse → fixed notices |
| **S4 — administration** | Approve action (HTML + JSON), status display, pending filter/banner, detail-page approval block, notices, `userResponse` fields | Approve is idempotent and audited; pending counts correct; non-admin gets `403`; route matrix updated |
| **S5 — hardening and docs** | Rate limiting, pending cap decision, README/ProjectStatus/DatabaseDesign/ProjectStructure, manual browser pass | CSP-clean consent redirect observed in a real browser; two consecutive registrations from one IP behave sanely; docs reviewed |

Each phase is independently shippable: S1 changes no user-visible behaviour, S2 adds a setting nobody can reach, S3 adds the flow behind the setting, S4 makes pending accounts actionable. Shipping S3 before S4 would create pending accounts nobody can approve — so S3 and S4 ship together, or S3 ships with the flag left `false`.

---

## 13. Test plan (design level)

| Area | Test | Asserts |
|---|---|---|
| Migration | `TestMigration0004AddsAccountStatus` | existing rows read `Approved`; `user_version = 4`; `Sessions` intact; CHECK rejects an unknown value |
| Domain | `TestIsEnabledRequiresApproval` | `Approved+enabled` true; pending false; disabled false; blank status reads approved; unknown reads pending |
| Auth | `TestLoginRejectsPendingAccount` | password login of a pending account returns the same generic `401` as a wrong password and writes no session |
| Auth | `TestSessionRejectedWhenApprovalWithdrawn` | a live session stops authenticating after the account becomes pending and the row is deleted |
| Discord login | `TestDiscordLoginRejectsPendingAccount` | neutral notice, no session, nothing written |
| Store | `TestCreateRegisteredUserIsAtomic` | identity and row appear together; a forced identity failure leaves no user row; duplicate username/identity → `ErrAlreadyExists` |
| Service | `TestRegistrationGuildDecisionTable` | the four rows of §6.2 produce approved/approved/pending/pending with the matching reason codes |
| Service | `TestRegistrationNeverCreatesSession` | `RegistrationService` has no issuer and `Complete` returns a `User` only |
| API | `TestRegisterFlowApprovedMember` | state → callback → ticket → complete creates an approved Member, sets a session cookie and redirects `/` |
| API | `TestRegisterFlowPendingMember` | non-member creates a pending account, no session cookie, `303 /login?notice=registration-pending` |
| API | `TestRegisterRejectsReplayedState` | reusing a state/callback redirects with `registration-state` and writes nothing |
| API | `TestRegisterTicketIsCookieOnly` | the completion page has no hidden identity fields; a hand-crafted form without the cookie is refused |
| API | `TestRegisterDuplicateUsernameRerenders` | `409` + `userName` field message; the ticket survives so a second submit can succeed |
| API | `TestRegisterStartIsAnchorNotForm` | the login and register pages contain no form targeting `/register/discord` (CSP guard, mirroring the P3B test) |
| API | `TestRegistrationDisabledRedirects` | with the flag off, or Discord unready, every registration route answers the unavailable path |
| Admin | `TestApproveIsIdempotentAndAudited` | a second approve is a no-op; one `USER_APPROVED` row; `ModBy` is the actor |
| Admin | `TestPendingRoleChangeRefused` | `PUT /api/v1/users/:id` with `accessLevel=Officer` on a pending account → `409`, role unchanged |
| Admin | `TestPendingFilterAndCount` | `?status=pending` lists only pending, oldest first; the banner count matches |
| API | `TestAuthorizeScopeIsPerFlow` | the three authorize URLs carry exactly `scope=identify` (link), `scope=identify` (sign-in) and `scope=identify%20guilds`/`identify+guilds` (registration); no flow can inherit another's scope |
| Route matrix | existing `authorization_matrix_test.go` + enforcement sweep | new routes registered and classified; public and admin sweeps pass |
| Manual | browser pass | consent redirect completes with no CSP console error; approved user lands signed in; pending user sees the pending notice; an officer approves; the new member signs in |

---

## 14. Remaining owner decisions

| ID | Decision | Options | Recommendation |
|---|---|---|---|
| **D-SR-1** | Is self-registration on or off by default, and is one setting enough? | (a) `SelfRegistrationEnabled` default `false`; (b) default `true`; (c) no setting, always on when Discord is configured | **(a)** — an upgrade must not silently open account creation; the flag also gives a one-click kill switch |
| **D-SR-2** | Who may approve a pending account? | (a) Administrators only; (b) Officers approve Members, Admins approve anything | **(a)** for the first release — same trust weight as account creation today; revisit if the queue becomes a bottleneck |
| **D-SR-3** | Username before or after OAuth? | (a) before, with a reservation table; (b) after, on a signed ticket | **(b)** (§7) |
| **D-SR-4** | What happens when `DiscordGuildId` is empty? | (a) all registrations pending; (b) refuse registration; (c) auto-approve | **(a)** — fail closed and still useful; (c) is unacceptable |
| **D-SR-5** | Stored value `PendingApproval` with display label `Pending approval`, or the reverse? | naming only | stored `PendingApproval`, display `Pending approval` — the stored value mirrors the state machine |
| **D-SR-6** | Is a global cap on pending accounts needed? | (a) none; (b) configurable cap (default e.g. 250) that blocks new registrations with a fixed message | **(a)** initially; add (b) only if abuse is observed or the guild is public-facing |
| **D-SR-7** | May a self-registered user set a password at registration? | (a) no — Discord-only, admin reset for password access; (b) optional password field on the completion form | **(a)** — one field, one credential; (b) is a small later addition if members ask for password access |
| **D-SR-8** | Do pending accounts expire? | (a) never; (b) auto-delete after N days (setting, default off) | **(a)** with age shown in the queue; expiry silently discards a request an officer may still want |
| **D-SR-9** | Is a separate "Reject with reason" action needed? | (a) no, delete is the rejection; (b) reject stores a reason and keeps the row | **(a)** now; (b) only if officers need rejection statistics |
| **D-SR-10** | Should `Origin` and `GuildVerifiedAt` ship in `0004`? | (a) yes, additive nullable columns; (b) defer and derive from audit | **(a)** — the queue is hard to use without them and a later migration costs more |
| **D-SR-11** | Show "Create account" when self-registration is enabled but Discord is misconfigured? | (a) hide it; (b) show it and explain the unavailability | **(b)** — a visible explanation produces fewer support messages than a missing button |
| **D-SR-12** | Is Officer approval of *character names* (not accounts) in scope later? | (a) no; (b) yes, an approval flag on `UserCharacters` | **(a)** now; the request system may want its own review step |
| **D-SR-13** | Which OAuth scopes does each flow request? | (a) every flow requests `identify guilds`; (b) registration requests `identify guilds`, login and linking keep `identify` | **(b) — decided 2026-11-26.** The finding in §6.5 is accepted, and the current link/login implementation stays unchanged. Only the dedicated registration flow asks for the guild list; ordinary login and linking are never broadened (§6.4) |
| **D-SR-14** | May Discord sign-in tell a pending or disabled account *why* it cannot sign in? | (a) keep one neutral message for unlinked, pending and disabled; (b) distinguish pending and disabled with fixed text | **(b) — decided 2026-11-26.** The caller has just proved control of that Discord identity, and `/register` already distinguishes `registration-pending` from `registration-exists` for the same identity, so (a) hides nothing an attacker does not already know while sending real members to a password form they cannot use. Password sign-in stays neutral |

---

## 15. Acceptance criteria

1. The login page shows `Sign in`, `Sign in with Discord` (when configured) and `Create account` (when enabled), and every OAuth entry is an `<a>` navigation.
2. A Discord user who is a member of the configured guild can create an account and is signed in immediately as an approved `Member`, with the Discord identity already linked.
3. A Discord user who is not verifiably a member can create an account that is `PendingApproval`; it cannot sign in by password or Discord, and it holds no character names.
4. A Discord identity that already owns an account can never create a second one; the flow redirects to sign-in with fixed text.
5. An administrator sees pending accounts in `/admin/users`, can approve one, and the approval is audited and idempotent; the approved user can then sign in.
6. Usernames remain case-insensitively unique; duplicates produce a field-level message without losing the verified Discord identity.
7. Existing Administrator-created accounts, passwords, sessions and roles are unchanged by the migration, and nobody is signed out by it.
8. `go build ./...`, `go vet ./...`, `gofmt -l .` are clean; all existing suites pass; the route matrix classifies every new route; the new migration and registration suites pass.
9. Documentation (README, DatabaseDesign, ProjectStructure, ProjectStatus) reflects the delivered behaviour, including the third Discord redirect URI.
10. Scope isolation holds: `/auth/discord` and `/account/discord/link` still request `identify` and nothing more, `/register/discord` requests `identify guilds`, and `TestAuthorizeScopeIsPerFlow` pins all three (D-SR-13). The Phase B guild check keeps its current behaviour, `403` included.

---

## 16. Open risks and mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Portal redirect URI not registered | Consent fails with `invalid_redirect_uri`; recovery is a config change, not a code change | Explicit deployment checklist (§10.2); the callback derives from `PublicBaseUrl`, so a wrong origin is visible immediately |
| `DiscordGuildId` left empty | Every registration is pending, which looks like the feature is broken | Settings page warning; `/register` states the outcome before consent; the pending banner makes the queue obvious |
| `GET /users/@me/guilds` unavailable or rate-limited | Registrations become pending even for members | Fail-closed but recoverable: an officer approves. Never retries into an approval |
| Guild list refuses an `identify`-only token (`403`) | Auto-approval can never trigger, and today's link flow silently records `guildVerified=false` for members | **Confirmed, not hypothetical** (§6.5): registration consent must request `identify guilds`; the `error` reason code keeps the deployment failure visible instead of mistaking it for "not a member" |
| Pending queue ignored | Members wait indefinitely; rows accumulate | Banner, `/admin/users?status=pending`, age display; owner decision D-SR-8 covers expiry |
| Zero-value `domain.User` in tests or partial code | `IsEnabled()` would read false if blank meant pending | `ParseAccountStatus("")` returns `Approved` (§2.2); the migration also guarantees real rows are never blank |
| A future column added to `Users` without updating the session join | Authenticated requests see a zero status and lock everyone out | §4.3 names all four touch points; the S1 test suite exercises login end to end |
| Administrators forget they can now also create accounts | Two creation paths drift | `CreateUser` sets `Approved`/`Origin` explicitly and both paths share `validateUserName` and the same unique indexes |

---

## 17. Implementation record (2026-11-26)

Implemented as designed, with D-SR-13 in force. Files: `domain/identity.go` (status model, redefined `IsEnabled`), `domain/registration.go` (new), `repository/sqlite/migrations/0004_self_registration.sql`, `repository/sqlite/users.go` and `sessions.go` (columns, `CreateRegisteredUser`, `SetUserAccountStatus`, `CountPending`, pending role guard), `service/registration.go` (new), `service/discord.go` (scope parameter, `RegisterRedirectURI`, `DiscordScopeRegister`), `service/user_admin.go` (`Approve`, `WithdrawApproval`, `ListStatus`, `PendingCount`), `service/settings.go` (`SelfRegistrationEnabled`), `api/register.go` and `api/register_ticket.go` (new), `api/router.go`, `api/users.go`, `api/auth.go`, `api/views.go`, `api/discord_state.go`, the templates (`register.html`, `register_complete.html`, `login.html`, `admin_users.html`, `admin_user_detail.html`, `settings.html`) and `platform/app/app.go`.

Two small additions beyond the text above, both recorded here rather than silently:

1. **An operator cannot withdraw their own approval.** The service refuses it with `409`, exactly as self-disable and self-delete are refused: withdrawing would delete the caller's own session mid-request. The first implementation omitted this and the route-enforcement sweep caught it — the `POST /api/v1/users/:id/withdraw-approval` probe targets user `1`, which is the signed-in administrator in that test.
2. **Two extra fixed notice codes:** `registration-rate` (the per-address limiter, mapped to 429 on the completion form and to fixed login text on the callback) and `registration-error` (an unexpected failure). Both follow §5.5's rule that only fixed text is ever shown.

`CharacterService.Add` also refuses a non-enabled account. A pending account holds no session, so this is defence in depth for the globally unique character-name space (§9 threat 8 and the character-profile rule in the approved model).

Verification: `gofmt -l .` empty, `go build ./...` and `go vet ./...` clean; new tests `domain/account_status_test.go`, `repository/sqlite/migration_0004_test.go`, `repository/sqlite/registration_test.go`, `service/scope_test.go` (`TestAuthorizeScopeIsPerFlow`), `service/registration_test.go`, `api/register_test.go`, `api/user_approval_test.go`; existing suites unchanged and passing.

**Follow-up, same day (D-SR-14).** Discord sign-in now names the refusal instead of always reporting an unlinked account. `domain.AccountStateError` (a typed error that unwraps to `ErrUnauthorized`) is returned by `DiscordService.Login` from the `domain.User` it already has — no second query — and `api/discord.go` maps it to the fixed notices `discord-pending` and `discord-disabled` *before* the `ErrUnauthorized` branch. The `IsEnabled()` gate itself, `discord-unlinked`, the session rule, the limiter and the password path are unchanged. Recorded in `ProjectStatus.md` §6.21.


