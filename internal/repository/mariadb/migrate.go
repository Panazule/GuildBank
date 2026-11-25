package mariadb

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const migrateLockName = "guildbank:migrate:"

// schemaMigrationsVersionMaxLen mirrors the width of the schema_migrations.Version
// column. A version is the migration filename without its extension, so a longer
// filename would execute its statements and then fail to record itself (error 1406),
// leaving partially applied schema behind that every later run trips over (error
// 1060). Reject an over-long version before executing any statement.
const schemaMigrationsVersionMaxLen = 32

// Migrate applies the embedded guild-store migrations once under a
// database-specific advisory lock and returns the latest applied version.
func (s *Store) Migrate(ctx context.Context) (string, error) {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		Version VARCHAR(32) PRIMARY KEY,
		AppliedAt DATETIME(6) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return "", fmt.Errorf("mariadb: create schema_migrations: %w", err)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("mariadb: migration connection: %w", err)
	}
	defer conn.Close()

	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT GET_LOCK(CONCAT('`+migrateLockName+`', LEFT(SHA2(DATABASE(), 256), 32)), 30)`).
		Scan(&acquired); err != nil {
		return "", fmt.Errorf("mariadb: acquire migration lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return "", fmt.Errorf("mariadb: migration lock timeout: %w", domain.ErrUnavailable)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(),
			`SELECT RELEASE_LOCK(CONCAT('`+migrateLockName+`', LEFT(SHA2(DATABASE(), 256), 32)))`)
	}()

	applied, err := readAppliedVersions(ctx, conn)
	if err != nil {
		return "", err
	}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return "", fmt.Errorf("mariadb: list migrations: %w", err)
	}
	sort.Strings(names)
	latest := ""
	maxApplied := ""
	for version := range applied {
		if version > maxApplied {
			maxApplied = version
		}
	}
	for _, path := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(path, "migrations/"), ".sql")
		if len(version) > schemaMigrationsVersionMaxLen {
			return latest, fmt.Errorf(
				"mariadb: migration %s: version is %d characters but schema_migrations.Version holds %d",
				version, len(version), schemaMigrationsVersionMaxLen)
		}
		if _, ok := applied[version]; ok {
			if version > latest {
				latest = version
			}
			continue
		}
		if maxApplied != "" && version < maxApplied {
			return latest, fmt.Errorf("mariadb: embedded migration %s is older than applied version %s", version, maxApplied)
		}
		body, err := migrationFiles.ReadFile(path)
		if err != nil {
			return latest, fmt.Errorf("mariadb: read migration %s: %w", version, err)
		}
		for _, statement := range splitSQLStatements(string(body)) {
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return latest, fmt.Errorf("mariadb: migration %s: %w", version, err)
			}
		}
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO schema_migrations (Version, AppliedAt) VALUES (?, ?)`, version, time.Now().UTC(),
		); err != nil {
			return latest, fmt.Errorf("mariadb: record migration %s: %w", version, err)
		}
		applied[version] = true
		latest = version
	}
	return latest, nil
}

func readAppliedVersions(ctx context.Context, conn *sql.Conn) (map[string]bool, error) {
	rows, err := conn.QueryContext(ctx, `SELECT Version FROM schema_migrations ORDER BY Version`)
	if err != nil {
		return nil, fmt.Errorf("mariadb: read schema_migrations: %w", err)
	}
	defer rows.Close()
	versions := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("mariadb: scan schema version: %w", err)
		}
		versions[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mariadb: read schema versions: %w", err)
	}
	return versions, nil
}

func splitSQLStatements(body string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	parts := strings.Split(cleaned.String(), ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if statement := strings.TrimSpace(part); statement != "" {
			out = append(out, statement)
		}
	}
	return out
}
