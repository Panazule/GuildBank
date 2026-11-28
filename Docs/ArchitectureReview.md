# Architecture Review — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | AR-001 |
| Source specification | `Design.md` v1.6.1 (authoritative; body L1–L854, decisions L855–L1161, amendments AM-01–AM-05 at L1163–L1263, informational status at L1265–L1274) |
| Review date | 2026-11-20 |
| Revision | 1.3 — 2026-11-21, incorporates v1.6/v1.6.1 decisions and the Phase 3 implementation status |
| Status | Complete — v1.6.1 amendments incorporated; Phases 0–3 implemented; B-5 is an operational follow-up (loader/grants), B-6 remains partial only for offline setup recovery |
| Scope | Layered architecture, data architecture, integration model, security posture, delivery readiness |

This review treats `Design.md` v1.6.1 as the authoritative specification. v1.6 appends **Version 1.6 Final Decisions** (D-01–D-13, L855–L1161); v1.6.1 appends **Version 1.6.1 Amendments** (AM-01–AM-05, L1163–L1263). The non-normative implementation-status block at L1265–L1274 records delivery progress only. Both decision sections state that they supersede conflicting text elsewhere. The body (L1–L854) and the decision text keep their original line numbers, so every anchor cited here (L109, L166, L233, L531, L699, L773, L797, L855, L930, L1049) remains valid. Where this document disagrees with the specification, the specification wins; every deviation is raised as a finding and requires an explicit specification change. Sections 3–8 preserve the baseline analysis in which the findings were raised; §11 records the disposition of every finding affected by v1.6 and v1.6.1, and §12 lists what remains open. Line references (`L###`) point at `Design.md`. `D-xx` denotes a v1.6 specification decision, `AM-xx` a v1.6.1 amendment, and `E-xx` an engineering decision from §9.

---

## 1. Executive summary

The specification describes a conventional, well-factored three-tier Go web application with a deliberate split between **application state** (SQLite: users, settings, sessions) and **guild operational data** (MariaDB: items, inventory, imports, audit). The layering rule — UI → REST → Service → Repository → Database (L109–L120) — is sound for the stated goals of validation, security and audit consistency, and it leaves room for the "future external tools" promise at L15 and L734.

The architecture was **implementable as written only after a set of gaps was closed**. Before the decision set, the most serious were: the `Audit` table was required (L107, L773–L783) but had no defined schema; the import format was undefined; there was no REST endpoint for `ImportList.SelectedFg` (L567, L646–L670); location normalization was shown only by example (L569–L605); and the spell merge key was undefined (L690–L698).

v1.6.1 closes the two hard defects recorded below and the packaging defect. **AM-01/AM-02** preserve the imported `EQUID` for unresolved items, prohibit sentinels such as `-1`, retain `EQUID` as the logical item reference while relaxing physical FK enforcement for unresolved rows, and add `MetadataMissingFg` (default `False`) as the missing-metadata marker. **AM-03** locates the canonical sample at `Docs/Samples/InventoryExport-Sample.txt` and fixes delimiter, header, column mapping, encoding and row-rejection rules, so the import contract is normative rather than tribal knowledge. **AM-04** standardizes `Hoard`, and **AM-05** corrects the version metadata and adds a change log. The specification is therefore **implementable as written**. What remains is operational (`Items` provisioning and production grants), setup recovery, the general audit taxonomy/UI and any future HTMX response shape. The warning sink, selection payload and session lifecycle were subsequently resolved by PD3-02/PD3-08 and PD-15; none of these was a Phase 1 blocker.

### 1.1 Assessment by dimension

| Dimension | Rating | Basis |
|---|---|---|
| Layering and separation of concerns | Strong | Explicit five-layer rule, repositories enforced as the only DB access path (L114–L120) |
| Data architecture | Adequate with risks | Two-store split confirmed by D-13 and identity fixed by D-01; AM-01/AM-02 remove the `-1` sentinel, preserve the source id and relax FK enforcement for unresolved rows; cross-store audit remains outbox-dependent |
| API design | Partially defined | Selection route is now mandated (D-11) and the HTMX→REST flow is fixed (D-03), but request/response contracts, error model and versioning are still absent |
| Security design | Partially specified | D-09/D-12 add hashing, cookie flags, CSRF, rate limiting and SQL allow-listing, yet token lifecycle, CSRF coverage of login/setup and rate-limit storage are undefined |
| Import pipeline design | Adequate with risks | D-04–D-08 and D-11 fix the format, normalization, merge, instances, missing-metadata behaviour and selection; parser edge cases and warning surfacing remain (E-06, E-14) |
| Deployability | Adequate with risks | Docker/Unraid target is explicit (L63–L67); no health checks, logging, backup, TLS or DB-permission requirements |
| Testability | Adequate | More fixtures are normative (D-05, D-06, D-07) and the sample now lives at its declared path with a documented contract (AM-03); only one decoding fixture exists (Blade of Carnage, L512–L529) and two real exports are still absent |

### 1.2 Blocking gaps (re-evaluated against v1.6.1)

| ID | Blocker | Finding refs | v1.6.1 status |
|---|---|---|---|
| B-1 | `Audit` table has no schema — blocks audit feature, screens and API | MR-01 | **Closed** — D-10 defines the table and append-only rule; taxonomy still open (DC-25) |
| B-2 | Import file format and column contract undefined — blocks the core import capability | MR-02, DC-23 | **Closed** — the sample is at its declared path, `Docs/Samples/InventoryExport-Sample.txt`, and AM-03 fixes delimiter, header, column mapping, encoding, variable-width rows, header skipping and row rejection |
| B-3 | No endpoint/contract to update `ImportList.SelectedFg` — blocks the review-and-deselect workflow | MR-03 | **Closed** — D-11 mandates `PUT /api/imports/{batchId}/selection`; the approved full-replacement payload, `422` invalid-ID handling and `409` terminal state are implemented |
| B-4 | Location normalization defined only by examples — non-deterministic implementation | MR-04 | **Closed** — D-05 gives rules, AM-04 settles `Hoard`, and PD3-01 resolves equipment/blank/unknown patterns to `Unspecified` with warnings |
| B-5 | Population of the read-only `Items` table is out of scope, and `EQUID` vs `Items.ID` conflicts — app cannot decode without it | MR-05, DC-01, MR-21 | **Closed for implementation; operational follow-up** — D-01 fixes identity, the development table is loaded and validated with 144,712 rows, and PD-19 maps it read-only; loader ownership/freshness and production grants remain E-01 |
| B-6 | Setup-mode detection by file existence only; bootstrap writes precede audit availability | DC-02, DC-03, DC-24 | **Partially closed** — D-02 gates on administrator count; missing-`Users` and all-admins-disabled paths open |
| B-7 | HTMX form posts vs JSON REST API boundary is undecided and architecturally load-bearing | DC-04, IC-27 | **Closed** — D-03 fixes the call chain; the implemented import UI uses JSON endpoints and vanilla `fetch` (PD3-09), so no fragment contract is pending |
| B-8 | Spell merge key (Name vs EQUID) and date behavior on quantity increase undefined | DC-05, DC-06, DC-21 | **Closed** — D-06 fixes identity and preserves dates; PD3-03/PD3-04/PD3-14 resolve import merge targets, concurrency and audit provenance |
| B-9 | D-08 imports unresolved items with a placeholder identifier | DC-19 | **Closed** — AM-01/AM-02 preserve the imported `EQUID`, forbid sentinels and add `MetadataMissingFg` |
| B-10 | v1.6 packaging incomplete (title, sample path, change log) | IC-25, DC-23 | **Closed** — v1.6.1 corrects the title, adds AM-01–AM-05 and a change log |

The import pipeline is implemented against the documented sample and the loaded 144,712-row development `Items` copy. B-5's remaining work is operational (loader ownership/freshness and production `SELECT`-only grants), not an import implementation blocker.

---

## 2. Review method

