-- 0001_guild_store.sql
-- Guild-store schema for guild inventory, imports and append-only audit.
-- Column and constraint names follow Docs/DatabaseDesign.md §4.2-§4.5.

CREATE TABLE IF NOT EXISTS ImportBatch (
    ImportBatchID    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    FileName         VARCHAR(255) NOT NULL,
    ImportedBy       VARCHAR(64) NOT NULL,
    ImportedAt       DATETIME(6) NOT NULL,
    Status           ENUM('Uploaded','Processing','Completed','Failed') NOT NULL DEFAULT 'Uploaded',
    ImportedByUserID INT NULL,
    FailureReason    TEXT NULL,
    SourceRowCount   INT UNSIGNED NULL,
    AcceptedRowCount INT UNSIGNED NULL,
    FileSha256       CHAR(64) NULL,
    PRIMARY KEY (ImportBatchID),
    KEY ix_batch_status (Status, ImportedAt),
    KEY ix_batch_sha (FileSha256),
    CONSTRAINT ck_batch_status CHECK (Status IN ('Uploaded','Processing','Completed','Failed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS ImportList (
    ImportID      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ImportBatchID BIGINT UNSIGNED NOT NULL,
    EQUID         INT UNSIGNED NOT NULL,
    Name          VARCHAR(128) NOT NULL,
    Quantity      INT UNSIGNED NOT NULL DEFAULT 1,
    LocationRaw   VARCHAR(128) NOT NULL,
    LocationGroup VARCHAR(64) NOT NULL,
    SelectedFg    TINYINT(1) NOT NULL DEFAULT 1,
    ItemType      ENUM('Item','Spell') NOT NULL,
    WarningText   TEXT NULL,
    SourceLine    INT UNSIGNED NULL,
    LocationSort  VARCHAR(80) NULL,
    NameSort      VARCHAR(160) NULL,
    PRIMARY KEY (ImportID),
    KEY ix_import_batch_selected (ImportBatchID, SelectedFg),
    KEY ix_import_batch_location (ImportBatchID, LocationGroup),
    KEY ix_import_batch_equid (ImportBatchID, EQUID),
    KEY ix_import_batch_sort (ImportBatchID, LocationGroup, LocationSort, NameSort, ImportID),
    CONSTRAINT fk_import_batch FOREIGN KEY (ImportBatchID)
        REFERENCES ImportBatch (ImportBatchID) ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT ck_import_quantity CHECK (Quantity >= 1),
    CONSTRAINT ck_import_selected CHECK (SelectedFg IN (0,1)),
    CONSTRAINT ck_import_location CHECK (LocationGroup <> '')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS GuildBankItems (
    GuildBankItemID   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    ItemType          ENUM('Item','Spell') NOT NULL,
    EQUID             INT UNSIGNED NOT NULL,
    MetadataMissingFg TINYINT(1) NOT NULL DEFAULT 0,
    Name              VARCHAR(128) NOT NULL,
    ClassString       VARCHAR(255) NOT NULL,
    RaceString        VARCHAR(255) NOT NULL,
    DateAdded         DATE NOT NULL,
    DateOpen          DATE NOT NULL,
    DateBazaar        DATE NOT NULL,
    Quantity          INT UNSIGNED NOT NULL DEFAULT 1,
    SourceLocation    VARCHAR(128) NULL,
    GivenTo           VARCHAR(128) NULL,
    DeletedFg         TINYINT(1) NOT NULL DEFAULT 0,
    ModAt             DATETIME(6) NOT NULL,
    ModBy             VARCHAR(64) NOT NULL,
    ModByUserID       INT NULL,
    NameSort          VARCHAR(160) NULL,
    PRIMARY KEY (GuildBankItemID),
    KEY ix_gbi_equid (EQUID),
    KEY ix_gbi_public (DeletedFg, ItemType, Name),
    KEY ix_gbi_name_sort (DeletedFg, NameSort),
    KEY ix_gbi_dates (DeletedFg, DateBazaar, DateOpen),
    KEY ix_gbi_given (GivenTo),
    CONSTRAINT ck_gbi_quantity CHECK (Quantity >= 1),
    CONSTRAINT ck_gbi_item_one CHECK (ItemType = 'Spell' OR Quantity = 1),
    CONSTRAINT ck_gbi_deleted CHECK (DeletedFg IN (0,1)),
    CONSTRAINT ck_gbi_meta_missing CHECK (MetadataMissingFg IN (0,1)),
    CONSTRAINT ck_gbi_bazaar CHECK (DateBazaar >= DateAdded),
    CONSTRAINT ck_gbi_open CHECK (DateOpen >= DateAdded)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS Audit (
    AuditID       BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    EventDate     DATETIME(6) NOT NULL,
    UserID        INT NULL,
    UserName      VARCHAR(64) NOT NULL,
    EventType     VARCHAR(64) NOT NULL,
    TableName     VARCHAR(64) NULL,
    RecordID      VARCHAR(64) NULL,
    OldValues     JSON NULL,
    NewValues     JSON NULL,
    Notes         TEXT NULL,
    CorrelationID CHAR(36) NULL,
    SourceIP      VARCHAR(45) NULL,
    PRIMARY KEY (AuditID),
    KEY ix_audit_table (TableName, RecordID, EventDate),
    KEY ix_audit_user (UserName, EventDate),
    KEY ix_audit_event (EventType, EventDate),
    KEY ix_audit_time (EventDate)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;