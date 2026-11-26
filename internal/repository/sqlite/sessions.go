package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const sessionSelect = `
	SELECT s.SessionID, s.TokenHash, s.UserID, s.CreatedAt, s.LastAccessed,
	       s.ExpiresAt, s.AbsoluteExpiresAt, s.RevokedAt, s.ClientIP, s.UserAgent,
	       u.UserID, u.UserName, u.PasswordHash, u.AccessLevel, u.LastLogin,
	       u.DisabledFg, u.ModAt, u.CreatedAt, u.ModBy, u.MustChangePassword,
	       u.AccountStatus, u.Origin, u.GuildVerifiedAt, u.HasPassword
	FROM Sessions s
	JOIN Users u ON u.UserID = s.UserID`

// CreateSession persists a new session. Only TokenHash is stored; the raw
// browser token never reaches SQLite (D-09).
func (s *Store) CreateSession(ctx context.Context, session domain.Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO Sessions (SessionID, TokenHash, UserID, CreatedAt, LastAccessed,
		                      ExpiresAt, AbsoluteExpiresAt, RevokedAt, ClientIP, UserAgent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.TokenHash, session.UserID,
		formatTime(session.CreatedAt), formatTime(session.LastAccessed),
		formatTime(session.ExpiresAt), formatTime(session.AbsoluteExpiresAt),
		nullableString(session.RevokedAt), nullString(session.ClientIP), nullString(session.UserAgent))
	if err != nil {
		return fmt.Errorf("sqlite: insert session: %w", err)
	}
	return nil
}

// FindSessionByTokenHash loads a live session and its account. Revocation and
// expiry are evaluated by the service against an injected clock.
func (s *Store) FindSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	row := s.db.QueryRowContext(ctx, sessionSelect+` WHERE s.TokenHash = ? AND s.RevokedAt IS NULL`, tokenHash)
	session, err := scanSession(row)
	if err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

func scanSession(scanner interface{ Scan(...any) error }) (domain.Session, error) {
	var (
		session   domain.Session
		createdAt string
		accessed  string
		expires   string
		absolute  string
		revoked   sql.NullString
		clientIP  sql.NullString
		userAgent sql.NullString
		lastLogin sql.NullString
		disabled  int
		modAt     string
		userMade  string
		modBy     sql.NullString
		must      int
		status    string
		origin    sql.NullString
		verified  sql.NullString
		hasPw     int
	)
	err := scanner.Scan(
		&session.ID, &session.TokenHash, &session.UserID, &createdAt, &accessed,
		&expires, &absolute, &revoked, &clientIP, &userAgent,
		&session.User.ID, &session.User.UserName, &session.User.PasswordHash,
		&session.User.AccessLevel, &lastLogin, &disabled, &modAt, &userMade,
		&modBy, &must, &status, &origin, &verified, &hasPw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Session{}, domain.ErrNotFound
		}
		return domain.Session{}, fmt.Errorf("sqlite: scan session: %w", err)
	}
	if session.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Session{}, err
	}
	if session.LastAccessed, err = parseTime(accessed); err != nil {
		return domain.Session{}, err
	}
	if session.ExpiresAt, err = parseTime(expires); err != nil {
		return domain.Session{}, err
	}
	if session.AbsoluteExpiresAt, err = parseTime(absolute); err != nil {
		return domain.Session{}, err
	}
	if session.RevokedAt, err = nullableTime(revoked); err != nil {
		return domain.Session{}, err
	}
	if clientIP.Valid {
		session.ClientIP = clientIP.String
	}
	if userAgent.Valid {
		session.UserAgent = userAgent.String
	}
	if session.User.LastLogin, err = nullableTime(lastLogin); err != nil {
		return domain.Session{}, err
	}
	if session.User.ModAt, err = parseTime(modAt); err != nil {
		return domain.Session{}, err
	}
	if session.User.CreatedAt, err = parseTime(userMade); err != nil {
		return domain.Session{}, err
	}
	session.User.Disabled = disabled == 1
	session.User.MustChangePassword = must == 1
	session.User.HasPassword = hasPw == 1
	session.User.AccountStatus = status
	if modBy.Valid {
		session.User.ModBy = modBy.String
	}
	if origin.Valid {
		session.User.Origin = origin.String
	}
	if session.User.GuildVerifiedAt, err = nullableTime(verified); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

// TouchSession refreshes sliding expiry and last access. The service throttles
// writes to at most once per minute (DatabaseDesign.md §3.3).
func (s *Store) TouchSession(ctx context.Context, id string, at, expires time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE Sessions SET LastAccessed = ?, ExpiresAt = ? WHERE SessionID = ? AND RevokedAt IS NULL`,
		formatTime(at), formatTime(expires), id)
	if err != nil {
		return fmt.Errorf("sqlite: touch session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// DeleteSession removes one session by raw-token hash.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM Sessions WHERE TokenHash = ?`, tokenHash)
	if err != nil {
		return fmt.Errorf("sqlite: delete session: %w", err)
	}
	return nil
}

// DeleteSessionsForUser revokes every session for an account.
func (s *Store) DeleteSessionsForUser(ctx context.Context, userID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM Sessions WHERE UserID = ?`, userID)
	if err != nil {
		return 0, fmt.Errorf("sqlite: delete user sessions: %w", err)
	}
	return res.RowsAffected()
}

// DeleteExpiredSessions purges idle- or absolutely-expired rows.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM Sessions WHERE ExpiresAt <= ? OR AbsoluteExpiresAt <= ?`,
		formatTime(now), formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("sqlite: purge sessions: %w", err)
	}
	return res.RowsAffected()
}