1. Read the complete specification (all 1,274 lines, including the informational status block: the body, the v1.6 decisions and the v1.6.1 amendments) and extracted every normative statement ("shall", "must", "only", numeric constants, enumerations).
2. Built a requirement inventory and mapped each to a layer.
3. Evaluated each layer against the stated benefits at L122–L129.
4. Traced each workflow (public browse, admin CRUD, setup, import, audit) across layers to find missing links.
5. Recorded findings by class: **MR** missing requirement, **DC** design conflict/ambiguity, **SR** security risk, **IC** implementation concern.
6. Confirmed repository state: `Design.md` plus the `Docs/` set; no code or schema exists.
7. Re-read v1.6 decisions D-01–D-13 and v1.6.1 amendments AM-01–AM-05, mapped each to the findings it affects, and verified the cited body anchors, the sample path and the amendment text.

**Status note.** Findings in §3–§8 are retained as raised against the original body, so their wording may describe a gap that v1.6 or v1.6.1 has since addressed. §11 is authoritative for each affected finding's disposition; §12 lists the findings that remain open after v1.6.1. No finding below is deleted, because the analysis is the audit trail for the decisions.

---

## 3. Conformance assessment of the prescribed architecture

`UI Layer → REST API Layer → Service Layer → Repository Layer → Database Layer` (L111). The specification then states four rules: the UI never touches tables (L114), all CUD flows through REST (L116), all REST flows through services (L118), all DB access flows through repositories (L120).

| Layer | Prescribed responsibility | Review assessment | Risks |
|---|---|---|---|
| UI | Server-rendered HTML with HTMX + Bootstrap 5 (L56–L61); public inventory and six admin screens (L805–L825) | Appropriate for a low-ops Unraid container | HTMX posts HTML form data while REST endpoints imply JSON; the request/response contract at this boundary is unspecified (DC-04). Server-side rendering also makes CSRF token propagation a per-form concern (SR-09). |
| REST | Owns every business operation (L730); listed endpoints (L736–L771); consumed by the UI today, external tools later (L732–L734) | Correct placement, but the endpoint list is incomplete and unversioned | No auth endpoint, no selection-update endpoint, no health endpoint; no pagination/filter/sort contract; unversioned surface is expensive to change once external tools depend on it (IC-01, IC-02). |
| Service | Validation, classification, date arithmetic, bitmask decoding, import commit | Right home for the domain logic implied by L376–L529 and L682–L698 | Domain rules are under-specified; services will encode guesses unless the spec is tightened (MR-04, DC-05, DC-06). |
| Repository | Sole DB access path | Enables the read-only `Items` guarantee only if DB grants also restrict writes (SR-13) | Cross-database references (users in SQLite, audit in MariaDB) cannot be enforced by the repository layer alone (DC-07). |
| Database | SQLite + MariaDB split (L69–L107) | Justified isolation of config/auth from guild data | No migration strategy, no transaction boundary definition, no retention policy (IC-05, IC-06, IC-07). |

### 3.1 Rule-to-implementation consequences

- **L116 ("all create/update/delete must pass through REST endpoints").** In a single-process Go application, the UI is colocated with the REST layer. The spec does not say whether the UI **calls its own HTTP endpoints over the network** or **dispatches in-process** to the same handlers. A loopback HTTP call costs a socket per interaction and complicates cookie/CSRF forwarding; in-process dispatch (shared handler functions invoked by both HTTP routes and UI actions) preserves the rule's intent. This must be decided (DC-04) because it shapes the entire handler package.
- **L114 ("UI shall never modify database tables directly").** Enforceable by package boundaries: UI packages must not import repository packages, and Go's `internal/` conventions plus a lint/import-graph check can make this mechanical.
- **L120 ("all database access through repositories").** Two repository domains are required: a SQLite repository set and a MariaDB repository set. No repository may span both stores; anything requiring both (audit of settings changes, user-referencing guild rows) is a service-level orchestration concern (DC-07).
- **L69–L107 (two databases).** The split is a sound operational choice: SQLite keeps auth/config available even if MariaDB is down, and MariaDB carries the larger, relational workload. The cost is that **there is no distributed transaction**. Every workflow that writes both stores must be defined as compensating or ordering-sensitive (see DatabaseDesign §7).

### 3.2 Architectural strengths worth preserving

1. **Soft delete for inventory** (L359–L368) preserves history and keeps references stable; it also keeps the public query simple (`DeletedFg = 0`).
2. **Denormalized decoded class/race strings** (L285–L309, L846–L848) is a defensible read-optimization for a guild-scale dataset; it makes filtering and display cheap at the cost of string-matching queries (IC-09).
3. **Staging table for imports** (L551–L567) is the right pattern: parse → review → commit, with audit-friendly retention of what was reviewed.
4. **Settings-driven lifecycle dates** (L200–L212, L314–L330) keep policy out of code, which fits the "configurable inventory lifecycle" goal (L14).
5. **Repositories as the only DB path** makes the read-only nature of `Items` a one-place decision and simplifies test doubles.

### 3.3 Architectural weaknesses

1. **Cross-store references are modeled as if they were foreign keys.** `GuildBankItems.ModBy` "Administrative user" (L373–L374), `ImportBatch.ImportedBy` (L540), `Audit` (L107) all refer to users that live in a different database. No FK, no cascade, no snapshot strategy is specified (DC-07).
2. **Stored derived values are declared immutable** — `DateOpen`/`DateBazaar` are "stored permanently" (L321, L330) while being derived from settings that remain editable (L214). Policy changes cannot be applied retroactively, and the intent (a rolling availability window) is not stated (DC-06).
3. **Single access level.** `AccessLevel` exists (L177) with only `Admin` (L185–L187). This is acceptable for v1 but the field implies an authorization model that is never defined; the API auth matrix (public vs admin) is therefore implicit (MR-06).
4. **The API is unversioned** (`/api/items`, L737) while external consumers are an explicit objective (L734). Adding `/api/v1` later is a breaking change (IC-02).
5. **No error model.** No status codes, error envelope, or validation message format is specified, so the UI and future clients cannot be built against a stable contract (IC-03).

---

## 4. Missing requirements

Each item is a requirement that the specification does not state but that the stated functionality depends on. None can be resolved by inference without a product decision.

