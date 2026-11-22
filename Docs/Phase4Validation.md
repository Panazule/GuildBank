# Phase 4 Validation Evidence

**Date:** 2026-11-22 UTC
**Scope:** Public inventory read path (`/`, `/inventory/{id}`, `/api/v1/items`, `/api/v1/items/{id}`)
**Status:** Implemented and live-validated; Phase 5 not started.

## 1. Approval record applied

The owner approved PD4-01 through PD4-17 subject to the documented
performance, indexing and validation conditions. The following owner
clarifications are implemented:

- PD4-11 uses a single UTC calendar date per request.
- `DateAdded`, `DateOpen` and `DateBazaar` calculations remain UTC; the Phase 3
  commit path already derived all three from a UTC date at midnight.
- `DateAdded` and `metadataMissing` are public fields.
- Phase 5 will validate `DateOpen <= DateBazaar` for new writes. Phase 5 was not
  started in this work.
- A future GuildTimezone setting may supersede UTC behavior in a later phase.

Migration `0002_public_inventory_indexes.sql` was approved and applied by the
existing migration runner under its advisory lock. The schema version advanced
from `0001_guild_store` to `0002_public_inventory_indexes` without changing
columns or data.

## 2. Implementation evidence

| Area | Evidence |
|---|---|
| Domain contract | `internal/domain/inventory.go`: eight-value sort enum, validated filter, narrow repository record, public item/page, availability state. |
| Service | `internal/service/inventory.go`: normalization, 128-rune search bound, canonical class/race validation, UTC availability, EQResource revalidation, public mapping and pagination math. `ClassValues`/`RaceValues` and canonical accessors share the decoder source. |
| Repository | `internal/repository/mariadb/inventory.go`: explicit public projection, parameterized search/class/race/type predicates, allowlisted order switch, repeatable-read read-only snapshot, active-only detail. |
| API | `internal/api/inventory.go`: anonymous HTML/JSON handlers, five-second request bound, explicit response DTOs, fields-keyed validation, 404/503 mapping and link building. |
| Routes | Registered in the public group after `SetupGate`; no `RequireAuth` and no admin item route. |
| UI | `inventory_list.html`, `inventory_detail.html`, public navigation, responsive CSS, table caption, labels, availability badges, paginated links and external-link attributes. |
| Wiring | `internal/platform/app/app.go` constructs the inventory service live or degraded. Existing and new router tests pass. |

## 3. Automated validation

Commands run from the repository root:

```text
go test ./...
go vet ./...
gofmt -l cmd internal
git diff --check
```

All completed without failures. New tests cover filter normalization, UTC
availability boundaries (including an inverted window and a non-UTC input),
EQResource URL rejection, settings-degradation behavior, pagination math,
parameterized repository predicates, natural-sort allowlisting, API/HTML
contracts, forbidden-field absence, anonymous and authenticated projection
parity, XSS escaping, method rejection, 404/422/503 paths, and template parsing.

*(Amended 2026-11-22 by the owner decision `PD5-40`: the upper availability
boundary is inclusive, so `open` is `DateOpen <= today < DateBazaar` and
`bazaar` starts on the `DateBazaar` day. The Phase 4 suite recorded above pinned
the earlier `today > DateBazaar` comparison; the current expectations are in
`Phase5Validation.md` §11.)*

## 4. Natural-sort collation and index proof

The database integration fixture inserted `Phase4 Order 1`, `2`, `10` and `11`
with `NaturalSortKey` values and queried through the repository. The observed
order was `1, 2, 10, 11`, proving the persisted key and MariaDB
`utf8mb4_unicode_ci` ordering agree for numeric runs.

The approved nulls-last rule is preserved without forcing a filesort on normal
data. The count query reports whether the filtered snapshot contains null
`NameSort` values; the `NameSort IS NULL ASC` term is added only when such
legacy rows exist. Production/imported rows always carry a key, so the normal
query uses `ix_gbi_name_sort` (or the ItemType-prefixed new index) and does not
sort in memory.

Captured `EXPLAIN` evidence at 1M active rows:

| Shape | Index | Extra |
|---|---|---|
| Default active name order | `ix_gbi_name_sort` | `Using where; Using index` |
| `ItemType='Item'` + name order | `ix_gbi_public_type_name_sort` | `Using where; Using index` |
| Class `WAR` membership + name order | `ix_gbi_name_sort` | `Using where` (membership predicate still evaluated per active row) |
| Date open ascending/descending | `ix_gbi_public_open` | `Using where; Using index` |
| Date bazaar descending | `ix_gbi_public_bazaar` | `Using where; Using index` |

The leading-wildcard `q` predicate and `FIND_IN_SET` class/race predicates remain
non-sargable by design. They scan the active set and are covered by the
measured gates below; the approved escalation (normalized search column and/or
membership tables) is not needed at the 1M target but remains the next scale
step.
## 5. Performance results

