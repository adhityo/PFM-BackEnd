package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

// PensionService implements the pension fund transactions of PRD §10.5:
// monthly premium invoices, benefit payout batches and member unit
// subscriptions/redemptions.
type PensionService struct {
	d        Deps
	tx       *database.TxManager
	audit    *audit.Recorder
	clients  repository.ClientRepository
	invoices repository.InvoiceRepository
	premiums repository.MemberPremiumPaymentRepository
	batches  repository.BenefitPayoutBatchRepository
	unitTxs  repository.MemberPortfolioTransactionRepository
}

func NewPensionService(d Deps, rec *audit.Recorder) *PensionService {
	return &PensionService{d: d, tx: d.TX, audit: rec, clients: repository.NewClientRepository(d.TX),
		invoices: repository.NewInvoiceRepository(d.TX), premiums: repository.NewMemberPremiumPaymentRepository(d.TX),
		batches: repository.NewBenefitPayoutBatchRepository(d.TX), unitTxs: repository.NewMemberPortfolioTransactionRepository(d.TX)}
}

func (s *PensionService) record(ctx context.Context, action, entity string, id uuid.UUID, desc, route string) error {
	return s.audit.Record(ctx, audit.Entry{Action: action, Entity: entity, EntityUUID: id, Description: desc, Route: route})
}

// ---------------------------------------------------------------- invoices

// GenerateInvoice creates the client's invoice for a month, or completes the
// existing draft with members added since, then recomputes its totals.
// Repeating the call is harmless; an issued invoice is returned unchanged.
func (s *PensionService) GenerateInvoice(ctx context.Context, req dto.InvoiceGenerateRequest) (uuid.UUID, bool, error) {
	var out uuid.UUID
	created := false
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		clientUUID, err := uuid.Parse(req.ClientUUID)
		if err != nil {
			return apperr.Invalid("client_uuid", "uuid", "harus berupa UUID yang valid")
		}
		client, err := s.clients.Get(ctx, clientUUID) // tenant scope
		if errors.Is(err, apperr.ErrNotFound) {
			return apperr.Invalid("client_uuid", "exists", "data relasi tidak ditemukan")
		}
		if err != nil {
			return err
		}
		q := s.tx.Queries(ctx)
		if err := q.AdvisoryLock(ctx, fmt.Sprintf("invoice/%d/%04d-%02d", client.ID, req.Year, req.Month)); err != nil {
			return database.MapError("invoice lock", err)
		}
		year, month := req.Year, req.Month
		inv, err := q.FindActiveInvoice(ctx, sqlc.FindActiveInvoiceParams{ClientID: &client.ID, Year: &year, Month: &month})
		var id int64
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			actor := actorName(ctx)
			out = crud.NewID()
			if id, err = q.InsertInvoice(ctx, sqlc.InsertInvoiceParams{Uuid: out, ClientID: &client.ID, Year: &year,
				Month: &month, Actor: &actor}); err != nil {
				return database.MapError("insert invoice", err)
			}
			if err := prepareInvoice(ctx, q, id, year, month); err != nil {
				return err
			}
			created = true
		case err != nil:
			return database.MapError("find invoice", err)
		default:
			out, id = inv.Uuid, inv.ID
			if statusOf(inv.InvoiceStatus) != InvoiceDraft {
				return nil
			}
		}
		start, end := periodBounds(year, month)
		n, err := q.InsertInvoiceLines(ctx, sqlc.InsertInvoiceLinesParams{InvoiceID: &id, Month: &month, Year: &year,
			Actor: actorName(ctx), ClientID: &client.ID, PeriodStart: start, PeriodEnd: end})
		if err != nil {
			return database.MapError("invoice lines", err)
		}
		if err := refreshInvoiceTotals(ctx, q, id); err != nil {
			return err
		}
		if err := q.MarkInvoicePeriodGenerated(ctx, sqlc.MarkInvoicePeriodGeneratedParams{Year: &year, Month: &month}); err != nil {
			return database.MapError("invoice period", err)
		}
		return s.record(ctx, "invoice.generate", "invoice", out, fmt.Sprintf("baris baru=%d", n), "POST /invoices/generate")
	})
	return out, created, err
}

