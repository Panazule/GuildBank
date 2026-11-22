# Phase 4 Implementation Plan — Public Inventory Read Path

**Status:** **APPROVED AND IMPLEMENTED.** PD4-01–PD4-17 approved by the owner on 2026-11-21 subject to the documented performance, indexing and validation conditions. Phase 4 implementation was authorized and completed on 2026-11-22 UTC. Evidence is in `Docs/Phase4Validation.md`; Phase 5 remains unauthorized.
**Date:** 2026-11-21
**Revision:** 1.1 — approval record and implementation status added; no contract changes from revision 1.0
**Scope owner:** single developer
**Implementation-plan mapping:** this is the user's "Phase 4" (public inventory read path). It corresponds to `Docs/ImplementationPlan.md` Phase 4 and depends on the approved Phase 3 import pipeline and the implemented `GuildBankItems` schema.
**Companion documents:** `Design.md` v1.6.1, `Docs/ArchitectureReview.md`, `Docs/DatabaseDesign.md`, `Docs/ImplementationPlan.md`, `Docs/ProjectStatus.md`, `Docs/ProjectStructure.md`, `Docs/Phase3ImplementationPlan.md`.

**Normative inputs**

- `Design.md` L19–L30 (public capabilities), L261–L374 (`GuildBankItems`), L699–L728 (natural sorting), L736–L795 (REST and EQResource), L812–L825 (public screen), D-01–D-13 and AM-01–AM-05.
- `Docs/DatabaseDesign.md` §4.2 (`GuildBankItems` DDL, indexes and field semantics), §6 (integrity), §9 (query design and natural sort key).
- `Docs/ImplementationPlan.md` Phase 4, §4 (API surface and response conventions) and §6 (test strategy).
- `Docs/ArchitectureReview.md` E-07 (authorization matrix/public projection), E-10 (date comparison), E-12 (API versioning/pagination), MR-06, MR-07, MR-14, MR-19, DC-10, DC-13 and DC-17.
- The implemented Phase 3 importer, repository, API, templates and tests, which are the system of record for the current data shapes.

Where this plan proposes behavior not yet fixed by `Design.md`, it is marked **PROPOSED** and listed in §3 for explicit owner approval. **SPEC** means the behavior already exists in the specification. **BASELINE** means it is implemented in Phase 3 and is not being redesigned here.

---

## 1. Outcome, scope and acceptance criteria

When Phase 4 is complete, an anonymous visitor can:

1. Open the public guild inventory at `/` without authentication.
2. Search item names case-insensitively.
3. Filter by class, race and item type.
4. Sort by natural item name or an availability-related date.
5. Move through deterministic, bounded pages with a visible result count.
6. Open a public item-detail page for one inventory record.
7. See decoded class and race restrictions, quantity, inventory dates and a consistent availability state.
8. Open the item on EQResource in a new tab with safe link attributes.
9. Use the same data through `GET /api/v1/items` and `GET /api/v1/items/{id}`.
10. Never receive internal fields such as `DeletedFg`, `SourceLocation`, `GivenTo`, `ModBy` or `ModByUserID`.

Phase 4 is complete when the acceptance criteria in §1.2 and the performance/security/test gates in §8–§10 pass, the live Compose validation in §10.6 succeeds, and the required documentation is updated. No Phase 5 write path, admin inventory grid or item mutation is part of this phase.

### 1.1 Non-goals

- No manual add, update, remove or restore of inventory. Those are Phase 5.
- No admin inventory management grid, row actions or write endpoints.
- No new import behavior or change to the Phase 3 commit model.
- No change to the `Items` table, the external metadata loader or production grants (E-01 remains operational work).
- No location-normalization schema change unless the owner explicitly approves §3 PD4-17 and a later migration. Phase 3 stores `SourceLocation` only; `GuildBankItems` has no `LocationGroup`.
- No item grouping or aggregation by `EQUID`; per-instance lifecycle is preserved (§3 PD4-01).
- No availability-date filter in the first slice. Availability is displayed and returned as a state; filtering by lifecycle date is deferred (§3 PD4-17).
- No HTMX, Bootstrap, CDN, client-side framework or external JavaScript in Phase 4. The UI is server-rendered and progressively usable with plain GET requests (§3 PD4-14).
- No external network call to EQResource in production request handling or in tests. Links are constructed from stored settings only.
- No public write action, audit event, cache layer, background job or rate limiter beyond the existing controls.
- No Phase 5 implementation or authorization.

### 1.2 Acceptance criteria

1. `GET /` returns a public inventory page with no session cookie and without an authentication redirect.
2. `GET /api/v1/items` and `GET /api/v1/items/{id}` return only `DeletedFg = 0` records and only the public projection in §4.5.
3. Search, class, race and item-type filters combine with AND and return the same result set through the API and HTML pages.
4. Class and race filtering treats stored `ALL` as a match for every specific class/race, matches exact comma-separated tokens rather than arbitrary substrings, and never treats blank/missing metadata as `ALL`.
5. Natural sorting places, for example, `Item 2` before `Item 10`, applies the persisted `NameSort`, and uses `GuildBankItemID` as a deterministic tie-breaker.
6. Pagination is bounded, deterministic for a stable snapshot, returns consistent `total`/`totalPages`, and never returns more than the maximum page size.
7. Invalid sort keys, invalid class/race values, malformed pages and oversized page sizes return `422` with the existing error envelope.
8. A soft-deleted or absent `GuildBankItemID` returns `404`; it is absent from lists, counts, search and detail.
9. Availability states are derived from one UTC request date and match the inclusive boundary rules in §5.4.
10. EQResource links are built only from the validated server setting and include `target="_blank" rel="noopener noreferrer"`; no malformed or non-EQResource URL is ever rendered.
11. Anonymous responses never contain `DeletedFg`, `SourceLocation`, `GivenTo`, `ModAt`, `ModBy`, `ModByUserID`, `NameSort` or internal audit data.
12. The public inventory remains unavailable with a clear `503`-class page/API response when MariaDB is unreachable, and remains gated by setup until setup is complete.
13. The performance gates in §8.5 pass on the agreed dataset and hardware.
14. `go test ./...`, `go vet ./...`, `gofmt -l` and the repository's Markdown checks pass; no external network is used by tests.

---

## 2. Current-state facts the plan builds on

### 2.1 Phase 3 and schema baseline

- Phase 3 is approved, implemented and live-validated. It writes `GuildBankItems` atomically, expands ordinary items to one row per unit, merges spells by EQUID, preserves imported names when metadata is absent, and persists structured import warnings.
- `GuildBankItems` is the system of record for the public read path. Its current fields and indexes are fixed by migration `internal/repository/mariadb/migrations/0001_guild_store.sql` and `Docs/DatabaseDesign.md` §4.2.
- `ClassString` and `RaceString` are already decoded at import time. Public browsing must not re-run mask decoding or call `Items` for every row.
- `NameSort` is written by Phase 3 with `service.NaturalSortKey`, truncated to 160 runes. Production writes set it; it is nullable only for migration/backfill tolerance.
- `DateAdded`, `DateOpen` and `DateBazaar` are stored `DATE` values computed at creation and intentionally frozen. A later setting change does not rewrite existing rows.
- `DeletedFg` is the only removal mechanism. No public query may bypass it.
- The current indexes are:
  - `ix_gbi_equid (EQUID)`
  - `ix_gbi_public (DeletedFg, ItemType, Name)`
  - `ix_gbi_name_sort (DeletedFg, NameSort)`
  - `ix_gbi_dates (DeletedFg, DateBazaar, DateOpen)`
  - `ix_gbi_given (GivenTo)`

### 2.2 Implemented conventions to reuse