| ID | Missing requirement | Spec refs | Impact if unresolved |
|---|---|---|---|
| MR-01 | **`Audit` table schema.** The table is listed (L107) and every write must create an entry (L773–L783), but no columns, types, entity reference format, before/after payload, actor identity, or retention rule are defined. | L107, L770–L783, L811 | Audit screen and `GET /api/audit` cannot be implemented; "audit consistency" (L126) is unachievable. **Blocks B-1.** |
| MR-02 | **Import input format.** "EverQuest inventory export" / "EQ Item Collector Export" (L236–L242, L608) is not defined: file type, delimiter, encoding, header row, column order, required columns (`ID`, `Name`, `Quantity`, `Location`), escape rules, and at least one golden fixture. | L236–L242, L564–L567, L615–L630 | Parser cannot be written or tested; Step 4's `ID = 0` / `Name = Empty` rules (L618–L625) imply a column contract that is never stated. **Blocks B-2.** |
| MR-03 | **Import selection persistence.** `SelectedFg` exists (L567) and the admin deselects records (L666) and locations (L646–L649), but the endpoint list (L747–L754) has no update/selection route and no summary route. | L567, L646–L670, L673–L680, L747–L754 | The "grid updates dynamically" workflow cannot persist; commit would import records the administrator deselected. **Blocks B-3.** |
| MR-04 | **Location normalization grammar.** Only examples are given (L569–L605); there is no rule set for case, whitespace, slot suffixes, `Hoard<n>`, `SharedBank<n>`, unknown formats, or collisions. | L565–L567, L569–L605, L637–L645 | Different implementations produce different `LocationGroup` values; selectors and records become inconsistent. **Blocks B-4.** |
| MR-05 | **`Items` data provisioning.** The table is "read-only" (L242–L244) and externally sourced, but nothing states how it is created/populated, which columns are mandatory, or how frequently it is refreshed. Aggregate fields "Stats, Effects, Requirements, Restrictions and all imported collector fields" (L255–L259) have no types. | L233–L259, L278, L846 | A fresh deployment has an empty `Items` table; import decoding cannot resolve class/race data (DC-01, DC-08). **Blocks B-5.** |
| MR-06 | **Authorization matrix.** Public browsing requires no authentication (L30), admin functions do (L43), but per-endpoint access is not specified. It is undefined whether `GET /api/items` returns soft-deleted rows to admins, whether `GivenTo` is public, and whether user administration is a separate privilege. | L19–L43, L174–L187, L737–L771, L797–L804 | API may leak internal data (`GivenTo`, `ModBy`, deleted rows) or block legitimate admin views. |
| MR-07 | **Collection query contract.** `GET /api/items` has no documented pagination, sort keys, filter syntax, search fields, or response envelope, although the public UI requires search, sort, natural sort, pagination, class and race filters (L817–L824). | L699–L728, L737, L817–L824 | UI and API cannot be built against a stable contract; sort/filter logic will be invented inconsistently. |
| MR-08 | **Manual inventory contract.** "Add/update/remove inventory" (L37–L39) is required, but `POST /api/items` and `PUT /api/items/{id}` have no field list, validation, `DateAdded` default, `EQUID` existence rule, mutable-field list, or undelete semantics. | L37–L39, L311–L374, L741–L745 | Admin CRUD is unimplementable without guessing; risk of bypassing lifecycle rules. |
| MR-09 | **Settings validation and secret handling.** Ranges/types for `GuildOpenDays` and `BazaarDays` (negative values would create inverted dates), what may be changed after setup, whether MariaDB connection changes are live, and whether `GET /api/settings` returns the DB password are all unstated. | L190–L214, L766–L768, L782 | Invalid settings produce corrupt lifecycle dates; the settings API could expose or overwrite credentials. |
| MR-10 | **Session policy.** No lifetime, idle timeout, sliding vs absolute expiry, renewal rule, logout endpoint, or revocation on password change/disable. | L216–L227, L799–L802 | "Session expiration" cannot be tested or implemented deterministically. |
| MR-11 | **Account lifecycle.** No password policy (length/complexity), password change/reset flow, username uniqueness/case rules, last-administrator protection, or self-deletion rule. | L139–L145, L174–L187, L756–L763, L798 | Security defaults are left to the implementer; risk of lockout or unauthenticated takeover paths. |
| MR-12 | **Import failure and observability.** `Status = Failed` exists (L547) but there is no `FailureReason`, no row-level error capture, no retry/re-import rule, and no statement on retaining the uploaded file or detecting duplicate uploads. | L536–L549, L556, L615–L630 | Failed imports are undiagnosable; duplicate commits may double inventory. |
| MR-13 | **Audit taxonomy.** The listed events (L776–L783) are examples, not an enumeration; action names, entity types, payload shape, and whether failed attempts or reads are recorded are undefined. | L773–L783 | Inconsistent audit records; reports cannot be built reliably. |
| MR-14 | **Public UI behavior.** Default sort, page size, empty state, the definition of "available" as displayed (L28), whether `GivenTo` or `SourceLocation` are public, `target`/`rel` attributes on EQResource links (L795), and the contents of the Dashboard screen (L806) are unspecified. | L20–L30, L314–L330, L784–L795, L805–L825 | Public behavior will be designed ad hoc and may expose internal data or fail accessibility/security checks. |
| MR-15 | **Operational requirements.** No health/readiness endpoint, structured logging, request IDs, backup/restore procedure, log retention, TLS termination assumption, trusted-proxy handling, or timezone rule for date arithmetic. | L63–L67, L830–L852 | Container cannot be monitored or restored; date math depends on host TZ. |
| MR-16 | **API conventions.** No versioning (`/api` vs `/api/v1`), error envelope, status-code mapping, content negotiation (HTML form vs JSON), or idempotency rule for `POST` operations. | L729–L771, L827–L838 | UI/REST friction (DC-04) and unstable external contract. |
| MR-17 | **Import review editing.** The administrator may deselect (L666) but it is not stated whether quantity, location group, or item name can be corrected in review, nor whether deselected rows remain in `ImportList`. | L551–L670 | Ambiguity in staging semantics; "retained for troubleshooting" (L556) conflicts with deleting deselects. |
| MR-18 | **Non-functional targets.** No expected inventory size, concurrency, response-time budget, or availability expectation, even though "Performance" is a design principle (L843). | L840–L852 | No basis for index, pagination, or SQLite write strategy decisions (IC-10). |
| MR-19 | **Date semantics.** `DateAdded`, `DateOpen`, `DateBazaar` are dates (L311–L330); no rule for inclusive/exclusive comparison, whether "open" means `DateOpen <= today` or `< today`, and what the public screen displays before/after each threshold. | L314–L330, L28 | Off-by-one availability errors; inconsistent public display. |
| MR-20 | **Data retention.** Import history "shall be retained" (L549) and inventory "never physically deleted" (L368), but no retention period, archival rule, or growth bound is given for `ImportList` and `Audit`. | L368, L549–L556, L773–L783 | Unbounded growth in MariaDB; no basis for cleanup jobs. |

---

## 5. Design conflicts and ambiguities

