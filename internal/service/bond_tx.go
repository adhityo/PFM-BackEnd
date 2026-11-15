package service

import (
	"context"
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
	"pfm-backend/internal/repository"
)

// Coupon schedule transaction types (legacy): 1 coupon, 2 principal.
const (
	ScheduleCoupon    int16 = 1
	SchedulePrincipal int16 = 2
)

// BondTxService implements the bond actions of PRD §10.5 (bond module).
type BondTxService struct {
	d         Deps
	tx        *database.TxManager
	audit     *audit.Recorder
	files     *FileService
	bonds     repository.BondRepository
	trades    repository.BondTradeRepository
	schedules repository.BondCouponScheduleRepository
	coupons   repository.InvestorCouponScheduleRepository
	fees      repository.AgentFeeTransactionRepository
}

func NewBondTxService(d Deps, rec *audit.Recorder) *BondTxService {
	tx := d.TX
	return &BondTxService{d: d, tx: tx, audit: rec, files: d.Files,
		bonds: repository.NewBondRepository(tx), trades: repository.NewBondTradeRepository(tx),
		schedules: repository.NewBondCouponScheduleRepository(tx), coupons: repository.NewInvestorCouponScheduleRepository(tx),
		fees: repository.NewAgentFeeTransactionRepository(tx)}
}

func (s *BondTxService) record(ctx context.Context, action, entity string, id uuid.UUID, desc, route string) error {
	return s.audit.Record(ctx, audit.Entry{Action: action, Entity: entity, EntityUUID: id, Description: desc, Route: route})
}

// lockBond locks a bond, applies tenant scope and checks the ETag.
func (s *BondTxService) lockBond(ctx context.Context, id uuid.UUID, expected time.Time) (sqlc.LockBondRow, error) {
	b, err := s.tx.Queries(ctx).LockBond(ctx, id)
	if err != nil {
		return b, database.MapError("lock bond", err)
	}
	if _, err := s.bonds.Get(ctx, id); err != nil {
		return b, err
	}
	if !b.UpdatedAt.Equal(expected) {
		return b, apperr.ErrPreconditionFailed
	}
	return b, nil
}

// GenerateCouponSchedule replaces the unpaid schedule of a bond that has no
// approved subscription: one coupon per period (rate = coupon × days / 360,
// as in the legacy data) and the principal (100%) at maturity.
func (s *BondTxService) GenerateCouponSchedule(ctx context.Context, id uuid.UUID, expected time.Time) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		b, err := s.lockBond(ctx, id, expected)
		if err != nil {
			return err
		}
		q := s.tx.Queries(ctx)
		if st := statusOf(b.BondStatus); st != BondDraft && st != BondOffering {
			return apperr.Conflict("jadwal kupon hanya dapat dibuat sebelum obligasi aktif")
		}
		if n, err := q.CountApprovedBondTrades(ctx, &b.ID); err != nil {
			return database.MapError("count trades", err)
		} else if n > 0 {
			return apperr.Conflict("sudah ada subscription yang disetujui; jadwal kupon tidak dapat dibuat ulang")
		}
		if b.IssuanceDate == nil || b.MaturityDate == nil || b.Coupon == nil || b.CouponPaymentPeriode == nil || *b.CouponPaymentPeriode < 1 {
			return apperr.Invalid("coupon_payment_periode", "required", "tanggal terbit, jatuh tempo, kupon dan periode kupon wajib diisi")
		}
		actor := actorName(ctx)
		if err := q.ClearUnpaidCouponSchedules(ctx, sqlc.ClearUnpaidCouponSchedulesParams{Actor: &actor, BondID: &b.ID}); err != nil {
			return database.MapError("clear schedule", err)
		}
		insert := func(date time.Time, rate decimal.Decimal, kind int16) error {
			return database.MapError("insert schedule", q.InsertCouponSchedule(ctx, sqlc.InsertCouponScheduleParams{
				Uuid: crud.NewID(), BondID: &b.ID, CouponDate: &date, CouponRate: &rate, TransactionType: &kind, Actor: &actor}))
		}
		start, end, step := *b.IssuanceDate, *b.MaturityDate, int(*b.CouponPaymentPeriode)
		for n := 0; start.Before(end); n++ {
			if n > 1000 {
				return apperr.Invalid("coupon_payment_periode", "max", "terlalu banyak periode kupon")
			}
			next := start.AddDate(0, 0, step)
			if next.After(end) {
				next = end
			}
			days := decimal.NewFromInt(int64(next.Sub(start).Hours() / 24))
			rate := b.Coupon.Mul(days).Div(decimal.NewFromInt(360)).Round(6)
			if err := insert(next, rate, ScheduleCoupon); err != nil {
				return err
			}
			start = next
		}
		if err := insert(end, decimal.NewFromInt(100), SchedulePrincipal); err != nil {
			return err
		}
		if err := q.RefreshBondCouponDates(ctx, &b.ID); err != nil {
			return database.MapError("coupon dates", err)
		}
		return s.record(ctx, "bond.generate_coupon_schedule", "bond", id, "", "POST /bonds/{uuid}/generate-coupon-schedule")
	})
}

