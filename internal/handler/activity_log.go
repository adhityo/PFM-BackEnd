package handler

import (
	"pfm-backend/internal/model"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"

	"github.com/gofiber/fiber/v3"
)

// ActivityLogHandler serves /activity-logs (read only).
type ActivityLogHandler struct {
	logs *service.ActivityLogService
}

func NewActivityLogHandler(logs *service.ActivityLogService) *ActivityLogHandler {
	return &ActivityLogHandler{logs: logs}
}

func (h *ActivityLogHandler) Register(r *auth.Router) {
	g := r.Group("/activity-logs")
	g.Get("", permission.ActivityLogRead, h.List)
	g.Get("/:uuid", permission.ActivityLogRead, h.Get)
}

func (h *ActivityLogHandler) List(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, service.ActivityLogSorts, "-log_time", "username", "log_action", "from", "to")
	if err != nil {
		return err
	}
	f := model.ActivityLogFilter{Username: q.Text("username"), LogAction: q.Text("log_action")}
	if f.From, err = q.Time("from"); err != nil {
		return err
	}
	if f.To, err = q.Time("to"); err != nil {
		return err
	}
	items, total, err := h.logs.List(c.Context(), f, paging(q))
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *ActivityLogHandler) Get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	l, err := h.logs.Get(c.Context(), id)
	if err != nil {
		return err
	}
	return httpx.OK(c, l)
}
