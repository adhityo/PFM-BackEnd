package crud

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
)

type service[Row, Req, Resp any] struct {
	res   Resource[Row, Req, Resp]
	tx    *database.TxManager
	audit *audit.Recorder
}

func (s *service[Row, Req, Resp]) list(ctx context.Context, q httpx.ListQuery) ([]Resp, int64, error) {
	rows, total, err := s.res.Store.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Resp, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.res.Response(r))
	}
	return out, total, nil
}

func (s *service[Row, Req, Resp]) get(ctx context.Context, id uuid.UUID) (Row, error) {
	return s.res.Store.Get(ctx, id)
}

func (s *service[Row, Req, Resp]) record(ctx context.Context, action string, id uuid.UUID, route string) error {
	return s.audit.Record(ctx, audit.Entry{Action: s.res.Table + "." + action, Entity: s.res.Table, EntityUUID: id,
		Route: route})
}

func (s *service[Row, Req, Resp]) create(ctx context.Context, req Req) (Row, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		var zero Row
		return zero, err
	}
	var out Row
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if s.res.Check != nil {
			if err := s.res.Check(ctx, &req, nil); err != nil {
				return err
			}
		}
		id, err := s.res.Store.Create(ctx, req, actor.AuditName())
		if err != nil {
			return err
		}
		if out, err = s.res.Store.Get(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, "create", id, "POST "+s.res.Path)
	})
	return out, err
}

func (s *service[Row, Req, Resp]) update(ctx context.Context, id uuid.UUID, req Req, expected time.Time) (Row, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		var zero Row
		return zero, err
	}
	var out Row
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := database.LockForUpdate(ctx, s.tx, s.res.Table, id); err != nil {
			return err
		}
		cur, err := s.res.Store.Get(ctx, id)
		if err != nil {
			return err
		}
		if !s.res.Store.Meta(cur).UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if s.res.Check != nil {
			if err := s.res.Check(ctx, &req, &cur); err != nil {
				return err
			}
		}
		if err := s.res.Store.Update(ctx, id, req, actor.AuditName(), expected); err != nil {
			return err
		}
		if out, err = s.res.Store.Get(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, "update", id, "PUT "+s.res.Path+"/{uuid}")
	})
	return out, err
}

// delete soft-deletes a row that no active row references. The row is locked
// FOR UPDATE while children lock their parent FOR SHARE when created, so a
// child cannot be attached to a row that is being deleted.
func (s *service[Row, Req, Resp]) delete(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		rowID, err := database.LockForUpdate(ctx, s.tx, s.res.Table, id)
		if err != nil {
			return err
		}
		ref, err := database.ActiveReference(ctx, s.tx, s.res.Table, rowID)
		if err != nil {
			return err
		}
		if ref != "" {
			return apperr.Conflict("data masih dipakai oleh " + ref)
		}
		if err := s.res.Store.SoftDelete(ctx, id, actor.AuditName(), expected); err != nil {
			return err
		}
		return s.record(ctx, "delete", id, "DELETE "+s.res.Path+"/{uuid}")
	})
}
