// Package server wires middleware, modules and routes into a Fiber app.
package server

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/helmet"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/config"
	"pfm-backend/internal/module/file"
	"pfm-backend/internal/module/iam"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/platform/storage"
	"pfm-backend/internal/platform/validatorx"
)

// Catalog is the full permission catalog of the application.
func Catalog() []auth.Permission {
	return auth.MergeCatalogs(iam.Permissions, file.Permissions)
}

// ImageStore is the local image storage with a readiness probe.
type ImageStore interface {
	storage.ImageStorage
	Ping(ctx context.Context) error
}

// Deps are the long-lived dependencies of the HTTP server.
type Deps struct {
	Cfg    config.Config
	Log    *slog.Logger
	Pool   *pgxpool.Pool
	Images ImageStore
	Docs   storage.DocumentStorage
	Signer *security.JWTSigner
	Hasher *security.PasswordHasher
}

// New builds the Fiber application.
func New(d Deps) *fiber.App {
	cfg := d.Cfg
	app := fiber.New(fiber.Config{
		AppName:      "pfm-backend",
		BodyLimit:    file.MaxUploadBytes + 1<<20,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorHandler: httpx.ErrorHandler(d.Log),
		TrustProxy:   len(cfg.TrustedProxies) > 0,
		TrustProxyConfig: fiber.TrustProxyConfig{
			Proxies: cfg.TrustedProxies,
		},
		ProxyHeader: proxyHeader(cfg),
	})

	app.Use(recoverer.New(recoverer.Config{
		PanicHandler: func(c fiber.Ctx, r any) error {
			d.Log.Error("panic", "request_id", httpx.RequestID(c), "panic", r, "stack", string(debug.Stack()))
			return errors.New("panic recovered")
		},
	}))
	app.Use(httpx.RequestIDMiddleware())
	app.Use(httpx.AccessLog(d.Log))
	app.Use(helmet.New(helmet.Config{
		XSSProtection:             "0",
		ContentTypeNosniff:        "nosniff",
		XFrameOptions:             "DENY",
		ReferrerPolicy:            "no-referrer",
		ContentSecurityPolicy:     "default-src 'none'; frame-ancestors 'none'",
		CrossOriginOpenerPolicy:   "same-origin",
		CrossOriginResourcePolicy: "same-origin",
		XDNSPrefetchControl:       "off",
		XDownloadOptions:          "noopen",
		XPermittedCrossDomain:     "none",
		HSTSMaxAge:                hstsMaxAge(cfg),
	}))
	if len(cfg.CORSAllowedOrigins) > 0 {
		app.Use(cors.New(cors.Config{
			AllowOrigins:  cfg.CORSAllowedOrigins,
			AllowMethods:  []string{fiber.MethodGet, fiber.MethodPost, fiber.MethodPut, fiber.MethodDelete},
			AllowHeaders:  []string{fiber.HeaderAuthorization, fiber.HeaderContentType, fiber.HeaderIfMatch, "Idempotency-Key", fiber.HeaderXRequestID},
			ExposeHeaders: []string{fiber.HeaderETag, fiber.HeaderLocation, fiber.HeaderXRequestID},
			MaxAge:        600,
		}))
	}
	app.Use(limiter.New(limiter.Config{
		Max:          cfg.RateLimitPerMinute,
		Expiration:   time.Minute,
		LimitReached: func(fiber.Ctx) error { return fiber.ErrTooManyRequests },
		Next:         func(c fiber.Ctx) bool { return c.Path() == "/healthz" || c.Path() == "/readyz" },
	}))
	app.Use(httpx.NoStore())
	app.Use(httpx.TimeoutMiddleware(cfg.RequestTimeout))

	tx := database.NewTxManager(d.Pool)
	v := validatorx.New()
	rec := audit.NewRecorder(tx)

	users := iam.NewUserRepository(tx)
	roles := iam.NewRoleRepository(tx)
	actions := iam.NewActionRepository(tx)
	tokens := iam.NewRefreshTokenRepository(tx)
	permCache := iam.NewPermissionCache(roles, time.Minute)
	authn := auth.NewAuthenticator(d.Signer, iam.NewActorLoader(users), permCache)

	iamHandler := iam.NewHandler(v,
		iam.NewAuthService(tx, users, tokens, roles, d.Hasher, d.Signer, rec, iam.AuthConfig{
			RefreshTTL: cfg.JWTRefreshTTL, ActivationTTL: cfg.ActivationKeyTTL,
			LoginMaxAttempts: cfg.LoginMaxAttempts, LoginLockFor: cfg.LoginLockFor,
		}),
		iam.NewUserService(tx, users, roles, tokens, iam.NewClientLookup(tx), rec, cfg.ActivationKeyTTL),
		iam.NewRoleService(tx, roles, actions, users, permCache, rec),
		iam.NewActionService(tx, actions, roles, rec),
		iam.NewActivityLogService(iam.NewActivityLogRepository(tx)),
	)
	fileHandler := file.NewHandler(file.NewService(tx, file.NewRepository(tx), d.Images, d.Docs, rec, d.Log, cfg.S3PresignTTL))

	app.Get("/healthz", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/readyz", readiness(d))

	authLimiter := limiter.New(limiter.Config{
		Max:          10,
		Expiration:   time.Minute,
		LimitReached: func(fiber.Ctx) error { return fiber.ErrTooManyRequests },
	})
	api := auth.NewRouter(app.Group("/api/v1"), authn.Middleware(), Catalog())
	iamHandler.Routes(api, authLimiter)
	fileHandler.Routes(api)

	app.Use(func(fiber.Ctx) error { return apperr.NotFound("endpoint tidak ditemukan") })
	return app
}

func proxyHeader(cfg config.Config) string {
	if len(cfg.TrustedProxies) > 0 {
		return fiber.HeaderXForwardedFor
	}
	return ""
}

func hstsMaxAge(cfg config.Config) int {
	if cfg.IsProduction() {
		return 31536000
	}
	return 0
}

// readiness reports component health without exposing error details.
func readiness(d Deps) fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
		defer cancel()
		checks := fiber.Map{"database": "ok", "image_storage": "ok", "document_storage": "ok"}
		ready := true
		if err := d.Pool.Ping(ctx); err != nil {
			checks["database"], ready = "error", false
			d.Log.Warn("readiness: database", "err", err)
		}
		if err := d.Images.Ping(ctx); err != nil {
			checks["image_storage"], ready = "error", false
			d.Log.Warn("readiness: image storage", "err", err)
		}
		if err := d.Docs.Ping(ctx); err != nil {
			checks["document_storage"], ready = "error", false
			d.Log.Warn("readiness: document storage", "err", err)
		}
		status := fiber.StatusOK
		if !ready {
			status = fiber.StatusServiceUnavailable
		}
		return c.Status(status).JSON(fiber.Map{"ready": ready, "checks": checks})
	}
}
