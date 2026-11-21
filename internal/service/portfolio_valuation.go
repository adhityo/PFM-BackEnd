package service

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/repository"
)

// PortfolioValuationService serves the current value of portfolios for the UI
// (summary, holdings, overview list), computed live from portfolio_holding.
// Access follows the portfolio itself: scope is checked through its repository.
type PortfolioValuationService struct {
	tx         *database.TxManager
	portfolios repository.PortfolioRepository
	now        func() time.Time
}

func NewPortfolioValuationService(tx *database.TxManager) *PortfolioValuationService {
	return &PortfolioValuationService{tx: tx, portfolios: repository.NewPortfolioRepository(tx), now: time.Now}
}

// allocations returns the per-asset-class rows grouped by portfolio id.
func (s *PortfolioValuationService) allocations(ctx context.Context, ids []int64) (map[int64][]sqlc.PortfolioAllocationsRow, error) {
	out := make(map[int64][]sqlc.PortfolioAllocationsRow, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.tx.Queries(ctx).PortfolioAllocations(ctx, ids)
	if err != nil {
		return nil, database.MapError("portfolio allocations", err)
	}
	for _, r := range rows {
		out[r.PortfolioID] = append(out[r.PortfolioID], r)
	}
	return out, nil
}

// Summary returns a portfolio with its valuation and allocation per asset class.
func (s *PortfolioValuationService) Summary(ctx context.Context, id uuid.UUID) (dto.PortfolioSummaryResponse, error) {
	p, err := s.portfolios.Get(ctx, id)
	if err != nil {
		return dto.PortfolioSummaryResponse{}, err
	}
	alloc, err := s.allocations(ctx, []int64{p.ID})
	if err != nil {
		return dto.PortfolioSummaryResponse{}, err
	}
	return dto.PortfolioSummary(p, alloc[p.ID], s.now()), nil
}

// Holdings lists the active positions of a portfolio, optionally of one asset class.
func (s *PortfolioValuationService) Holdings(ctx context.Context, id uuid.UUID, assetClass string) ([]dto.PortfolioHoldingResponse, error) {
	var class *string
	if assetClass != "" {
		if !slices.Contains(dto.AssetClasses, assetClass) {
			return nil, apperr.Validation(apperr.FieldError{Field: "asset_class", Rule: "oneof",
				Message: "harus salah satu dari: STOCK, MF-ASSET, CRYPTO, BOND, DEPOSITO"})
		}
		class = &assetClass
	}
	p, err := s.portfolios.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	q := s.tx.Queries(ctx)
	rows, err := q.ListPortfolioHoldings(ctx, sqlc.ListPortfolioHoldingsParams{PortfolioID: &p.ID, AssetClass: class})
	if err != nil {
		return nil, database.MapError("portfolio holdings", err)
	}
	alloc, err := s.allocations(ctx, []int64{p.ID})
	if err != nil {
		return nil, err
	}
	total := decimal.Zero
	for _, a := range alloc[p.ID] {
		total = total.Add(a.MarketValue)
	}
	return dto.PortfolioHoldings(rows, total), nil
}

// Overview lists portfolios (same filters, sorting and scope as GET
// /portfolios) with their valuation.
func (s *PortfolioValuationService) Overview(ctx context.Context, q httpx.ListQuery) ([]dto.PortfolioOverviewResponse, int64, error) {
	rows, total, err := s.portfolios.List(ctx, q)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	alloc, err := s.allocations(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	out := make([]dto.PortfolioOverviewResponse, len(rows))
	for i, r := range rows {
		out[i] = dto.PortfolioOverview(r, alloc[r.ID])
	}
	return out, total, nil
}

// revaluePortfolios stores the current valuation (position market values and
// portfolio.unrealized_profit_loss) of the given portfolios.
func revaluePortfolios(ctx context.Context, q *sqlc.Queries, ids ...int64) error {
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if err := q.RevaluePortfolio(ctx, id); err != nil {
			return database.MapError("revalue portfolio", err)
		}
	}
	return nil
}

// revalueInstrument revalues every portfolio holding the instrument, plus the
// given portfolios (e.g. one whose position was just closed).
func revalueInstrument(ctx context.Context, q *sqlc.Queries, assetClass string, instrumentID int64, also ...int64) error {
	ids, err := q.PortfoliosHoldingInstrument(ctx, sqlc.PortfoliosHoldingInstrumentParams{AssetClass: assetClass, InstrumentID: instrumentID})
	if err != nil {
		return database.MapError("portfolios holding instrument", err)
	}
	return revaluePortfolios(ctx, q, append(ids, also...)...)
}
