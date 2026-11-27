-- 0008_inventory_request.sql
-- Request workflow Phase 1: additive schema only. Nothing existing is rebuilt,
-- no row is backfilled and no behaviour is switched on.
--
-- Two additions:
--   1. InventoryRequest, the workflow row that records what was asked for, by
--      whom and for which character, plus immutable snapshots (EQUID, ItemName,
--      OnHandAtCreate) so the request still reads correctly after a rename, a
--      consolidation or a later issue of the same lot.
--   2. InventoryMovement.RequestID, the ledger's back-reference to the request
--      an ISSUE movement satisfied, so a movement history can say "issued on
--      request #123" without a reverse lookup on the request table.
--
-- The RequestID column is appended WITHOUT an AFTER clause. MariaDB 11.4 can
-- add a trailing InnoDB column in place (INSTANT); anchoring it to a neighbour
-- with AFTER would force a full copy of the append-only ledger, which is the one
-- table in this schema that only ever grows. Column order carries no meaning
-- here: every statement in this package names its columns explicitly.
--
-- Idempotency: the migration runner records this file in schema_migrations only
-- after every statement below succeeds, so a second Migrate() run is a no-op and
-- executes none of it. Like 0007, the statements themselves are unguarded (the
-- runner has no procedural escape hatch), so a failure part-way through this
-- file must be repaired by hand before the version row is written.

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
    -- MariaDB has no partial indexes. A STORED generated column collapses to
    -- NULL for every non-pending row, and a UNIQUE index tolerates repeated
    -- NULLs, so this enforces "at most one Pending request per character per
    -- item" in the engine, for member and public requests alike. The column
    -- inherits the table collation (utf8mb4_unicode_ci), so the duplicate test
    -- is case-insensitive, matching how character names are compared elsewhere.
    -- Application code never reads PendingCharacterKey.
    PendingCharacterKey VARCHAR(64)
                        AS (CASE WHEN Status = 'Pending' THEN CharacterName ELSE NULL END) STORED,
    UNIQUE KEY uq_request_movement (CompletedMovementID),
    UNIQUE KEY uq_request_pending (GuildBankItemID, PendingCharacterKey),
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
    CONSTRAINT ck_request_issued_le CHECK (
        IssuedQuantity IS NULL OR IssuedQuantity <= RequestedQuantity),
    CONSTRAINT ck_request_completed CHECK (
        Status <> 'Completed' OR (CompletedMovementID IS NOT NULL AND IssuedQuantity IS NOT NULL)),
    CONSTRAINT ck_request_decided CHECK (
        Status = 'Pending' OR (DecidedAt IS NOT NULL AND DecidedByName IS NOT NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

ALTER TABLE InventoryMovement
    ADD COLUMN RequestID BIGINT UNSIGNED NULL;

-- A UNIQUE index permits multiple NULLs, so every manual ISSUE (RequestID NULL)
-- stays legal while one movement can never complete two requests.
CREATE UNIQUE INDEX uq_movement_request ON InventoryMovement (RequestID);

ALTER TABLE InventoryMovement
    ADD CONSTRAINT fk_movement_request
        FOREIGN KEY (RequestID) REFERENCES InventoryRequest (RequestID)
        ON UPDATE RESTRICT ON DELETE RESTRICT;