// lockInvoice locks an invoice, applies tenant scope and checks the ETag.
func (s *PensionService) lockInvoice(ctx context.Context, id uuid.UUID, expected time.Time) (sqlc.LockInvoiceRow, error) {
	inv, err := s.tx.Queries(ctx).LockInvoice(ctx, id)
	if err != nil {
		return inv, database.MapError("lock invoice", err)
	}
	if _, err := s.invoices.Get(ctx, id); err != nil {
		return inv, err
	}
	if !inv.UpdatedAt.Equal(expected) {
		return inv, apperr.ErrPreconditionFailed
	}
	return inv, nil
}

// IssueInvoice freezes a draft invoice with at least one line.
func (s *PensionService) IssueInvoice(ctx context.Context, id uuid.UUID, expected time.Time) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		inv, err := s.lockInvoice(ctx, id, expected)
		if err != nil {
			return err
		}
		if statusOf(inv.InvoiceStatus) != InvoiceDraft {
			return apperr.Conflict("hanya invoice draft yang dapat diterbitkan")
		}
		q := s.tx.Queries(ctx)
		n, err := q.CountInvoiceLines(ctx, &inv.ID)
		if err != nil {
			return database.MapError("count lines", err)
		}
		if n.Lines == 0 {
			return apperr.Invalid("invoice", "lines", "invoice tanpa baris iuran tidak dapat diterbitkan")
		}
		st, actor := InvoiceIssued, actorName(ctx)
		if err := q.SetInvoiceStatus(ctx, sqlc.SetInvoiceStatusParams{Status: &st, Actor: &actor, ID: inv.ID}); err != nil {
			return database.MapError("issue invoice", err)
		}
		return s.record(ctx, "invoice.issue", "invoice", id, "", "POST /invoices/{uuid}/issue")
	})
}

// CancelInvoice cancels a draft or issued invoice without paid lines; the
// period can then be generated again.
func (s *PensionService) CancelInvoice(ctx context.Context, id uuid.UUID, expected time.Time, note string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		inv, err := s.lockInvoice(ctx, id, expected)
		if err != nil {
			return err
		}
		if st := statusOf(inv.InvoiceStatus); st != InvoiceDraft && st != InvoiceIssued {
			return apperr.Conflict("invoice sudah lunas atau dibatalkan")
		}
		q := s.tx.Queries(ctx)
		n, err := q.CountInvoiceLines(ctx, &inv.ID)
		if err != nil {
			return database.MapError("count lines", err)
		}
		if n.Paid > 0 {
			return apperr.Conflict("invoice dengan iuran yang sudah dibayar tidak dapat dibatalkan")
		}
		st, actor := InvoiceCancelled, actorName(ctx)
		if err := q.SetInvoiceStatus(ctx, sqlc.SetInvoiceStatusParams{Status: &st, Actor: &actor, ID: inv.ID}); err != nil {
			return database.MapError("cancel invoice", err)
		}
		return s.record(ctx, "invoice.cancel", "invoice", id, note, "POST /invoices/{uuid}/cancel")
	})
}

// PayPremium records a member's payment of an issued invoice line (or of a
// line without invoice); the invoice becomes paid with its last line.
func (s *PensionService) PayPremium(ctx context.Context, id uuid.UUID, expected time.Time, req dto.PremiumPaymentRequest) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		p, err := q.LockPremium(ctx, id)
		if err != nil {
			return database.MapError("lock premium", err)
		}
		if _, err := s.premiums.Get(ctx, id); err != nil {
			return err
		}
		if !p.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(p.Status) != PremiumUnpaid {
			return apperr.Conflict("iuran sudah dibayar")
		}
		if p.InvoiceID != nil {
			st, err := q.LockInvoiceByID(ctx, *p.InvoiceID)
			if err != nil {
				return database.MapError("lock invoice", err)
			}
			if statusOf(st) != InvoiceIssued {
				return apperr.Conflict("iuran hanya dapat dibayar setelah invoice diterbitkan")
			}
		}
		paid := deref(p.PaymentNominal)
		if req.PaidNominal != "" {
			paid = *crud.Decimal(req.PaidNominal)
		}
		actor := actorName(ctx)
		if err := q.PayPremium(ctx, sqlc.PayPremiumParams{PaymentDate: crud.Date(req.PaymentDate), Paid: paid, Actor: &actor,
			ID: p.ID}); err != nil {
			return database.MapError("pay premium", err)
		}
		if err := q.AddMemberPremium(ctx, sqlc.AddMemberPremiumParams{Amount: paid, ID: *p.MemberID}); err != nil {
			return database.MapError("member premium total", err)
		}
		if p.InvoiceID != nil {
			if _, err := q.MarkInvoicePaidIfSettled(ctx, sqlc.MarkInvoicePaidIfSettledParams{Actor: actor, ID: *p.InvoiceID}); err != nil {
				return database.MapError("invoice paid", err)
			}
		}
		return s.record(ctx, "member_premium_payment.pay", "member_premium_payment", id, "dibayar="+paid.String(),
			"POST /member-premium-payments/{uuid}/pay")
	})
}

