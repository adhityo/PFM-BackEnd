package crud

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/database"
)

// Conversions from validated request strings to nullable column values.
// Empty input means NULL.

func Text(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// Decimal parses a string already checked by the "decimal" validator.
func Decimal(s string) *decimal.Decimal {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil
	}
	return &d
}

// Date parses YYYY-MM-DD already checked by the validator.
func Date(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil
	}
	return &t
}

// Time parses an RFC 3339 timestamp already checked by the validator.
func Time(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

// NewID returns a time-ordered UUIDv7.
func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// Affected maps an :execrows result of a conditional UPDATE: zero rows means
// the row changed since it was read.
func Affected(op string, n int64, err error) error {
	if err != nil {
		return database.MapError(op, err)
	}
	if n == 0 {
		return apperr.ErrPreconditionFailed
	}
	return nil
}
