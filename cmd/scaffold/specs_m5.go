package main

// Tenant scope through the member's client (member.client_id, migration 00127).
const (
	scopeViaMember          = "(SELECT sm.client_id FROM member sm WHERE sm.id = t.member_id)"
	scopeViaMemberPortfolio = "(SELECT sm.client_id FROM member sm JOIN member_portfolio mp ON mp.member_id = sm.id WHERE mp.id = t.member_portfolio_id)"
)

// Period defaults: lines take month/year from their invoice or payout batch.
const (
	invoiceMonth = "COALESCE(sqlc.narg('month')::integer, (SELECT i.month FROM invoice i WHERE i.id = sqlc.narg('invoice_id')))"
	invoiceYear  = "COALESCE(sqlc.narg('year')::integer, (SELECT i.year FROM invoice i WHERE i.id = sqlc.narg('invoice_id')))"
	batchMonth   = "COALESCE(sqlc.narg('month')::integer, (SELECT b.month FROM benefit_payout_batch b WHERE b.id = sqlc.narg('benefit_payout_batch_id')))"
	batchYear    = "COALESCE(sqlc.narg('year')::integer, (SELECT b.year FROM benefit_payout_batch b WHERE b.id = sqlc.narg('benefit_payout_batch_id')))"
)

var (
	invoiceStatus = []string{"draft", "issued", "paid", "cancelled"}
	premiumStatus = []string{"unpaid", "paid"}
	payoutStatus  = []string{"draft", "approved", "paid", "cancelled"}
	memberPII     = []string{"identity_number", "npwp", "bank_account_number", "mobile_phone", "birthdate"}
	dependantPII  = []string{"identity_number", "npwp", "bank_account_number", "handphone", "birthdate"}
	period        = map[string]string{"month": "min=1,max=12", "year": "min=2000,max=2100"}
)

func withPeriod(v map[string]string) map[string]string {
	for k, tag := range period {
		v[k] = tag
	}
	return v
}

