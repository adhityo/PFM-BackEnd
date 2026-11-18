package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
)

// ActionRepository persists the action catalog.
type ActionRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID) (model.Action, error)
	GetByUUIDs(ctx context.Context, ids []uuid.UUID) ([]model.Action, error)
	List(ctx context.Context, f model.ActionFilter, p model.Paging) ([]model.Action, int64, error)
	Catalog(ctx context.Context) ([]model.Action, error)
	Create(ctx context.Context, in model.ActionInput) (model.Action, error)
	Update(ctx context.Context, id uuid.UUID, in model.ActionInput, expected time.Time) (model.Action, error)
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error
	CountRoleUsage(ctx context.Context, actionID int64) (int64, error)
	// SyncSystem inserts or refreshes a permission of the code catalog.
	SyncSystem(ctx context.Context, p auth.Permission, actor string) error
}

type actionRepository struct{ tx *database.TxManager }

func NewActionRepository(tx *database.TxManager) ActionRepository { return &actionRepository{tx: tx} }

func toAction(a sqlc.Action) model.Action {
	return model.Action{ID: a.ID, UUID: a.Uuid, ActionCode: deref(a.ActionCode), ActionName: deref(a.ActionName),
		Description: a.Description, Module: deref(a.Module), ResourceLabel: a.ResourceLabel, IsSystem: a.IsSystem,
		CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func toActions(rows []sqlc.Action) []model.Action {
	out := make([]model.Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAction(row))
	}
	return out
}

func (r *actionRepository) GetByUUID(ctx context.Context, id uuid.UUID) (model.Action, error) {
	row, err := r.tx.Queries(ctx).GetActionByUUID(ctx, id)
	if err != nil {
		return model.Action{}, database.MapError("get action", err)
	}
	return toAction(row), nil
}

func (r *actionRepository) GetByUUIDs(ctx context.Context, ids []uuid.UUID) ([]model.Action, error) {
	rows, err := r.tx.Queries(ctx).GetActionsByUUIDs(ctx, ids)
	if err != nil {
		return nil, database.MapError("get actions", err)
	}
	out := make([]model.Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAction(row))
	}
	return out, nil
}

func (r *actionRepository) List(ctx context.Context, f model.ActionFilter, p model.Paging) ([]model.Action, int64, error) {
	qs := r.tx.Queries(ctx)
	rows, err := qs.ListActions(ctx, sqlc.ListActionsParams{Q: f.Q, Module: f.Module, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list actions", err)
	}
	total, err := qs.CountActions(ctx, sqlc.CountActionsParams{Q: f.Q, Module: f.Module})
	if err != nil {
		return nil, 0, database.MapError("count actions", err)
	}
	return toActions(rows), total, nil
}

func (r *actionRepository) Catalog(ctx context.Context) ([]model.Action, error) {
	rows, err := r.tx.Queries(ctx).ListCatalogActions(ctx)
	if err != nil {
		return nil, database.MapError("action catalog", err)
	}
	return toActions(rows), nil
}

func (r *actionRepository) Create(ctx context.Context, in model.ActionInput) (model.Action, error) {
	row, err := r.tx.Queries(ctx).CreateAction(ctx, sqlc.CreateActionParams{Uuid: newUUID(), ActionCode: &in.ActionCode,
		ActionName: &in.ActionName, Description: in.Description, ResourceLabel: in.ResourceLabel, Actor: &in.Actor})
	if err != nil {
		return model.Action{}, database.MapError("create action", err)
	}
	return toAction(row), nil
}

func (r *actionRepository) Update(ctx context.Context, id uuid.UUID, in model.ActionInput, expected time.Time) (model.Action, error) {
	row, err := r.tx.Queries(ctx).UpdateAction(ctx, sqlc.UpdateActionParams{ActionCode: &in.ActionCode,
		ActionName: &in.ActionName, Description: in.Description, ResourceLabel: in.ResourceLabel, Actor: &in.Actor,
		Uuid: id, ExpectedUpdatedAt: expected})
	if err != nil {
		return model.Action{}, staleIfNoRows("update action", err)
	}
	return toAction(row), nil
}

func (r *actionRepository) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error {
	_, err := r.tx.Queries(ctx).SoftDeleteAction(ctx, sqlc.SoftDeleteActionParams{Uuid: id, Actor: &actor, ExpectedUpdatedAt: expected})
	return staleIfNoRows("delete action", err)
}

func (r *actionRepository) CountRoleUsage(ctx context.Context, actionID int64) (int64, error) {
	n, err := r.tx.Queries(ctx).CountActiveRoleActionsByAction(ctx, &actionID)
	return n, database.MapError("count action usage", err)
}

func (r *actionRepository) SyncSystem(ctx context.Context, p auth.Permission, actor string) error {
	err := r.tx.Queries(ctx).SyncSystemAction(ctx, sqlc.SyncSystemActionParams{Uuid: newUUID(), ActionCode: &p.Code,
		ActionName: &p.Name, Module: &p.Module, ResourceLabel: &p.ResourceLabel, Actor: &actor})
	return database.MapError("sync action", err)
}
