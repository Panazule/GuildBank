# Database Design — Aeternus Guild Bank

| Field | Value |
|---|---|
| Document ID | DD-001 |
| Source specification | `Design.md` v1.6.1 (authoritative; body L1–L854, decisions L855–L1161, amendments AM-01–AM-05 at L1163–L1263, informational status at L1265–L1274) |
| Review date | 2026-11-20 |
| Revision | 1.4 — 2026-11-22, implementation status aligned with PD4-01–PD4-17 and migration `0002_public_inventory_indexes` |
| Companion documents | `ArchitectureReview.md`, `ImplementationPlan.md`, `ProjectStatus.md`, `Phase3ImplementationPlan.md`, `Phase4ImplementationPlan.md`, `Phase4Validation.md` |
| Scope | Logical and physical schema for the SQLite application store and the MariaDB guild store |
| Implementation status | SQLite §§3.1–3.3 and MariaDB §§4.1–4.5 are implemented for Phases 1–4; migrations `0001_guild_store` and `0002_public_inventory_indexes` are applied; §3.4 `AuditOutbox` and the general audit UI remain later-phase work |

This document is an implementation-ready data design derived from `Design.md`. Columns marked **SPEC** are mandated by the specification; columns marked **SPEC-V1.6** are mandated by decisions D-01–D-13; columns marked **SPEC-V1.6.1** are mandated by amendments AM-01–AM-05. Columns, tables, constraints and indexes marked **PROPOSED** are engineering additions required to make the specification implementable; each is cross-referenced to a finding in `ArchitectureReview.md`. The additions used by Phases 1–3 were approved through PD-10–PD-20 and PD3-01–PD3-14 and are implemented in the migrations; remaining proposals such as `AuditOutbox` await a later phase. v1.6.1 resolves the two conflicts this design previously recorded (the `-1` sentinel and the sample path), so the physical model now conforms to the specification without a blocking deviation.

---

## 1. Modeling principles

1. **Two stores, one owner each.** SQLite owns identity, configuration and sessions. MariaDB owns items, inventory, imports and audit. No repository spans both stores (AR §3.1). This separation is confirmed by D-13 and is not negotiable in v1.
2. **Inventory is append-only in practice.** `GuildBankItems` rows are never physically deleted (L368); removal sets `DeletedFg = 1` and updates `ModAt`/`ModBy`.
3. **`Items` is externally owned and read-only.** The application only reads it (L242–L244). Runtime grants must enforce this (SR-13).
4. **Derived values are stored deliberately.** `Name`, `ClassString`, `RaceString`, `DateOpen`, `DateBazaar` are copied at creation (L280–L330, L846–L848). Consequence: changes in `Items` or settings do not retroactively alter inventory rows (DC-06).
5. **Cross-store references use immutable snapshots.** No foreign key can span SQLite and MariaDB, so actor identity is stored as `UserID` + `UserName` snapshot (DC-07).
6. **Audit is append-only.** No update or delete path exists for `Audit`; the runtime DB user receives `INSERT` and `SELECT` only (SR-14, D-10).
7. **Dates are date-only values; timestamps are UTC.** Lifecycle fields use `DATE`; event fields use `DATETIME(6)` written in UTC by the application (not DB `NOW()`), avoiding host-timezone drift (IC-12).
8. **Text is UTF-8 (`utf8mb4`).** Item names may contain apostrophes and non-ASCII characters; storage must not assume ASCII (SR-07).
9. **Natural sort requires a persisted key.** Sorting is not delegated to collation (DC-13, IC-11).
10. **Every write maps to one transaction per store.** Cross-store workflows are ordered and documented, never assumed atomic (IC-06, IC-07).
11. **Item identity is `Items.ID`.** D-01 declares `Items.ID` authoritative and synonymous with `GuildBankItems.EQUID` and `ImportList.EQUID`; `GuildBankItemID`, `ImportID` and `ImportBatchID` are unrelated application identifiers.
12. **Spells merge by identity; items never merge.** A spell is identified by `(ItemType='Spell', EQUID)` and increasing its quantity preserves the lifecycle dates (D-06). Every imported item unit becomes its own row (D-07).
13. **Missing metadata must not corrupt identity.** An unresolved identifier is preserved exactly as imported and flagged with `MetadataMissingFg` (AM-01/AM-02). `EQUID` is never replaced by a sentinel, and because an unresolved id is absent from `Items`, the physical FK is not enforced; the service validates the reference when metadata becomes available.

---

## 2. Schema map

| Store | Table | Owner | App access | Spec source |
|---|---|---|---|---|
| SQLite | `Users` | Application | Read/Write | L166–L181 |
| SQLite | `Settings` | Application | Read/Write | L190–L214 |
| SQLite | `Sessions` | Application | Read/Write | L216–L227; D-09 (L1066–L1082) |
| SQLite | `AuditOutbox` **PROPOSED (later phase)** | Application | Read/Write | DC-03 |
| MariaDB | `Items` | External loader | **Read only** (PD-19; production grant pending E-01) | L233–L259 |
| MariaDB | `GuildBankItems` | Application | Insert/Update (no delete); implemented | L261–L374 |
| MariaDB | `ImportBatch` | Application | Insert/Update (no delete); implemented | L531–L549 |
| MariaDB | `ImportList` | Application | Insert/Update (no delete); implemented | L551–L605 |
| MariaDB | `Audit` | Application | Insert only; import-scoped writes implemented | L107, L773–L783; D-10 (L1083–L1116) |

The store split in this table is fixed by D-13 (L1147–L1161) and is not a migration convenience: SQLite never holds guild data and MariaDB never holds credentials or sessions.

Database names: SQLite file `/config/guildbank.db` (L83–L85); MariaDB database `GuildBank` (L93–L95).

---

## 3. SQLite schema (application data)

Connection prerequisites: `PRAGMA foreign_keys = ON;` per connection; `PRAGMA journal_mode = WAL;`; `PRAGMA busy_timeout = 5000;`; `PRAGMA synchronous = NORMAL;`. Use a dedicated write connection (IC-08).
### 3.1 `Users`

Spec fields (L172–L181): `UserID (PK)`, `UserName`, `PasswordHash`, `AccessLevel`, `LastLogin`, `DisabledFg`, `ModAt`. Passwords use bcrypt and are never plaintext (L181–L183).

```sql
CREATE TABLE Users (
    UserID        INTEGER PRIMARY KEY AUTOINCREMENT,
    UserName      TEXT    NOT NULL COLLATE NOCASE,   -- SPEC
    PasswordHash  TEXT    NOT NULL,                  -- SPEC: bcrypt (60 chars)
    AccessLevel   TEXT    NOT NULL DEFAULT 'Admin'   -- SPEC value set
                          CHECK (AccessLevel IN ('Admin')),
    LastLogin     TEXT    NULL,                      -- SPEC: UTC ISO-8601
    DisabledFg    INTEGER NOT NULL DEFAULT 0         -- SPEC
                          CHECK (DisabledFg IN (0,1)),
    ModAt         TEXT    NOT NULL,                  -- SPEC: UTC ISO-8601
    CreatedAt     TEXT    NOT NULL,                  -- IMPLEMENTED (Phase 1)
    ModBy         TEXT    NULL,                      -- IMPLEMENTED: username snapshot
    MustChangePassword INTEGER NOT NULL DEFAULT 0    -- IMPLEMENTED
                          CHECK (MustChangePassword IN (0,1))
);

CREATE UNIQUE INDEX ux_users_username ON Users (UserName COLLATE NOCASE);
```

**Notes**
- Uniqueness and `NOT NULL` are refinements; the spec lists fields but not constraints (MR-11). Case-insensitive uniqueness prevents `Admin`/`admin` confusion.
- `AccessLevel` is constrained to the only documented value (L185–L187). Adding roles is a migration.
- `DisabledFg` is the intended deactivation mechanism; `DELETE /api/users/{id}` should disable (DC-11). Physical deletion is not granted to the runtime user.
- bcrypt hashes are ~60 characters; do not apply a short `VARCHAR` limit.

### 3.2 `Settings`

Spec fields (L194–L198): `SettingKey`, `SettingValue`. Initial settings (L198–L213): `GuildName`, `GuildOpenDays`, `BazaarDays`, `EQResourceBaseUrl` with defaults `Aeternus`, `14`, `30`, `https://items.eqresource.com/items.php?id=`.

The wizard also "stores configuration" for MariaDB (L147–L164). Those keys are not enumerated by the specification; the implemented names were approved with the Phase 1 settings set.

