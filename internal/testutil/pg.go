// Package testutil provides migrated PostgreSQL databases for integration tests.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"pfm-backend/internal/db"
	"pfm-backend/internal/platform/database"
)

// NewDB creates an isolated database on TEST_DATABASE_URL, applies the
// migration set name (db.Core, db.Security, db.Log, db.Document or db.All)
// and drops it when the test ends. The test is skipped when
// TEST_DATABASE_URL is not set.
func NewDB(t *testing.T, name string) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("TEST_DATABASE_URL")
	if admin == "" {
		t.Skip("TEST_DATABASE_URL tidak diset; integration test dilewati")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	dbName := "pfm_test_" + name + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatalf("create database: %v", err)
	}
	_ = conn.Close(ctx)

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + dbName
	pool, err := database.NewPool(ctx, u.String(), 10)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	m, err := db.NewMigrator(sqlDB, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Up(ctx); err != nil {
		t.Fatalf("migrate up %s: %v", name, err)
	}
	_ = sqlDB.Close()

	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		_, _ = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
	})
	return pool
}

// NewPools creates the four runtime databases of one test.
func NewPools(t *testing.T) database.Pools {
	t.Helper()
	return database.Pools{Core: NewDB(t, db.Core), Security: NewDB(t, db.Security), Log: NewDB(t, db.Log),
		Document: NewDB(t, db.Document)}
}
