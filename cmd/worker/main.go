// Command worker runs periodic maintenance jobs:
// removing stale pending uploads and expired refresh tokens.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"pfm-backend/internal/config"
	"pfm-backend/internal/module/file"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/logger"
	"pfm-backend/internal/platform/storage"
)

const (
	interval          = 15 * time.Minute
	pendingFileMaxAge = 24 * time.Hour
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
	log := logger.New(os.Stdout, cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL, 4)
	if err != nil {
		return err
	}
	defer pool.Close()
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

	tx := database.NewTxManager(pool)
	files := file.NewService(tx, file.NewRepository(tx), images, docs, audit.NewRecorder(tx), log, cfg.S3PresignTTL)

	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		runOnce(ctx, log, tx, files)
		select {
		case <-ctx.Done():
			log.Info("worker stopped")
			return nil
		case <-tick.C:
		}
	}
}

func runOnce(ctx context.Context, log *slog.Logger, tx *database.TxManager, files *file.Service) {
	if n, err := files.CleanupStale(ctx, time.Now().Add(-pendingFileMaxAge)); err != nil {
		log.Error("cleanup stale files", "err", err)
	} else if n > 0 {
		log.Info("cleanup stale files", "deleted", n)
	}
	if n, err := tx.Queries(ctx).DeleteExpiredRefreshTokens(ctx); err != nil {
		log.Error("cleanup refresh tokens", "err", err)
	} else if n > 0 {
		log.Info("cleanup refresh tokens", "deleted", n)
	}
}
