package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// Trade status codes (stock_trade.stock_trade_status, mutual_fund_trade.status).
const (
	TradeDraft     int16 = 0
	TradeSettled   int16 = 1
	TradeCancelled int16 = 2
	TradeReversed  int16 = 3
)

// tradeRow is the instrument-independent view of a trade being processed.
type tradeRow struct {
	ID           int64
	UUID         uuid.UUID
	PortfolioID  *int64
	InstrumentID *int64
	TradeTypeID  *int64
	Date         *time.Time
	Price        *decimal.Decimal
	Quantity     *decimal.Decimal
	Value        *decimal.Decimal
	Fee          *decimal.Decimal
	Status       int16
	Code         *string
	UpdatedAt    time.Time
}

// settledTrade is one step of a position replay.
type settledTrade struct {
	ID       int64
	Date     time.Time
	Sell     bool
	Price    decimal.Decimal
	Quantity decimal.Decimal
	Value    decimal.Decimal
	Fee      decimal.Decimal
}

// position is the result of replaying settled trades.
type position struct {
	Quantity  decimal.Decimal
	Cost      decimal.Decimal
	Inception *time.Time
	LastPrice decimal.Decimal
	basis     map[int64]decimal.Decimal // cost basis removed by each sell
}

func (p position) averagePrice() decimal.Decimal {
	if p.Quantity.IsZero() {
		return decimal.Zero
	}
	return p.Cost.Div(p.Quantity).Round(8)
}

// replay rebuilds a position with the average-cost method. Fees are part of
// the cost of a buy; selling more than is held is rejected.
func replay(trades []settledTrade) (position, error) {
	p := position{basis: map[int64]decimal.Decimal{}}
	for _, t := range trades {
		p.LastPrice = t.Price
		if !t.Sell {
			p.Quantity = p.Quantity.Add(t.Quantity)
			p.Cost = p.Cost.Add(t.Value).Add(t.Fee)
			if p.Inception == nil {
				d := t.Date
				p.Inception = &d
			}
			continue
		}
		if t.Quantity.GreaterThan(p.Quantity) {
			return p, apperr.Invalid("quantity", "holding", fmt.Sprintf("jumlah jual %s melebihi kepemilikan %s", t.Quantity, p.Quantity))
		}
		basis := p.Cost.Mul(t.Quantity).Div(p.Quantity).Round(4)
		p.basis[t.ID] = basis
		p.Cost = p.Cost.Sub(basis)
		p.Quantity = p.Quantity.Sub(t.Quantity)
		if p.Quantity.IsZero() {
			p.Cost, p.Inception = decimal.Zero, nil
		}
	}
	return p, nil
}

