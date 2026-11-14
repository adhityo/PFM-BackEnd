package service

import (
	"context"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/database"
)

// Draft trades are freely editable; settled, cancelled and reversed trades
// change only through the settle/cancel/reverse actions.

var errNotDraft = apperr.Conflict("hanya trade draft yang dapat diubah atau dihapus")

func requireDraft(status *int16) error {
	if statusOf(status) != TradeDraft {
		return errNotDraft
	}
	return nil
}

func checkStockTrade(_ context.Context, _ Deps, _ *dto.StockTradeRequest, cur *sqlc.GetStockTradeByUUIDRow) error {
	if cur == nil {
		return nil
	}
	return requireDraft(cur.StockTradeStatus)
}

func checkStockTradeDelete(_ context.Context, _ Deps, cur *sqlc.GetStockTradeByUUIDRow) error {
	return requireDraft(cur.StockTradeStatus)
}

// checkMutualFundTrade needs units or amount, and a NAV (given or the fund's latest).
func checkMutualFundTrade(ctx context.Context, d Deps, req *dto.MutualFundTradeRequest, cur *sqlc.GetMutualFundTradeByUUIDRow) error {
	if cur != nil {
		if err := requireDraft(cur.Status); err != nil {
			return err
		}
	}
	if req.TotalUnit == "" && req.TotalInvestmentValue == "" {
		return apperr.Invalid("total_unit", "required_without", "isi total_unit atau total_investment_value")
	}
	if req.NetAssetValue != "" {
		return nil
	}
	fundID, err := database.RefID(ctx, d.TX, "mutual_fund", req.MutualFundUUID, "mutual_fund_uuid")
	if err != nil {
		return err
	}
	nav, err := d.TX.Queries(ctx).GetMutualFundNAV(ctx, *fundID)
	if err != nil {
		return database.MapError("fund nav", err)
	}
	if nav == nil || !nav.IsPositive() {
		return apperr.Invalid("net_asset_value", "required", "NAV reksa dana belum tersedia; isi net_asset_value")
	}
	return nil
}

func checkMutualFundTradeDelete(_ context.Context, _ Deps, cur *sqlc.GetMutualFundTradeByUUIDRow) error {
	return requireDraft(cur.Status)
}

func cryptoDraft(status *string) error {
	if status != nil && *status != "draft" {
		return errNotDraft
	}
	return nil
}

func checkCryptoTrade(_ context.Context, _ Deps, _ *dto.CryptoTradeRequest, cur *sqlc.GetCryptoTradeByUUIDRow) error {
	if cur == nil {
		return nil
	}
	return cryptoDraft(cur.Status)
}

func checkCryptoTradeDelete(_ context.Context, _ Deps, cur *sqlc.GetCryptoTradeByUUIDRow) error {
	return cryptoDraft(cur.Status)
}
