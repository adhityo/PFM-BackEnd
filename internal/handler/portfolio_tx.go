package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// PortfolioTxHandler serves placement details and deposito rollover/withdraw.
type PortfolioTxHandler struct {
	v   httpx.Validator
	svc *service.PortfolioTxService
}

func NewPortfolioTxHandler(v httpx.Validator, svc *service.PortfolioTxService) *PortfolioTxHandler {
	return &PortfolioTxHandler{v: v, svc: svc}
}

func (h *PortfolioTxHandler) Register(r *auth.Router) {
	r.Get("/placements/:uuid/details", permission.PlacementDetailRead, h.GetPlacementDetails)
	r.Put("/placements/:uuid/details", permission.PlacementDetailUpdate, h.PutPlacementDetails)
	r.Post("/deposito-portfolios/:uuid/rollover", permission.DepositoRollover, h.Rollover)
	r.Post("/deposito-portfolios/:uuid/withdraw", permission.DepositoWithdraw, h.Withdraw)
}

func (h *PortfolioTxHandler) GetPlacementDetails(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	p, rows, err := h.svc.PlacementDetails(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, p.UpdatedAt)
	return httpx.OK(c, dto.PlacementDetailsFromRows(p.Uuid, rows))
}

func (h *PortfolioTxHandler) PutPlacementDetails(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.PlacementDetailsRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	p, rows, err := h.svc.ReplacePlacementDetails(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, p.UpdatedAt)
	return httpx.OK(c, dto.PlacementDetailsFromRows(p.Uuid, rows))
}

func (h *PortfolioTxHandler) Rollover(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.DepositoRolloverRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	row, err := h.svc.RolloverDeposito(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, row.UpdatedAt)
	return httpx.Created(c, "/api/v1/deposito-portfolios/"+row.Uuid.String(), dto.DepositoPortfolioFromRow(row))
}

func (h *PortfolioTxHandler) Withdraw(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.DepositoWithdrawRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	row, err := h.svc.WithdrawDeposito(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, row.UpdatedAt)
	return httpx.OK(c, dto.DepositoPortfolioFromRow(row))
}
