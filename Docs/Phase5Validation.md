# Phase 5 Validation Evidence

**Date:** 2026-11-22 UTC
**Scope:** Administrative inventory management (migration `0003`, startup backfill, admin HTML/JSON routes, audit viewer, owner UI amendments `PD5-33`–`PD5-39`, owner boundary decision `PD5-40`)
**Status:** Implemented and live-validated against the Compose development topology, including the inclusive `Bazaar` boundary of `PD5-40`. Performance gates at 250,000/1,000,000 rows and the opt-in database integration suite remain outstanding.

## 1. Approval applied

The owner approved `PD5-01`–`PD5-39` and the §16.6 date interpretations (a), (b) and (c), reaffirmed the lifecycle date rule (`PD5-39`), accepted `PD-05`, `PD-18`, `E-02` and the `AM-02` refresh residual as out of scope for Phase 5, and then approved Phase 5 with one further decision, `PD5-40` (availability thresholds are inclusive, so `Bazaar` begins on the `DateBazaar` day). Implementation status is recorded in `Docs/ProjectStatus.md` §1.7 and the decision record in `Docs/Phase5ImplementationPlan.md` §3 and §9.5.

## 2. Environment and deployment sequence

| Item | Value |
|---|---|
| Host | Windows development workstation, Docker Desktop 29.9.2 |
| Application image | `guildbank:dev` rebuilt from the Phase 5 working tree (`dev-compose`) |
| MariaDB | `mariadb:11.4` Compose service, healthy before and after the change |
| Previous schema | `0002_public_inventory_indexes`, 706 `GuildBankItems` rows, 8 `Audit` rows |
| SQLite | `guildbank_config` volume removed to exercise a fresh setup; MariaDB volume kept so `0003` and the backfill ran against pre-existing rows |

Deployment followed §14.2 exactly: `docker compose down`, config volume removed, image rebuilt, `docker compose up -d`. The application applied `0003`, ran the backfill before serving and reported healthy afterwards; no manual schema edit was made.

State left behind deliberately: the 11 rows created during the smoke run were soft-deleted through SQL (`DeletedFg=1`, `ModBy='phase5-cleanup'`) so the public list is back to the 706 pre-validation rows, and `GuildOpenDays`/`BazaarDays` were reset to `14`/`30` through the settings API. Their 19 admin audit rows (11 `ITEM_CREATED`, 6 `ITEM_UPDATED`, 2 `ITEM_REMOVED`) and the 4 pre-existing import rows (23 total) were kept, because the audit trail is append-only. The recreated SQLite volume holds the disposable `phase5admin` account used for the run.

The `PD5-40` boundary check later added three more probe rows (`GuildBankItemID` 718–720) through the admin form and removed each one through the remove form, so those probes also left their own evidence: `ITEM_CREATED`/`ITEM_REMOVED` pairs under `ModBy='phase5admin'` and `DeletedFg=1` on all three rows. Final state after both runs: **706 active rows**, `MAX(AuditID)=29`, 29 audit rows in total (`14` `ITEM_CREATED`, `6` `ITEM_UPDATED`, `5` `ITEM_REMOVED`, `2` `IMPORT_CREATED`, `2` `IMPORT_COMMITTED`).

## 3. Migration and backfill evidence

Observed log lines:

```text
guild store schema ready  schema_version=0003_admin_inventory
location backfill starting
location backfill complete  rows=706
```

Database state after startup, compared with the pre-deployment capture:

| Check | Before | After |
|---|---|---|
| `SELECT MAX(Version) FROM schema_migrations` | `0002_public_inventory_indexes` | `0003_admin_inventory` |
| `LocationGroup` / `LocationSort` columns | absent | present (`varchar(64)`, `varchar(80)`) |
| Rows with null derived location | n/a | `0` |
| `MAX(ModAt)` on `GuildBankItems` | `2026-11-22 03:05:23.040333` | `2026-11-22 03:05:23.040333` (unchanged) |
| `MAX(AuditID)` | `8` | `8` (backfill wrote no audit rows) |
| CRC32 signature over id/EQUID/name/dates/quantity/location/`ModAt` | `1437834220594` | `1437834220594` (no business data rewritten) |

The backfill therefore wrote only the two derived columns, preserved `SourceLocation` and `ModAt`, wrote no audit rows and is safe to re-run (a second application start produced the same state).

## 4. Schema and index evidence

`SHOW INDEX FROM GuildBankItems` confirms the three admin indexes and the audit correlation index with the planned column order:

| Index | Columns |
|---|---|
| `ix_gbi_admin_location` | `DeletedFg, LocationGroup, LocationSort, NameSort, GuildBankItemID` |
| `ix_gbi_admin_modified` | `DeletedFg, ModAt, GuildBankItemID` |
| `ix_gbi_admin_type_modified` | `DeletedFg, ItemType, ModAt, GuildBankItemID` |
| `ix_audit_correlation` | correlation-ID lookup for the audit viewer |

`EXPLAIN` for the §10.2 shapes (development dataset, 717 rows):

| Shape | Key | Extra |
|---|---|---|
| Default active list, `ModAt DESC` | `ix_gbi_admin_modified` | `Using where; Using index` (no filesort) |
| `LocationGroup` filter, grouped name sort | `ix_gbi_admin_location` | `Using where; Using index` (no filesort) |
| `ItemType` filter, `ModAt DESC` | `ix_gbi_admin_type_modified` | `Using where; Using index` |
| Exact `GivenTo` filter | `ix_gbi_given` | `Using index condition; Using where` |
| Name search `LIKE '%cap%'`, `ModAt DESC` | `ix_gbi_admin_modified` (range) | `Using where`; documented scan, no filesort |
| `DateBazaar DESC` | `ix_gbi_public_bazaar` | `Using where; Using index` |
| Audit `RecordID` filter | `PRIMARY` | `Using where`; documented scan (no `RecordID` index in this phase) |
| Audit date-only filter | `PRIMARY` chosen over `ix_audit_time` | `Using where`; documented broad scan |

## 5. Functional smoke results

All requests were made over HTTP against the running Compose stack with a fresh administrator (`phase5admin`), a CSRF token and a session cookie.

| Scenario | Result |
|---|---|
| `GET /admin/inventory`, `/admin/inventory/new`, `/{id}`, `/{id}/edit`, `/admin/audit` | `200`, server-rendered pages |
| `POST /admin/inventory` with `Item`, EQUID `25210`, quantity `2` | `303` to `/admin/inventory/709?notice=created`; two rows created with quantity `1` each |
| Derived location on create: `General5-Slot1` | `locationGroup=General 5`, natural-sort `LocationSort` populated |
| Default dates on create (Settings `GuildOpenDays=14`, `BazaarDays=30`) | `DateAdded=2026-11-22`, `DateOpen=2026-12-06`, `DateBazaar=2026-12-22` |
| `PUT /api/v1/items/{id}` with `quantity=2` on an `Item` row | `422`, field `quantity` “an item record must keep quantity 1”; row unchanged |
| `PUT /api/v1/items/{id}` with overrides `dateOpen=2026-12-05`, `dateBazaar=2027-01-15` | `200`; values stored verbatim, new `modAt` returned |
| `PUT` with `dateOpen=2028-01-01` against `dateBazaar=2027-01-15` | `422`, field `dateOpen` “cannot be after the other lifecycle date”; nothing written |
| `POST /admin/inventory/{id}` (HTML edit posting `quantity=1`) | `303 ...?notice=unchanged` when values matched; `?notice=updated` on a real change |
| `POST /admin/inventory/{id}/remove` | `303 ...?notice=removed`; `deleted=true`; public list excludes the row |
| `POST /api/v1/items/{id}/restore?expectedModAt=…` | `200`; `deleted=false` |
| Missing/stale `expectedModAt` | `422` with an `expectedModAt` field error |
| Audit for the record | `ITEM_CREATED` plus update/remove/restore events; the per-record history panel renders them |
| `GET /api/v1/audit?pageSize=200` | `200`; `meta.total=25`, `eventType` and `userId` present, `{}`/JSON `old`/`new` payloads |
| JSON create with `sourceLocation=WeirdZone-Slot1` | `201`; `locationGroup=Unspecified`; `warnings[0].code=location_unrecognized` |
| Recognized groups | `General n`, `Bank n`, `SharedBank n`, `Hoard`; `Bazaar1-Slot3` correctly falls to `Unspecified` (bazaar is an availability state, not a bank location) |
| Settings changed to `GuildOpenDays=21`, `BazaarDays=45` | Existing row dates and `modAt` unchanged (non-retroactive); a new create stored `DateOpen=2026-12-13`, `DateBazaar=2027-01-06` |
| Availability badge on the admin detail page | `upcoming / Mains` for a future window; `open / Persona` while `DateOpen <= today < DateBazaar`; `bazaar / Bazaar` from the `DateBazaar` day on (`PD5-40`) |

## 6. UI amendment verification

Rendered HTML and CSS were captured from the live stack: the public list and detail pages, the admin list, new form, detail page, edit form and audit page.