| ID | Conflict | Spec refs | Resolution required |
|---|---|---|---|
| DC-01 | **Item key naming.** `Items` "contains" `EQUID` (L251) while `GuildBankItems.EQUID` is documented as referencing `Items.ID` (L278). The primary key name and the referenced column are inconsistent. | L251, L274–L278 | Declare the canonical item key (recommend `EQUID` as PK of `Items`) and align the reference; otherwise the FK, indexes and API field names are ambiguous. **Blocks B-5.** |
| DC-02 | **Setup mode has no explicit state.** Setup mode is triggered solely by absence of `/config/guildbank.db` (L131–L135). A created-but-empty file, a failed migration, a MariaDB validation failure at Step 3 (L156–L159), or a partially completed wizard leaves an undefined state, and the wizard has no resume/restart rule. | L130–L164, L216–L227 | Introduce an explicit setup-complete marker (setting row or schema version) and behavior for partial setup. **Blocks B-6.** |
| DC-03 | **Audit cannot cover bootstrap writes.** All writes "shall create audit entries" including user changes and settings changes (L773–L783), but audit lives in MariaDB (L107) while the setup wizard creates the administrator and stores configuration in SQLite (L138–L164) *before* MariaDB is even validated. Additionally, if every settings write must be audited transactionally, a MariaDB outage would prevent login-configuration changes. | L107, L138–L164, L770–L783 | Define a documented exception for bootstrap/system writes, or an outbox/compensating audit strategy and degraded-mode policy. |
| DC-04 | **HTMX/SSR vs. JSON REST.** The UI is server-rendered HTMX (L56–L61) but must consume REST endpoints (L732) and all CUD must pass through them (L116). HTMX submits form-encoded data and expects HTML fragments; REST handlers conventionally accept/emit JSON. The spec does not define whether the UI calls its own API over HTTP or dispatches in-process, nor how CSRF, redirects, and validation errors flow. | L56–L61, L109–L120, L730–L734 | Choose the integration model explicitly (recommended: in-process invocation of shared handlers, JSON for `/api`, HTML adapters for HTMX). **Blocks B-7.** |
| DC-05 | **Spell identity and merge key.** "If spell already exists: increase quantity; if not: create record" (L690–L698) never defines existence. Is it keyed by `Name` (including the `Spell:`/`Song:`/`Ancient:` prefix), by `EQUID`, or by normalized spell name? Two spells at different source locations must merge, which discards per-location provenance. | L381–L399, L690–L698 | Declare the merge key and the treatment of `SourceLocation`, `DateAdded` and `GivenTo` on merge. **Blocks B-8.** |
| DC-06 | **"Stored permanently" vs. configurable lifecycle.** `DateOpen` and `DateBazaar` are computed from `GuildOpenDays`/`BazaarDays` (L314–L330) and "stored permanently", yet those settings remain editable (L200–L214) and lifecycle management is a headline feature (L14, L18). Changing the policy has no effect on existing stock; adding quantity to an existing spell row cannot restart its window. | L200–L214, L311–L330, L690–L698 | State the intended semantics per record (frozen at creation vs. recomputed) and define behavior on quantity increase. **Blocks B-8.** |
| DC-07 | **Cross-database references are unenforceable.** `ModBy` (L373), `ImportedBy` (L540) and every audit actor refer to `Users`, which lives in SQLite (L89). No FK is possible; `DELETE /api/users/{id}` (L763) can orphan history; usernames can be reused. | L69–L107, L174–L181, L370–L374, L538–L542, L756–L783 | Store an immutable actor identifier and a username snapshot in MariaDB, and restrict user deletion to disable (the existence of `DisabledFg` at L179 already suggests soft delete). |
| DC-08 | **`Items` is read-only but imports depend on it.** The app "shall not update" `Items` (L242–L244) and copies decoded strings from it (L281–L309), but no rule covers an import row whose `EQUID` is absent from `Items` (stale collector DB). | L233–L259, L276–L309, L615–L630 | Define behavior for missing metadata: reject the row, create a placeholder, or accept with empty class/race and a warning. |
| DC-09 | **Classification by name prefix vs. one row per instance.** `Spell:`/`Song:`/`Ancient:` names become stackable single rows (L381–L399), while "All other inventory" is one row per instance (L401–L416). EverQuest spell scrolls are physical items; the rule conflates item identity with name text, and omits `Tome:` (melee spell tomes) and any renamed/localized variants. | L376–L416, L531–L567 | Confirm the prefix list (including `Tome:`), whether classification uses only the prefix, and how non-stackable items with a spell-like prefix are handled. |
| DC-10 | **Public data exposure is undefined.** The public may view inventory, class/race restrictions and availability dates (L20–L30), but `GivenTo` (member names), `SourceLocation`, `ModBy`, and soft-deleted rows have no stated visibility rule. `GET /api/items` is public by omission. | L20–L43, L351–L374, L737–L745, L817–L825 | Define a public projection of fields and filtering (`DeletedFg = 0`) vs. an admin projection; state it in the API contract (MR-06). |
| DC-11 | **Delete semantics are contradictory.** `DELETE /api/items/{id}` (L745) is required, while inventory records "shall never be physically deleted" (L368). Users have `DisabledFg` (L179) but also `DELETE /api/users/{id}` (L763) with no rule preferring disable. | L359–L374, L745, L179, L763 | State that item DELETE is a soft delete (`DeletedFg = 1`, `ModAt`/`ModBy`), and choose disable-only for users. |
| DC-12 | **Editable connection settings vs. setup-only validation.** MariaDB host/port/database/user/password are validated only during setup Step 3 (L156–L159); `PUT /api/settings` (L768) makes all settings editable (L214), including these, with no re-validation, pool restart, or rollback rule. Likewise, an editable `EQResourceBaseUrl` (L203) may break every link. | L147–L164, L190–L214, L766–L768, L784–L795 | Define which settings are mutable after setup, require connection re-validation + atomic swap for DB settings, and validate the base URL (scheme allow-list, trailing `id=`). |
| DC-13 | **Natural sort mandate vs. SQL capability.** Natural sorting is required for location selectors, import views and inventory views (L699–L728), but neither SQLite nor MariaDB provides natural ordering. The spec does not say whether sorting is server-side, client-side, or via a normalized sort key column. | L699–L728, L817–L824 | Choose one implementation (recommended: a persisted `LocationSort`/name sort key or application-side comparator applied before pagination), otherwise API and UI will sort differently. |
| DC-14 | **Setup wizard bypasses the REST rule.** "All create, update, and delete operations must pass through REST endpoints" (L116) and "all REST endpoints must pass through business logic services" (L118), yet the first-run wizard writes the administrator and configuration directly to SQLite (L138–L164). | L109–L120, L130–L164 | Declare setup a documented exception implemented via service calls (not raw SQL), or expose setup endpoints; do not leave the rule silently violated. |
| DC-15 | **Session design is incomplete.** `Sessions` stores `SessionID`, `UserID`, timestamps (L216–L227); no hashed-token design, no logout endpoint, no per-request `DisabledFg` check, no purge of expired rows, and `LastAccessed` implies a write per request. | L216–L227, L756–L763, L799–L802 | Specify token storage (recommend hashing the cookie token), logout route, expiry evaluation, revocation, and a purge strategy (DatabaseDesign §3.3). |
| DC-16 | **Import batch state machine is undefined.** Statuses exist (L543–L548) and history is retained (L549), but nothing prevents committing a batch twice, re-committing a `Completed` batch, deleting a batch, or abandoning a `Processing` batch. Selection changes during commit are also unguarded. | L531–L549, L607–L698, L754 | Define allowed transitions, idempotency for `POST /api/imports/{id}/commit` (e.g., `Completed` is terminal), and concurrency control during commit. |
| DC-17 | **EQResource URL assembly is under-specified.** The base URL already ends with `id=` (L212) and the link appends `EQUID` (L789–L793). No rule covers trailing slash/query strings, URL validation, or non-http schemes in an administrator-editable value. | L203, L784–L795 | Specify canonical concatenation, validation (`https` only), and HTML escaping/`rel="noopener noreferrer"` (see SR-08). |
| DC-18 | **Import review lacks classification data.** `ImportList` has no `ItemType`, yet the review grid must display spells and the summary must count "Items Selected / Spells Selected" (L673–L680) before commit-time classification exists (L682–L698). Counting units (rows vs. `Quantity`) are also undefined. | L81–L107, L551–L567, L651–L698 | Add a derived or stored `ItemType` in staging, and define summary counts (records vs. resulting inventory rows). |

---

## 6. Security risks

The specification names the right control families (bcrypt, sessions, secure cookies, CSRF, expiry, rate limiting, admin-only routes, L797–L804) but sets no parameters and assigns no enforcement point. The risks below are ordered by severity.

