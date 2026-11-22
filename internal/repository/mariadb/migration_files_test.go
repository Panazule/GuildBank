package mariadb

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"
)

// expectedMigrationHashes pins the exact embedded migration bytes (PD5-22).
// Changing a migration requires deliberately updating this manifest, which is
// the real review gate because migrationFiles is the only source the runner
// executes.
var expectedMigrationHashes = map[string]string{
	"migrations/0001_guild_store.sql":              "40f28cd5743b2fc918976d8625debe115ce4e0f39e95ea250bd39bdde076dea6",
	"migrations/0002_public_inventory_indexes.sql": "acb095f99aa20fcea87c5bccbd9f6fd675843fbf46895e37eb9bbc6809e730fa",
	"migrations/0003_admin_inventory.sql":          "c27f52cd9db7e9b0b79fa52cde9afb43dc1e93d940372602eab6061c67517911",
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
		sum := sha256.Sum256(body)
		if actual := hex.EncodeToString(sum[:]); actual != expected {
			t.Fatalf("%s hash = %s, want %s", name, actual, expected)
		}
		base := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		if len(base) < 5 || base[4] != '_' || len(base) != len(strings.TrimSpace(base)) {
			t.Fatalf("migration %q does not match NNNN_name.sql", name)
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
}
