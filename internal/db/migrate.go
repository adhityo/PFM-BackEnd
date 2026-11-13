package db

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// NewMigrator returns a goose provider over the embedded migrations, guarded
// by a PostgreSQL advisory lock so concurrent runners cannot interleave.
func NewMigrator(sqlDB *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("goose locker: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, Migrations(), goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("goose provider: %w", err)
	}
	return p, nil
}
