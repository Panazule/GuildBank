package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const characterColumns = `CharacterID, UserID, CharacterName, CreatedAt, CreatedBy`

func scanCharacter(scanner interface{ Scan(...any) error }) (domain.Character, error) {
	var (
		character domain.Character
		createdAt string
		createdBy sql.NullString
	)
	if err := scanner.Scan(&character.ID, &character.UserID, &character.Name, &createdAt, &createdBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Character{}, domain.ErrNotFound
		}
		return domain.Character{}, fmt.Errorf("sqlite: scan character: %w", err)
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return domain.Character{}, err
	}
	character.CreatedAt = parsed
	if createdBy.Valid {
		character.CreatedBy = createdBy.String
	}
	return character, nil
}

// ListCharacters returns one account's names in case-insensitive order. The
// user ID is a required argument, never an after-the-fact filter.
func (s *Store) ListCharacters(ctx context.Context, userID int64) ([]domain.Character, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+characterColumns+` FROM UserCharacters WHERE UserID = ?
		 ORDER BY CharacterName COLLATE NOCASE, CharacterID`, userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list characters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	characters := make([]domain.Character, 0, 4)
	for rows.Next() {
		character, err := scanCharacter(rows)
		if err != nil {
			return nil, err
		}
		characters = append(characters, character)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list characters: %w", err)
	}
	return characters, nil
}

// CountCharacters returns how many names an account holds. The cap is enforced
// inside AddCharacter's transaction; this is for presentation.
func (s *Store) CountCharacters(ctx context.Context, userID int64) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM UserCharacters WHERE UserID = ?`, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("sqlite: count characters: %w", err)
	}
	return count, nil
}

// AddCharacter inserts one name for an account. The per-account cap is checked
// inside the same transaction as the insert, so two concurrent adds cannot both
// pass a stale count. A name owned by any account is refused.
func (s *Store) AddCharacter(ctx context.Context, userID int64, name string,
	at time.Time, createdBy string) (domain.Character, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: begin character add: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM UserCharacters WHERE UserID = ?`, userID).Scan(&count); err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: count characters: %w", err)
	}
	if count >= domain.CharacterMaxPerUser {
		return domain.Character{}, fmt.Errorf("%w: an account may have at most %d characters",
			domain.ErrConflict, domain.CharacterMaxPerUser)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO UserCharacters (UserID, CharacterName, CreatedAt, CreatedBy)
		VALUES (?, ?, ?, ?)`,
		userID, name, formatTime(at), nullString(createdBy))
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Character{}, domain.ErrAlreadyExists
		}
		return domain.Character{}, fmt.Errorf("sqlite: insert character: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: character id: %w", err)
	}
	row := tx.QueryRowContext(ctx, `SELECT `+characterColumns+` FROM UserCharacters WHERE CharacterID = ?`, id)
	character, err := scanCharacter(row)
	if err != nil {
		return domain.Character{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: commit character add: %w", err)
	}
	return character, nil
}

// DeleteCharacter removes one name that belongs to the account and returns the
// removed row so the caller can audit it. A name owned by a different account is
// reported as not found, so existence is never confirmed.
func (s *Store) DeleteCharacter(ctx context.Context, characterID, userID int64) (domain.Character, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: begin character delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `SELECT `+characterColumns+`
		FROM UserCharacters WHERE CharacterID = ? AND UserID = ?`, characterID, userID)
	character, err := scanCharacter(row)
	if err != nil {
		return domain.Character{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM UserCharacters WHERE CharacterID = ? AND UserID = ?`, characterID, userID); err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: delete character: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Character{}, fmt.Errorf("sqlite: commit character delete: %w", err)
	}
	return character, nil
}
