// Command worker runs background jobs:
//   - every few seconds: ship the audit outboxes (pfm_core, pfm_security,
//     pfm_document) into pfm_log, and send queued email (mail_outbox) over SMTP;
//   - every 15 minutes: remove stale pending uploads (pfm_document) and expired
//     refresh tokens (pfm_security).
//
// It reads the same DATABASE_URL_* variables as the API, but should run with
// its own users: one that may read/delete audit_outbox in the source
// databases and one that may only INSERT into pfm_log (deploy/postgres).
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
	"pfm-backend/internal/db"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/logger"
	"pfm-backend/internal/platform/mail"
	"pfm-backend/internal/platform/storage"
	"pfm-backend/internal/repository"
	"pfm-backend/internal/service"
)

const (
	shipInterval      = 5 * time.Second
	cleanupInterval   = 15 * time.Minute
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
	if errs := cfg.ValidateDatabases(); len(errs) > 0 {
		return fmt.Errorf("konfigurasi tidak valid: %v", errs)
	}
	if err := cfg.ValidateMail(); err != nil {
		return err
	}
	log := logger.New(os.Stdout, cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pools, err := database.OpenPools(ctx, cfg.DatabaseURLs(), 4)
	if err != nil {
		return err
	}
	defer pools.Close()
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

	core, sec := database.NewTxManager(pools.Core), database.NewTxManager(pools.Security)
	docTx, logTx := database.NewTxManager(pools.Document), database.NewTxManager(pools.Log)
	sources := map[string]*database.TxManager{db.Core: core, db.Security: sec, db.Document: docTx}
	shipper := audit.NewShipper(logTx, sources)
	var mailer *mail.Deliverer
	if cfg.MailEnabled() {
		sender, err := mail.NewSMTPSender(mail.SMTPConfig{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername,
			Password: cfg.SMTPPassword, TLS: cfg.SMTPTLS, From: cfg.SMTPFrom, Timeout: cfg.SMTPTimeout})
		if err != nil {
			return err
		}
		mailer = mail.NewDeliverer(sources, sender, log)
		log.Info("pengiriman email aktif", "smtp", cfg.SMTPHost, "port", cfg.SMTPPort, "tls", cfg.SMTPTLS)
	} else {
		log.Warn("SMTP_HOST kosong: email tetap di antrean mail_outbox sampai SMTP dikonfigurasi")
	}
	files := service.NewFileService(docTx, repository.NewFileObjectRepository(docTx), images, docs,
		audit.NewRecorder(docTx, db.Document), log, cfg.S3PresignTTL)

	ship := time.NewTicker(shipInterval)
	defer ship.Stop()
	cleanup := time.NewTicker(cleanupInterval)
	defer cleanup.Stop()
	runCleanup(ctx, log, sec, files)
	for {
		if n, err := shipper.ShipAll(ctx); err != nil {
			log.Error("ship audit outbox", "err", err)
		} else if n > 0 {
			log.Debug("ship audit outbox", "events", n)
		}
		if mailer != nil {
			if n, err := mailer.DeliverDue(ctx); err != nil {
				log.Error("kirim email", "err", err)
			} else if n > 0 {
				log.Info("email terkirim", "jumlah", n)
			}
		}
		select {
		case <-ctx.Done():
			log.Info("worker stopped")
			return nil
		case <-cleanup.C:
			runCleanup(ctx, log, sec, files)
		case <-ship.C:
		}
	}
}

func runCleanup(ctx context.Context, log *slog.Logger, sec *database.TxManager, files *service.FileService) {
	if n, err := files.CleanupStale(ctx, time.Now().Add(-pendingFileMaxAge)); err != nil {
		log.Error("cleanup stale files", "err", err)
	} else if n > 0 {
		log.Info("cleanup stale files", "deleted", n)
	}
	if n, err := sec.Queries(ctx).DeleteExpiredRefreshTokens(ctx); err != nil {
		log.Error("cleanup refresh tokens", "err", err)
	} else if n > 0 {
		log.Info("cleanup refresh tokens", "deleted", n)
	}
}