- Routes are built in `internal/api/router.go`. Public pages/API belong in the public group after `SetupGate`; admin routes stay behind `RequireAuth` → CSRF → `RequireAdmin`.
- Success envelopes already use `data` and optional `meta`; errors use `{"error":{"code","message","fields","request_id"}}` in `internal/api/errors.go`.
- `domain.ValidationError` maps to `422`, `domain.ErrNotFound` to `404`, and `domain.ErrUnavailable` to `503`.
- Templates are embedded `html/template` files, rendered through `internal/api/views.go`, with custom CSS and no external assets.
- The Phase 3 import list already demonstrates parameterized search with `escapeLike`, page/pageSize validation and the natural sort key.
- `SettingsService` validates `EQResourceBaseUrl` as HTTPS `items.eqresource.com/items.php?id=` and can be read through `Values`.
- `SecurityHeaders` currently sets a strict CSP, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` and `Cache-Control: no-store`.

### 2.3 Data-shape consequences

| Observation | Phase 4 consequence |
|---|---|
| One ordinary item unit = one `GuildBankItems` row | The public list represents records, not distinct item names. Duplicate instances can appear. |
| Spells merge and may have `Quantity > 1` | Quantity is displayed for spells; ordinary items remain one row each with quantity 1. |
| `ClassString`/`RaceString` are comma-separated strings | Exact token filtering uses `FIND_IN_SET` (or an equivalent comma-boundary predicate), not `LIKE '%WAR%'`. |
| `ClassString = 'ALL'`/`RaceString = 'ALL'` | A specific class/race filter must also include stored `ALL`. |
| Blank strings can mean a zero mask or missing metadata | Blank never matches a specific class/race filter; it is presented as unknown, not as unrestricted. |
| `MetadataMissingFg = 1` can coexist with a valid EQUID | The record remains public and linkable; restrictions are displayed as unavailable. |
| Dates are frozen `DATE` values | Availability is computed in Go from one UTC "today"; no SQL `CURDATE()` or per-row clock. |
| `SourceLocation` is raw and `LocationGroup` is not stored | Public location filtering is not possible without a schema/normalization decision; it is out of Phase 4. |
| `GivenTo` may contain a member name | It is not part of the public projection. |
| `NameSort` is designed for byte-order natural sorting but stored under `utf8mb4_unicode_ci` | Phase 4 integration tests must verify the database actually orders `General 2` before `General 10`; see risk P4-R10. |

---

## 3. Proposed decisions requiring approval

All rows below are **PROPOSED**. None is approved by this document. If the owner accepts the defaults in this section, the plan can proceed without further product decisions; if any row is changed, the API contract, tests and UI copy must be updated together before coding.

| ID | Proposal | Rationale and impact | Status |
|---|---|---|---|
| PD4-01 | **Inventory identity is `GuildBankItemID`.** The public list returns one row per active inventory record. Duplicate units of the same item and duplicate spell name/quantity are not grouped. Detail is addressed by `GuildBankItemID`, not `EQUID`. | This preserves D-07 per-instance lifecycle, frozen dates and future per-instance administration. Grouping would hide distinct availability dates and conflict with Phase 5 update/remove semantics. It means the public list can show visually repeated names. | **PROPOSED** |
| PD4-02 | **Public projection** contains only `id`, `equid`, `name`, `itemType`, `quantity`, `classString`, `raceString`, `metadataMissing`, `dateAdded`, `dateOpen`, `dateBazaar`, `availability` and `eqResourceUrl`. | Satisfies the public capability list and avoids exposing `SourceLocation`, `GivenTo`, `Mod*`, `DeletedFg`, `NameSort` and audit data. `metadataMissing` is included so blank restrictions can be explained without guessing. | **PROPOSED** |
| PD4-03 | **Public and authenticated visitors receive the same item projection on public routes.** An authenticated administrator gains only the existing admin navigation/actions; no Phase 4 route switches to an internal or soft-deleted dataset. The admin inventory grid and admin item DTO remain Phase 5. | Closes the immediate E-07/DC-10 risk without creating a second Phase 4 authorization surface. Response shape must not depend on a cookie, which keeps authorization and caching behavior simple. | **PROPOSED** |
| PD4-04 | **Routes:** public HTML list at `/`, detail at `/inventory/{id}`, JSON list at `/api/v1/items`, JSON detail at `/api/v1/items/{id}`. No unversioned `/api/items` alias in Phase 4. | Matches the ImplementationPlan public route and the implemented `/api/v1` convention. `/` is the existing public route, so no new competing home screen is introduced. | **PROPOSED** |
| PD4-05 | **Query parameters:** `q`, `class`, `race`, `itemType`, `sort`, `page`, `pageSize`. Defaults: no search, no class/race/type filter, `sort=name`, `page=1`, `pageSize=50`; `pageSize` maximum 100; `page` maximum 10,000. Invalid values return `422`. | Provides an explicit, versioned collection contract for MR-07/E-12 and bounds work. This keeps the implemented Phase 3 search name `q` instead of the ImplementationPlan placeholder `search`, and uses camelCase `itemType` for the multiword parameter. A page cap is a denial-of-service guard, not a product limit; deep-pagination design is revisited if inventory exceeds the agreed scale. | **PROPOSED** |
| PD4-06 | **Search is a case-insensitive substring of `Name` only**, after trimming. Empty search is ignored; maximum 128 characters; `%`, `_` and `!` are escaped as literals. EQUID, class, race and location are not searched by `q`. | Matches the Phase 3 search behavior and avoids a hidden multi-field search contract. Name-only search is predictable and can be indexed or replaced later without changing the parameter meaning. | **PROPOSED** |
| PD4-07 | **Class filter accepts one canonical class abbreviation or `ALL`.** Absent/empty/`ALL` means no class predicate. A specific value matches `ClassString = 'ALL'` or exact membership via `FIND_IN_SET`; blank/unknown restrictions never match a specific class. Multi-class selection is deferred. | Exact-token semantics prevent false positives and preserve AM-02. Treating `ALL` as a filter value rather than the storage sentinel avoids ambiguity in the API. `FIND_IN_SET` is parameterized and works with the stored comma format. | **PROPOSED** |
| PD4-08 | **Race filter accepts one canonical race label or `ALL`**, with the same absent/`ALL` and exact-membership semantics as class. Race labels with spaces are valid parameter values. Multi-race selection is deferred. | Mirrors PD4-07 and preserves values such as `Wood Elf` and `Vah Shir`. | **PROPOSED** |
| PD4-09 | **Item-type filter accepts `Item` or `Spell`** case-insensitively; absent/empty/`all` means no type predicate. No other values are accepted. | Uses the existing non-null `GuildBankItems.ItemType` ENUM and gives users a meaningful distinction for merged spell quantities. | **PROPOSED** |
| PD4-10 | **Sort allowlist:** `name` (default), `name_desc`, `date_added`, `date_added_desc`, `date_open`, `date_open_desc`, `date_bazaar`, `date_bazaar_desc`. Name order uses `NameSort`, then `GuildBankItemID`; date order uses the date column, then `GuildBankItemID`. Unknown keys are `422`. Natural sorting is the default. | Implements D-05/L699–L728 and DC-13 using the persisted sort key. The allowlist closes the D-12 dynamic-SQL risk. A deterministic tie-breaker prevents records from moving between pages when names or dates are equal. | **PROPOSED** |
| PD4-11 | **Availability is one of `upcoming`, `open` or `bazaar`, computed from a single UTC date per request.** Precedence: `today < DateOpen` → `upcoming`; else `today >= DateBazaar` → `bazaar`; else `open`. `DateOpen` and `DateBazaar` are inclusive boundaries, so `bazaar` begins on the `DateBazaar` day itself; equal `DateOpen`/`DateBazaar` values leave no `open` day. *(Amended by the owner decision of 2026-11-22, `PD5-40`; this plan originally returned `bazaar` only for `today > DateBazaar`.)* | E-10 is open; this makes the comparison explicit. On a misconfigured row where `DateBazaar < DateOpen`, the item stays `upcoming` until its open date and then becomes `bazaar`; the impossible window is not silently presented as open. The precedence must be approved because settings currently do not enforce date ordering. | **PROPOSED** |
| PD4-12 | **EQResource links are constructed server-side only when the stored base URL passes the existing exact validator.** The service revalidates at request time, appends the positive decimal EQUID, and emits no link for an invalid setting. UI links use `target="_blank" rel="noopener noreferrer"` and an accessible external-link label. | Satisfies DC-17/SR-08 and preserves L784–L795. No URL is accepted from the client, no external request is made, and `html/template` still escapes the attribute. The server-side check protects against direct SQLite edits that bypass settings validation. | **PROPOSED** |
| PD4-13 | **Pagination is offset-based `page`/`pageSize` with a total count.** Count and page rows are read in one repeatable-read read-only transaction so `total` and `totalPages` describe the same snapshot. A page beyond the end returns an empty `items` array with valid meta. | Matches the established `meta.page/pageSize/total` convention and supports numbered UI pagination. The snapshot avoids a commit between `COUNT(*)` and `SELECT` producing contradictory meta. Requests across separate navigations are intentionally not one long transaction. | **PROPOSED** |
| PD4-14 | **UI architecture is full-page server rendering with plain GET forms and links.** No HTMX, Bootstrap, CDN, client-side framework or new JavaScript. Filters/sort/page changes are normal navigations; the same service method backs the HTML and JSON surfaces. | Matches the implemented Phase 3 choice (PD3-09) and the strict CSP. It avoids the unresolved E-03/E-16 HTMX fragment contract. Public browsing remains usable without JavaScript and accessible by standard form controls. | **PROPOSED** |
| PD4-15 | **Blank restrictions are displayed as `Unknown`, stored `ALL` as `All`, and missing metadata shows a short "metadata unavailable" explanation.** Unknown never implies "all classes/races". The detail page shows the stored date values. | AM-02 deliberately leaves restrictions blank when metadata is absent; presenting that as unrestricted would be misleading. `metadataMissing` in the public DTO lets both API and HTML explain the blank safely. A legitimate zero-mask row remains visually unknown, which is the conservative interpretation. | **PROPOSED** |
| PD4-16 | **Add a forward migration `0002_public_inventory_indexes.sql`** with `(DeletedFg, ItemType, NameSort, GuildBankItemID)`, `(DeletedFg, DateAdded, GuildBankItemID)`, `(DeletedFg, DateOpen, GuildBankItemID)` and `(DeletedFg, DateBazaar, GuildBankItemID)`. No columns, generated columns or FULLTEXT indexes are added in Phase 4. | Current indexes support the active name path but not typed name sorting or DateAdded windows. These are index-only changes and preserve the schema contract. Class/race `FIND_IN_SET` and substring `LIKE` remain scans; §8 defines measurement and a deferral path for denormalization. DDL requires explicit approval. | **PROPOSED** |
| PD4-17 | **Defer location filtering and availability-date filtering.** Public Phase 4 filters only name, class, race and item type. A location filter would require a normalized location value on `GuildBankItems` or a service-side re-normalization that cannot paginate efficiently; it is not added now. | D-05 says `LocationGroup` is for filtering, but Phase 3 stores only raw `SourceLocation` in guild inventory. Adding a column now would be a Phase 5/lifecycle schema decision with import/backfill implications. Availability filtering is not required by the published public capability list and can be added later without breaking the query contract. | **PROPOSED** |

---

## 4. API contract

### 4.1 Routes and access

| Method | Path | Access | Response | Notes |
|---|---|---|---|---|
| GET | `/` | Public | HTML | Public inventory list. Optional session only controls navigation; the item projection never changes. Setup gate redirects to `/setup` while pending. |
| GET | `/inventory/{id}` | Public | HTML | Detail for one active `GuildBankItemID`; invalid/absent/deleted → `404` page. |
| GET | `/api/v1/items` | Public | JSON | Paginated public collection. No `401` for anonymous callers. |
| GET | `/api/v1/items/{id}` | Public | JSON | Public detail; absent or soft-deleted → `404` error envelope. |
| GET | admin routes | Admin | Unchanged | Keep `RequireAuth` → CSRF → `RequireAdmin`; Phase 4 adds no admin item endpoint. |

All four inventory routes are registered in the existing public group after `SetupGate`, not in the admin group. The JSON routes receive the same security headers as every application route. `GET` requests have no CSRF requirement because they are read-only; no state may be changed by these routes.

### 4.2 Query parameters

The same parameters apply to `/` and `/api/v1/items`. HTML pages display normalized values and preserve them in pagination/sort links.

| Parameter | Type | Allowed / default | Validation and behavior |
|---|---|---|---|
| `q` | string | default empty | Trim surrounding whitespace; empty means no name predicate; at most 128 Unicode code points; literal `%`, `_`, `!`; case-insensitive `Name` substring. |
| `class` | string | one class abbreviation or `ALL`; default empty | Case-insensitive input normalized to canonical uppercase. Empty or `ALL` means no class predicate. Any other value → `422` field `class`. |
| `race` | string | one race label or `ALL`; default empty | Case-insensitive exact match to the canonical labels; empty or `ALL` means no race predicate. Any other value → `422` field `race`. |
| `itemType` | string | `Item`, `Spell`, `all`; default empty | Case-insensitive; empty or `all` means no type predicate. Any other value → `422` field `itemType`. |
| `sort` | string | `name` default; see PD4-10 | Exact allowlist; unknown value → `422` field `sort`. |
| `page` | integer | default `1`; range `1..10000` | Malformed, zero, negative or above the cap → `422` field `page`. A valid page beyond the result set returns an empty page, not an error. |
| `pageSize` | integer | default `50`; range `1..100` | Malformed, zero, negative or above 100 → `422` field `pageSize`. |

Multiword query parameters use camelCase (`itemType`, `pageSize`), matching the existing JSON/API field style. `q` is the canonical search parameter, retained from the implemented import filter; the earlier ImplementationPlan placeholder name `search` is not part of the Phase 4 contract unless the owner changes PD4-05.

Unknown query parameters are ignored for forward compatibility; known parameters are never silently reinterpreted. The API must not accept SQL fragments, column names, `ORDER BY` expressions or comma-separated sort lists. The service owns validation so the HTML and JSON paths cannot drift.

### 4.3 Response shapes

Collection:

```json
{
  "data": {
    "items": [
      {
        "id": 474,
        "equid": 28813,
        "name": "Blade of Carnage",
        "itemType": "Item",
        "quantity": 1,
        "classString": "WAR,PAL,RNG,SHD,BRD",
        "raceString": "ALL",
        "metadataMissing": false,
        "dateAdded": "2026-11-21",
        "dateOpen": "2026-12-05",
        "dateBazaar": "2026-12-21",
        "availability": "upcoming",
        "eqResourceUrl": "https://items.eqresource.com/items.php?id=28813"
      }
    ]
  },
  "meta": {
    "page": 1,
    "pageSize": 50,
    "total": 1,
    "totalPages": 1,
    "sort": "name"
  }
}
```

Detail:

```json
{
  "data": {
    "item": {
      "id": 474,
      "equid": 28813,
      "name": "Blade of Carnage",
      "itemType": "Item",
      "quantity": 1,
      "classString": "WAR,PAL,RNG,SHD,BRD",
      "raceString": "ALL",
      "metadataMissing": false,
      "dateAdded": "2026-11-21",
      "dateOpen": "2026-12-05",
      "dateBazaar": "2026-12-21",
      "availability": "upcoming",
      "eqResourceUrl": "https://items.eqresource.com/items.php?id=28813"
    }
  }
}
```

`items` is always an array (never `null`). `totalPages` is `0` when `total` is `0`; otherwise it is `(total + pageSize - 1) / pageSize`. `page` echoes the requested page even when it is beyond the result set. The `sort` value echoes the canonical key, not the raw query string.

### 4.4 Error and content contract

- JSON errors use the existing `{"error":{"code","message","fields","request_id"}}` envelope.
- `422 validation_failed` is used for every invalid query parameter, with a `fields` map keyed by parameter name.
- `404 not_found` is used for absent, non-positive or soft-deleted detail IDs.
- `503 dependency_unavailable` is used when the guild store is unavailable; HTML pages render an explanatory unavailable state rather than a blank inventory.
- `500 internal_error` never includes SQL, database names, paths or settings values.
- Collection/detail success is `200` with `Content-Type: application/json; charset=utf-8`.
- Dates are calendar strings in `YYYY-MM-DD` form, not RFC 3339 timestamps. `availability` is one of `upcoming`, `open`, `bazaar`.
- `eqResourceUrl` is `null` when the stored setting fails revalidation. It is never constructed from a client value.
- No `ETag`, `Last-Modified` or long-lived cache header is introduced while `SecurityHeaders` keeps application responses `no-store`. A future caching design must not make authenticated navigation or setup-gated responses cacheable.

### 4.5 Public projection and forbidden fields

| Field | Public | Notes |
|---|---|---|
| `GuildBankItemID` | Yes, as `id` | Detail key; positive integer. |
| `EQUID` | Yes, as `equid` | EQResource identifier; not necessarily unique in inventory. |
| `Name` | Yes | Stored display name snapshot. |
| `ItemType` | Yes | `Item` or `Spell`. |
| `Quantity` | Yes | `1` for ordinary items; may exceed 1 for spells. |
| `ClassString` | Yes | Empty string when unknown/blank; `ALL` when stored unrestricted. |
| `RaceString` | Yes | Empty string when unknown/blank; `ALL` when stored unrestricted. |
| `MetadataMissingFg` | Yes, as `metadataMissing` | Boolean explanation for blank restrictions; not a security-sensitive field. |
| `DateAdded`, `DateOpen`, `DateBazaar` | Yes | Calendar dates; frozen at creation. |
| Derived `availability` | Yes | `upcoming`, `open` or `bazaar` according to PD4-11. |
| Derived `eqResourceUrl` | Yes, nullable | Built from validated server setting; no external call. |
| `SourceLocation` | **No** | Internal storage/location detail; not required by the public screen. |
| `GivenTo` | **No** | May identify a guild member. |
| `DeletedFg` | **No** | Public queries are already restricted to `0`; do not signal existence of removed rows. |
| `ModAt`, `ModBy`, `ModByUserID` | **No** | Administrative metadata. |
| `NameSort` | **No** | Internal sort key with control characters; never serialize. |
| Import warnings, audit rows, import IDs | **No** | Staging/administrative history is not public inventory data. |

The API must use a dedicated public response type. Serializing an internal `domain.GuildBankItem` and deleting fields in the handler is prohibited; a future added internal field must not become public by default.

---

## 5. Public inventory behavior

### 5.1 Listing and record identity

- The list defaults to all active records, natural name order, 50 rows per page.
- Every record is an independent inventory row. A stack of five identical ordinary items appears as five rows with distinct `GuildBankItemID` and possibly distinct dates; spells appear once per merged EQUID with their aggregate quantity.
- The list is not grouped by name or EQUID. Any future grouping view must be a separate projection and must not replace this record-level contract.
- The detail route uses `GuildBankItemID`. A request for a record with `DeletedFg=1` is treated as not found even if the caller knows the numeric ID.
- Public totals count inventory records, not distinct item names or item units. The link between units and resulting records is already documented by Phase 3.

### 5.2 Search and filter semantics

All predicates are combined with AND in this order-independent set: name, class, race, item type. An absent predicate is omitted, not represented by an empty string comparison.

**Name search.** `q` is trimmed, limited to 128 runes and matched as a case-insensitive substring of `Name` using a parameterized `LIKE` with `ESCAPE '!'`. `%`, `_` and `!` in the user's input are escaped, so they are literal characters. Search does not include `SourceLocation`, `ClassString`, `RaceString` or `EQUID`. Empty input means no predicate.

**Class filter.** Canonical class values are `WAR`, `CLR`, `PAL`, `RNG`, `SHD`, `DRU`, `MNK`, `BRD`, `ROG`, `SHM`, `NEC`, `WIZ`, `MAG`, `ENC`, `BST` and `BER`, in the decoder's stable order. Input is case-insensitive and normalized before querying.

- `class` absent, empty or `ALL` → no class predicate.
- `class=WAR` → `(ClassString = 'ALL' OR FIND_IN_SET('WAR', ClassString) > 0)`.
- `ClassString = ''` or `NULL` does not match a specific class.
- `ClassString = 'ALL'` matches every specific class.
- Other values → `422 field class`; no arbitrary SQL is built from the value.

**Race filter.** Canonical race values are `Human`, `Barbarian`, `Erudite`, `Wood Elf`, `High Elf`, `Dark Elf`, `Half Elf`, `Dwarf`, `Troll`, `Ogre`, `Halfling`, `Gnome`, `Iksar`, `Vah Shir`, `Froglok` and `Drakkin`. Input is case-insensitive and normalized to the canonical label.

- `race` absent, empty or `ALL` → no race predicate.
- `race=Wood Elf` → `(RaceString = 'ALL' OR FIND_IN_SET('Wood Elf', RaceString) > 0)`.
- Blank/unknown races never match a specific race.
- Other values → `422 field race`.

`FIND_IN_SET` is a parameterized predicate, not a dynamic column/expression. It is intentionally chosen over `LIKE '%WAR%'` because it respects comma boundaries and handles `Wood Elf` exactly. Its inability to use a normal index is accepted for Phase 4 and measured in §8.

**Item type.** `itemType=Item` or `Spell` adds `ItemType = ?`; absent, empty or `all` adds no predicate. No other value is accepted.

### 5.3 Display rules

| Stored value | List display | Detail display |
|---|---|---|
| `ClassString = 'ALL'`, `RaceString = 'ALL'` | `All` | `All classes` / `All races` |
| Comma-separated list | The stored abbreviations/names, with comma+space for readability | Same, as an accessible list or comma-separated text |
| Blank string, `MetadataMissingFg = 0` | `Unknown` | `Unknown` (zero/unspecified restriction) |
| Blank string, `MetadataMissingFg = 1` | `Unknown` plus short "metadata unavailable" note | `Unknown` plus the same note; do not imply unrestricted |
| `Quantity = 1` ordinary item | No special spinner; quantity may be shown as `1` | Quantity `1` |
| `Quantity > 1` spell | Quantity shown as `×N` or `N` with a clear label | Quantity and item type shown |
| Any active record | Name links to `/inventory/{id}` | Name and EQResource link |

Dates are displayed with a machine-readable `<time datetime="YYYY-MM-DD">` value and a readable date. Unknown restrictions must never be styled like `All`. The public UI must not expose `SourceLocation` or `GivenTo` even if a template later changes; the DTO is the enforcement boundary.

### 5.4 Availability computation and presentation

One UTC date is captured once per request from the injected service clock. Date values are compared as calendar dates, not as timestamps with a database session timezone. The state table is:

| State | Condition | List/detail copy | Date fields shown |
|---|---|---|---|
| `upcoming` | `today < DateOpen` | `Opens {DateOpen}` | Detail shows `DateAdded`, `DateOpen`, `DateBazaar` |
| `open` | `DateOpen <= today < DateBazaar` | `Available now` | Detail shows all three dates |
| `bazaar` | `today >= DateBazaar` and `today >= DateOpen` | `Bazaar eligible` | Detail shows all three dates |

Both thresholds are inclusive, and the `open` interval is half-open at its upper
bound: `bazaar` starts on the `DateBazaar` day itself, which is what "open to the
bazaar" means. *(The owner decision of 2026-11-22, `PD5-40`, amended this section;
it originally kept `open` through the `DateBazaar` day and switched to `bazaar`
the next day.)* If `DateOpen == DateBazaar`, there is no `open` day and the
record is `bazaar` from that date. If a row has `DateBazaar < DateOpen` because
the settings were inconsistent, the precedence in PD4-11 applies: it is
`upcoming` until `DateOpen`, then `bazaar`; it is never `open`. The detail page
still shows the stored dates so the inconsistency is visible rather than hidden.

The list should not render a second, independently calculated label. API and HTML use the same `availability` value from the service. `DateAdded` is informational; it does not change the state. No end-of-day, local-timezone or "expires at midnight" rule is introduced in Phase 4.

### 5.5 EQResource links

- The only source is the stored `EQResourceBaseUrl` setting, whose normal value is `https://items.eqresource.com/items.php?id=`.
- The service revalidates the exact HTTPS host, path and trailing `?id=` before use. A missing/invalid value yields `eqResourceUrl = null` and no anchor in HTML; the public row remains visible.
- The link target appends the positive decimal `EQUID`; no name, class or user input is used. No request is made to EQResource.
- The anchor is rendered only for an active record and includes `target="_blank"` and `rel="noopener noreferrer"`.
- The anchor text/accessible name must communicate that a new tab opens (for example, "Open Blade of Carnage on EQResource (new tab)"). The external link is optional decoration; the name link and item detail remain available if the setting is invalid.

