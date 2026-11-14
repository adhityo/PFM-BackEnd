package main

// Status names by code for transaction-like tables (PRD §10.1: enums as strings).
var (
	tradeStatus      = []string{"draft", "settled", "cancelled", "reversed"}
	settlementStatus = []string{"draft", "approved", "cancelled", "reversed"}
)

// Default stock fee: the broker's buying or selling fee for the trade type.
const stockFeePct = "COALESCE(sqlc.narg('transaction_fee_percentage')::numeric, " +
	"(SELECT CASE WHEN tt.trade_type_code = 'SELL' THEN b.stock_selling_fee_percentage ELSE b.stock_buying_fee_percentage END " +
	"FROM broker b, trade_type tt WHERE b.id = sqlc.narg('broker_id') AND tt.id = sqlc.narg('trade_type_id')), 0)"

// NAV defaults to the fund's latest NAV.
const mutualFundNAV = "COALESCE(sqlc.narg('net_asset_value')::numeric, " +
	"(SELECT mf.net_asset_value FROM mutual_fund mf WHERE mf.id = sqlc.narg('mutual_fund_id')))"

func init() {
	moduleSpecs["accounting"] = []*Spec{
		{Table: "finance_account", Label: "akun keuangan", Check: "checkFinanceAccount",
			Required: []string{"finance_account_name", "finance_account_number"},
			Search:   []string{"finance_account_name", "finance_account_number"},
			Sorts:    []string{"finance_account_number", "finance_account_name"}, DefaultSort: "finance_account_number",
			Validate: map[string]string{"finance_account_number": "code"}},
		{Table: "cash_settlement_category", Label: "kategori cash settlement", Check: "checkCashSettlementCategory",
			Validate: map[string]string{"settlement_fee_percentage": pct}},
		{Table: "cash_settlement", Label: "cash settlement", Scope: "t.client_id",
			Required: []string{"cash_settlement_category_id", "cash_settlement_value", "cash_settlement_subject"},
			Enum:     map[string][]string{"cash_settlement_status": settlementStatus},
			Files:    map[string]string{"document_file_id": "cash_settlement.document"},
			Check:    "checkCashSettlement", CheckDelete: "checkCashSettlementDelete",
			Validate: map[string]string{"cash_settlement_value": "dgt=0"}},
	}

	moduleSpecs["trading"] = []*Spec{
		{Table: "stock_trade", Label: "transaksi saham", Scope: scopeViaPortfolio, NestedUnder: []string{"portfolio_id"},
			Required: []string{"portfolio_id", "stock_id", "trade_type_id", "broker_id", "stock_trade_date", "price", "quantity_lot"},
			ReadOnly: []string{"quantity_share", "trade_value", "transaction_fee_amount"},
			Enum:     map[string][]string{"stock_trade_status": tradeStatus},
			Snapshot: map[string]SnapFK{
				"stock_code":  {FKColumn: "stock_id", RefColumn: "stock_code"},
				"stock_name":  {FKColumn: "stock_id", RefColumn: "stock_name"},
				"broker_name": {FKColumn: "broker_id", RefColumn: "broker_name"},
			},
			Computed: map[string]string{
				"quantity_share":             "sqlc.narg('quantity_lot')::numeric * 100",
				"trade_value":                "round(sqlc.narg('price')::numeric * sqlc.narg('quantity_lot')::numeric * 100, 4)",
				"transaction_fee_percentage": stockFeePct,
				"transaction_fee_amount":     "round(sqlc.narg('price')::numeric * sqlc.narg('quantity_lot')::numeric * 100 * " + stockFeePct + " / 100, 4)",
			},
			Check: "checkStockTrade", CheckDelete: "checkStockTradeDelete",
			Sorts: []string{"stock_trade_date"}, DefaultSort: "-stock_trade_date",
			Validate: map[string]string{"price": "dgt=0", "quantity_lot": "dgt=0", "transaction_fee_percentage": pct}},
		{Table: "mutual_fund_trade", Label: "transaksi reksa dana", Scope: scopeViaPortfolio, NestedUnder: []string{"portfolio_id"},
			Required: []string{"portfolio_id", "mutual_fund_id", "trade_type_id", "transaction_date"},
			Enum:     map[string][]string{"status": tradeStatus},
			Snapshot: map[string]SnapFK{"mutual_fund_name": {FKColumn: "mutual_fund_id", RefColumn: "mutual_fund_name"}},
			Computed: map[string]string{
				"net_asset_value": mutualFundNAV,
				"total_unit": "COALESCE(sqlc.narg('total_unit')::numeric, round(sqlc.narg('total_investment_value')::numeric / NULLIF(" +
					mutualFundNAV + ", 0), 8))",
				"total_investment_value": "COALESCE(sqlc.narg('total_investment_value')::numeric, round(sqlc.narg('total_unit')::numeric * " +
					mutualFundNAV + ", 4))",
			},
			Check: "checkMutualFundTrade", CheckDelete: "checkMutualFundTradeDelete",
			Sorts: []string{"transaction_date"}, DefaultSort: "-transaction_date",
			Validate: map[string]string{"total_unit": "dgt=0", "total_investment_value": "dgt=0", "net_asset_value": "dgt=0",
				"transaction_fee": "dgte=0"}},
		{Table: "crypto_trade", Label: "transaksi kripto", Scope: scopeViaPortfolio, NestedUnder: []string{"portfolio_id"},
			Required: []string{"portfolio_id", "crypto_id", "trade_type_id", "trade_date", "price", "quantity"},
			ReadOnly: []string{"status", "trade_value"},
			Snapshot: map[string]SnapFK{
				"crypto_code":     {FKColumn: "crypto_id", RefColumn: "crypto_code"},
				"crypto_name":     {FKColumn: "crypto_id", RefColumn: "crypto_name"},
				"trade_type_name": {FKColumn: "trade_type_id", RefColumn: "trade_type_name"},
			},
			Computed: map[string]string{"trade_value": "round(sqlc.narg('price')::numeric * sqlc.narg('quantity')::numeric, 8)"},
			Check:    "checkCryptoTrade", CheckDelete: "checkCryptoTradeDelete",
			Sorts: []string{"trade_date"}, DefaultSort: "-trade_date",
			Validate: map[string]string{"price": "dgt=0", "quantity": "dgt=0", "transaction_fee_amount": "dgte=0"}},
	}
}
