// Package config loads and validates runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	AppEnv      string `env:"APP_ENV" envDefault:"development"`
	HTTPAddr    string `env:"HTTP_ADDR" envDefault:":8080"`
	MetricsAddr string `env:"METRICS_ADDR" envDefault:"127.0.0.1:9090"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`

	// Four databases (PRD §7.1), each with its own credentials: a leaked DSN
	// exposes one database only. The log DSN of the API should be read-only;
	// the worker uses an INSERT-only log user to ship the audit outbox.
	DatabaseURLCore     string `env:"DATABASE_URL_CORE"`
	DatabaseURLSecurity string `env:"DATABASE_URL_SECURITY"`
	DatabaseURLLog      string `env:"DATABASE_URL_LOG"`
	DatabaseURLDocument string `env:"DATABASE_URL_DOCUMENT"`

	MigrationURLCore     string `env:"MIGRATION_DATABASE_URL_CORE"`
	MigrationURLSecurity string `env:"MIGRATION_DATABASE_URL_SECURITY"`
	MigrationURLLog      string `env:"MIGRATION_DATABASE_URL_LOG"`
	MigrationURLDocument string `env:"MIGRATION_DATABASE_URL_DOCUMENT"`
	// ScaffoldDatabaseURL is the dev-only union database for cmd/scaffold and `migrate -db all`.
	ScaffoldDatabaseURL string `env:"SCAFFOLD_DATABASE_URL"`

	DBMaxConns int32 `env:"DB_MAX_CONNS" envDefault:"20"`

	JWTPrivateKeyFile string        `env:"JWT_PRIVATE_KEY_FILE"`
	JWTIssuer         string        `env:"JWT_ISSUER" envDefault:"pfm-backend"`
	JWTAudience       string        `env:"JWT_AUDIENCE" envDefault:"pfm-api"`
	JWTAccessTTL      time.Duration `env:"JWT_ACCESS_TTL" envDefault:"15m"`
	JWTRefreshTTL     time.Duration `env:"JWT_REFRESH_TTL" envDefault:"168h"`
	ActivationKeyTTL  time.Duration `env:"ACTIVATION_KEY_TTL" envDefault:"24h"`

	LoginMaxAttempts int           `env:"LOGIN_MAX_ATTEMPTS" envDefault:"5"`
	LoginLockFor     time.Duration `env:"LOGIN_LOCK_DURATION" envDefault:"15m"`

	CORSAllowedOrigins []string      `env:"CORS_ALLOWED_ORIGINS" envSeparator:","`
	TrustedProxies     []string      `env:"TRUSTED_PROXIES" envSeparator:","`
	RequestTimeout     time.Duration `env:"REQUEST_TIMEOUT" envDefault:"10s"`
	RateLimitPerMinute int           `env:"RATE_LIMIT_PER_MINUTE" envDefault:"300"`

	StorageImageRoot string `env:"STORAGE_IMAGE_ROOT"`

	S3Bucket       string        `env:"S3_BUCKET"`
	S3Region       string        `env:"S3_REGION" envDefault:"ap-southeast-3"`
	S3Endpoint     string        `env:"S3_ENDPOINT"`
	S3UsePathStyle bool          `env:"S3_USE_PATH_STYLE" envDefault:"false"`
	S3KMSKeyID     string        `env:"S3_KMS_KEY_ID"`
	S3PresignTTL   time.Duration `env:"S3_PRESIGN_TTL" envDefault:"5m"`
}

// IsProduction reports whether stricter production rules apply.
func (c Config) IsProduction() bool { return c.AppEnv == "production" }

// Load parses the environment.
// Outside production, a .env file in the working directory (or ENV_FILE) is
// read first; real environment variables take precedence over it.
func Load() (Config, error) {
	var c Config
	if os.Getenv("APP_ENV") != "production" {
		file := os.Getenv("ENV_FILE")
		if file == "" {
			file = ".env"
		}
		if err := loadDotEnv(file); err != nil {
			return c, err
		}
	}
	if err := env.Parse(&c); err != nil {
		return c, fmt.Errorf("parse env: %w", err)
	}
	return c, nil
}

// ValidateAPI checks the settings the HTTP server needs.
func (c Config) ValidateAPI() error {
	var errs []error
	errs = append(errs, c.ValidateDatabases()...)
	if c.JWTPrivateKeyFile == "" {
		errs = append(errs, errors.New("JWT_PRIVATE_KEY_FILE wajib diisi"))
	}
	if c.StorageImageRoot == "" {
		errs = append(errs, errors.New("STORAGE_IMAGE_ROOT wajib diisi"))
	}
	if c.S3Bucket == "" {
		errs = append(errs, errors.New("S3_BUCKET wajib diisi"))
	}
	if c.JWTAccessTTL <= 0 || c.JWTAccessTTL > time.Hour {
		errs = append(errs, errors.New("JWT_ACCESS_TTL harus > 0 dan <= 1h"))
	}
	if c.JWTRefreshTTL <= c.JWTAccessTTL {
		errs = append(errs, errors.New("JWT_REFRESH_TTL harus lebih besar dari JWT_ACCESS_TTL"))
	}
	if c.LoginMaxAttempts < 1 {
		errs = append(errs, errors.New("LOGIN_MAX_ATTEMPTS minimal 1"))
	}
	for _, o := range c.CORSAllowedOrigins {
		if strings.TrimSpace(o) == "*" {
			errs = append(errs, errors.New("CORS_ALLOWED_ORIGINS tidak boleh '*'"))
		}
	}
	for _, p := range c.TrustedProxies {
		if net.ParseIP(p) == nil {
			if _, _, err := net.ParseCIDR(p); err != nil {
				errs = append(errs, fmt.Errorf("TRUSTED_PROXIES berisi nilai tidak valid: %q", p))
			}
		}
	}
	if c.IsProduction() && c.S3Endpoint != "" && strings.HasPrefix(c.S3Endpoint, "http://") {
		errs = append(errs, errors.New("S3_ENDPOINT wajib https di production"))
	}
	return errors.Join(errs...)
}

// DatabaseURLs returns the runtime DSNs by database name.
func (c Config) DatabaseURLs() map[string]string {
	return map[string]string{"core": c.DatabaseURLCore, "security": c.DatabaseURLSecurity,
		"log": c.DatabaseURLLog, "document": c.DatabaseURLDocument}
}

// ValidateDatabases requires the four runtime DSNs and forbids sharing one
// DSN between databases (that would undo the separation).
func (c Config) ValidateDatabases() []error {
	var errs []error
	seen := map[string]string{}
	for _, name := range []string{"core", "security", "log", "document"} {
		dsn := c.DatabaseURLs()[name]
		if dsn == "" {
			errs = append(errs, fmt.Errorf("DATABASE_URL_%s wajib diisi", strings.ToUpper(name)))
			continue
		}
		if other, dup := seen[dsn]; dup {
			errs = append(errs, fmt.Errorf("DATABASE_URL_%s sama dengan DATABASE_URL_%s; tiap database harus terpisah",
				strings.ToUpper(name), strings.ToUpper(other)))
		}
		seen[dsn] = name
	}
	return errs
}

// MigrationURL returns the DSN used to migrate one database (falls back to
// the runtime DSN); "all" is the scaffold union database.
func (c Config) MigrationURL(name string) string {
	m := map[string]string{"core": c.MigrationURLCore, "security": c.MigrationURLSecurity,
		"log": c.MigrationURLLog, "document": c.MigrationURLDocument, "all": c.ScaffoldDatabaseURL}
	if m[name] != "" || name == "all" {
		return m[name]
	}
	return c.DatabaseURLs()[name]
}
