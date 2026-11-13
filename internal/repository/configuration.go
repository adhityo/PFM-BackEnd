package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/database"
)

// ConfigurationRepository persists the configuration singleton.
type ConfigurationRepository interface {
	Get(ctx context.Context) (sqlc.GetConfigurationRow, error)
	// Lock returns the id and updated_at of the singleton, locked FOR UPDATE.
	Lock(ctx context.Context) (int64, time.Time, error)
	Update(ctx context.Context, p sqlc.UpdateConfigurationParams) error
	Ensure(ctx context.Context) error
}

type configurationRepository struct{ tx *database.TxManager }

func NewConfigurationRepository(tx *database.TxManager) ConfigurationRepository {
	return &configurationRepository{tx: tx}
}

var errConfigurationMissing = apperr.NotFound("konfigurasi belum di-seed (jalankan `migrate seed`)")

func (r *configurationRepository) Get(ctx context.Context) (sqlc.GetConfigurationRow, error) {
	row, err := r.tx.Queries(ctx).GetConfiguration(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errConfigurationMissing
	}
	return row, database.MapError("get configuration", err)
}

func (r *configurationRepository) Lock(ctx context.Context) (int64, time.Time, error) {
	row, err := r.tx.Queries(ctx).LockConfiguration(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, time.Time{}, errConfigurationMissing
	}
	return row.ID, row.UpdatedAt, database.MapError("lock configuration", err)
}

func (r *configurationRepository) Update(ctx context.Context, p sqlc.UpdateConfigurationParams) error {
	n, err := r.tx.Queries(ctx).UpdateConfiguration(ctx, p)
	if err != nil {
		return database.MapError("update configuration", err)
	}
	if n == 0 {
		return apperr.ErrPreconditionFailed
	}
	return nil
}

func (r *configurationRepository) Ensure(ctx context.Context) error {
	id, err := uuid.NewV7()
	if err != nil {
		id = uuid.New()
	}
	return database.MapError("ensure configuration", r.tx.Queries(ctx).EnsureConfiguration(ctx, id))
}
