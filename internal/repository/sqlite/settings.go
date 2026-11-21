package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Panazule/guildbank/internal/domain"
)

const settingColumns = `SettingKey, SettingValue, SettingType, IsSecret, ModAt, ModBy`

// ListSettings returns all configuration rows ordered by key.
func (s *Store) ListSettings(ctx context.Context) ([]domain.Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+settingColumns+` FROM Settings ORDER BY SettingKey`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list settings: %w", err)
	}
	defer rows.Close()

	var out []domain.Setting
	for rows.Next() {
		var (
			setting domain.Setting
			kind    string
			secret  int
			modAt   string
			modBy   sql.NullString
		)
		if err := rows.Scan(&setting.Key, &setting.Value, &kind, &secret, &modAt, &modBy); err != nil {
			return nil, fmt.Errorf("sqlite: scan setting: %w", err)
		}
		setting.Type = domain.SettingType(kind)
		setting.IsSecret = secret == 1
		var err error
		if setting.ModAt, err = parseTime(modAt); err != nil {
			return nil, err
		}
		if modBy.Valid {
			setting.ModBy = modBy.String
		}
		out = append(out, setting)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: iterate settings: %w", err)
	}
	return out, nil
}

// UpsertSettings writes every supplied row in one transaction.
func (s *Store) UpsertSettings(ctx context.Context, inputs []domain.SettingInput, modBy string, at time.Time) error {
	if len(inputs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin settings update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, in := range inputs {
		if err := upsertSettingTx(ctx, tx, in, modBy, at); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit settings update: %w", err)
	}
	return nil
}

func upsertSettingTx(ctx context.Context, tx *sql.Tx, in domain.SettingInput, modBy string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO Settings (SettingKey, SettingValue, SettingType, IsSecret, ModAt, ModBy)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(SettingKey) DO UPDATE SET
			SettingValue = excluded.SettingValue,
			SettingType  = excluded.SettingType,
			IsSecret     = excluded.IsSecret,
			ModAt        = excluded.ModAt,
			ModBy        = excluded.ModBy`,
		in.Key, in.Value, string(in.Type), boolInt(in.Secret), formatTime(at), nullString(modBy))
	if err != nil {
		return fmt.Errorf("sqlite: upsert setting %s: %w", in.Key, err)
	}
	return nil
}
