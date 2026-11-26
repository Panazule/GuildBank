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

// execer is satisfied by *sql.DB and *sql.Tx, so a write can run inside a
// transaction or on its own.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertUser(ctx context.Context, e execer, u domain.User) (int64, error) {
	res, err := e.ExecContext(ctx, `
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

func insertUserTx(ctx context.Context, tx *sql.Tx, u domain.User) (int64, error) {
	return insertUser(ctx, tx, u)
}

// CreateUser inserts one account and returns the stored row. The caller supplies
// the bcrypt hash, so password policy is enforced by the service. Empty
// timestamps default to the current time: a zero value would otherwise be
// written as an empty string and become unreadable. Authorization is the
// service's responsibility because creation protects no shared invariant.
func (s *Store) CreateUser(ctx context.Context, u domain.User) (domain.User, error) {
	u.UserName = domain.NormalizeUserName(u.UserName)
	if err := domain.ValidateUserName(u.UserName); err != nil {
		return domain.User{}, err
	}
	role := domain.Role(u.AccessLevel)
	if !role.Valid() {
		return domain.User{}, domain.NewValidationError("accessLevel", "must be Member, Officer or Admin")
	}
	if strings.TrimSpace(u.PasswordHash) == "" {
		return domain.User{}, domain.NewValidationError("password", "must not be blank")
	}
	if u.ModAt.IsZero() {
		u.ModAt = time.Now().UTC()
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = u.ModAt
	}
	u.AccessLevel = string(role)
	u.LastLogin = nil

	id, err := insertUser(ctx, s.db, u)
	if err != nil {
		return domain.User{}, err
	}
	return s.GetUserByID(ctx, id)
}

// ListUsers returns every account, ordered case-insensitively by username. The
// password hash is included because this is a store-level row, but no service or
// API response may expose it.
func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM Users ORDER BY UserName COLLATE NOCASE, UserID`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	users := make([]domain.User, 0, 8)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list users: %w", err)
	}
	return users, nil
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

// ResetUserPassword stores an administrator-supplied temporary password,
// requires a change at the next sign-in and revokes every session for the
// account, all in one transaction. The actor must be an Administrator; the route
// layer enforces the same rule.
func (s *Store) ResetUserPassword(ctx context.Context, userID int64, passwordHash string,
	at time.Time, actor domain.Actor) (domain.User, error) {
	if !actor.IsAdmin {
		return domain.User{}, domain.ErrForbidden
	}
	if strings.TrimSpace(passwordHash) == "" {
		return domain.User{}, domain.NewValidationError("password", "must not be blank")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("sqlite: begin password reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := getUserByID(ctx, tx, userID); err != nil {
		return domain.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE Users SET PasswordHash = ?, MustChangePassword = 1, ModAt = ?, ModBy = ?
		WHERE UserID = ?`,
		passwordHash, formatTime(at), nullString(actor.UserName), userID); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: reset password: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM Sessions WHERE UserID = ?`, userID); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: revoke sessions: %w", err)
	}
	updated, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: commit password reset: %w", err)
	}
	return updated, nil
}

// SetOwnPassword replaces an account's password after the service has verified
// the current one, clears MustChangePassword and revokes every session except
// keepSessionID. A blank keepSessionID revokes all of them. No actor is required
// because the caller is the account owner.
func (s *Store) SetOwnPassword(ctx context.Context, userID int64, passwordHash string,
	at time.Time, keepSessionID string) (domain.User, error) {
	if strings.TrimSpace(passwordHash) == "" {
		return domain.User{}, domain.NewValidationError("password", "must not be blank")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("sqlite: begin password change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE Users SET PasswordHash = ?, MustChangePassword = 0, ModAt = ?, ModBy = ?
		WHERE UserID = ?`,
		passwordHash, formatTime(at), nullString(current.UserName), userID); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: change own password: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM Sessions WHERE UserID = ? AND SessionID <> ?`, userID, keepSessionID); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: revoke other sessions: %w", err)
	}
	updated, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: commit password change: %w", err)
	}
	return updated, nil
}

// DeleteUser permanently removes an account together with its sessions. Removing
// the last enabled Administrator is refused: the role would otherwise disappear
// from the deployment. The actor must be an Administrator.
func (s *Store) DeleteUser(ctx context.Context, userID int64, actor domain.Actor) error {
	if !actor.IsAdmin {
		return domain.ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin user delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getUserByID(ctx, tx, userID)
	if err != nil {
		return err
	}
	if current.Role() == domain.RoleAdmin && !current.Disabled {
		remaining, err := countEnabledAdmins(ctx, tx, userID)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return fmt.Errorf("%w: at least one enabled administrator is required", domain.ErrConflict)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM Sessions WHERE UserID = ?`, userID); err != nil {
		return fmt.Errorf("sqlite: revoke sessions: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM Users WHERE UserID = ?`, userID)
	if err != nil {
		return fmt.Errorf("sqlite: delete user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit user delete: %w", err)
	}
	return nil
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