// SetStatus moves a bond to offering (from draft) or active (from draft or
// offering; requires a coupon schedule).
func (s *BondTxService) SetStatus(ctx context.Context, id uuid.UUID, expected time.Time, target int16) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		b, err := s.lockBond(ctx, id, expected)
		if err != nil {
			return err
		}
		q := s.tx.Queries(ctx)
		cur := statusOf(b.BondStatus)
		switch target {
		case BondOffering:
			if cur != BondDraft {
				return apperr.Conflict("hanya obligasi draft yang dapat dibuka untuk offering")
			}
		case BondActive:
			if cur != BondDraft && cur != BondOffering {
				return apperr.Conflict("obligasi sudah aktif atau jatuh tempo")
			}
			if n, err := q.CountCouponSchedules(ctx, &b.ID); err != nil {
				return database.MapError("count schedule", err)
			} else if n == 0 {
				return apperr.Invalid("bond", "coupon_schedule", "buat jadwal kupon sebelum mengaktifkan obligasi")
			}
		}
		actor := actorName(ctx)
		if err := q.SetBondStatus(ctx, sqlc.SetBondStatusParams{Status: &target, Actor: &actor, ID: b.ID}); err != nil {
			return database.MapError("bond status", err)
		}
		action := map[int16]string{BondOffering: "offer", BondActive: "activate"}[target]
		return s.record(ctx, "bond."+action, "bond", id, "", "POST /bonds/{uuid}/"+action)
	})
}

// ApproveTrade (maker-checker) approves a subscription: certificate number
// from a row-locked per-bond counter, investor coupon schedule and bond
// totals, all in one transaction.
func (s *BondTxService) ApproveTrade(ctx context.Context, id uuid.UUID, expected time.Time, note string) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		t, err := q.LockBondTrade(ctx, id)
		if err != nil {
			return database.MapError("lock bond trade", err)
		}
		if _, err := s.trades.Get(ctx, id); err != nil { // tenant scope
			return err
		}
		if !t.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(t.BondTradeStatus) != BondTradeDraft {
			return apperr.Conflict("hanya subscription draft yang dapat disetujui")
		}
		if t.CreatedBy != nil && *t.CreatedBy == actor.AuditName() {
			return apperr.Forbidden("pembuat subscription tidak boleh menyetujuinya (maker-checker)")
		}
		if t.BondID == nil || t.Nominal == nil {
			return apperr.Invalid("bond_uuid", "required", "obligasi dan nominal wajib diisi")
		}
		cert, err := q.NextCertificate(ctx, *t.BondID)
		if err != nil {
			return database.MapError("certificate counter", err)
		}
		if st := statusOf(cert.BondStatus); st != BondOffering && st != BondActive {
			return apperr.Conflict("obligasi tidak dalam masa offering/aktif")
		}
		if n, err := q.CountCouponSchedules(ctx, t.BondID); err != nil {
			return database.MapError("count schedule", err)
		} else if n == 0 {
			return apperr.Invalid("bond_uuid", "coupon_schedule", "jadwal kupon obligasi belum dibuat")
		}
		option := t.BondPeriodeOptionID
		if option == nil {
			option = cert.BondPeriodeOptionID
		}
		tpl, err := q.CertificateTemplate(ctx, option)
		if err != nil {
			return database.MapError("certificate template", err)
		}
		if tpl == "" {
			tpl = "{bondCode}-{series}{counter}"
		}
		number := renderNumber(tpl, map[string]string{"bondCode": orDefault(cert.BondCode, ""),
			"series": orDefault(cert.BondSeriesNumber, ""), "bondSeries": orDefault(cert.BondSeriesNumber, ""),
			"year": time.Now().UTC().Format("2006"), "counter": pad(*cert.LatestCertificateCounter, 7)})
		name := actor.AuditName()
		if err := q.ApproveBondTrade(ctx, sqlc.ApproveBondTradeParams{Counter: cert.LatestCertificateCounter, Number: &number,
			Actor: &name, Note: crud.Text(note), ID: t.ID}); err != nil {
			return database.MapError("approve trade", err)
		}
		if _, err := q.CreateInvestorCoupons(ctx, sqlc.CreateInvestorCouponsParams{BondTradeID: &t.ID, Column2: *t.Nominal,
			BondID: t.BondID, Column4: name}); err != nil {
			return database.MapError("investor coupons", err)
		}
		if err := q.AddScheduleCouponValues(ctx, sqlc.AddScheduleCouponValuesParams{Column1: *t.Nominal, BondID: t.BondID}); err != nil {
			return database.MapError("schedule values", err)
		}
		if err := q.AddBondSubscription(ctx, sqlc.AddBondSubscriptionParams{Column1: *t.Nominal, ID: *t.BondID}); err != nil {
			return database.MapError("bond totals", err)
		}
		return s.record(ctx, "bond_trade.approve", "bond_trade", id, "sertifikat="+number, "POST /bond-trades/{uuid}/approve")
	})
}

