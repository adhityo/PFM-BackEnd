package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// ActionService manages the permission catalog.
type ActionService struct {
	tx      *database.TxManager
	actions repository.ActionRepository
	roles   repository.RoleRepository
	audit   *audit.Recorder
}

func NewActionService(tx *database.TxManager, actions repository.ActionRepository, roles repository.RoleRepository, rec *audit.Recorder) *ActionService {
	return &ActionService{tx: tx, actions: actions, roles: roles, audit: rec}
}

// ActionSorts is the sort whitelist for action lists.
var ActionSorts = []string{"action_code", "-action_code", "-created_at"}

func (s *ActionService) List(ctx context.Context, q *string, p model.Paging) ([]dto.ActionResponse, int64, error) {
	actions, total, err := s.actions.List(ctx, q, p)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.ActionResponse, 0, len(actions))
	for _, a := range actions {
		out = append(out, dto.ActionFromModel(a))
	}
	return out, total, nil
}

func (s *ActionService) Get(ctx context.Context, id uuid.UUID) (model.Action, error) {
	return s.actions.GetByUUID(ctx, id)
}

func (s *ActionService) Create(ctx context.Context, in dto.ActionRequest) (model.Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Action{}, err
	}
	var out model.Action
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		out, err = s.actions.Create(ctx, model.ActionInput{ActionCode: in.ActionCode, ActionName: in.ActionName,
			Description: crud.Text(in.Description), Actor: actor.AuditName()})
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.create", Entity: "action", EntityUUID: out.UUID,
			Description: "code=" + out.ActionCode, Route: "POST /actions"})
	})
	return out, err
}

func (s *ActionService) Update(ctx context.Context, id uuid.UUID, in dto.ActionRequest, expected time.Time) (model.Action, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.Action{}, err
	}
	var out model.Action
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.actions.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		if cur.ActionCode != in.ActionCode {
			n, err := s.actions.CountRoleUsage(ctx, cur.ID)
			if err != nil {
				return err
			}
			if n > 0 {
				return apperr.Conflict("kode action yang sudah dipakai role tidak dapat diubah")
			}
		}
		out, err = s.actions.Update(ctx, id, model.ActionInput{ActionCode: in.ActionCode, ActionName: in.ActionName,
			Description: crud.Text(in.Description), Actor: actor.AuditName()}, expected)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.update", Entity: "action", EntityUUID: id,
			Description: "code=" + out.ActionCode, Route: "PUT /actions/{uuid}"})
	})
	return out, err
}

func (s *ActionService) Delete(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		cur, err := s.actions.GetByUUID(ctx, id)
		if err != nil {
			return err
		}
		n, err := s.actions.CountRoleUsage(ctx, cur.ID)
		if err != nil {
			return err
		}
		if n > 0 {
			return apperr.Conflict("action masih dipakai oleh role")
		}
		if err := s.actions.SoftDelete(ctx, id, actor.AuditName(), expected); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "action.delete", Entity: "action", EntityUUID: id,
			Route: "DELETE /actions/{uuid}"})
	})
}
