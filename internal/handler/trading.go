package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

type actionFunc func(ctx context.Context, id uuid.UUID, expected time.Time) error

func parseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, apperr.ErrNotFound
	}
	return id, nil
}

// TradeHandler serves settle/cancel/reverse for every trade table. CRUD of
// draft trades is the generic resource of the same path.
type TradeHandler struct {
	svc *service.TradeService
}

func NewTradeHandler(svc *service.TradeService) *TradeHandler { return &TradeHandler{svc: svc} }

func (h *TradeHandler) Register(r *auth.Router) {
	for _, kind := range h.svc.Kinds() {
		g := r.Group(h.svc.Path(kind))
		for _, a := range permission.TradeActions {
			g.Post("/:uuid/"+a, kind+":"+a, h.action(kind, a))
		}
	}
}

func (h *TradeHandler) action(kind, name string) fiber.Handler {
	run := map[string]func(context.Context, string, uuid.UUID, time.Time) error{
		"settle": h.svc.Settle, "cancel": h.svc.Cancel, "reverse": h.svc.Reverse,
	}[name]
	return func(c fiber.Ctx) error {
		id, err := httpx.UUIDParam(c, "uuid")
		if err != nil {
			return err
		}
		expected, err := httpx.IfMatch(c)
		if err != nil {
			return err
		}
		if err := run(c.Context(), kind, id, expected); err != nil {
			return err
		}
		res, updated, err := h.svc.View(c.Context(), kind, id)
		if err != nil {
			return err
		}
		httpx.SetETag(c, updated)
		return httpx.OK(c, res)
	}
}
