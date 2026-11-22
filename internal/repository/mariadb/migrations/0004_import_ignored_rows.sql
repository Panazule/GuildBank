-- 0004_import_ignored_rows.sql
-- IMP-01: persist the count of nested-slot rows ignored at staging so the
-- operator can reconcile a batch without the original file. NULL on batches
-- staged before this migration; readers treat NULL as zero.

ALTER TABLE ImportBatch
    ADD COLUMN IgnoredRowCount INT UNSIGNED NULL AFTER AcceptedRowCount;