// instrument adapts one trade table (stock, mutual fund, crypto) to the engine.
type instrument struct {
	table      string // also the permission/audit prefix
	path       string
	assetClass string // asset_class_code used for the account mapping
	visible    func(ctx context.Context, id uuid.UUID) error
	lock       func(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (tradeRow, error)
	setStatus  func(ctx context.Context, q *sqlc.Queries, id int64, status int16, actor string) error
	settled    func(ctx context.Context, q *sqlc.Queries, portfolioID, instrumentID int64) ([]settledTrade, error)
	store      func(ctx context.Context, q *sqlc.Queries, t tradeRow, p position, actor string) error
	link       func(j *Journal, tradeID int64)
	find       func(tradeID int64) sqlc.FindPostedJournalParams
}

// TradeService settles, cancels and reverses trades (PRD §10.5 trading).
type TradeService struct {
	tx       *database.TxManager
	journals *JournalService
	audit    *audit.Recorder
	kinds    map[string]instrument
}

func NewTradeService(tx *database.TxManager, journals *JournalService, rec *audit.Recorder) *TradeService {
	s := &TradeService{tx: tx, journals: journals, audit: rec, kinds: map[string]instrument{}}
	for _, k := range []instrument{stockInstrument(tx), mutualFundInstrument(tx), cryptoInstrument(tx)} {
		s.kinds[k.table] = k
	}
	return s
}

// Kinds lists the trade tables handled by the service.
func (s *TradeService) Kinds() []string {
	return []string{"stock_trade", "mutual_fund_trade", "crypto_trade"}
}

// Path returns the resource path of a trade table.
func (s *TradeService) Path(kind string) string { return s.kinds[kind].path }

func (s *TradeService) begin(ctx context.Context, k instrument, id uuid.UUID, expected time.Time) (tradeRow, string, error) {
	q := s.tx.Queries(ctx)
	t, err := k.lock(ctx, q, id)
	if err != nil {
		return t, "", err
	}
	if err := k.visible(ctx, id); err != nil { // tenant scope
		return t, "", err
	}
	if !t.UpdatedAt.Equal(expected) {
		return t, "", apperr.ErrPreconditionFailed
	}
	if t.PortfolioID == nil || t.InstrumentID == nil || t.TradeTypeID == nil || t.Date == nil ||
		t.Quantity == nil || t.Value == nil || !t.Quantity.IsPositive() {
		return t, "", apperr.Invalid("trade", "incomplete", "data trade belum lengkap (portofolio, instrumen, tipe, tanggal, jumlah, nilai)")
	}
	code, err := q.GetTradeTypeCode(ctx, *t.TradeTypeID)
	if err != nil {
		return t, "", database.MapError("trade type", err)
	}
	if code == nil || (*code != "BUY" && *code != "SELL") {
		return t, "", apperr.Invalid("trade_type_uuid", "oneof", "tipe transaksi harus BUY atau SELL")
	}
	return t, *code, nil
}

func isLast(trades []settledTrade, id int64) bool {
	return len(trades) > 0 && trades[len(trades)-1].ID == id
}

// accounts resolves the journal accounts of a trade.
func (s *TradeService) accounts(ctx context.Context, k instrument, portfolioID int64, side string) (asset, cash, gain, loss int64, err error) {
	q := s.tx.Queries(ctx)
	m, err := q.GetAssetClassAccounts(ctx, sqlc.GetAssetClassAccountsParams{PortfolioID: &portfolioID, AssetClassCode: &k.assetClass})
	missing := apperr.Invalid("portfolio_uuid", "accounting",
		fmt.Sprintf("akun kelas aset %s belum dikonfigurasi pada portofolio (asset_class_finance_account)", k.assetClass))
	if err != nil {
		return 0, 0, 0, 0, orMissing(err, missing)
	}
	assetID := m.BuyAccountID
	if side == "SELL" && m.SellAccountID != nil {
		assetID = m.SellAccountID
	}
	cashID, err := q.GetSettlementAccount(ctx, sqlc.GetSettlementAccountParams{PortfolioID: &portfolioID,
		AssetClassCode: &k.assetClass, TradeTypeCode: &side})
	if err != nil {
		return 0, 0, 0, 0, orMissing(err, apperr.Invalid("portfolio_uuid", "accounting",
			fmt.Sprintf("akun kas %s/%s belum dikonfigurasi (portfolio_accounting_structure)", k.assetClass, side)))
	}
	if assetID == nil || cashID == nil || (side == "SELL" && (m.CapitalGainAccountID == nil || m.CapitalLossAccountID == nil)) {
		return 0, 0, 0, 0, missing
	}
	if side == "SELL" {
		gain, loss = *m.CapitalGainAccountID, *m.CapitalLossAccountID
	}
	return *assetID, *cashID, gain, loss, nil
}

func orMissing(err, missing error) error {
	if e, ok := apperr.As(database.MapError("lookup", err)); ok && e.Kind == apperr.KindNotFound {
		return missing
	}
	return database.MapError("account mapping", err)
}

// journalFor builds the settlement journal and the realized profit/loss.
func (s *TradeService) journalFor(ctx context.Context, k instrument, t tradeRow, side string, basis decimal.Decimal) (Journal, decimal.Decimal, error) {
	asset, cash, gain, loss, err := s.accounts(ctx, k, *t.PortfolioID, side)
	if err != nil {
		return Journal{}, decimal.Zero, err
	}
	value, fee := *t.Value, deref(t.Fee)
	label := ""
	if t.Code != nil {
		label = *t.Code
	}
	j := Journal{Date: *t.Date, PortfolioID: t.PortfolioID, Description: fmt.Sprintf("%s %s %s", side, label, t.UUID)}
	k.link(&j, t.ID)
	if side == "BUY" {
		cost := value.Add(fee)
		j.Lines = []JournalLine{Debit(asset, cost, "pembelian "+label), Credit(cash, cost, "pembayaran")}
		return j, decimal.Zero, nil
	}
	proceeds := value.Sub(fee)
	pl := proceeds.Sub(basis)
	j.Lines = []JournalLine{Debit(cash, proceeds, "hasil penjualan"), Credit(asset, basis, "pelepasan "+label)}
	switch {
	case pl.IsPositive():
		j.Lines = append(j.Lines, Credit(gain, pl, "laba realisasi"))
	case pl.IsNegative():
		j.Lines = append(j.Lines, Debit(loss, pl.Neg(), "rugi realisasi"))
	}
	return j, pl, nil
}

// Settle posts a draft trade: position, history and journal in one transaction.
func (s *TradeService) Settle(ctx context.Context, kind string, id uuid.UUID, expected time.Time) error {
	k := s.kinds[kind]
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		t, side, err := s.begin(ctx, k, id, expected)
		if err != nil {
			return err
		}
		if t.Status != TradeDraft {
			return apperr.Conflict("hanya trade draft yang dapat di-settle")
		}
		actor := actorName(ctx)
		q := s.tx.Queries(ctx)
		if err := k.setStatus(ctx, q, t.ID, TradeSettled, actor); err != nil {
			return err
		}
		trades, err := k.settled(ctx, q, *t.PortfolioID, *t.InstrumentID)
		if err != nil {
			return database.MapError("settled trades", err)
		}
		if !isLast(trades, t.ID) {
			return apperr.Conflict("ada trade ter-settle dengan tanggal setelah trade ini; settle harus berurutan per tanggal")
		}
		pos, err := replay(trades)
		if err != nil {
			return err
		}
		if err := k.store(ctx, q, t, pos, actor); err != nil {
			return err
		}
		j, pl, err := s.journalFor(ctx, k, t, side, pos.basis[t.ID])
		if err != nil {
			return err
		}
		if _, _, err := s.journals.Post(ctx, j); err != nil {
			return err
		}
		if !pl.IsZero() {
			if err := q.AddRealizedProfitLoss(ctx, sqlc.AddRealizedProfitLossParams{Delta: pl, ID: *t.PortfolioID}); err != nil {
				return database.MapError("realized p/l", err)
			}
		}
		// Crypto's last price comes from settled trades, so every holder is revalued.
		if err := revalueInstrument(ctx, q, k.assetClass, *t.InstrumentID, *t.PortfolioID); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: kind + ".settle", Entity: kind, EntityUUID: id,
			Description: side + " nilai=" + t.Value.String(), Route: "POST " + k.path + "/{uuid}/settle"})
	})
}

