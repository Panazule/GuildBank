# Items Data Profile — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | DP-001 |
| As-of date | 2026-11-21 |
| Source | Development MariaDB `GuildBank.items`, copied from the collector dump `EQItemList.items` |
| Scope | Read-only profile of the externally owned item metadata used by GuildBank |
| Companion documents | `Design.md` v1.6.1, `Docs/DatabaseDesign.md` §4.1, `Docs/ProjectStatus.md` |
| Status | Development copy loaded and profiled; PD-19/PD-20 approved and implemented through the read-only repository, decoders and Phase 2 metadata resolver (2026-11-21) |

This document records what the development copy actually contains. It does not
change the logical contract in `DatabaseDesign.md` §4.1 and does not authorize any
write to the source table. Every query used for this profile was a `SELECT`.

---

## 1. Dataset and environment

| Property | Observed value |
|---|---|
| MariaDB server | `11.4.13-MariaDB-ubu2404` |
| Database | `GuildBank` |
| Physical table | `items` (lowercase; MariaDB reports `lower_case_table_names=0`) |
| Logical name in the specification | `Items` |
| Exact row count | **144,712** |
| Distinct `id` values | **144,712** (no duplicate or null identifiers) |
| `id` range | 1,001–178,976; no zero or negative identifiers |
| Source dump | `Docs/Samples/EQItemList-items.sql`, 218,230,768 bytes, dumped from database `EQItemList` |
| Storage | InnoDB, `utf8mb4_unicode_ci` |
| Approximate data/index size | 153,092,096 / 41,058,304 bytes |

The source dump is local provisioning input, not an application fixture. It
contains a `DROP TABLE IF EXISTS` statement targeting `items` and must never be
applied to a shared or production database as part of an application startup or
test.

---

## 2. Physical schema

The table has **315 columns**. `id` is the only non-text column:

```text
id      BIGINT(20) NOT NULL  PRIMARY KEY
name    LONGTEXT   NULL
classes LONGTEXT   NULL
races   LONGTEXT   NULL
... 311 further collector columns, all LONGTEXT NULL
```

Indexes relevant to the application:

| Index | Definition |
|---|---|
| `PRIMARY` | `id` |
| `idx_name` | `name(255)` |
| `idx_classes` | `classes(20)` |
| `idx_races` | `races(20)` |
| `idx_itemclass`, `idx_itemtype`, `idx_reqlevel`, `idx_reclevel`, `idx_updated` | Other collector-search indexes |

Logical-to-physical mapping:

| Logical contract (DD §4.1) | Physical development column | Notes |
|---|---|---|
| `Items` | `items` | Table names are case-sensitive on this server; queries must use `items` |
| `ID` / `EQUID` | `id` | `BIGINT`, values fit the canonical positive identifier range |
| `Name` | `name` | Not unique: 144,712 rows contain 139,190 distinct names |
| `ClassList` | `classes` | Decimal **text**, not an integer column |
| `RaceList` | `races` | Decimal **text**, not an integer column |
| `MetadataMissingFg` | absent | Application-owned inventory flag; correctly absent from source metadata |

No `ClassList` or `RaceList` column exists in the physical table. A repository
must isolate this mapping; no handler or SQL outside the item repository should
encode the physical names.

---

## 3. Value profile

### 3.1 Names

| Check | Result |
|---|---|
| `name IS NULL` | 0 |
| `name = ''` | 0 |
| Distinct `name` values | 139,190 |
| Distinct case-folded names | 139,190 |
| Rows sharing a name with another row | 5,522 |
| Name length range (characters) | 3–50 |
| Names longer than the logical `VARCHAR(128)` | 0 |

Names are display metadata, not an identity key. Lookups must use `id`/EQUID.

### 3.2 Mask values

Both masks are non-null, non-empty decimal strings. No row contains a
non-numeric value, whitespace, a leading sign, or a value above `UINT32`.

| Check | `classes` | `races` |
|---|---:|---:|
| Distinct values | 413 | 262 |
| Minimum / maximum length | 1 / 10 | 1 / 9 |
| Minimum / maximum value | 0 / 4,211,081,216 | 0 / 251,658,240 |
| Value `0` (no defined class/race bits) | 9,975 | 10,434 |
| Value `65535` (`ALL` under the specification) | 60,948 | 104,340 |
| Value `131071` (`65535` plus undefined high bit 16) | 205 | 70 |
| Values greater than `65535` | **570** | **628** |

For the 570 out-of-range class masks:

- 273 have a defined low 16-bit value of `0`;
- 209 have a defined low 16-bit value of `65535`;
- 88 carry some other defined low-16-bit combination.

For the 628 out-of-range race masks:

- 550 have a defined low 16-bit value of `0`;
- 71 have a defined low 16-bit value of `65535`;
- 7 carry some other defined low-16-bit combination.

The specification defines 16 class bits and 16 race bits and states that
`65535` means `ALL` (`Design.md` L418–L527). The dataset contains values that
do not fit that contract and cannot be decoded correctly without a decision.
The undefined high bits vary widely; they are not a simple constant flag.

---

## 4. Decoding validation

The published decoding fixture resolves exactly as specified:

