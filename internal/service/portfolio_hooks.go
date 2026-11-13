package service

import (
	"context"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
)

// Hand-written business rules attached to scaffolded portfolio resources.

// checkPortfolio: the parent portfolio must not create a cycle and must
// belong to the same client.
func checkPortfolio(ctx context.Context, d Deps, req *dto.PortfolioRequest, cur *sqlc.GetPortfolioByUUIDRow) error {
	var self *int64
	if cur != nil {
		self = &cur.ID
	}
	if err := checkTreeParent(ctx, d, "portfolio", "parent_portfolio_id", "parent_portfolio_uuid", req.ParentPortfolioUUID, self); err != nil {
		return err
	}
	parentID, err := database.RefID(ctx, d.TX, "portfolio", req.ParentPortfolioUUID, "parent_portfolio_uuid")
	if err != nil || parentID == nil {
		return err
	}
	clientID, err := database.RefID(ctx, d.TX, "client", req.ClientUUID, "client_uuid")
	if err != nil {
		return err
	}
	parentClient, err := d.TX.Queries(ctx).GetPortfolioClientID(ctx, *parentID)
	if err != nil {
		return database.MapError("parent portfolio", err)
	}
	if !sameID(parentClient, clientID) {
		return apperr.Invalid("parent_portfolio_uuid", "same_client", "induk portofolio harus milik client yang sama")
	}
	return nil
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// checkDateRange compares YYYY-MM-DD strings (already validated).
func checkDateRange(start, end, field string) error {
	if start != "" && end != "" && end < start {
		return apperr.Invalid(field, "gtefield", "tanggal akhir tidak boleh sebelum tanggal mulai")
	}
	return nil
}

func checkPlacement(_ context.Context, _ Deps, req *dto.PlacementRequest, _ *sqlc.GetPlacementByUUIDRow) error {
	return checkDateRange(req.StartDate, req.EndDate, "end_date")
}

// checkDepositoPortfolio: a rolled-over placement is closed history.
func checkDepositoPortfolio(_ context.Context, _ Deps, req *dto.DepositoPortfolioRequest, cur *sqlc.GetDepositoPortfolioByUUIDRow) error {
	if cur != nil && cur.NextPortfolioID != nil {
		return apperr.Conflict("penempatan deposito sudah di-rollover dan tidak dapat diubah")
	}
	return checkDateRange(req.PortfolioStartDate, req.PortfolioEndDate, "portfolio_end_date")
}

// snapshotDepositoPortfolio appends to deposito_portfolio_history on every change.
func snapshotDepositoPortfolio(ctx context.Context, d Deps, before, after *sqlc.GetDepositoPortfolioByUUIDRow) error {
	row := after
	if row == nil {
		row = before
	}
	err := d.TX.Queries(ctx).SnapshotDepositoPortfolio(ctx, sqlc.SnapshotDepositoPortfolioParams{
		Uuid: crud.NewID(), Actor: actorName(ctx), ID: row.ID,
	})
	return database.MapError("snapshot deposito portfolio", err)
}