| ID | Risk | Severity | Spec refs | Required control |
|---|---|---|---|---|
| SR-01 | **Setup wizard remains reachable.** Setup is triggered by DB-file absence (L131–L135) and includes unauthenticated admin creation (L138–L145). Without a hard "setup complete" gate, a deleted/renamed DB, a container restart with an empty volume, or a partially failed setup exposes an anonymous administrator-creation path. | Critical | L130–L164 | Persist a setup-complete marker; return `410 Gone` for `/setup` once complete; require CSRF; never allow re-running Step 1 if a user exists. |
| SR-02 | **MariaDB credentials at rest and in transit.** Host/port/database/user/password are "stored in SQLite" (L161–L164) with no encryption, no file-permission rule, and no statement that `GET /api/settings` redacts the password (L766). Credentials may then leak via backups, logs, or the settings API. | Critical | L147–L164, L190–L214, L766 | Encrypt secrets at rest (or document reliance on volume permissions), redact in all API output and logs, use a dedicated least-privilege DB user, and require TLS to MariaDB where supported. |
| SR-03 | **Session token handling unspecified.** `SessionID` is the primary key (L223) and presumably the cookie value; no hashing at rest, no rotation on login, no logout/revocation, no `Secure`/`HttpOnly`/`SameSite` values, and no per-request `DisabledFg` check. | High | L216–L227, L797–L804 | Store only a hash of a high-entropy token, rotate on privilege change, set `HttpOnly; Secure; SameSite=Lax/Strict`, check `DisabledFg` and expiry every request, and provide logout/revoke-all. |
| SR-04 | **Authorization relies on naming, not a matrix.** "Administrator-only secured routes" (L804) is stated but only `AccessLevel = Admin` exists (L185). No endpoint-level requirement prevents a missing middleware on a new route, and public `GET /api/items` can return admin-only fields (DC-10). | High | L19–L43, L174–L187, L737–L771, L804 | Define a route→role matrix; enforce with a default-deny middleware group; test every route for anonymous access; separate public/admin response DTOs. |
| SR-05 | **CSRF parameters undefined.** CSRF protection is required (L801) but there is no token issuance/validation rule for HTMX form posts, JSON requests, or the setup wizard, and no `SameSite` fallback. | High | L56–L61, L130–L164, L797–L804 | Synchronizer-token or double-submit pattern on every state-changing route including setup and logout; reject on missing/mismatched token; combine with `SameSite`. |
| SR-06 | **Login rate limiting undefined.** No threshold, window, key (IP vs. username vs. combination), persistence across restart, lockout policy, or protection for the MariaDB connection test. | High | L156–L159, L798–L804 | Define limits (e.g., per-IP and per-username sliding window), persistent counters or documented in-memory behavior, exponential backoff, audit of failures, and a generic failure message. |
| SR-07 | **Import upload is unvalidated.** No maximum size, content-type allow-list, encoding rule, row-count limit, or parse-time resource bound is specified (L608–L615). Large or malformed files can exhaust memory; names can carry markup. | High | L531–L567, L607–L630 | Cap upload size, stream-parse with row limits, reject unknown encodings, and treat all imported text as untrusted in templates. |
| SR-08 | **Stored XSS through administrator-editable values.** `EQResourceBaseUrl` is rendered into an `href` (L784–L795); a `javascript:` or `data:` value yields script execution for every public visitor. Item names, `GivenTo`, and `LocationRaw` are also free text rendered in HTML. | High | L203, L343–L358, L565–L566, L784–L795 | Validate the base URL to `https` with a host allow-list, HTML-escape all output, add a CSP, and use `target="_blank" rel="noopener noreferrer"` (L795 only says "new tab"). |
| SR-09 | **SQL injection via sort/filter.** Natural sorting (L699–L728) and public filters (L817–L824) invite dynamic `ORDER BY`/`WHERE` construction. | High | L699–L728, L737, L817–L824 | Whitelist sort keys and filter operators; parameterize all values; never concatenate identifiers from request input. |
| SR-10 | **SSRF via the MariaDB connection test.** Step 3 accepts an arbitrary host and port from an unauthenticated wizard (L147–L159), enabling internal network probing (e.g., internal admin ports, metadata endpoints). | Medium-High | L147–L159 | Restrict setup to a one-time bootstrap context, limit host/port, apply connect timeouts, and never echo raw driver errors. |
| SR-11 | **Transport and proxy trust.** "Secure cookies" (L800) require HTTPS, but TLS, reverse-proxy termination, and `X-Forwarded-Proto` handling are not specified. Gin's default proxy trust can allow spoofed client IPs, defeating rate limiting and audit. | Medium-High | L63–L67, L797–L804, L830–L852 | Require HTTPS via the Unraid reverse proxy, set trusted proxies explicitly, enable HSTS, and document the TLS assumption. |
| SR-12 | **Password policy and hashing parameters unset.** bcrypt is mandated (L181, L798) but cost, maximum length (bcrypt truncates at 72 bytes), complexity, reuse, and reset flow are undefined; no lockout complements rate limiting. | Medium | L139–L145, L174–L181, L798 | Set a cost (≥12), enforce length bounds and a modest complexity/breach check, and define reset/change flows. |
| SR-13 | **Read-only `Items` is only an application convention.** "Shall not update" (L242–L244) can be violated by a bug, a later feature, or a compromised process. | Medium | L233–L247, L827–L838 | Grant the runtime MariaDB user `SELECT` only on `Items`; use a separate migration/loader principal; document the grant matrix. |
| SR-14 | **Audit trail is tamperable.** Audit lives in MariaDB (L107) and nothing states append-only enforcement, write-once privileges, hash chaining, or that no update/delete routes exist. The same app user that writes audit can rewrite it. | Medium-High | L107, L773–L783, L811 | Make audit insert-only at the DB grant level, expose no mutation routes, and consider an integrity hash chain; define retention. |
| SR-15 | **Secrets and hashes in audit payloads.** "User Changes" and "Settings Changes" are audited (L782); naive before/after capture would persist bcrypt hashes and the MariaDB password in `Audit`. | Medium | L773–L783 | Redact secret fields in audit payloads; store field names and safe representations only. |
| SR-16 | **User enumeration and error leakage.** No requirement for uniform login failure messages or generic 5xx responses; raw driver errors from the connection test could reveal infrastructure. | Medium | L139–L145, L147–L159, L797–L804 | Uniform auth errors, structured internal logging, and generic client-facing errors with correlation IDs. |
| SR-17 | **Container and volume hardening.** No non-root user, read-only root filesystem, volume permission mode, or secret-injection rule for `/config/guildbank.db` and DB credentials. | Medium | L63–L67, L83–L85, L161–L164 | Run as non-root, document `/config` ownership, prefer Docker secrets/env over baked config, and publish an Unraid template with least privilege. |
| SR-18 | **Unbounded public/Audit reads.** Pagination is listed for the public screen (L821) but no limits are defined; `GET /api/audit` and `GET /api/imports` have no pagination contract, enabling enumeration and resource exhaustion. | Medium | L747–L771, L817–L824 | Mandatory maximum page size, cursor/token pagination, and rate limiting on public list endpoints. |

---

## 7. Implementation concerns

These are issues that will surface during construction even if every product decision above is made. They are not spec violations; they are engineering risks the plan must own.

