package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// PortfolioSetupHandler creates a portfolio with its account mapping in one transaction.
type PortfolioSetupHandler struct {
	v   httpx.Validator
	svc *service.PortfolioSetupService
}

func NewPortfolioSetupHandler(v httpx.Validator, svc *service.PortfolioSetupService) *PortfolioSetupHandler {
	return &PortfolioSetupHandler{v: v, svc: svc}
}

func (h *PortfolioSetupHandler) Register(r *auth.Router) {
	r.Post("/portfolios/setup", permission.PortfolioCreate, h.Setup)
}

func (h *PortfolioSetupHandler) Setup(c fiber.Ctx) error {
	var req dto.PortfolioSetupRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	out, updatedAt, err := h.svc.Setup(c.Context(), req)
	if err != nil {
		return err
	}
	httpx.SetETag(c, updatedAt)
	return httpx.Created(c, "/api/v1/portfolios/"+out.Portfolio.UUID.String(), out)
}
