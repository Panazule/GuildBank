-- 0002_public_inventory_indexes.sql
-- Index-only migration approved by PD4-16. Existing Phase 3 indexes are kept.

ALTER TABLE GuildBankItems
    ADD KEY ix_gbi_public_type_name_sort (DeletedFg, ItemType, NameSort, GuildBankItemID),
    ADD KEY ix_gbi_public_added (DeletedFg, DateAdded, GuildBankItemID),
    ADD KEY ix_gbi_public_open (DeletedFg, DateOpen, GuildBankItemID),
    ADD KEY ix_gbi_public_bazaar (DeletedFg, DateBazaar, GuildBankItemID);