package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/database"
)

// ClientLookup resolves client UUIDs (owned by the portfolio module later).
type ClientLookup interface {
	ClientIDByUUID(ctx context.Context, id uuid.UUID) (int64, error)
}

type clientLookup struct{ tx *database.TxManager }

func NewClientLookup(tx *database.TxManager) ClientLookup { return &clientLookup{tx: tx} }

func (r *clientLookup) ClientIDByUUID(ctx context.Context, id uuid.UUID) (int64, error) {
	cid, err := r.tx.Queries(ctx).GetClientIDByUUID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, apperr.Invalid("client_uuid", "exists", "client tidak ditemukan")
		}
		return 0, database.MapError("get client", err)
	}
	return cid, nil
}
