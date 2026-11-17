package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
)

// Journal status codes (finance_transaction.transaction_status).
const (
	JournalPosted   int16 = 1
	JournalReversed int16 = 3
)

// JournalLine is one debit or credit of a journal.
type JournalLine struct {
	AccountID   int64
	Debit       decimal.Decimal
	Credit      decimal.Decimal
	Description string
}

// Journal is a balanced double-entry posting.
type Journal struct {
	Date              time.Time
	Description       string
	PortfolioID       *int64
	StockTradeID      *int64
	MutualFundTradeID *int64
	CryptoTradeID     *int64
	CashSettlementID  *int64
	ReversalOfID      *int64
	Lines             []JournalLine
}

// Debit and Credit build journal lines.
func Debit(account int64, amount decimal.Decimal, desc string) JournalLine {
	return JournalLine{AccountID: account, Debit: amount, Description: desc}
}

func Credit(account int64, amount decimal.Decimal, desc string) JournalLine {
	return JournalLine{AccountID: account, Credit: amount, Description: desc}
}

// JournalService is the single entry point for posting to the ledger (PRD §9.4).
type JournalService struct {
	tx *database.TxManager
}

func NewJournalService(tx *database.TxManager) *JournalService { return &JournalService{tx: tx} }

// validate enforces: at least two lines, each line exactly one positive side,
// and total debit == total credit > 0. Zero lines are dropped first.
func (j *Journal) validate() (decimal.Decimal, error) {
	lines := j.Lines[:0:0]
	for _, l := range j.Lines {
		if l.Debit.IsZero() && l.Credit.IsZero() {
			continue
		}
		lines = append(lines, l)
	}
	j.Lines = lines
	if len(lines) < 2 {
		return decimal.Zero, apperr.Invalid("lines", "min", "jurnal minimal memiliki 2 baris")
	}
	var dr, cr decimal.Decimal
	for i, l := range lines {
		field := fmt.Sprintf("lines[%d]", i)
		if l.Debit.IsNegative() || l.Credit.IsNegative() {
			return decimal.Zero, apperr.Invalid(field, "dgte", "nilai debit/kredit tidak boleh negatif")
		}
		if l.Debit.IsPositive() && l.Credit.IsPositive() {
			return decimal.Zero, apperr.Invalid(field, "one_side", "satu baris hanya boleh debit atau kredit")
		}
		dr, cr = dr.Add(l.Debit), cr.Add(l.Credit)
	}
	if !dr.Equal(cr) {
		return decimal.Zero, apperr.Invalid("lines", "balanced", fmt.Sprintf("jurnal tidak seimbang: debit %s, kredit %s", dr, cr))
	}
	return dr, nil
}

// Post writes a balanced journal; it must run inside the caller's transaction
// so the ledger and the source document commit together.
func (s *JournalService) Post(ctx context.Context, j Journal) (int64, uuid.UUID, error) {
	if !s.tx.InTx(ctx) {
		return 0, uuid.Nil, fmt.Errorf("journal.Post must run inside a transaction")
	}
	total, err := j.validate()
	if err != nil {
		return 0, uuid.Nil, err
	}
	q := s.tx.Queries(ctx)
	n, err := q.NextNumber(ctx, sqlc.NextNumberParams{Uuid: crud.NewID(), Name: "JV", Period: j.Date.Format("2006")})
	if err != nil {
		return 0, uuid.Nil, database.MapError("journal number", err)
	}
	number := fmt.Sprintf("JV-%s-%06d", j.Date.Format("2006"), n)
	actor := actorName(ctx)
	date := j.Date
	row, err := q.InsertJournal(ctx, sqlc.InsertJournalParams{Uuid: crud.NewID(), TransactionDate: &date,
		TransactionNumber: &number, TransactionValue: &total, Description: crud.Text(j.Description),
		PortfolioID: j.PortfolioID, StockTradeID: j.StockTradeID, MutualFundTradeID: j.MutualFundTradeID,
		CryptoTradeID: j.CryptoTradeID, CashSettlementID: j.CashSettlementID, ReversalOfID: j.ReversalOfID, Actor: &actor})
	if err != nil {
		return 0, uuid.Nil, database.MapError("insert journal", err)
	}
	for _, l := range j.Lines {
		debit, credit := l.Debit, l.Credit
		if err := q.InsertJournalLine(ctx, sqlc.InsertJournalLineParams{Uuid: crud.NewID(), FinanceTransactionID: &row.ID,
			FinanceAccountID: &l.AccountID, Debit: &debit, Credit: &credit, Description: crud.Text(l.Description),
			Actor: &actor}); err != nil {
			return 0, uuid.Nil, database.MapError("insert journal line", err)
		}
	}
	return row.ID, row.Uuid, nil
}

// Reverse posts the mirror image of a posted journal dated on date, links it
// to the original and marks the original reversed.
func (s *JournalService) Reverse(ctx context.Context, journalID int64, date time.Time, reason string) (uuid.UUID, error) {
	q := s.tx.Queries(ctx)
	lock, err := q.LockJournal(ctx, journalID)
	if err != nil {
		return uuid.Nil, database.MapError("lock journal", err)
	}
	if lock.TransactionStatus == nil || *lock.TransactionStatus != JournalPosted {
		return uuid.Nil, apperr.Conflict("jurnal sudah dibalik")
	}
	lines, err := q.ListJournalLines(ctx, &journalID)
	if err != nil {
		return uuid.Nil, database.MapError("journal lines", err)
	}
	mirror := Journal{Date: date, Description: strings.TrimSpace("Pembalikan: " + reason), ReversalOfID: &journalID}
	for _, l := range lines {
		mirror.Lines = append(mirror.Lines, JournalLine{AccountID: *l.FinanceAccountID, Debit: deref(l.Credit),
			Credit: deref(l.Debit), Description: "pembalikan"})
	}
	// Carry the source links so the reversal is found next to the original.
	orig, err := q.GetJournalLinks(ctx, journalID)
	if err != nil {
		return uuid.Nil, database.MapError("journal links", err)
	}
	mirror.PortfolioID, mirror.StockTradeID, mirror.MutualFundTradeID = orig.PortfolioID, orig.StockTradeID, orig.MutualFundTradeID
	mirror.CryptoTradeID, mirror.CashSettlementID = orig.CryptoTradeID, orig.CashSettlementID
	_, id, err := s.Post(ctx, mirror)
	if err != nil {
		return uuid.Nil, err
	}
	actor := actorName(ctx)
	if err := q.SetJournalStatus(ctx, sqlc.SetJournalStatusParams{Status: ptr(JournalReversed), Actor: &actor, ID: journalID}); err != nil {
		return uuid.Nil, database.MapError("mark reversed", err)
	}
	return id, nil
}

func deref(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}
