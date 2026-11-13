package router

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type m2Case struct {
	table, path, field string // field is changed on update
	body               func(p map[string]string) map[string]any
}

// m2Parents creates the master data the M2 resources point at.
func m2Parents(e *env, token string) map[string]string {
	p := map[string]string{}
	mk := func(key, path string, body map[string]any) {
		p[key] = e.create(token, path, body)["uuid"].(string)
	}
	mk("sector", "/sectors", map[string]any{"sector_name": "Pertanian", "sector_code": "AGRI"})
	mk("sub_sector", "/sub-sectors", map[string]any{"sub_sector_name": "Perkebunan", "sub_sector_code": "PLNT", "sector_uuid": p["sector"]})
	mk("other_sector", "/sectors", map[string]any{"sector_name": "Keuangan", "sector_code": "FIN"})
	mk("stock_type", "/stock-types", map[string]any{"stock_type_name": "Defensive", "stock_type_code": "DEF"})
	mk("rating_agency", "/rating-agencies", map[string]any{"rating_agency_name": "Pefindo", "rating_agency_code": "PEF"})
	mk("bank_class", "/bank-classes", map[string]any{"bank_class_name": "BUMN", "bank_class_code": "BUMN"})
	mk("bank", "/banks", map[string]any{"bank_name": "Bank Mandiri", "bank_class_uuid": p["bank_class"]})
	mk("mutual_fund_category", "/mutual-fund-categories", map[string]any{"mutual_fund_category_name": "Saham", "mutual_fund_category_code": "EQ"})
	mk("trade_type", "/trade-types", map[string]any{"trade_type_name": "Beli", "trade_type_code": "BUY"})
	mk("asset_class", "/asset-classes", map[string]any{"asset_class_name": "Saham", "asset_class_code": "STOCK"})
	mk("stock_size_classification", "/stock-size-classifications", map[string]any{
		"stock_size_classification_name": "Big Cap", "stock_size_classification_code": "BIG"})
	mk("property_asset_type", "/property-asset-types", map[string]any{"property_asset_type_name": "Tanah", "property_asset_type_code": "LAND"})

	// finance_account belongs to the accounting module (M3); create it directly.
	var fa string
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO finance_account (uuid, finance_account_name, finance_account_number) VALUES ($1, 'Kas', '10001') RETURNING uuid::text`,
		uuid.New()).Scan(&fa); err != nil {
		e.t.Fatal(err)
	}
	p["finance_account"] = fa
	return p
}

func m2Cases() []m2Case {
	b := func(m map[string]any) func(map[string]string) map[string]any {
		return func(p map[string]string) map[string]any {
			out := map[string]any{}
			for k, v := range m {
				if s, ok := v.(string); ok && strings.HasPrefix(s, "$") {
					v = p[s[1:]]
				}
				out[k] = v
			}
			return out
		}
	}
	return []m2Case{
		// market
		{"crypto", "/cryptos", "crypto_name", b(map[string]any{"crypto_name": "Bitcoin", "crypto_code": "BTC", "status": "active"})},
		{"crypto_exchange", "/crypto-exchanges", "crypto_exchange_name", b(map[string]any{"crypto_exchange_name": "Indodax", "crypto_exchange_code": "IDX"})},
		{"crypto_listing_exchange", "/crypto-listing-exchanges", "description", b(map[string]any{"crypto_exchange_uuid": "$crypto_exchange", "description": "listing"})},
		{"broker", "/brokers", "broker_name", b(map[string]any{"broker_name": "Mirae", "broker_code": "YP", "stock_buying_fee_percentage": "0.15"})},
		{"broker_api_setting", "/broker-api-settings", "broker_api_setting_name", b(map[string]any{"broker_api_setting_name": "FIX",
			"broker_uuid": "$broker", "protocol": "https", "ip_address": "10.1.2.3", "ip_address_list": []string{"10.0.0.0/8", "192.168.1.10"}})},
		{"investment_manager", "/investment-managers", "investment_manager_name", b(map[string]any{"investment_manager_name": "Danareksa", "investment_manager_code": "DIM"})},
		{"indices", "/indices", "indices_name", b(map[string]any{"indices_name": "LQ45", "indices_code": "LQ45"})},
		{"emitent", "/emitents", "emitent_name", b(map[string]any{"emitent_name": "PT Contoh Tbk", "email": "ir@contoh.co.id",
			"npwp": "012345678901234", "sector_uuid": "$sector", "sub_sector_uuid": "$sub_sector"})},
		{"stock", "/stocks", "stock_name", b(map[string]any{"stock_name": "Astra Agro", "stock_code": "AALI", "sector_uuid": "$sector",
			"sub_sector_uuid": "$sub_sector", "stock_type_uuid": "$stock_type"})},
		{"stock_type_detail", "/stock-type-details", "description", b(map[string]any{"stock_uuid": "$stock", "stock_type_uuid": "$stock_type"})},
		{"indices_stock", "/indices-stocks", "weight", b(map[string]any{"indices_uuid": "$indices", "stock_uuid": "$stock", "weight": "12.5", "registration_date": "2026-01-02"})},
		{"mutual_fund", "/mutual-funds", "mutual_fund_name", b(map[string]any{"mutual_fund_name": "RD Saham", "mutual_fund_code": "RDS",
			"mutual_fund_category_uuid": "$mutual_fund_category", "investment_manager_uuid": "$investment_manager"})},
		{"deposito", "/depositos", "deposito_name", b(map[string]any{"deposito_name": "Deposito 3M", "deposito_code": "DEP3",
			"bank_uuid": "$bank", "tenor": 3, "placement_value": "1000000000"})},
		{"stock_history", "/stock-histories", "closing_price", b(map[string]any{"stock_uuid": "$stock", "closing_date": "2026-11-10", "closing_price": "15200"})},
		{"stock_beta_history", "/stock-beta-histories", "beta_value", b(map[string]any{"stock_uuid": "$stock", "register_date": "2026-11-10",
			"beta_value": "1.05", "data_provider_uuid": "$rating_agency"})},
		{"stock_fundamental_history", "/stock-fundamental-histories", "return_on_equity", b(map[string]any{"stock_uuid": "$stock",
			"register_date": "2026-09-30", "return_on_equity": "12.5"})},
		{"stock_size_classification_history", "/stock-size-classification-histories", "description", b(map[string]any{"stock_uuid": "$stock",
			"stock_size_classification_uuid": "$stock_size_classification", "size_change_date": "2026-01-01"})},
		{"indices_history", "/indices-histories", "closing_value", b(map[string]any{"indices_uuid": "$indices", "closing_date": "2026-11-10", "closing_value": "950.5"})},
		{"mutual_fund_history", "/mutual-fund-histories", "net_asset_value", b(map[string]any{"mutual_fund_uuid": "$mutual_fund",
			"closing_date": "2026-11-10", "net_asset_value": "1523.12"})},
		{"deposito_interest_history", "/deposito-interest-histories", "interest_rate", b(map[string]any{"deposito_uuid": "$deposito", "interest_rate": "5.25"})},
		// portfolio
		{"client", "/clients", "client_name", b(map[string]any{"client_name": "PT Sumber Energi", "client_code": "SEAM", "email": "ops@seam.co.id"})},
		{"portfolio", "/portfolios", "portfolio_name", b(map[string]any{"portfolio_name": "Top Fund", "portfolio_code": "TOPF",
			"client_uuid": "$client", "investment_manager_uuid": "$investment_manager", "portfolio_status": "active"})},
		{"property_asset", "/property-assets", "property_asset_name", b(map[string]any{"property_asset_name": "Ruko", "property_asset_code": "RK1",
			"property_asset_type_uuid": "$property_asset_type", "purchase_value": "5000000000"})},
		{"crypto_trade_plan", "/crypto-trade-plans", "crypto_pair_symbol", b(map[string]any{"crypto_uuid": "$crypto",
			"crypto_exchange_uuid": "$crypto_exchange", "crypto_pair_symbol": "BTC/IDR"})},
		{"portfolio_bank", "/portfolio-banks", "account_name", b(map[string]any{"portfolio_uuid": "$portfolio", "bank_uuid": "$bank",
			"account_number": "0330010185", "account_name": "PT Sumber Energi"})},
		{"portfolio_budget", "/portfolio-budgets", "allocation_budget", b(map[string]any{"portfolio_uuid": "$portfolio",
			"asset_class_uuid": "$asset_class", "budget_year": 2026, "allocation_budget": "1000000000"})},
		{"portfolio_cost_structure", "/portfolio-cost-structures", "transaction_cost", b(map[string]any{"portfolio_uuid": "$portfolio",
			"trade_type_uuid": "$trade_type", "transaction_cost": "0.15", "finance_account_uuid": "$finance_account"})},
		{"portfolio_accounting_structure", "/portfolio-accounting-structures", "description", b(map[string]any{"portfolio_uuid": "$portfolio",
			"asset_class_uuid": "$asset_class", "trade_type_uuid": "$trade_type", "finance_account_uuid": "$finance_account"})},
		{"portfolio_note", "/portfolio-notes", "note_subject", b(map[string]any{"portfolio_uuid": "$portfolio", "note_subject": "Rapat komite", "notes": "Catatan"})},
		{"asset_class_finance_account", "/asset-class-finance-accounts", "sell_account_uuid", b(map[string]any{"portfolio_uuid": "$portfolio",
			"asset_class_uuid": "$asset_class", "buy_account_uuid": "$finance_account"})},
		{"placement", "/placements", "placement_subject", b(map[string]any{"placement_number": "PL-001", "placement_subject": "Penempatan",
			"placement_value": "1000000", "start_date": "2026-01-01", "end_date": "2026-12-31", "investment_manager_uuid": "$investment_manager"})},
		{"deposito_portfolio", "/deposito-portfolios", "net_interest_rate", b(map[string]any{"deposito_uuid": "$deposito", "portfolio_uuid": "$portfolio",
			"total_deposito_allocation": "500000000", "portfolio_start_date": "2026-01-01", "tenor": 3, "net_interest_rate": "5"})},
	}
}

func updatedValue(field string, p map[string]string) any {
	switch {
	case strings.HasSuffix(field, "_uuid"):
		return p["finance_account"]
	case field == "weight", field == "interest_rate", field == "net_interest_rate", field == "return_on_equity":
		return "7.5"
	case field == "closing_price", field == "closing_value", field == "net_asset_value", field == "beta_value",
		field == "allocation_budget", field == "transaction_cost":
		return "99"
	default:
		return "Diubah"
	}
}

func TestM2CRUDAllResources(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	p := m2Parents(e, admin)

	cases := m2Cases()
	for _, tc := range cases {
		body := tc.body(p)
		created := e.create(admin, tc.path, body)
		id := created["uuid"].(string)
		p[tc.table] = id
		item := tc.path + "/" + id
		tag := e.etag(admin, item)
		if e.expect(e.do("GET", "/api/v1"+tc.path+"?per_page=5", admin, nil), 200, "list "+tc.path).body["meta"].(map[string]any)["total"].(float64) < 1 {
			t.Fatalf("%s: empty list", tc.table)
		}
		body[tc.field] = updatedValue(tc.field, p)
		upd := e.expect(e.do("PUT", "/api/v1"+item, admin, body, "If-Match", tag), 200, "update "+tc.path).data()
		if got, want := fmt.Sprint(upd[tc.field]), fmt.Sprint(body[tc.field]); !strings.HasPrefix(got, strings.TrimRight(want, "0")) {
			t.Fatalf("%s: %s = %v, want %v", tc.table, tc.field, got, want)
		}
		e.expect(e.do("PUT", "/api/v1"+item, admin, body, "If-Match", tag), 412, "stale "+tc.path)
	}

	// Read-only resources exist but cannot be written.
	for _, path := range []string{"/stock-portfolios", "/bond-portfolios", "/mutual-fund-portfolios", "/crypto-portfolios",
		"/stock-portfolio-histories", "/bond-portfolio-histories", "/mutual-fund-portfolio-histories",
		"/deposito-portfolio-histories", "/portfolio-budget-histories", "/deposito-histories"} {
		e.expect(e.do("GET", "/api/v1"+path, admin, nil), 200, "read "+path)
		if r := e.do("POST", "/api/v1"+path, admin, map[string]any{}); r.status < 400 {
			t.Fatalf("POST %s must be rejected, got %d", path, r.status)
		}
	}
	// Deposito and placement changes are snapshotted.
	hist := e.expect(e.do("GET", "/api/v1/depositos/"+p["deposito"]+"/deposito-histories", admin, nil), 200, "deposito history")
	if hist.body["meta"].(map[string]any)["total"].(float64) != 2 {
		t.Fatalf("deposito history (create+update) = %s", hist.raw)
	}

	// Delete children before parents.
	for i := len(cases) - 1; i >= 0; i-- {
		tc := cases[i]
		item := tc.path + "/" + p[tc.table]
		e.expect(e.do("DELETE", "/api/v1"+item, admin, nil, "If-Match", e.etag(admin, item)), 204, "delete "+tc.path)
	}
}

func TestTimeseriesRefreshAndImport(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	stock := e.create(admin, "/stocks", map[string]any{"stock_name": "Bank BCA", "stock_code": "BBCA"})["uuid"].(string)
	latest := func() (any, any) {
		d := e.expect(e.do("GET", "/api/v1/stocks/"+stock, admin, nil), 200, "stock").data()
		return d["latest_closing_price"], d["latest_closing_date"]
	}
	h1 := e.create(admin, "/stock-histories", map[string]any{"stock_uuid": stock, "closing_date": "2026-11-10", "closing_price": "100"})
	h2 := e.create(admin, "/stock-histories", map[string]any{"stock_uuid": stock, "closing_date": "2026-11-11", "closing_price": "110"})
	if price, date := latest(); price != "110" || date != "2026-11-11" {
		t.Fatalf("latest after inserts = %v %v", price, date)
	}
	if h2["stock_code"] != "BBCA" {
		t.Fatalf("snapshot stock_code: %v", h2)
	}
	// Moving the newest row back in time makes the other row the latest.
	h2Path := "/stock-histories/" + h2["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+h2Path, admin, map[string]any{"stock_uuid": stock, "closing_date": "2026-11-09", "closing_price": "110"},
		"If-Match", e.etag(admin, h2Path)), 200, "move history")
	if price, date := latest(); price != "100" || date != "2026-11-10" {
		t.Fatalf("latest after update = %v %v", price, date)
	}
	h1Path := "/stock-histories/" + h1["uuid"].(string)
	e.expect(e.do("DELETE", "/api/v1"+h1Path, admin, nil, "If-Match", e.etag(admin, h1Path)), 204, "delete history")
	if price, _ := latest(); price != "110" {
		t.Fatalf("latest after delete = %v", price)
	}
	e.expect(e.do("POST", "/api/v1/stock-histories", admin, map[string]any{"stock_uuid": stock, "closing_date": "2026-11-09", "closing_price": "1"}), 409, "duplicate date")

	csvReq := func(body string, ctype string) resp {
		req := httptest.NewRequest("POST", "/api/v1/stock-histories/import", strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		req.Header.Set("Authorization", "Bearer "+admin)
		return e.send(req)
	}
	count := func() float64 {
		return e.expect(e.do("GET", "/api/v1/stocks/"+stock+"/stock-histories", admin, nil), 200, "count").body["meta"].(map[string]any)["total"].(float64)
	}
	before := count()
	good := "stock_uuid,closing_date,closing_price\n" + stock + ",2026-11-12,120\n" + stock + ",2026-11-13,125\n" + stock + ",2026-11-14,130\n"
	r := e.expect(csvReq(good, "text/csv"), 201, "import")
	if r.data()["imported"].(float64) != 3 || count() != before+3 {
		t.Fatalf("import: %s", r.raw)
	}
	if price, date := latest(); price != "130" || date != "2026-11-14" {
		t.Fatalf("latest after import = %v %v", price, date)
	}
	bad := "stock_uuid,closing_date,closing_price\n" + stock + ",2026-11-15,140\n" + stock + ",2026-11-16,-5\n"
	r = e.expect(csvReq(bad, "text/csv"), 422, "invalid row")
	if !strings.Contains(string(r.raw), "rows[3].closing_price") {
		t.Fatalf("row error not located: %s", r.raw)
	}
	dup := "stock_uuid,closing_date,closing_price\n" + stock + ",2026-11-17,150\n" + stock + ",2026-11-14,151\n"
	r = e.expect(csvReq(dup, "text/csv"), 409, "duplicate in import")
	if !strings.Contains(string(r.raw), "baris 3") {
		t.Fatalf("conflict not located: %s", r.raw)
	}
	if count() != before+3 {
		t.Fatal("failed imports must not write anything")
	}
	e.expect(csvReq("stock_uuid,password\n"+stock+",x\n", "text/csv"), 422, "unknown column")
	e.expect(csvReq(good, "application/json"), 415, "wrong content type")

	// Other parents refresh the same way.
	mf := e.create(admin, "/mutual-funds", map[string]any{"mutual_fund_name": "RD", "mutual_fund_code": "RD1"})["uuid"].(string)
	e.create(admin, "/mutual-fund-histories", map[string]any{"mutual_fund_uuid": mf, "closing_date": "2026-11-10", "net_asset_value": "1500"})
	if nav := e.expect(e.do("GET", "/api/v1/mutual-funds/"+mf, admin, nil), 200, "mf").data()["net_asset_value"]; nav != "1500" {
		t.Fatalf("mutual fund NAV = %v", nav)
	}
}

func TestPortfolioTenantScope(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	ca := e.create(admin, "/clients", map[string]any{"client_name": "Client A", "client_code": "CA"})["uuid"].(string)
	cb := e.create(admin, "/clients", map[string]any{"client_name": "Client B", "client_code": "CB"})["uuid"].(string)
	pa := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "PA", "portfolio_code": "PA", "client_uuid": ca})["uuid"].(string)
	pb := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "PB", "portfolio_code": "PB", "client_uuid": cb})["uuid"].(string)
	e.create(admin, "/portfolio-notes", map[string]any{"portfolio_uuid": pb, "note_subject": "rahasia B"})

	// A client user with full portfolio permissions, bound to client A.
	role := e.create(admin, "/roles", map[string]any{"role_code": "CLIENT-OPS", "role_name": "Client Ops", "status": "active"})
	var ids []string
	for _, code := range []string{"client:read", "portfolio:read", "portfolio:create", "portfolio:update", "portfolio_note:read", "portfolio_note:create"} {
		var id string
		if err := e.pool.QueryRow(context.Background(), "SELECT uuid::text FROM action WHERE action_code=$1", code).Scan(&id); err != nil {
			t.Fatal(code, err)
		}
		ids = append(ids, id)
	}
	rolePath := "/roles/" + role["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+rolePath+"/actions", admin, map[string]any{"action_uuids": ids}, "If-Match", e.etag(admin, rolePath)), 200, "grant")
	u := e.create(admin, "/users", map[string]any{"username": "client.a", "email": "a@client.test", "role_uuid": role["uuid"],
		"client_uuid": ca, "status": "active"})
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": u["activation_key"].(string),
		"new_password": "Client-A-Pass-1"}), 204, "activate")
	ua, _ := e.login("client.a", "Client-A-Pass-1")

	list := e.expect(e.do("GET", "/api/v1/portfolios", ua, nil), 200, "scoped list")
	if items := list.body["data"].([]any); len(items) != 1 || items[0].(map[string]any)["uuid"] != pa {
		t.Fatalf("client A must only see PA: %s", list.raw)
	}
	clients := e.expect(e.do("GET", "/api/v1/clients", ua, nil), 200, "scoped clients")
	if items := clients.body["data"].([]any); len(items) != 1 || items[0].(map[string]any)["uuid"] != ca {
		t.Fatalf("client A must only see itself: %s", clients.raw)
	}
	e.expect(e.do("GET", "/api/v1/portfolios/"+pb, ua, nil), 404, "other client's portfolio")
	e.expect(e.do("GET", "/api/v1/portfolios/"+pb+"/portfolio-notes", ua, nil), 200, "nested other client")
	if n := e.do("GET", "/api/v1/portfolios/"+pb+"/portfolio-notes", ua, nil).body["data"].([]any); len(n) != 0 {
		t.Fatal("notes of another client leaked through nested route")
	}
	e.expect(e.do("POST", "/api/v1/portfolios", ua, map[string]any{"portfolio_name": "X", "portfolio_code": "X1", "client_uuid": cb}), 403, "create for other client")
	e.expect(e.do("POST", "/api/v1/portfolios", ua, map[string]any{"portfolio_name": "X", "portfolio_code": "X2"}), 403, "create without client")
	e.expect(e.do("POST", "/api/v1/portfolio-notes", ua, map[string]any{"portfolio_uuid": pb, "note_subject": "intrusi"}), 403, "note on other client's portfolio")
	e.create(ua, "/portfolios", map[string]any{"portfolio_name": "PA2", "portfolio_code": "PA2", "client_uuid": ca})
	e.expect(e.do("PUT", "/api/v1/portfolios/"+pb, ua, map[string]any{"portfolio_name": "hijack", "portfolio_code": "PB"}, "If-Match", `"x"`), 404, "update other client's portfolio")

	var notesB int
	_ = e.pool.QueryRow(context.Background(), "SELECT count(*) FROM portfolio_note n JOIN portfolio p ON p.id = n.portfolio_id WHERE p.uuid = $1", pb).Scan(&notesB)
	if notesB != 1 {
		t.Fatalf("rejected writes must roll back, notes of PB = %d", notesB)
	}
	// Parent portfolio must belong to the same client.
	e.expect(e.do("POST", "/api/v1/portfolios", admin, map[string]any{"portfolio_name": "Child", "portfolio_code": "CH",
		"client_uuid": ca, "parent_portfolio_uuid": pb}), 422, "cross-client parent")
}

func TestPlacementDetailsAndDepositoRollover(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	pl := e.create(admin, "/placements", map[string]any{"placement_number": "PL-9", "placement_subject": "Penempatan",
		"placement_value": "1000000", "start_date": "2026-01-01", "end_date": "2026-12-31"})["uuid"].(string)
	detPath := "/api/v1/placements/" + pl + "/details"
	tag := e.expect(e.do("GET", detPath, admin, nil), 200, "details").header.Get("ETag")
	item := func(date, value string) map[string]any {
		return map[string]any{"placement_detail_date": date, "placement_value": value}
	}
	e.expect(e.do("PUT", detPath, admin, map[string]any{"details": []any{item("2026-02-01", "600000"), item("2026-03-01", "500000")}},
		"If-Match", tag), 422, "total over value")
	e.expect(e.do("PUT", detPath, admin, map[string]any{"details": []any{item("2027-02-01", "1")}}, "If-Match", tag), 422, "date out of range")
	r := e.expect(e.do("PUT", detPath, admin, map[string]any{"details": []any{item("2026-02-01", "600000"), item("2026-03-01", "300000")}},
		"If-Match", tag), 200, "replace details")
	if n := len(r.data()["details"].([]any)); n != 2 {
		t.Fatalf("details = %d", n)
	}
	e.expect(e.do("PUT", detPath, admin, map[string]any{"details": []any{}}, "If-Match", tag), 412, "stale details")
	r = e.expect(e.do("PUT", detPath, admin, map[string]any{"details": []any{item("2026-04-01", "100")}}, "If-Match", r.header.Get("ETag")), 200, "replace again")
	if n := len(r.data()["details"].([]any)); n != 1 {
		t.Fatalf("details after replace = %d", n)
	}

	// Deposito placement lifecycle.
	bank := e.create(admin, "/banks", map[string]any{"bank_name": "Bank BRI"})["uuid"].(string)
	dep := e.create(admin, "/depositos", map[string]any{"deposito_name": "Dep", "deposito_code": "D1", "bank_uuid": bank})["uuid"].(string)
	pf := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "P", "portfolio_code": "P1"})["uuid"].(string)
	dp := e.create(admin, "/deposito-portfolios", map[string]any{"deposito_uuid": dep, "portfolio_uuid": pf,
		"total_deposito_allocation": "500000000", "portfolio_start_date": "2026-01-15", "tenor": 3, "net_interest_rate": "5.5"})["uuid"].(string)
	dpPath := "/deposito-portfolios/" + dp
	e.expect(e.do("POST", "/api/v1"+dpPath+"/rollover", admin, map[string]any{}, "If-Match", e.etag(admin, dpPath)), 422, "rollover without end/start")
	next := e.expect(e.do("POST", "/api/v1"+dpPath+"/rollover", admin, map[string]any{"start_date": "2026-04-15", "net_interest_rate": "5.75"},
		"If-Match", e.etag(admin, dpPath)), 201, "rollover").data()
	if next["portfolio_start_date"] != "2026-04-15" || next["portfolio_end_date"] != "2026-07-15" ||
		next["previous_portfolio_uuid"] != dp || next["net_interest_rate"] != "5.75" {
		t.Fatalf("rollover result: %v", next)
	}
	old := e.expect(e.do("GET", "/api/v1"+dpPath, admin, nil), 200, "old").data()
	if old["next_portfolio_uuid"] != next["uuid"] || old["portfolio_end_date"] != "2026-04-15" {
		t.Fatalf("old placement not linked: %v", old)
	}
	e.expect(e.do("PUT", "/api/v1"+dpPath, admin, map[string]any{"deposito_uuid": dep, "portfolio_uuid": pf,
		"total_deposito_allocation": "1", "portfolio_start_date": "2026-01-15", "tenor": 3}, "If-Match", e.etag(admin, dpPath)), 409, "edit rolled-over")
	e.expect(e.do("POST", "/api/v1"+dpPath+"/rollover", admin, map[string]any{"start_date": "2026-04-15"}, "If-Match", e.etag(admin, dpPath)), 409, "rollover twice")
	nextPath := "/deposito-portfolios/" + next["uuid"].(string)
	e.expect(e.do("POST", "/api/v1"+nextPath+"/withdraw", admin, map[string]any{"withdrawal_date": "2026-03-01"},
		"If-Match", e.etag(admin, nextPath)), 422, "withdraw before start")
	w := e.expect(e.do("POST", "/api/v1"+nextPath+"/withdraw", admin, map[string]any{"withdrawal_date": "2026-06-01"},
		"If-Match", e.etag(admin, nextPath)), 200, "withdraw").data()
	if w["portfolio_end_date"] != "2026-06-01" {
		t.Fatalf("withdraw: %v", w)
	}
	e.expect(e.do("DELETE", "/api/v1"+dpPath, admin, nil, "If-Match", e.etag(admin, dpPath)), 409, "delete linked placement")
	var snaps int
	_ = e.pool.QueryRow(context.Background(), "SELECT count(*) FROM deposito_portfolio_history").Scan(&snaps)
	if snaps < 4 { // create, rollover(old+new), withdraw
		t.Fatalf("deposito portfolio snapshots = %d", snaps)
	}
}
