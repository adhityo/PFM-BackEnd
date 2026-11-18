// Package permission holds the permission codes (table `action`) used to
// guard routes and the static part of the catalog.
package permission

import (
	"strings"

	"pfm-backend/internal/platform/auth"
)

const (
	UserRead          = "app_user:read"
	UserCreate        = "app_user:create"
	UserUpdate        = "app_user:update"
	UserDelete        = "app_user:delete"
	UserResetPassword = "app_user:reset_password" //nolint:gosec // permission code, not a credential
	UserLock          = "app_user:lock"

	RoleRead         = "role:read"
	RoleCreate       = "role:create"
	RoleUpdate       = "role:update"
	RoleDelete       = "role:delete"
	RoleActionRead   = "role_action:read"
	RoleActionUpdate = "role_action:update"

	ActionRead   = "action:read"
	ActionCreate = "action:create"
	ActionUpdate = "action:update"
	ActionDelete = "action:delete"

	ActivityLogRead = "activity_log:read"

	FileRead   = "file_object:read"
	FileCreate = "file_object:create"
	FileDelete = "file_object:delete"

	ConfigurationRead   = "configuration:read"
	ConfigurationUpdate = "configuration:update"

	PlacementDetailRead   = "placement:read"
	PlacementDetailUpdate = "placement:update"
	DepositoRollover      = "deposito_portfolio:rollover"
	DepositoWithdraw      = "deposito_portfolio:withdraw"

	FinanceTransactionRead    = "finance_transaction:read"
	FinanceTransactionCreate  = "finance_transaction:create"
	FinanceTransactionReverse = "finance_transaction:reverse"
	CashSettlementApprove     = "cash_settlement:approve"
	CashSettlementCancel      = "cash_settlement:cancel"
	CashSettlementReverse     = "cash_settlement:reverse"

	BondOffer                   = "bond:offer"
	BondActivate                = "bond:activate"
	BondGenerateCouponSchedule  = "bond:generate_coupon_schedule"
	BondTradeApprove            = "bond_trade:approve"
	BondTradeCancel             = "bond_trade:cancel"
	BondCouponSchedulePay       = "bond_coupon_schedule:pay"
	InvestorCouponSchedulePay   = "investor_coupon_schedule:pay"
	AgentFeePay                 = "agent_fee_transaction:pay"
	InvestorChangeCreate        = "investor_data_history:create"
	InvestorChangeApprove       = "investor_data_history:approve"
	InvestorChangeReject        = "investor_data_history:reject"
	MarketingAgentChangeCreate  = "marketing_agent_data_history:create"
	MarketingAgentChangeApprove = "marketing_agent_data_history:approve"
	MarketingAgentChangeReject  = "marketing_agent_data_history:reject"

	InvoiceGenerate       = "invoice:generate"
	InvoiceIssue          = "invoice:issue"
	InvoiceCancel         = "invoice:cancel"
	PremiumPay            = "member_premium_payment:pay"
	PayoutGenerate        = "benefit_payout_batch:generate"
	PayoutApprove         = "benefit_payout_batch:approve"
	PayoutPay             = "benefit_payout_batch:pay"
	PayoutCancel          = "benefit_payout_batch:cancel"
	UnitTransactionSettle = "member_portfolio_transaction:settle"
	UnitTransactionCancel = "member_portfolio_transaction:cancel"
)

// TradeActions are the action permissions of every trade table.
var TradeActions = []string{"settle", "cancel", "reverse"}

// Permission catalog modules (auth.Permission.Module), in display order.
const (
	ModuleSystem     = "system"
	ModuleMasterdata = "masterdata"
	ModuleMarket     = "market"
	ModulePortfolio  = "portfolio"
	ModuleAccounting = "accounting"
	ModuleTrading    = "trading"
	ModuleBond       = "bond"
	ModulePension    = "pension"
	// ModuleCustom holds actions created through the API, e.g. frontend-only
	// switches such as "dashboard_widget:view". The backend never requires them.
	ModuleCustom = "custom"
)

// Modules lists the modules in display order with their labels.
var Modules = []struct{ Code, Label string }{
	{ModuleSystem, "Sistem & Keamanan"}, {ModuleMasterdata, "Master Data"}, {ModuleMarket, "Instrumen Pasar"},
	{ModulePortfolio, "Portofolio"}, {ModuleAccounting, "Akuntansi"}, {ModuleTrading, "Trading"},
	{ModuleBond, "Obligasi"}, {ModulePension, "Dana Pensiun"}, {ModuleCustom, "Kustom"},
}

// ResourceLabels labels resources without a CRUD resource of their own
// (CRUD resources carry their label).
var ResourceLabels = map[string]string{
	"app_user": "pengguna", "role": "role", "role_action": "permission role", "action": "katalog permission",
	"activity_log": "log aktivitas", "pii": "data pribadi", "file_object": "file", "configuration": "konfigurasi",
	"finance_transaction": "jurnal",
}

var tradeVerbs = map[string]string{"settle": "Settle", "cancel": "Batalkan", "reverse": "Balik"}

