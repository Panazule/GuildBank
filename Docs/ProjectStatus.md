# Project Status — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | PS-001 |
| As-of date | 2026-11-21 |
| Source specification | `Design.md` v1.6.1 (1,263 lines: body L1–L854, decisions D-01–D-13 at L855–L1161, amendments AM-01–AM-05 at L1163–L1263) |
| Repository state | Phase 0 skeleton: Go service, configuration, logging, SQLite and MariaDB connection layers, health endpoints, Docker/CI; no business features |
| Documents issued | `ArchitectureReview.md` (AR-001), `DatabaseDesign.md` (DD-001), `ImplementationPlan.md` (IP-001), `ProjectStructure.md` (PS-002), this status report |
| Overall status | **v1.6.1 published; Phase 0 skeleton complete; business features and migrations not started** |

---

## 1. As-is snapshot

| Area | State | Evidence |
|---|---|---|
| Requirements | Written; v1.6.1 amendments appended; canonical import sample at its declared path | `Design.md` v1.6.1 |
| Architecture review | Complete, revised | AR-001 rev 1.2: 22 missing requirements, 25 conflicts, 22 security risks, 28 implementation concerns, D-01–D-13 and AM-01–AM-05 adopted, E-01–E-16 registered |
| Data design | Complete, with proposed additions pending approval | DD-001 rev 1.2: DDL for 4 SQLite tables (1 proposed), 5 MariaDB tables (`Audit` specified by D-10, `MetadataMissingFg` by AM-02) |
| Delivery plan | Complete | IP-001: 9 phases, estimates, test and deployment strategy |
| Source code | Phase 0 skeleton present | `cmd/guildbank`, `internal/{domain,service,repository,api,web,platform}`; no business logic |
| Build/CI | Complete for the skeleton | `go.mod`/`go.sum` (Go 1.27.3), `Makefile`, `.github/workflows/ci.yml`, `Dockerfile`, `docker-compose.yml` |
| Environments | Development topology defined | Compose app + MariaDB 11.4; disposable credentials; no production environment |
| Test assets | Canonical import sample plus skeleton unit tests | `Docs/Samples/InventoryExport-Sample.txt` (548 rows); tests for config, logging, app, sqlite, mariadb, service, api, web |
| Deployment | Configured, not deployed | Multi-stage static image, non-root UID 10001, `/config` volume, `EXPOSE 8080`, healthcheck `/healthz` |
| Operations | Health and logs implemented; backup/support pending | `/healthz`, `/readyz`, JSON logs with request IDs, graceful shutdown with WAL checkpoint |

**Maturity by capability** (specification and implementation are tracked separately, because a well-specified feature can still be unimplemented):

| Capability | Specification | Implementation | Blocker |
|---|---|---|---|
| Setup wizard | Partial — gate fixed by D-02; recovery undefined | Not started | E-02, DC-24 |
| Authentication/sessions | Partial — storage fixed by D-09; lifecycle open | Not started | E-08, SR-22 |
| Public inventory browsing | Defined | Not started | E-10, E-12 |
| Class/race decoding | Defined with one fixture | Not started | — |
| Inventory administration | Partial — provisioning unowned | Not started | E-01 |
| Import pipeline | **Specified** — D-04/AM-03 contract, D-08/AM-01/AM-02 metadata rule | Not started | B-4 (edge cases only) |
| Spell merge/lifecycle dates | Defined by D-06; provenance open | Not started | DC-21 |
| Settings | Partial — types/secrets undefined | Not started | E-11 |
| User management | Partial — policy/last-admin rules | Not started | E-07 |
| Audit | Schema defined by D-10; taxonomy open | Not started | DC-25, E-09 |
| Deployment/operations | Partial — no health/backup/TLS rules | Not started | — |

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
| Dockerfile / Compose | Complete; image build and container probe verified | Multi-stage, static binary, UID 10001, `/config` writable, `EXPOSE 8080`, healthcheck; Compose app + MariaDB 11.4 |
| Build/run instructions | Complete | `README.md`, `Makefile`, CI workflow |
| Tests | Complete for skeleton | `go build`, `go vet`, `go test ./...` pass; runtime endpoint probes pass |
| ADRs for D-01–D-13, AM-01–AM-05, E-14–E-16 | **Not started** | Planned under `Docs/decisions/`; no files created |
| E-14 warning sink, E-15 selection payload, E-09 audit taxonomy closure | **Open** | Product/engineering decisions still required before Phases 3 and 6 |
| Two real collector exports | **Open** | External input; canonical sample is tracked |

