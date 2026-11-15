package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
)

// Bond status codes (bond.bond_status).
const (
	BondDraft    int16 = 0
	BondOffering int16 = 1
	BondActive   int16 = 2
	BondMatured  int16 = 3
)

// Bond trade codes follow the legacy data: 0 pending, 1 approved, -1 cancelled.
const (
	BondTradeDraft     int16 = 0
	BondTradeApproved  int16 = 1
	BondTradeCancelled int16 = -1
)

var rePlaceholder = regexp.MustCompile(`\$?\{([A-Za-z]+)\}`)

// renderNumber fills {name} and ${name} placeholders (see validator number_template).
func renderNumber(tpl string, vars map[string]string) string {
	return rePlaceholder.ReplaceAllStringFunc(tpl, func(m string) string {
		name := rePlaceholder.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return ""
	})
}

func pad(n int32, width int) string { return fmt.Sprintf("%0*d", width, n) }

func orDefault(s *string, def string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return def
	}
	return *s
}

var errNoConfiguration = apperr.Unavailable("konfigurasi belum di-seed (jalankan `migrate seed`)", nil)

// ---------------------------------------------------------------- bond

// checkBond: maturity after issuance; financial terms are frozen once active.
func checkBond(_ context.Context, _ Deps, req *dto.BondRequest, cur *sqlc.GetBondByUUIDRow) error {
	if err := checkDateRange(req.IssuanceDate, req.MaturityDate, "maturity_date"); err != nil {
		return err
	}
	if req.IssuanceDate == req.MaturityDate {
		return apperr.Invalid("maturity_date", "gtfield", "tanggal jatuh tempo harus setelah tanggal terbit")
	}
	if cur == nil || statusOf(cur.BondStatus) < BondActive {
		return nil
	}
	same := dateEq(cur.IssuanceDate, req.IssuanceDate) && dateEq(cur.MaturityDate, req.MaturityDate) &&
		cur.Coupon != nil && crud.Decimal(req.Coupon) != nil && cur.Coupon.Equal(*crud.Decimal(req.Coupon)) &&
		cur.CouponPaymentPeriode != nil && req.CouponPaymentPeriode != nil && *cur.CouponPaymentPeriode == *req.CouponPaymentPeriode
	if !same {
		return apperr.Conflict("kupon, periode kupon, tanggal terbit dan jatuh tempo tidak dapat diubah setelah obligasi aktif")
	}
	return nil
}

func dateEq(t *time.Time, s string) bool { return t != nil && t.Format(time.DateOnly) == s }

// snapshotBond records bond_history on creation and on price/rating changes.
func snapshotBond(ctx context.Context, d Deps, before, after *sqlc.GetBondByUUIDRow) error {
	if after == nil {
		return nil
	}
	desc := "dibuat"
	if before != nil {
		priceChanged := (before.BondPrice == nil) != (after.BondPrice == nil) ||
			(before.BondPrice != nil && after.BondPrice != nil && !before.BondPrice.Equal(*after.BondPrice))
		if !priceChanged && sameID(before.RatingID, after.RatingID) {
			return nil
		}
		desc = "perubahan harga/rating"
	}
	return snapshotBondRow(ctx, d, after.ID, desc)
}

func snapshotBondRow(ctx context.Context, d Deps, id int64, desc string) error {
	err := d.TX.Queries(ctx).SnapshotBond(ctx, sqlc.SnapshotBondParams{Uuid: crud.NewID(), Description: &desc, Actor: actorName(ctx), ID: id})
	return database.MapError("snapshot bond", err)
}

// refreshBondRating sets bond.rating_id to the latest rating and snapshots it.
func refreshBondRating(ctx context.Context, d Deps, before, after *sqlc.GetBondRatingByUUIDRow) error {
	ids := parents(func(r *sqlc.GetBondRatingByUUIDRow) *int64 { return r.BondID }, before, after)
	return refreshEach(ids, func(id *int64) error {
		if err := d.TX.Queries(ctx).RefreshBondRating(ctx, id); err != nil {
			return err
		}
		return snapshotBondRow(ctx, d, *id, "rating")
	})
}

func checkBondCouponSchedule(_ context.Context, _ Deps, _ *dto.BondCouponScheduleRequest, cur *sqlc.GetBondCouponScheduleByUUIDRow) error {
	if cur != nil && statusOf(cur.Status) != 0 {
		return apperr.Conflict("jadwal kupon yang sudah dibayar tidak dapat diubah")
	}
	return nil
}

func checkBondCouponScheduleDelete(_ context.Context, _ Deps, cur *sqlc.GetBondCouponScheduleByUUIDRow) error {
	return checkBondCouponSchedule(context.Background(), Deps{}, nil, cur)
}

// ---------------------------------------------------------------- investor & agent

// assignInvestorNumber gives a new investor its number from the configured
// template and counter (row-locked, gapless).
func assignInvestorNumber(ctx context.Context, d Deps, before, after *sqlc.GetInvestorByUUIDRow) error {
	if before != nil || after == nil {
		return nil
	}
	q := d.TX.Queries(ctx)
	row, err := q.NextInvestorNumber(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNoConfiguration
	}
	if err != nil {
		return database.MapError("investor counter", err)
	}
	number := renderNumber(orDefault(row.InvestorNumberTemplate, "INV${year}${counter}"),
		map[string]string{"year": time.Now().UTC().Format("2006"), "counter": pad(*row.LatestInvestorCounter, 5)})
	return database.MapError("set investor number", q.SetInvestorNumber(ctx, sqlc.SetInvestorNumberParams{
		Number: &number, Counter: row.LatestInvestorCounter, ID: after.ID}))
}