| Amendment | Evidence |
|---|---|
| `UI-01` responsive width | `main.card.inventory-card` and the admin cards use `width: min(120rem, calc(100% - 1.5rem))`; `clamp()` padding retained; 900 px breakpoints keep the layout usable |
| `UI-02` grid columns | The public list has no `EQUID` or `ID` header; the admin grid keeps both, because they are its identity fields |
| `UI-03` compact filters | `.filter-grid` (public) and `.admin-filter-grid` (admin/audit) render one compact multi-column filter grid per screen, collapsing to two columns and then one |
| `UI-04` EQResource link | Icon-only anchor with `aria-label`, `title`, `rel="noopener noreferrer"` and `target="_blank"` |
| `UI-05` density | `.cell-nowrap` applies `white-space: nowrap` above 900 px; the failure column is capped with ellipsis |
| `UI-06` dates | Detail page shows `Date added`, `Open to personas` and `Open to bazaar` with ISO `datetime` values plus human text; the edit form posts two `type="date"` inputs and a `readonly` quantity |
| `UI-07` availability | Badge text `Mains`/`Persona`/`Bazaar` derives from `service.Availability`; the manual captures show `Mains`, `Persona` and, after `PD5-40`, `Bazaar` on the `DateBazaar` day (§11) |

## 7. Performance observations

Measured on the development dataset (717 active rows, warm pool, 15 sequential requests per endpoint):

| Endpoint | min | median | max |
|---|---:|---:|---:|
| `/admin/inventory` | 3.74 ms | 4.38 ms | 17.20 ms |
| `/admin/inventory?q=cap` | 3.24 ms | 3.81 ms | 4.90 ms |
| `/admin/inventory?locationGroup=General+5` | 2.73 ms | 3.00 ms | 3.39 ms |
| `/api/v1/admin/items/710` | 1.32 ms | 1.52 ms | 2.06 ms |
| `/admin/audit` | 2.20 ms | 2.58 ms | 3.21 ms |
| `/api/v1/audit?pageSize=50` | 2.21 ms | 2.38 ms | 3.65 ms |
| Public `/` | 3.44 ms | 4.24 ms | 13.20 ms |

These numbers confirm the query plans and the absence of pathological template work on a small dataset. **They are not the §10.1 gates.** The 250,000-row and 1,000,000-row runs (`GUILDBANK_PHASE5_PERF`), the backfill throughput benchmark and the 100,000-unit create benchmark were not executed because no Phase 5 performance fixture exists yet; the plan’s follow-up index decisions depend on those runs.

## 8. Automated gates

Commands run from the repository root:

```text
gofmt -l cmd internal   # no output
go vet ./...            # exit 0
go test -count=1 ./...  # every package ok
```

The same three commands were re-run after the `PD5-40` boundary change on 2026-11-22: `gofmt -l cmd internal` listed no files, `go vet ./...` exited `0`, and `go test -count=1 ./...` reported `ok` for `internal/api`, `internal/integration`, `internal/platform/app`, `internal/platform/config`, `internal/platform/logging`, `internal/repository/mariadb`, `internal/repository/sqlite`, `internal/service` and `internal/web` (`cmd/guildbank` and `internal/domain` have no test files). `go test ./internal/service/ ./internal/api/` was also run alone before the full sweep and passed.

`go test -race ./...` cannot run in this workspace (`-race` requires cgo and no C compiler is installed), so the race gate is delegated to CI and is not claimed as locally verified. The opt-in suites `GUILDBANK_INTEGRATION=1` (§13.2) and `GUILDBANK_PHASE5_PERF=1` (§13.3) are not written; `internal/integration` reports `ok` for its existing tests only.

The migration manifest test passes with the pinned hashes for `0001_guild_store.sql`, `0002_public_inventory_indexes.sql` and `0003_admin_inventory.sql`, confirming the embedded set is exactly three migrations with no second copy of `0003`.

## 9. Findings, deviations and follow-ups

1. **Availability boundary (resolved by `PD5-40`, 2026-11-22).** `PD5-37`/`UI-07` describe `Bazaar` as starting “when the open-to-bazaar date is reached”, while the Phase 4 `service.Availability` comparison returned `bazaar` only for `today > DateBazaar` and therefore rendered `Persona` on the `DateBazaar` day itself. The owner ruled for inclusive thresholds — `Mains` while `today < DateOpen`, `Persona` while `DateOpen <= today < DateBazaar`, `Bazaar` when `today >= DateBazaar` — which aligns `DateBazaar` with the already-inclusive `DateOpen` and with the meaning of “open to the bazaar”. The change is one comparison in `internal/service/inventory.go` plus the tests and documentation that pinned the old value; the state names, JSON field, routes, filters, stored dates and label mapping are unchanged. The only behavioural consequence is that the public `availability` value on the `DateBazaar` day itself moves from `open` to `bazaar`; `PD5-37`'s “no public JSON change” clause is superseded on that single day. Equal `DateOpen`/`DateBazaar` values now have no `Persona` day, which follows directly from the owner's formula. Evidence is in §11.
2. **Backfill progress logging.** The per-10,000-row progress line named in §14.2 is not emitted; start and completion lines with the row count are. Batch size, advisory lock, resumability and idempotence match the plan.
3. **Route authorization matrix test.** The admin routes sit behind the existing admin group (`RequireAuth → CSRF → RequireAdmin`) and were exercised as an authenticated administrator only. The §13.1 anonymous/member/admin route matrix test is not yet added.
4. **Database integration and performance suites.** §13.2 and §13.3 remain unwritten, as do the §10.1 measurements.
5. **Manual acceptance (§13.5).** Keyboard-only flow, screen-reader error labels, 320 px layout behaviour and confirmation-page focus order were not executed in this environment; the CSS and markup that support them were inspected, which is not a substitute for the checklist.
6. **Documentation corrections (§12.5)** in the downstream documents are still outstanding.

