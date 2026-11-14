package service

import (
	"context"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
)

// Settlement status codes (cash_settlement.cash_settlement_status).
const (
	SettlementDraft     int16 = 0
	SettlementApproved  int16 = 1
	SettlementCancelled int16 = 2
	SettlementReversed  int16 = 3
)

func checkFinanceAccount(ctx context.Context, d Deps, req *dto.FinanceAccountRequest, cur *sqlc.GetFinanceAccountByUUIDRow) error {
	var self *int64
	if cur != nil {
		self = &cur.ID
	}
	return checkTreeParent(ctx, d, "finance_account", "parent_account_id", "parent_account_uuid", req.ParentAccountUUID, self)
}

// checkCashSettlementCategory: a category is either inbound or outbound.
func checkCashSettlementCategory(_ context.Context, _ Deps, req *dto.CashSettlementCategoryRequest, _ *sqlc.GetCashSettlementCategoryByUUIDRow) error {
	if req.InboundSettlement == req.OutboundSettlement {
		return apperr.Invalid("inbound_settlement", "xor", "pilih tepat satu: inbound_settlement atau outbound_settlement")
	}
	return nil
}

// checkCashSettlement: drafts only, and at most one position link.
func checkCashSettlement(_ context.Context, _ Deps, req *dto.CashSettlementRequest, cur *sqlc.GetCashSettlementByUUIDRow) error {
	if cur != nil && statusOf(cur.CashSettlementStatus) != SettlementDraft {
		return apperr.Conflict("hanya cash settlement draft yang dapat diubah")
	}
	n := 0
	for _, v := range []string{req.StockPortfolioUUID, req.BondPortfolioUUID, req.DepositoPortfolioUUID, req.MutualFundPortfolioUUID} {
		if v != "" {
			n++
		}
	}
	if n > 1 {
		return apperr.Invalid("stock_portfolio_uuid", "one_of", "maksimal satu posisi yang ditautkan")
	}
	return nil
}

func checkCashSettlementDelete(_ context.Context, _ Deps, cur *sqlc.GetCashSettlementByUUIDRow) error {
	if statusOf(cur.CashSettlementStatus) != SettlementDraft {
		return apperr.Conflict("hanya cash settlement draft yang dapat dihapus")
	}
	return nil
}
