package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/httpx"
)

// Asset classes of portfolio_holding (asset_class.asset_class_code), in display order.
const (
	AssetStock      = "STOCK"
	AssetMutualFund = "MF-ASSET"
	AssetCrypto     = "CRYPTO"
	AssetBond       = "BOND"
	AssetDeposito   = "DEPOSITO"
)

var AssetClasses = []string{AssetStock, AssetMutualFund, AssetCrypto, AssetBond, AssetDeposito}

var assetClassLabels = map[string]string{AssetStock: "Saham", AssetMutualFund: "Reksa Dana", AssetCrypto: "Kripto",
	AssetBond: "Obligasi", AssetDeposito: "Deposito"}

// positionPaths are the read endpoints of each position (relative to /api/v1).
var positionPaths = map[string]string{AssetStock: "/stock-portfolios/", AssetMutualFund: "/mutual-fund-portfolios/",
	AssetCrypto: "/crypto-portfolios/", AssetBond: "/bond-portfolios/", AssetDeposito: "/deposito-portfolios/"}

var hundred = decimal.NewFromInt(100)

// pct is part/whole in percent (4 decimals), nil when whole is zero.
func pct(part, whole decimal.Decimal) *decimal.Decimal {
	if whole.IsZero() {
		return nil
	}
	v := part.Div(whole).Mul(hundred).Round(4)
	return &v
}

func orZero(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}

// PortfolioValuation is the current value of a portfolio. Market value uses the
// latest instrument price (average price when none); deposito counts at
// principal and adds no unrealized P/L.
type PortfolioValuation struct {
	HoldingCount         int              `json:"holding_count"`
	CostValue            decimal.Decimal  `json:"cost_value"`
	MarketValue          decimal.Decimal  `json:"market_value"`
	UnrealizedProfitLoss decimal.Decimal  `json:"unrealized_profit_loss"`
	UnrealizedReturnPct  *decimal.Decimal `json:"unrealized_return_pct"`
	RealizedProfitLoss   decimal.Decimal  `json:"realized_profit_loss"`
	TotalProfitLoss      decimal.Decimal  `json:"total_profit_loss"`
	CashFundValue        *decimal.Decimal `json:"cash_fund_value"`
}

// AssetAllocation is one asset class of a portfolio; weight_pct is its share of
// the portfolio market value.
type AssetAllocation struct {
	AssetClass           string           `json:"asset_class"`
	AssetClassLabel      string           `json:"asset_class_label"`
	HoldingCount         int              `json:"holding_count"`
	CostValue            decimal.Decimal  `json:"cost_value"`
	MarketValue          decimal.Decimal  `json:"market_value"`
	UnrealizedProfitLoss decimal.Decimal  `json:"unrealized_profit_loss"`
	UnrealizedReturnPct  *decimal.Decimal `json:"unrealized_return_pct"`
	WeightPct            decimal.Decimal  `json:"weight_pct"`
}

// PortfolioSummaryResponse is GET /portfolios/{uuid}/summary.
type PortfolioSummaryResponse struct {
	Portfolio  PortfolioResponse  `json:"portfolio"`
	Valuation  PortfolioValuation `json:"valuation"`
	Allocation []AssetAllocation  `json:"allocation"`
	ValuedAt   time.Time          `json:"valued_at"`
}

// PortfolioOverviewResponse is one row of GET /portfolio-overviews: the
// portfolio fields plus its valuation and allocation.
type PortfolioOverviewResponse struct {
	PortfolioResponse
	Valuation  PortfolioValuation `json:"valuation"`
	Allocation []AssetAllocation  `json:"allocation"`
}

// PortfolioHoldingResponse is one active position of GET /portfolios/{uuid}/holdings.
type PortfolioHoldingResponse struct {
	UUID                 uuid.UUID        `json:"uuid"`
	PositionPath         string           `json:"position_path"`
	AssetClass           string           `json:"asset_class"`
	AssetClassLabel      string           `json:"asset_class_label"`
	InstrumentUUID       *uuid.UUID       `json:"instrument_uuid"`
	InstrumentCode       *string          `json:"instrument_code"`
	InstrumentName       *string          `json:"instrument_name"`
	Quantity             *decimal.Decimal `json:"quantity"`
	AveragePrice         *decimal.Decimal `json:"average_price"`
	LastPrice            *decimal.Decimal `json:"last_price"`
	PriceDate            *httpx.Date      `json:"price_date"`
	CostValue            decimal.Decimal  `json:"cost_value"`
	MarketValue          decimal.Decimal  `json:"market_value"`
	UnrealizedProfitLoss decimal.Decimal  `json:"unrealized_profit_loss"`
	UnrealizedReturnPct  *decimal.Decimal `json:"unrealized_return_pct"`
	WeightPct            decimal.Decimal  `json:"weight_pct"`
	InceptionDate        *httpx.Date      `json:"inception_date"`
}

