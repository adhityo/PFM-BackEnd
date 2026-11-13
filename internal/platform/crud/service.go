package crud

import (
	"context"
	"errors"
	"strconv"
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

func listRows[Row, Resp any](ctx context.Context, store Reader[Row], resp func(Row) Resp, q httpx.ListQuery) ([]Resp, int64, error) {
	rows, total, err := store.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	out := make([]Resp, 0, len(rows))
	for _, r := range rows {
		out = append(out, resp(r))
	}
	return out, total, nil
}

func (s *service[Row, Req, Resp]) list(ctx context.Context, q httpx.ListQuery) ([]Resp, int64, error) {
	return listRows(ctx, s.res.Store, s.res.Response, q)
}

func (s *service[Row, Req, Resp]) get(ctx context.Context, id uuid.UUID) (Row, error) {
	return s.res.Store.Get(ctx, id)
}

func (s *service[Row, Req, Resp]) record(ctx context.Context, action string, id uuid.UUID, desc, route string) error {
	return s.audit.Record(ctx, audit.Entry{Action: s.res.Table + "." + action, Entity: s.res.Table, EntityUUID: id,
		Description: desc, Route: route})
}

var errOutOfScope = apperr.Forbidden("data berada di luar scope client Anda")

// visible re-reads a written row through the scoped store. A scoped actor
// must be able to see what it wrote; otherwise the write is rolled back.
func (s *service[Row, Req, Resp]) visible(ctx context.Context, id uuid.UUID) (Row, error) {
	row, err := s.res.Store.Get(ctx, id)
	if errors.Is(err, apperr.ErrNotFound) {
		return row, errOutOfScope
	}
	return row, err
}

// insert runs the create pipeline inside an existing transaction.
func (s *service[Row, Req, Resp]) insert(ctx context.Context, req Req, actor string) (Row, error) {
	var zero Row
	if s.res.Check != nil {
		if err := s.res.Check(ctx, &req, nil); err != nil {
			return zero, err
		}
	}
	id, err := s.res.Store.Create(ctx, req, actor)
	if err != nil {
		return zero, err
	}
	row, err := s.visible(ctx, id)
	if err != nil {
		return zero, err
	}
	if s.res.AfterWrite != nil {
		if err := s.res.AfterWrite(ctx, nil, &row); err != nil {
			return zero, err
		}
	}
	return row, nil
}

func (s *service[Row, Req, Resp]) create(ctx context.Context, req Req) (Row, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		var zero Row
		return zero, err
	}
	var out Row
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		if out, err = s.insert(ctx, req, actor.AuditName()); err != nil {
			return err
		}
		return s.record(ctx, "create", s.res.Store.Meta(out).UUID, "", "POST "+s.res.Path)
	})
	return out, err
}

// importRows creates every row in one transaction (all-or-nothing).
func (s *service[Row, Req, Resp]) importRows(ctx context.Context, reqs []Req) (int, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return 0, err
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		for i, req := range reqs {
			if _, err := s.insert(ctx, req, actor.AuditName()); err != nil {
				return rowError(i, err)
			}
		}
		return s.record(ctx, "import", uuid.Nil, "jumlah baris="+strconv.Itoa(len(reqs)), "POST "+s.res.Path+"/import")
	})
	if err != nil {
		return 0, err
	}
	return len(reqs), nil
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
		cur, err := s.res.Store.Get(ctx, id) // scoped: 404 outside the actor's client
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
		if out, err = s.visible(ctx, id); err != nil {
			return err
		}
		if s.res.AfterWrite != nil {
			if err := s.res.AfterWrite(ctx, &cur, &out); err != nil {
				return err
			}
		}
		return s.record(ctx, "update", id, "", "PUT "+s.res.Path+"/{uuid}")
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
		cur, err := s.res.Store.Get(ctx, id) // scoped
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
		if s.res.AfterWrite != nil {
			if err := s.res.AfterWrite(ctx, &cur, nil); err != nil {
				return err
			}
		}
		return s.record(ctx, "delete", id, "", "DELETE "+s.res.Path+"/{uuid}")
	})
}