// ---------------------------------------------------------------- benefit payout

// GeneratePayoutBatch creates the month's batch (or completes the draft) with
// one line per retired member and per dependant with a monthly benefit.
func (s *PensionService) GeneratePayoutBatch(ctx context.Context, req dto.PeriodRequest) (uuid.UUID, bool, error) {
	var out uuid.UUID
	created := false
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		if err := q.AdvisoryLock(ctx, fmt.Sprintf("payout/%04d-%02d", req.Year, req.Month)); err != nil {
			return database.MapError("payout lock", err)
		}
		year, month := req.Year, req.Month
		b, err := q.FindActivePayoutBatch(ctx, sqlc.FindActivePayoutBatchParams{Year: &year, Month: &month})
		var id int64
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			actor := actorName(ctx)
			out = crud.NewID()
			if id, err = q.InsertPayoutBatch(ctx, sqlc.InsertPayoutBatchParams{Uuid: out, Year: &year, Month: &month, Actor: &actor}); err != nil {
				return database.MapError("insert payout batch", err)
			}
			if err := preparePayoutBatch(ctx, q, id, year, month); err != nil {
				return err
			}
			created = true
		case err != nil:
			return database.MapError("find payout batch", err)
		default:
			out, id = b.Uuid, b.ID
			if statusOf(b.PayoutBatchStatus) != PayoutDraft {
				return nil
			}
		}
		_, end := periodBounds(year, month)
		n, err := q.InsertPayoutLines(ctx, sqlc.InsertPayoutLinesParams{BatchID: &id, Month: &month, Year: &year,
			Actor: actorName(ctx), PeriodEnd: end})
		if err != nil {
			return database.MapError("payout lines", err)
		}
		if err := refreshPayoutTotals(ctx, q, id); err != nil {
			return err
		}
		if err := q.MarkPayoutPeriodGenerated(ctx, sqlc.MarkPayoutPeriodGeneratedParams{Year: &year, Month: &month}); err != nil {
			return database.MapError("payout period", err)
		}
		return s.record(ctx, "benefit_payout_batch.generate", "benefit_payout_batch", out, fmt.Sprintf("baris baru=%d", n),
			"POST /benefit-payout-batches/generate")
	})
	return out, created, err
}

func (s *PensionService) lockBatch(ctx context.Context, id uuid.UUID, expected time.Time) (sqlc.LockPayoutBatchRow, error) {
	b, err := s.tx.Queries(ctx).LockPayoutBatch(ctx, id)
	if err != nil {
		return b, database.MapError("lock payout batch", err)
	}
	if !b.UpdatedAt.Equal(expected) {
		return b, apperr.ErrPreconditionFailed
	}
	return b, nil
}

// setBatch moves a batch and its lines to status.
func (s *PensionService) setBatch(ctx context.Context, b sqlc.LockPayoutBatchRow, status int16, payoutDate *time.Time) error {
	q, actor := s.tx.Queries(ctx), actorName(ctx)
	if err := q.SetPayoutBatchStatus(ctx, sqlc.SetPayoutBatchStatusParams{Status: &status, Actor: &actor, ID: b.ID}); err != nil {
		return database.MapError("payout batch status", err)
	}
	return database.MapError("payout lines status", q.SetPayoutLinesStatus(ctx, sqlc.SetPayoutLinesStatusParams{
		Status: &status, PayoutDate: payoutDate, Actor: &actor, BatchID: &b.ID}))
}

