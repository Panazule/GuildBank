# Project Status — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-001 |
| As-of date | 2026-11-21 |
| Source specification | `Design.md` v1.6.1 (1,263 lines: body L1–L854, decisions D-01–D-13 at L855–L1161, amendments AM-01–AM-05 at L1163–L1263) |
| Repository state | Phase 1: SQLite schema, four-step setup wizard, bcrypt administrator, login/logout, server-side sessions, CSRF, rate limiting, default-deny admin routes and settings persistence; inventory/imports/audit/MariaDB business features absent |
| Documents issued | `ArchitectureReview.md` (AR-001), `DatabaseDesign.md` (DD-001), `ImplementationPlan.md` (IP-001), `ProjectStructure.md` (PS-002), this status report |
| Overall status | **v1.6.1 published; Phase 0 skeleton and Phase 1 identity/settings layer complete; inventory, imports and audit remain later phases** |

---

## 1. As-is snapshot

| Area | State | Evidence |
|---|---|---|
| Requirements | Written; v1.6.1 amendments appended; canonical import sample at its declared path | `Design.md` v1.6.1 |
| Architecture review | Complete, revised | AR-001 rev 1.2: 22 missing requirements, 25 conflicts, 22 security risks, 28 implementation concerns, D-01–D-13 and AM-01–AM-05 adopted, E-01–E-16 registered |
| Data design | Complete, with proposed additions pending approval | DD-001 rev 1.2: DDL for 4 SQLite tables (1 proposed), 5 MariaDB tables (`Audit` specified by D-10, `MetadataMissingFg` by AM-02) |
| Delivery plan | Complete | IP-001: 9 phases, estimates, test and deployment strategy |
| Source code | Phase 1 application layer present | `cmd/guildbank`, `internal/{domain,service,repository,api,web,platform}`; setup, auth, sessions, settings; no inventory/import/audit |
| Build/CI | Complete for the current scope | `go.mod`/`go.sum` (Go 1.27.3), `Makefile`, `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml` |
| Environments | Development topology defined | Compose app + MariaDB 11.4; disposable credentials; no production environment |
| Test assets | Canonical import sample plus Phase 0/1 unit and API tests | `Docs/Samples/InventoryExport-Sample.txt` (548 rows); tests for config, logging, app, sqlite migrations/repositories, mariadb, service, api, web |
| Deployment | Configured, not deployed | Multi-stage static image, non-root UID 10001, `/config` volume, `EXPOSE 8080`, healthcheck `/healthz` |
| Operations | Health/logs and session purge implemented; backup/support pending | `/healthz`, `/readyz`, JSON logs with request IDs, hourly session purge, graceful shutdown with WAL checkpoint |

**Maturity by capability** (specification and implementation are tracked separately, because a well-specified feature can still be unimplemented):

| Capability | Specification | Implementation | Blocker |
|---|---|---|---|
| Setup wizard | Partial — gate/order implemented; recovery undefined | **Implemented** for the four-step flow | E-02 offline recovery |
| Authentication/sessions | Partial — lifecycle implemented to E-08 defaults; approval open | **Implemented** (login/logout, hashed sessions, expiry, CSRF, lockout) | E-08, PD-11–PD-17 approval |
| Public inventory browsing | Defined | Not started | E-10, E-12 |
| Class/race decoding | Defined with one fixture | Not started | — |
| Inventory administration | Partial — provisioning unowned | Not started | E-01 |
| Import pipeline | **Specified** — D-04/AM-03 contract, D-08/AM-01/AM-02 metadata rule | Not started | B-4 (edge cases only) |
| Spell merge/lifecycle dates | Defined by D-06; provenance open | Not started | DC-21 |
| Settings | Partial — typed/secrets implemented; policy approval open | **Implemented** (read/update/redaction) | E-11, PD-11 |
| User management | Partial — policy/last-admin rules | Not started | E-07 |
| Audit | Schema defined by D-10; taxonomy open | Not started (AuditOutbox deferred) | DC-25, E-09 |
| Deployment/operations | Partial — no backup/TLS rules | Not started | — |

