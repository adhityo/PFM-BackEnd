package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// PortfolioValuationHandler serves portfolio valuation views for the UI.
type PortfolioValuationHandler struct {
	svc       *service.PortfolioValuationService
	parseList func(fiber.Ctx) (httpx.ListQuery, error)
}

// NewPortfolioValuationHandler takes parseList, the list parameter parser of
// GET /portfolios: the overview accepts the same sorts and filters.
func NewPortfolioValuationHandler(svc *service.PortfolioValuationService,
	parseList func(fiber.Ctx) (httpx.ListQuery, error)) *PortfolioValuationHandler {
	return &PortfolioValuationHandler{svc: svc, parseList: parseList}
}

func (h *PortfolioValuationHandler) Register(r *auth.Router) {
	r.Get("/portfolio-overviews", permission.PortfolioRead, h.Overview)
	r.Get("/portfolios/:uuid/summary", permission.PortfolioRead, h.Summary)
	r.Get("/portfolios/:uuid/holdings", permission.PortfolioRead, h.Holdings)
}

func (h *PortfolioValuationHandler) Overview(c fiber.Ctx) error {
	q, err := h.parseList(c)
	if err != nil {
		return err
	}
	items, total, err := h.svc.Overview(c.Context(), q)
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *PortfolioValuationHandler) Summary(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	out, err := h.svc.Summary(c.Context(), id)
	if err != nil {
		return err
	}
	return httpx.OK(c, out)
}

func (h *PortfolioValuationHandler) Holdings(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	for k := range c.Queries() {
		if k != "asset_class" {
			return apperr.Validation(apperr.FieldError{Field: k, Rule: "unknown", Message: "parameter tidak dikenal"})
		}
	}
	out, err := h.svc.Holdings(c.Context(), id, c.Query("asset_class"))
	if err != nil {
		return err
	}
	return httpx.OK(c, out)
}
