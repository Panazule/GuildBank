package sqlite

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// migrationFiles is embedded so a static binary carries its schema.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate applies forward-only migrations and returns the resulting
// PRAGMA user_version. Each migration and its version bump commit atomically;
// a file newer than the binary is refused rather than silently downgraded.
func (s *Store) Migrate(ctx context.Context) (int, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return 0, fmt.Errorf("sqlite: list migrations: %w", err)
	}
	sort.Strings(names)

	var current int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&current); err != nil {
		return 0, fmt.Errorf("sqlite: read user_version: %w", err)
	}
	if current > len(names) {
		return current, fmt.Errorf("sqlite: database schema version %d is newer than binary version %d", current, len(names))
	}
	for i := current; i < len(names); i++ {
		target := i + 1
		body, err := migrationFiles.ReadFile(names[i])
		if err != nil {
			return current, fmt.Errorf("sqlite: read migration %s: %w", names[i], err)
		}
		if err := s.applyMigration(ctx, target, string(body)); err != nil {
			return current, err
		}
		current = target
	}
	return current, nil
}

func (s *Store) applyMigration(ctx context.Context, version int, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin migration %d: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, statement := range splitStatements(body) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("sqlite: migration %d: %w", version, err)
		}
	}
	// PRAGMA cannot be parameterized; version is derived from a local integer.
	if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+strconv.Itoa(version)); err != nil {
		return fmt.Errorf("sqlite: set schema version %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit migration %d: %w", version, err)
	}
	return nil
}

// splitStatements keeps the migration files readable while avoiding reliance
// on driver-specific multi-statement handling. SQL line comments are removed
// first because a semicolon inside prose must not start a new statement. Phase
// 1 migrations contain no triggers or semicolons inside string literals.
func splitStatements(body string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	var out []string
	for _, part := range strings.Split(cleaned.String(), ";") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") || strings.Contains(msg, "constraint failed: unique")
}
