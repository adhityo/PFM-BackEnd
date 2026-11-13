// Command migrate manages the database schema (goose) and seeds reference data.
//
//	migrate up | down | down-to <version> | status | version | reset
//	migrate seed        # sync permission catalog (action) and default roles
//	migrate seed-admin  # create the first ADMIN user from ADMIN_USERNAME/ADMIN_EMAIL/ADMIN_PASSWORD
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	if len(args) == 0 {
		return errors.New("perintah: up | down | down-to <v> | status | version | reset | seed | seed-admin")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	dsn := cfg.MigrationURL()
	if dsn == "" {
		return errors.New("MIGRATION_DATABASE_URL atau DATABASE_URL wajib diisi")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, dsn, 4)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch args[0] {
	case "seed":
		return seed(ctx, database.NewTxManager(pool))
	case "seed-admin":
		return seedAdmin(ctx, database.NewTxManager(pool))
	}

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	p, err := db.NewMigrator(sqlDB)
	if err != nil {
		return err
	}

	switch args[0] {
	case "up":
		res, err := p.Up(ctx)
		for _, r := range res {
			fmt.Printf("OK   %s (%s)\n", r.Source.Path, r.Duration.Round(time.Millisecond))
		}
		return err
	case "down":
		r, err := p.Down(ctx)
		if r != nil {
			fmt.Printf("DOWN %s\n", r.Source.Path)
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
			fmt.Printf("DOWN %s\n", r.Source.Path)
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
			fmt.Printf("%-55s %s\n", s.Source.Path, applied)
		}
		return nil
	case "version":
		v, err := p.GetDBVersion(ctx)
		if err != nil {
			return err
		}
		fmt.Println(v)
		return nil
	default:
		return fmt.Errorf("perintah tidak dikenal: %s", args[0])
	}
}

func seed(ctx context.Context, tx *database.TxManager) error {
	catalog := router.Catalog()
	if err := service.SyncCatalog(ctx, tx, repository.NewActionRepository(tx), repository.NewRoleRepository(tx), catalog); err != nil {
		return err
	}
	if err := repository.NewConfigurationRepository(tx).Ensure(ctx); err != nil {
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
