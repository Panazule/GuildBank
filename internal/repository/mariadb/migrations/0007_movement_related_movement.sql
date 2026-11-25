-- 0007_movement_related_movement.sql
-- Admin issued-row projection: attribute a RETURN to the specific ISSUE movement
-- it closes. Additive only: one nullable column, one index and a self-referencing
-- foreign key. No existing row is changed, no quantity is rewritten and no
-- movement is fabricated. Existing RETURN rows keep RelatedMovementID = NULL and
-- are attributed by the one-time FIFO reconciliation described in the design.

ALTER TABLE InventoryMovement
    ADD COLUMN RelatedMovementID BIGINT UNSIGNED NULL AFTER RelatedItemID;

ALTER TABLE InventoryMovement
    ADD KEY ix_movement_related_movement (RelatedMovementID);

ALTER TABLE InventoryMovement
    ADD CONSTRAINT fk_movement_related_movement
        FOREIGN KEY (RelatedMovementID) REFERENCES InventoryMovement (MovementID)
        ON UPDATE RESTRICT ON DELETE RESTRICT;
