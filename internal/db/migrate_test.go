package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"

	"pfm-backend/internal/db"
	"pfm-backend/internal/testutil"
)

// TestMigrationsRoundTrip runs down to zero and up again (PRD §7.3) and checks
// schema invariants: uuid on every table, an index for every FK, no float or
// timezone-less timestamp columns.
func TestMigrationsRoundTrip(t *testing.T) {
	pool := testutil.NewDB(t) // already migrated up
	ctx := context.Background()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()
	m, err := db.NewMigrator(sqlDB)
	if err != nil {
		t.Fatal(err)
	}

	sources := m.ListSources()
	if len(sources) != 124 {
		t.Fatalf("expected 124 migrations, got %d", len(sources))
	}
	for i, s := range sources {
		if s.Version != int64(i+1) {
			t.Fatalf("migration numbering gap at %s", s.Path)
		}
	}

	if _, err := m.DownTo(ctx, 0); err != nil {
		t.Fatalf("down to 0: %v", err)
	}
	var tables int
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name <> 'goose_db_version'").Scan(&tables)
	if tables != 0 {
		t.Fatalf("%d tables left after down", tables)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("up again: %v", err)
	}

	checks := map[string]string{
		"tables without uuid": `SELECT count(*) FROM information_schema.tables t
			WHERE t.table_schema='public' AND t.table_name <> 'goose_db_version'
			AND NOT EXISTS (SELECT 1 FROM information_schema.columns c
			                WHERE c.table_schema='public' AND c.table_name=t.table_name AND c.column_name='uuid')`,
		"foreign keys without index": `SELECT count(*) FROM pg_constraint c WHERE c.contype='f'
			AND NOT EXISTS (SELECT 1 FROM pg_index i WHERE i.indrelid=c.conrelid AND i.indkey[0]=c.conkey[1])`,
		"float or timestamp without tz columns": `SELECT count(*) FROM information_schema.columns
			WHERE table_schema='public' AND table_name <> 'goose_db_version'
			AND data_type IN ('double precision','real','timestamp without time zone')`,
	}
	for name, q := range checks {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s: %d", name, n)
		}
	}
	_ = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name <> 'goose_db_version'").Scan(&tables)
	if tables != 122 {
		t.Fatalf("expected 122 tables, got %d", tables)
	}
}