### 5.6 Public versus authenticated behavior

| Surface | Anonymous | Authenticated administrator |
|---|---|---|
| `/`, `/inventory/{id}`, `/api/v1/items`, `/api/v1/items/{id}` | Allowed, same public projection | Allowed, same public projection |
| Optional session handling | None required; absent cookie is normal | Cookie may add admin navigation only |
| `SourceLocation`, `GivenTo`, `DeletedFg`, `Mod*` | Never | Not exposed by Phase 4 public routes; reserved for Phase 5 admin projection |
| Removed records | Not visible; detail `404` | Not visible through Phase 4 public routes; Phase 5 admin view is not implemented |
| Existing `/admin/settings`, `/admin/imports` | Redirect to login / unauthorized | Allowed under existing admin middleware |
| Import/upload/commit | Not allowed | Unchanged admin behavior |

Authentication must not change the public item payload. This prevents an accidentally privileged cache or serialization path and keeps the public API usable without a session. If a logged-in administrator needs internal fields in a later phase, it will be a separately authorized admin DTO/route with its own tests.

---

## 6. UI architecture

### 6.1 Page structure and navigation

| Page | Route | Content |
|---|---|---|
| Inventory list | `/` | Heading, result summary, GET filter form, sort selector, responsive result table, empty/no-match state, pagination. |
| Item detail | `/inventory/{id}` | Name, type, quantity, class/race display, availability badge, all three dates, EQResource link, and a plain back-to-inventory link. Do not trust an arbitrary `return`/`next` URL or reflect a raw request URI. |
| Error/unavailable | existing error/503 paths | Clear recovery text; no internal error detail. |

