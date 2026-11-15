package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
)

// Member status codes (member.status).
const (
	MemberInactive int16 = 0
	MemberActive   int16 = 1
	MemberRetired  int16 = 2
)

// Invoice and premium codes.
const (
	InvoiceDraft     int16 = 0
	InvoiceIssued    int16 = 1
	InvoicePaid      int16 = 2
	InvoiceCancelled int16 = 3
	PremiumUnpaid    int16 = 0
	PremiumPaid      int16 = 1
)

// Benefit payout codes (batch and lines).
const (
	PayoutDraft     int16 = 0
	PayoutApproved  int16 = 1
	PayoutPaid      int16 = 2
	PayoutCancelled int16 = 3
)

// periodBounds returns the first and last day of a month.
func periodBounds(year, month int32) (time.Time, time.Time) {
	start := time.Date(int(year), time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, -1)
}

// sameUUID compares a stored reference with a (validated) request UUID.
func sameUUID(cur *uuid.UUID, raw string) bool {
	if raw == "" {
		return cur == nil
	}
	id, err := uuid.Parse(raw)
	return err == nil && cur != nil && *cur == id
}

func i32(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// ownedBy checks that the row resolved from childID belongs to memberID.
func ownedBy(ctx context.Context, d Deps, table, raw, field string, memberID *int64,
	owner func(context.Context, int64) (*int64, error), msg string) error {
	childID, err := database.RefID(ctx, d.TX, table, raw, field)
	if err != nil || childID == nil {
		return err
	}
	got, err := owner(ctx, *childID)
	if err != nil {
		return database.MapError("owner of "+table, err)
	}
	if !sameID(got, memberID) {
		return apperr.Invalid(field, "owner", msg)
	}
	return nil
}

// ---------------------------------------------------------------- member

// checkMember: dates in order; a member's client is fixed once created.
func checkMember(_ context.Context, _ Deps, req *dto.MemberRequest, cur *sqlc.GetMemberByUUIDRow) error {
	if err := checkDateRange(req.Birthdate, req.StartWorkingDate, "start_working_date"); err != nil {
		return err
	}
	if err := checkDateRange(req.StartWorkingDate, req.RetiredDate, "retired_date"); err != nil {
		return err
	}
	if cur != nil && !sameUUID(cur.ClientUuid, req.ClientUUID) {
		return apperr.Conflict("client peserta tidak dapat dipindah; nonaktifkan dan daftarkan ulang di client baru")
	}
	return nil
}

// assignMemberNumber numbers new members gaplessly (MBR000001, ...).
func assignMemberNumber(ctx context.Context, d Deps, before, after *sqlc.GetMemberByUUIDRow) error {
	if before != nil || after == nil || after.MemberNumber != nil {
		return nil
	}
	q := d.TX.Queries(ctx)
	n, err := q.NextNumber(ctx, sqlc.NextNumberParams{Uuid: crud.NewID(), Name: "MEMBER", Period: "ALL"})
	if err != nil {
		return database.MapError("member number", err)
	}
	number := fmt.Sprintf("MBR%06d", n)
	return database.MapError("set member number", q.SetMemberNumber(ctx, sqlc.SetMemberNumberParams{Number: &number, ID: after.ID}))
}

func checkTaxReport(ctx context.Context, d Deps, req *dto.TaxReportRequest, _ *sqlc.GetTaxReportByUUIDRow) error {
	memberID, err := database.RefID(ctx, d.TX, "member", req.MemberUUID, "member_uuid")
	if err != nil {
		return err
	}
	return ownedBy(ctx, d, "dependant", req.DependantUUID, "dependant_uuid", memberID, d.TX.Queries(ctx).GetDependantMemberID,
		"tanggungan bukan milik peserta ini")
}

func checkMemberDocument(ctx context.Context, d Deps, req *dto.MemberDocumentRequest, _ *sqlc.GetMemberDocumentByUUIDRow) error {
	memberID, err := database.RefID(ctx, d.TX, "member", req.MemberUUID, "member_uuid")
	if err != nil {
		return err
	}
	q := d.TX.Queries(ctx)
	if err := ownedBy(ctx, d, "member_premium_payment", req.MemberPremiumPaymentUUID, "member_premium_payment_uuid",
		memberID, q.GetPremiumMemberID, "iuran bukan milik peserta ini"); err != nil {
		return err
	}
	return ownedBy(ctx, d, "member_benefit_payout", req.MemberBenefitPayoutUUID, "member_benefit_payout_uuid",
		memberID, q.GetPayoutMemberID, "pembayaran manfaat bukan milik peserta ini")
}

// ---------------------------------------------------------------- member units

// checkMemberPortfolio: the portfolio belongs to the member's client; the
// pair is fixed once opened.
func checkMemberPortfolio(ctx context.Context, d Deps, req *dto.MemberPortfolioRequest, cur *sqlc.GetMemberPortfolioByUUIDRow) error {
	if cur != nil && (!sameUUID(cur.MemberUuid, req.MemberUUID) || !sameUUID(cur.PortfolioUuid, req.PortfolioUUID)) {
		return apperr.Conflict("peserta dan portofolio rekening unit tidak dapat diubah")
	}
	q := d.TX.Queries(ctx)
	memberID, err := database.RefID(ctx, d.TX, "member", req.MemberUUID, "member_uuid")
	if err != nil {
		return err
	}
	portfolioID, err := database.RefID(ctx, d.TX, "portfolio", req.PortfolioUUID, "portfolio_uuid")
	if err != nil {
		return err
	}
	mc, err := q.GetMemberClientID(ctx, *memberID)
	if err != nil {
		return database.MapError("member client", err)
	}
	pc, err := q.GetPortfolioClientID(ctx, *portfolioID)
	if err != nil {
		return database.MapError("portfolio client", err)
	}
	if !sameID(mc, pc) {
		return apperr.Invalid("portfolio_uuid", "client", "portofolio harus milik client peserta")
	}
	return nil
}

// checkMemberPortfolioTransaction: drafts only; BUY (subscribe) or SELL
// (redeem); amount given as units or value.
func checkMemberPortfolioTransaction(ctx context.Context, d Deps, req *dto.MemberPortfolioTransactionRequest,
	cur *sqlc.GetMemberPortfolioTransactionByUUIDRow) error {
	if cur != nil && statusOf(cur.TransactionStatus) != TradeDraft {
		return apperr.Conflict("hanya transaksi unit draft yang dapat diubah")
	}
	if req.TotalUnit == "" && req.TotalTransactionValue == "" {
		return apperr.Invalid("total_transaction_value", "required", "isi total_transaction_value atau total_unit")
	}
	ttID, err := database.RefID(ctx, d.TX, "trade_type", req.TradeTypeUUID, "trade_type_uuid")
	if err != nil {
		return err
	}
	code, err := d.TX.Queries(ctx).GetTradeTypeCode(ctx, *ttID)
	if err != nil {
		return database.MapError("trade type", err)
	}
	if c := orDefault(code, ""); c != "BUY" && c != "SELL" {
		return apperr.Invalid("trade_type_uuid", "oneof", "tipe transaksi harus BUY (subscribe) atau SELL (redeem)")
	}
	return nil
}

func checkMemberPortfolioTransactionDelete(_ context.Context, _ Deps, cur *sqlc.GetMemberPortfolioTransactionByUUIDRow) error {
	if statusOf(cur.TransactionStatus) != TradeDraft {
		return apperr.Conflict("hanya transaksi unit draft yang dapat dihapus")
	}
	return nil
}

// ---------------------------------------------------------------- invoice

// checkInvoice: drafts only; client and period are fixed once created.
func checkInvoice(_ context.Context, _ Deps, req *dto.InvoiceRequest, cur *sqlc.GetInvoiceByUUIDRow) error {
	if err := checkDateRange(req.InvoiceDate, req.InvoiceDueDate, "invoice_due_date"); err != nil {
		return err
	}
	if cur == nil {
		return nil
	}
	if statusOf(cur.InvoiceStatus) != InvoiceDraft {
		return apperr.Conflict("hanya invoice draft yang dapat diubah")
	}
	if !sameUUID(cur.ClientUuid, req.ClientUUID) || i32(cur.Year) != i32(req.Year) || i32(cur.Month) != i32(req.Month) {
		return apperr.Conflict("client dan periode invoice tidak dapat diubah")
	}
	return nil
}

func checkInvoiceDelete(_ context.Context, _ Deps, cur *sqlc.GetInvoiceByUUIDRow) error {
	if statusOf(cur.InvoiceStatus) != InvoiceDraft {
		return apperr.Conflict("hanya invoice draft yang dapat dihapus; gunakan /cancel")
	}
	return nil
}

// prepareInvoice numbers a new invoice (NN/DP/MM/YYYY, per month), sets its
// dates (1st, due on the 10th) and recomputes its totals.
func prepareInvoice(ctx context.Context, q *sqlc.Queries, id int64, year, month int32) error {
	start, _ := periodBounds(year, month)
	n, err := q.NextNumber(ctx, sqlc.NextNumberParams{Uuid: crud.NewID(), Name: "INVOICE", Period: start.Format("2006-01")})
	if err != nil {
		return database.MapError("invoice number", err)
	}
	if err := q.AssignInvoiceDefaults(ctx, sqlc.AssignInvoiceDefaultsParams{Number: fmt.Sprintf("%02d/DP/%02d/%d", n, month, year),
		InvoiceDate: start, DueDate: start.AddDate(0, 0, 9), ID: id}); err != nil {
		return database.MapError("invoice defaults", err)
	}
	return refreshInvoiceTotals(ctx, q, id)
}

func refreshInvoiceTotals(ctx context.Context, q *sqlc.Queries, id int64) error {
	total, err := q.RefreshInvoiceTotals(ctx, id)
	if err != nil {
		return database.MapError("invoice totals", err)
	}
	words := Terbilang(deref(total))
	return database.MapError("invoice billing", q.SetInvoiceBillingDescription(ctx, sqlc.SetInvoiceBillingDescriptionParams{
		Description: &words, ID: id}))
}

func refreshInvoice(ctx context.Context, d Deps, before, after *sqlc.GetInvoiceByUUIDRow) error {
	if after == nil {
		return nil
	}
	q := d.TX.Queries(ctx)
	if after.InvoiceNumber == nil {
		return prepareInvoice(ctx, q, after.ID, i32(after.Year), i32(after.Month))
	}
	return refreshInvoiceTotals(ctx, q, after.ID)
}

// checkPremium: unpaid lines of draft invoices only; the member must belong
// to the invoice's client; lines without invoice need their own period.
func checkPremium(ctx context.Context, d Deps, req *dto.MemberPremiumPaymentRequest, cur *sqlc.GetMemberPremiumPaymentByUUIDRow) error {
	if cur != nil {
		if err := checkPremiumDelete(ctx, d, cur); err != nil {
			return err
		}
	}
	q := d.TX.Queries(ctx)
	invoiceID, err := database.RefID(ctx, d.TX, "invoice", req.InvoiceUUID, "invoice_uuid")
	if err != nil {
		return err
	}
	if invoiceID == nil {
		if req.Month == nil || req.Year == nil {
			return apperr.Invalid("month", "required", "month dan year wajib diisi bila iuran tanpa invoice")
		}
		return nil
	}
	inv, err := q.GetInvoiceState(ctx, *invoiceID)
	if err != nil {
		return database.MapError("invoice state", err)
	}
	if statusOf(inv.InvoiceStatus) != InvoiceDraft {
		return apperr.Invalid("invoice_uuid", "status", "baris iuran hanya dapat ditambah ke invoice draft")
	}
	memberID, err := database.RefID(ctx, d.TX, "member", req.MemberUUID, "member_uuid")
	if err != nil {
		return err
	}
	mc, err := q.GetMemberClientID(ctx, *memberID)
	if err != nil {
		return database.MapError("member client", err)
	}
	if !sameID(mc, inv.ClientID) {
		return apperr.Invalid("member_uuid", "client", "peserta bukan milik client invoice ini")
	}
	return nil
}

func checkPremiumDelete(ctx context.Context, d Deps, cur *sqlc.GetMemberPremiumPaymentByUUIDRow) error {
	if statusOf(cur.Status) != PremiumUnpaid {
		return apperr.Conflict("iuran yang sudah dibayar tidak dapat diubah")
	}
	if cur.InvoiceID == nil {
		return nil
	}
	inv, err := d.TX.Queries(ctx).GetInvoiceState(ctx, *cur.InvoiceID)
	if err != nil {
		return database.MapError("invoice state", err)
	}
	if statusOf(inv.InvoiceStatus) != InvoiceDraft {
		return apperr.Conflict("baris iuran pada invoice yang sudah diterbitkan tidak dapat diubah")
	}
	return nil
}

func refreshPremiumInvoice(ctx context.Context, d Deps, before, after *sqlc.GetMemberPremiumPaymentByUUIDRow) error {
	ids := parents(func(r *sqlc.GetMemberPremiumPaymentByUUIDRow) *int64 { return r.InvoiceID }, before, after)
	q := d.TX.Queries(ctx)
	return refreshEach(ids, func(id *int64) error { return refreshInvoiceTotals(ctx, q, *id) })
}

// ---------------------------------------------------------------- benefit payout

func checkPayoutBatch(_ context.Context, _ Deps, req *dto.BenefitPayoutBatchRequest, cur *sqlc.GetBenefitPayoutBatchByUUIDRow) error {
	if cur == nil {
		return nil
	}
	if statusOf(cur.PayoutBatchStatus) != PayoutDraft {
		return apperr.Conflict("hanya batch payout draft yang dapat diubah")
	}
	if i32(cur.Year) != i32(req.Year) || i32(cur.Month) != i32(req.Month) {
		return apperr.Conflict("periode batch payout tidak dapat diubah")
	}
	return nil
}

func checkPayoutBatchDelete(_ context.Context, _ Deps, cur *sqlc.GetBenefitPayoutBatchByUUIDRow) error {
	if statusOf(cur.PayoutBatchStatus) != PayoutDraft {
		return apperr.Conflict("hanya batch payout draft yang dapat dihapus; gunakan /cancel")
	}
	return nil
}

// preparePayoutBatch numbers a new batch (NN/PAYMENT/MM/YYYY) and recomputes its totals.
func preparePayoutBatch(ctx context.Context, q *sqlc.Queries, id int64, year, month int32) error {
	start, _ := periodBounds(year, month)
	n, err := q.NextNumber(ctx, sqlc.NextNumberParams{Uuid: crud.NewID(), Name: "PAYOUT", Period: start.Format("2006-01")})
	if err != nil {
		return database.MapError("payout batch number", err)
	}
	if err := q.AssignPayoutBatchDefaults(ctx, sqlc.AssignPayoutBatchDefaultsParams{
		Number: fmt.Sprintf("%02d/PAYMENT/%02d/%d", n, month, year), GeneratedDate: start,
		Name: fmt.Sprintf("Pembayaran Manfaat %02d/%d", month, year), ID: id}); err != nil {
		return database.MapError("payout batch defaults", err)
	}
	return refreshPayoutTotals(ctx, q, id)
}

func refreshPayoutTotals(ctx context.Context, q *sqlc.Queries, id int64) error {
	total, err := q.RefreshPayoutBatchTotals(ctx, id)
	if err != nil {
		return database.MapError("payout totals", err)
	}
	words := Terbilang(deref(total))
	return database.MapError("payout words", q.SetPayoutBatchValueDescription(ctx, sqlc.SetPayoutBatchValueDescriptionParams{
		Description: &words, ID: id}))
}

func refreshPayoutBatch(ctx context.Context, d Deps, _, after *sqlc.GetBenefitPayoutBatchByUUIDRow) error {
	if after == nil {
		return nil
	}
	q := d.TX.Queries(ctx)
	if after.BatchNumber == nil {
		return preparePayoutBatch(ctx, q, after.ID, i32(after.Year), i32(after.Month))
	}
	return refreshPayoutTotals(ctx, q, after.ID)
}

// checkMemberPayout: lines of draft batches only; dependant and tax report
// belong to the member.
func checkMemberPayout(ctx context.Context, d Deps, req *dto.MemberBenefitPayoutRequest, cur *sqlc.GetMemberBenefitPayoutByUUIDRow) error {
	if cur != nil {
		if err := checkMemberPayoutDelete(ctx, d, cur); err != nil {
			return err
		}
	}
	q := d.TX.Queries(ctx)
	batchID, err := database.RefID(ctx, d.TX, "benefit_payout_batch", req.BenefitPayoutBatchUUID, "benefit_payout_batch_uuid")
	if err != nil {
		return err
	}
	st, err := q.GetPayoutBatchStatus(ctx, *batchID)
	if err != nil {
		return database.MapError("payout batch", err)
	}
	if statusOf(st) != PayoutDraft {
		return apperr.Invalid("benefit_payout_batch_uuid", "status", "baris hanya dapat ditambah ke batch payout draft")
	}
	memberID, err := database.RefID(ctx, d.TX, "member", req.MemberUUID, "member_uuid")
	if err != nil {
		return err
	}
	if err := ownedBy(ctx, d, "dependant", req.DependantUUID, "dependant_uuid", memberID, q.GetDependantMemberID,
		"tanggungan bukan milik peserta ini"); err != nil {
		return err
	}
	return ownedBy(ctx, d, "tax_report", req.TaxReportUUID, "tax_report_uuid", memberID, q.GetTaxReportMemberID,
		"bukti potong bukan milik peserta ini")
}

func checkMemberPayoutDelete(ctx context.Context, d Deps, cur *sqlc.GetMemberBenefitPayoutByUUIDRow) error {
	if cur.BenefitPayoutBatchID == nil {
		return nil
	}
	st, err := d.TX.Queries(ctx).GetPayoutBatchStatus(ctx, *cur.BenefitPayoutBatchID)
	if err != nil {
		return database.MapError("payout batch", err)
	}
	if statusOf(st) != PayoutDraft {
		return apperr.Conflict("baris pada batch payout yang sudah disetujui tidak dapat diubah")
	}
	return nil
}

func refreshMemberPayoutBatch(ctx context.Context, d Deps, before, after *sqlc.GetMemberBenefitPayoutByUUIDRow) error {
	ids := parents(func(r *sqlc.GetMemberBenefitPayoutByUUIDRow) *int64 { return r.BenefitPayoutBatchID }, before, after)
	q := d.TX.Queries(ctx)
	return refreshEach(ids, func(id *int64) error { return refreshPayoutTotals(ctx, q, *id) })
}