Dataset: disposable `GuildBankPhase4Perf` MariaDB 11.4 schema, 50-row pages,
warm pool, repeatable-read read-only transactions, one count plus one page
read. Fixture rows include item/spell types, quantity > 1 spells, class and
race membership lists, blank restrictions, duplicate patterns, parsed and
deleted noise, and date states. Deleted/noise rows were 10% of the active
fixture.

### 5.1 Design target: 250,000 active rows + 25,000 deleted

| Query | p50 | p95 |
|---|---:|---:|
| Default page 1 | 68.2 ms | 72.3 ms |
| Default page 5 | 68.2 ms | 71.7 ms |
| Deep page 200 | 84.6 ms | 93.0 ms |
| Broad name search | 146.3 ms | 161.7 ms |
| No-match name search | 219.6 ms | 246.9 ms |
| Class `WAR` | 123.8 ms | 132.1 ms |
| Race `Human` | 127.9 ms | 128.7 ms |
| Race `Wood Elf` | 127.1 ms | 128.7 ms |
| Combined search + class + race + type | 127.8 ms | 136.8 ms |
| Date open ascending | 68.9 ms | 72.5 ms |
| Date open descending | 67.9 ms | 72.7 ms |
| Date bazaar descending | 68.0 ms | 72.8 ms |
| Detail by ID | < 50 ms | < 50 ms |

### 5.2 Stress target: 1,000,000 active rows + 100,000 deleted

| Query | p50 | p95 | Gate |
|---|---:|---:|---:|
| Default page 1 | 157.6 ms | 287.9 ms | ≤ 1 s |
| Default page 5 | 138.6 ms | 163.2 ms | ≤ 1 s |
| Deep page 200 | 156.1 ms | 181.8 ms | ≤ 1 s |
| Broad name search | 956.8 ms | 1.018 s | ≤ 2 s |
| No-match name search | 1.457 s | 1.507 s | ≤ 2 s |
| Class `WAR` | 821.3 ms | 877.9 ms | ≤ 2 s |
| Race `Human` | 825.5 ms | 887.8 ms | ≤ 2 s |
| Race `Wood Elf` | 827.8 ms | 922.3 ms | ≤ 2 s |
| Combined search + class + race + type | 979.0 ms | 1.037 s | ≤ 2 s |
| Date open ascending | 150.7 ms | 285.3 ms | ≤ 1 s |
| Date open descending | 138.2 ms | 148.7 ms | ≤ 1 s |
| Date bazaar descending | 135.7 ms | 147.8 ms | ≤ 1 s |
| Detail by ID | < 100 ms | < 100 ms | ≤ 100 ms |

The no-match search initially failed the 1M gate at 2.74 s because count and
page each scanned the full active set. The final repository skips the count
when page 1 returns zero rows, which is both snapshot-safe and measurably below
the 2 s stress gate. The temporary performance database and port-forwarding
container were removed after the run.

## 6. Live Compose validation

Rebuilt `guildbank:dev` against Compose MariaDB 11.4 and observed
`schema_version=0002_public_inventory_indexes`.

| Check | Result |
|---|---|
| `/healthz`, `/readyz` | `200`, `200` |
| Anonymous `/` and `/api/v1/items` | `200` with the same public projection |
| Result count | 706 inventory records at validation time |
| Natural first/last record (page size 1) | `Ancient Burrower Hide Tunic` / `Volatilis Wing Cloak` |
| Reverse-name first record | `Volatilis Wing Cloak` |
| `q=Blade` | 1 record |
| `class=WAR` | 673 records |
| `race=Wood Elf` | 685 records |
| `itemType=Spell` | 3 records |
| Detail/list parity | Same record and name for the same `GuildBankItemID` |
| EQResource link | `https://items.eqresource.com/items.php?id=8498` |
| HTML external link | `target="_blank"` and `rel="noopener noreferrer"` present |
| Invalid sort | `422 validation_failed`, field `sort` |
| Absent detail | `404 not_found` |
| App restart | `200`; total remained 706 |
| MariaDB stopped | item API `503 dependency_unavailable`; `/readyz` `503` |
| MariaDB restored | `/readyz` `200`; item API `200`, total 706 |

No fixture rows were written to the development guild database. The temporary
performance schema, logs and proxy were removed.

## 7. Remaining risks and deferred work

1. Leading-wildcard name search and `FIND_IN_SET` class/race filters remain
   full active-set scans. They pass the approved targets at 1M, but the
   normalized-search-column and membership-table escalation remains the next
   scale action.
2. Anonymous in-app rate limiting is not implemented; `no-store` is retained
   and proxy-level rate limiting remains the operational expectation.
3. Legacy `NameSort` nulls trigger the nulls-last expression and a filesort.
   No current/imported row has a null key; the behavior is preserved for
   integrity rather than as a normal query path.
4. No deleted row existed in the development inventory during live validation.
   Deleted/absent detail rejection is covered by repository/API tests; a live
   deleted-row probe was not possible without writing fixture data.
5. Phase 5 manual inventory writes, the admin inventory grid/DTO, location
   normalization/backfill and `DateOpen <= DateBazaar` write validation are not
   started.
6. Production `SELECT`-only `Items` grants and metadata-loader freshness remain
   external operational work (E-01).