// CancelTrade cancels (rejects) a draft subscription.
func (s *BondTxService) CancelTrade(ctx context.Context, id uuid.UUID, expected time.Time, note string) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		t, err := q.LockBondTrade(ctx, id)
		if err != nil {
			return database.MapError("lock bond trade", err)
		}
		if _, err := s.trades.Get(ctx, id); err != nil {
			return err
		}
		if !t.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(t.BondTradeStatus) != BondTradeDraft {
			return apperr.Conflict("hanya subscription draft yang dapat dibatalkan")
		}
		actor := actorName(ctx)
		if err := q.CancelBondTrade(ctx, sqlc.CancelBondTradeParams{Note: crud.Text(note), Actor: &actor, ID: t.ID}); err != nil {
			return database.MapError("cancel trade", err)
		}
		return s.record(ctx, "bond_trade.cancel", "bond_trade", id, note, "POST /bond-trades/{uuid}/cancel")
	})
}

// addPaid books a payment on the bond totals and closes the schedule when
// every investor coupon of it is paid.
func (s *BondTxService) addPaid(ctx context.Context, bondID, scheduleID int64, kind int16, amount decimal.Decimal, date *time.Time) error {
	q := s.tx.Queries(ctx)
	coupon, principal := amount, decimal.Zero
	if kind == SchedulePrincipal {
		coupon, principal = decimal.Zero, amount
	}
	if err := q.AddBondPaid(ctx, sqlc.AddBondPaidParams{Coupon: coupon, Principal: principal, ID: bondID}); err != nil {
		return database.MapError("bond paid totals", err)
	}
	if err := q.AddSchedulePaid(ctx, sqlc.AddSchedulePaidParams{ID: scheduleID, Column2: amount}); err != nil {
		return database.MapError("schedule paid", err)
	}
	if _, err := q.CloseScheduleIfSettled(ctx, sqlc.CloseScheduleIfSettledParams{ID: scheduleID, SettlementDate: date,
		Column3: actorName(ctx)}); err != nil {
		return database.MapError("close schedule", err)
	}
	return database.MapError("coupon dates", q.RefreshBondCouponDates(ctx, &bondID))
}

// PaySchedule pays every unpaid investor coupon of a bond coupon schedule.
func (s *BondTxService) PaySchedule(ctx context.Context, id uuid.UUID, expected time.Time, req dto.CouponPaymentRequest) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		sch, err := q.LockCouponSchedule(ctx, id)
		if err != nil {
			return database.MapError("lock schedule", err)
		}
		if _, err := s.schedules.Get(ctx, id); err != nil {
			return err
		}
		if !sch.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(sch.Status) != 0 {
			return apperr.Conflict("jadwal kupon sudah dibayar")
		}
		date := crud.Date(req.SettlementDate)
		total, err := q.PayScheduleInvestorCoupons(ctx, sqlc.PayScheduleInvestorCouponsParams{BondCouponScheduleID: &sch.ID,
			PaymentSettlementDate: date, Column3: actorName(ctx)})
		if err != nil {
			return database.MapError("pay investor coupons", err)
		}
		kind := ScheduleCoupon
		if sch.TransactionType != nil {
			kind = *sch.TransactionType
		}
		if err := s.addPaid(ctx, *sch.BondID, sch.ID, kind, total, date); err != nil {
			return err
		}
		return s.record(ctx, "bond_coupon_schedule.pay", "bond_coupon_schedule", id, "total="+total.String(),
			"POST /bond-coupon-schedules/{uuid}/pay")
	})
}