Known deliberate omissions: no migrations, no setup/auth/session routes, no
import or inventory services, no audit recorder, no HTML UI, and no automated
MariaDB integration test (connectivity was validated manually against a
container; Phase 3 adds the automated integration suite).

---

## 2. Documents produced in this review

| Document | Contents | Key outputs |
|---|---|---|
| `Docs/ArchitectureReview.md` | Conformance assessment, findings, decisions, v1.6 disposition | MR-01–MR-22, DC-01–DC-25, SR-01–SR-22, IC-01–IC-28, A-01–A-12, E-01–E-16, D-01–D-13 mapping |
| `Docs/DatabaseDesign.md` | Physical schema, constraints, transactions, grants, retention | SQLite DDL (Users, Settings, Sessions, AuditOutbox), MariaDB DDL (Items, GuildBankItems, ImportBatch, ImportList, Audit), 9 workflow specifications, D-01–D-13 conformance |
| `Docs/ImplementationPlan.md` | Phased delivery, API surface, audit taxonomy, testing, deployment | 9 phases (58–84 engineer-days with contingency), route table, D-10 event taxonomy, fixtures, DoD, risk register |
| `Docs/ProjectStatus.md` | This report | Traceability, blockers, next actions |

No application functionality beyond the platform skeleton exists. Phase 0 created the buildable service, configuration, logging, both connection layers, health endpoints, deployment files and tests; the full map is in `Docs/ProjectStructure.md`.

---

## 3. Blocking items

| ID | Blocker | Decision / amendment | Status | Consequence / next action |
|---|---|---|---|---|
| B-1 | `Audit` has no schema | D-10 | **Closed** | Taxonomy/retention still open (DC-25) |
| B-2 | Import file format undefined | D-04, AM-03 | **Closed** | Sample at `Docs/Samples/InventoryExport-Sample.txt`; delimiter, header, mapping, UTF-8, variable-width rows and row rejection fixed |
| B-3 | No endpoint to persist `SelectedFg` | D-11 | **Closed** | Payload semantics open (DC-22) |
| B-4 | Location normalization is examples only | D-05 | **Partial** | Unknown patterns and case/whitespace remain (DC-20); `Hoard` settled by AM-04 |
| B-5 | `Items` provisioning; `EQUID` vs `Items.ID` | D-01 | **Partial** | Identity fixed; loader ownership absent (MR-21) |
| B-6 | Setup mode has no explicit state | D-02 | **Partial** | Missing `Users`, all-admins-disabled lockout and wizard ordering (DC-24) |
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
| FR-14 | Manage settings | L35, L190–L214 | Partial (types/secrets) | Not started | DD §3.2, §7.6 |
| FR-15 | Manage users | L43, L756–L763 | Partial (policy/delete) | Not started | DD §3.1, §7.7, IP Phase 7 |
| FR-16 | Audit trail (items, imports, users, settings, auth) | L773–L783; D-10 | Defined (taxonomy open) | Not started | DD §4.5, IP §5 |
| FR-17 | First-run wizard (4 steps) | L130–L164; D-02 | Partial (recovery) | Not started | DD §7.1, IP Phase 2 |
| FR-18 | Public + six admin screens | L805–L825 | Partial (dashboard contents) | Not started | IP §4, Phases 4–7 |
| FR-19 | Future external API/tool support | L729–L734 | Defined (intent only) | Not started | AR IC-02, IP §4 |

### 4.2 Data and non-functional requirements