The existing navigation becomes public-aware:

- The brand links to `/`.
- A public "Inventory" link is always present.
- Anonymous visitors see no admin links and no sign-out control.
- Authenticated visitors retain the existing admin links and sign-out control.
- A public page can inspect the session cookie only to decide navigation. That optional lookup must not turn an absent cookie into an authentication error, and the item data is loaded independently of session status.

### 6.2 Rendering strategy

- Implement `/` and `/inventory/{id}` as full-page `html/template` renders through `internal/api/views.go`.
- Use standard `<form method="get">` controls for search, class, race, item type and sort. Submission performs a normal navigation; no JavaScript event handler is required.
- Use anchors for pagination and sort alternatives. Every link rebuilds the query from validated values; no raw request URI is trusted or copied blindly.
- The HTML handler invokes the same `InventoryService` query method used by the JSON handler, then maps the public domain result to a view model. The templates do not access repositories or build SQL.
- No HTMX partial, JSON fetch, bootstrap script, CDN or third-party CSS is added. This keeps the public page compatible with the existing strict CSP and avoids committing to E-03/E-16 before an interaction actually needs them.
- CSS is added to the existing embedded `app.css`: accessible table overflow, clear focus styles, badge variants for the three availability states, responsive filter grid, and distinct `Unknown` styling. No inline classes carrying user data.
- All user-facing values are rendered through `html/template` contexts. The validated `eqResourceUrl` string is rendered in a URL context so the template applies its URL filtering and escaping; do not introduce `template.URL`. The link's `target`/`rel` are literals, never user input.
- The page sets `lang="en"` and uses a descriptive `<title>`; the public heading is not hardcoded to a personal guild name beyond the existing application title.

### 6.3 Query-state and empty-state behavior

- Filters, sort and page are reflected in the URL so a page can be bookmarked and shared. The filter form retains submitted values after a normal render.
- Pagination displays previous/next and page/total-page links within a bounded window. Links omit empty/default parameters where practical, but canonical query parameter names remain stable.
- When total is zero and no filters are active, show "No inventory records are available yet."
- When total is zero with filters active, show "No items match these filters" plus a "Clear filters" link.
- When a requested page is beyond the result set, show an empty result message and a link to the first/last valid page; do not silently rewrite the URL in the server response.
- Invalid query values on an HTML request render the existing `422`/error presentation with a concise message rather than a stack trace or blank page.
- If MariaDB is unavailable, the public page renders the existing unavailable alert/`503` path; it does not show a misleading empty inventory.
- If an EQResource setting is invalid, no dead link is rendered; the item remains usable.

### 6.4 Accessibility and responsive behavior

- Use a table with `<caption>`, `<thead>`, `<th scope="col">` and sensible column order: Name, Type, Class, Race, Quantity, Availability, EQResource.
- Each filter has a visible `<label>`; selects have a neutral default option; the submit button is keyboard reachable.
- Availability is conveyed by text, not color alone. Badge colors are supplementary.
- The detail page uses a description list or definition table for dates and restrictions; headings follow a logical order.
- On narrow viewports, the table is horizontally scrollable inside a labelled region; the page must not force document-level horizontal overflow.
- External links announce the new-tab behavior; focus styles meet the existing stylesheet conventions.
- An accessibility smoke test covers labels, keyboard order and the presence of a table caption/caption-equivalent.

### 6.5 What the UI must not do

- It must not call an internal admin endpoint or expose admin fields.
- It must not calculate availability independently from the API.
- It must not hide the fact that restrictions are unknown.
- It must not construct an EQResource URL in JavaScript or from a template variable supplied by the browser.
- It must not add a public write control, quantity editor, delete control or import link.
- It must not require JavaScript to see results or change pages.

---

## 7. Backend design and implementation sequence

### 7.1 Package impact

| Area | Change |
|---|---|
| `internal/domain/inventory.go` | New pure types: sort enum, validated filter, repository/projection record, public item, availability state and page result. No database or template imports. |
| `internal/service/inventory.go` | New `InventoryService`: validation, normalization, availability calculation, EQResource construction and public mapping. Depends only on narrow interfaces plus `SettingsService`. |
| `internal/service/class_mask.go`, `race_mask.go` | Add read-only canonical value accessors (or equivalent exported slices) so filter validation and UI select lists use the same ordered source as decoding. Do not duplicate the labels. |
| `internal/repository/mariadb/inventory.go` | New explicit public queries, allowlisted `ORDER BY`, parameterized predicates, count/page snapshot and detail scan. No write methods. |
| `internal/api/inventory.go` | New JSON handlers, HTML handlers, query parsing, response DTOs, view models and link builders. |
| `internal/api/router.go` | Register the four public routes in the public group; add `Inventory` to `Deps`. |
| `internal/api/views.go` | Add list/detail page data and render methods. |
| `internal/api/ui.go` | Remove the placeholder `home` action so `/` is owned by the inventory handler; keep `adminRoot`. |
| `internal/web/templates/inventory_list.html`, `inventory_detail.html` | New embedded templates. Replace the placeholder `home.html` content/route so there is no competing Phase 4 landing page. Update `partials.html` navigation as described in §6.1. |
| `internal/web/static/app.css` | Public table, filter, badge, empty-state and responsive styles. |
| `internal/platform/app/app.go` | Construct the inventory service with the existing guild store or a nil/degraded store and pass it to the router. |
| `internal/web/templates/home.html` | Remove or replace the Phase 1 placeholder; `/` must not retain copy saying the public read path arrives in Phase 4. |
| `internal/repository/mariadb/migrations/0002_public_inventory_indexes.sql` | Approved index-only migration per PD4-16. Migration runner discovers it automatically. |
| `Docs/DatabaseDesign.md`, `Docs/ProjectStructure.md`, `README.md`, `Docs/PublicInventoryAPI.md` (new), `Docs/ProjectStatus.md` | Update after implementation and live validation, not before approval. |

No new configuration variable, environment variable, admin route, repository package import in API/UI, or external dependency is required.

### 7.2 Domain model

The implementation should keep the public contract explicit. Exact field names may follow existing style, but the semantics below are fixed by this plan:

```go
type InventorySort string

const (
    InventorySortName          InventorySort = "name"
    InventorySortNameDesc      InventorySort = "name_desc"
    InventorySortDateAdded     InventorySort = "date_added"
    InventorySortDateAddedDesc InventorySort = "date_added_desc"
    InventorySortDateOpen      InventorySort = "date_open"
    InventorySortDateOpenDesc  InventorySort = "date_open_desc"
    InventorySortDateBazaar    InventorySort = "date_bazaar"
    InventorySortDateBazaarDesc InventorySort = "date_bazaar_desc"
)

type InventoryFilter struct {
    Search   string
    Class    string // canonical abbreviation or empty
    Race     string // canonical label or empty
    ItemType ItemType // zero value means no type predicate
    Sort     InventorySort
    Page     int
    PageSize int
}

type AvailabilityState string

const (
    AvailabilityUpcoming AvailabilityState = "upcoming"
    AvailabilityOpen     AvailabilityState = "open"
    AvailabilityBazaar   AvailabilityState = "bazaar"
)

// InventoryRecord is the narrow repository projection. It intentionally omits
// SourceLocation, GivenTo, ModAt, ModBy, ModByUserID, DeletedFg and NameSort.
type InventoryRecord struct {
    GuildBankItemID  int64
    EQUID            uint64
    Name             string
    ItemType         ItemType
    Quantity         uint32
    ClassString      string
    RaceString       string
    MetadataMissing  bool
    DateAdded        time.Time
    DateOpen         time.Time
    DateBazaar       time.Time
}

type PublicInventoryItem struct {
    ID              int64
    EQUID           uint64
    Name            string
    ItemType        ItemType
    Quantity        uint32
    ClassString     string
    RaceString      string
    MetadataMissing bool
    DateAdded       time.Time
    DateOpen        time.Time
    DateBazaar      time.Time
    Availability    AvailabilityState
    EQResourceURL   *string
}

type PublicInventoryPage struct {
    Items     []PublicInventoryItem
    Page      int
    PageSize  int
    Total     int
    TotalPages int
    Sort      InventorySort
}
```

The repository record and public item are separate types on purpose. The API response structs additionally carry JSON tags and must not be generated by reflecting over a database row or an internal domain object.

### 7.3 Service behavior

`InventoryService` is constructed with the guild store, `SettingsService`, an injected clock and a logger. Match the Phase 3 pattern: `NewInventoryService(nil, ...)` yields a degraded service with `Ready() == false`, and every read returns `domain.ErrUnavailable` rather than panicking.

`ListPublic(ctx, filter)` performs the following in order:

1. Require a ready store.
2. Normalize and validate the filter once. Return `*domain.ValidationError` with field keys `q`, `class`, `race`, `itemType`, `sort`, `page` or `pageSize`.
3. Capture `today` once as the UTC calendar date.
4. Read the page from the repository, which applies the active scope, predicates, allowlisted order, count and limit.
5. Read `SettingsService.Values` for `EQResourceBaseUrl`. A settings read failure degrades links to `null` and logs a sanitized warning; it must not turn a valid inventory read into a page error.
6. Map each record to `PublicInventoryItem`, calculating `Availability` and `EQResourceURL` from the same `today` and validated base URL.
7. Return a non-nil empty `Items` slice when there are no rows.

`GetPublic(ctx, id)` applies the same projection and enrichment to one record. Non-positive, malformed, absent or soft-deleted IDs produce `domain.ErrNotFound`.

Filter normalization rules:

- Trim all strings. Convert class to uppercase, race/item type/sort to their canonical case.
- Treat class `ALL`, race `ALL`, itemType empty/`all`, and an empty search as absent predicates.
- Reject a class or race not in the canonical accessor lists; never pass an unknown token to `FIND_IN_SET`.
- Default an omitted sort to `name`; accept only the eight sort values in PD4-10.
- Apply `page`/`pageSize` defaults only at the boundary; then enforce `page >= 1`, `page <= 10000`, `pageSize >= 1`, `pageSize <= 100`.
- Return field-specific validation messages, not one generic error.

`BuildEQResourceURL(base string, equid uint64) *string`:

- Parse the setting; require scheme `https`, host equal to `items.eqresource.com` (case-insensitive), path `/items.php`, raw query exactly `id=`, no fragment and no userinfo.
- Require `equid > 0`.
- Set the raw query to `id=<decimal>` and return the absolute URL string.
- Return `nil` for an invalid setting rather than a user-visible error; log at most a warning with no secret. This deliberately duplicates the settings validator's safety at the point of use because SQLite could have been edited outside the application.

`availability(today, dateOpen, dateBazaar)` is a pure function implementing PD4-11. Normalize all three values to year/month/day in UTC before comparing; do not compare full timestamps. Unit tests must cover the day before, the exact open date, the exact bazaar date, the day after, equal open/bazaar dates, an inverted window and a date-only value near a timezone boundary. *(The owner decision of 2026-11-22, `PD5-40`, changed the expectation for the exact `DateBazaar` case from `open` to `bazaar` and the equal-dates case from `open` to `bazaar`; the day before `DateBazaar` must still return `open`.)*

The service never imports `internal/repository`, never calls `Items`, never writes audit records and never mutates a filter after the repository has received it.

### 7.4 Repository queries

The repository must use an explicit column projection and build only predicates with bound parameters. No `SELECT *`; no internal column is selected unless the public projection needs it.

```sql
SELECT GuildBankItemID, EQUID, Name, ItemType, Quantity,
       ClassString, RaceString, MetadataMissingFg,
       DateAdded, DateOpen, DateBazaar
FROM GuildBankItems
WHERE DeletedFg = 0
  -- optional: AND Name LIKE ? ESCAPE '!'
  -- optional: AND (ClassString = 'ALL' OR FIND_IN_SET(?, ClassString) > 0)
  -- optional: AND (RaceString = 'ALL' OR FIND_IN_SET(?, RaceString) > 0)
  -- optional: AND ItemType = ?
ORDER BY <allowlisted expression>
LIMIT ? OFFSET ?
```

The predicate arguments are appended in a fixed order; the class/race values are canonical and parameterized. The search argument is `%` + `escapeLike(q)` + `%`. The page limit and offset are bound integers, never interpolated text. `DeletedFg = 0` is unconditional.

Allowlisted order expressions:

| Sort value | SQL order expression |
|---|---|
| `name` | `NameSort IS NULL ASC, NameSort ASC, GuildBankItemID ASC` |
| `name_desc` | `NameSort IS NULL ASC, NameSort DESC, GuildBankItemID DESC` |
| `date_added` | `DateAdded ASC, GuildBankItemID ASC` |
| `date_added_desc` | `DateAdded DESC, GuildBankItemID DESC` |
| `date_open` | `DateOpen ASC, GuildBankItemID ASC` |
| `date_open_desc` | `DateOpen DESC, GuildBankItemID DESC` |
| `date_bazaar` | `DateBazaar ASC, GuildBankItemID ASC` |
| `date_bazaar_desc` | `DateBazaar DESC, GuildBankItemID DESC` |

The order expression is selected by a switch over the validated enum. The query builder must have no default branch that accepts caller text. `NameSort IS NULL ASC` keeps legacy null keys at the end in both directions.

`ListPublic` should read the count and page in one repeatable-read read-only transaction (or a repeatable-read transaction if the driver cannot request read-only). The count uses exactly the same `WHERE` predicate:

```sql
SELECT COUNT(*) FROM GuildBankItems WHERE <same predicate>
```

If the transaction cannot be created, return the mapped store error; do not silently fall back to two independent snapshots. The read transaction commits/rolls back without writes. It is per request, not a long-lived transaction across navigation. The repository should verify with the MariaDB driver whether `sql.TxOptions{ReadOnly: true}` is supported; if not, document the chosen isolation option and still avoid writes.

`GetPublic` uses the same explicit projection plus `GuildBankItemID = ? AND DeletedFg = 0`; no page or sort. It maps `sql.ErrNoRows` to `domain.ErrNotFound`.

Scanning must set dates as UTC calendar values and preserve blank strings. `MetadataMissingFg` is scanned to a bool. `Quantity` must be positive; malformed row data is a store error, not a silently dropped item. All errors pass through the existing `mapStoreError` classification so `ErrUnavailable` remains distinguishable from `ErrNotFound`.

The repository should not call `FIND_IN_SET` with a raw user string before service validation. It should not add a write, audit, cache or metric side effect. It respects the request context, so a bounded service/API context cancels a runaway query.

### 7.5 API handlers and route registration

`internal/api/inventory.go` adds:

- `inventoryHandler` with the inventory service, views and logger.
- Query parsing that distinguishes absent, empty and malformed values. Parse integers with `strconv.Atoi`; reject partial strings; do not let `0` or a negative value reach the service.
- JSON response types with explicit tags, mapped from `domain.PublicInventoryItem`, and a page meta object. Do not expose domain time formatting directly; format dates as `YYYY-MM-DD`.
- `list`, `get`, `page` and `detailPage` handlers.
- A small query/link builder using `url.Values` for pagination/sort URLs. It takes normalized filter values and literal page parameters; it never copies an arbitrary raw query string.
- Error mapping through `respondError` for JSON and the existing HTML error path for pages. `ErrUnavailable` must be explicit; generic 500 is not acceptable for a MariaDB outage.

Route registration in `internal/api/router.go`:

```text
public.GET("/", inventoryHandler.page)
public.GET("/inventory/:id", inventoryHandler.detailPage)
public.GET("/api/v1/items", inventoryHandler.list)
public.GET("/api/v1/items/:id", inventoryHandler.get)
```

These go with the existing public group after the setup gate. They must not be registered in the admin group and must not use `RequireAuth`. `Deps.Inventory` is validated by `NewRouter` exactly like the existing services; tests and `app.go` pass either a live or degraded service.

The HTML page needs the optional authenticated user for navigation. Reuse the existing session-token/authentication helper only when a cookie is present; ignore an invalid cookie on a public page rather than rendering a login redirect. The API does not need the principal.

### 7.6 Application wiring and migration

- In `internal/platform/app/app.go`, construct `InventoryService` with `guildStore` when available and with `nil` otherwise, then pass it through `api.Deps`. Keep the existing startup behavior: an unreachable store is degraded, not a panic.
- The migration runner already discovers embedded `0001_guild_store.sql`; adding `0002_public_inventory_indexes.sql` must update the embedded files and expected schema version in the migration tests. No startup code change beyond the existing migrate call is required.
- The index migration should be a single forward-only version recorded by the existing migration runner under its advisory lock. It adds no column, does not backfill data, and must not be re-run manually after being recorded.
- Update `importguard_test.go` expectations only if the route/package inventory changes the import graph; the API and web packages still must not import `repository`.
- Extend service/router construction in tests with the new dependency. Update every `NewRouter` test fixture so a missing inventory service cannot silently disable the public route or panic.

### 7.7 Proposed migration `0002_public_inventory_indexes.sql`

Subject to PD4-16 approval, add approximately:

```sql
ALTER TABLE GuildBankItems
    ADD KEY ix_gbi_public_type_name_sort (DeletedFg, ItemType, NameSort, GuildBankItemID),
    ADD KEY ix_gbi_public_added (DeletedFg, DateAdded, GuildBankItemID),
    ADD KEY ix_gbi_public_open (DeletedFg, DateOpen, GuildBankItemID),
    ADD KEY ix_gbi_public_bazaar (DeletedFg, DateBazaar, GuildBankItemID);
```

The exact index names should follow the existing `ix_gbi_*` convention and be checked against MariaDB's identifier/length limits. Do not drop `ix_gbi_public`, `ix_gbi_name_sort` or `ix_gbi_dates` in the same change. If benchmarks show the new indexes are unnecessary, the owner may instead approve a benchmark-gated implementation with no migration; in that case the plan's performance gate becomes the condition for adding them later.

---

## 8. Performance for large inventories

### 8.1 Assumed scale

- One Phase 3 batch can expand to 100,000 inventory rows (PD3-06). Repeated imports over time can therefore produce hundreds of thousands of `GuildBankItems` rows, even though the development sample currently has hundreds.
- Phase 4 should be designed for **250,000 active records** across item and spell types, and stress-tested at **1,000,000**. These are planning targets, not specification limits; the owner must confirm them in §12.
- The dominant access pattern is: active rows, default natural name order, first few pages, occasional search/class/race filters, occasional date sort, rare deep pages.
- The metadata table has 144,712 rows today but is not joined per public request. Class/race strings are denormalized, so adding more metadata does not increase the public query cost except through added inventory rows.

### 8.2 Query cost and pressure points

| Query shape | Cost | Why |
|---|---|---|
| Default name list + count | Moderate | Active filter then natural-sort index; `COUNT(*)` still scans the active set or a covering index. |
| `ItemType` + name sort | Moderate | Existing indexes do not cover `(DeletedFg, ItemType, NameSort)` together; PD4-16 adds one. |
| Date sort | Moderate | Existing `ix_gbi_dates` starts with `DateBazaar, DateOpen`, not `DateAdded`, `DateOpen` or `DateBazaar` alone; PD4-16 adds dedicated orders. |
| Name substring search | High | Leading `%` cannot use the `Name` suffix of an index; the active set is scanned. |
| Class/race filter | High | `FIND_IN_SET` is a function predicate over a comma string and cannot use a normal B-tree index. |
| Combined search + class + race | Highest | Multiple non-sargable predicates; count and page both evaluate them. |
| Deep page | High | `LIMIT ... OFFSET N` still reads/skips N matching rows. Page cap bounds the worst case. |
| Detail | Low | Primary-key lookup plus active predicate. |
| Settings lookup for EQResource | Low | One SQLite read per request; no MariaDB call. |

