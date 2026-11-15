package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// PensionHandler serves the pension-fund actions: monthly invoices, premium
// payments, benefit payout batches and member unit transactions.
type PensionHandler struct {
	txActions
	svc *service.PensionService
}

func NewPensionHandler(v httpx.Validator, svc *service.PensionService) *PensionHandler {
	return &PensionHandler{txActions: txActions{v: v, view: svc.View}, svc: svc}
}

func (h *PensionHandler) Register(r *auth.Router) {
	inv := r.Group("/invoices")
	inv.Post("/generate", permission.InvoiceGenerate, generate(h, "/invoices/", "invoice", h.svc.GenerateInvoice))
	inv.Post("/:uuid/issue", permission.InvoiceIssue, h.action("invoice", h.noBody(h.svc.IssueInvoice)))
	inv.Post("/:uuid/cancel", permission.InvoiceCancel, h.action("invoice", h.decision(h.svc.CancelInvoice)))
	r.Group("/member-premium-payments").Post("/:uuid/pay", permission.PremiumPay,
		h.action("member_premium_payment", withBody(h.txActions, h.svc.PayPremium)))

	b := r.Group("/benefit-payout-batches")
	b.Post("/generate", permission.PayoutGenerate, generate(h, "/benefit-payout-batches/", "benefit_payout_batch", h.svc.GeneratePayoutBatch))
	b.Post("/:uuid/approve", permission.PayoutApprove, h.action("benefit_payout_batch", h.decision(h.svc.ApprovePayoutBatch)))
	b.Post("/:uuid/pay", permission.PayoutPay, h.action("benefit_payout_batch", withBody(h.txActions, h.svc.PayPayoutBatch)))
	b.Post("/:uuid/cancel", permission.PayoutCancel, h.action("benefit_payout_batch", h.decision(h.svc.CancelPayoutBatch)))

	u := r.Group("/member-portfolio-transactions")
	u.Post("/:uuid/settle", permission.UnitTransactionSettle, h.action("member_portfolio_transaction", h.noBody(h.svc.SettleUnitTransaction)))
	u.Post("/:uuid/cancel", permission.UnitTransactionCancel, h.action("member_portfolio_transaction", h.noBody(h.svc.CancelUnitTransaction)))
}

// generate runs an idempotent period generation: 201 when the document was
// created, 200 when an existing one was returned (and completed if draft).
func generate[Req any](h *PensionHandler, path, kind string, fn func(context.Context, Req) (uuid.UUID, bool, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		var req Req
		if err := httpx.BindJSON(c, h.v, &req); err != nil {
			return err
		}
		id, created, err := fn(c.Context(), req)
		if err != nil {
			return err
		}
		res, updated, err := h.svc.View(c.Context(), kind, id)
		if err != nil {
			return err
		}
		httpx.SetETag(c, updated)
		if created {
			return httpx.Created(c, "/api/v1"+path+id.String(), res)
		}
		return httpx.OK(c, res)
	}
}