| Row | Value | Decoded result |
|---|---|---|
| `id=25210`, `Blade of Carnage` | `classes=157` | `WAR,PAL,RNG,SHD,BRD` (1 + 4 + 8 + 16 + 128) |
| `id=25210`, `Blade of Carnage` | `races=65535` | `ALL` |
| `id=10802`, `Flayed Skin Cap` | `races=5` | `Human,Erudite` (1 + 4) |
| `id=10802`, `Flayed Skin Cap` | `classes=1040` | `SHD,NEC` (16 + 1024) |
| `id=1041`, `Worthless Coin` | `classes=0`, `races=0` | No defined class/race bits (blank under the approved PD-20 rule) |

A primary-key lookup by `id` uses the `PRIMARY` index:

```text
SIMPLE  items  const  PRIMARY  PRIMARY  8  const  1
```

### Approved failure behavior for malformed/wide masks

The approved PD-20 policy is:

1. Parse each value as an unsigned 32-bit decimal integer.
2. Decode only the defined low 16 bits using the specification masks.
3. Treat a defined mask of `65535` as `ALL`.
4. Treat a defined mask of `0` as an empty string.
5. When any undefined high bit is set, keep the decoded low-bit result and
   attach a non-fatal metadata warning. Do not alter the EQUID and do not set
   `MetadataMissingFg` (that flag is reserved for an absent `Items` row).
6. If a value is non-numeric, negative, or exceeds `UINT32`, leave the affected
   string blank and attach the same class of warning.

This preserves the meaningful low bits in the 209 + 71 `ALL` cases and imports
the row rather than failing the whole batch. PD-20 approved this policy on
2026-11-21; import warnings persist through PD3-02, and high-bit meanings remain
unknown by design rather than guessed.

---

## 5. Findings and risks

| ID | Finding | Impact |
|---|---|---|
| DP-F1 | Physical table is lowercase `items`; logical names `ClassList`/`RaceList` do not exist. | Any query copied literally from the logical design fails with `ER_NO_SUCH_TABLE` or `ER_BAD_FIELD_ERROR`. Map in one repository. |
| DP-F2 | `classes` and `races` are `LONGTEXT` decimal strings. | Parse and validate in Go; do not use implicit SQL casts or scan into integer types. |
| DP-F3 | 570 class masks and 628 race masks exceed 16 bits. | PD-20 was approved on 2026-11-21: decode low 16 bits, return a structured non-fatal warning for undefined high bits and keep the row. |
| DP-F4 | The development `guildbank` account has `ALL PRIVILEGES ON GuildBank.*`. | Read-only behavior is not enforced by dev grants. Production must use a least-privilege account or a separate metadata account. |
| DP-F5 | The 218 MB SQL dump is present under `Docs/Samples/` and is not tracked. | Keep it local; it is too large for Git and contains a `DROP TABLE` statement. `.gitignore` excludes local dumps. |
| DP-F6 | The source names are not unique. | Never resolve inventory by name; use `id`/EQUID and preserve the copied name as a snapshot. |

---

## 6. Approved decisions

### PD-19 — Physical source mapping

Use `GuildBank.items` with lowercase table name and map `id`, `name`,
`classes`, `races` to the logical `EQUID`, `Name`, `ClassList`, `RaceList`
inside the read-only item repository. Do not rename, alter, or create a view
over the externally owned table. Encapsulate physical names and never use
`SELECT *`.

**Status:** Approved 2026-11-21. Implemented by
`internal/repository/mariadb/items.go`; the application depends on
`domain.Item` and does not reference the physical source names. Single, batch,
count and keyset-page reads are read-only and covered by the opt-in full-table
integration test.

### PD-20 — Out-of-range mask policy

Decode defined low 16 bits only; map `65535` to `ALL` and `0` to blank; retain
the decoded low-bit result and emit a non-fatal warning when undefined high bits
are encountered. `MetadataMissingFg` remains reserved for an absent EQUID.

**Status:** Approved 2026-11-21. Implemented by the repository-independent
`ClassMaskDecoder` and `RaceMaskDecoder` services in `internal/service`.
`ItemMetadataService` combines those results with EQUID resolution; it returns
structured warnings and leaves their persistence to E-14. Malformed non-numeric
source values are a repository parse error rather than an undefined-mask
warning.

---

## 7. Reproduction

The profile can be reproduced with read-only queries against the development
database. Representative statements:

```sql
SELECT COUNT(*), COUNT(DISTINCT id), MIN(id), MAX(id) FROM items;
SELECT id, name, classes, races FROM items WHERE id = 25210;
SELECT COUNT(*) FROM items WHERE CAST(classes AS UNSIGNED) > 65535;
SELECT COUNT(*) FROM items WHERE CAST(races AS UNSIGNED) > 65535;
SHOW INDEX FROM items;
```

Do not run the source dump or any DDL/DML against this table. The application's
runtime queries belong in the MariaDB repository, not in this profile.

---

## 8. Limitations

- The profile is a snapshot of the development copy; it is not a freshness
  guarantee or a production qualification.
- High-bit semantics are unknown. No attempt was made to infer or invent class
  or race meanings beyond the 16 bits defined by the specification.
- The collector loader procedure, refresh cadence and production account model
  remain open under E-01.
- The Phase 2 metadata resolver is implemented and tested but has no separate
  HTTP route; the Phase 3 import pipeline consumes it. Import warnings persist
  in `ImportList.WarningText` under PD3-02; public/manual inventory remain later
  phases.
- The 5,522 duplicate-name rows were counted, not individually dispositioned.