func checkMarketingAgent(ctx context.Context, d Deps, req *dto.MarketingAgentRequest, cur *sqlc.GetMarketingAgentByUUIDRow) error {
	var self *int64
	if cur != nil {
		self = &cur.ID
	}
	return checkTreeParent(ctx, d, "marketing_agent", "leader_agent_id", "leader_agent_uuid", req.LeaderAgentUUID, self)
}

func assignAgentNumber(ctx context.Context, d Deps, before, after *sqlc.GetMarketingAgentByUUIDRow) error {
	if before != nil || after == nil {
		return nil
	}
	q := d.TX.Queries(ctx)
	row, err := q.NextAgentNumber(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNoConfiguration
	}
	if err != nil {
		return database.MapError("agent counter", err)
	}
	number := renderNumber(orDefault(row.AgentNumberTemplate, "AGT${counter}"),
		map[string]string{"year": time.Now().UTC().Format("2006"), "counter": pad(*row.LatestAgentCounter, 5)})
	return database.MapError("set agent number", q.SetAgentNumber(ctx, sqlc.SetAgentNumberParams{Number: &number, ID: after.ID}))
}

// checkBankAccount: an account belongs to exactly one investor or agent.
func checkBankAccount(_ context.Context, _ Deps, req *dto.BankAccountRequest, _ *sqlc.GetBankAccountByUUIDRow) error {
	if (req.InvestorUUID == "") == (req.MarketingAgentUUID == "") {
		return apperr.Invalid("investor_uuid", "xor", "isi tepat satu: investor_uuid atau marketing_agent_uuid")
	}
	return nil
}

// ---------------------------------------------------------------- bond trade

// checkBondTrade: drafts only; the bond must be open; nominal meets the
// minimum denomination; the bank account belongs to the investor.
func checkBondTrade(ctx context.Context, d Deps, req *dto.BondTradeRequest, cur *sqlc.GetBondTradeByUUIDRow) error {
	if cur != nil && statusOf(cur.BondTradeStatus) != BondTradeDraft {
		return apperr.Conflict("hanya subscription draft yang dapat diubah")
	}
	q := d.TX.Queries(ctx)
	bondID, err := database.RefID(ctx, d.TX, "bond", req.BondUUID, "bond_uuid")
	if err != nil {
		return err
	}
	st, err := q.GetBondState(ctx, *bondID)
	if err != nil {
		return database.MapError("bond state", err)
	}
	if s := statusOf(st.BondStatus); s != BondOffering && s != BondActive {
		return apperr.Invalid("bond_uuid", "status", "obligasi belum dibuka untuk subscription (status harus offering/active)")
	}
	if nominal := crud.Decimal(req.Nominal); st.MinimumDenomination != nil && nominal != nil && nominal.LessThan(*st.MinimumDenomination) {
		return apperr.Invalid("nominal", "min", "nominal di bawah denominasi minimum "+st.MinimumDenomination.String())
	}
	if req.BankAccountUUID != "" {
		accID, err := database.RefID(ctx, d.TX, "bank_account", req.BankAccountUUID, "bank_account_uuid")
		if err != nil {
			return err
		}
		owner, err := q.BankAccountInvestor(ctx, *accID)
		if err != nil {
			return database.MapError("bank account", err)
		}
		investorID, err := database.RefID(ctx, d.TX, "investor", req.InvestorUUID, "investor_uuid")
		if err != nil {
			return err
		}
		if !sameID(owner, investorID) {
			return apperr.Invalid("bank_account_uuid", "owner", "rekening bukan milik investor ini")
		}
	}
	return nil
}

func checkBondTradeDelete(_ context.Context, _ Deps, cur *sqlc.GetBondTradeByUUIDRow) error {
	if statusOf(cur.BondTradeStatus) != BondTradeDraft {
		return apperr.Conflict("hanya subscription draft yang dapat dihapus")
	}
	return nil
}

// snapshotBondTrade keeps the previous values of every edit (bond_trade_data_history).
func snapshotBondTrade(ctx context.Context, d Deps, before, after *sqlc.GetBondTradeByUUIDRow) error {
	if before == nil || after == nil {
		return nil
	}
	err := d.TX.Queries(ctx).SnapshotBondTrade(ctx, sqlc.SnapshotBondTradeParams{Uuid: crud.NewID(),
		PreviousBondCode: before.BondCode, PreviousBondName: before.BondName, PreviousInvestmentLength: before.InvestmentLength,
		PreviousInvestorName: before.InvestorName, PreviousInvestorNumber: before.InvestorNumber,
		PreviousMarketingAgentCode: before.MarketingAgentCode, PreviousMarketingAgentName: before.MarketingAgentName,
		PreviousNominal: before.Nominal, PreviousSettlementDate: before.SettlementDate,
		PreviousSubscriptionNumber: before.SubscriptionNumber, PreviousTradeDate: before.TradeDate,
		PreviousTransactionFee: before.TransactionFee, Actor: actorName(ctx), ID: after.ID})
	return database.MapError("snapshot bond trade", err)
}

func checkAgentFee(_ context.Context, _ Deps, _ *dto.AgentFeeTransactionRequest, cur *sqlc.GetAgentFeeTransactionByUUIDRow) error {
	if cur != nil && statusOf(cur.Status) != 0 {
		return apperr.Conflict("hanya fee agen draft yang dapat diubah")
	}
	return nil
}

func checkAgentFeeDelete(_ context.Context, _ Deps, cur *sqlc.GetAgentFeeTransactionByUUIDRow) error {
	return checkAgentFee(context.Background(), Deps{}, nil, cur)
}
