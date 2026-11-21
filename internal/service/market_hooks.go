package service

import (
	"context"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
)

// Hand-written business rules attached to scaffolded market resources.

// parents returns the distinct, non-nil parent ids of the rows before and
// after a write, so moving a history row to another parent refreshes both.
func parents[R any](id func(*R) *int64, rows ...*R) []*int64 {
	var out []*int64
	seen := map[int64]bool{}
	for _, r := range rows {
		if r == nil {
			continue
		}
		if p := id(r); p != nil && !seen[*p] {
			seen[*p] = true
			out = append(out, p)
		}
	}
	return out
}

func refreshEach(ids []*int64, fn func(*int64) error) error {
	for _, id := range ids {
		if err := fn(id); err != nil {
			return database.MapError("refresh parent", err)
		}
	}
	return nil
}

func refreshStockPrice(ctx context.Context, d Deps, before, after *sqlc.GetStockHistoryByUUIDRow) error {
	ids := parents(func(r *sqlc.GetStockHistoryByUUIDRow) *int64 { return r.StockID }, before, after)
	return refreshEach(ids, func(id *int64) error {
		q := d.TX.Queries(ctx)
		if err := q.RefreshStockLatestPrice(ctx, id); err != nil {
			return err
		}
		return revalueInstrument(ctx, q, dto.AssetStock, *id)
	})
}

func refreshStockBeta(ctx context.Context, d Deps, before, after *sqlc.GetStockBetaHistoryByUUIDRow) error {
	ids := parents(func(r *sqlc.GetStockBetaHistoryByUUIDRow) *int64 { return r.StockID }, before, after)
	return refreshEach(ids, func(id *int64) error { return d.TX.Queries(ctx).RefreshStockBeta(ctx, id) })
}

func refreshStockFundamental(ctx context.Context, d Deps, before, after *sqlc.GetStockFundamentalHistoryByUUIDRow) error {
	ids := parents(func(r *sqlc.GetStockFundamentalHistoryByUUIDRow) *int64 { return r.StockID }, before, after)
	return refreshEach(ids, func(id *int64) error { return d.TX.Queries(ctx).RefreshStockFundamental(ctx, id) })
}

func refreshIndices(ctx context.Context, d Deps, before, after *sqlc.GetIndicesHistoryByUUIDRow) error {
	ids := parents(func(r *sqlc.GetIndicesHistoryByUUIDRow) *int64 { return r.IndicesID }, before, after)
	return refreshEach(ids, func(id *int64) error { return d.TX.Queries(ctx).RefreshIndicesLatest(ctx, id) })
}

func refreshMutualFund(ctx context.Context, d Deps, before, after *sqlc.GetMutualFundHistoryByUUIDRow) error {
	ids := parents(func(r *sqlc.GetMutualFundHistoryByUUIDRow) *int64 { return r.MutualFundID }, before, after)
	return refreshEach(ids, func(id *int64) error {
		q := d.TX.Queries(ctx)
		if err := q.RefreshMutualFundNAV(ctx, id); err != nil {
			return err
		}
		return revalueInstrument(ctx, q, dto.AssetMutualFund, *id)
	})
}

func refreshDepositoRate(ctx context.Context, d Deps, before, after *sqlc.GetDepositoInterestHistoryByUUIDRow) error {
	ids := parents(func(r *sqlc.GetDepositoInterestHistoryByUUIDRow) *int64 { return r.DepositoID }, before, after)
	return refreshEach(ids, func(id *int64) error { return d.TX.Queries(ctx).RefreshDepositoRate(ctx, id) })
}

// actorName is the audit name of the actor in ctx.
func actorName(ctx context.Context) string {
	if a, ok := auth.ActorFrom(ctx); ok {
		return a.AuditName()
	}
	return auth.SystemActor().Username
}

// snapshotDeposito keeps deposito_history in step with every change (PRD §10.5 HIST).
func snapshotDeposito(ctx context.Context, d Deps, before, after *sqlc.GetDepositoByUUIDRow) error {
	action, row := "update", after
	switch {
	case before == nil:
		action = "create"
	case after == nil:
		action, row = "delete", before
	}
	err := d.TX.Queries(ctx).SnapshotDeposito(ctx, sqlc.SnapshotDepositoParams{
		Uuid: crud.NewID(), Action: action, Actor: actorName(ctx), ID: row.ID,
	})
	return database.MapError("snapshot deposito", err)
}

// checkStock ensures the sub-sector belongs to the chosen sector.
func checkStock(ctx context.Context, d Deps, req *dto.StockRequest, _ *sqlc.GetStockByUUIDRow) error {
	if req.SectorUUID == "" || req.SubSectorUUID == "" {
		return nil
	}
	sectorID, err := database.RefID(ctx, d.TX, "sector", req.SectorUUID, "sector_uuid")
	if err != nil {
		return err
	}
	subID, err := database.RefID(ctx, d.TX, "sub_sector", req.SubSectorUUID, "sub_sector_uuid")
	if err != nil {
		return err
	}
	parent, err := d.TX.Queries(ctx).GetSubSectorSectorID(ctx, *subID)
	if err != nil {
		return database.MapError("sub sector", err)
	}
	if parent == nil || *parent != *sectorID {
		return apperr.Invalid("sub_sector_uuid", "belongs_to", "sub sektor tidak berada di bawah sektor yang dipilih")
	}
	return nil
}
