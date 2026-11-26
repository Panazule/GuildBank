package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

// DiscordIdentity returns the account's linked Discord identity. An account that
// has never linked returns a zero identity with no error.
func (s *Store) DiscordIdentity(ctx context.Context, userID int64) (domain.DiscordIdentity, error) {
	var (
		discordID   sql.NullString
		discordName sql.NullString
		linkedAt    sql.NullString
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT DiscordUserID, DiscordUserName, DiscordLinkedAt FROM Users WHERE UserID = ?`,
		userID).Scan(&discordID, &discordName, &linkedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DiscordIdentity{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.DiscordIdentity{}, fmt.Errorf("sqlite: read discord identity: %w", err)
	}
	identity := domain.DiscordIdentity{UserID: userID}
	if !discordID.Valid || discordID.String == "" {
		return identity, nil
	}
	identity.DiscordUserID = discordID.String
	if discordName.Valid {
		identity.DiscordUserName = discordName.String
	}
	if linkedAt.Valid && linkedAt.String != "" {
		at, err := parseTime(linkedAt.String)
		if err != nil {
			return domain.DiscordIdentity{}, err
		}
		identity.LinkedAt = &at
	}
	return identity, nil
}

// DiscordUserIDOwner returns the account that already owns a Discord identity.
// It reports ErrNotFound when the identity is unclaimed, which is the normal
// path for a first link.
func (s *Store) DiscordUserIDOwner(ctx context.Context, discordUserID string) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT UserID FROM Users WHERE DiscordUserID = ?`, discordUserID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("sqlite: discord identity owner: %w", err)
	}
	return userID, nil
}

// UserByDiscordID resolves the account linked to a Discord identity. Sign-in
// uses it; an unlinked Discord user yields ErrNotFound so the caller can answer
// neutrally without creating anything.
func (s *Store) UserByDiscordID(ctx context.Context, discordUserID string) (domain.User, error) {
	if discordUserID == "" {
		return domain.User{}, domain.ErrNotFound
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM Users WHERE DiscordUserID = ?`, discordUserID)
	return scanUser(row)
}

// SetDiscordIdentity links an account to a Discord identity. The partial unique
// index ux_users_discord is the authority: a second account claiming the same
// Discord user is reported as ErrAlreadyExists even if a concurrent link won the
// race.
func (s *Store) SetDiscordIdentity(ctx context.Context, userID int64, discordUserID, discordUserName string,
	at time.Time, modBy string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE Users SET DiscordUserID = ?, DiscordUserName = ?, DiscordLinkedAt = ?,
			ModAt = ?, ModBy = ?
		WHERE UserID = ?`,
		discordUserID, nullString(discordUserName), formatTime(at), formatTime(at), nullString(modBy), userID)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.ErrAlreadyExists
		}
		return fmt.Errorf("sqlite: link discord identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClearDiscordIdentity unlinks an account. The row is kept; only the three
// identity columns are nulled.
func (s *Store) ClearDiscordIdentity(ctx context.Context, userID int64, at time.Time, modBy string) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE Users SET DiscordUserID = NULL, DiscordUserName = NULL, DiscordLinkedAt = NULL,
			ModAt = ?, ModBy = ?
		WHERE UserID = ?`, formatTime(at), nullString(modBy), userID)
	if err != nil {
		return fmt.Errorf("sqlite: unlink discord identity: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