func init() {
	moduleSpecs["pension"] = []*Spec{
		{Table: "member_group", Label: "kelompok peserta", Required: []string{"member_group_name"},
			Search: []string{"member_group_name", "member_group_number"},
			Sorts:  []string{"member_group_number", "member_group_name"}, DefaultSort: "member_group_number",
			Validate: map[string]string{"member_group_number": "code"}},
		{Table: "member", Label: "peserta", Scope: "t.client_id", NestedUnder: []string{"client_id"},
			Required: []string{"member_name", "client_id"}, Check: "checkMember", AfterWrite: "assignMemberNumber",
			Search: []string{"member_name", "member_number", "pension_number"},
			Sorts:  []string{"member_number", "member_name"}, DefaultSort: "-created_at",
			ReadOnly: []string{"member_number", "total_pension_premium_value"},
			PII:      memberPII,
			Validate: map[string]string{"email": "email", "npwp": "npwp", "mobile_phone": "phone_id", "status": "max=2",
				"latest_salary": "dgte=0", "latest_allowance": "dgte=0", "latest_pension_premium": "dgte=0",
				"pension_benefit_value": "dgte=0"}},
		{Table: "dependant", Label: "tanggungan peserta", Scope: scopeViaMember, NestedUnder: []string{"member_id"},
			Required: []string{"member_id", "dependant_name"}, PII: dependantPII,
			Validate: map[string]string{"email": "email", "npwp": "npwp", "handphone": "phone_id", "pension_benefit_value": "dgte=0"}},
		{Table: "tax_report", Label: "bukti potong pajak", Scope: scopeViaMember, NestedUnder: []string{"member_id", "dependant_id"},
			Required: []string{"member_id", "tax_report_number"}, Check: "checkTaxReport",
			Sorts: []string{"tax_report_number"}, DefaultSort: "-created_at"},

		{Table: "member_portfolio", Label: "rekening unit peserta", Scope: scopeViaMember,
			NestedUnder: []string{"member_id", "portfolio_id"}, Required: []string{"member_id", "portfolio_id"},
			ReadOnly: []string{"total_unit", "current_net_asset_value", "total_investment", "latest_transaction_date"},
			Check:    "checkMemberPortfolio"},
		{Table: "member_portfolio_history", Label: "riwayat unit peserta", Kind: "read", Scope: scopeViaMemberPortfolio,
			NestedUnder: []string{"member_portfolio_id"}, Sorts: []string{"portfolio_date"}, DefaultSort: "-portfolio_date"},
		{Table: "member_portfolio_transaction", Label: "transaksi unit peserta", Scope: scopeViaMemberPortfolio,
			NestedUnder: []string{"member_portfolio_id"},
			Required:    []string{"member_portfolio_id", "trade_type_id", "transaction_date", "effective_nav"},
			Enum:        map[string][]string{"transaction_status": tradeStatus},
			Computed: map[string]string{
				"total_unit": "COALESCE(sqlc.narg('total_unit')::numeric, round((sqlc.narg('total_transaction_value')::numeric - " +
					"COALESCE(sqlc.narg('cost_of_transaction_value')::numeric, 0)) / NULLIF(sqlc.narg('effective_nav')::numeric, 0), 8))",
				"total_transaction_value": "COALESCE(sqlc.narg('total_transaction_value')::numeric, " +
					"round(sqlc.narg('total_unit')::numeric * sqlc.narg('effective_nav')::numeric, 4))",
			},
			Check: "checkMemberPortfolioTransaction", CheckDelete: "checkMemberPortfolioTransactionDelete",
			Sorts: []string{"transaction_date"}, DefaultSort: "-transaction_date",
			Validate: map[string]string{"effective_nav": "dgt=0", "total_unit": "dgt=0", "total_transaction_value": "dgt=0",
				"cost_of_transaction_value": "dgte=0"}},

		{Table: "invoice", Label: "tagihan iuran", Scope: "t.client_id", NestedUnder: []string{"client_id"},
			Required: []string{"client_id", "year", "month"},
			Enum:     map[string][]string{"invoice_status": invoiceStatus},
			ReadOnly: []string{"invoice_number", "invoice_value", "invoice_total_value", "total_member", "billing_description"},
			Check:    "checkInvoice", CheckDelete: "checkInvoiceDelete", AfterWrite: "refreshInvoice",
			Search: []string{"invoice_number"}, Sorts: []string{"invoice_number", "invoice_date"}, DefaultSort: "-invoice_date",
			Validate: withPeriod(map[string]string{"invoice_tax_value": "dgte=0"})},
		{Table: "member_premium_payment", Label: "iuran peserta", Scope: scopeViaMember,
			NestedUnder: []string{"member_id", "invoice_id"},
			Required:    []string{"member_id", "payment_nominal"},
			Enum:        map[string][]string{"status": premiumStatus},
			ReadOnly:    []string{"paid_nominal", "payment_date"},
			Computed:    map[string]string{"month": invoiceMonth, "year": invoiceYear},
			Check:       "checkPremium", CheckDelete: "checkPremiumDelete", AfterWrite: "refreshPremiumInvoice",
			Sorts: []string{"year", "month"}, DefaultSort: "-created_at",
			Validate: withPeriod(map[string]string{"payment_nominal": "dgte=0", "percentage": pct,
				"self_payment_nominal": "dgte=0", "allowance_payment_nominal": "dgte=0"})},

		{Table: "benefit_payout_batch", Label: "batch pembayaran manfaat", Required: []string{"year", "month"},
			Enum: map[string][]string{"payout_batch_status": payoutStatus},
			ReadOnly: []string{"batch_number", "benefit_payout_total", "total_beneficiaries", "value_description",
				"payout_generated_date", "payout_approval_date", "approval_description"},
			Check: "checkPayoutBatch", CheckDelete: "checkPayoutBatchDelete", AfterWrite: "refreshPayoutBatch",
			Search: []string{"batch_number", "benefit_payout_batch_name"},
			Sorts:  []string{"batch_number"}, DefaultSort: "-created_at",
			Validate: withPeriod(map[string]string{})},
		{Table: "member_benefit_payout", Label: "pembayaran manfaat peserta", Scope: scopeViaMember,
			NestedUnder: []string{"benefit_payout_batch_id", "member_id"},
			Required:    []string{"benefit_payout_batch_id", "member_id", "benefit_payout_value"},
			Enum:        map[string][]string{"member_benefit_payout_status": payoutStatus},
			ReadOnly:    []string{"final_payout_value", "benefit_payout_date"},
			Computed: map[string]string{"month": batchMonth, "year": batchYear,
				"final_payout_value": "round(sqlc.narg('benefit_payout_value')::numeric - COALESCE(sqlc.narg('benefit_payout_tax_value')::numeric, 0), 4)"},
			Check: "checkMemberPayout", CheckDelete: "checkMemberPayoutDelete", AfterWrite: "refreshMemberPayoutBatch",
			Validate: withPeriod(map[string]string{"benefit_payout_value": "dgte=0", "benefit_payout_tax_value": "dgte=0"})},

		{Table: "member_document", Label: "dokumen peserta", Scope: scopeViaMember, NestedUnder: []string{"member_id"},
			Required: []string{"member_id", "document_file_uuid"}, Check: "checkMemberDocument",
			Files: map[string]string{"document_file_uuid": "member_document.document"}},
	}
}
