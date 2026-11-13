package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/database"
)

// RoleRepository persists role and role_action.
type RoleRepository interface {
	GetByUUID(ctx context.Context, id uuid.UUID) (model.Role, error)
	GetByUUIDForUpdate(ctx context.Context, id uuid.UUID) (model.Role, error)
	List(ctx context.Context, q *string, p model.Paging) ([]model.Role, int64, error)
	Create(ctx context.Context, in model.RoleInput) (model.Role, error)
	Update(ctx context.Context, id uuid.UUID, in model.RoleInput, expected time.Time) (model.Role, error)
	SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error
	Actions(ctx context.Context, roleID int64) ([]model.Action, error)
	ReplaceActions(ctx context.Context, role model.Role, actions []model.Action, actor string) error
	PermissionCodes(ctx context.Context, roleID int64) ([]string, error)
	UpsertByCode(ctx context.Context, code, name, actor string) (model.Role, error)
}

type roleRepository struct{ tx *database.TxManager }

func NewRoleRepository(tx *database.TxManager) RoleRepository { return &roleRepository{tx: tx} }

func toRole(r sqlc.Role) model.Role {
	return model.Role{ID: r.ID, UUID: r.Uuid, RoleCode: deref(r.RoleCode), RoleName: deref(r.RoleName),
		Description: r.Description, MenuURL: r.MenuUrl, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (r *roleRepository) GetByUUID(ctx context.Context, id uuid.UUID) (model.Role, error) {
	row, err := r.tx.Queries(ctx).GetRoleByUUID(ctx, id)
	if err != nil {
		return model.Role{}, database.MapError("get role", err)
	}
	return toRole(row), nil
}

func (r *roleRepository) GetByUUIDForUpdate(ctx context.Context, id uuid.UUID) (model.Role, error) {
	row, err := r.tx.Queries(ctx).GetRoleByUUIDForUpdate(ctx, id)
	if err != nil {
		return model.Role{}, database.MapError("lock role", err)
	}
	return toRole(row), nil
}

func (r *roleRepository) List(ctx context.Context, q *string, p model.Paging) ([]model.Role, int64, error) {
	qs := r.tx.Queries(ctx)
	rows, err := qs.ListRoles(ctx, sqlc.ListRolesParams{Q: q, Sort: p.Sort, Limit: p.Limit, Offset: p.Offset})
	if err != nil {
		return nil, 0, database.MapError("list roles", err)
	}
	total, err := qs.CountRoles(ctx, q)
	if err != nil {
		return nil, 0, database.MapError("count roles", err)
	}
	out := make([]model.Role, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRole(row))
	}
	return out, total, nil
}

func (r *roleRepository) Create(ctx context.Context, in model.RoleInput) (model.Role, error) {
	row, err := r.tx.Queries(ctx).CreateRole(ctx, sqlc.CreateRoleParams{Uuid: newUUID(), RoleCode: &in.RoleCode,
		RoleName: &in.RoleName, Description: in.Description, MenuUrl: in.MenuURL, Status: &in.Status, Actor: &in.Actor})
	if err != nil {
		return model.Role{}, database.MapError("create role", err)
	}
	return toRole(row), nil
}

func (r *roleRepository) Update(ctx context.Context, id uuid.UUID, in model.RoleInput, expected time.Time) (model.Role, error) {
	row, err := r.tx.Queries(ctx).UpdateRole(ctx, sqlc.UpdateRoleParams{RoleCode: &in.RoleCode, RoleName: &in.RoleName,
		Description: in.Description, MenuUrl: in.MenuURL, Status: &in.Status, Actor: &in.Actor, Uuid: id, ExpectedUpdatedAt: expected})
	if err != nil {
		return model.Role{}, staleIfNoRows("update role", err)
	}
	return toRole(row), nil
}

func (r *roleRepository) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error {
	_, err := r.tx.Queries(ctx).SoftDeleteRole(ctx, sqlc.SoftDeleteRoleParams{Uuid: id, Actor: &actor, ExpectedUpdatedAt: expected})
	return staleIfNoRows("delete role", err)
}

func (r *roleRepository) Actions(ctx context.Context, roleID int64) ([]model.Action, error) {
	rows, err := r.tx.Queries(ctx).ListActionsByRole(ctx, &roleID)
	if err != nil {
		return nil, database.MapError("list role actions", err)
	}
	out := make([]model.Action, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAction(row))
	}
	return out, nil
}

// ReplaceActions must run inside a transaction (the caller owns it).
func (r *roleRepository) ReplaceActions(ctx context.Context, role model.Role, actions []model.Action, actor string) error {
	q := r.tx.Queries(ctx)
	if err := q.SoftDeleteRoleActionsByRole(ctx, sqlc.SoftDeleteRoleActionsByRoleParams{RoleID: &role.ID, Actor: &actor}); err != nil {
		return database.MapError("clear role actions", err)
	}
	for _, a := range actions {
		err := q.InsertRoleAction(ctx, sqlc.InsertRoleActionParams{Uuid: newUUID(), RoleID: &role.ID, ActionID: ptr(a.ID),
			RoleName: &role.RoleName, ActionName: &a.ActionName, Actor: &actor})
		if err != nil {
			return database.MapError("insert role action", err)
		}
	}
	return database.MapError("touch role", q.TouchRole(ctx, sqlc.TouchRoleParams{ID: role.ID, Actor: &actor}))
}

func (r *roleRepository) PermissionCodes(ctx context.Context, roleID int64) ([]string, error) {
	codes, err := r.tx.Queries(ctx).ListPermissionCodesByRole(ctx, &roleID)
	return codes, database.MapError("permission codes", err)
}

func (r *roleRepository) UpsertByCode(ctx context.Context, code, name, actor string) (model.Role, error) {
	row, err := r.tx.Queries(ctx).UpsertRoleByCode(ctx, sqlc.UpsertRoleByCodeParams{Uuid: newUUID(), RoleCode: &code,
		RoleName: &name, Actor: &actor})
	if err != nil {
		return model.Role{}, database.MapError("upsert role", err)
	}
	return toRole(row), nil
}
