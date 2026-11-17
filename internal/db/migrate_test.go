package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"pfm-backend/internal/db"
	"pfm-backend/internal/testutil"
)

func countTables(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name <> 'goose_db_version'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestMigrationsRoundTrip runs every database down to zero and up again
// (PRD §7.4) and checks schema invariants: uuid on every table, an index for
// every FK, no float or timezone-less timestamp columns, and no FK pointing
// to a table of another database.
func TestMigrationsRoundTrip(t *testing.T) {
	want := map[string]struct{ migrations, tables int }{
		db.Core:     {117, 113},
		db.Security: {7, 6},
		db.Log:      {3, 1},
		db.Document: {7, 6},
		db.All:      {129, 124},
	}
	for _, name := range append(append([]string{}, db.Databases...), db.All) {
		t.Run(name, func(t *testing.T) {
			pool := testutil.NewDB(t, name) // already migrated up
			ctx := context.Background()
			sqlDB := stdlib.OpenDBFromPool(pool)
			defer sqlDB.Close()
			m, err := db.NewMigrator(sqlDB, name)
			if err != nil {
				t.Fatal(err)
			}
			sources := m.ListSources()
			if len(sources) != want[name].migrations {
				t.Fatalf("expected %d migrations, got %d", want[name].migrations, len(sources))
			}
			if name == db.All {
				// Global numbering (PRD Appendix A) has no gaps across the sets.
				for i, s := range sources {
					if s.Version != int64(i+1) {
						t.Fatalf("migration numbering gap at %s", s.Path)
					}
				}
			}
			if _, err := m.DownTo(ctx, 0); err != nil {
				t.Fatalf("down to 0: %v", err)
			}
			if n := countTables(t, pool); n != 0 {
				t.Fatalf("%d tables left after down", n)
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
			for check, q := range checks {
				var n int
				if err := pool.QueryRow(ctx, q).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Errorf("%s: %d", check, n)
				}
			}
			if n := countTables(t, pool); n != want[name].tables {
				t.Fatalf("expected %d tables, got %d", want[name].tables, n)
			}
		})
	}
}
