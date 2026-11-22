-- 0003_admin_inventory.sql
-- Phase 5: persisted D-05 location groups and administrative query indexes.

ALTER TABLE GuildBankItems
    ADD COLUMN LocationGroup VARCHAR(64) NULL AFTER SourceLocation,
    ADD COLUMN LocationSort  VARCHAR(80) NULL AFTER LocationGroup,
    ADD KEY ix_gbi_admin_location
        (DeletedFg, LocationGroup, LocationSort, NameSort, GuildBankItemID),
    ADD KEY ix_gbi_admin_modified
        (DeletedFg, ModAt, GuildBankItemID),
    ADD KEY ix_gbi_admin_type_modified
        (DeletedFg, ItemType, ModAt, GuildBankItemID);

ALTER TABLE Audit
    ADD KEY ix_audit_correlation (CorrelationID, EventDate);