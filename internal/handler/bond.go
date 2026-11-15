package handler

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// BondHandler serves the bond-module actions: bond lifecycle, subscription
// approval, coupon and agent fee payments, and data change requests.
type BondHandler struct {
	v       httpx.Validator
	svc     *service.BondTxService
	changes *service.ChangeRequestService
}

func NewBondHandler(v httpx.Validator, svc *service.BondTxService, changes *service.ChangeRequestService) *BondHandler {
	return &BondHandler{v: v, svc: svc, changes: changes}
}

func (h *BondHandler) Register(r *auth.Router) {
	b := r.Group("/bonds")
	b.Post("/:uuid/offer", permission.BondOffer, h.action("bond", h.status(service.BondOffering)))
	b.Post("/:uuid/activate", permission.BondActivate, h.action("bond", h.status(service.BondActive)))
	b.Post("/:uuid/generate-coupon-schedule", permission.BondGenerateCouponSchedule, h.action("bond", h.noBody(h.svc.GenerateCouponSchedule)))

	t := r.Group("/bond-trades")
	t.Post("/:uuid/approve", permission.BondTradeApprove, h.action("bond_trade", h.decision(h.svc.ApproveTrade)))
	t.Post("/:uuid/cancel", permission.BondTradeCancel, h.action("bond_trade", h.decision(h.svc.CancelTrade)))

	r.Group("/bond-coupon-schedules").Post("/:uuid/pay", permission.BondCouponSchedulePay,
		h.action("bond_coupon_schedule", withBody(h, h.svc.PaySchedule)))
	r.Group("/investor-coupon-schedules").Post("/:uuid/pay", permission.InvestorCouponSchedulePay,
		h.action("investor_coupon_schedule", withBody(h, h.svc.PayInvestorCoupon)))
	r.Group("/agent-fee-transactions").Post("/:uuid/pay", permission.AgentFeePay,
		h.action("agent_fee_transaction", withBody(h, h.svc.PayAgentFee)))

	r.Group("/investors").Post("/:uuid/change-requests", permission.InvestorChangeCreate,
		changeRequest(h, "/investor-data-histories", "investor_data_history", h.changes.CreateInvestor))
	r.Group("/marketing-agents").Post("/:uuid/change-requests", permission.MarketingAgentChangeCreate,
		changeRequest(h, "/marketing-agent-data-histories", "marketing_agent_data_history", h.changes.CreateAgent))
	ih := r.Group("/investor-data-histories")
	ih.Post("/:uuid/approve", permission.InvestorChangeApprove, h.decide("investor_data_history", h.changes.DecideInvestor, true))
	ih.Post("/:uuid/reject", permission.InvestorChangeReject, h.decide("investor_data_history", h.changes.DecideInvestor, false))
	ah := r.Group("/marketing-agent-data-histories")
	ah.Post("/:uuid/approve", permission.MarketingAgentChangeApprove, h.decide("marketing_agent_data_history", h.changes.DecideAgent, true))
	ah.Post("/:uuid/reject", permission.MarketingAgentChangeReject, h.decide("marketing_agent_data_history", h.changes.DecideAgent, false))
}

// bodyAction runs one action with the request already bound.
type bodyAction func(c fiber.Ctx, id uuid.UUID, expected time.Time) error

// action wraps an If-Match guarded action and answers with the updated record.
func (h *BondHandler) action(kind string, run bodyAction) fiber.Handler {
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
		res, updated, err := h.svc.View(c.Context(), kind, id)
		if err != nil {
			return err
		}
		httpx.SetETag(c, updated)
		return httpx.OK(c, res)
	}
}

func (h *BondHandler) noBody(fn actionFunc) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error { return fn(c.Context(), id, expected) }
}

func (h *BondHandler) status(target int16) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error {
		return h.svc.SetStatus(c.Context(), id, expected, target)
	}
}

// decision binds an optional {"note": "..."} body (an empty body is allowed).
func (h *BondHandler) decision(fn func(context.Context, uuid.UUID, time.Time, string) error) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error {
		req, err := h.note(c)
		if err != nil {
			return err
		}
		return fn(c.Context(), id, expected, req.Note)
	}
}

func (h *BondHandler) note(c fiber.Ctx) (dto.DecisionRequest, error) {
	var req dto.DecisionRequest
	if len(c.Body()) == 0 {
		return req, nil
	}
	err := httpx.BindJSON(c, h.v, &req)
	return req, err
}

func withBody[Req any](h *BondHandler, fn func(context.Context, uuid.UUID, time.Time, Req) error) bodyAction {
	return func(c fiber.Ctx, id uuid.UUID, expected time.Time) error {
		var req Req
		if err := httpx.BindJSON(c, h.v, &req); err != nil {
			return err
		}
		return fn(c.Context(), id, expected, req)
	}
}

// changeRequest files a change request and answers 201 with it.
func changeRequest[Req any](h *BondHandler, path, table string, fn func(context.Context, uuid.UUID, Req) (uuid.UUID, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		target, err := httpx.UUIDParam(c, "uuid")
		if err != nil {
			return err
		}
		var req Req
		if err := httpx.BindJSON(c, h.v, &req); err != nil {
			return err
		}
		id, err := fn(c.Context(), target, req)
		if err != nil {
			return err
		}
		res, err := h.changes.View(c.Context(), table, id)
		if err != nil {
			return err
		}
		return httpx.Created(c, "/api/v1"+path+"/"+id.String(), res)
	}
}

// decide approves or rejects a change request. Requests are immutable, so
// no If-Match is needed; the pending-status check prevents double decisions.
func (h *BondHandler) decide(table string, fn func(context.Context, uuid.UUID, bool, string) error, approve bool) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.UUIDParam(c, "uuid")
		if err != nil {
			return err
		}
		req, err := h.note(c)
		if err != nil {
			return err
		}
		if err := fn(c.Context(), id, approve, req.Note); err != nil {
			return err
		}
		res, err := h.changes.View(c.Context(), table, id)
		if err != nil {
			return err
		}
		return httpx.OK(c, res)
	}
}