| ID | Concern | Refs | Mitigation |
|---|---|---|---|
| IC-01 | **REST surface is incomplete.** Missing: login/logout/session, setup steps, import selection update and bulk select, import summary/preview, health/readiness, and a defined upload representation for `POST /api/imports` (multipart vs. text body). | L736–L771, L805–L811 | Freeze a complete route table before coding the handler layer (ImplementationPlan §3). |
| IC-02 | **Unversioned API with an external-consumer goal.** `/api/items` cannot evolve compatibly once third parties integrate. | L729–L734, L827–L838 | Mount the surface at `/api/v1` while keeping `/api` as an alias, or accept an internal-only v1 and document the commitment. |
| IC-03 | **No error contract.** Without a stable error envelope and status mapping, HTMX fragments, admin forms and future clients will each parse failures differently. | L729–L771 | Define `{error:{code,message,fields}}`, map validation→422, auth→401/403, missing→404, conflict→409, rate limit→429. |
| IC-04 | **Package boundaries must make the layer rules mechanical.** Nothing prevents a handler from importing a repository or a template from issuing SQL. | L109–L120 | Adopt `cmd/` + `internal/{domain,service,repository,api,web}` and an import-graph check (e.g., `go list`-based lint or `depguard`). |
| IC-05 | **No schema migration strategy.** Neither database has a version marker; the setup wizard and later releases both change schema. `Items` may be created by an external loader. | L69–L107, L130–L164 | Use one forward-only migration tool per store, recorded version tables, a startup lock, and separate `Items` provisioning documentation. |
| IC-06 | **Transaction boundaries are undefined.** Import commit must be all-or-nothing across `GuildBankItems`, `ImportList` state and `ImportBatch.Status`+audit; partial commits would corrupt inventory. Settings/user writes touch SQLite while their audit rows touch MariaDB. | L607–L698, L773–L783 | Single MariaDB transaction per commit (inventory + batch + audit), then a post-commit status update if necessary; document the cross-store ordering and failure recovery (DatabaseDesign §7). |
| IC-07 | **No distributed transaction.** Any workflow spanning SQLite and MariaDB can half-succeed. | L69–L107 | Choose the source of truth per operation, order writes so the retry-safe side is last, and record compensating actions; do not pretend atomicity exists. |
| IC-08 | **SQLite write contention.** Session `LastAccessed` writes on every request, plus login/settings writes, against a single file. Default pooled connections and no WAL increase `database is locked` errors. | L216–L227 | Enable WAL, set `busy_timeout`, use a dedicated write connection (or `SetMaxOpenConns(1)` for writes) with separate read connections, and throttle session touch writes. |
| IC-09 | **Filtering decoded strings is imprecise.** `ClassString`/`RaceString` are comma-joined abbreviations (L285–L309); filtering by class needs token matching, and `ALL` must short-circuit. Naive `LIKE '%WAR%'` is index-hostile and can match unintended tokens. | L285–L309, L418–L529, L821–L824 | Define canonical delimiters, store a leading/trailing separator (e.g., `,WAR,CLR,`) for exact token `LIKE`, or retain the numeric mask in a query column; always special-case `ALL`. |
| IC-10 | **Performance and pagination.** No dataset size is given (MR-18). Public list queries need stable ordering + count for pagination, and natural sort cannot be delegated to SQL naively. | L699–L728, L817–L824 | Add covering indexes (`DeletedFg`, `ItemType`, `Name`), avoid `SELECT *`, use keyset pagination where possible, and measure against a synthetic dataset (e.g., 100k rows). |
| IC-11 | **Natural sort vs. pagination.** Sorting only the current page produces wrong results; sorting the full table in application memory does not scale. | L699–L728 | Persist a zero-padded sort key (`sortGroup`, `sortIndex`, `sortName`) computed on write so MariaDB can order and paginate correctly; unit-test the comparator against the spec's `General 1/2/3/10/11` case. |
| IC-12 | **Date arithmetic placement.** `DateAdded + GuildOpenDays` (L319) can be computed in Go or SQL. Mixing both risks timezone shifts (container TZ vs. DB session TZ) and off-by-one differences. | L311–L330, L19 | Compute date-only values in one place (service, UTC, `time.Date` + `AddDate(0,0,n)`), store `DATE`, and define the comparison rule (MR-19). |
| IC-13 | **Bitmask decoding correctness.** Maps must follow the literal class order (L423–L453) and race order (L470–L500), handle `65535 = ALL`, unknown high bits, and mask `0`. The only acceptance fixture is Blade of Carnage (L512–L529). | L418–L529 | Implement as pure table-driven functions with exhaustive bit tests, the ALL sentinel, unknown-bit behavior, and the Blade of Carnage golden test; add property tests (decode→re-encode round trip). |
| IC-14 | **Location normalization drift.** Examples (L569–L605) do not cover `SharedBank 1` (shown in the selector list L644 but never normalized), mixed case, multi-digit slots, or malformed strings. Two implementations (preview vs. commit) must not diverge. | L565–L605, L637–L645 | One pure `NormalizeLocation` function used by both staging and commit, with a decision table for unknown formats (preserve raw as group + warning) and exhaustive tests. |
| IC-15 | **Import pipeline resource and state handling.** Streaming vs. buffering, encoding, row-level error collection, status transitions, and re-entrancy are unspecified. | L607–L698 | Stream-parse, stage inside one transaction, collect row errors into the batch, and make commit idempotent/terminal (`Completed` cannot recommit). |
| IC-16 | **Manual add consistency with import.** Manual creation must produce the same decoded strings, dates, `ItemType`, and `SourceLocation` conventions as the import path. | L37–L39, L261–L374, L682–L698 | Route both through one `InventoryService.Create` domain operation; never duplicate date/decoding logic in handlers. |
| IC-17 | **Soft-delete propagation.** Every public query, count, filter, export and dashboard tile must exclude `DeletedFg = 1`; the spec never enumerates them. | L359–L374, L817–L825 | Encapsulate "active inventory" as a repository scope used by all reads; add tests that removed rows disappear from list, search, counts and filters. |
| IC-18 | **Audit write path.** Placing audit calls at handler level misses service-level changes; placing them in repositories duplicates entries and cannot capture semantic before/after values. | L773–L783 | Write audit in the service layer via a dedicated recorder, with redaction and correlation IDs, inside the same MariaDB transaction where the guild data lives. |
| IC-19 | **Settings caching and connection hot-swap.** Settings are read constantly (lifecycle math, EQResource base) but change rarely; MariaDB connection settings can change at runtime (DC-12). | L190–L214, L766–L768 | Cache settings with explicit invalidation on `PUT`, and rebuild/swap the MariaDB pool atomically after a successful validation. |
| IC-20 | **Middleware order and HTMX behavior.** Correct ordering matters: recovery → request ID → access log → rate limit → session → CSRF → authorization. HTMX also needs special handling for `401` (redirect to login) and `422` (re-render fragment with errors). | L56–L61, L797–L804 | Document and test the middleware chain; configure HTMX response handling; ensure CSRF tokens are present in every rendered form. |
| IC-21 | **Testing infrastructure.** Two database engines must be exercised; `Items` is externally owned; EQResource must never be called in tests. | L63–L107, L512–L529 | Unit tests for pure domain logic; integration tests against temporary SQLite and a MariaDB container; fixture-driven parser/normalizer tests; contract tests for the route table. |
| IC-22 | **Deployment mechanics.** No Dockerfile, health check, graceful shutdown, static asset embedding, migration-on-start, `/config` volume, or Unraid template is specified. | L63–L67, L83–L85 | Multi-stage build (non-root, static binary), embedded templates/assets, graceful shutdown, `/healthz` + `/readyz`, migration lock, and a published Unraid XML template with volume/port mapping. |
| IC-23 | **Library choices are recommendations, not requirements.** Gin, HTMX and Bootstrap 5 are "recommended" (L51–L61); the session, migration, SQL and bcrypt libraries are unspecified. | L45–L67 | Record final choices and versions in the implementation plan; avoid ORMs that hide transaction boundaries unless explicitly adopted. |
| IC-24 | **Observability.** No logging format, level, request correlation, metric, or separation between audit and diagnostic logs. | L773–L783, L830–L852 | Structured JSON logs (slog), request IDs echoed in error responses, no secrets or password hashes in logs, and a documented log level switch. |

---

## 8. Recommended additions to the specification

These are additive recommendations, not substitutions. Each should be reflected in a future Design.md revision before the corresponding feature is built.

**Status at v1.6.1:** Recommendation A-01 is implemented by D-10, A-02 by D-02 (partially), A-03 by D-04 plus AM-03 (sample at its declared path, contract documented), A-04 by D-11, A-05 by D-05 plus AM-04, and A-07 by D-01/D-13. A-06 and A-08–A-12 remain open engineering work. Where the wording here differs from a decision or amendment, the decision or amendment supersedes. (Recommendation IDs `A-xx` in this section are distinct from the specification amendments `AM-xx`.)

| ID | Recommendation | Addresses |
|---|---|---|
| A-01 | Add an explicit `Audit` schema (append-only, actor snapshot, entity, action, before/after JSON, correlation ID) and an audit action taxonomy. | MR-01, MR-13, SR-14, SR-15 |
| A-02 | Add an explicit setup-complete marker and define partial-setup/resume behavior; disable setup routes when complete. | DC-02, SR-01 |
| A-03 | Specify the import file contract with at least two golden fixtures (items-only, spells-only) and encoding/limit rules. | MR-02, SR-07 |
| A-04 | Add import selection endpoints (route fixed by D-11 as `PUT /api/imports/{batchId}/selection`; bulk select/deselect by location still desired) and a summary endpoint. | MR-03, DC-16 |
| A-05 | Replace location examples with a normative grammar and canonical list, including `SharedBank`, plus a policy for unknown formats. | MR-04, IC-14 |
| A-06 | Publish the authorization matrix (route → anonymous/admin) and the public/admin response projections. | MR-06, DC-10, SR-04 |
| A-07 | Declare the canonical item key (`EQUID`), minimum `Items` columns, and the out-of-band provisioning procedure for that table. | MR-05, DC-01, DC-08 |
| A-08 | Define spell identity (merge key), quantity-increase behavior, and whether lifecycle dates are frozen or recomputed. | DC-05, DC-06 |
| A-09 | Add settings types, ranges, mutability-after-setup, secret redaction, and connection re-validation rules. | MR-09, DC-12, SR-02 |
| A-10 | Version the API (`/api/v1`), define the error envelope, pagination/filter/sort contract, and idempotency rules. | MR-07, MR-16, IC-01–IC-03, IC-10–IC-11 |
| A-11 | Add operational requirements: `/healthz`, `/readyz`, structured logging, backup/restore, TLS/proxy trust, timezone, retention jobs. | MR-15, MR-20, SR-11, SR-17, IC-24 |
| A-12 | State the documented exceptions to the "all writes through REST" rule (setup bootstrap, session touch, audit recorder) and the permitted internal dispatch model. | DC-03, DC-04, DC-14, IC-06 |

---

## 9. Decision register

The following decisions are prerequisites that are **not** settled by v1.6 or v1.6.1. Where a v1.6 decision overlaps, that decision wins and the row records only the remaining engineering gap. Until these are recorded, downstream work should not begin on the affected phases.

