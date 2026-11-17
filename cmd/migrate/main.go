// Command migrate manages the schema of the four databases (goose) and seeds
// reference data.
//
//	migrate [-db core|security|log|document] up | status | version
//	migrate -db <name> down | down-to <version> | reset    # one database at a time
//	migrate -db all up                                     # scaffold union DB (SCAFFOLD_DATABASE_URL)
//	migrate seed        # permission catalog & default roles (security), configuration (core)
//	migrate seed-admin  # first ADMIN user from ADMIN_USERNAME/ADMIN_EMAIL/ADMIN_PASSWORD (security)
//
// Without -db, up/status/version run on every database in order. Each
// database uses MIGRATION_DATABASE_URL_<NAME> (or DATABASE_URL_<NAME>).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/config"
	"pfm-backend/internal/db"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/repository"
	"pfm-backend/internal/router"
	"pfm-backend/internal/service"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fl := flag.NewFlagSet("migrate", flag.ContinueOnError)
	only := fl.String("db", "", "database: core, security, log, document (default: semua) atau all (scaffold)")
	if err := fl.Parse(args); err != nil {
		return err
	}
	args = fl.Args()
	if len(args) == 0 {
		return errors.New("perintah: up | down | down-to <v> | status | version | reset | seed | seed-admin")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	switch args[0] {
	case "seed":
		return withPools(ctx, cfg, []string{db.Security, db.Core}, func(tx map[string]*database.TxManager) error {
			return seed(ctx, tx[db.Security], tx[db.Core])
		})
	case "seed-admin":
		return withPools(ctx, cfg, []string{db.Security}, func(tx map[string]*database.TxManager) error {
			return seedAdmin(ctx, tx[db.Security])
		})
	}

	targets := db.Databases
	if *only != "" {
		if *only != db.All && !slices.Contains(db.Databases, *only) {
			return fmt.Errorf("database tidak dikenal: %s", *only)
		}
		targets = []string{*only}
	}
	if slices.Contains([]string{"down", "down-to", "reset"}, args[0]) && len(targets) != 1 {
		return errors.New(args[0] + " harus memilih satu database dengan -db")
	}
	for _, name := range targets {
		if err := migrateOne(ctx, cfg, name, args); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// withPools opens the named databases with their migration DSNs.
func withPools(ctx context.Context, cfg config.Config, names []string, fn func(map[string]*database.TxManager) error) error {
	tx := map[string]*database.TxManager{}
	for _, name := range names {
		dsn := cfg.MigrationURL(name)
		if dsn == "" {
			return fmt.Errorf("MIGRATION_DATABASE_URL_%s atau DATABASE_URL_%s wajib diisi", upper(name), upper(name))
		}
		pool, err := database.NewPool(ctx, dsn, 4)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		defer pool.Close()
		tx[name] = database.NewTxManager(pool)
	}
	return fn(tx)
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

func migrateOne(ctx context.Context, cfg config.Config, name string, args []string) error {
	dsn := cfg.MigrationURL(name)
	if dsn == "" {
		if name == db.All {
			return errors.New("SCAFFOLD_DATABASE_URL wajib diisi")
		}
		return fmt.Errorf("MIGRATION_DATABASE_URL_%s atau DATABASE_URL_%s wajib diisi", upper(name), upper(name))
	}
	pool, err := database.NewPool(ctx, dsn, 4)
	if err != nil {
		return err
	}
	defer pool.Close()
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	p, err := db.NewMigrator(sqlDB, name)
	if err != nil {
		return err
	}

	switch args[0] {
	case "up":
		res, err := p.Up(ctx)
		for _, r := range res {
			fmt.Printf("[%s] OK   %s (%s)\n", name, r.Source.Path, r.Duration.Round(time.Millisecond))
		}
		return err
	case "down":
		r, err := p.Down(ctx)
		if r != nil {
			fmt.Printf("[%s] DOWN %s\n", name, r.Source.Path)
		}
		return err
	case "down-to":
		if len(args) < 2 {
			return errors.New("down-to membutuhkan nomor versi")
		}
		v, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return err
		}
		if v == 0 && cfg.IsProduction() {
			return errors.New("down-to 0 tidak diizinkan di production")
		}
		res, err := p.DownTo(ctx, v)
		for _, r := range res {
			fmt.Printf("[%s] DOWN %s\n", name, r.Source.Path)
		}
		return err
	case "reset":
		if cfg.IsProduction() {
			return errors.New("reset tidak diizinkan di production")
		}
		_, err := p.DownTo(ctx, 0)
		return err
	case "status":
		st, err := p.Status(ctx)
		if err != nil {
			return err
		}
		for _, s := range st {
			applied := "pending"
			if !s.AppliedAt.IsZero() {
				applied = s.AppliedAt.Format(time.RFC3339)
			}
			fmt.Printf("[%s] %-55s %s\n", name, s.Source.Path, applied)
		}
		return nil
	case "version":
		v, err := p.GetDBVersion(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("[%s] %d\n", name, v)
		return nil
	default:
		return fmt.Errorf("perintah tidak dikenal: %s", args[0])
	}
}

func seed(ctx context.Context, sec, core *database.TxManager) error {
	catalog := router.Catalog()
	if err := service.SyncCatalog(ctx, sec, repository.NewActionRepository(sec), repository.NewRoleRepository(sec), catalog); err != nil {
		return err
	}
	if err := repository.NewConfigurationRepository(core).Ensure(ctx); err != nil {
		return err
	}
	fmt.Printf("seed OK: %d permission, %d role\n", len(catalog), len(service.DefaultRoles))
	return nil
}

func seedAdmin(ctx context.Context, tx *database.TxManager) error {
	username, email, password := os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD")
	if username == "" || email == "" || len(password) < 12 {
		return errors.New("ADMIN_USERNAME, ADMIN_EMAIL dan ADMIN_PASSWORD (min 12 karakter) wajib diisi")
	}
	users := repository.NewUserRepository(tx)
	roles := repository.NewRoleRepository(tx)
	hasher := security.NewPasswordHasher(security.DefaultArgon2Params)

	return tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := users.CredentialByLogin(ctx, username); err == nil {
			fmt.Println("admin sudah ada, tidak ada perubahan")
			return nil
		} else if !errors.Is(err, apperr.ErrNotFound) {
			return err
		}
		role, err := roles.UpsertByCode(ctx, auth.RoleAdmin, "ADMINISTRATOR", "system")
		if err != nil {
			return err
		}
		_, keyHash, err := security.NewOpaqueToken()
		if err != nil {
			return err
		}
		id := uuid.Must(uuid.NewV7())
		uid, err := users.Create(ctx, model.NewUser{UUID: id, Username: username, Email: email, Status: model.StatusActive,
			RoleID: role.ID, ActivationKeyHash: keyHash, ActivationExpires: time.Now(), Actor: "system"})
		if err != nil {
			return err
		}
		hash, err := hasher.Hash(password)
		if err != nil {
			return err
		}
		if err := users.SetPassword(ctx, uid, hash, "system"); err != nil {
			return err
		}
		fmt.Printf("admin %q dibuat (uuid %s)\n", username, id)
		return nil
	})
}