| ID | Requirement | Spec refs | Spec status | Implementation | Covered in |
|---|---|---|---|---|---|
| DR-01 | SQLite for users/settings/sessions | L69–L107, L166–L231 | Defined | Not started | DD §3 |
| DR-02 | MariaDB for guild data | L69–L107, L233–L605 | Defined | Not started | DD §4 |
| DR-03 | `Items` read-only | L242–L244 | Defined (unenforced) | Not started | DD §4.1, §10.1 |
| DR-04 | Decoded class/race strings stored | L285–L309, L846–L848 | Defined | Not started | DD §9.6 |
| DR-05 | Lifecycle dates stored permanently | L314–L330 | Defined (conflicts with edits) | Not started | AR DC-06, DD §4.2 |
| DR-06 | Soft delete only | L359–L368 | Defined | Not started | DD §4.2, §7.3 |
| DR-07 | Cross-store actor identity | L370–L374, L538–L542 | Defined | Not started | DD §5–§6 |
| NR-01 | Layered architecture enforcement | L109–L129 | Defined | Not started | AR §3, IP Phase 1 |
| NR-02 | bcrypt password storage | L181–L183, L798 | Partial (cost/policy) | Not started | AR SR-12 |
| NR-03 | Session expiration | L216–L227, L799–L802 | Partial (timeouts) | Not started | AR SR-03, DD §3.3 |
| NR-04 | CSRF protection | L801 | Partial (mechanism) | Not started | AR SR-05, IP Phase 2 |
| NR-05 | Login rate limiting | L802 | Partial (thresholds) | Not started | AR SR-06, IP Phase 2 |
| NR-06 | Secure cookies / HTTPS | L800 | Partial (transport) | Not started | AR SR-11 |
| NR-07 | Administrator-only routes | L43, L804 | Partial (matrix) | Not started | AR SR-04, IP §4 |
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
| E-07 | Authorization matrix/projections | Open | Public read subset; admin-only writes |
| E-08 | Session lifecycle | Open | 8 h absolute, 30 min idle, rotation, purge |
| E-09 | Audit taxonomy closure | Open | D-10 names closed; extensions approved |
| E-10 | Date comparison semantics | Open | `DateOpen <= today` means open; UTC |
| E-11 | Settings validation/secrets | Open | Typed settings; redaction; volume-permission reliance documented |
| E-12 | API versioning/pagination | Open | `/api/v1`; max page 100 |
| E-13 | Missing-metadata strategy | **Resolved** | AM-01/AM-02: preserve id, set `MetadataMissingFg`, relax FK |
| E-14 | Warning sink | Open | `ImportList.WarningText` + batch counts |
| E-15 | Selection payload contract | Open | Full set; batch-scoped; `422` foreign ids |
| E-16 | HTMX error/auth semantics | Open | `HX-Redirect` on 401; fragment on 403 |

D-01–D-13 are specification decisions and do not require engineering approval; the E-items are proposals until signed.

---

## 7. Next actions

| # | Action | Owner | Effort | Depends on | Output |
|---|---|---|---|---|---|
| 1 | Sign off the revised documents and residual decisions E-14–E-16 | Product owner | 0.5 d | — | Signed decision log |
| 2 | Add two real collector exports beside the canonical sample; commit the repository (currently no commits) so the sample and fixtures are tracked | Guild admin + dev | external | — | Import fixtures (closes E-04) |
| 3 | ~~Amend D-08~~ — done: AM-01/AM-02 published | Product + dev | — | — | Signed amendment (closes B-9) |
| 4 | ~~Publish `Design.md` v1.6.1~~ — done: title, `Hoard`, amendments AM-01–AM-05, change log | Dev | — | — | Corrected specification (closes B-10) |
| 5 | Define the `Items` loader procedure and load a development dataset | Operator | 1–2 d | — | Populated `Items`, runbook |
| 6 | Provision MariaDB (dev and target) with the grant matrix from DD §10.1 | Operator | 0.5 d | #5 | Connection details, grants |
| 7 | Close the audit taxonomy (E-09) and define the warning sink (E-14) | Dev + product | 0.5 d | #1 | Closed allowlist, warning field |
| 8 | Freeze the API route table (including `PUT .../selection`) and HTMX error semantics | Dev | 1 d | #2, #7 | `Docs/API.md` |
| 9 | Scaffold Phase 1: module, packages, config, logging, health, CI, Dockerfile | Dev | 3–5 d | #4, #8 | Deployable skeleton |
| 10 | Commit decoder/location/import/merge/settings fixtures and test harness | Dev | 1 d | #2 | Fixture repository |
| 11 | Build the Unraid template and compose sample | Dev + ops | 0.5 d | #9 | Deployment artifacts |
| 12 | Sign off session lifecycle, CSRF coverage, rate-limit and secret-handling parameters | Dev + product | 0.5 d | #1 (E-08, E-11) | Security configuration record |

