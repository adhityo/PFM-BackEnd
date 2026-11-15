package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/httpx"
)

// viewFunc returns the current API representation of a record and its ETag time.
type viewFunc func(ctx context.Context, kind string, id uuid.UUID) (any, time.Time, error)

// bodyAction runs one action with the request already bound.
type bodyAction func(c fiber.Ctx, id uuid.UUID, expected time.Time) error

// txActions builds If-Match guarded action endpoints that answer with the
// updated record (bond and pension modules).
type txActions struct {
	v    httpx.Validator
	view viewFunc
}

func (a txActions) action(kind string, run bodyAction) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.UUIDParam(c, "uuid")
		if err != nil {
			return err
		}
		expected, err := httpx.IfMatch(c)
		if err != nil {
			return err
		}
		if err := run(c, id, expected); err != nil {
			return err
		}
		res, updated, err := a.view(c.Context(), kind, id)
		if err != nil {
			return err
		}
		httpx.SetETag(c, updated)
		return httpx.OK(c, res)
	}
}

func (a txActions) noBody(fn actionFunc) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error { return fn(c.Context(), id, expected) }
}

// decision binds an optional {"note": "..."} body (an empty body is allowed).
func (a txActions) decision(fn func(context.Context, uuid.UUID, time.Time, string) error) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error {
		req, err := a.note(c)
		if err != nil {
			return err
		}
		return fn(c.Context(), id, expected, req.Note)
	}
}

func (a txActions) note(c fiber.Ctx) (dto.DecisionRequest, error) {
	var req dto.DecisionRequest
	if len(c.Body()) == 0 {
		return req, nil
	}
	err := httpx.BindJSON(c, a.v, &req)
	return req, err
}

func withBody[Req any](a txActions, fn func(context.Context, uuid.UUID, time.Time, Req) error) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error {
		var req Req
		if err := httpx.BindJSON(c, a.v, &req); err != nil {
			return err
		}
		return fn(c.Context(), id, expected, req)
	}
}
