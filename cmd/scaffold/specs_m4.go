package main

// Tenant scope through the bond's client (PRD §12.3).
const (
	scopeViaBond           = "(SELECT sb.client_id FROM bond sb WHERE sb.id = t.bond_id)"
	scopeViaBondTrade      = "(SELECT sb.client_id FROM bond sb JOIN bond_trade x ON x.bond_id = sb.id WHERE x.id = t.bond_trade_id)"
	scopeViaCouponSchedule = "(SELECT sb.client_id FROM bond sb JOIN bond_coupon_schedule x ON x.bond_id = sb.id WHERE x.id = t.bond_coupon_schedule_id)"
)

var (
	bondStatus     = []string{"draft", "offering", "active", "matured"}
	couponStatus   = []string{"unpaid", "paid"}
	agentFeeStatus = []string{"draft", "paid", "cancelled"}
	// Legacy bond_trade codes: 0 pending, 1 approved, -1 cancelled/rejected.
	bondTradeStatus = map[int16]string{0: "draft", 1: "approved", -1: "cancelled"}
	approvalStatus  = map[int16]string{0: "pending", 1: "approved", 2: "rejected"}
	investorPII     = []string{"identity_number", "npwp", "bank_account_number", "mobile_phone"}
	agentPII        = []string{"identity_number", "npwp", "bank_account_number", "handphone", "birthdate"}
)

