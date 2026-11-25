package mariadb

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
)

// expectedMigrationHashes pins the exact embedded migration bytes (PD5-22) after
// normalizing CRLF to LF. Git stores these files with LF (`text=auto`) and Linux
// builds embed LF, while a Windows working copy checks them out with CRLF; the
// normalization makes the review gate platform-independent. Changing a migration
// still requires deliberately updating this manifest, which is the real review
// gate because migrationFiles is the only source the runner executes.
var expectedMigrationHashes = map[string]string{
	"migrations/0001_guild_store.sql":               "4b2ce81832427e8c3809df943c107adba11a9d432d0b4f2d5d3e6f21010ba1cf",
	"migrations/0002_public_inventory_indexes.sql":  "6fa33acbd96ee1b8649a958cc7d0cd62ec4b12c0a626b8c52171e4068c17e93c",
	"migrations/0003_admin_inventory.sql":           "71c3674b83eb81c97353fc00ceae0fdabb3feb4707a25565f855e74c727cbcb0",
	"migrations/0004_import_ignored_rows.sql":       "fcbe65869bbf9074bfad89c02112e5f3d497bfcf8ad4dac09db2f9b68a53d580",
	"migrations/0005_item_quantity_model.sql":       "c7de158df2f218a9222cc3be6eae1d7ec23e2e9cad44ef38a197e1384dbdce99",
	"migrations/0006_inventory_movement.sql":        "f8e54995c21ad24b5cc04e0f63ddaea95c2a04eb4624445c89b9f530bc30807e",
	"migrations/0007_movement_related_movement.sql": "1b3d78fbfa4583a7cf155c838d874ab83f0afb4f42fce60eda5064d75dfb0c40",
}