// ApprovePayoutBatch (maker-checker) approves a draft batch with lines.
func (s *PensionService) ApprovePayoutBatch(ctx context.Context, id uuid.UUID, expected time.Time, note string) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		b, err := s.lockBatch(ctx, id, expected)
		if err != nil {
			return err
		}
		if statusOf(b.PayoutBatchStatus) != PayoutDraft {
			return apperr.Conflict("hanya batch payout draft yang dapat disetujui")
		}
		if b.CreatedBy != nil && *b.CreatedBy == actor.AuditName() {
			return apperr.Forbidden("pembuat batch payout tidak boleh menyetujuinya (maker-checker)")
		}
		if b.TotalBeneficiaries == nil || *b.TotalBeneficiaries == 0 {
			return apperr.Invalid("benefit_payout_batch", "lines", "batch payout tanpa penerima tidak dapat disetujui")
		}
		if err := s.setBatch(ctx, b, PayoutApproved, nil); err != nil {
			return err
		}
		today, name := time.Now().UTC().Truncate(24*time.Hour), actor.AuditName()
		if err := s.tx.Queries(ctx).ApprovePayoutBatch(ctx, sqlc.ApprovePayoutBatchParams{ApprovalDate: &today,
			Note: crud.Text(note), Actor: &name, ID: b.ID}); err != nil {
			return database.MapError("approve payout batch", err)
		}
		return s.record(ctx, "benefit_payout_batch.approve", "benefit_payout_batch", id, note,
			"POST /benefit-payout-batches/{uuid}/approve")
	})
}

// PayPayoutBatch marks an approved batch and its lines as paid.
func (s *PensionService) PayPayoutBatch(ctx context.Context, id uuid.UUID, expected time.Time, req dto.PayoutPaymentRequest) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		b, err := s.lockBatch(ctx, id, expected)
		if err != nil {
			return err
		}
		if statusOf(b.PayoutBatchStatus) != PayoutApproved {
			return apperr.Conflict("hanya batch payout yang sudah disetujui yang dapat dibayar")
		}
		if err := s.setBatch(ctx, b, PayoutPaid, crud.Date(req.PayoutDate)); err != nil {
			return err
		}
		return s.record(ctx, "benefit_payout_batch.pay", "benefit_payout_batch", id, "", "POST /benefit-payout-batches/{uuid}/pay")
	})
}

// CancelPayoutBatch cancels a draft or approved (unpaid) batch.
func (s *PensionService) CancelPayoutBatch(ctx context.Context, id uuid.UUID, expected time.Time, note string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		b, err := s.lockBatch(ctx, id, expected)
		if err != nil {
			return err
		}
		if st := statusOf(b.PayoutBatchStatus); st != PayoutDraft && st != PayoutApproved {
			return apperr.Conflict("batch payout sudah dibayar atau dibatalkan")
		}
		if err := s.setBatch(ctx, b, PayoutCancelled, nil); err != nil {
			return err
		}
		return s.record(ctx, "benefit_payout_batch.cancel", "benefit_payout_batch", id, note,
			"POST /benefit-payout-batches/{uuid}/cancel")
	})
}

// ---------------------------------------------------------------- member units

