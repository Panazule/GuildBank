-- QTY-01/QTY-02: an ordinary item row stores the quantity of one imported or
-- manually created line instead of being expanded into one row per unit.
-- ck_gbi_quantity (Quantity >= 1) remains the quantity invariant, and no unique
-- index is added on EQUID because a later import of the same item must create a
-- new row rather than merge.
ALTER TABLE GuildBankItems DROP CONSTRAINT IF EXISTS ck_gbi_item_one;

-- QTY-09: rows absorbed by the item-consolidation maintenance command keep a
-- pointer to the surviving row. Restore of an absorbed row is rejected, and the
-- pointer never changes after the consolidation transaction commits.
ALTER TABLE GuildBankItems ADD COLUMN ConsolidatedInto BIGINT UNSIGNED NULL AFTER DeletedFg;