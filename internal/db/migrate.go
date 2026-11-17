package db

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// NewMigrator returns a goose provider over the embedded migrations of one
// database (see Databases), guarded by a PostgreSQL advisory lock so
// concurrent runners cannot interleave.
func NewMigrator(sqlDB *sql.DB, name string) (*goose.Provider, error) {
	fsys, err := Migrations(name)
	if err != nil {
		return nil, err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("goose locker: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("goose provider %s: %w", name, err)
	}
	return p, nil
}