// Cancel abandons a draft trade.
func (s *TradeService) Cancel(ctx context.Context, kind string, id uuid.UUID, expected time.Time) error {
	k := s.kinds[kind]
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		t, err := k.lock(ctx, q, id)
		if err != nil {
			return err
		}
		if err := k.visible(ctx, id); err != nil {
			return err
		}
		if !t.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if t.Status != TradeDraft {
			return apperr.Conflict("hanya trade draft yang dapat dibatalkan; gunakan reverse untuk trade ter-settle")
		}
		if err := k.setStatus(ctx, q, t.ID, TradeCancelled, actorName(ctx)); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: kind + ".cancel", Entity: kind, EntityUUID: id,
			Route: "POST " + k.path + "/{uuid}/cancel"})
	})
}

// Reverse undoes the latest settled trade of a position: position, history,
// realized P/L and a mirror journal, in one transaction.
func (s *TradeService) Reverse(ctx context.Context, kind string, id uuid.UUID, expected time.Time) error {
	k := s.kinds[kind]
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		t, side, err := s.begin(ctx, k, id, expected)
		if err != nil {
			return err
		}
		if t.Status != TradeSettled {
			return apperr.Conflict("hanya trade ter-settle yang dapat dibalik")
		}
		q := s.tx.Queries(ctx)
		trades, err := k.settled(ctx, q, *t.PortfolioID, *t.InstrumentID)
		if err != nil {
			return database.MapError("settled trades", err)
		}
		if !isLast(trades, t.ID) {
			return apperr.Conflict("hanya trade ter-settle terakhir pada posisi ini yang dapat dibalik")
		}
		before, err := replay(trades)
		if err != nil {
			return err
		}
		after, err := replay(trades[:len(trades)-1])
		if err != nil {
			return err
		}
		actor := actorName(ctx)
		if err := k.setStatus(ctx, q, t.ID, TradeReversed, actor); err != nil {
			return err
		}
		if err := k.store(ctx, q, t, after, actor); err != nil {
			return err
		}
		journalID, err := q.FindPostedJournal(ctx, k.find(t.ID))
		if err != nil {
			return database.MapError("find journal", err)
		}
		if _, err := s.journals.Reverse(ctx, journalID, time.Now().UTC(), "reverse "+kind+" "+t.UUID.String()); err != nil {
			return err
		}
		if side == "SELL" {
			pl := t.Value.Sub(deref(t.Fee)).Sub(before.basis[t.ID])
			if !pl.IsZero() {
				if err := q.AddRealizedProfitLoss(ctx, sqlc.AddRealizedProfitLossParams{Delta: pl.Neg(), ID: *t.PortfolioID}); err != nil {
					return database.MapError("realized p/l", err)
				}
			}
		}
		if err := revalueInstrument(ctx, q, k.assetClass, *t.InstrumentID, *t.PortfolioID); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: kind + ".reverse", Entity: kind, EntityUUID: id,
			Route: "POST " + k.path + "/{uuid}/reverse"})
	})
}

