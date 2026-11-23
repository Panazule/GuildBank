-- 0006_inventory_movement.sql
-- Partial issuance Phase 1: additive append-only movement ledger.
-- No existing table, quantity or GivenTo value is changed. Existing lots become
-- opening balances; no movement history is backfilled.

CREATE TABLE IF NOT EXISTS InventoryMovement (
    MovementID        BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    GuildBankItemID   BIGINT UNSIGNED NOT NULL,
    EQUID             INT UNSIGNED NOT NULL,
    MovementType      ENUM('ISSUE','RETURN','RECEIVE','MOVE','ADJUST','WRITE_OFF','MERGE') NOT NULL,
    Quantity          INT UNSIGNED NOT NULL,
    Recipient         VARCHAR(128) NULL,
    ActorUserID       INT NULL,
    ActorName         VARCHAR(64) NOT NULL,
    OccurredAt        DATETIME(6) NOT NULL,
    FromLocation      VARCHAR(128) NULL,
    ToLocation        VARCHAR(128) NULL,
    RemainingQuantity INT UNSIGNED NOT NULL,
    RelatedItemID     BIGINT UNSIGNED NULL,
    Note              VARCHAR(255) NULL,
    CorrelationID     CHAR(36) NULL,
    IdempotencyKey    VARCHAR(64) NULL,
    PRIMARY KEY (MovementID),
    UNIQUE KEY uq_movement_idempotency (IdempotencyKey),
    KEY ix_movement_lot_time (GuildBankItemID, OccurredAt),
    KEY ix_movement_equid_time (EQUID, OccurredAt),
    KEY ix_movement_type_time (MovementType, OccurredAt),
    KEY ix_movement_recipient_time (Recipient, OccurredAt),
    KEY ix_movement_correlation (CorrelationID),
    CONSTRAINT fk_movement_item FOREIGN KEY (GuildBankItemID)
        REFERENCES GuildBankItems (GuildBankItemID) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT ck_movement_quantity CHECK (Quantity > 0),
    CONSTRAINT ck_movement_remaining CHECK (RemainingQuantity >= 0),
    CONSTRAINT ck_movement_issue_recipient CHECK (MovementType <> 'ISSUE' OR Recipient IS NOT NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