func init() {
	moduleSpecs["bond"] = []*Spec{
		{Table: "bond", Label: "obligasi", Scope: "t.client_id", Check: "checkBond", AfterWrite: "snapshotBond",
			NestedUnder: []string{"client_id"},
			Required:    []string{"bond_name", "bond_code", "issuance_date", "maturity_date", "coupon", "coupon_payment_periode"},
			Enum:        map[string][]string{"bond_status": bondStatus},
			ReadOnly: []string{"total_coupon", "total_fund", "total_matured_value", "total_paid_coupon", "total_paid_fee",
				"total_revolving_value", "total_transaction_fee", "latest_certificate_counter", "next_coupon_date",
				"last_coupon_date", "bond_activate_sync_status", "bond_expire_sync_status", "bond_offering_sync_status",
				"remote_product_id"},
			Snapshot: map[string]SnapFK{"emitent_name": {FKColumn: "emitent_id", RefColumn: "emitent_name"}},
			Sorts:    []string{"bond_code", "bond_name", "issuance_date"}, DefaultSort: "-issuance_date",
			Validate: map[string]string{"coupon": pct, "yield_to_maturity": pct, "minimum_denomination": "dgt=0",
				"coupon_payment_periode": "min=1,max=366", "bond_price": "dgte=0", "default_transaction_fee": "dgte=0"}},
		{Table: "bond_history", Label: "riwayat obligasi", Kind: "read", Scope: scopeViaBond, NestedUnder: []string{"bond_id"}},
		{Table: "bond_rating", Label: "rating obligasi", Scope: scopeViaBond, NestedUnder: []string{"bond_id"},
			Required: []string{"bond_id", "rating_id", "rating_date"}, AfterWrite: "refreshBondRating",
			Snapshot: map[string]SnapFK{
				"bond_name":          {FKColumn: "bond_id", RefColumn: "bond_name"},
				"rating_agency_name": {FKColumn: "rating_agency_id", RefColumn: "rating_agency_name"},
				"rating_value":       {FKColumn: "rating_id", RefColumn: "rating_code"},
			},
			Sorts: []string{"rating_date"}, DefaultSort: "-rating_date"},
		{Table: "bond_coupon_schedule", Label: "jadwal kupon obligasi", Scope: scopeViaBond, NestedUnder: []string{"bond_id"},
			Required: []string{"bond_id", "coupon_date", "coupon_rate", "transaction_type"},
			Enum:     map[string][]string{"status": couponStatus},
			ReadOnly: []string{"coupon_value", "coupon_paid_value", "settlement_date"},
			Check:    "checkBondCouponSchedule", CheckDelete: "checkBondCouponScheduleDelete",
			Sorts: []string{"coupon_date"}, DefaultSort: "coupon_date",
			Validate: map[string]string{"coupon_rate": pct, "transaction_type": "min=1,max=3"}},
		{Table: "bond_periode_distribution", Label: "distribusi periode obligasi", Scope: scopeViaBond,
			NestedUnder: []string{"bond_id"}, Required: []string{"bond_id", "bond_periode_name"},
			Snapshot: map[string]SnapFK{"bond_name": {FKColumn: "bond_id", RefColumn: "bond_name"}},
			Validate: map[string]string{"bond_number_template": "number_template", "coupon_rate": pct}},

		{Table: "investor", Label: "investor", Required: []string{"investor_name"}, AfterWrite: "assignInvestorNumber",
			Search: []string{"investor_name", "investor_number"},
			Sorts:  []string{"investor_number", "investor_name"}, DefaultSort: "-created_at",
			ReadOnly: []string{"investor_number", "investor_number_counter", "investor_unique_number", "is_synchronized", "synchronize_notes"},
			PII:      investorPII,
			Validate: map[string]string{"email": "email", "npwp": "npwp", "mobile_phone": "phone_id"}},
		{Table: "investor_data_history", Label: "perubahan data investor", Kind: "read", NestedUnder: []string{"investor_id"},
			EnumMap: map[string]map[int16]string{"approval_status": approvalStatus},
			PII: append(append([]string{}, investorPII...), "previous_identity_number", "previous_npwp",
				"previous_bank_account_number", "previous_mobile_phone")},
		{Table: "marketing_agent", Label: "agen pemasaran", Required: []string{"marketing_agent_name"},
			Check: "checkMarketingAgent", AfterWrite: "assignAgentNumber",
			Search: []string{"marketing_agent_name", "marketing_agent_number"},
			Sorts:  []string{"marketing_agent_number", "marketing_agent_name"}, DefaultSort: "-created_at",
			ReadOnly: []string{"marketing_agent_number", "is_synchronized", "latest_synchronized_time", "marketing_unique_number", "synchronize_notes"},
			Snapshot: map[string]SnapFK{
				"leader_code": {FKColumn: "leader_agent_id", RefColumn: "marketing_agent_number"},
				"leader_name": {FKColumn: "leader_agent_id", RefColumn: "marketing_agent_name"},
			},
			PII:      agentPII,
			Validate: map[string]string{"email": "email", "npwp": "npwp", "handphone": "phone_id"}},
		{Table: "marketing_agent_data_history", Label: "perubahan data agen", Kind: "read", NestedUnder: []string{"marketing_agent_id"},
			EnumMap: map[string]map[int16]string{"approval_status": approvalStatus},
			PII: append(append([]string{}, agentPII...), "previous_identity_number", "previous_npwp", "previous_handphone",
				"previous_birthdate")},
		{Table: "bank_account", Label: "rekening bank", NestedUnder: []string{"investor_id", "marketing_agent_id"},
			Required: []string{"bank_account_name", "bank_account_number", "bank_name"}, Check: "checkBankAccount",
			PII: []string{"bank_account_number"}, Validate: map[string]string{"bank_account_number": "numeric"}},

		{Table: "bond_trade", Label: "subscription obligasi", Scope: scopeViaBond, NestedUnder: []string{"bond_id", "investor_id"},
			Required: []string{"bond_id", "investor_id", "nominal", "trade_date"},
			EnumMap:  map[string]map[int16]string{"bond_trade_status": bondTradeStatus},
			ReadOnly: []string{"certificate_counter", "certificate_number", "certificate_uploaded", "approved_by", "approved_time",
				"approval_description", "is_synchronized", "synchronize_notes", "remote_subscription_id", "bond_trade_unique_code",
				"validation_number", "agreement_number"},
			Snapshot: map[string]SnapFK{
				"bond_code":            {FKColumn: "bond_id", RefColumn: "bond_code"},
				"bond_name":            {FKColumn: "bond_id", RefColumn: "bond_name"},
				"investor_name":        {FKColumn: "investor_id", RefColumn: "investor_name"},
				"investor_number":      {FKColumn: "investor_id", RefColumn: "investor_number"},
				"marketing_agent_code": {FKColumn: "marketing_agent_id", RefColumn: "marketing_agent_number"},
				"marketing_agent_name": {FKColumn: "marketing_agent_id", RefColumn: "marketing_agent_name"},
				"bank_account_name":    {FKColumn: "bank_account_id", RefColumn: "bank_account_name"},
				"bank_account_number":  {FKColumn: "bank_account_id", RefColumn: "bank_account_number"},
				"bank_branch":          {FKColumn: "bank_account_id", RefColumn: "bank_branch_name"},
				"bank_name":            {FKColumn: "bank_account_id", RefColumn: "bank_name"},
				"broker_name":          {FKColumn: "broker_id", RefColumn: "broker_name"},
			},
			PII:   []string{"bank_account_number"},
			Check: "checkBondTrade", CheckDelete: "checkBondTradeDelete", AfterWrite: "snapshotBondTrade",
			Sorts: []string{"trade_date", "certificate_number"}, DefaultSort: "-trade_date",
			Validate: map[string]string{"nominal": "dgt=0", "transaction_fee_percentage": pct, "investment_length": "max=600"}},
		{Table: "bond_trade_data_history", Label: "riwayat perubahan subscription", Kind: "read", Scope: scopeViaBond,
			NestedUnder: []string{"bond_trade_id"}},
		{Table: "investor_coupon_schedule", Label: "kupon investor", Kind: "read", Scope: scopeViaBondTrade,
			NestedUnder: []string{"bond_trade_id", "bond_coupon_schedule_id"},
			Enum:        map[string][]string{"coupon_status": couponStatus}},
		{Table: "payment_document", Label: "dokumen pembayaran", Scope: scopeViaCouponSchedule,
			NestedUnder: []string{"bond_coupon_schedule_id"}, Required: []string{"bond_coupon_schedule_id", "document_name"},
			Files: map[string]string{"document_file_id": "payment_document.document"}},
		{Table: "agent_fee_transaction", Label: "fee agen", Scope: scopeViaBond, NestedUnder: []string{"marketing_agent_id"},
			Required: []string{"marketing_agent_id", "bond_id", "outstanding_value"},
			Enum:     map[string][]string{"status": agentFeeStatus},
			ReadOnly: []string{"paid_value", "payment_date", "tax_paid_value"},
			Snapshot: map[string]SnapFK{
				"agent_name":          {FKColumn: "marketing_agent_id", RefColumn: "marketing_agent_name"},
				"agent_number":        {FKColumn: "marketing_agent_id", RefColumn: "marketing_agent_number"},
				"agent_level":         {FKColumn: "marketing_agent_id", RefColumn: "agent_level"},
				"email":               {FKColumn: "marketing_agent_id", RefColumn: "email"},
				"identity_number":     {FKColumn: "marketing_agent_id", RefColumn: "identity_number"},
				"npwp":                {FKColumn: "marketing_agent_id", RefColumn: "npwp"},
				"bank":                {FKColumn: "marketing_agent_id", RefColumn: "bank"},
				"bank_account_name":   {FKColumn: "marketing_agent_id", RefColumn: "bank_account_name"},
				"bank_account_number": {FKColumn: "marketing_agent_id", RefColumn: "bank_account_number"},
				"bank_branch":         {FKColumn: "marketing_agent_id", RefColumn: "bank_branch"},
			},
			Files: map[string]string{"payment_document_file_id": "agent_fee_transaction.payment_document",
				"tax_document_file_id": "agent_fee_transaction.tax_document"},
			PII:   []string{"identity_number", "npwp", "bank_account_number"},
			Check: "checkAgentFee", CheckDelete: "checkAgentFeeDelete",
			Validate: map[string]string{"outstanding_value": "dgte=0", "tax_value": "dgte=0", "leader_commision": pct,
				"personal_commision": pct}},
	}
}
