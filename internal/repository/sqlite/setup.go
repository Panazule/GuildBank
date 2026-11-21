package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/Panazule/guildbank/internal/domain"
)

// CompleteSetup writes the first administrator and every wizard setting in one
// transaction. The administrator row is the setup switch (D-02); there is no
// state in which setup closes before configuration exists.
func (s *Store) CompleteSetup(ctx context.Context, data domain.SetupData) (domain.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.User{}, fmt.Errorf("sqlite: begin setup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var admins int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM Users WHERE AccessLevel = ?`, domain.AccessAdmin).Scan(&admins); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: count administrators: %w", err)
	}
	if admins > 0 {
		return domain.User{}, domain.ErrSetupClosed
	}

	id, err := insertUserTx(ctx, tx, data.Admin)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			return domain.User{}, domain.ErrConflict
		}
		return domain.User{}, err
	}
	for _, setting := range data.Settings {
		if err := upsertSettingTx(ctx, tx, setting, data.Admin.UserName, data.Admin.ModAt); err != nil {
			return domain.User{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.User{}, fmt.Errorf("sqlite: commit setup: %w", err)
	}

	user := data.Admin
	user.ID = id
	return user, nil
}