The implementation must avoid N+1 queries: one count, one page select and one settings read per list request; one detail query and one settings read per detail request. It must not load all matching rows into Go to filter or sort them, and it must not call `Items` per row.

### 8.3 Index and schema strategy

Phase 4 uses PD4-16's index-only migration as the baseline. Keep the existing indexes because Phase 3 and future admin queries may still use them.

- `ix_gbi_public_type_name_sort` covers the common item-type filter plus natural sorting and supplies the tie-break ID.
- `ix_gbi_public_added`, `ix_gbi_public_open` and `ix_gbi_public_bazaar` cover the three date sorts and their counts where possible.
- The partial active prefix `DeletedFg` is intentional; every public query starts with `DeletedFg = 0`.
- Do not add a normal index on `ClassString` or `RaceString` and assume it helps membership filtering; MariaDB cannot use it for `FIND_IN_SET`. A prefix index would not make `FIND_IN_SET` sargable and could mislead future maintainers.
- Do not add a FULLTEXT index in Phase 4. Full-text tokenization would not preserve the required "substring of the stored name" semantics without a separate normalized column and import backfill, which is a larger schema/behavior change.
- Do not create class/race child tables in Phase 4. They would need dual-write/backfill in Phase 3/5 and would contradict the current "decoded strings are stored permanently" contract unless separately approved.

If benchmarks fail, the approved escalation path is explicit, not silent:

1. **Search:** add a normalized lowercase `NameSearch` column with a suitable index (prefix/trigram/FULLTEXT design to be chosen) and a forward backfill; update import commit to write it. This requires schema and import changes beyond Phase 4's initial DDL approval.
2. **Class/race:** add derived membership tables (one row per item/class and item/race) maintained in the same transaction as inventory writes, or use a generated/denormalized representation that can be indexed. This is a data-model change, not a handler optimization.
3. **Deep pagination:** add a keyset cursor (`afterId`/sort tuple) while retaining page/pageSize for the standard UI; closing E-12's keyset recommendation.
4. **Read load:** add a bounded, short-lived cache for counts/pages or an external HTTP cache, but only after authorization/cache-variance review. The current `no-store` header must not be weakened casually.

No fallback may concatenate a user search term or filter into SQL, and no optimization may include deleted or internal-field rows in the public response.

### 8.4 Runtime protections

- Apply a `5s` context deadline to public inventory reads (list/detail), bounded below the HTTP server's existing `120s` timeout. A deadline should map to a logged `503 dependency_unavailable`/retryable response, not a partially rendered page. The exact deadline is PROPOSED and can be adjusted after benchmarks.
- Enforce `pageSize <= 100` and `page <= 10000` before computing `OFFSET`.
- Keep the MariaDB pool's existing bounds (20 open / 10 idle) and do not open a transaction per row.
- Render only the requested page; do not prefetch adjacent pages or counts for every filter combination.
- Avoid logging search terms or names by default; log duration, result count, filter categories and request ID for diagnosis.
- Do not add per-request `Ping` calls, metadata lookups or schema checks. `Ready()` follows the Phase 3 store-pointer pattern.
- If the store is unreachable, fail fast with `ErrUnavailable`; do not retry a full scan inside the request.

### 8.5 Benchmark and acceptance gates

Benchmarks run against a real MariaDB 11.4 instance using the Compose topology and a seeded `GuildBankItems` fixture. The fixture must include at least 250,000 active rows plus deleted rows, mixed `Item`/`Spell`, duplicate names, names with numbers, `ALL`, class/race lists, blank/missing metadata, three date states and both ascending/descending ties.

Minimum query mix:

1. Default first page and page 5.
2. Deep page at the cap or a documented realistic deep page.
3. Name search with a broad term and a no-match term.
4. Class filter (`WAR`), race filter (`Human` and `Wood Elf`).
5. Combined name + class + race + item type.
6. Each date sort ascending and descending.
7. Detail by ID (active, deleted→404).
8. Default count with no filters.

Proposed gates on the agreed development hardware:

| Measurement | Target at 250k active rows | Stress at 1M |
|---|---|---|
| Default list (count + page) p95 | ≤ 250 ms | ≤ 1 s |
| Filtered search/class/race list p95 | ≤ 750 ms | ≤ 2 s |
| Detail p95 | ≤ 50 ms | ≤ 100 ms |
| No public query | > 2 s without cancellation | > 5 s (hard deadline) |
| API request memory | Bounded by page size; no full-result buffering | Same |
| Phase 3 import commit regression | No more than the agreed index-write overhead; no failure | Not re-run at 1M in Phase 4 CI |

The exact hardware and thresholds require owner approval in §12. Capture `EXPLAIN`/`EXPLAIN ANALYZE` for the eight shapes, record the chosen index and filesort behavior, and attach the results to the implementation evidence. A failure is a gate failure, not a note to fix later.

### 8.6 What is deliberately deferred

- Keyset pagination, because the public UI contract is page-numbered and the current inventory is far below the keyset threshold.
- Result caching, because `SecurityHeaders` currently sets `no-store` and no cache-invalidation contract exists for imports/updates.
- A search service or external index, because that adds an operational dependency without evidence at the current scale.
- Class/race denormalization, because it is a schema/import change and would affect Phase 5 writes.
- Count approximations, because the API promises a total and the UI shows it.

Each deferred item has a measurable trigger in §8.5; none is silently accepted as permanently unnecessary.

---

## 9. Security review

### 9.1 Threat and control matrix

| Threat | Control | Required test |
|---|---|---|
| Public route accidentally exposes admin data or requires wrong auth | Register all four routes in the public group after `SetupGate`; no `RequireAuth`; admin routes unchanged; dedicated public DTO. | Route matrix: anonymous list/detail `200`; `/admin/*` still redirects/`401`; admin cannot obtain deleted or `GivenTo` data from public routes. |
| Internal-field leakage | Explicit repository projection and explicit response type; forbidden-field list in §4.5; no serialization of `domain.GuildBankItem`. | Reflection/tag test asserts the public DTO has only allowed fields; response-body tests reject `DeletedFg`, `SourceLocation`, `GivenTo`, `ModAt`, `ModBy`, `ModByUserID`, `NameSort`. |
| SQL injection through `q`, class, race, itemType, sort, page | Parameterized predicates; canonical allowlists; sort switch over enum; no SQL fragments; integer parsing and bounds; no caller text in `ORDER BY`. | Inject payloads such as `' OR 1=1 --`, `%`, `_`, `!`, comma lists, `SLEEP()`, `NameSort; DROP` in every parameter; assert `200` with literal search/no match or `422`, never SQL errors or extra rows. |
| `LIKE` wildcard misuse changes result semantics | Escape `%`, `_` and `!` with `ESCAPE '!'`; search only `Name`. | Search for literal `%`, `_` and `!` matches only rows containing those characters. |
| Stored XSS in names/classes/races | `html/template` contextual escaping; no `template.HTML`; names from a controlled snapshot but still untrusted for output. | Seed a name such as `<script>alert(1)</script>` and assert escaped text, no script element; CSP remains strict. |
| Reflected XSS through filters/page values | Server normalizes values; template escapes; links built from `url.Values`; no raw query echo. | `q`, `class`, `race` and page payloads render escaped; invalid values render the error page without reflection as HTML. |
| Malicious EQResource URL (configuration compromise) | Exact HTTPS host/path/query validation at use; append numeric EQUID only; no client-supplied URL; template treats field as URL and escaping remains enabled. | Set invalid schemes/hosts/paths in a test settings fake and assert `eqResourceUrl = null` and no anchor. Verify a valid URL has `target="_blank"` and `rel="noopener noreferrer"`. |
| Reverse tabnabbing | `rel="noopener noreferrer"` on every external anchor. | Template assertion for every EQResource anchor. |
| CSRF on public reads | Public routes are `GET`/read-only; no mutation handler. Admin writes remain CSRF-protected. | Method tests: `POST /api/v1/items` is not introduced; existing admin CSRF tests still pass. |
| Setup bypass | Existing `SetupGate` wraps the public group; pending setup redirects/`503`. | Setup-pending test: `/` redirects to `/setup`; `/api/v1/items` returns `503 setup_required`. |
| Removed-record enumeration | Every list/count/detail predicate includes `DeletedFg=0`; deleted detail returns `404`, not `410`/`200`. | Seed active and deleted rows with adjacent IDs; list/count omit deleted; detail returns `404`. |
| Error/log leakage | Existing error mapping; no SQL/details/settings in responses; log request ID, duration and counts only; do not log names or search terms by default. | Force store errors and malformed queries; assert generic envelopes and absence of DSN/password/SQL/name. |
| Cache leakage | Keep `Cache-Control: no-store`; public payload does not vary by session. | Response header test; compare anonymous and authenticated response bodies for identical items. |
| DoS via huge page/size/search/filter scan | Max page and pageSize; 128-rune search; 5s read context; 20-connection pool; no full-result buffering. | Boundary and over-limit tests; timeout test with a fake slow store; page cap returns `422`. |
| Excessive public crawling | No in-app public rate limiter in Phase 4; document reverse-proxy rate limiting as the deployment control. | Deployment note; no claim that the app rate-limits public GETs. |
| Cross-site framing/clickjacking | Existing `X-Frame-Options: DENY` and CSP `frame-ancestors 'none'`. | Existing header tests remain. |
| MIME sniffing and referrer leakage | `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`. | Existing header tests remain. |
| External network dependency | EQResource is never fetched; only a URL string is built. | Tests fail on unexpected outbound HTTP; no test uses the real domain. |
| Metadata-missing misinterpretation | Blank restrictions display `Unknown`, never `All`; specific filters exclude blanks; `metadataMissing` explains the state. | Seed missing metadata + zero masks; filter and display assertions. |

### 9.2 Security decisions and residual risk