```sql
CREATE TABLE Settings (
    SettingKey   TEXT PRIMARY KEY,                   -- SPEC
    SettingValue TEXT NOT NULL,                      -- SPEC
    SettingType  TEXT NOT NULL DEFAULT 'string'      -- IMPLEMENTED (MR-09)
                     CHECK (SettingType IN ('string','integer','boolean','secret','url')),
    IsSecret     INTEGER NOT NULL DEFAULT 0          -- IMPLEMENTED (SR-02)
                     CHECK (IsSecret IN (0,1)),
    ModAt        TEXT NOT NULL,                      -- IMPLEMENTED
    ModBy        TEXT NULL                           -- IMPLEMENTED
);
```

Seed rows (idempotent `INSERT ... ON CONFLICT(SettingKey) DO NOTHING`):

| SettingKey | Seed value | Type | Secret | Validation |
|---|---|---|---|---|
| `GuildName` | `Aeternus` | string | no | 1–64 chars |
| `GuildOpenDays` | `14` | integer | no | `0 ≤ n ≤ 3650` |
| `BazaarDays` | `30` | integer | no | `0 ≤ n ≤ 3650` |
| `EQResourceBaseUrl` | `https://items.eqresource.com/items.php?id=` | url | no | `https`, host allow-list, ends with `id=` |
| `MariaDbHost` | *(setup)* | string | no | hostname/IP, non-empty |
| `MariaDbPort` | `3306` | integer | no | `1–65535` |
| `MariaDbDatabase` | `GuildBank` | string | no | identifier-safe |
| `MariaDbUsername` | *(setup)* | string | no | non-empty |
| `MariaDbPassword` | *(setup)* | secret | **yes** | non-empty |
| `SetupComplete` | `0` | boolean | no | must be `1` before setup routes close |
| `SessionIdleMinutes` | `30` | integer | no | `5–1440` (APPROVED PD-15) |
| `SessionAbsoluteHours` | `8` | integer | no | `1–168` (APPROVED PD-15) |

**Secret storage limitation.** SQLite offers no native secret protection. Options: (a) rely on `/config` volume permissions and document the trust boundary; (b) encrypt `SettingValue` with a key injected via environment or stored outside the volume. Encryption with a key stored beside the ciphertext adds little. The chosen approach must be recorded in the specification (SR-02, E-11). Regardless, `GET /api/settings` must never return `IsSecret = 1` values (MR-09).

### 3.3 `Sessions`

Spec fields (L221–L227): `SessionID`, `UserID`, `CreatedAt`, `LastAccessed`, `ExpiresAt`.

```sql
CREATE TABLE Sessions (
    SessionID   TEXT PRIMARY KEY,                -- SPEC-V1.6 (D-09): opaque session id, never the raw token
    TokenHash   TEXT NOT NULL UNIQUE,            -- SPEC-V1.6 (D-09): SHA-256 of the raw token
    UserID      INTEGER NOT NULL
                REFERENCES Users(UserID) ON DELETE CASCADE,
    CreatedAt   TEXT NOT NULL,
    LastAccessed TEXT NOT NULL,
    ExpiresAt   TEXT NOT NULL,                   -- idle expiry
    -- APPROVED PD-15 (SR-03, E-08)
    AbsoluteExpiresAt TEXT NOT NULL,
    RevokedAt   TEXT NULL,
    ClientIP    TEXT NULL,
    UserAgent   TEXT NULL
);

CREATE INDEX ix_sessions_user     ON Sessions (UserID);
CREATE INDEX ix_sessions_expiry   ON Sessions (ExpiresAt);
CREATE INDEX ix_sessions_absolute ON Sessions (AbsoluteExpiresAt);
```

**Notes**
- D-09 mandates a distinct `TokenHash` column: the raw token exists only in the client cookie, `TokenHash` is the lookup key and `SessionID` must never be the token or its hash image presented to the browser (SR-03, SR-22).
- `TokenHash` is unique so a token maps to exactly one session; `SessionID` remains the key used by foreign references, logs and revocation.
- `ON DELETE CASCADE` is defensive; the runtime disables users rather than deleting them.
- Expired rows must be purged by a scheduled task (`DELETE FROM Sessions WHERE ExpiresAt < ? OR AbsoluteExpiresAt < ?`); SQLite has no event scheduler (DC-15).
- `LastAccessed` writes on every request are a contention risk; update at most once per minute per session (IC-08).

### 3.4 `AuditOutbox` **PROPOSED**

> **Phase 1 implementation note:** the owner's Phase 1 scope excludes audit, so
> migration `0001` creates `Users`, `Settings` and `Sessions` but not
> `AuditOutbox`. The table below remains the approved target for the audit
> phase; settings/user writes currently have no audit side effect.

Required by DC-03: settings and user changes live in SQLite, while audit lives in MariaDB. If MariaDB is unavailable, the write must still succeed and the audit must not be lost. An outbox is the minimal reliable pattern.

```sql
CREATE TABLE AuditOutbox (
    OutboxID    INTEGER PRIMARY KEY AUTOINCREMENT,
    OccurredAt  TEXT NOT NULL,
    Payload     TEXT NOT NULL,        -- JSON audit record, secrets already redacted
    Attempts    INTEGER NOT NULL DEFAULT 0,
    LastError   TEXT NULL,
    SentAt      TEXT NULL
);

CREATE INDEX ix_outbox_pending ON AuditOutbox (SentAt, OccurredAt);
```

Drain policy: on startup and after each settings/user write, insert pending payloads into MariaDB `Audit` in `OutboxID` order; mark `SentAt` on success. Failed rows retry with backoff. Rows are never deleted before `SentAt` is set; after 30 days sent rows may be purged (MR-20).

### 3.5 SQLite schema version

Use `PRAGMA user_version` for forward-only migrations (IC-05). Each migration increments it once. Startup refuses to run when the file version is newer than the binary. Setup mode exists only while the `Users` table contains no administrator (D-02); file presence and `SetupComplete` are not gates, and `SetupComplete` remains a cache/telemetry flag. If `Users` is missing or unreadable, startup fails closed with a recovery instruction rather than offering setup, because an absent table is a corruption/upgrade fault, not a first run (DC-24).

---

## 4. MariaDB schema (guild data)

All tables: `ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`. No `TIMESTAMP` columns are used for lifecycle data; `DATE` holds date-only values and `DATETIME(6)` holds UTC event times written by the application (IC-12).

### 4.1 `Items` (externally owned, read-only)

Spec content (L233–L259): imported EQ Item Collector database with `EQUID`, `Name`, `ClassList`, `RaceList`, `Stats`, `Effects`, `Requirements`, `Restrictions` "and all imported collector fields". Types are not specified, so the authoritative DDL belongs to the loader, not the application (MR-05).

Minimum contract the application depends on:

| Column | Type | Required | Use |
|---|---|---|---|
| `ID` (= `EQUID`) | `INT UNSIGNED` | yes (PK) | Canonical item key (D-01); the two names denote the same value |
| `Name` | `VARCHAR(128)` | yes | Copied into inventory at creation (L280–L283) |
| `ClassList` | `INT UNSIGNED` | yes | Class bitmask decoded into `ClassString` (L285–L296) |
| `RaceList` | `INT UNSIGNED` | yes | Race bitmask decoded into `RaceString` (L298–L309) |
| `Stats`, `Effects`, `Requirements`, `Restrictions` | `TEXT`/`JSON` | loader-defined | Displayed item detail; not decoded by the app |
| other collector fields | loader-defined | no | Passthrough data |

