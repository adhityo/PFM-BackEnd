package router

import (
	"sort"
	"strings"
	"testing"
)

// errorFields returns "field:rule" of a validation/permission error, sorted.
func errorFields(r resp) []string {
	var out []string
	d, _ := r.body["error"].(map[string]any)["details"].([]any)
	for _, f := range d {
		f := f.(map[string]any)
		out = append(out, f["field"].(string)+":"+f["rule"].(string))
	}
	sort.Strings(out)
	return out
}

// TestPortfolioSetup covers POST /portfolios/setup: a portfolio ready for
// trading in one transaction, all-or-nothing, with the CRUD rules and permissions.
func TestPortfolioSetup(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	b := setupBooks(e, admin)
	stockClass := e.scalar(t, "SELECT uuid::text FROM asset_class WHERE asset_class_code = 'STOCK'")
	fundClass := e.scalar(t, "SELECT uuid::text FROM asset_class WHERE asset_class_code = 'MF-ASSET'")
	bank := e.create(admin, "/banks", map[string]any{"bank_name": "Bank BRI"})["uuid"].(string)
	count := func(table string) string {
		return e.scalar(t, "SELECT count(*)::text FROM "+table+" WHERE deleted_at IS NULL")
	}

	body := func(code string) map[string]any {
		return map[string]any{
			"portfolio": map[string]any{"portfolio_name": "Fund " + code, "portfolio_code": code, "client_uuid": b.client},
			"asset_classes": []any{
				map[string]any{"asset_class_uuid": stockClass, "buy_account_uuid": b.stockAcc,
					"capital_gain_account_uuid": b.gain, "capital_loss_account_uuid": b.loss,
					"cash_accounts": []any{
						map[string]any{"trade_type_uuid": b.buy, "finance_account_uuid": b.cash},
						map[string]any{"trade_type_uuid": b.sell, "finance_account_uuid": b.cash}}},
				map[string]any{"asset_class_uuid": fundClass, "buy_account_uuid": b.fundAcc},
			},
			"banks": []any{map[string]any{"bank_uuid": bank, "account_number": "0011223344", "account_name": "Fund " + code}},
		}
	}

	r := e.expect(e.do("POST", "/api/v1/portfolios/setup", admin, body("F2")), 201, "setup")
	out := r.data()
	pf := out["portfolio"].(map[string]any)
	if r.header.Get("Location") != "/api/v1/portfolios/"+pf["uuid"].(string) || r.header.Get("ETag") == "" || pf["client_name"] != "Dana Pensiun" {
		t.Fatalf("setup response: %v %v", r.header, pf)
	}
	maps, structs, banks := out["asset_class_finance_accounts"].([]any), out["portfolio_accounting_structures"].([]any), out["portfolio_banks"].([]any)
	if len(maps) != 2 || len(structs) != 2 || len(banks) != 1 || maps[0].(map[string]any)["asset_class_code"] != "STOCK" ||
		maps[0].(map[string]any)["portfolio_code"] != "F2" {
		t.Fatalf("setup rows: %v", out)
	}
	// Every row is audited as a create made through the setup route.
	if got := e.scalar(t, `SELECT string_agg(log_action, ',' ORDER BY id) FROM audit_outbox
		WHERE action_query = 'POST /portfolios/setup'`); got != "portfolio.create,asset_class_finance_account.create,"+
		"portfolio_accounting_structure.create,portfolio_accounting_structure.create,asset_class_finance_account.create,portfolio_bank.create" {
		t.Fatalf("audit = %s", got)
	}

	// The new portfolio settles trades right away.
	trade := e.create(admin, "/stock-trades", map[string]any{"portfolio_uuid": pf["uuid"], "stock_uuid": b.stock,
		"broker_uuid": b.broker, "trade_type_uuid": b.buy, "stock_trade_date": "2026-11-10", "price": "1000", "quantity_lot": "1"})
	e.act(admin, "/stock-trades/"+trade["uuid"].(string), "settle", 200)

	// Every field error is reported at once, at its place in the request; nothing is written.
	bad := body("F3")
	bad["portfolio"].(map[string]any)["portfolio_name"] = ""
	ac := bad["asset_classes"].([]any)
	ac[0].(map[string]any)["buy_account_uuid"] = "bukan-uuid"
	ac[0].(map[string]any)["cash_accounts"].([]any)[0].(map[string]any)["finance_account_uuid"] = ""
	ac[1].(map[string]any)["asset_class_uuid"] = ""
	bad["banks"].([]any)[0].(map[string]any)["account_number"] = ""
	before := count("portfolio")
	r = e.expect(e.do("POST", "/api/v1/portfolios/setup", admin, bad), 422, "field errors")
	if got := strings.Join(errorFields(r), " "); got != "asset_classes[0].buy_account_uuid:uuid "+
		"asset_classes[0].cash_accounts[0].finance_account_uuid:required asset_classes[1].asset_class_uuid:required "+
		"banks[0].account_number:required portfolio.portfolio_name:required" {
		t.Fatalf("field errors = %s", got)
	}
	dup := body("F3")
	dup["asset_classes"] = []any{map[string]any{"asset_class_uuid": stockClass}, map[string]any{"asset_class_uuid": stockClass}}
	if got := strings.Join(errorFields(e.expect(e.do("POST", "/api/v1/portfolios/setup", admin, dup), 422, "duplicate")), " "); got != "asset_classes:unique" {
		t.Fatalf("duplicate errors = %s", got)
	}

	// A failure after the portfolio was written rolls everything back.
	missing := body("F3")
	missing["banks"].([]any)[0].(map[string]any)["bank_uuid"] = "00000000-0000-4000-8000-000000000000"
	maps0, structs0 := count("asset_class_finance_account"), count("portfolio_accounting_structure")
	if got := strings.Join(errorFields(e.expect(e.do("POST", "/api/v1/portfolios/setup", admin, missing), 422, "unknown bank")), " "); got != "banks[0].bank_uuid:exists" {
		t.Fatalf("unknown bank errors = %s", got)
	}
	if count("portfolio") != before || count("asset_class_finance_account") != maps0 || count("portfolio_accounting_structure") != structs0 {
		t.Fatal("failed setup left rows behind")
	}
	r = e.expect(e.do("POST", "/api/v1/portfolios/setup", admin, body("F2")), 409, "duplicate code")
	if msg := r.body["error"].(map[string]any)["message"].(string); !strings.HasPrefix(msg, "portfolio: ") {
		t.Fatalf("conflict message = %q", msg)
	}

	// Each kind of row needs its own create permission; scope applies to the portfolio.
	clerk := e.userWith(admin, "PFCLERK", "pf.clerk", "Clerk-Pass-123", b.client, "portfolio:create", "portfolio:read")
	r = e.expect(e.do("POST", "/api/v1/portfolios/setup", clerk, body("F4")), 403, "missing child permissions")
	if got := strings.Join(errorFields(r), " "); got != "permission:asset_class_finance_account:create "+
		"permission:portfolio_accounting_structure:create permission:portfolio_bank:create" {
		t.Fatalf("permission errors = %s", got)
	}
	only := map[string]any{"portfolio": map[string]any{"portfolio_name": "Fund F4", "portfolio_code": "F4", "client_uuid": b.client}}
	e.expect(e.do("POST", "/api/v1/portfolios/setup", clerk, only), 201, "portfolio only")
	other := e.create(admin, "/clients", map[string]any{"client_name": "Lain", "client_code": "OTH"})["uuid"].(string)
	foreign := map[string]any{"portfolio": map[string]any{"portfolio_name": "Fund F5", "portfolio_code": "F5", "client_uuid": other}}
	e.expect(e.do("POST", "/api/v1/portfolios/setup", clerk, foreign), 403, "foreign client")
	if n := e.scalar(t, "SELECT count(*)::text FROM portfolio WHERE portfolio_code = 'F5'"); n != "0" {
		t.Fatal("out-of-scope setup was written")
	}
}
