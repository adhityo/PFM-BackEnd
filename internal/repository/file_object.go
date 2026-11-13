package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/database"
)

// FileObjectRepository persists file_object.
type FileObjectRepository interface {
	Insert(ctx context.Context, p sqlc.InsertFileObjectParams) (model.FileObject, error)
	GetByUUID(ctx context.Context, id uuid.UUID) (model.FileObject, error)
	GetByUUIDForUpdate(ctx context.Context, id uuid.UUID) (model.FileObject, error)
	Activate(ctx context.Context, id int64, actor string) error
	SoftDelete(ctx context.Context, id uuid.UUID, actor string) (model.FileObject, error)
	ListStalePending(ctx context.Context, before time.Time, limit int32) ([]model.FileObject, error)
	HardDelete(ctx context.Context, id int64) error
}

type fileObjectRepository struct{ tx *database.TxManager }

func NewFileObjectRepository(tx *database.TxManager) FileObjectRepository {
	return &fileObjectRepository{tx: tx}
}

func (r *fileObjectRepository) Insert(ctx context.Context, p sqlc.InsertFileObjectParams) (model.FileObject, error) {
	o, err := r.tx.Queries(ctx).InsertFileObject(ctx, p)
	return o, database.MapError("insert file", err)
}

func (r *fileObjectRepository) GetByUUID(ctx context.Context, id uuid.UUID) (model.FileObject, error) {
	o, err := r.tx.Queries(ctx).GetFileObjectByUUID(ctx, id)
	return o, database.MapError("get file", err)
}

func (r *fileObjectRepository) GetByUUIDForUpdate(ctx context.Context, id uuid.UUID) (model.FileObject, error) {
	o, err := r.tx.Queries(ctx).GetFileObjectByUUIDForUpdate(ctx, id)
	return o, database.MapError("lock file", err)
}

func (r *fileObjectRepository) Activate(ctx context.Context, id int64, actor string) error {
	return database.MapError("activate file", r.tx.Queries(ctx).ActivateFileObject(ctx, sqlc.ActivateFileObjectParams{ID: id, Actor: &actor}))
}

func (r *fileObjectRepository) SoftDelete(ctx context.Context, id uuid.UUID, actor string) (model.FileObject, error) {
	o, err := r.tx.Queries(ctx).SoftDeleteFileObject(ctx, sqlc.SoftDeleteFileObjectParams{Uuid: id, Actor: &actor})
	return o, database.MapError("delete file", err)
}

func (r *fileObjectRepository) ListStalePending(ctx context.Context, before time.Time, limit int32) ([]model.FileObject, error) {
	o, err := r.tx.Queries(ctx).ListStalePendingFiles(ctx, sqlc.ListStalePendingFilesParams{Before: before, Limit: limit})
	return o, database.MapError("list stale files", err)
}

func (r *fileObjectRepository) HardDelete(ctx context.Context, id int64) error {
	return database.MapError("hard delete file", r.tx.Queries(ctx).HardDeleteFileObject(ctx, id))
}