```sql
-- Reference shape; owned by the out-of-band collector loader (MR-05)
CREATE TABLE IF NOT EXISTS Items (
    ID          INT UNSIGNED NOT NULL,               -- D-01: authoritative id; EQUID is the same value
    Name        VARCHAR(128) NOT NULL,
    ClassList   INT UNSIGNED NOT NULL DEFAULT 65535,
    RaceList    INT UNSIGNED NOT NULL DEFAULT 65535,
    Stats        TEXT NULL,
    Effects      TEXT NULL,
    Requirements TEXT NULL,
    Restrictions TEXT NULL,
    LoadedAt     DATETIME(6) NULL,
    PRIMARY KEY (ID),
    KEY ix_items_name (Name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Rules**
- The application must never issue `INSERT`/`UPDATE`/`DELETE` against this table; enforce with grants (SR-13).
- `65535` in either mask means `ALL`; the decoder treats `ALL` as matching every class or race (L294–L307, IC-09).
- The loader may add columns; the application must tolerate unknown columns and must not use `SELECT *`.

> **Development dataset note (2026-11-21).** The development copy is now
> provisioned as `GuildBank.items` — lowercase, because the server reports
> `lower_case_table_names=0`, and with 144,712 rows. Its physical columns are
> `id` (`BIGINT` primary key), `name`, `classes` and `races`; the table has 315
> columns in total and the remaining 311 are nullable `LONGTEXT` collector
> fields. `ClassList`/`RaceList` and `MetadataMissingFg` do not exist in the
> source. The repository must map `id`→`EQUID`, `name`→`Name`,
> `classes`→`ClassList` and `races`→`RaceList` and isolate the physical names
> (PD-19, approved 2026-11-21).
>
> `classes` and `races` are decimal strings, not integer columns. All 144,712
> values are numeric, but 570 class masks and 628 race masks exceed the
> specified 16-bit range (maximum `4,211,081,216` and `251,658,240`
> respectively). The defined decoder alone is therefore insufficient for this
> dataset; PD-20 (approved 2026-11-21) decodes the low 16 bits, maps `65535` to
> `ALL`, and returns a structured non-fatal warning for undefined high bits
> without rejecting the row. `ItemMetadataService` returns those warnings to
> callers without choosing a persistence sink (E-14); `MetadataMissingFg` stays
> reserved for an absent EQUID. See `Docs/ItemsDataProfile.md` for the full
> profile and evidence.
>
> The development `guildbank` account currently has `ALL PRIVILEGES` on
> `GuildBank.*`; read-only access is a code rule there, not a grant. Production
> must enforce `SELECT`-only access for this table (E-01, SR-13).

### 4.2 `GuildBankItems`

Spec fields (L266–L374): `GuildBankItemID` (PK), `ItemType` (`Item`|`Spell`), `EQUID`, `Name`, `ClassString`, `RaceString`, `DateAdded`, `DateOpen`, `DateBazaar`, `Quantity`, `SourceLocation`, `GivenTo`, `DeletedFg`, `ModAt`, `ModBy`, plus `MetadataMissingFg` (AM-02).

```sql
CREATE TABLE GuildBankItems (
    GuildBankItemID BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ItemType        ENUM('Item','Spell') NOT NULL,           -- SPEC
    EQUID           INT UNSIGNED NOT NULL,                   -- SPEC; equals Items.ID (D-01)
    MetadataMissingFg TINYINT(1) NOT NULL DEFAULT 0,         -- SPEC-V1.6.1 (AM-02); 1 when EQUID was absent from Items at commit
    Name            VARCHAR(128) NOT NULL,                   -- SPEC
    ClassString     VARCHAR(255) NOT NULL,                   -- SPEC
    RaceString      VARCHAR(255) NOT NULL,                   -- SPEC
    DateAdded       DATE NOT NULL,                           -- SPEC
    DateOpen        DATE NOT NULL,                           -- SPEC
    DateBazaar      DATE NOT NULL,                           -- SPEC
    Quantity        INT UNSIGNED NOT NULL DEFAULT 1,         -- SPEC
    SourceLocation  VARCHAR(128) NULL,                       -- SPEC
    GivenTo         VARCHAR(128) NULL,                       -- SPEC
    DeletedFg       TINYINT(1) NOT NULL DEFAULT 0,           -- SPEC
    ConsolidatedInto BIGINT UNSIGNED NULL,                   -- QTY-09 (absorbed-row marker)
    ModAt           DATETIME(6) NOT NULL,                    -- SPEC (UTC)
    ModBy           VARCHAR(64) NOT NULL,                    -- SPEC (username snapshot)
    ModByUserID     INT NULL,                                -- APPROVED and implemented (DC-07)
    NameSort        VARCHAR(160) NULL,                       -- APPROVED and implemented (DC-13)
    PRIMARY KEY (GuildBankItemID),
    KEY ix_gbi_equid     (EQUID),
    KEY ix_gbi_public    (DeletedFg, ItemType, Name),
    KEY ix_gbi_name_sort (DeletedFg, NameSort),
    KEY ix_gbi_dates     (DeletedFg, DateBazaar, DateOpen),
    KEY ix_gbi_given     (GivenTo),
    -- Added by approved index-only migration 0002_public_inventory_indexes.
    KEY ix_gbi_public_type_name_sort (DeletedFg, ItemType, NameSort, GuildBankItemID),
    KEY ix_gbi_public_added (DeletedFg, DateAdded, GuildBankItemID),
    KEY ix_gbi_public_open  (DeletedFg, DateOpen, GuildBankItemID),
    KEY ix_gbi_public_bazaar (DeletedFg, DateBazaar, GuildBankItemID),
    CONSTRAINT ck_gbi_quantity  CHECK (Quantity >= 1),
    -- ck_gbi_item_one was dropped by 0005_item_quantity_model (QTY-01/QTY-02).
    CONSTRAINT ck_gbi_deleted   CHECK (DeletedFg IN (0,1)),
    CONSTRAINT ck_gbi_meta_missing CHECK (MetadataMissingFg IN (0,1)),
    CONSTRAINT ck_gbi_bazaar    CHECK (DateBazaar >= DateAdded),
    CONSTRAINT ck_gbi_open      CHECK (DateOpen  >= DateAdded)
    -- No physical FK to Items: an unresolved EQUID is absent from Items (AM-01).
    -- The service validates EQUID against Items whenever the row exists.
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes**
- `ck_gbi_item_one` encoded L332–L341: ordinary items were single instances. **Dropped 2026-11-22 by `0005_item_quantity_model.sql` (QTY-01/QTY-02).** An ordinary item row now stores the quantity of one imported or manually created line; `ck_gbi_quantity (Quantity >= 1)` is the remaining invariant.
- `ConsolidatedInto` (**added by `0005`, QTY-09**) marks a row absorbed by the owner-run item-stack consolidation command. It names the surviving row, is set in the same transaction as `DeletedFg=1`, and blocks restore.
- `ModBy` stores the username at the time of the action because the user row lives in SQLite (DC-07); `ModByUserID` preserves the numeric identity without a cross-store FK.
- `DateOpen`/`DateBazaar` are computed at insert time from settings (L314–L330). The CHECKs prevent inverted windows but cannot detect later settings changes (DC-06).
- The physical FK to `Items` is intentionally **not enforced** (AM-01). An unresolved `EQUID` is by definition absent from `Items` at import time, so a foreign key would reject exactly the rows D-08 requires. `EQUID` still denotes the collector's item identifier (D-01), is never rewritten, and is validated in the service whenever the referenced `Items` row becomes available. `MetadataMissingFg = 1` marks the unresolved state and `ix_gbi_equid` keeps lookups cheap. An index is not a constraint, so the identifier remains the single key with no sentinel or shadow column. `Name` is copied at insert (L281–L283), so an `Items` rename never rewrites inventory (intended by L296/L309/L321).
- No `ON DELETE` path exists: the runtime user has no `DELETE` grant (principle 2). Soft delete is the only removal.
- `NameSort` is a zero-padded, case-folded natural sort key maintained by the service (IC-11). It is nullable only so the migration can backfill; production writes always set it. Public reads use the `NameSort` index; the nulls-last expression is applied only when a filtered snapshot actually contains null keys.
- `0002_public_inventory_indexes` is index-only: it adds `ix_gbi_public_type_name_sort`, `ix_gbi_public_added`, `ix_gbi_public_open` and `ix_gbi_public_bazaar` and retains all prior indexes. `EXPLAIN` and 250k/1M performance evidence are in `Phase4Validation.md`. Class/race membership (`FIND_IN_SET`) and leading-wildcard name search remain scan predicates.

### 4.3 `ImportBatch`

Spec fields (L531–L549): `ImportBatchID`, `FileName`, `ImportedBy`, `ImportedAt`, `Status` with `Uploaded`, `Processing`, `Completed`, `Failed`; history is retained.

```sql
CREATE TABLE ImportBatch (
    ImportBatchID  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    FileName       VARCHAR(255) NOT NULL,              -- SPEC
    ImportedBy     VARCHAR(64)  NOT NULL,              -- SPEC (username snapshot, DC-07)
    ImportedAt     DATETIME(6)  NOT NULL,              -- SPEC (UTC)
    Status         ENUM('Uploaded','Processing','Completed','Failed')
                                NOT NULL DEFAULT 'Uploaded',  -- SPEC
    ImportedByUserID INT NULL,                          -- APPROVED and implemented (Phase 3)
    FailureReason  TEXT NULL,                           -- APPROVED and implemented (MR-12)
    SourceRowCount INT UNSIGNED NULL,                   -- APPROVED and implemented (Phase 3)
    AcceptedRowCount INT UNSIGNED NULL,                 -- APPROVED and implemented (Phase 3)
    IgnoredRowCount INT UNSIGNED NULL,                  -- IMP-01: nested-slot rows ignored at staging
    FileSha256     CHAR(64) NULL,                       -- APPROVED and implemented (duplicate detection)
    PRIMARY KEY (ImportBatchID),
    KEY ix_batch_status (Status, ImportedAt),
    KEY ix_batch_sha (FileSha256),
    CONSTRAINT ck_batch_status CHECK (Status IN ('Uploaded','Processing','Completed','Failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes**
- `ImportedBy` cannot be a foreign key (SQLite users, DC-07); it is a label plus optional numeric reference.
- History retention (L549) means the runtime never deletes batches. `Failed` rows persist with `FailureReason` (MR-12).
- Allowed transitions are `Uploaded → Processing → Completed|Failed`; `Completed` and `Failed` are terminal (DC-16). Status changes are guarded by `SELECT ... FOR UPDATE` during commit.
- `FileSha256` is nullable and indexed by `ix_batch_sha`; the import service uses it to report duplicate uploads without rejecting them (MR-12). `ck_batch_status` is defense in depth behind the service state machine.

### 4.4 `ImportList`

Spec fields (L551–L567): `ImportID`, `ImportBatchID`, `EQUID`, `Name`, `Quantity`, `LocationRaw`, `LocationGroup`, `SelectedFg`. Data remains available for review and troubleshooting (L556).

```sql
CREATE TABLE ImportList (
    ImportID       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ImportBatchID  BIGINT UNSIGNED NOT NULL,            -- SPEC
    EQUID          INT UNSIGNED NOT NULL,               -- SPEC
    Name           VARCHAR(128) NOT NULL,               -- SPEC
    Quantity       INT UNSIGNED NOT NULL DEFAULT 1,     -- SPEC
    LocationRaw    VARCHAR(128) NOT NULL,               -- SPEC
    LocationGroup  VARCHAR(64)  NOT NULL,               -- SPEC (normalized)
    SelectedFg     TINYINT(1) NOT NULL DEFAULT 1,       -- SPEC
    ItemType       ENUM('Item','Spell') NOT NULL,       -- APPROVED (Phase 3 staging contract); required by D-06/D-07
    WarningText    TEXT NULL,                           -- APPROVED PD3-02 (structured warning JSON)
    SourceLine     INT UNSIGNED NULL,                   -- APPROVED (troubleshooting)
    LocationSort   VARCHAR(80) NULL,                    -- APPROVED PD3-11 (DC-13)
    NameSort       VARCHAR(160) NULL,                   -- APPROVED PD3-11 (DC-13)
    PRIMARY KEY (ImportID),
    KEY ix_import_batch_selected (ImportBatchID, SelectedFg),
    KEY ix_import_batch_location (ImportBatchID, LocationGroup),
    KEY ix_import_batch_equid    (ImportBatchID, EQUID),
    KEY ix_import_batch_sort     (ImportBatchID, LocationGroup, LocationSort, NameSort, ImportID),
    CONSTRAINT fk_import_batch FOREIGN KEY (ImportBatchID)
        REFERENCES ImportBatch (ImportBatchID)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT ck_import_quantity CHECK (Quantity >= 1),
    CONSTRAINT ck_import_selected CHECK (SelectedFg IN (0,1)),
    CONSTRAINT ck_import_location CHECK (LocationGroup <> '')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes**
- Deliberately **no FK on `EQUID`**: staging must accept a row whose metadata is missing or stale, because classification/metadata validation happens at commit (DC-08). Staging is a landing zone, not inventory.
- `ItemType` is populated at parse time by the same classifier used at commit (DC-18); D-06/D-07 make it load-bearing. PD3-07 approved it as a non-null staging field; legacy nullable data is not part of the implemented schema.
- `WarningText` is the approved sink for import warnings (PD3-02). It stores a compact JSON array of structured warnings, is nullable and is never used for control flow.
- `NameSort` was added by PD3-11 so the import grid and location drill-down follow natural-sort order for names as well as locations; `ix_import_batch_sort` serves those reads.
- `LocationRaw` is preserved exactly as imported; `LocationGroup` is the normalized selector value (L569–L605, D-05, PD3-01). Both are retained so a normalization-rule fix can be re-run without re-upload (MR-04, IC-14).
- Deselected rows remain in the table (L556); they are excluded at commit and never deleted (MR-17).

### 4.5 `Audit` **SPEC-V1.6**

D-10 defines the table, its ten fields and its append-only rule (L1083–L1116), replacing the proposal that previously filled this gap (MR-01). The field names below are therefore normative and must not be renamed; `CorrelationID` and `SourceIP` are implemented nullable additions.

```sql
CREATE TABLE Audit (
    AuditID      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,  -- SPEC-V1.6 (D-10)
    EventDate    DATETIME(6) NOT NULL,                     -- SPEC-V1.6; UTC, app-generated
    UserID       INT NULL,                                 -- SPEC-V1.6; SQLite Users.UserID snapshot
    UserName     VARCHAR(64) NOT NULL,                     -- SPEC-V1.6; snapshot, 'system' for bootstrap
    EventType    VARCHAR(64) NOT NULL,                     -- SPEC-V1.6; e.g. 'ITEM_CREATED', 'LOGIN'
    TableName    VARCHAR(64) NULL,                         -- SPEC-V1.6; affected table or logical entity
    RecordID     VARCHAR(64) NULL,                         -- SPEC-V1.6; string form of the affected key
    OldValues    JSON NULL,                                -- SPEC-V1.6; redacted before/after snapshots
    NewValues    JSON NULL,                                -- SPEC-V1.6
    Notes        TEXT NULL,                                -- SPEC-V1.6
    CorrelationID CHAR(36) NULL,                           -- IMPLEMENTED (request traceability)
    SourceIP     VARCHAR(45) NULL,                         -- IMPLEMENTED (IPv4/IPv6 textual)
    PRIMARY KEY (AuditID),
    KEY ix_audit_table (TableName, RecordID, EventDate),
    KEY ix_audit_user  (UserName, EventDate),
    KEY ix_audit_event (EventType, EventDate),
    KEY ix_audit_time  (EventDate)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

**Notes**
- `OldValues`/`NewValues` must omit `PasswordHash` and `MariaDbPassword` (SR-15). The recorder consults `Settings.IsSecret` and a fixed denylist before serializing.
- D-10 calls its event list "example", so the taxonomy is **not closed** (DC-25, E-09). The implementation must publish a closed allowlist, use the exact names (`LOGIN`, `LOGOUT`, `IMPORT_CREATED`, `IMPORT_COMMITTED`, `ITEM_CREATED`, `ITEM_UPDATED`, `ITEM_REMOVED`, `USER_CREATED`, `USER_UPDATED`, `USER_DISABLED`, `SETTINGS_UPDATED`) and map internal detail events such as restore or selection changes onto them or an approved extension.
- `TableName`/`RecordID` have no enumerated values. For SQLite-origin events (`Users`, `Settings`) `TableName` is a logical label, not a foreign key; no cross-store constraint is possible.
- MariaDB implements `JSON` as `LONGTEXT` plus a JSON validity check, so these declarations are MySQL-compatible and migration-safe.
- The runtime user holds `INSERT` and `SELECT` only; there is no update or delete route (SR-14, D-10). No retention period is specified (MR-20).
- Authentication events originate in SQLite while `Audit` is in MariaDB: they are delivered through `AuditOutbox`. If MariaDB is unavailable, login still succeeds but its audit row is queued; this is the only way to honour D-10 without blocking authentication.

---

## 5. Relationships

```
SQLite (guildbank.db)
  Users 1 ──── n Sessions            (FK, ON DELETE CASCADE)
  Users 1 ──── n AuditOutbox         (logical: UserID/UserName in payload)

MariaDB (GuildBank)
  Items 1 ──── n GuildBankItems      (Items.ID ← GuildBankItems.EQUID, RESTRICT; D-01)
  ImportBatch 1 ──── n ImportList    (ImportBatchID, RESTRICT)
  ImportBatch 1 ──── n Audit         (logical: EntityType='ImportBatch', EntityID)
  GuildBankItems 1 ──── n Audit      (logical: EntityType='GuildBankItem', EntityID)
```

| Relationship | Enforced by | Cross-store? | Notes |
|---|---|---|---|
| `Sessions.UserID → Users.UserID` | SQLite FK | No | Cascade only for physical user deletion, which is not a supported flow |
| `GuildBankItems.EQUID → Items.ID` | Logical reference (no physical FK) | No | AM-01 preserves the imported id and relaxes FK enforcement; the service validates when `Items` has the row |
| `ImportList.ImportBatchID → ImportBatch.ImportBatchID` | MariaDB FK | No | `RESTRICT` preserves history; batches are never deleted |
| `GuildBankItems.ModBy(UserID)` → `Users.UserID` | Application only | **Yes** | Store username snapshot + numeric ID; validate on write (DC-07) |
| `ImportBatch.ImportedBy(UserID)` → `Users.UserID` | Application only | **Yes** | Same pattern as `ModBy` |
| `Audit.UserID` → `Users.UserID` | Application only | **Yes** | Snapshot plus optional numeric reference; D-10 field names are normative |
| `AuditOutbox` → `Audit` | Application | **Yes** | Outbox row is inserted into MariaDB; `SentAt` tracks delivery |

---

## 6. Cross-store integrity rules

Because SQLite and MariaDB cannot share a transaction, these rules are the contract:

1. **Actor validity is checked before the MariaDB write.** The service loads the acting user from SQLite; if the user is missing or disabled, the operation is rejected with `403` before any guild write occurs.
2. **Snapshots are immutable.** `ModBy`, `ImportedBy` and `UserName` in audit are copied at write time and never updated when a user is renamed or disabled. History remains interpretable.
3. **Deletion is disable.** `DELETE /api/users/{id}` sets `DisabledFg = 1` and revokes sessions; it never removes the row. This preserves cross-store references (DC-07, DC-11).
4. **SQLite is the source of truth for settings/users; MariaDB is the source of truth for inventory/imports/audit.** No field is duplicated across stores except the identity snapshots.
5. **Outbox delivery is at-least-once.** A crash after inserting into `Audit` but before marking `SentAt` can duplicate an audit row on retry. Duplicates are acceptable for audit; the alternative (lost entries) is not. A `CorrelationID` makes duplicates detectable.
6. **Ordering rule for mixed writes.** Perform the MariaDB write first, then the SQLite write, then flush the outbox. If the SQLite step fails, the MariaDB change stands and the failure is surfaced; if the MariaDB step fails, nothing is changed. Reversing this order can leave unaudited configuration changes.
7. **Authentication audit crosses stores.** D-10 places `Audit` in MariaDB but login/logout/user events originate in SQLite. The SQLite write commits first and the outbox row is part of the same transaction; delivery is asynchronous (D-10, D-13).

---

## 7. Transaction specifications

Sections 7.1–7.3 describe later inventory surfaces; 7.4, 7.5 and 7.9 are the
implemented import transactions. Import commit takes a per-database advisory
lock in addition to the batch `FOR UPDATE` (PD3-04) and writes inventory, batch
state and import-scoped audit in one MariaDB transaction.

### 7.1 First-run setup (SQLite only, then MariaDB validation)

| Step | Store | Action | Commit point |
|---|---|---|---|
| 1 | SQLite | Open/create file, migrate to current `user_version`, seed default settings | Transaction 1 |
| 2 | MariaDB | Validate connection (`Ping`, read-only smoke query); create schema/migrations if the configured user has DDL rights (or instruct the operator) | No application data |
| 3 | SQLite | In one transaction: insert the first administrator (`AccessLevel='Admin'`, bcrypt hash) **and** write the `MariaDb*` settings; Setup Mode ends at commit | Transaction 2 |
| 4 | SQLite | Set `SetupComplete = 1` as telemetry/cache only | Transaction 3 |

- D-02 makes the existence of an administrator the sole setup switch, so the administrator row must not be committed before the wizard's remaining steps are complete. The wizard may collect Step 1 fields first, but persistence happens in Step 3. If the specification instead requires the administrator to be persisted at Step 1 (body L138–L145), D-02 must be amended, because setup routes would otherwise close before MariaDB is configured (DC-24).
- Steps 1–3 are the documented exception to "all writes through REST" (DC-14); they run through services, never raw SQL in handlers.
- If Step 2 fails, nothing is persisted for MariaDB and no administrator exists, so the wizard resumes safely. Re-running is idempotent by username check.
- Bootstrap audit is written to `AuditOutbox` with `UserName='system'` and delivered once MariaDB is reachable; it cannot be written to MariaDB before Step 2 succeeds (SR-14, D-10).

### 7.2 Manual inventory create (`POST /api/items`)

```
BEGIN;
  SELECT id, name, classes, races FROM items WHERE id = ?;  -- must exist; logical names mapped per §4.1
  -- decode masks; compute today's date; compute DateOpen/DateBazaar from settings
  INSERT INTO GuildBankItems (
      ItemType, EQUID, Name, ClassString, RaceString,
      DateAdded, DateOpen, DateBazaar, Quantity,
      SourceLocation, GivenTo, DeletedFg, ModAt, ModBy, ModByUserID, NameSort)
  VALUES (...);
  INSERT INTO Audit (...) VALUES ('inventory.create', ...);
COMMIT;
```

- Ordinary items are inserted once per instance with `Quantity = 1`; spells accept a quantity > 1 (L332–L341).
- `DateAdded = today` (UTC date), `DateOpen = today + GuildOpenDays`, `DateBazaar = today + BazaarDays` (L311–L330).
- A missing `EQUID` in `Items` is a `422` unless the specification chooses placeholder creation (DC-08).

### 7.3 Inventory update and soft delete

```
BEGIN;
  SELECT ... FROM GuildBankItems WHERE GuildBankItemID = ? FOR UPDATE;
  UPDATE GuildBankItems
     SET Quantity = ?, GivenTo = ?, SourceLocation = ?, DeletedFg = ?, ModAt = ?, ModBy = ?
   WHERE GuildBankItemID = ?;
  INSERT INTO Audit (...) VALUES ('inventory.update' | 'inventory.remove', ...);
COMMIT;
```

- `DELETE /api/items/{id}` sets `DeletedFg = 1`; it never issues SQL `DELETE` (L368, DC-11).
- `Name`, `ClassString`, `RaceString`, `DateAdded`, `DateOpen`, `DateBazaar` are immutable after creation (L296–L330). A lifecycle-policy change does not rewrite existing rows (DC-06).
- Restoring a removed row is a `DeletedFg = 0` update and must be audited as `inventory.restore`.
- AI-02 (implemented): an edit may request removal in the same transaction. The
  field update and the soft delete commit together; `ITEM_UPDATED` is written
  only for changed editable fields and `ITEM_REMOVED` is always written, both
  with the same `EventDate` and `CorrelationID`. Restoring later does not revert
  the edits.
- AI-01 (implemented): page-local bulk removal locks the selected rows in
  ascending `GuildBankItemID` order, re-validates every `ExpectedModAt`, and
  commits all deletions plus one `ITEM_REMOVED` per record in one transaction.
  A single stale or already-removed record rolls the selection back. There is no
  bulk event type and no filter-wide action.

### 7.4 Import upload and staging (`POST /api/imports`)

```
BEGIN;                                    -- MariaDB
  INSERT INTO ImportBatch (FileName, ImportedBy, ImportedAt, Status, FileSha256)
  VALUES (?, ?, ?, 'Processing', ?);
  -- rows collected by the streaming parser before this transaction
  INSERT INTO ImportList (ImportBatchID, EQUID, Name, Quantity,
                          LocationRaw, LocationGroup, SelectedFg, ItemType,
                          WarningText, SourceLine, LocationSort, NameSort)
  VALUES (...), (...);                    -- batched
  UPDATE ImportBatch SET Status = 'Uploaded', SourceRowCount = ?, AcceptedRowCount = ?, IgnoredRowCount = ?, FileSha256 = ?;
  INSERT INTO Audit (...) VALUES ('IMPORT_CREATED', ...);
COMMIT;
```

- Format contract (D-04, AM-03): the sample at `Docs/Samples/InventoryExport-Sample.txt` is tab-delimited with header `Location`, `Name`, `ID`, `Count`, `Slots`; `Count` maps to `Quantity` and `Slots` is collector metadata. The file is read as UTF-8; blank lines and section-header rows are skipped; three-field rows default `Count` to 1; a non-numeric fourth field is treated as `Slots`; rows with `ID = 0` or an empty `Name` are discarded.
- Parsing failures set `Status='Failed'`, `FailureReason`, and commit whatever diagnostics apply; a failed batch keeps zero inventory impact because nothing is written to `GuildBankItems` (MR-12).
- `SelectedFg` is computed at staging by the single default rule. A container row
  (a raw location without a trailing `-Slot<n>`) is **not** selected in General,
  Bank or SharedBank; every Hoard row **is** selected (IMP-05); every
  `Unspecified` row is **never** selected (IMP-02). The rule is not retroactive:
  an existing batch keeps its persisted selection, and `GET
  /api/imports/{id}/default-selection` plus `PUT .../selection` is the explicit
  way to adopt it. The `SelectedFg` column default remains `1` because rows
  always supply the value explicitly; it is not the policy.
- Location normalization is applied once here and stored in `LocationGroup` using the D-05/PD3-01 rules: `General`/`Bank`/`SharedBank` `n-Slot m` → `Area n`, every `Hoard<n>` → `Hoard`, and equipment/blank/unparseable reserved patterns → `Unspecified` with a warning. `LocationRaw` is never rewritten. `LocationSort` and `NameSort` are written by the natural-sort key function.
- IMP-01: a well-formed row whose location has two or more trailing slot suffixes (`General 1-Slot2-Slot1`) describes the contents of a container, not a bank slot. It is not staged; `IgnoredRowCount` records the count, `AcceptedRowCount` excludes it, and the `IMPORT_CREATED` audit payload carries `ignoredRows`. The empty nested rows in the canonical sample are `ID = 0` discards and therefore do not increment this counter.
- Staging records the unresolved-metadata warning in `WarningText`; `MetadataMissingFg` belongs to `GuildBankItems` and is set at commit when the `EQUID` is absent from `Items` (AM-01/AM-02, PD3-02, PD3-07).
- Staging rows are retained indefinitely for review and troubleshooting (L556, MR-20).

### 7.5 Import commit (`POST /api/imports/{id}/commit`)

```
BEGIN;                                                       -- MariaDB
  SELECT GET_LOCK(import-commit-lock, timeout);              -- per-database serialization
  SELECT Status FROM ImportBatch
   WHERE ImportBatchID = ? FOR UPDATE;                       -- must be 'Uploaded'
  -- reject if Status IN ('Processing','Completed','Failed')
  UPDATE ImportBatch SET Status = 'Processing' WHERE ImportBatchID = ?;

  -- 1. item rows (ItemType='Item'): one inventory row per unit
  INSERT INTO GuildBankItems (...) SELECT ... FROM ImportList
   WHERE ImportBatchID = ? AND SelectedFg = 1 AND ItemType = 'Item';

  -- 2. spell rows: merge by (ItemType='Spell', EQUID), per D-06
  UPDATE GuildBankItems
     SET Quantity = Quantity + ?, ModAt = ?, ModBy = ?
   WHERE EQUID = ? AND ItemType = 'Spell' AND DeletedFg = 0;
  -- if affected rows = 0, INSERT a new spell row (Quantity = staged quantity);
  -- DateAdded/DateOpen/DateBazaar are never changed by a merge (D-06)
  -- selection state comes only from ImportList.SelectedFg (D-11)

  UPDATE ImportBatch SET Status = 'Completed', AcceptedRowCount = ?;
  INSERT INTO Audit (...) VALUES ('IMPORT_COMMITTED', ...);
  -- plus one ITEM_CREATED summary and one ITEM_UPDATED per merged spell (PD3-14)
  SELECT RELEASE_LOCK(import-commit-lock);
COMMIT;
```

- **All-or-nothing.** Inventory inserts, merges, batch status and audit share one transaction; a failure rolls back inventory completely (IC-06).
- **Idempotent terminal state.** `Completed` cannot be re-committed; a retry returns `409 Conflict` (DC-16).
- **Row locking.** The `FOR UPDATE` on the batch serializes concurrent commit attempts.
- **Quantity expansion.** D-07 requires one inventory row per imported item unit; a staged spell row produces one row with summed quantity (L682–L698).
- **Merge provenance** (which row and location received the added quantity) was settled for imports by PD3-03/PD3-04/PD3-14: only active rows are merged, multiple active targets resolve to the lowest `GuildBankItemID`, the anomaly is reported, and each merge writes an `ITEM_UPDATED` audit row with `OldValues`/`NewValues`.
- **Spell metadata** follows the item rule (PD3-16/DEFECT-01): a newly inserted spell row uses the resolved `items.name` and the decoded class/race strings, and `MetadataMissingFg=1` with the imported name and blank strings only when the EQUID is unresolved. A merge never rewrites stored name, class/race or dates (D-06).
- **Other rows** (`LocationGroup='Unspecified'`) are internal staging rows (PD3-15/IMP-C): this transaction selects only `LocationGroup <> 'Unspecified'`, so such a row can never be imported, even on a legacy batch that still carries `SelectedFg=1`.
- **Selection and location filters** apply to `SelectedFg` only; location deselection is expressed by updating `SelectedFg` for all rows in the group, through the D-11 endpoint (MR-03).

### 7.6 Settings update (`PUT /api/settings`)

1. Validate the payload against the types and ranges in §3.2; reject unknown keys unless the specification allows extensibility.
2. If `MariaDb*` keys change: build a candidate pool, `Ping` it, and only continue on success (DC-12).
3. Begin the SQLite transaction: update `Settings`, write an `AuditOutbox` row covering the changed keys (secret values redacted), commit.
4. Swap the pooled connection for the candidate; if the swap fails, restore the old pool and report a `500` with the previous settings intact.
5. Flush the outbox to MariaDB `Audit`.

Settings are cached in memory; the cache is invalidated inside the same transaction that updates the rows (IC-19).

### 7.7 User create / disable / password change

- Create: validate username (trim, case-insensitive uniqueness), hash with bcrypt, insert, write outbox audit.
- Disable: set `DisabledFg = 1`, set `ModAt`/`ModBy`, delete all sessions for the user, write outbox audit — all in one SQLite transaction (SR-03).
- Password change: update `PasswordHash`, set `MustChangePassword = 0`, revoke sessions except the current one, write audit.
- Never physically delete a user; the last enabled administrator cannot be disabled (MR-11).

### 7.8 Login, request validation and logout

```
-- login
SELECT UserID, PasswordHash, DisabledFg FROM Users WHERE UserName = ? COLLATE NOCASE;
-- bcrypt compare; generic failure message on any mismatch or disabled account
INSERT INTO Sessions (SessionID, TokenHash, UserID, CreatedAt, LastAccessed, ExpiresAt, AbsoluteExpiresAt)
VALUES (?, ?, ?, now, now, now + idle, now + absolute);
UPDATE Users SET LastLogin = now WHERE UserID = ?;
-- Set-Cookie: token (raw); DB stores SHA-256(token)
```

Per request: hash the cookie value, select the session joined to `Users`, reject when `RevokedAt IS NOT NULL`, `ExpiresAt <= now`, `AbsoluteExpiresAt <= now`, or `DisabledFg = 1`; otherwise optionally extend `ExpiresAt` and touch `LastAccessed` at most once per minute.

Logout deletes (or revokes) the row and clears the cookie. A background task purges expired rows (DC-15).

---

### 7.9 Import selection (`PUT /api/imports/{batchId}/selection`)

D-11 requires an endpoint that persists `ImportList.SelectedFg` from a set of selected `ImportID` values. It is a single MariaDB transaction:

```
BEGIN;
  SELECT Status FROM ImportBatch WHERE ImportBatchID = ? FOR UPDATE;
  -- reject when Status IN ('Processing','Completed','Failed')
  UPDATE ImportList SET SelectedFg = 0 WHERE ImportBatchID = ?;
  UPDATE ImportList SET SelectedFg = 1 WHERE ImportBatchID = ? AND ImportID IN (...);
  -- assert the final selected count equals the requested set before COMMIT
COMMIT;
```

- The payload is a **full replacement set** scoped to the batch; IDs that do not belong to the batch, duplicates and non-positive IDs are `422 selection_invalid`. This is implemented under D-11 and the approved Phase 3 contract.
- Location-group toggles resolve to row-level `SelectedFg` updates for every row in the group; the selector remains a derived view.
- Selection changes are intentionally **not audited** (PD3-08): they are staging state, and the selected set remains recoverable from `ImportList.SelectedFg`. The commit audit records the resulting counts.

---

## 8. Migrations and schema lifecycle

| Store | Mechanism | Rules |
|---|---|---|
| SQLite | `PRAGMA user_version` + embedded ordered migrations | Forward-only; each migration runs in a transaction; startup refuses `user_version > binary version`; backup the file before migrating |
| MariaDB | `schema_migrations (Version VARCHAR(32) PRIMARY KEY, AppliedAt DATETIME(6) NOT NULL)` **IMPLEMENTED** | Forward-only; per-database advisory lock (`GET_LOCK`) around migration; lexical downgrade guard; fatal startup on migration failure (PD3-10) |
| `Items` | Out-of-band loader | Not managed by application migrations; version/compatibility documented separately (MR-05) |

Startup order: (1) open SQLite, migrate, (2) load settings, (3) if the `Users` table has no administrator, serve setup only (D-02), (4) connect MariaDB, migrate, (5) serve application. A failure at any step leaves the previous state usable and logs a structured error; it never auto-recreates a database (SR-01).

Migration content rules: add columns as `NULL` or with defaults; backfill in a separate migration; never rename a column in place without a compatibility view; keep `Items` DDL in the loader repository, not the application (SR-13).

---

## 9. Query design, indexes and natural sorting

### 9.1 Public inventory read

```sql
SELECT GuildBankItemID, ItemType, EQUID, Name, ClassString, RaceString,
       DateAdded, DateOpen, DateBazaar, Quantity, GivenTo
  FROM GuildBankItems
 WHERE DeletedFg = 0
   AND (:class = 'ALL' OR ClassString = 'ALL' OR FIND_IN_SET(:class, ClassString) > 0)
   AND (:race  = 'ALL' OR RaceString  = 'ALL' OR FIND_IN_SET(:race,  RaceString)  > 0)
   AND (:search IS NULL OR Name LIKE CONCAT('%', :search, '%'))
 ORDER BY NameSort ASC, GuildBankItemID ASC
 LIMIT :limit OFFSET :offset;
```

- `DeletedFg = 0` is mandatory in every public read (IC-17).
- `ClassString = 'ALL'` short-circuits token matching (IC-09); `FIND_IN_SET` handles comma-separated tokens that contain spaces (race names).
- `Name LIKE '%…%'` cannot use a B-tree index; acceptable at guild scale, with `FULLTEXT` as the later upgrade path (IC-10).
- The public projection excludes `ModBy`, `ModByUserID`, `DeletedFg` and (unless declared public) `SourceLocation` (DC-10, MR-06).

### 9.2 Counts and pagination

- Page counts must use the same predicates as the page query; a mismatched count is a correctness bug.
- Offset pagination is acceptable for the page-number UI (L821). Keyset pagination `(NameSort, GuildBankItemID) > (?, ?)` is preferred for API consumers.
- Enforce a maximum `limit` (default 50, max 200) in the service (SR-18).

### 9.3 Import review reads

```sql
SELECT LocationGroup, COUNT(*) AS RowCount, SUM(SelectedFg) AS SelectedCount
  FROM ImportList
 WHERE ImportBatchID = ? AND LocationGroup <> 'Unspecified'
 GROUP BY LocationGroup
 ORDER BY MIN(LocationSort);

SELECT ImportID, EQUID, Name, Quantity, LocationRaw, LocationGroup, SelectedFg, ItemType
  FROM ImportList
 WHERE ImportBatchID = ? AND LocationGroup <> 'Unspecified' AND LocationGroup = ?
 ORDER BY LocationSort, ImportID;
```

`ix_import_batch_location` serves both statements. Summary counts must state their unit (staged rows vs. resulting inventory rows), per DC-18. The `Unspecified` predicate is PD3-15 (IMP-C): those rows stay staged and counted by `ImportBatch.AcceptedRowCount`, but the import review never lists, selects or commits them, so `stagedRows` and `totalLocations` report the reviewable set.

### 9.4 Audit reads

`GET /api/audit` filters by `EventDate` range, `UserName`, `EventType` or `TableName`; the four indexes in §4.5 map to those filters. Pagination is mandatory (SR-18). No query joins across stores; the actor label is stored locally.

### 9.5 Natural sort key

Natural sort (L699–L728) is implemented as a persisted key, not a collation trick:

1. Lower-case the value (Unicode casefold).
2. Replace every run of characters outside `[a-z0-9]` with a single space; trim.
3. Split into alternating text/number runs; zero-pad each numeric run to 10 digits.
4. Join runs with a separator that sorts before digits (e.g., ASCII `0x01`), so `General 2` precedes `General 10`.

| Input | Key | Order |
|---|---|---|
| `General 1` | `general\x010000000001` | 1st |
| `General 2` | `general\x010000000002` | 2nd |
| `General 3` | `general\x010000000003` | 3rd |
| `General 10` | `general\x010000000010` | 4th |
| `General 11` | `general\x010000000011` | 5th |
| `Bank 4` | `bank\x010000000004` | — |
| `Hoard` | `hoard` | — |
| `SharedBank 1` | `sharedbank\x010000000001` | — |

`LocationSort` and `NameSort` in `ImportList`, and `NameSort` in `GuildBankItems`, are written by the same key function. Ties break on the numeric primary key for deterministic pagination (IC-11). D-05 normalizes every `Hoard<n>` value to `Hoard`, and AM-04 makes `Hoard` the single spelling, so all hoard locations collapse to one selector entry; PD3-01 resolves unknown patterns to `Unspecified`.

### 9.6 Bitmask decoding

| Mask input | Result | Notes |
|---|---|---|
| `0` | empty string | No classes/races; UI supplies a display fallback |
| `3` | `WAR,CLR` | L461–L463 |
| `5` | `Human,Erudite` | L508–L510 |
| `65535` | `ALL` | L455–L457, L502–L504 |
| `157` (Blade of Carnage) | `WAR,PAL,RNG,SHD,BRD` | 1+4+8+16+128; validation fixture (L512–L529) |
| unknown high bits | ignored | Log once at `WARN`; never store `UNKNOWN` tokens |

- Class order: WAR 1, CLR 2, PAL 4, RNG 8, SHD 16, DRU 32, MNK 64, BRD 128, ROG 256, SHM 512, NEC 1024, WIZ 2048, MAG 4096, ENC 8192, BST 16384, BER 32768 (L423–L453).
- Race order: Human 1 … Drakkin 32768 (L470–L500); races containing spaces are joined with commas exactly as displayed.
- Output is comma-separated; classes carry no spaces, race names keep their internal spaces. `ALL` is the literal upper-case token.
- The mask is decoded once at inventory creation; `Items` corrections never rewrite existing strings (L296, L309).

---

## 10. Privileges, security and data governance

### 10.1 Grant matrix

| Principal | SQLite | MariaDB |
|---|---|---|
| Application runtime | Owner of `guildbank.db` | `SELECT` on `Items`; `SELECT, INSERT, UPDATE` on `GuildBankItems`; `SELECT, INSERT, UPDATE` on `ImportBatch`; `SELECT, INSERT, UPDATE` on `ImportList`; `SELECT, INSERT` on `Audit` |
| Migration principal | Owner | `CREATE, ALTER, INDEX, DROP, REFERENCES` on `GuildBank`, plus DML for backfills |
| `Items` loader | — | `INSERT, UPDATE, DELETE` on `Items` only |

```sql
GRANT SELECT                 ON GuildBank.Items          TO 'guildbank_app'@'%';
GRANT SELECT, INSERT, UPDATE ON GuildBank.GuildBankItems TO 'guildbank_app'@'%';
GRANT SELECT, INSERT, UPDATE ON GuildBank.ImportBatch    TO 'guildbank_app'@'%';
GRANT SELECT, INSERT, UPDATE ON GuildBank.ImportList     TO 'guildbank_app'@'%';
GRANT SELECT, INSERT         ON GuildBank.Audit          TO 'guildbank_app'@'%';
```

The absence of `DELETE` is intentional: it makes soft delete and audit immutability database-enforced rather than code conventions (SR-13, SR-14, D-10).

### 10.2 Sensitive data

| Data | Where | Control |
|---|---|---|
| Password hashes | SQLite `Users.PasswordHash` | bcrypt; never logged; redacted from audit |
| MariaDB password | SQLite `Settings.MariaDbPassword` | `IsSecret = 1`; never returned by API; redacted from audit and logs; at-rest protection per E-11 |
| Session tokens | Cookie + SQLite `Sessions.TokenHash` | Store SHA-256 only; `SessionID` is never the raw token; `HttpOnly; Secure` when HTTPS (D-09, D-12) |
| Member names in `GivenTo` | MariaDB `GuildBankItems` | Public projection decision (DC-10); included in audit only when changed |
| Source IPs | `Audit.SourceIP` | Retain per policy; truncate/anonymize where required |

### 10.3 Audit payload rules

- Record `UserID`/`UserName`, `EventType`, `TableName`/`RecordID`, UTC `EventDate` and correlation ID for every write (MR-13, D-10).
- Before/after state goes in `OldValues`/`NewValues` and contains only changed fields where practical; always redact `PasswordHash` and `IsSecret` values (SR-15).
- Never include imported file contents in `Audit`; reference `ImportBatchID` instead.
- No update or delete audit endpoints are exposed (SR-14, D-10).

### 10.4 Retention and backup

| Data | Retention | Mechanism |
|---|---|---|
| `GuildBankItems` | Indefinite (soft delete only) | No purge; `DeletedFg = 1` |
| `ImportBatch` / `ImportList` | Indefinite per L549–L556; operator may archive after an agreed period | Archive to cold storage; never delete active references |
| `Audit` | ≥ 2 years recommended (not specified) | Operator archival; insert-only grants |
| `Sessions` | Until expiry | Daily purge job |
| `AuditOutbox` (sent rows) | 30 days | Purge job |

**Backups:** checkpoint WAL (`PRAGMA wal_checkpoint(TRUNCATE)`) before copying `guildbank.db`, or copy the database together with `-wal`/`-shm` while writes are paused; use `mariadb-dump --single-transaction` for `GuildBank`. Restore MariaDB before SQLite if inventory must be visible immediately; `Items` must be restored from the loader when the database is rebuilt. Backups contain the MariaDB password and inherit the same protection requirements (SR-02).

---

## 11. Validation summary

| Table | Constraint | Source |
|---|---|---|
| `Users` | unique case-insensitive `UserName`; `DisabledFg ∈ {0,1}`; `AccessLevel ∈ {'Admin'}` | L174–L187, MR-11 |
| `Settings` | `SettingKey` unique; types constrained to the declared set; numeric ranges enforced in the service | L190–L214, MR-09 |
| `Sessions` | FK to `Users`; `TokenHash` unique and non-null; timestamps present; expiry columns non-null | L216–L227, D-09 |
| `GuildBankItems` | `Quantity ≥ 1`; item quantity = 1 unless `ItemType='Spell'`; `DeletedFg ∈ {0,1}`; `MetadataMissingFg ∈ {0,1}`; `DateOpen ≥ DateAdded`; `DateBazaar ≥ DateAdded`; `EQUID` indexed but FK not enforced, validated by the service (AM-01) | L266–L374, D-01, D-07 |
| `ImportBatch` | `Status` restricted to four values; terminal states not re-entered | L543–L548, DC-16 |
| `ImportList` | `Quantity ≥ 1`; `SelectedFg ∈ {0,1}`; `LocationGroup` non-empty; FK to batch | L558–L567 |
| `Audit` | Insert-only; `EventDate`, `UserName`, `EventType` non-null; JSON validity for `OldValues`/`NewValues` | L1083–L1116, D-10 |

Service-level rules that cannot be expressed as DDL: item identity is `Items.ID` (D-01); an unresolved `EQUID` preserves the source id, sets `MetadataMissingFg`, and is validated against `Items` when metadata becomes available (AM-01/AM-02); lifecycle dates derive from settings at creation (L314–L330); spells merge on `(ItemType='Spell', EQUID)` without changing dates (D-06); items never merge (D-07); the last enabled administrator cannot be disabled (MR-11); and settings secrets are never serialized into responses or audit.

---

## 12. Decision conformance and traceability

### 12.1 v1.6 decision conformance

| Decision | Schema implementation | Status |
|---|---|---|
| D-01 Canonical item identifier | §4.1 `Items.ID` PK; §4.2 `EQUID` indexed, FK relaxed per AM-01; §5 relationship | Conformant |
| D-02 Setup mode | §3.5, §7.1, §8 | Conformant at schema level; step ordering conflict recorded (DC-24) |
| D-03 HTMX / REST | No schema impact; enforced by layering | Not applicable |
| D-04 Import contract | §7.4 staging shape; sample present at `Docs/Samples/InventoryExport-Sample.txt` | **Conformant** — D-04 path and AM-03 contract (tab-delimited header `Location/Name/ID/Count/Slots`, UTF-8, row rejection) are implemented by §7.4 |
| D-05 Location normalization | §4.4 `LocationRaw`/`LocationGroup`; §9.5 key | **Conformant and implemented** — PD3-01 resolves equipment/blank/unknown patterns to `Unspecified`; all `Hoard<n>` collapse to `Hoard` |
| D-06 Spell merge | §4.2 quantity rule and §7.5 merge path | **Conformant and implemented** — PD3-03/PD3-04 settle active-only targets, lowest-id anomaly handling and concurrency; QTY-02 leaves spell quantity unrestricted by the dropped item-only constraint |
| D-07 Item import | §4.2 quantity rule; §7.5 one row per imported item line | **Conformant and implemented, amended 2026-11-22 by QTY-01/QTY-02** — one row per line with the line quantity; later imports do not merge; manual create follows the same model |
| D-08 Missing metadata | §4.2 `MetadataMissingFg` (SPEC-V1.6.1); §4.4 `WarningText` (PD3-02); service validation of `EQUID` | **Conformant and implemented** — AM-01/AM-02 preserve the identifier, forbid `-1` and define the flag; import warnings persist in `ImportList.WarningText` |
| D-09 Session security | §3.3 `TokenHash`; §10.2 | **Conformant and implemented** for storage and the PD-15 lifecycle; broader lifecycle review may refine values |
| D-10 Audit table | §4.5, §10.1 grants, §10.3 | **Conformant** — import-scoped writes implemented by PD3-14; general taxonomy/retention and audit UI remain later-phase work |
| D-11 Selection endpoint | §7.9, §4.4 indexes | **Conformant and implemented** — full-replacement selection with batch ownership checks, `422` invalid IDs and `409` terminal state |
| D-12 Security requirements | §3.3, §10.1–§10.2 | **Conformant** — CSRF coverage, cookie flags, lockout and parameterized queries are implemented for the current surface |
| D-13 Store separation | §1, §2 | Conformant |

### 12.2 Resolved and remaining schema decisions

| Decision | Affects | Status / cross-reference |
|---|---|---|
| `Items` provisioning, ownership and freshness (identity fixed by D-01) | `Items`, `GuildBankItems` | **Remaining operational follow-up** — E-01, MR-21; the development copy is loaded and validated (144,712 rows) |
| `MetadataMissingFg` refresh procedure and audit of later resolution | `GuildBankItems`, service layer | **Remaining later-phase procedure** — AM-02, MR-22; import-time flagging is implemented |
| Warning sink for unresolved items | `ImportList.WarningText` | **Resolved for imports** by PD3-02 and implemented |
| Spell merge provenance, soft-deleted targets and concurrency | `GuildBankItems` commit path | **Resolved for imports** by PD3-03/PD3-04/PD3-14 and implemented |
| Selection payload semantics and response | §7.9 | **Resolved and implemented** — full replacement, batch-scoped, `422` invalid IDs, `409` terminal state |
| Unknown location grammar and case/whitespace normalization (`Hoard` settled by AM-04) | `ImportList.LocationGroup` | **Resolved for imports** by PD3-01: `Unspecified` with warnings; every `Hoard<n>` collapses to `Hoard` |
| Session lifecycle: entropy, rotation, `SameSite`, idle expiry, purge | `Sessions`, `TokenHash` | **Resolved and implemented** by PD-15; broader review may refine values |
| Secret-at-rest approach for MariaDB credentials | `Settings` | **Remaining review** — E-11/SR-02; the `/config` permission boundary is the current mitigation |
| Closed general audit event taxonomy and retention | `Audit` | **Partially resolved** — import subset closed by PD3-14; general taxonomy/retention and UI remain open (DC-25, E-09, MR-20) |
| Public vs. admin field projection | All read paths | **Remaining later-phase contract** — DC-10, MR-06 |
| Whether natural sort keys are persisted columns or generated | `NameSort`, `LocationSort` | **Resolved and implemented** — persisted columns written by PD3-11/DC-13 |

### 12.3 Spec area coverage

| Spec area | Schema section | Status |
|---|---|---|
| SQLite tables (L166–L231) | §3.1–§3.3 | **Implemented** for Phase 1; `TokenHash` specified; `AuditOutbox` remains later-phase |
| `Items` (L233–L259) | §4.1 | Identity fixed (D-01) and read-only mapping implemented (PD-19); provisioning and production grants remain open (MR-21/E-01) |
| `GuildBankItems` (L261–L374) | §4.2 | **Implemented**; D-06/D-07 rules encoded; AM-01/AM-02 missing-metadata fields specified |
| `ImportBatch` (L531–L549) | §4.3 | **Implemented**; failure, hash and observability fields approved |
| `ImportList` (L551–L605) | §4.4 | **Implemented**; `ItemType`, `WarningText`, `LocationSort` and `NameSort` approved by PD3 decisions |
| `Audit` (L107, L773–L783; D-10) | §4.5 | **Implemented** for import-scoped writes; general taxonomy, retention and UI open |
| v1.6 decisions and v1.6.1 amendments (L855–L1263; status L1265–L1274) | §1, §3.3, §4.1–§4.5, §7.9 | 12 conformant, 1 not applicable |
| Decoding/sorting rules (L376–L529, L699–L728) | §9.5–§9.6 | Implementable; fixtures defined |
| Security requirements (L797–L804; D-12) | §10 | Governance model defined at schema level |