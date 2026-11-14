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
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/repository"
)

// AccountingService serves manual journals and cash settlement actions.
type AccountingService struct {
	tx          *database.TxManager
	journals    *JournalService
	audit       *audit.Recorder
	portfolios  repository.PortfolioRepository
	settlements repository.CashSettlementRepository
}

func NewAccountingService(tx *database.TxManager, journals *JournalService, rec *audit.Recorder) *AccountingService {
	return &AccountingService{tx: tx, journals: journals, audit: rec,
		portfolios: repository.NewPortfolioRepository(tx), settlements: repository.NewCashSettlementRepository(tx)}
}

// JournalSorts is the sort whitelist for journal lists.
var JournalSorts = []string{"transaction_date", "-transaction_date", "transaction_number", "-transaction_number"}

// ---------------------------------------------------------------- manual journals

// CreateJournal posts a manual journal. Scoped (client) users must link one
// of their portfolios.
func (s *AccountingService) CreateJournal(ctx context.Context, req dto.JournalRequest) (uuid.UUID, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		j := Journal{Date: *crud.Date(req.TransactionDate), Description: req.Description}
		if req.PortfolioUUID != "" {
			pid, err := uuid.Parse(req.PortfolioUUID)
			if err != nil {
				return apperr.Invalid("portfolio_uuid", "uuid", "harus berupa UUID yang valid")
			}
			p, err := s.portfolios.Get(ctx, pid) // scoped
			if err != nil {
				return apperr.Invalid("portfolio_uuid", "exists", "portofolio tidak ditemukan")
			}
			j.PortfolioID = &p.ID
		} else if actor.ScopeClientID() != nil {
			return apperr.Invalid("portfolio_uuid", "required", "wajib diisi untuk pengguna client")
		}
		ids := make([]uuid.UUID, len(req.Lines))
		for i, l := range req.Lines {
			if ids[i], err = uuid.Parse(l.FinanceAccountUUID); err != nil {
				return apperr.Invalid(fmt.Sprintf("lines[%d].finance_account_uuid", i), "uuid", "harus berupa UUID yang valid")
			}
		}
		rows, err := s.tx.Queries(ctx).ResolveAccounts(ctx, ids)
		if err != nil {
			return database.MapError("resolve accounts", err)
		}
		accounts := make(map[uuid.UUID]int64, len(rows))
		for _, r := range rows {
			accounts[r.Uuid] = r.ID
		}
		for i, l := range req.Lines {
			acc, ok := accounts[ids[i]]
			if !ok {
				return apperr.Invalid(fmt.Sprintf("lines[%d].finance_account_uuid", i), "exists", "akun tidak ditemukan")
			}
			j.Lines = append(j.Lines, JournalLine{AccountID: acc, Debit: amount(l.Debit), Credit: amount(l.Credit), Description: l.Description})
		}
		if _, id, err = s.journals.Post(ctx, j); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "finance_transaction.create", Entity: "finance_transaction",
			EntityUUID: id, Route: "POST /finance-transactions"})
	})
	return id, err
}

func amount(s string) decimal.Decimal {
	if d := crud.Decimal(s); d != nil {
		return *d
	}
	return decimal.Zero
}

// GetJournal returns a journal with its lines (scoped by portfolio client).
func (s *AccountingService) GetJournal(ctx context.Context, id uuid.UUID) (sqlc.GetJournalRow, []sqlc.ListJournalLinesRow, error) {
	q := s.tx.Queries(ctx)
	h, err := q.GetJournal(ctx, sqlc.GetJournalParams{Uuid: id, ScopeClientID: crud.ScopeClientID(ctx)})
	if err != nil {
		return h, nil, database.MapError("get journal", err)
	}
	lines, err := q.ListJournalLines(ctx, &h.ID)
	return h, lines, database.MapError("journal lines", err)
}