func TestMigrationManifest(t *testing.T) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(names) != len(expectedMigrationHashes) {
		t.Fatalf("embedded migration count = %d, want %d: %v", len(names), len(expectedMigrationHashes), names)
	}
	for _, name := range names {
		expected, ok := expectedMigrationHashes[name]
		if !ok {
			t.Fatalf("unexpected embedded migration %q", name)
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sum := sha256.Sum256([]byte(strings.ReplaceAll(string(body), "\r\n", "\n")))
		if actual := hex.EncodeToString(sum[:]); actual != expected {
			t.Fatalf("%s hash = %s, want %s", name, actual, expected)
		}
		base := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		if len(base) < 5 || base[4] != '_' || len(base) != len(strings.TrimSpace(base)) {
			t.Fatalf("migration %q does not match NNNN_name.sql", name)
		}
		// The version IS the filename base, so it must fit schema_migrations.Version
		// (VARCHAR(32)); an over-long name applies its DDL and then fails to record
		// itself, leaving unrecoverable partial schema (incident 2026-11-25).
		if len(base) > schemaMigrationsVersionMaxLen {
			t.Fatalf("migration %q is %d characters but schema_migrations.Version holds %d",
				name, len(base), schemaMigrationsVersionMaxLen)
		}
		for _, r := range base[:4] {
			if r < '0' || r > '9' {
				t.Fatalf("migration %q does not start with a four-digit version", name)
			}
		}
	}

	body, err := migrationFiles.ReadFile("migrations/0003_admin_inventory.sql")
	if err != nil {
		t.Fatalf("read 0003: %v", err)
	}
	text := string(body)
	if got := strings.Count(text, "ALTER TABLE"); got != 2 {
		t.Fatalf("0003 ALTER TABLE count = %d, want 2", got)
	}
	for _, forbidden := range []string{"DELIMITER", "TRIGGER", "PROCEDURE", "CREATE FUNCTION"} {
		if strings.Contains(strings.ToUpper(text), forbidden) {
			t.Fatalf("0003 must not contain %s", forbidden)
		}
	}
	for _, required := range []string{"LocationGroup", "LocationSort", "ix_gbi_admin_location", "ix_audit_correlation"} {
		if !strings.Contains(text, required) {
			t.Fatalf("0003 is missing %s", required)
		}
	}

	body, err = migrationFiles.ReadFile("migrations/0004_import_ignored_rows.sql")
	if err != nil {
		t.Fatalf("read 0004: %v", err)
	}
	text = string(body)
	if got := strings.Count(text, "ALTER TABLE"); got != 1 {
		t.Fatalf("0004 ALTER TABLE count = %d, want 1", got)
	}
	if !strings.Contains(text, "IgnoredRowCount") || !strings.Contains(text, "ImportBatch") {
		t.Fatal("0004 does not add ImportBatch.IgnoredRowCount")
	}
	for _, forbidden := range []string{"DELIMITER", "TRIGGER", "PROCEDURE", "CREATE FUNCTION"} {
		if strings.Contains(strings.ToUpper(text), forbidden) {
			t.Fatalf("0004 must not contain %s", forbidden)
		}
	}

	body, err = migrationFiles.ReadFile("migrations/0005_item_quantity_model.sql")
	if err != nil {
		t.Fatalf("read 0005: %v", err)
	}
	text = string(body)
	if got := strings.Count(text, "ALTER TABLE"); got != 2 {
		t.Fatalf("0005 ALTER TABLE count = %d, want 2", got)
	}
	for _, required := range []string{"DROP CONSTRAINT IF EXISTS ck_gbi_item_one", "ConsolidatedInto"} {
		if !strings.Contains(text, required) {
			t.Fatalf("0005 is missing %s", required)
		}
	}
	if strings.Contains(text, "DROP CONSTRAINT ck_gbi_quantity") {
		t.Fatal("0005 must keep ck_gbi_quantity")
	}
	for _, forbidden := range []string{"DELIMITER", "TRIGGER", "PROCEDURE", "CREATE FUNCTION"} {
		if strings.Contains(strings.ToUpper(text), forbidden) {
			t.Fatalf("0005 must not contain %s", forbidden)
		}
	}

	body, err = migrationFiles.ReadFile("migrations/0006_inventory_movement.sql")
	if err != nil {
		t.Fatalf("read 0006: %v", err)
	}
	text = string(body)
	if got := strings.Count(text, "CREATE TABLE"); got != 1 {
		t.Fatalf("0006 CREATE TABLE count = %d, want 1", got)
	}
	for _, required := range []string{
		"InventoryMovement",
		"uq_movement_idempotency",
		"ix_movement_lot_time",
		"ix_movement_equid_time",
		"ix_movement_type_time",
		"ix_movement_recipient_time",
		"ix_movement_correlation",
		"ck_movement_quantity",
		"ck_movement_remaining",
		"ck_movement_issue_recipient",
		"fk_movement_item",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("0006 is missing %s", required)
		}
	}
	for _, forbidden := range []string{"DELIMITER", "TRIGGER", "PROCEDURE", "CREATE FUNCTION"} {
		if strings.Contains(strings.ToUpper(text), forbidden) {
			t.Fatalf("0006 must not contain %s", forbidden)
		}
	}

	body, err = migrationFiles.ReadFile("migrations/0007_movement_related_movement.sql")
	if err != nil {
		t.Fatalf("read 0007: %v", err)
	}
	text = string(body)
	if got := strings.Count(text, "ALTER TABLE"); got != 3 {
		t.Fatalf("0007 ALTER TABLE count = %d, want 3", got)
	}
	for _, required := range []string{
		"RelatedMovementID",
		"ix_movement_related_movement",
		"fk_movement_related_movement",
		"REFERENCES InventoryMovement (MovementID)",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("0007 is missing %s", required)
		}
	}
	for _, forbidden := range []string{"DELIMITER", "TRIGGER", "PROCEDURE", "CREATE FUNCTION", "DROP"} {
		if strings.Contains(strings.ToUpper(text), forbidden) {
			t.Fatalf("0007 must not contain %s", forbidden)
		}
	}
}