// unrealized is market - cost; deposito is valued at principal.
func unrealized(class string, cost, market decimal.Decimal) (decimal.Decimal, *decimal.Decimal) {
	if class == AssetDeposito {
		return decimal.Zero, nil
	}
	pl := market.Sub(cost)
	return pl, pct(pl, cost)
}

// Valuate builds the valuation and allocation of a portfolio from its
// per-asset-class rows (sqlc PortfolioAllocations).
func Valuate(p sqlc.GetPortfolioByUUIDRow, rows []sqlc.PortfolioAllocationsRow) (PortfolioValuation, []AssetAllocation) {
	v := PortfolioValuation{RealizedProfitLoss: orZero(p.RealizedProfitLoss), CashFundValue: p.CashFundValue}
	var plCost decimal.Decimal // cost of the holdings that carry P/L (not deposito)
	for _, r := range rows {
		v.HoldingCount += int(r.Holdings)
		v.CostValue = v.CostValue.Add(r.CostValue)
		v.MarketValue = v.MarketValue.Add(r.MarketValue)
		if r.AssetClass != AssetDeposito {
			plCost = plCost.Add(r.CostValue)
			v.UnrealizedProfitLoss = v.UnrealizedProfitLoss.Add(r.MarketValue.Sub(r.CostValue))
		}
	}
	v.UnrealizedReturnPct = pct(v.UnrealizedProfitLoss, plCost)
	v.TotalProfitLoss = v.UnrealizedProfitLoss.Add(v.RealizedProfitLoss)

	alloc := make([]AssetAllocation, 0, len(rows))
	for _, class := range AssetClasses {
		for _, r := range rows {
			if r.AssetClass != class {
				continue
			}
			a := AssetAllocation{AssetClass: class, AssetClassLabel: assetClassLabels[class], HoldingCount: int(r.Holdings),
				CostValue: r.CostValue, MarketValue: r.MarketValue, WeightPct: decimal.Zero}
			a.UnrealizedProfitLoss, a.UnrealizedReturnPct = unrealized(class, r.CostValue, r.MarketValue)
			if w := pct(r.MarketValue, v.MarketValue); w != nil {
				a.WeightPct = *w
			}
			alloc = append(alloc, a)
		}
	}
	return v, alloc
}

// PortfolioSummary is the response of GET /portfolios/{uuid}/summary.
func PortfolioSummary(p sqlc.GetPortfolioByUUIDRow, rows []sqlc.PortfolioAllocationsRow, at time.Time) PortfolioSummaryResponse {
	v, alloc := Valuate(p, rows)
	return PortfolioSummaryResponse{Portfolio: PortfolioFromRow(p), Valuation: v, Allocation: alloc, ValuedAt: at.UTC()}
}

// PortfolioOverview is one row of GET /portfolio-overviews.
func PortfolioOverview(p sqlc.GetPortfolioByUUIDRow, rows []sqlc.PortfolioAllocationsRow) PortfolioOverviewResponse {
	v, alloc := Valuate(p, rows)
	return PortfolioOverviewResponse{PortfolioResponse: PortfolioFromRow(p), Valuation: v, Allocation: alloc}
}

// PortfolioHoldings converts holding rows; weights are relative to total, the
// market value of the whole portfolio.
func PortfolioHoldings(rows []sqlc.ListPortfolioHoldingsRow, total decimal.Decimal) []PortfolioHoldingResponse {
	out := make([]PortfolioHoldingResponse, len(rows))
	for i, r := range rows {
		cost, market := orZero(r.CostValue), orZero(r.MarketValue)
		h := PortfolioHoldingResponse{UUID: r.Uuid, PositionPath: positionPaths[r.AssetClass] + r.Uuid.String(),
			AssetClass: r.AssetClass, AssetClassLabel: assetClassLabels[r.AssetClass],
			InstrumentUUID: r.InstrumentUuid, InstrumentCode: r.InstrumentCode, InstrumentName: r.InstrumentName,
			Quantity: r.Quantity, AveragePrice: r.AveragePrice, LastPrice: r.LastPrice, PriceDate: httpx.DateOf(r.PriceDate),
			CostValue: cost, MarketValue: market, WeightPct: decimal.Zero, InceptionDate: httpx.DateOf(r.InceptionDate)}
		h.UnrealizedProfitLoss, h.UnrealizedReturnPct = unrealized(r.AssetClass, cost, market)
		if w := pct(market, total); w != nil {
			h.WeightPct = *w
		}
		out[i] = h
	}
	return out
}