// Static returns the hand-defined permissions. CRUD resources add their own
// <table>:read|create|update|delete entries (see router.Catalog).
func Static() []auth.Permission {
	sys := append(auth.CRUD("app_user", "pengguna"),
		auth.Permission{Code: UserResetPassword, Name: "Reset kredensial pengguna"},
		auth.Permission{Code: UserLock, Name: "Kunci/buka kunci pengguna"},
	)
	sys = append(sys, auth.CRUD("role", "role")...)
	sys = append(sys,
		auth.Permission{Code: RoleActionRead, Name: "Lihat permission role"},
		auth.Permission{Code: RoleActionUpdate, Name: "Ubah permission role"},
	)
	sys = append(sys, auth.CRUD("action", "katalog permission")...)
	sys = append(sys,
		auth.Permission{Code: ActivityLogRead, Name: "Lihat log aktivitas"},
		auth.Permission{Code: auth.PermPIIRead, Name: "Lihat data pribadi tanpa masking"},
		auth.Permission{Code: FileRead, Name: "Lihat/unduh file milik pengguna lain"},
		auth.Permission{Code: FileCreate, Name: "Unggah file"},
		auth.Permission{Code: FileDelete, Name: "Hapus file pending milik pengguna lain"},
		auth.Permission{Code: ConfigurationRead, Name: "Lihat konfigurasi sistem"},
		auth.Permission{Code: ConfigurationUpdate, Name: "Ubah konfigurasi sistem"},
	)
	p := auth.InModule(ModuleSystem, sys...)
	p = append(p, auth.InModule(ModulePortfolio,
		auth.Permission{Code: DepositoRollover, Name: "Rollover penempatan deposito"},
		auth.Permission{Code: DepositoWithdraw, Name: "Cairkan penempatan deposito"},
	)...)
	p = append(p, auth.InModule(ModuleAccounting,
		auth.Permission{Code: FinanceTransactionRead, Name: "Lihat jurnal"},
		auth.Permission{Code: FinanceTransactionCreate, Name: "Posting jurnal manual"},
		auth.Permission{Code: FinanceTransactionReverse, Name: "Balik jurnal manual"},
		auth.Permission{Code: CashSettlementApprove, Name: "Setujui cash settlement"},
		auth.Permission{Code: CashSettlementCancel, Name: "Batalkan cash settlement"},
		auth.Permission{Code: CashSettlementReverse, Name: "Balik cash settlement"},
	)...)
	var trades []auth.Permission
	for _, table := range []string{"stock_trade", "mutual_fund_trade", "crypto_trade"} {
		for _, a := range TradeActions {
			trades = append(trades, auth.Permission{Code: table + ":" + a, Name: tradeVerbs[a] + " " + table})
		}
	}
	p = append(p, auth.InModule(ModuleTrading, trades...)...)
	p = append(p, auth.InModule(ModuleBond,
		auth.Permission{Code: BondOffer, Name: "Buka masa offering obligasi"},
		auth.Permission{Code: BondActivate, Name: "Aktifkan obligasi"},
		auth.Permission{Code: BondGenerateCouponSchedule, Name: "Buat jadwal kupon obligasi"},
		auth.Permission{Code: BondTradeApprove, Name: "Setujui subscription obligasi"},
		auth.Permission{Code: BondTradeCancel, Name: "Batalkan subscription obligasi"},
		auth.Permission{Code: BondCouponSchedulePay, Name: "Bayar kupon obligasi (semua investor)"},
		auth.Permission{Code: InvestorCouponSchedulePay, Name: "Bayar kupon investor"},
		auth.Permission{Code: AgentFeePay, Name: "Bayar fee agen"},
		auth.Permission{Code: InvestorChangeCreate, Name: "Ajukan perubahan data investor"},
		auth.Permission{Code: InvestorChangeApprove, Name: "Setujui perubahan data investor"},
		auth.Permission{Code: InvestorChangeReject, Name: "Tolak perubahan data investor"},
		auth.Permission{Code: MarketingAgentChangeCreate, Name: "Ajukan perubahan data agen"},
		auth.Permission{Code: MarketingAgentChangeApprove, Name: "Setujui perubahan data agen"},
		auth.Permission{Code: MarketingAgentChangeReject, Name: "Tolak perubahan data agen"},
	)...)
	p = append(p, auth.InModule(ModulePension,
		auth.Permission{Code: InvoiceGenerate, Name: "Generate tagihan iuran bulanan"},
		auth.Permission{Code: InvoiceIssue, Name: "Terbitkan tagihan iuran"},
		auth.Permission{Code: InvoiceCancel, Name: "Batalkan tagihan iuran"},
		auth.Permission{Code: PremiumPay, Name: "Catat pembayaran iuran peserta"},
		auth.Permission{Code: PayoutGenerate, Name: "Generate batch pembayaran manfaat"},
		auth.Permission{Code: PayoutApprove, Name: "Setujui batch pembayaran manfaat"},
		auth.Permission{Code: PayoutPay, Name: "Tandai batch pembayaran manfaat dibayar"},
		auth.Permission{Code: PayoutCancel, Name: "Batalkan batch pembayaran manfaat"},
		auth.Permission{Code: UnitTransactionSettle, Name: "Settle transaksi unit peserta"},
		auth.Permission{Code: UnitTransactionCancel, Name: "Batalkan transaksi unit peserta"},
	)...)
	return p
}

// Label fills missing resource labels: CRUD resources label their own
// table, the action permissions of the same table reuse it.
func Label(catalog []auth.Permission) []auth.Permission {
	labels := map[string]string{}
	for k, v := range ResourceLabels {
		labels[k] = v
	}
	for _, p := range catalog {
		if p.ResourceLabel != "" {
			labels[p.Resource()] = p.ResourceLabel
		}
	}
	for i := range catalog {
		if catalog[i].ResourceLabel == "" {
			if l, ok := labels[catalog[i].Resource()]; ok {
				catalog[i].ResourceLabel = l
			} else {
				catalog[i].ResourceLabel = strings.ReplaceAll(catalog[i].Resource(), "_", " ")
			}
		}
	}
	return catalog
}
