package mariadb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"

	"github.com/go-sql-driver/mysql"

	"github.com/Panazule/guildbank/internal/domain"
)

// mapStoreError translates driver failures into the small domain vocabulary
// consumed by the service and API. It never exposes SQL text to callers.
func mapStoreError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1213, 1205:
			return &domain.RetryableError{Err: fmt.Errorf("%s: transient database conflict", operation)}
		case 1062:
			return fmt.Errorf("%w: %s", domain.ErrConflict, operation)
		case 1044, 1045, 1049, 2002, 2003, 2006, 2013:
			return fmt.Errorf("%w: %s", domain.ErrUnavailable, operation)
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %s", domain.ErrUnavailable, operation)
	}
	return fmt.Errorf("mariadb: %s: %w", operation, err)
}