### 1.1 Phase 0 implementation record (2026-11-21)

Per the owner's Phase 0 directive, the deliverable was the platform skeleton
and infrastructure. `ImplementationPlan.md` classifies this scope as Phase 1;
the documentation-closure tasks remain in Phase 0 as written. This report
tracks both so neither is reported as complete when it is not.

| Deliverable | State | Evidence |
|---|---|---|
| Go module and pinned dependencies | Complete | `go.mod` (Go 1.27.3), `go.sum`; Gin 1.12.0, modernc SQLite 1.62.0, MariaDB driver 1.10.2 |
| Recommended folder structure | Complete | `cmd/guildbank`; `internal/{domain,service,repository/{sqlite,mariadb},api,web,platform/{config,logging,app}}` |
| Configuration framework / environment loading | Complete | Defaults → env → flags, validation, usage, password-safe logging |
| SQLite connection layer | Complete | WAL, `busy_timeout(5000)`, foreign keys, single writer, setup probe, WAL checkpoint on close |
| MariaDB connection layer | Complete; live connectivity verified | UTF-8 DSN, UTC, 5 s bounded ping, explicit pool limits, optional at startup; `/readyz` reported `mariadb: ok` against a MariaDB 11.4 container |
| Logging framework | Complete | `slog` JSON/text, request IDs, access log, panic recovery, no secrets |
| Application startup/shutdown | Complete | Composition root, writable `/config` check, bounded 10 s graceful shutdown |
| Gin server and health endpoints | Complete | `/healthz` 200 JSON; `/readyz` 200/503 with per-check reasons; JSON 404/405 |
| Dockerfile / Compose | Complete; image, container, Compose topology and volume persistence verified | Multi-stage, static binary, UID 10001, `/config` writable, `EXPOSE 8080`, healthcheck; Compose app + MariaDB 11.4 both healthy, `/readyz` `mariadb: ok`; `/config` backed by named volume `guildbank_config` and survives container recreation |
| Build/run instructions | Complete | `README.md`, `Makefile`, CI workflow |
| Tests | Complete for skeleton | `go build`, `go vet`, `go test ./...` pass; runtime endpoint probes pass |
| ADRs for D-01–D-13, AM-01–AM-05, E-14–E-16 | **Not started** | Planned under `Docs/decisions/`; no files created |
| E-14 warning sink, E-15 selection payload, E-09 audit taxonomy closure | **Open** | Product/engineering decisions still required before Phases 3 and 6 |
| Two real collector exports | **Open** | External input; canonical sample is tracked |

Known deliberate omissions: no migrations, no setup/auth/session routes, no
import or inventory services, no audit recorder, no HTML UI, and no automated
MariaDB integration test (connectivity was validated manually against a
container; Phase 3 adds the automated integration suite).

### 1.2 Phase 1 implementation record (2026-11-21)

The owner authorized Phase 1: SQLite schema, Users/Settings/Sessions, setup
wizard, administrator creation, bcrypt, login/logout, session management,
authentication middleware and settings persistence. Audit, inventory, imports
and MariaDB business features were explicitly excluded; `AuditOutbox` is
therefore deferred with the audit recorder.