// ListJournals lists journals within the actor's scope.
func (s *AccountingService) ListJournals(ctx context.Context, q httpx.ListQuery) ([]sqlc.ListJournalsRow, int64, error) {
	pf, err := database.FilterRefID(ctx, s.tx, "portfolio", "portfolio_uuid", q.Text("portfolio_uuid"))
	if err != nil {
		return nil, 0, err
	}
	from, to := crud.Date(deref2(q.Text("from"))), crud.Date(deref2(q.Text("to")))
	if (q.Text("from") != nil && from == nil) || (q.Text("to") != nil && to == nil) {
		return nil, 0, apperr.Invalid("from", "datetime", "format tanggal harus YYYY-MM-DD")
	}
	qs := s.tx.Queries(ctx)
	scope := crud.ScopeClientID(ctx)
	rows, err := qs.ListJournals(ctx, sqlc.ListJournalsParams{ScopeClientID: scope, PortfolioID: pf, From: from, To: to,
		Q: q.Like("q"), Sort: q.Sort, Limit: q.Page.Limit(), Offset: q.Page.Offset()})
	if err != nil {
		return nil, 0, database.MapError("list journals", err)
	}
	total, err := qs.CountJournals(ctx, sqlc.CountJournalsParams{ScopeClientID: scope, PortfolioID: pf, From: from, To: to, Q: q.Like("q")})
	return rows, total, database.MapError("count journals", err)
}

func deref2(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ReverseJournal reverses a manual journal. Journals created by trades or
// settlements are reversed through their source document.
func (s *AccountingService) ReverseJournal(ctx context.Context, id uuid.UUID, expected time.Time) (uuid.UUID, error) {
	var out uuid.UUID
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		h, _, err := s.GetJournal(ctx, id)
		if err != nil {
			return err
		}
		if !h.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if h.StockTradeID != nil || h.MutualFundTradeID != nil || h.CryptoTradeID != nil || h.CashSettlementID != nil {
			return apperr.Conflict("jurnal ini berasal dari dokumen sumber; balik melalui dokumen sumbernya")
		}
		if h.ReversalOfID != nil {
			return apperr.Conflict("jurnal pembalik tidak dapat dibalik lagi")
		}
		if out, err = s.journals.Reverse(ctx, h.ID, time.Now().UTC(), deref2(h.TransactionNumber)); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "finance_transaction.reverse", Entity: "finance_transaction",
			EntityUUID: id, Description: "pembalik=" + out.String(), Route: "POST /finance-transactions/{uuid}/reverse"})
	})
	return out, err
}

// ---------------------------------------------------------------- cash settlements

func (s *AccountingService) lockSettlement(ctx context.Context, id uuid.UUID, expected time.Time) (sqlc.LockCashSettlementRow, error) {
	row, err := s.tx.Queries(ctx).LockCashSettlement(ctx, id)
	if err != nil {
		return row, database.MapError("lock cash settlement", err)
	}
	if _, err := s.settlements.Get(ctx, id); err != nil { // tenant scope
		return row, err
	}
	if !row.UpdatedAt.Equal(expected) {
		return row, apperr.ErrPreconditionFailed
	}
	return row, nil
}

func (s *AccountingService) setSettlementStatus(ctx context.Context, id int64, status int16) error {
	actor := actorName(ctx)
	return database.MapError("settlement status", s.tx.Queries(ctx).SetCashSettlementStatus(ctx,
		sqlc.SetCashSettlementStatusParams{Status: &status, Actor: &actor, ID: id}))
}