| ID | Decision | Blocking phase | Default recommendation |
|---|---|---|---|
| E-01 | `Items` provisioning and freshness contract (key is fixed by D-01) | Schema, import | Out-of-band loader documented with version/freshness metadata; runtime user has `SELECT` only |
| E-02 | Bootstrap/lockout path inside setup mode (gate fixed by D-02) | Setup, auth | Setup-only routes plus a documented offline recovery command; audit bootstrap as `system` via outbox |
| E-03 | HTMX response representation under the D-03 chain | API, UI | REST returns JSON; thin HTML adapters render fragments; no second business path |
| E-04 | Import file column contract and golden fixtures (path and contract fixed by D-04 + AM-03) | Import | Add two real exports; confirm the sample remains the golden parse |
| E-05 | Spell merge provenance and concurrency (identity fixed by D-06) | Import, inventory | Record merged-into row and location in audit; lock the target row; preserve first-seen dates |
| E-06 | Unknown/invalid location grammar and vocabulary (rules fixed by D-05) | Import, UI | Reject only empty values; group unknown patterns as their own normalized group; standardize `Hoard` |
| E-07 | Auth matrix + public projection | API, UI | Public read-only subset; admin-only writes, users, settings, audit, imports |
| E-08 | Session lifecycle (storage fixed by D-09) | Auth | 8-day absolute (configured in days, 1–730) + 30-minute idle sliding expiry; rotation on login; logout + revoke-all; purge job |
| E-09 | Audit taxonomy closure (schema fixed by D-10) | Audit | Use D-10's names as the closed set; add explicit events for restore, selection and failed login |
| E-10 | Date comparison semantics | Inventory, public UI | `DateOpen <= today` means open; one UTC date per request; document in UI labels |
| E-11 | Settings validation/secret policy | Settings | Enumerated types/ranges; secrets redacted; DB settings validated before pool swap |
| E-12 | API versioning/pagination | API | `/api/v1`, max page 100, whitelisted sorts, keyset for large lists |
| E-13 | Missing-metadata strategy (**resolved** by AM-01/AM-02) | Import, schema | Preserve the original `EQUID` and set `MetadataMissingFg`; no sentinel and no FK removal |
| E-14 | Warning sink for unresolved items (D-08) | Import, UI | Store warnings on `ImportList`/`ImportBatch`, surface counts in review and commit summary |
| E-15 | Selection payload contract (route fixed by D-11) | Import, API | Full replacement set of `ImportID`s scoped to the batch; `409` on terminal state; `422` for foreign IDs |
| E-16 | HTMX error/auth semantics | UI | `HX-Redirect` on `401`, `403` fragment with CSRF refresh, JSON only for API clients |

---

## 10. Traceability snapshot (spec → findings)

| Spec area | Lines | Assessment | Findings |
|---|---|---|---|
| Public capabilities | L19–L30 | Implementable, behavior under-specified | MR-06, MR-07, MR-14, MR-19, DC-10 |
| Admin capabilities | L32–L43 | Endpoints missing contracts | MR-03, MR-08, DC-11 |
| Stack and deployment | L45–L67 | Adequate, operational gaps | MR-15, SR-11, SR-17, IC-22, IC-23 |
| Database architecture | L69–L107 | Split confirmed (D-13), key fixed (D-01); no migration/retention; missing metadata resolved (AM-01/AM-02) | MR-20, DC-07, IC-05, IC-07, SR-13 |
| Layering rules | L109–L129 | Sound, one documented exception needed | DC-04, DC-14, IC-04, IC-18 |
| First-run experience | L130–L164 | Gate now administrator-count based (D-02); bootstrap/lockout undefined | DC-02, DC-24, DC-14, SR-01, SR-10 |
| SQLite schema | L166–L227 | Fields defined; hash-only session storage mandated (D-09); policies absent | MR-09–MR-11, DC-15, SR-02, SR-03, SR-22, IC-08 |
| Items table | L233–L259 | Read-only contract; identity fixed (D-01); provisioning still unassigned | MR-05, MR-21, DC-08, SR-13 |
| GuildBankItems | L261–L374 | Complete fields, mutable/immutable semantics unclear | DC-06, DC-11, DC-18, IC-16, IC-17 |
| Classification/decoding | L376–L529 | Deterministic constants, edge cases open | DC-09, IC-13 |
| Import tables/workflow | L531–L698 | Normalization, merge, instance rules (D-05–D-07) and the file contract (AM-03) are defined | MR-02–MR-04, MR-12, MR-17, MR-22, DC-05, DC-16, DC-18, IC-15 |
| Natural sorting | L699–L728 | Mandate without implementation strategy | DC-13, IC-11 |
| REST API | L729–L771 | Selection route added (D-11); contracts still absent | MR-03, MR-07, MR-16, DC-22, IC-01–IC-03, IC-27 |
| Audit | L773–L783 | Schema defined (D-10); taxonomy, retention and outage behaviour open | MR-01, MR-13, DC-25, SR-14, SR-15 |
| EQResource links | L784–L795 | Concatenation and XSS unaddressed | DC-17, SR-08 |
| Security requirements | L797–L804 | Control families named; D-09/D-12 add parameters, coverage gaps remain | SR-01–SR-22 |
| v1.6 final decisions | L855–L1161 | Normative; B-4 closed by PD3-01, B-5 operational (loader/grants), B-6 partial only for setup recovery | §11, §12 |
| v1.6.1 amendments | L1163–L1263; status L1265–L1274 | Normative amendments; closes B-2, B-9, B-10. Status block is informational only | §11, §12 |
| Store separation | L1147–L1161 | Confirmed by D-13; provisioning and audit-availability caveats | D-13, MR-21, DC-07 |
| Screens | L805–L825 | Dashboard undefined | MR-14 |
| Future enhancements | L827–L838 | No v1 impact except RBAC/API compatibility | IC-02, DC-07 |
| Philosophy | L839–L852 | Consistent with recommendations | — |

---

## 11. Design.md decision incorporation

v1.6 and v1.6.1 are normative: both sections state that they supersede conflicting statements elsewhere. The table is the authoritative disposition of every finding the decisions and amendments touch. **Adopted** means the intent is implementable as written; **closed by amendment** means v1.6.1 corrected the defect raised against the decision.

