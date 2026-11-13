package httpx

import (
	"context"
	"log/slog"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type requestIDKey struct{}

var reRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestIDMiddleware accepts a well-formed incoming X-Request-ID or generates
// a UUIDv7, stores it in the request context and echoes it in the response.
func RequestIDMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		rid := c.Get(fiber.HeaderXRequestID)
		if !reRequestID.MatchString(rid) {
			id, err := uuid.NewV7()
			if err != nil {
				id = uuid.New()
			}
			rid = id.String()
		}
		c.Set(fiber.HeaderXRequestID, rid)
		c.Locals(requestIDKey{}, rid)
		c.SetContext(context.WithValue(c.Context(), requestIDKey{}, rid))
		return c.Next()
	}
}

// RequestID returns the current request ID.
func RequestID(c fiber.Ctx) string {
	if v, ok := c.Locals(requestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// RequestIDFromContext returns the request ID stored in a context.Context.
func RequestIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey{}).(string)
	return v
}

// TimeoutMiddleware attaches a deadline to the request context so database
// calls are cancelled when a request runs too long.
func TimeoutMiddleware(d time.Duration) fiber.Handler {
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), d)
		defer cancel()
		c.SetContext(ctx)
		return c.Next()
	}
}

// AccessLog logs one line per request without query strings or bodies.
// Errors are rendered here (through the app error handler) so the logged
// status matches what the client receives.
func AccessLog(log *slog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		if err := c.Next(); err != nil {
			if herr := c.App().ErrorHandler(c, err); herr != nil {
				_ = c.SendStatus(fiber.StatusInternalServerError)
			}
		}
		log.Info("http_request",
			"request_id", RequestID(c),
			"method", c.Method(),
			"route", c.Route().Path,
			"status", c.Response().StatusCode(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.IP(),
		)
		return nil
	}
}

// NoStore marks API responses as non-cacheable.
func NoStore() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.Next()
	}
}
