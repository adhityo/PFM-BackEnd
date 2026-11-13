package httpx

import (
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
)

// ETag encodes updated_at (microsecond precision, as stored by PostgreSQL).
func ETag(updatedAt time.Time) string {
	return `"` + strconv.FormatInt(updatedAt.UTC().UnixMicro(), 36) + `"`
}

// SetETag writes the ETag header.
func SetETag(c fiber.Ctx, updatedAt time.Time) {
	c.Set(fiber.HeaderETag, ETag(updatedAt))
}

// IfMatch parses the mandatory If-Match header back to updated_at.
func IfMatch(c fiber.Ctx) (time.Time, error) {
	raw := strings.TrimSpace(c.Get(fiber.HeaderIfMatch))
	if raw == "" {
		return time.Time{}, apperr.ErrPreconditionRequired
	}
	raw = strings.TrimPrefix(raw, "W/")
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return time.Time{}, apperr.ErrPreconditionFailed
	}
	us, err := strconv.ParseInt(raw[1:len(raw)-1], 36, 64)
	if err != nil {
		return time.Time{}, apperr.ErrPreconditionFailed
	}
	return time.UnixMicro(us).UTC(), nil
}