- **Public data is not a secret.** The inventory is intentionally public, but internal lifecycle, location, recipient and administrative fields are not. The security boundary is the DTO/search predicate, not CSS hiding.
- **Authentication does not broaden public routes.** An admin cookie adds navigation only. This deliberately avoids a "same URL, different payload" authorization branch in Phase 4.
- **EQResource is an external destination.** The app validates and constructs the URL, but the destination's content and tracking are outside this system. `Referrer-Policy: no-referrer` and `rel="noreferrer"` limit leakage. No response body or query parameter is sent to EQResource by the application.
- **No new secret at rest.** Phase 4 adds no credential or token. Existing E-11 secret-at-rest review remains open for the MariaDB password.
- **No public rate limit.** A reverse proxy or Unraid ingress is the expected operational control. If the owner requires application-level anonymous throttling, it is a separate approved feature; the existing login limiter must not be reused with different semantics.
- **No CORS relaxation.** The public API remains same-origin; no wildcard `Access-Control-Allow-Origin` is added. Future external tools are a separate integration decision.
- **Timing side channels are not material.** Search and filters return public data; no secret-dependent branch is added. The app should still avoid returning detailed database error distinctions to anonymous callers.
- **Security sign-off is required before merge.** The plan's tests and a manual review of route registration, DTO fields, SQL construction and template attributes are the gate.

### 9.3 Security review checklist for implementation

1. Confirm every SQL predicate is parameterized and sort expressions are constants.
2. Confirm no public handler reads `GivenTo`, `SourceLocation`, `Mod*`, `DeletedFg` or audit data into a response type.
3. Confirm the public DTO is not an alias of an internal struct.
4. Confirm `DeletedFg=0` is present in list, count and detail.
5. Confirm setup gate and unavailable paths.
6. Confirm `html/template` escaping and external-link attributes.
7. Confirm settings revalidation for EQResource, including direct-DB tamper tests.
8. Confirm header and cache behavior does not vary between anonymous and authenticated item responses.
9. Confirm invalid input produces the documented `422` and does not leak SQL.
10. Confirm tests make no external network request.

---

## 10. Test plan

### 10.1 Unit tests

| Area | Cases |
|---|---|
| Filter normalization | trim; class case/`ALL`; race case with spaces; itemType case/`all`; default sort; page/pageSize defaults; each invalid field produces its own key. |
| Search escaping | `%`, `_`, `!`, mixed case, empty, 128-rune boundary, over-limit; no raw SQL fragment. |
| Sort mapping | all eight values map to the approved SQL clause; unknown value rejected before the repository. |
| Availability | day before `DateOpen`; exact `DateOpen`; inside window; exact `DateBazaar`; day after; equal open/bazaar; inverted window; UTC date around midnight. |
| EQResource URL | valid default; invalid scheme; invalid host; invalid path; missing trailing `id=`; fragment/userinfo; EQUID zero; expected encoded URL. |
| Public mapping | blank + missing metadata; `ALL`; formatted comma; quantity; dates; nil link; no forbidden fields. |
| Pagination math | total 0 → totalPages 0; exact multiples; remainder; page beyond end. |

Tests should be table-driven and pure. Availability and URL building must not require a database or template.

### 10.2 Repository integration tests

Use a dedicated, disposable MariaDB 11.4 database/schema (the existing Compose service or a test container). The test fixture must seed only synthetic `GuildBankItems` rows in a temporary schema/table scope; it must not write the development `items` source or leave rows behind. If the environment cannot provide MariaDB, SQL-builder tests may run, but the Phase 4 gate requires the live repository suite to run at least once against the approved topology.

Fixture rows must include:

1. Active ordinary items with quantity 1; active spells with quantity > 1.
2. Deleted rows adjacent in ID to active rows.
3. `ClassString` values: `ALL`, `WAR`, `WAR,CLR`, `CLR,PAL`, blank, near-token values that would false-positive under `LIKE`.
4. `RaceString` values: `ALL`, `Human`, `Human,Erudite`, `Wood Elf`, blank.
5. `MetadataMissingFg` both values, including blank restrictions.
6. Names with natural numbers (`Item 1`, `Item 2`, `Item 10`, `Item 11`), case variants, punctuation, duplicate names and a long name.
7. Dates on both sides of open/bazaar boundaries, equal dates and an inverted `DateBazaar < DateOpen` row.
8. Equal sort keys to exercise the `GuildBankItemID` tie-breaker.

Assertions:

- Active scope is present in list, count and detail.
- Exact class/race membership and `ALL` union; blank never matches a specific value.
- Combined filters AND correctly.
- Each sort returns the expected record order; name sort places `Item 2` before `Item 10`; descending reverses ties predictably.
- Count matches the filtered set; page slices are disjoint and complete for a stable fixture.
- `OFFSET` boundary and page beyond end return the expected page/meta.
- Detail returns the public projection; deleted/absent returns `ErrNotFound`.
- `EXPLAIN` for default, type, date and filtered queries uses an index where expected; record any filesort.
- Source inspection confirms `inventory.go` contains no public write verbs and no `SELECT *`.

### 10.3 API contract tests

Assemble the real router with fake services/repositories as needed:

- Anonymous `GET /api/v1/items` and detail return `200`; no redirect/401.
- Anonymous `GET /` returns HTML and contains no admin navigation.
- Authenticated admin responses contain the same public item fields; no internal fields.
- Every filter combination returns the expected service filter and meta.
- Unknown sort/class/race/type and bad page/pageSize are `422` with field keys.
- Page beyond end is `200` with empty items and valid meta.
- Deleted/absent detail is `404` envelope.
- Store unavailable is `503 dependency_unavailable`.
- Setup pending: HTML redirects to `/setup`, API returns `503 setup_required`.
- Security headers/CSP/cache headers are present on HTML and JSON.
- `POST`/`PUT`/`DELETE` on the public item routes return `405` (no accidental mutation route).
- Public response body never contains forbidden field names/values.

### 10.4 Template and accessibility tests

- List renders table caption, column headers, filter labels, result count, active sort and page links.
- Pagination/sort links preserve non-default filters and do not echo raw unsafe query text.
- Empty inventory, no-match and unavailable states render distinct copy.
- Detail renders all dates, availability state, quantity, class/race and EQResource link.
- XSS fixture is escaped; no `template.HTML` bypass.
- EQResource anchor always has `target="_blank"` and `rel="noopener noreferrer"`.
- Keyboard order reaches all filters, submit, table name links, external link and pagination.
- A smoke check for `html/template` parse errors and embedded asset presence.

### 10.5 Performance tests

- Seed 250,000 active rows plus 25,000 deleted/noise rows in a disposable MariaDB instance.
- Run the §8.5 query mix with a warm pool and record p50/p95/p99, rows examined, query count and duration.
- Run the 1M stress mix separately; do not make it a default CI requirement if it is too slow.
- Run the Phase 3 import commit smoke at the normal sample size to detect index-write regression.
- Capture `EXPLAIN`/`ANALYZE` output with the chosen indexes.
- A failed gate blocks completion; do not mark it "known slow" without owner approval.

### 10.6 Live Compose validation

After implementation, rebuild the app and run against the existing Compose topology with a disposable seeded dataset:

1. `/healthz` and `/readyz` return `200` after setup.
2. `/` and `/api/v1/items` return inventory without a session.
3. Search, every class/race filter and item type return expected counts.
4. Natural and date sorts return expected first/last records.
5. Page navigation preserves filters and has consistent totals.
6. Detail returns the same item as its list row.
7. Deleted and absent IDs return `404`.
8. Invalid parameters return `422`.
9. EQResource link is present/absent according to settings and has correct attributes.
10. Restart the app; public pages still work from persisted data.
11. Stop MariaDB; public page/API show the unavailable path and `/readyz` reflects the outage; restore and verify recovery.
12. Remove temporary seeded data and validation artifacts; leave no test credentials or fixture rows in the development database.

### 10.7 Quality commands

Run at minimum:

```text
go test ./...
go vet ./...
gofmt -l .
git diff --check
```

Markdown table validation and the temp-artifact check should be run as in the Phase 3 final verification. Tests must not call `items.eqresource.com` or any external host.

---

## 11. Work breakdown, estimate and gates

### 11.1 Estimate

The approved `Docs/ImplementationPlan.md` estimate is **7–10 engineer-days** for Phase 4. This plan retains that range for one experienced developer, excluding approval wait, index benchmark hardware setup and any escalation prompted by a failed performance gate.

| Work package | Estimate | Depends on |
|---|---|---|
| Approval and contract freeze (no code) | 0.5–1 day | This plan review |
| Domain types, filter validation, availability and URL builders + unit tests | 1–1.5 days | PD4-01–PD4-12 |
| Repository queries, snapshot pagination, scan tests and SQL-builder tests | 1.5–2 days | Domain; index decision |
| API handlers, DTOs, route wiring and contract tests | 1–1.5 days | Repository/service |
| HTML list/detail, navigation, CSS and template tests | 1.5–2 days | Service/API |
| Security tests, accessibility smoke, performance fixture and benchmarks | 1–1.5 days | All slices |
| Migration, Compose validation, docs and final verification | 0.5–1 day | Approval; implementation |

### 11.2 Implementation order

1. Record approvals/decisions and update status docs only after approval.
2. Add the index migration if approved; run migration tests.
3. Add domain types and pure service helpers with tests.
4. Add the repository query surface and integration tests.
5. Add `InventoryService` and API route/handlers with contract tests.
6. Add HTML templates, navigation and CSS with template/accessibility tests.
7. Run security and performance gates; address findings within the approved scope.
8. Run live Compose validation and update documentation.
9. Obtain security/completion sign-off. Do not start Phase 5.

### 11.3 Phase gates

| Gate | Exit condition |
|---|---|
| G0 — Plan approval | PD4-01–PD4-17, E-07/E-10/E-12 dispositions, DDL approval and performance targets accepted. |
| G1 — Domain/service | Unit tests pass; no database dependency in pure helpers. |
| G2 — Repository/API | Query integration and contract tests pass; no internal fields leak. |
| G3 — UI | List/detail render, accessibility smoke and empty/error states pass. |
| G4 — Security/performance | §9 checklist and §8.5 gates pass or have explicit owner disposition. |
| G5 — Live validation | Compose run and cleanup complete. |
| G6 — Completion | Docs updated, full quality commands pass, owner approves Phase 4 completion. Phase 5 remains unauthorized. |

---

## 12. Remaining design gaps, risks and required approvals

### 12.1 Design gaps and their disposition

