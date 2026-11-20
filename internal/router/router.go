// Package router builds the Fiber app: middleware chain and dependency
// wiring (repository -> service -> handler) for every route.
package router

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

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/config"
	"pfm-backend/internal/db"
	"pfm-backend/internal/handler"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/platform/security"
	"pfm-backend/internal/platform/storage"
	"pfm-backend/internal/platform/validatorx"
	"pfm-backend/internal/repository"
	"pfm-backend/internal/service"
)

// Catalog is the full permission catalog of the application.
func Catalog() []auth.Permission {
	return permission.Label(auth.MergeCatalogs(permission.Static(), handler.CRUDPermissions(crudResources(service.Deps{}))))
}

// crudResources lists every scaffolded resource, module by module.
func crudResources(d service.Deps) []crud.Registrar {
	var out []crud.Registrar
	out = append(out, service.MasterdataResources(d)...)
	out = append(out, service.MarketResources(d)...)
	out = append(out, service.PortfolioResources(d)...)
	out = append(out, service.AccountingResources(d)...)
	out = append(out, service.TradingResources(d)...)
	out = append(out, service.BondResources(d)...)
	return append(out, service.PensionResources(d)...)
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
	Pools  database.Pools
	Images ImageStore
	Docs   storage.DocumentStorage
	Signer *security.JWTSigner
	Hasher *security.PasswordHasher
}

// New builds the Fiber application.
func New(d Deps) *fiber.App {
	app, _ := build(d)
	return app
}

// Describe lists every API route (paths relative to /api/v1) with its access
// rule, plus the CRUD resource descriptions, for API documentation
// (cmd/apidoc). No connection is opened: dependencies are only wired.
func Describe() ([]auth.RouteInfo, []crud.ResourceDoc) {
	_, api := build(Deps{Log: slog.New(slog.DiscardHandler)})
	var docs []crud.ResourceDoc
	for _, r := range crudResources(service.Deps{}) {
		docs = append(docs, r.Doc())
	}
	return api.Routes(), docs
}

func build(d Deps) (*fiber.App, *auth.Router) {
	cfg := d.Cfg
	app := fiber.New(fiber.Config{
		AppName:      "pfm-backend",
		BodyLimit:    service.MaxUploadBytes + 1<<20,
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
			ExposeHeaders: []string{fiber.HeaderETag, fiber.HeaderLocation, fiber.HeaderXRequestID, auth.HeaderPermissionsVersion},
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

	// One transaction manager and audit recorder per database (PRD §7.1).
	tx := database.NewTxManager(d.Pools.Core)
	sec := database.NewTxManager(d.Pools.Security)
	logs := database.NewTxManager(d.Pools.Log)
	docs := database.NewTxManager(d.Pools.Document)
	v := validatorx.New()
	rec := audit.NewRecorder(tx, db.Core)
	secRec := audit.NewRecorder(sec, db.Security)
	docRec := audit.NewRecorder(docs, db.Document)

	// repositories
	users := repository.NewUserRepository(sec)
	roles := repository.NewRoleRepository(sec)
	actions := repository.NewActionRepository(sec)
	tokens := repository.NewRefreshTokenRepository(sec)
	clients := repository.NewClientLookup(tx)

	// services
	permCache := service.NewPermissionCache(roles, actions, time.Minute)
	authSvc := service.NewAuthService(sec, users, tokens, roles, d.Hasher, d.Signer, secRec, service.AuthConfig{
		RefreshTTL: cfg.JWTRefreshTTL, ActivationTTL: cfg.ActivationKeyTTL,
		LoginMaxAttempts: cfg.LoginMaxAttempts, LoginLockFor: cfg.LoginLockFor,
	})
	userSvc := service.NewUserService(sec, users, roles, tokens, clients, secRec, cfg.ActivationKeyTTL)
	roleSvc := service.NewRoleService(sec, roles, actions, users, permCache, secRec)
	actionSvc := service.NewActionService(sec, actions, roles, permCache, secRec)
	logSvc := service.NewActivityLogService(repository.NewActivityLogRepository(logs))
	fileSvc := service.NewFileService(docs, repository.NewFileObjectRepository(docs), d.Images, d.Docs, docRec, d.Log, cfg.S3PresignTTL)
	configSvc := service.NewConfigurationService(tx, repository.NewConfigurationRepository(tx), fileSvc, rec)
	deps := service.Deps{TX: tx, Security: sec, Files: fileSvc}

	app.Get("/healthz", func(c fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })
	app.Get("/readyz", readiness(d))

	// routes
	authLimiter := limiter.New(limiter.Config{
		Max:          10,
		Expiration:   time.Minute,
		LimitReached: func(fiber.Ctx) error { return fiber.ErrTooManyRequests },
	})
	authn := auth.NewAuthenticator(d.Signer, service.NewActorLoader(users, clients), permCache)
	api := auth.NewRouter(app.Group("/api/v1"), authn.Middleware(), Catalog())

	handler.NewAuthHandler(v, authSvc).Register(api, authLimiter)
	handler.NewUserHandler(v, userSvc, roleSvc).Register(api)
	handler.NewRoleHandler(v, roleSvc).Register(api)
	handler.NewActionHandler(v, actionSvc).Register(api)
	handler.NewActivityLogHandler(logSvc).Register(api)
	handler.NewFileHandler(fileSvc).Register(api)
	handler.NewConfigurationHandler(v, configSvc).Register(api)
	handler.NewPortfolioTxHandler(v, service.NewPortfolioTxService(tx, rec)).Register(api)
	journals := service.NewJournalService(tx)
	handler.NewAccountingHandler(v, service.NewAccountingService(tx, journals, rec)).Register(api)
	handler.NewTradeHandler(service.NewTradeService(tx, journals, rec)).Register(api)
	handler.NewBondHandler(v, service.NewBondTxService(deps, rec), service.NewChangeRequestService(deps, rec)).Register(api)
	handler.NewPensionHandler(v, service.NewPensionService(deps, rec)).Register(api)
	handler.RegisterCRUD(crud.Env{Router: api, Validator: v, TX: tx, Audit: rec, Files: fileSvc}, crudResources(deps))

	app.Use(func(fiber.Ctx) error { return apperr.NotFound("endpoint tidak ditemukan") })
	return app, api
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
		checks := fiber.Map{"image_storage": "ok", "document_storage": "ok"}
		ready := true
		for name, pool := range d.Pools.Named() {
			checks["database_"+name] = "ok"
			if err := pool.Ping(ctx); err != nil {
				checks["database_"+name], ready = "error", false
				d.Log.Warn("readiness: database", "db", name, "err", err)
			}
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