// ApproveSettlement (maker-checker) posts the settlement journal:
// inbound  Dr cash (value - fee), Dr fee (fee), Cr counter (value);
// outbound Dr counter (value), Dr fee (fee), Cr cash (value + fee).
func (s *AccountingService) ApproveSettlement(ctx context.Context, id uuid.UUID, expected time.Time) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		row, err := s.lockSettlement(ctx, id, expected)
		if err != nil {
			return err
		}
		if statusOf(row.CashSettlementStatus) != SettlementDraft {
			return apperr.Conflict("hanya cash settlement draft yang dapat disetujui")
		}
		if row.CreatedBy != nil && *row.CreatedBy == actor.AuditName() {
			return apperr.Forbidden("pembuat cash settlement tidak boleh menyetujuinya (maker-checker)")
		}
		if row.CashSettlementCategoryID == nil || row.CashSettlementValue == nil {
			return apperr.Invalid("cash_settlement_category_uuid", "required", "kategori dan nilai wajib diisi")
		}
		rules, err := s.tx.Queries(ctx).GetCashSettlementCategoryRules(ctx, *row.CashSettlementCategoryID)
		if err != nil {
			return database.MapError("settlement category", err)
		}
		value := *row.CashSettlementValue
		fee := decimal.Zero
		if rules.SettlementFeePercentage != nil {
			fee = value.Mul(*rules.SettlementFeePercentage).Div(decimal.NewFromInt(100)).Round(4)
		}
		missing := apperr.Invalid("cash_settlement_category_uuid", "accounting", "akun kategori cash settlement belum lengkap")
		if rules.CashSettlementAccountID == nil || rules.CashSettlementCounterAccountID == nil ||
			(fee.IsPositive() && rules.SettlementFeeAccountID == nil) {
			return missing
		}
		cash, counter := *rules.CashSettlementAccountID, *rules.CashSettlementCounterAccountID
		j := Journal{Date: time.Now().UTC(), Description: "Cash settlement " + deref2(row.CashSettlementSubject),
			PortfolioID: firstID(row.StockPositionPortfolioID, row.BondPositionPortfolioID, row.DepositoPositionPortfolioID,
				row.FundPositionPortfolioID), CashSettlementID: &row.ID}
		switch {
		case rules.InboundSettlement:
			j.Lines = []JournalLine{Debit(cash, value.Sub(fee), "penerimaan"), Credit(counter, value, "lawan")}
		case rules.OutboundSettlement:
			j.Lines = []JournalLine{Debit(counter, value, "lawan"), Credit(cash, value.Add(fee), "pembayaran")}
		default:
			return apperr.Invalid("cash_settlement_category_uuid", "xor", "kategori harus inbound atau outbound")
		}
		if fee.IsPositive() {
			j.Lines = append(j.Lines, Debit(*rules.SettlementFeeAccountID, fee, "biaya settlement"))
		}
		if _, _, err := s.journals.Post(ctx, j); err != nil {
			return err
		}
		if err := s.setSettlementStatus(ctx, row.ID, SettlementApproved); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "cash_settlement.approve", Entity: "cash_settlement", EntityUUID: id,
			Description: "nilai=" + value.String(), Route: "POST /cash-settlements/{uuid}/approve"})
	})
}

func firstID(ids ...*int64) *int64 {
	for _, id := range ids {
		if id != nil {
			return id
		}
	}
	return nil
}

// CancelSettlement abandons a draft.
func (s *AccountingService) CancelSettlement(ctx context.Context, id uuid.UUID, expected time.Time) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		row, err := s.lockSettlement(ctx, id, expected)
		if err != nil {
			return err
		}
		if statusOf(row.CashSettlementStatus) != SettlementDraft {
			return apperr.Conflict("hanya cash settlement draft yang dapat dibatalkan")
		}
		if err := s.setSettlementStatus(ctx, row.ID, SettlementCancelled); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "cash_settlement.cancel", Entity: "cash_settlement", EntityUUID: id,
			Route: "POST /cash-settlements/{uuid}/cancel"})
	})
}

// ReverseSettlement reverses the journal of an approved settlement.
func (s *AccountingService) ReverseSettlement(ctx context.Context, id uuid.UUID, expected time.Time) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		row, err := s.lockSettlement(ctx, id, expected)
		if err != nil {
			return err
		}
		if statusOf(row.CashSettlementStatus) != SettlementApproved {
			return apperr.Conflict("hanya cash settlement yang sudah disetujui yang dapat dibalik")
		}
		jid, err := s.tx.Queries(ctx).FindPostedJournal(ctx, sqlc.FindPostedJournalParams{CashSettlementID: &row.ID})
		if err != nil {
			return database.MapError("find journal", err)
		}
		if _, err := s.journals.Reverse(ctx, jid, time.Now().UTC(), "cash settlement "+id.String()); err != nil {
			return err
		}
		if err := s.setSettlementStatus(ctx, row.ID, SettlementReversed); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "cash_settlement.reverse", Entity: "cash_settlement", EntityUUID: id,
			Route: "POST /cash-settlements/{uuid}/reverse"})
	})
}

// View returns the current representation of a cash settlement.
func (s *AccountingService) View(ctx context.Context, id uuid.UUID) (dto.CashSettlementResponse, time.Time, error) {
	r, err := s.settlements.Get(ctx, id)
	if err != nil {
		return dto.CashSettlementResponse{}, time.Time{}, err
	}
	return dto.CashSettlementFromRow(r), r.UpdatedAt, nil
}