## 10. Conclusion

Phase 5 is functionally complete and verified end to end on the Compose development topology: migration and backfill, admin create/edit/remove/restore with audit, the JSON contract behaviour, the lifecycle date rule, location normalization, the audit viewer, every owner UI amendment that new rows can exercise, and the inclusive `Bazaar` boundary of `PD5-40`. The remaining items are the §10.1 performance gates, the opt-in §13.2/§13.3 suites, the CI race run and the §13.5 manual checklist; none of them reopens the approved design.

## 11. Inclusive `Bazaar` boundary (`PD5-40`)

The owner decision of 2026-11-22 replaced the Phase 4 upper-bound comparison with inclusive thresholds:

| State | Condition (unchanged names) |
|---|---|
| `upcoming` | `today < DateOpen` |
| `open` | `DateOpen <= today < DateBazaar` |
| `bazaar` | `today >= DateBazaar` and `today >= DateOpen` |

Implementation change: `Availability` in `internal/service/inventory.go` returns `domain.AvailabilityBazaar` when `!today.Before(dateBazaar)` after the open-date check, and the doc comment states the three intervals and the equal-date consequence. No template, handler, route, filter, DTO or schema changed, because the labels and colours are derived from the same state enum.

Unit coverage after the change:

| Test | Cases pinned |
|---|---|
| `service.TestAvailabilityUTCBoundaries` | day before open → `upcoming`; exact open → `open`; inside window → `open`; **day before bazaar (2026-12-20 23:59) → `open`; exact bazaar (2026-12-21 00:00 and 23:59) → `bazaar`**; day after → `bazaar`; non-UTC value normalized to the UTC date → `upcoming`; **equal `DateOpen`/`DateBazaar` on the shared date → `bazaar`** and the next day → `bazaar`; inverted window: before open → `upcoming`, on the open date → `bazaar`, after → `bazaar` |
| `api.TestAvailabilityOwnerBoundariesAsLabels` | the same five boundary days mapped through `availabilityLabel` to `Mains`, `Persona`, `Persona`, `Bazaar`, `Bazaar`, plus the equal-date case → `Bazaar` |

Live verification (image rebuilt with `docker compose build guildbank`, restarted, `2026-11-22` UTC, three rows created through the admin form and read back through the admin page, the public page and the public JSON, then removed through the remove form):

| Row | `DateOpen..DateBazaar` | Admin badge | Public JSON `availability` | Public page badge |
|---|---|---|---|---|
| 718 | 2026-11-22 .. 2026-11-22 | `bazaar` / `Bazaar` | `bazaar` | `bazaar` / `Bazaar` |
| 719 | 2026-11-22 .. 2026-11-23 | `open` / `Persona` | `open` | `open` / `Persona` |
| 720 | 2026-11-23 .. 2026-12-01 | `upcoming` / `Mains` | `upcoming` | `upcoming` / `Mains` |

Row 718 is the decisive case: its `DateBazaar` is the current UTC date, and all three renderings report `bazaar`/`Bazaar`, where the previous comparison produced `open`/`Persona`. Each removal returned `303` to `/admin/inventory/{id}?notice=removed`, the public lookup of a removed row returned `404`, and `/healthz` and `/` both returned `200` afterwards. Row 715 from the earlier smoke run (`DateOpen = DateBazaar = 2026-11-22`, soft-deleted) is the same shape and now also classifies as `bazaar` if it were restored.

Dataset impact: after the three probes were removed, all **706** active rows are `upcoming` today and classify identically under the old and new comparisons, so the change is data-neutral on the current dataset; only rows whose `DateBazaar` is today would move, and none exist in the stored baseline. Final stored state: 706 active rows, `MAX(AuditID)=29`, 29 audit rows (see §2).