| Deliverable | State | Evidence |
|---|---|---|
| SQLite migration framework and Phase 1 schema | Complete | `migrations/0001_phase1.sql`; `Migrate` uses `PRAGMA user_version=1`; newer schemas refused; atomic migration and setup transactions tested |
| Users table | Complete | DDL per DD §3.1; case-insensitive unique username; Admin-only access; `DisabledFg`; `LastLogin`; bcrypt hash length 60 verified live |
| Settings table | Complete | Typed/secret columns, seeded defaults, MariaDB keys, session policy; read/update validation and atomic upsert |
| Sessions table | Complete | Distinct `SessionID`/`TokenHash`, idle and absolute expiry, revocation/purge; raw tokens never stored (live `TokenHash` length 64) |
| Setup wizard | Complete | Four steps; admin/guild/database validation; connection test; single atomic commit; `410` after completion; no partial admin |
| Administrator creation | Complete | `SetupService.Complete` hashes password, commits admin + settings, issues a session |
| bcrypt password hashing | Complete | `golang.org/x/crypto/bcrypt` cost 10; round-trip and timing-equalizer tests |
| Login | Complete | Generic failure for unknown/wrong/disabled; `LastLogin`; fifth failure `429`; `HttpOnly` `SameSite=Lax` cookie |
| Logout | Complete | Revokes the row, clears cookie, CSRF-protected; live `204` then `401` |
| Session management | Complete | 32-byte token, SHA-256 hash, 30-min idle / 8-hour absolute, one-minute touch throttle, hourly purge, disable check per request |
| Authentication middleware | Complete | Default-deny admin group; API `401`, HTML redirect; admin-only access |
| Settings persistence | Complete | Admin UI + `GET/PUT` API; secrets redacted; blank MariaDB password preserves the stored secret; invalid values `422` |
| CSRF | Complete | Signed double-submit cookie; login, setup and every write; `403` without a token |
| Login rate limiting | Complete | In-process IP+username 5-failure/15-minute lockout |
| Tests and live validation | Complete | `go build`, `go vet`, `go test ./...`; Compose app + MariaDB healthy; setup, login, settings, logout and restart verified |

Phase 1 decisions requiring owner approval are recorded as PD-10–PD-18 in
`Docs/ProjectStructure.md` §4.2. The most material are: only three SQLite
tables in this phase, password-policy values, custom CSS/JS rather than
Bootstrap/HTMX, explicit secure-cookie configuration, and MariaDB runtime
continuing on the environment bootstrap until Phase 3.

---

## 2. Documents produced in this review

| Document | Contents | Key outputs |
|---|---|---|
| `Docs/ArchitectureReview.md` | Conformance assessment, findings, decisions, v1.6 disposition | MR-01–MR-22, DC-01–DC-25, SR-01–SR-22, IC-01–IC-28, A-01–A-12, E-01–E-16, D-01–D-13 mapping |
| `Docs/DatabaseDesign.md` | Physical schema, constraints, transactions, grants, retention | SQLite DDL (Users, Settings, Sessions, AuditOutbox), MariaDB DDL (Items, GuildBankItems, ImportBatch, ImportList, Audit), 9 workflow specifications, D-01–D-13 conformance |
| `Docs/ImplementationPlan.md` | Phased delivery, API surface, audit taxonomy, testing, deployment | 9 phases (58–84 engineer-days with contingency), route table, D-10 event taxonomy, fixtures, DoD, risk register |
| `Docs/ProjectStatus.md` | This report | Traceability, blockers, next actions |

No application functionality beyond the platform skeleton and Phase 1
identity/settings layer exists. Phase 0 created the buildable service,
configuration, logging, both connection layers, health endpoints, deployment
files and tests. Phase 1 added the SQLite schema, setup, authentication,
sessions, CSRF, rate limiting, settings and the first screens; the full map is
in `Docs/ProjectStructure.md`.

---

## 3. Blocking items