**Immediate next three:** obtain two real exports (#2), assign the `Items` loader (#5), and sign off the residual decisions E-14–E-16 (#1). The specification defects are closed, so implementation is no longer blocked on documentation.

---

## 8. Exit criteria for starting implementation

Implementation of Phase 1 may begin when (the platform skeleton has already been built under the owner's Phase 0 directive; the Phase 1 exit criteria below still apply):

- The residual engineering decisions E-14–E-16 are decided; D-01–D-13 and AM-01–AM-05 are already adopted.
- `Design.md` v1.6.1 is published with the amendments and change log applied.
- The D-04 sample is present at its declared path; two real exports are desirable before Phase 6 but no longer block Phase 1.
- A development MariaDB instance and the `Items` dataset are reachable from the dev environment.

Implementation of Phases 2–3 additionally requires E-08/E-10/E-11/E-12 and a populated `Items` table. Phase 6 cannot begin until two real exports confirm the AM-03 contract.

---

## 9. Assumptions and limitations of this document set

1. All statements are derived from `Design.md` v1.6.1; where the specification is silent, the gap is recorded rather than silently resolved.
2. Line references (`L###`) are valid for the body (L1–L854) and the decisions (L855–L1161) and were re-verified at the cited anchors; the v1.6.1 amendments are cited as `AM-xx` because they are append-only at L1163–L1263. Decisions D-01–D-13 have no per-decision line anchors (IC-26). Any further edit to `Design.md` requires re-checking.
3. The documents introduce **PROPOSED** schema elements and defaults. These are recommendations, not approved requirements, until the decision log is signed.
4. No application code, migrations or tests exist; effort estimates are planning ranges, not measured velocity.
5. The review assumes a single Go service with server-rendered HTMX, Gin and SQLite/MariaDB as specified. Alternative stacks would invalidate the package layout and some mitigation details, but not the findings.
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

---

## 11. Status summary

The GuildBank specification is **buildable as written**, and the platform skeleton now builds, starts, logs, serves health endpoints and opens both stores. v1.6 closed the audit schema, the selection route, the HTMX/REST layering model, spell merge and item identity, and it defined session storage, the audit table and store separation. v1.6.1 closed the two remaining contradictions: the canonical sample lives at `Docs/Samples/InventoryExport-Sample.txt` with a normative contract (AM-03), and missing metadata is representable by preserving the imported `EQUID` and setting `MetadataMissingFg`, with no sentinel and no enforced FK for unresolved rows (AM-01/AM-02). Setup recovery, audit taxonomy, location vocabulary, selection payload, warning sink and session lifecycle remain soft gaps with recorded recommendations.

Documentation is consistent and the Phase 0 skeleton is implemented and validated (`go build`, `go vet`, `go test ./...`, runtime `/healthz` and `/readyz`, Compose config, container image). No user management, authentication, import, inventory, audit or UI behaviour exists, and no schema migration has been written; those remain Phase 2 onward.

The project is not blocked on specification defects. Remaining prerequisites are operational (`Items` provisioning, two real collector exports, a development MariaDB) and product decisions (E-08, E-09, E-11–E-16), not engineering capacity. Approval is required before Phase 1 proceeds, including the provisional MariaDB environment bootstrap (PD-05) and the residual E-14/E-15/E-09 contracts.