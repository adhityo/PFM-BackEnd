// Package repository persists domain data through sqlc queries. Each
// entity has an interface (used by services) and a PostgreSQL implementation.
package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/database"
)

// Conditional updates (WHERE updated_at = expected) return no rows when the
// row changed meanwhile. Services always load the row first (scope + 404),
// so "no rows" here means a stale ETag.
func staleIfNoRows(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.ErrPreconditionFailed
	}
	return database.MapError(op, err)
}

func ptr[T any](v T) *T { return &v }

func newUUID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