| ID | Blocker | Decision / amendment | Status | Consequence / next action |
|---|---|---|---|---|
| B-1 | `Audit` has no schema | D-10 | **Closed** | Taxonomy/retention still open (DC-25) |
| B-2 | Import file format undefined | D-04, AM-03 | **Closed** | Sample at `Docs/Samples/InventoryExport-Sample.txt`; delimiter, header, mapping, UTF-8, variable-width rows and row rejection fixed |
| B-3 | No endpoint to persist `SelectedFg` | D-11 | **Closed** | Payload semantics open (DC-22) |
| B-4 | Location normalization is examples only | D-05 | **Partial** | Unknown patterns and case/whitespace remain (DC-20); `Hoard` settled by AM-04 |
| B-5 | `Items` provisioning; `EQUID` vs `Items.ID` | D-01 | **Partial** | Identity fixed; loader ownership absent (MR-21) |
| B-6 | Setup mode has no explicit state | D-02 | **Closed for implementation** | Explicit pending/complete/unknown state; missing `Users` fails closed; disabled admins still count; admin + settings commit atomically; offline recovery command remains E-02 |
| B-7 | HTMX/SSR ↔ JSON REST model undecided | D-03 | **Closed** | JSON-vs-fragment response still open (IC-27) |
| B-8 | Spell merge key and date behavior undefined | D-06 | **Closed** | Merge provenance/concurrency open (DC-21) |
| B-9 | D-08 wrote a placeholder identifier for unresolved items | D-08, AM-01/AM-02 | **Closed** | Original `EQUID` preserved, `MetadataMissingFg` added, no sentinel, FK enforcement relaxed |
| B-10 | Specification packaging incomplete | AM-05 | **Closed** | v1.6.1 title, `Hoard`, amendments and change log published |

Supporting engineering decisions still required before the relevant phase: session lifecycle (E-08), secrets at rest (E-11), authorization matrix (E-07), API conventions (E-12), warning sink (E-14), selection payload (E-15), HTMX error semantics (E-16). E-13 (missing metadata) is resolved by AM-01/AM-02.

---

## 4. Requirement traceability

Status legend: **Defined** = implementable as written; **Partial** = exists but lacks parameters/contract; **Missing** = required behavior absent.

### 4.1 Functional requirements

| ID | Requirement | Spec refs | Spec status | Implementation | Covered in |
|---|---|---|---|---|---|
| FR-01 | Public inventory browsing without login | L20–L22 | Defined | Not started | IP Phase 4 |
| FR-02 | Display item details incl. stats/restrictions | L24–L27 | Defined | Not started | IP Phase 4 |
| FR-03 | Display availability dates | L28 | Partial (semantics) | Not started | DD §4.2, IP Phase 4 |
| FR-04 | Class and race filtering | L25–L26, L821–L824 | Partial (ALL/lookup) | Not started | DD §9.1, §9.6 |
| FR-05 | Search, sort, natural sort, pagination | L699–L728, L817–L824 | Partial (contract) | Not started | DD §9.5, IP Phase 4 |
| FR-06 | EQResource links in new tab | L30, L784–L795 | Partial (URL safety) | Not started | AR SR-08, IP Phase 4 |
| FR-07 | Admin add inventory | L37–L39, L741–L745 | Partial (validation) | Not started | DD §7.2, IP Phase 5 |
| FR-08 | Admin update inventory | L37, L744 | Partial (mutable fields) | Not started | DD §7.3, IP Phase 5 |
| FR-09 | Admin remove inventory (soft delete) | L38, L359–L368 | Defined | Not started | DD §7.3, IP Phase 5 |
| FR-10 | Import inventory upload | L41, L607–L630; D-04, AM-03 | Defined | Not started | AR MR-02, IP Phase 6 |
| FR-11 | Review import, deselect rows/locations | L646–L670; D-11 | Defined (payload open) | Not started | DD §7.9, AR DC-22, IP Phase 6 |
| FR-12 | Commit import with quantity expansion | L673–L698; D-06, D-07 | Defined | Not started | DD §7.5, IP Phase 6 |
| FR-13 | Retain import history | L549–L556 | Defined | Not started | DD §4.3–4.4, §10.4 |
| FR-14 | Manage settings | L35, L190–L214 | Partial (types/secrets) | **Implemented** (read/update/redaction) | DD §3.2, IP Phases 2–3 |
| FR-15 | Manage users | L43, L756–L763 | Partial (policy/delete) | Not started | DD §3.1, §7.7, IP Phase 7 |
| FR-16 | Audit trail (items, imports, users, settings, auth) | L773–L783; D-10 | Defined (taxonomy open) | Not started | DD §4.5, IP §5 |
| FR-17 | First-run wizard (4 steps) | L130–L164; D-02 | Partial (recovery) | **Implemented** | DD §7.1, IP Phase 2 |
| FR-18 | Public + six admin screens | L805–L825 | Partial (dashboard contents) | Not started | IP §4, Phases 4–7 |
| FR-19 | Future external API/tool support | L729–L734 | Defined (intent only) | Not started | AR IC-02, IP §4 |