| Gap | Status after this plan | Blocking? |
|---|---|---|
| E-07 — public/admin authorization matrix and public projection | Resolved by PD4-02/PD4-03 if approved; must be recorded and tested. No admin item projection in Phase 4. | Approval blocks coding. |
| E-10 — date comparison semantics | Resolved by PD4-11 if approved: single UTC date, inclusive open/bazaar boundaries, explicit precedence. | Approval blocks availability UI/API. |
| E-12 — API versioning and pagination | `/api/v1` fixed; page/pageSize/meta/limits fixed by PD4-05/PD4-13. Keyset remains a documented scale escalation. | Approval blocks API contract. |
| Query-name mismatch with the earlier plan | `Docs/ImplementationPlan.md` used placeholder names `search`/`itemtype`; PD4-05 proposes the implemented `q` and camelCase `itemType`. | Approval blocks API contract. |
| Public detail route not fixed in the specification | PD4-04 proposes `/inventory/{id}` and `GET /api/v1/items/{id}`. | Approval blocks HTML/API route work. |
| MR-14 — public UI defaults | Default sort, page size, empty states, field visibility, external-link attributes and availability labels are set in §4–§6/PD4-14/15. | Approval blocks UI. |
| DC-10 — public exposure | Public/admin projection separated; internal fields forbidden by §4.5. | Approval blocks API/UI. |
| DC-17 — EQResource link safety | Exact server-side validation, numeric append, `target`/`rel` and no external call. | Approval blocks link rendering. |
| No normalized location on `GuildBankItems` | Deferred. D-05's location filtering/grouping cannot be applied to public inventory without a schema/backfill or expensive per-row normalization. | Does not block Phase 4 because location filter is out of scope; blocks any later location-filter request until decided. |
| Record grouping vs per-instance display | Per-instance rows chosen by PD4-01. Duplicates may look repetitive. A grouped display is not implemented. | Approval blocks list semantics. |
| Date-setting order (`DateBazaar < DateOpen`) | Not prevented by settings validation or DB check. PD4-11 defines display precedence; the misconfiguration remains visible in dates. | Availability can ship with precedence, but lifecycle semantics should be revisited in Phase 5. |
| Guild-local timezone / "today" | Phase 4 uses UTC per E-10; no timezone setting exists. The displayed availability may differ from a guild member's local calendar date. | Owner must accept UTC for Phase 4 or approve a timezone setting (not planned). |
| `NameSort` database collation | The key is designed for byte ordering, but the column is `utf8mb4_unicode_ci`. Integration tests must prove natural order. If not, use a binary collation/order expression or a dedicated index decision. | Blocks completion if the natural-sort golden test fails. |
| Search/class/race query scalability | `%term%` and `FIND_IN_SET` are scans. §8 defines measurement and escalation. | Does not block first implementation; blocks completion if §8.5 fails. |
| Deep pagination and exact total count | Offset pagination + `COUNT(*)` at up to 250k/1M; keyset deferred. | Performance gate decides. |
| Authenticated admin view | Same public projection; internal-field admin view deferred to Phase 5. | Owner approval required; otherwise a new admin route/DTO must be planned. |
| Availability-date filtering | Not included; only display/state. | Not required by the public capability list. If required, add a query parameter and index/work estimate. |
| Scale target/hardware | Proposed 250k design / 1M stress, not yet owner-confirmed. | Approval needed for performance gate. |
| Public caching/rate limiting | No in-app cache or anonymous throttle; `no-store` retained; proxy-based control expected. | Operational decision; not a Phase 4 blocker unless the owner requires in-app throttling. |
| `Items` metadata freshness | Public reads use denormalized `GuildBankItems`; missing metadata stays unknown and linkable. Loader/freshness remains E-01. | Not a Phase 4 blocker; operational follow-up. |
| API documentation artifact | This plan defines the contract; implementation should add `Docs/PublicInventoryAPI.md`. | Documentation deliverable, not a design blocker. |
| `metadataMissing` public field | Proposed by PD4-02; it is non-sensitive and helps explain blank restrictions. | Owner approval required; removing it changes the DTO and UI copy. |
| `DateAdded` public field | Proposed by PD4-02; it is informative but not required by the public capability list. | Owner may remove it without affecting availability logic. |

### 12.2 Risk register

| ID | Risk | Severity | Likelihood | Mitigation |
|---|---|---|---|---|
| P4-R01 | Public response accidentally includes `GivenTo`, `SourceLocation`, `Mod*` or deleted rows | High | Medium | Dedicated DTO/repository projection, forbidden-field tests, security checklist. |
| P4-R02 | SQL injection or unauthorized sort expression through dynamic query construction | Critical | Low | Parameterized predicates, enum-switch `ORDER BY`, no raw caller text, injection tests. |
| P4-R03 | Stored/reflected XSS or unsafe EQResource URL | High | Low | `html/template`, strict CSP, exact URL validation at use, `target`/`rel`, XSS tests. |
| P4-R04 | Public full-scan search/class/race enables request amplification | Medium | Medium | 5s context, page caps, pool limits, reverse-proxy rate limiting, performance gates. |
| P4-R05 | Off-by-one availability dates or timezone drift | Medium | Medium | One UTC date, inclusive rules, pure boundary tests, explicit UI labels. |
| P4-R06 | Database ordering disagrees with the natural sort key | Medium | Medium | Golden integration test; binary collation or ordered expression if needed; do not declare done on a Go-only test. |
| P4-R07 | Deep offset pages and exact counts exceed latency targets | Medium | Medium | Index migration, page cap, `EXPLAIN`, keyset escalation decision. |
| P4-R08 | Index migration locks or fails on a large/populated table | Medium | Low–Medium | Test on a clone, forward-only migration, documented DDL window, no destructive changes; startup failure is fatal by design. |
| P4-R09 | New indexes slow Phase 3 import commit unacceptably | Medium | Low | Measure commit with indexes; retain only indexes with demonstrated value; owner approves trade-off. |
| P4-R10 | Natural sort key contains control bytes and behaves unexpectedly under database collation | Medium | Medium | Integration fixtures with 1/2/10/11, case/punctuation; verify raw byte order; add `COLLATE` only if justified. |
| P4-R11 | Duplicate per-instance rows make the public list confusing or huge | Low–Medium | Medium | Document record-level semantics; UI shows repeated names with distinct dates/IDs; grouping deferred. |
| P4-R12 | Public/admin route confusion or missing setup gate | High | Low | Public group registration, route matrix tests, setup-pending test, no auth-dependent payload. |
| P4-R13 | Invalid settings URL produces a broken or unsafe link | Medium | Low | Revalidate at use, nil link on failure, template test, no external call. |
| P4-R14 | `FIND_IN_SET` semantics differ for spaces/case or malformed stored data | Medium | Low | Canonical allowlists, exact integration fixtures, blank exclusion tests. |
| P4-R15 | Counting and paging see different snapshots during imports | Medium | Medium | Repeatable-read count+page transaction; document that separate navigations are not a single snapshot. |
| P4-R16 | Public API contract is frozen before the owner decides `ALL`, dates or projection | Medium | Medium | G0 approval gate; no code before PD4-01–PD4-17 disposition. |
| P4-R17 | Later Phase 5 admin projection duplicates or diverges from public mapping | Medium | Medium | Keep mapping in one service, public DTO separate, plan admin projection explicitly in Phase 5. |
| P4-R18 | Tests rely on the development `items` source or real EQResource | High | Low | Disposable synthetic `GuildBankItems` fixture, no external network, read-only source rule. |

### 12.3 Required approvals

The following must be explicitly recorded before implementation. Approval can be a single response that accepts PD4-01–PD4-17 as written, or it can list modifications by ID.

| Approval | Owner | Needed before |
|---|---|---|
| PD4-01–PD4-17 disposition | Product/scope owner | Any Phase 4 code |
| E-07 public projection/auth matrix closure (or acceptance of PD4-02/03) | Owner + architecture reviewer | API/UI |
| E-10 date semantics closure (or acceptance of PD4-11) | Owner | Availability logic/UI |
| E-12 pagination contract closure (or acceptance of PD4-05/13) | Owner + architecture reviewer | API |
| `metadataMissing` and `DateAdded` public fields | Owner | DTO freeze |
| `GuildBankItemID` as public detail key and one-row-per-instance display | Owner | API/UI |
| Location/availability filter deferral (PD4-17) | Owner | Scope freeze |
| `0002_public_inventory_indexes.sql` DDL and migration window | Database/scope owner | Migration |
| Performance hardware, dataset size and §8.5 thresholds | Owner | Performance gate |
| Anonymous rate-limit expectation (proxy vs app) | Operations owner | Deployment |
| Security review sign-off | Owner/security reviewer | Merge |
| Phase 4 implementation authorization | Owner | Any code |

The plan is not self-approving. Until these approvals are recorded, Phase 4 remains **not started**, and Phase 5 is also not authorized.

---

## 13. Review checklist and requested disposition

### 13.1 Questions the reviewer should answer

1. **Record identity:** Is one public row per `GuildBankItemID` acceptable, including repeated item names, or should Phase 4 add a grouped view? (PD4-01)
2. **Projection:** Are `DateAdded` and `metadataMissing` approved as public fields, and are `SourceLocation`/`GivenTo` intentionally excluded? (PD4-02)
3. **Authenticated view:** Is "same public projection, admin navigation only" acceptable for Phase 4, with internal admin data deferred to Phase 5? (PD4-03)
4. **Route shape:** Are `/` list, `/inventory/{id}` detail and `/api/v1/items` the approved public routes? (PD4-04)
5. **Search:** Is name-only substring search, 128-rune limit and literal wildcard handling sufficient? (PD4-06)
6. **Class/race semantics:** Does `ALL` mean "no filter", and must a specific class/race include stored `ALL` while excluding blanks? (PD4-07/08)
7. **Sorting:** Is the eight-value sort allowlist sufficient, with natural name ascending as the default? (PD4-10)
8. **Availability:** Are the inclusive boundaries, UTC "today", and "upcoming before bazaar" precedence correct for this guild? (PD4-11)
9. **Location/availability filters:** Is deferring both filters acceptable for Phase 4? (PD4-17)
10. **Scale:** Are 250k design / 1M stress and the §8.5 thresholds the right targets? (PD4-16/§8)
11. **Database:** Is the index-only `0002` migration approved, and when may it run in Compose/production? (PD4-16/§7.7)
12. **Security:** Is proxy-level anonymous rate limiting acceptable, or is an in-app public limiter required? (§9)

### 13.2 Recommended approval record

If the owner accepts the plan without changes, record a disposition equivalent to:

> Phase 4 plan approved as written. PD4-01 through PD4-17 are approved. E-07, E-10 and E-12 are closed by PD4-02/03, PD4-11 and PD4-05/13 respectively. The index-only migration `0002_public_inventory_indexes.sql` is approved. Performance targets and anonymous rate-limit responsibility are accepted. Phase 4 implementation is authorized; Phase 5 remains unauthorized.

If any item is changed, list it by PD4 ID. Do not begin implementation on an implied or partial approval, because the API contract, UI copy, filters, dates and migration are coupled.

### 13.3 Review outcome

This document is a **plan only**. It does not modify code, schema, runtime configuration or the approved Phase 3 baseline. It creates no Phase 4 route, migration, template or test. The intent is to obtain a reviewed, explicit decision set so that implementation can proceed as one vertical slice without inventing public behavior while coding.

**Current status: approved, implemented and live-validated on 2026-11-22 UTC.
Phase 5 is not authorized.**