// ---------------------------------------------------------------- adapters

func toSettled[R any](rows []R, conv func(R) settledTrade) []settledTrade {
	out := make([]settledTrade, 0, len(rows))
	for _, r := range rows {
		out = append(out, conv(r))
	}
	return out
}

func settledStep(id int64, date *time.Time, code *string, price, qty, value, fee *decimal.Decimal) settledTrade {
	t := settledTrade{ID: id, Sell: code != nil && *code == "SELL", Price: deref(price), Quantity: deref(qty),
		Value: deref(value), Fee: deref(fee)}
	if date != nil {
		t.Date = *date
	}
	return t
}

func visibleVia[Row any](store interface {
	Get(ctx context.Context, id uuid.UUID) (Row, error)
}) func(ctx context.Context, id uuid.UUID) error {
	return func(ctx context.Context, id uuid.UUID) error {
		_, err := store.Get(ctx, id)
		return err
	}
}

func stockInstrument(tx *database.TxManager) instrument {
	return instrument{
		table: "stock_trade", path: "/stock-trades", assetClass: dto.AssetStock,
		visible: visibleVia[sqlc.GetStockTradeByUUIDRow](repository.NewStockTradeRepository(tx)),
		lock: func(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (tradeRow, error) {
			r, err := q.LockStockTrade(ctx, id)
			if err != nil {
				return tradeRow{}, database.MapError("lock stock trade", err)
			}
			return tradeRow{ID: r.ID, UUID: r.Uuid, PortfolioID: r.PortfolioID, InstrumentID: r.StockID, TradeTypeID: r.TradeTypeID,
				Date: r.StockTradeDate, Price: r.Price, Quantity: r.QuantityShare, Value: r.TradeValue, Fee: r.TransactionFeeAmount,
				Status: statusOf(r.StockTradeStatus), Code: r.StockCode, UpdatedAt: r.UpdatedAt}, nil
		},
		setStatus: func(ctx context.Context, q *sqlc.Queries, id int64, status int16, actor string) error {
			return database.MapError("set status", q.SetStockTradeStatus(ctx, sqlc.SetStockTradeStatusParams{Status: &status, Actor: &actor, ID: id}))
		},
		settled: func(ctx context.Context, q *sqlc.Queries, pf, inst int64) ([]settledTrade, error) {
			rows, err := q.SettledStockTrades(ctx, sqlc.SettledStockTradesParams{PortfolioID: &pf, InstrumentID: &inst})
			return toSettled(rows, func(r sqlc.SettledStockTradesRow) settledTrade {
				return settledStep(r.ID, r.TradeDate, r.TradeTypeCode, r.Price, r.Quantity, r.TradeValue, r.Fee)
			}), err
		},
		store: func(ctx context.Context, q *sqlc.Queries, t tradeRow, p position, actor string) error {
			avg := p.averagePrice()
			posID, err := q.UpsertStockPosition(ctx, sqlc.UpsertStockPositionParams{Uuid: crud.NewID(), PortfolioID: t.PortfolioID,
				InstrumentID: t.InstrumentID, InceptionDate: p.Inception, AveragePrice: &avg, Quantity: &p.Quantity,
				LastPrice: p.LastPrice, Actor: &actor})
			if err != nil {
				return database.MapError("upsert stock position", err)
			}
			return database.MapError("snapshot stock position", q.SnapshotStockPosition(ctx, sqlc.SnapshotStockPositionParams{
				Uuid: crud.NewID(), PositionID: &posID, Code: t.Code, Quantity: &p.Quantity, Price: t.Price, Cost: &p.Cost,
				TradeDate: t.Date, Actor: &actor}))
		},
		link: func(j *Journal, id int64) { j.StockTradeID = &id },
		find: func(id int64) sqlc.FindPostedJournalParams { return sqlc.FindPostedJournalParams{StockTradeID: &id} },
	}
}

func mutualFundInstrument(tx *database.TxManager) instrument {
	return instrument{
		table: "mutual_fund_trade", path: "/mutual-fund-trades", assetClass: dto.AssetMutualFund,
		visible: visibleVia[sqlc.GetMutualFundTradeByUUIDRow](repository.NewMutualFundTradeRepository(tx)),
		lock: func(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (tradeRow, error) {
			r, err := q.LockMutualFundTrade(ctx, id)
			if err != nil {
				return tradeRow{}, database.MapError("lock mutual fund trade", err)
			}
			return tradeRow{ID: r.ID, UUID: r.Uuid, PortfolioID: r.PortfolioID, InstrumentID: r.MutualFundID, TradeTypeID: r.TradeTypeID,
				Date: r.TransactionDate, Price: r.NetAssetValue, Quantity: r.TotalUnit, Value: r.TotalInvestmentValue,
				Fee: r.TransactionFee, Status: statusOf(r.Status), Code: r.MutualFundName, UpdatedAt: r.UpdatedAt}, nil
		},
		setStatus: func(ctx context.Context, q *sqlc.Queries, id int64, status int16, actor string) error {
			return database.MapError("set status", q.SetMutualFundTradeStatus(ctx, sqlc.SetMutualFundTradeStatusParams{Status: &status, Actor: &actor, ID: id}))
		},
		settled: func(ctx context.Context, q *sqlc.Queries, pf, inst int64) ([]settledTrade, error) {
			rows, err := q.SettledMutualFundTrades(ctx, sqlc.SettledMutualFundTradesParams{PortfolioID: &pf, InstrumentID: &inst})
			return toSettled(rows, func(r sqlc.SettledMutualFundTradesRow) settledTrade {
				return settledStep(r.ID, r.TradeDate, r.TradeTypeCode, r.Price, r.Quantity, r.TradeValue, r.Fee)
			}), err
		},
		store: func(ctx context.Context, q *sqlc.Queries, t tradeRow, p position, actor string) error {
			avg := p.averagePrice()
			posID, err := q.UpsertMutualFundPosition(ctx, sqlc.UpsertMutualFundPositionParams{Uuid: crud.NewID(), PortfolioID: t.PortfolioID,
				InstrumentID: t.InstrumentID, InceptionDate: p.Inception, AveragePrice: &avg, Quantity: &p.Quantity,
				LastPrice: &p.LastPrice, Cost: &p.Cost, Actor: &actor})
			if err != nil {
				return database.MapError("upsert mutual fund position", err)
			}
			return database.MapError("snapshot mutual fund position", q.SnapshotMutualFundPosition(ctx, sqlc.SnapshotMutualFundPositionParams{
				Uuid: crud.NewID(), PositionID: &posID, Price: t.Price, TradeDate: t.Date, Quantity: &p.Quantity, Cost: &p.Cost, Actor: &actor}))
		},
		link: func(j *Journal, id int64) { j.MutualFundTradeID = &id },
		find: func(id int64) sqlc.FindPostedJournalParams {
			return sqlc.FindPostedJournalParams{MutualFundTradeID: &id}
		},
	}
}

// Crypto status is stored as text in the legacy schema.
var cryptoStatus = []string{"draft", "settled", "cancelled", "reversed"}

func cryptoInstrument(tx *database.TxManager) instrument {
	return instrument{
		table: "crypto_trade", path: "/crypto-trades", assetClass: dto.AssetCrypto,
		visible: visibleVia[sqlc.GetCryptoTradeByUUIDRow](repository.NewCryptoTradeRepository(tx)),
		lock: func(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (tradeRow, error) {
			r, err := q.LockCryptoTrade(ctx, id)
			if err != nil {
				return tradeRow{}, database.MapError("lock crypto trade", err)
			}
			status := int16(-1)
			for i, n := range cryptoStatus {
				if r.Status != nil && *r.Status == n {
					status = int16(i)
				}
			}
			return tradeRow{ID: r.ID, UUID: r.Uuid, PortfolioID: r.PortfolioID, InstrumentID: r.CryptoID, TradeTypeID: r.TradeTypeID,
				Date: r.TradeDate, Price: r.Price, Quantity: r.Quantity, Value: r.TradeValue, Fee: r.TransactionFeeAmount,
				Status: status, Code: r.CryptoCode, UpdatedAt: r.UpdatedAt}, nil
		},
		setStatus: func(ctx context.Context, q *sqlc.Queries, id int64, status int16, actor string) error {
			name := cryptoStatus[status]
			return database.MapError("set status", q.SetCryptoTradeStatus(ctx, sqlc.SetCryptoTradeStatusParams{Status: &name, Actor: &actor, ID: id}))
		},
		settled: func(ctx context.Context, q *sqlc.Queries, pf, inst int64) ([]settledTrade, error) {
			rows, err := q.SettledCryptoTrades(ctx, sqlc.SettledCryptoTradesParams{PortfolioID: &pf, InstrumentID: &inst})
			return toSettled(rows, func(r sqlc.SettledCryptoTradesRow) settledTrade {
				return settledStep(r.ID, r.TradeDate, r.TradeTypeCode, r.Price, r.Quantity, r.TradeValue, r.Fee)
			}), err
		},
		store: func(ctx context.Context, q *sqlc.Queries, t tradeRow, p position, actor string) error {
			avg := p.averagePrice()
			_, err := q.UpsertCryptoPosition(ctx, sqlc.UpsertCryptoPositionParams{Uuid: crud.NewID(), PortfolioID: t.PortfolioID,
				InstrumentID: t.InstrumentID, AveragePrice: &avg, Quantity: &p.Quantity, Cost: &p.Cost, Actor: &actor})
			return database.MapError("upsert crypto position", err)
		},
		link: func(j *Journal, id int64) { j.CryptoTradeID = &id },
		find: func(id int64) sqlc.FindPostedJournalParams { return sqlc.FindPostedJournalParams{CryptoTradeID: &id} },
	}
}

func statusOf(s *int16) int16 {
	if s == nil {
		return TradeDraft
	}
	return *s
}

// View returns the current API representation of a trade and its updated_at.
func (s *TradeService) View(ctx context.Context, kind string, id uuid.UUID) (any, time.Time, error) {
	switch kind {
	case "stock_trade":
		r, err := repository.NewStockTradeRepository(s.tx).Get(ctx, id)
		return dto.StockTradeFromRow(r), r.UpdatedAt, err
	case "mutual_fund_trade":
		r, err := repository.NewMutualFundTradeRepository(s.tx).Get(ctx, id)
		return dto.MutualFundTradeFromRow(r), r.UpdatedAt, err
	default:
		r, err := repository.NewCryptoTradeRepository(s.tx).Get(ctx, id)
		return dto.CryptoTradeFromRow(r), r.UpdatedAt, err
	}
}