### 4.2 Data and non-functional requirements

| ID | Requirement | Spec refs | Spec status | Implementation | Covered in |
|---|---|---|---|---|---|
| DR-01 | SQLite for users/settings/sessions | L69–L107, L166–L231 | Defined | **Implemented** | DD §3 |
| DR-02 | MariaDB for guild data | L69–L107, L233–L605 | Defined | Not started | DD §4 |
| DR-03 | `Items` read-only | L242–L244 | Defined (unenforced) | Not started | DD §4.1, §10.1 |
| DR-04 | Decoded class/race strings stored | L285–L309, L846–L848 | Defined | Not started | DD §9.6 |
| DR-05 | Lifecycle dates stored permanently | L314–L330 | Defined (conflicts with edits) | Not started | AR DC-06, DD §4.2 |
| DR-06 | Soft delete only | L359–L368 | Defined | Not started | DD §4.2, §7.3 |
| DR-07 | Cross-store actor identity | L370–L374, L538–L542 | Defined | Not started | DD §5–§6 |
| NR-01 | Layered architecture enforcement | L109–L129 | Defined | **Implemented** (import guards) | AR §3, IP Phase 1 |
| NR-02 | bcrypt password storage | L181–L183, L798 | Partial (cost/policy) | **Implemented** (proposal PD-11) | AR SR-12 |
| NR-03 | Session expiration | L216–L227, L799–L802 | Partial (timeouts) | **Implemented** (proposal PD-15) | AR SR-03, DD §3.3 |
| NR-04 | CSRF protection | L801 | Partial (mechanism) | **Implemented** (proposal PD-13) | AR SR-05, IP Phase 2 |
| NR-05 | Login rate limiting | L802 | Partial (thresholds) | **Implemented** (proposal PD-16) | AR SR-06, IP Phase 2 |
| NR-06 | Secure cookies / HTTPS | L800 | Partial (transport) | **Implemented** explicit flag (proposal PD-17); TLS deployment pending | AR SR-11 |
| NR-07 | Administrator-only routes | L43, L804 | Partial (matrix) | **Implemented** default-deny admin group | AR SR-04, IP §4 |
| NR-08 | HTTPS-ready Docker/Unraid deployment | L63–L67 | Partial (health/ops) | Not started | IP §7 |
| NR-09 | Performance suitability | L843 | Partial (no targets) | Not started | AR MR-18, IP Phase 8 |

### 4.3 Coverage summary

| Category | Defined | Partial | Missing | Total |
|---|---|---|---|---|
| Functional | 7 | 12 | 0 | 19 |
| Data/NFR | 8 | 8 | 0 | 16 |
| **Total** | **15 (43 %)** | **20 (57 %)** | **0 (0 %)** | **35** |

No requirement is classed **Missing** after v1.6.1. The former contradictions are resolved: FR-10 is implementable against the documented sample, and the D-08 missing-metadata rule is implementable under AM-01/AM-02. The residual risk is behavioural (location edge cases) and operational (`Items` provisioning).

---

## 5. Risk register (project level)