// SettleUnitTransaction applies a draft subscription (BUY) or redemption
// (SELL) to the member's unit account at its effective NAV, with a history
// snapshot, in one transaction. Settlement is in date order per account.
func (s *PensionService) SettleUnitTransaction(ctx context.Context, id uuid.UUID, expected time.Time) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		t, err := q.LockMemberTransaction(ctx, id)
		if err != nil {
			return database.MapError("lock unit transaction", err)
		}
		if _, err := s.unitTxs.Get(ctx, id); err != nil {
			return err
		}
		if !t.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(t.TransactionStatus) != TradeDraft {
			return apperr.Conflict("hanya transaksi unit draft yang dapat di-settle")
		}
		code, err := q.GetTradeTypeCode(ctx, *t.TradeTypeID)
		if err != nil {
			return database.MapError("trade type", err)
		}
		acc, err := q.LockMemberPortfolio(ctx, *t.MemberPortfolioID)
		if err != nil {
			return database.MapError("lock unit account", err)
		}
		if acc.LatestTransactionDate != nil && t.TransactionDate.Before(*acc.LatestTransactionDate) {
			return apperr.Invalid("transaction_date", "order", "tanggal transaksi sebelum transaksi terakhir rekening unit")
		}
		units, value := deref(t.TotalUnit), deref(t.TotalTransactionValue)
		holding, invested := deref(acc.TotalUnit), deref(acc.TotalInvestment)
		desc := "subscribe"
		if orDefault(code, "") == "SELL" {
			if units.GreaterThan(holding) {
				return apperr.Invalid("total_unit", "max", "unit yang di-redeem melebihi saldo unit "+holding.String())
			}
			cost := invested.Mul(units).Div(holding).Round(4)
			holding, invested, desc = holding.Sub(units), invested.Sub(cost), "redeem"
		} else {
			holding, invested = holding.Add(units), invested.Add(value)
		}
		actor := actorName(ctx)
		if err := q.ApplyMemberPortfolio(ctx, sqlc.ApplyMemberPortfolioParams{TotalUnit: &holding, TotalInvestment: &invested,
			Nav: t.EffectiveNav, Date: t.TransactionDate, Actor: &actor, ID: acc.ID}); err != nil {
			return database.MapError("apply unit account", err)
		}
		desc += " " + id.String()
		if err := q.InsertMemberPortfolioHistory(ctx, sqlc.InsertMemberPortfolioHistoryParams{Uuid: crud.NewID(),
			MemberPortfolioID: &acc.ID, TotalUnit: &holding, Nav: t.EffectiveNav, TotalInvestment: &invested,
			Date: t.TransactionDate, Description: &desc, Actor: &actor}); err != nil {
			return database.MapError("unit history", err)
		}
		st := TradeSettled
		if err := q.SetMemberTransactionStatus(ctx, sqlc.SetMemberTransactionStatusParams{Status: &st, Actor: &actor, ID: t.ID}); err != nil {
			return database.MapError("unit transaction status", err)
		}
		return s.record(ctx, "member_portfolio_transaction.settle", "member_portfolio_transaction", id, desc,
			"POST /member-portfolio-transactions/{uuid}/settle")
	})
}

// CancelUnitTransaction cancels a draft unit transaction.
func (s *PensionService) CancelUnitTransaction(ctx context.Context, id uuid.UUID, expected time.Time) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		t, err := q.LockMemberTransaction(ctx, id)
		if err != nil {
			return database.MapError("lock unit transaction", err)
		}
		if _, err := s.unitTxs.Get(ctx, id); err != nil {
			return err
		}
		if !t.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(t.TransactionStatus) != TradeDraft {
			return apperr.Conflict("hanya transaksi unit draft yang dapat dibatalkan")
		}
		st, actor := TradeCancelled, actorName(ctx)
		if err := q.SetMemberTransactionStatus(ctx, sqlc.SetMemberTransactionStatusParams{Status: &st, Actor: &actor, ID: t.ID}); err != nil {
			return database.MapError("unit transaction status", err)
		}
		return s.record(ctx, "member_portfolio_transaction.cancel", "member_portfolio_transaction", id, "",
			"POST /member-portfolio-transactions/{uuid}/cancel")
	})
}

// View returns the current (PII-masked) representation of a pension record.
func (s *PensionService) View(ctx context.Context, kind string, id uuid.UUID) (any, time.Time, error) {
	switch kind {
	case "invoice":
		return anyView(InvoiceResource(s.d).View(ctx, s.audit, id))
	case "member_premium_payment":
		return anyView(MemberPremiumPaymentResource(s.d).View(ctx, s.audit, id))
	case "benefit_payout_batch":
		return anyView(BenefitPayoutBatchResource(s.d).View(ctx, s.audit, id))
	default:
		return anyView(MemberPortfolioTransactionResource(s.d).View(ctx, s.audit, id))
	}
}
