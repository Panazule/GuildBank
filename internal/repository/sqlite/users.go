package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// timestampLayout is RFC3339Nano in UTC. SQLite stores these values as TEXT;
// the fixed-width fraction keeps lexical ordering equal to chronological
// ordering for the expiry comparisons.
const timestampLayout = time.RFC3339Nano

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timestampLayout)
}

func parseTime(value string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("sqlite: parse timestamp %q: %w", value, err)
	}
	return t.UTC(), nil
}

func nullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid || value.String == "" {
		return nil, nil
	}
	t, err := parseTime(value.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func scanUser(scanner interface{ Scan(...any) error }) (domain.User, error) {
	var (
		u         domain.User
		lastLogin sql.NullString
		disabled  int
		modBy     sql.NullString
		must      int
		modAt     string
		createdAt string
	)
	if err := scanner.Scan(
		&u.ID, &u.UserName, &u.PasswordHash, &u.AccessLevel, &lastLogin,
		&disabled, &modAt, &createdAt, &modBy, &must,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, domain.ErrNotFound
		}
		return domain.User{}, fmt.Errorf("sqlite: scan user: %w", err)
	}
	var err error
	if u.LastLogin, err = nullableTime(lastLogin); err != nil {
		return domain.User{}, err
	}
	if u.ModAt, err = parseTime(modAt); err != nil {
		return domain.User{}, err
	}
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.User{}, err
	}
	u.Disabled = disabled == 1
	u.MustChangePassword = must == 1
	if modBy.Valid {
		u.ModBy = modBy.String
	}
	return u, nil
}

const userColumns = `UserID, UserName, PasswordHash, AccessLevel, LastLogin, DisabledFg, ModAt, CreatedAt, ModBy, MustChangePassword`

// GetUserByName returns the account with the supplied case-insensitive name.
func (s *Store) GetUserByName(ctx context.Context, name string) (domain.User, error) {
	if strings.TrimSpace(name) == "" {
		return domain.User{}, domain.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM Users WHERE UserName = ?`, name)
	return scanUser(row)
}

// GetUserByID returns the account with the supplied identifier.
func (s *Store) GetUserByID(ctx context.Context, id int64) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM Users WHERE UserID = ?`, id)
	return scanUser(row)
}

// UpdateLastLogin records a successful authentication.
func (s *Store) UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE Users SET LastLogin = ? WHERE UserID = ?`, formatTime(at), userID)
	if err != nil {
		return fmt.Errorf("sqlite: update last login: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// CountAdministrators counts every Admin row, including disabled accounts, so
// setup cannot reopen (D-02, B-6).
func (s *Store) CountAdministrators(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Users WHERE AccessLevel = ?`, domain.AccessAdmin).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: count administrators: %w", err)
	}
	return n, nil
}

func insertUserTx(ctx context.Context, tx *sql.Tx, u domain.User) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO Users (UserName, PasswordHash, AccessLevel, LastLogin, DisabledFg, ModAt, CreatedAt, ModBy, MustChangePassword)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.UserName, u.PasswordHash, u.AccessLevel, nullableString(u.LastLogin),
		boolInt(u.Disabled), formatTime(u.ModAt), formatTime(u.CreatedAt), nullString(u.ModBy),
		boolInt(u.MustChangePassword))
	if err != nil {
		if isUniqueViolation(err) {
			return 0, domain.ErrAlreadyExists
		}
		return 0, fmt.Errorf("sqlite: insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("sqlite: user id: %w", err)
	}
	return id, nil
}

// rowQuerier is satisfied by *sql.DB and *sql.Tx, so the last-administrator
// guards can run inside a write transaction or as a standalone read.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// CountEnabledAdmins counts enabled Administrator accounts, optionally
// excluding one account. This is the last-administrator guard: at least one
// enabled Administrator must always remain.
func (s *Store) CountEnabledAdmins(ctx context.Context, excludeUserID int64) (int, error) {
	return countEnabledAdmins(ctx, s.db, excludeUserID)
}

func countEnabledAdmins(ctx context.Context, q rowQuerier, excludeUserID int64) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM Users
		WHERE AccessLevel = ? AND DisabledFg = 0 AND UserID <> ?`,
		domain.AccessAdmin, excludeUserID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("sqlite: count enabled administrators: %w", err)
	}
	return n, nil
}

func getUserByID(ctx context.Context, q rowQuerier, userID int64) (domain.User, error) {
	row := q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM Users WHERE UserID = ?`, userID)
	return scanUser(row)
}

// SetUserAccessLevel changes one account's role. It runs in a single
// transaction and refuses to remove the last enabled Administrator. The actor
// must be an Administrator; the route layer already enforces the same rule.
func (s *Store) SetUserAccessLevel(ctx context.Context, userID int64, role domain.Role,
	at time.Time, actor domain.Actor) (domain.User, error) {
	if !role.Valid() {
		return domain.User{}, domain.NewValidationError("role", "role must be Admin, Officer or Member")
	}
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("sqlite: begin role change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if current.Role() == domain.RoleAdmin && !current.Disabled && role != domain.RoleAdmin {
		remaining, err := countEnabledAdmins(ctx, tx, userID)
		if err != nil {
			return domain.User{}, err
		}
		if remaining == 0 {
			return domain.User{}, fmt.Errorf("%w: at least one enabled administrator is required", domain.ErrConflict)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE Users SET AccessLevel = ?, ModAt = ?, ModBy = ? WHERE UserID = ?`,
		string(role), formatTime(at), nullString(actor.UserName), userID); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: update access level: %w", err)
	}
	updated, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: commit role change: %w", err)
	}
	return updated, nil
}

// SetUserDisabled enables or disables one account. Disabling revokes that
// account's sessions in the same transaction and is refused when it would leave
// no enabled Administrator. The actor must be an Administrator.
func (s *Store) SetUserDisabled(ctx context.Context, userID int64, disabled bool,
	at time.Time, actor domain.Actor) (domain.User, error) {
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("sqlite: begin status change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if disabled && !current.Disabled && current.Role() == domain.RoleAdmin {
		remaining, err := countEnabledAdmins(ctx, tx, userID)
		if err != nil {
			return domain.User{}, err
		}
		if remaining == 0 {
			return domain.User{}, fmt.Errorf("%w: at least one enabled administrator is required", domain.ErrConflict)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE Users SET DisabledFg = ?, ModAt = ?, ModBy = ? WHERE UserID = ?`,
		boolInt(disabled), formatTime(at), nullString(actor.UserName), userID); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: update disabled flag: %w", err)
	}
	if disabled {
		if _, err := tx.ExecContext(ctx, `DELETE FROM Sessions WHERE UserID = ?`, userID); err != nil {
			return domain.User{}, fmt.Errorf("sqlite: revoke sessions: %w", err)
		}
	}
	updated, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: commit status change: %w", err)
	}
	return updated, nil
}

func nullableString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
