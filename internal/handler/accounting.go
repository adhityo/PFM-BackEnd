package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// AccountingHandler serves /finance-transactions and cash settlement actions.
type AccountingHandler struct {
	v   httpx.Validator
	svc *service.AccountingService
}

func NewAccountingHandler(v httpx.Validator, svc *service.AccountingService) *AccountingHandler {
	return &AccountingHandler{v: v, svc: svc}
}

func (h *AccountingHandler) Register(r *auth.Router) {
	g := r.Group("/finance-transactions")
	g.Get("", permission.FinanceTransactionRead, h.ListJournals)
	g.Post("", permission.FinanceTransactionCreate, h.CreateJournal)
	g.Get("/:uuid", permission.FinanceTransactionRead, h.GetJournal)
	g.Post("/:uuid/reverse", permission.FinanceTransactionReverse, h.ReverseJournal)

	cs := r.Group("/cash-settlements")
	cs.Post("/:uuid/approve", permission.CashSettlementApprove, h.settlementAction(h.svc.ApproveSettlement))
	cs.Post("/:uuid/cancel", permission.CashSettlementCancel, h.settlementAction(h.svc.CancelSettlement))
	cs.Post("/:uuid/reverse", permission.CashSettlementReverse, h.settlementAction(h.svc.ReverseSettlement))
}

func (h *AccountingHandler) ListJournals(c fiber.Ctx) error {
	q, err := httpx.ParseListQuery(c, service.JournalSorts, "-transaction_date", "q", "portfolio_uuid", "from", "to")
	if err != nil {
		return err
	}
	rows, total, err := h.svc.ListJournals(c.Context(), q)
	if err != nil {
		return err
	}
	items := make([]dto.JournalSummaryResponse, 0, len(rows))
	for _, r := range rows {
		items = append(items, dto.JournalSummaryFromRow(r))
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *AccountingHandler) writeJournal(c fiber.Ctx, status int, id string) error {
	uid, err := httpx.UUIDParam(c, "uuid")
	if id != "" {
		uid, err = parseUUID(id)
	}
	if err != nil {
		return err
	}
	head, lines, err := h.svc.GetJournal(c.Context(), uid)
	if err != nil {
		return err
	}
	httpx.SetETag(c, head.UpdatedAt)
	if status == fiber.StatusCreated {
		return httpx.Created(c, "/api/v1/finance-transactions/"+head.Uuid.String(), dto.JournalFromRows(head, lines))
	}
	return httpx.OK(c, dto.JournalFromRows(head, lines))
}

func (h *AccountingHandler) GetJournal(c fiber.Ctx) error {
	return h.writeJournal(c, fiber.StatusOK, "")
}

func (h *AccountingHandler) CreateJournal(c fiber.Ctx) error {
	var req dto.JournalRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	id, err := h.svc.CreateJournal(c.Context(), req)
	if err != nil {
		return err
	}
	return h.writeJournal(c, fiber.StatusCreated, id.String())
}

func (h *AccountingHandler) ReverseJournal(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	rev, err := h.svc.ReverseJournal(c.Context(), id, expected)
	if err != nil {
		return err
	}
	return h.writeJournal(c, fiber.StatusCreated, rev.String())
}

func (h *AccountingHandler) settlementAction(action actionFunc) fiber.Handler {
	return func(c fiber.Ctx) error {
		id, err := httpx.UUIDParam(c, "uuid")
		if err != nil {
			return err
		}
		expected, err := httpx.IfMatch(c)
		if err != nil {
			return err
		}
		if err := action(c.Context(), id, expected); err != nil {
			return err
		}
		res, updated, err := h.svc.View(c.Context(), id)
		if err != nil {
			return err
		}
		httpx.SetETag(c, updated)
		return httpx.OK(c, res)
	}
}