| Risk | Likelihood | Impact | Score | Mitigation | Owner |
|---|---|---|---|---|---|
| Residual contracts not closed (E-14/E-15/E-09) | Medium | High | **High** | Timebox Phase 0; use AR §9 defaults | Product owner |
| Import format diverges from real exports (D-04/AM-03) | Medium | High | **High** | Add two real exports before Phase 6; keep the parser behind an interface | Product/ops |
| Unresolved metadata never revalidated (AM-01/AM-02) | Medium | Medium | Medium | Implement the `MetadataMissingFg` refresh job and audit resolution | Dev |
| `Items` table unavailable or stale in target environment | Medium | High | **High** | Define loader procedure and test with real metadata | Operator |
| Setup/bootstrap lockout or ordering flaw (D-02) | Medium | High | **High** | Fail closed on unreadable `Users`, admin-commit-last wizard, CSRF, tests | Dev |
| Secret leakage (DB password, hashes) | Medium | High | **High** | Redaction tests, `IsSecret`, volume permissions | Dev/ops |
| SQLite lock contention under admin load | Medium | Medium | Medium | WAL, single writer, load test | Dev |
| MariaDB outage blocks operational writes | Medium | Medium | Medium | Outbox + degraded mode + clear UX | Dev |
| Scope creep from future enhancements | Medium | Medium | Medium | Non-goals list; decision gate per new table/route | Product owner |
| Single-maintainer knowledge concentration | High | Medium | Medium | ADRs, docs, tests, runbooks | Project lead |
| Performance below expectation at scale | Low | Medium | Low | Indexes, persisted sort keys, Phase 8 measurement | Dev |

---

## 6. Decision log

### 6.1 v1.6 specification decisions (adopted)

| Decision | Status | Effect |
|---|---|---|
| D-01 item identity | **Adopted** | `Items.ID` = `EQUID`; app ids distinct |
| D-02 setup mode | **Adopted** | Gate is "no administrator exists"; ordering caveat (DC-24) |
| D-03 HTMX/REST | **Adopted** | UI→HTMX→REST→Service→Repository→DB |
| D-04 import contract | **Adopted** | Sample at `Docs/Samples/`; AM-03 fixes delimiter, header, mapping, UTF-8 and row rejection |
| D-05 location normalization | **Adopted** | `Area n-Slot m`→`Area n`; `Hoard n`→`Hoard` |
| D-06 spell merge | **Adopted** | `(Spell, EQUID)`; quantity adds; dates unchanged |
| D-07 item import | **Adopted** | Never merge; one row per unit |
| D-08 missing metadata | **Adopted** | AM-01/AM-02: `EQUID` preserved, `MetadataMissingFg`, no sentinel, FK relaxed |
| D-09 session security | **Adopted** | `TokenHash` stored; no plaintext tokens |
| D-10 audit table | **Adopted** | Ten fields; append-only; example event types |
| D-11 selection endpoint | **Adopted** | `PUT /api/imports/{batchId}/selection` |
| D-12 additional security | **Adopted** | CSRF (authenticated writes), cookie flags, 5/15 lockout, SQL allowlist |
| D-13 store separation | **Adopted** | SQLite identity/config/sessions; MariaDB guild data |

### 6.2 Engineering decision register

