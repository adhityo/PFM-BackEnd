// Command api runs the PFM HTTP API.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/config"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/logger"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/platform/storage"
	"pfm-backend/internal/router"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateAPI(); err != nil {
		return fmt.Errorf("konfigurasi tidak valid:\n%w", err)
	}
	log := logger.New(os.Stdout, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pools, err := database.OpenPools(ctx, cfg.DatabaseURLs(), cfg.DBMaxConns)
	if err != nil {
		return err
	}
	defer pools.Close()

	priv, err := security.LoadEd25519PrivateKey(cfg.JWTPrivateKeyFile)
	if err != nil {
		return err
	}
	images, err := storage.NewLocalImageStorage(cfg.StorageImageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = images.Close() }()
	docs, err := storage.NewS3DocumentStorage(ctx, storage.S3Config{
		Bucket: cfg.S3Bucket, Region: cfg.S3Region, Endpoint: cfg.S3Endpoint,
		UsePathStyle: cfg.S3UsePathStyle, KMSKeyID: cfg.S3KMSKeyID,
	})
	if err != nil {
		return err
	}

	app := router.New(router.Deps{
		Cfg: cfg, Log: log, Pools: pools, Images: images, Docs: docs,
		Signer: security.NewJWTSigner(priv, cfg.JWTIssuer, cfg.JWTAudience, cfg.JWTAccessTTL),
		Hasher: security.NewPasswordHasher(security.DefaultArgon2Params),
	})

	log.Info("starting api", "addr", cfg.HTTPAddr, "env", cfg.AppEnv)
	return app.Listen(cfg.HTTPAddr, fiber.ListenConfig{
		GracefulContext:       ctx,
		ShutdownTimeout:       30 * time.Second,
		DisableStartupMessage: true,
	})
}