// PayInvestorCoupon pays one investor coupon with a transfer proof (PDF).
func (s *BondTxService) PayInvestorCoupon(ctx context.Context, id uuid.UUID, expected time.Time, req dto.InvestorCouponPaymentRequest) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		c, err := q.LockInvestorCoupon(ctx, id)
		if err != nil {
			return database.MapError("lock investor coupon", err)
		}
		if _, err := s.coupons.Get(ctx, id); err != nil {
			return err
		}
		if !c.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(c.CouponStatus) != 0 {
			return apperr.Conflict("kupon investor sudah dibayar")
		}
		fileUUID, err := uuid.Parse(req.PaymentDocumentFileUUID)
		if err != nil {
			return apperr.Invalid("payment_document_file_uuid", "uuid", "harus berupa UUID yang valid")
		}
		fileID, err := s.files.Attach(ctx, fileUUID, "investor_coupon_schedule.payment_document", "payment_document_file_uuid")
		if err != nil {
			return err
		}
		date, actor := crud.Date(req.PaymentSettlementDate), actorName(ctx)
		if err := q.PayInvestorCoupon(ctx, sqlc.PayInvestorCouponParams{SettlementDate: date, Reference: crud.Text(req.PaymentReferenceNumber),
			FileID: &fileID, Actor: &actor, ID: c.ID}); err != nil {
			return database.MapError("pay investor coupon", err)
		}
		kind := ScheduleCoupon
		if c.TransactionType != nil {
			kind = *c.TransactionType
		}
		if err := s.addPaid(ctx, *c.BondID, *c.BondCouponScheduleID, kind, deref(c.CouponValue), date); err != nil {
			return err
		}
		return s.record(ctx, "investor_coupon_schedule.pay", "investor_coupon_schedule", id, "",
			"POST /investor-coupon-schedules/{uuid}/pay")
	})
}

// PayAgentFee settles a draft agent fee; the payment proof must be attached first.
func (s *BondTxService) PayAgentFee(ctx context.Context, id uuid.UUID, expected time.Time, req dto.AgentFeePaymentRequest) error {
	return s.tx.WithinTx(ctx, func(ctx context.Context) error {
		q := s.tx.Queries(ctx)
		f, err := q.LockAgentFee(ctx, id)
		if err != nil {
			return database.MapError("lock agent fee", err)
		}
		if _, err := s.fees.Get(ctx, id); err != nil {
			return err
		}
		if !f.UpdatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		if statusOf(f.Status) != 0 {
			return apperr.Conflict("hanya fee agen draft yang dapat dibayar")
		}
		if f.PaymentDocumentFileID == nil {
			return apperr.Invalid("payment_document_file_uuid", "required", "lampirkan bukti pembayaran terlebih dahulu")
		}
		if err := q.PayAgentFee(ctx, sqlc.PayAgentFeeParams{ID: f.ID, PaymentDate: crud.Date(req.PaymentDate),
			Column3: *crud.Decimal(req.PaidValue), TaxPaidValue: crud.Decimal(req.TaxPaidValue), Column5: actorName(ctx)}); err != nil {
			return database.MapError("pay agent fee", err)
		}
		return s.record(ctx, "agent_fee_transaction.pay", "agent_fee_transaction", id, "dibayar="+req.PaidValue,
			"POST /agent-fee-transactions/{uuid}/pay")
	})
}

// View returns the current API representation of a bond-module record,
// masking PII for actors without pii:read.
func (s *BondTxService) View(ctx context.Context, kind string, id uuid.UUID) (any, time.Time, error) {
	switch kind {
	case "bond":
		return anyView(BondResource(s.d).View(ctx, s.audit, id))
	case "bond_trade":
		return anyView(BondTradeResource(s.d).View(ctx, s.audit, id))
	case "bond_coupon_schedule":
		return anyView(BondCouponScheduleResource(s.d).View(ctx, s.audit, id))
	case "investor_coupon_schedule":
		return anyView(InvestorCouponScheduleResource(s.d).View(ctx, s.audit, id))
	default:
		return anyView(AgentFeeTransactionResource(s.d).View(ctx, s.audit, id))
	}
}

func anyView[Resp any](r Resp, t time.Time, err error) (any, time.Time, error) { return r, t, err }