| ID | Subject | Status | Default |
|---|---|---|---|
| E-01 | `Items` provisioning/freshness | Open | Loader-owned, documented, `SELECT`-only |
| E-05 | Spell merge provenance/concurrency | Open | Row lock; record merged-into row in audit |
| E-06 | Unknown location patterns/vocabulary | Open | Preserve raw; one group per unknown pattern; `Hoard` |
| E-07 | Authorization matrix/projections | Partial | Admin-only routes enforced; public inventory projection still open |
| E-08 | Session lifecycle | **Implemented as proposal PD-15** | 8 h absolute, 30 min idle, sliding refresh, purge; approval open |
| E-09 | Audit taxonomy closure | Open | D-10 names closed; extensions approved |
| E-10 | Date comparison semantics | Open | `DateOpen <= today` means open; UTC |
| E-11 | Settings validation/secrets | **Implemented as proposal PD-11/PD-18** | Typed settings, redaction, volume-permission reliance; approval open |
| E-12 | API versioning/pagination | Partial | `/api/v1` used for Phase 1; pagination rules pending |
| E-13 | Missing-metadata strategy | **Resolved** | AM-01/AM-02: preserve id, set `MetadataMissingFg`, relax FK |
| E-14 | Warning sink | Open | `ImportList.WarningText` + batch counts |
| E-15 | Selection payload contract | Open | Full set; batch-scoped; `422` foreign ids |
| E-16 | HTMX error/auth semantics | Open | `HX-Redirect` on 401; fragment on 403; Phase 1 uses plain forms |

D-01–D-13 are specification decisions and do not require engineering approval. Phase 1's security/scope defaults are listed as PD-10–PD-18 in `Docs/ProjectStructure.md` §4.2 and remain proposals until signed.

---

## 7. Next actions

| # | Action | Owner | Effort | Depends on | Output |
|---|---|---|---|---|---|
| 1 | Review and approve/reject Phase 1 decisions PD-10–PD-18 | Product owner | 0.5 d | — | Signed Phase 1 decision record |
| 2 | Review Phase 1 implementation and authorize the next phase | Product owner | — | #1 | Written go/no-go |
| 3 | Add two real collector exports beside the canonical sample | Guild admin + dev | external | — | Import fixtures (closes E-04) |
| 4 | Define the `Items` loader procedure and load a development dataset | Operator | 1–2 d | — | Populated `Items`, runbook |
| 5 | Close the audit taxonomy (E-09) and define the warning sink (E-14) | Dev + product | 0.5 d | #1 | Closed allowlist, warning field |
| 6 | Resolve selection payload (E-15) and HTMX error semantics (E-16) | Dev + product | 0.5 d | #1 | API/UI contract for imports |
| 7 | Decide when Settings become the authoritative MariaDB configuration (PD-18) | Product + dev | 0.5 d | #1 | Phase 3 entry condition |
| 8 | Write ADRs for D-01–D-13, AM-01–AM-05, E-14–E-16 | Dev | 1 d | #1, #5, #6 | `Docs/decisions/` records |
| 9 | Provision the target MariaDB and Unraid template | Operator + ops | 0.5 d | #4 | Deployment artifacts |