| Spec decision | Subject | Affected findings | Disposition | Residual gap |
|---|---|---|---|---|
| D-01 | Canonical item identifier | DC-01, DC-08 | **Adopted.** `Items.ID` is authoritative; `Items.ID`, `GuildBankItems.EQUID` and `ImportList.EQUID` denote the same value; `GuildBankItemID`/`ImportID`/`ImportBatchID` are unrelated. | Loader ownership and freshness still unassigned (MR-21). |
| D-02 | Setup mode | DC-02, SR-01 | **Adopted.** Setup exists only while `Users` contains no administrator; database-file existence is irrelevant. | Missing/unreadable `Users`, all-admins-disabled lockout and audited bootstrap remain undefined (DC-24). |
| D-03 | HTMX / REST architecture | DC-04, IC-04 | **Adopted.** UI → HTMX → REST → Service → Repository → DB; direct UI database access is prohibited. | Response representation and HTMX `401`/CSRF behaviour are still open (IC-27, E-03/E-16). |
| D-04 | Import format contract | MR-02, DC-23 | **Adopted; defect closed by AM-03.** `Docs/Samples/InventoryExport-Sample.txt` is authoritative and the file is present there; AM-03 fixes the tab-delimited header (`Location`, `Name`, `ID`, `Count`, `Slots`), column mapping, UTF-8 decoding, trailing-column omission (Count defaults to 1), section-header/blank-line skipping and `ID = 0`/empty-`Name` rejection. | Two real collector exports are still absent (test coverage, not a contract gap). |
| D-05 | Location normalization | MR-04, DC-20 | **Adopted; vocabulary settled by AM-04.** `General`/`Bank`/`SharedBank` `n-Slot m` → `Area n`; `Hoard n` → `Hoard`; `LocationRaw` preserved; `LocationGroup` drives filtering and selection. | **Resolved for imports:** PD3-01 maps equipment/blank/unknown reserved patterns to `Unspecified` with warnings and collapses every `Hoard<n>` to `Hoard`. |
| D-06 | Spell merge rules | DC-05, DC-06, DC-21 | **Adopted.** Identity is `ItemType='Spell'` + `EQUID`; quantities add; `DateAdded`, `DateOpen`, `DateBazaar` do not change. | **Resolved for imports:** PD3-03/PD3-04/PD3-14 define active-only targets, lowest-id anomaly handling, advisory-lock concurrency and audit provenance. |
| D-07 | Item import rules | DC-18, MR-17 | **Adopted.** Items never merge; each imported unit becomes an independent `GuildBankItems` row with its own lifecycle. | **Resolved for imports:** `ItemType` is a non-null staging field populated at parse time (PD3-07/PD3-11); manual-add semantics remain a later phase. |
| D-08 | Missing metadata behaviour | DC-08, DC-19 | **Adopted; conflict closed by AM-01/AM-02.** Unresolved items keep the imported `EQUID`, are imported with blank masks and `MetadataMissingFg = 1`, stay visible, and produce an import warning; sentinels such as `EQUID = -1` are prohibited, the `Items` reference is validated by the service, and FK enforcement is relaxed for unresolved rows. | **Resolved for imports:** warnings persist in `ImportList.WarningText` (PD3-02); the later metadata-refresh procedure remains a separate later-phase task. |
| D-09 | Session security | SR-03, DC-15 | **Adopted.** `Sessions` stores `TokenHash`; tokens are never plaintext; the browser holds the raw value. | **Resolved and implemented by PD-15:** 30-minute idle / 8-day absolute (the absolute lifetime setting is stored in days, 1–730), sliding refresh throttle, hourly purge, `HttpOnly`, `SameSite=Lax` and explicit `Secure`. |
| D-10 | Audit table definition | MR-01, SR-14, SR-15 | **Adopted.** MariaDB `Audit` has the ten named fields, is append-only, stores JSON snapshots and lists example event types. | **Import subset resolved by PD3-14**; general taxonomy, `TableName`/`RecordID` values, retention and audit UI remain later-phase work. |
| D-11 | Import selection endpoint | MR-03, DC-22 | **Adopted.** `PUT /api/imports/{batchId}/selection` persists `ImportList.SelectedFg` from selected `ImportID` values. | **Resolved and implemented:** full-replacement semantics, batch scoping, `422` invalid IDs, `409` terminal state and the response contract are fixed. |
| D-12 | Additional security | SR-05, SR-06, SR-09, SR-19, SR-20 | **Adopted.** CSRF on authenticated writes, `HttpOnly` and conditional `Secure` cookies, 5 failures/15-minute lockout, allow-listed sort/filter. | **Implemented for the current surface:** login/setup/logout CSRF, cookie flags and lockout are covered; future HTMX token transport is deferred (E-16). |
| D-13 | SQLite / MariaDB separation | DC-07, SR-02, MR-21 | **Adopted.** SQLite owns `Users`, `Settings`, `Sessions` and MariaDB connection information; MariaDB owns `Items`, `GuildBankItems`, `ImportBatch`, `ImportList` and `Audit`. | `Items` provisioning is still unassigned and authentication audit now depends on MariaDB availability. |

**Net effect after Phase 3.** B-1, B-2, B-3, B-4, B-7, B-8, B-9 and B-10 are closed; B-5 remains operational (loader ownership/freshness and production grants) and B-6 remains partial only for offline setup recovery. The specification is implementable as written, and the import pipeline is implemented against the loaded 144,712-row development `Items` copy.

## 12. Residual findings after v1.6.1

These findings remained open after v1.6.1. Items closed by the amendments or by Phase 3 are marked in the action column so their identifiers stay traceable; any unmarked row is a later-phase or operational item.

| ID | Class | Finding | Spec refs | Required action |
|---|---|---|---|---|
| MR-21 | Missing requirement | `Items` provisioning and freshness remain unowned even though D-01/D-13 make the table load-bearing | D-01, D-13, L233–L259 | Assign a loader owner; publish version/freshness metadata and a rebuild runbook |
| MR-22 | Missing requirement | D-08's "warning entry" had no table, API or UI sink | D-08, L1049–L1065 | **Resolved for imports by PD3-02:** warnings persist in `ImportList.WarningText` and counts surface in review/commit |
| DC-20 | Ambiguity | Unknown location patterns (unrecognized area names, case/whitespace variants, out-of-range indexes) had no policy, although `Hoard` is now the single spelling (AM-04) | L569–L605, D-05 | **Resolved for imports by PD3-01:** `Unspecified` fallback with warnings and `Hoard<n>` → `Hoard` |
| DC-21 | Ambiguity | Spell merge did not say which row/location receives added quantity, or how a soft-deleted target is handled | D-06, L690–L698 | **Resolved for imports by PD3-03/PD3-04/PD3-14:** active-only targets, lowest-id anomaly record and audit provenance |
| DC-22 | Ambiguity | Selection payload ("selected `ImportID` values") was undefined: replacement vs delta, batch scoping, idempotency, response | D-11, L1117–L1129 | **Resolved and implemented:** full-replacement, batch-scoped contract with `422` invalid IDs and a tested response shape |
| DC-24 | Ambiguity | The D-02 gate conflicts with the four-step wizard: committing the administrator at Step 1 disables setup before MariaDB is configured; there is also no recovery when `Users` is unreadable or all administrators are disabled | D-02, L877–L892, L138–L164 | Commit the administrator last in one SQLite transaction, fail closed on an unreadable `Users`, and define offline recovery (E-02) |
| DC-25 | Ambiguity | D-10 event types are examples, not a closed taxonomy; `TableName`/`RecordID` values and retention are unspecified | D-10, L1083–L1116 | Close the taxonomy and map every write path (E-09) |
| SR-19 | Security risk | D-12 requires CSRF only for **authenticated** writes, leaving login and setup outside the rule | D-12, L1130–L1145 | Require CSRF on login, logout and setup; test each route |
| SR-20 | Security risk | Rate limiting has a threshold but no key, storage, response or recovery model; username lockout is a denial-of-service vector | D-12 | Specify key (IP+username), persisted counters, `429`/lockout UX and admin unlock |
| SR-22 | Security risk | D-09 fixes storage but not entropy, rotation, `SameSite`, idle expiry or purge; `SessionID` may still be treated as the token | D-09, L1066–L1082 | Define token lifecycle and session-column semantics (E-08) |
| IC-26 | Implementation concern | Decision text is unnumbered prose; only L855 is a stable anchor, so citations must use decision IDs | L855–L1161 | Cite `D-xx`, not line offsets; add anchors only if line-level citation is required |
| IC-27 | Implementation concern | D-03 mandates HTMX→REST but not whether REST returns JSON or HTML fragments; two rendering paths may emerge | D-03, L893–L929 | Adopt JSON-first with server-side fragment adapters (E-03) |
| IC-28 | Implementation concern | `ItemType` in staging was PROPOSED although D-06/D-07 classification depends on it | D-06, D-07, L551–L567 | **Resolved and implemented:** `ImportList.ItemType` is a non-null Phase 3 staging field |

Every residual item is a contract-level question, not a stylistic one; the companion `DatabaseDesign.md` §12 and `ProjectStatus.md` §3/§6 carry the same items under the same IDs. **Closed by v1.6.1:** DC-19 and SR-21 (AM-01/AM-02 preserve the identifier and add `MetadataMissingFg`), DC-23 (AM-03 documents the sample path and contract), IC-25 (AM-05 corrects the title and adds the change log).

---

## 13. Conclusion

The architecture remains appropriate for a self-hosted guild utility and is now fully specified for v1: the layer rule is explicit (D-03), item identity is fixed (D-01), the audit table and append-only rule exist (D-10), import selection has a route (D-11), spell merge is determined (D-06), item instances are defined (D-07), and missing metadata is representable without breaking item identity (AM-01/AM-02). The import contract is now normative and points at a file that exists at the declared path (AM-03/AM-05). The specification is therefore **buildable as written**. Setup recovery, HTMX response representation, the audit taxonomy, the warning sink, the selection payload and the session lifecycle form the remaining tier; none is a contradiction, and each has a recorded engineering recommendation.

Resolution order: (1) assign the `Items` loader and load a development dataset; (2) define setup recovery and the session lifecycle; (3) close the audit taxonomy and the warning sink; (4) freeze the selection and HTMX contracts; (5) obtain two real collector exports before Phase 6. `Items` provisioning is an operational prerequisite, not a design decision, and proceeds in parallel.

With those underway, Phases 1–4 can start immediately and Phase 6 can follow on the documented sample. The remaining risk is concentrated in the parser and in cross-store audit delivery; both are testable with the fixtures the plan already requires, provided the fixtures are real.