**Immediate next three:** approve/reject PD-10–PD-18 (#1), review and authorize the next phase (#2), and add the two real exports (#3). Phase 2 will not start until the owner reviews this delivery.

---

## 8. Exit criteria and next-phase gate

The owner-approved Phase 1 is complete: the skeleton, SQLite schema, setup
wizard, administrator/session/authentication layer and settings persistence
are implemented and validated. `ImplementationPlan.md` labels the equivalent
identity work Phase 2; the owner's numbering governs this report.

Before the next phase starts:

- Review and approve/reject PD-10–PD-18.
- Review the Phase 1 implementation and record a go/no-go.
- Keep the two real collector exports and `Items` provisioning on the critical
  path for imports/inventory; they are not Phase 1 prerequisites.
- For `ImplementationPlan` Phase 3 (MariaDB guild store, audit and settings
  adoption), resolve E-09/E-11/E-14/E-15 and decide PD-18.
- `Design.md` v1.6.1 and the D-04 sample remain the published baseline.

No Phase 2 work will start until the owner completes the review.

---

## 9. Assumptions and limitations of this document set

1. All statements are derived from `Design.md` v1.6.1; where the specification is silent, the gap is recorded rather than silently resolved.
2. Line references (`L###`) are valid for the body (L1–L854) and the decisions (L855–L1161) and were re-verified at the cited anchors; the v1.6.1 amendments are cited as `AM-xx` because they are append-only at L1163–L1263. Decisions D-01–D-13 have no per-decision line anchors (IC-26). Any further edit to `Design.md` requires re-checking.
3. The documents introduce **PROPOSED** schema elements and defaults. These are recommendations, not approved requirements, until the decision log is signed.
4. Application code now covers the Phase 0 skeleton and Phase 1 identity/settings layer, including migrations and security/API tests. Inventory, import, audit, user-management and MariaDB business tests do not yet exist; effort estimates remain planning ranges, not measured velocity.
5. The review assumes a single Go service with server-rendered HTMX, Gin and SQLite/MariaDB as specified. Phase 1 used plain embedded forms and vanilla JS rather than HTMX/Bootstrap (PD-12); the layer boundaries still match the specification.
6. Security findings are design-level. A penetration test and dependency scanning are still required before production use.
7. Estimates assume one experienced developer and exclude waiting time for external data (`Items`, export files, Unraid access).

---

## 10. Change log

| Version | Date | Change |
|---|---|---|
| 1.0 | 2026-11-20 | Initial status report; four documents issued; 8 blockers, 12 open decisions |
| 1.1 | 2026-11-20 | Re-baselined on `Design.md` v1.6; D-01–D-13 adopted; engineering decisions renumbered E-xx; 4 blockers closed, 4 partial/open, 2 new (B-9, B-10) |
| 1.2 | 2026-11-20 | Re-baselined on `Design.md` v1.6.1; AM-01–AM-05 adopted; B-2, B-9, B-10 closed; E-13 resolved; 3 blockers remain partial |
| 1.3 | 2026-11-21 | Phase 0 skeleton implemented: Go module, package layout, config, logging, SQLite/MariaDB layers, health endpoints, Docker/Compose, CI, tests; `ProjectStructure.md` added; ADRs, E-14/E-15/E-09 and real exports remain open |
| 1.4 | 2026-11-21 | PD-05 approved as temporary MariaDB bootstrap configuration (Settings become authoritative after the Setup Wizard); `/config` named-volume persistence verified across container recreation |
| 1.5 | 2026-11-21 | Phase 1 implemented: SQLite migrations (Users, Settings, Sessions), four-step setup, bcrypt administrator, login/logout, hashed server-side sessions, CSRF, login lockout, default-deny admin middleware, settings persistence/redaction and embedded screens; live Compose/MariaDB flow and restart verified; PD-10–PD-18 await approval |

---

## 11. Status summary

The GuildBank specification is **buildable as written**. v1.6 closed the audit schema, the selection route, the HTMX/REST layering model, spell merge and item identity, and defined session storage, the audit table and store separation. v1.6.1 closed the two remaining contradictions: the canonical sample lives at `Docs/Samples/InventoryExport-Sample.txt` with a normative contract (AM-03), and missing metadata is representable by preserving the imported `EQUID` and setting `MetadataMissingFg`, with no sentinel and no enforced FK for unresolved rows (AM-01/AM-02). Setup recovery, audit taxonomy, location vocabulary, selection payload, warning sink and session-policy approval remain soft gaps.

The platform skeleton and the owner-approved Phase 1 identity/settings layer are implemented and validated: `go build`, `go vet`, `go test ./...`, SQLite migration/rollback tests, service security tests, API flow tests, live Compose app + MariaDB, four-step setup, login/logout, settings update, `410` setup closure, `401` after logout and restart persistence. Users, Settings and Sessions exist under `user_version=1`; passwords are bcrypt hashes and only session-token hashes are stored.

No inventory, import, audit, user-management, dashboard or MariaDB business functionality exists. Phase 1 decisions PD-10–PD-18 require owner approval. PD-05 remains the runtime MariaDB bootstrap until Phase 3; the wizard persists those settings but does not yet swap the live pool. Phase 2 will not begin until the owner completes the Phase 1 review.