package router

import (
	"context"
	"strings"
	"testing"
)

// TestPortfolioValuation covers the UI endpoints of a portfolio holding every
// asset class: live valuation, allocation, holdings, overview, the stored
// revaluation after price changes and trades, and tenant scope.
func TestPortfolioValuation(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	b := setupBooks(e, admin)
	ctx := context.Background()
	pf := "/portfolios/" + b.portfolio

	// Stock: 1000 shares at 1000 + 0.15% fee (average 1001.5). No closing
	// price yet, so the last settled trade price values the position.
	buy := e.create(admin, "/stock-trades", map[string]any{"portfolio_uuid": b.portfolio, "stock_uuid": b.stock,
		"broker_uuid": b.broker, "trade_type_uuid": b.buy, "stock_trade_date": "2026-11-10", "price": "1000", "quantity_lot": "10"})
	e.act(admin, "/stock-trades/"+buy["uuid"].(string), "settle", 200)
	v := e.expect(e.do("GET", "/api/v1"+pf+"/summary", admin, nil), 200, "summary").data()["valuation"].(map[string]any)
	if v["cost_value"] != "1001500" || v["market_value"] != "1000000" || v["unrealized_profit_loss"] != "-1500" {
		t.Fatalf("valuation after buy: %v", v)
	}
	if pl := e.scalar(t, "SELECT unrealized_profit_loss::text FROM portfolio WHERE uuid = $1", b.portfolio); pl != "-1500" {
		t.Fatalf("stored unrealized after settle = %s", pl)
	}

	// A closing price revalues the stored position and portfolio.
	e.create(admin, "/stock-histories", map[string]any{"stock_uuid": b.stock, "closing_date": "2026-11-15", "closing_price": "1200"})
	if mv := e.scalar(t, "SELECT market_value::text FROM stock_portfolio"); mv != "1200000" {
		t.Fatalf("stock market value after closing price = %s", mv)
	}

	// Mutual fund: 2000 units at NAV 1500, then NAV 1650.
	mf := e.create(admin, "/mutual-fund-trades", map[string]any{"portfolio_uuid": b.portfolio, "mutual_fund_uuid": b.fund,
		"trade_type_uuid": b.buy, "transaction_date": "2026-11-02", "total_investment_value": "3000000"})
	e.act(admin, "/mutual-fund-trades/"+mf["uuid"].(string), "settle", 200)
	e.create(admin, "/mutual-fund-histories", map[string]any{"mutual_fund_uuid": b.fund, "closing_date": "2026-11-20", "net_asset_value": "1650"})
	if nav := e.scalar(t, "SELECT latest_nav::text FROM mutual_fund_portfolio"); nav != "1650" {
		t.Fatalf("fund latest NAV after history = %s", nav)
	}

	// Crypto: 0.5 BTC at 100,000,000 (valued at the last settled price).
	cr := e.create(admin, "/crypto-trades", map[string]any{"portfolio_uuid": b.portfolio, "crypto_uuid": b.crypto,
		"trade_type_uuid": b.buy, "trade_date": "2026-11-03", "price": "100000000", "quantity": "0.5"})
	e.act(admin, "/crypto-trades/"+cr["uuid"].(string), "settle", 200)

	// Bond and deposito positions are maintained outside trading; a closed
	// and a rolled-over deposito are not holdings.
	for _, q := range []string{
		`INSERT INTO bond_portfolio (portfolio_id, bond_code, bond_name, nominal, average_price, inception_date)
		 SELECT id, 'FR0100', 'SUN FR0100', 100000000, 98, '2026-10-01' FROM portfolio WHERE uuid = $1`,
		`INSERT INTO deposito_portfolio (portfolio_id, total_deposito_allocation, portfolio_start_date, portfolio_end_date)
		 SELECT id, 10000000, '2026-11-01', '2099-12-31' FROM portfolio WHERE uuid = $1`,
		`INSERT INTO deposito_portfolio (portfolio_id, total_deposito_allocation, portfolio_start_date, portfolio_end_date)
		 SELECT id, 7000000, '2020-01-01', '2020-12-31' FROM portfolio WHERE uuid = $1`,
		`INSERT INTO deposito_portfolio (portfolio_id, total_deposito_allocation, portfolio_start_date, next_portfolio_id)
		 SELECT p.id, 5000000, '2026-01-01', d.id FROM portfolio p, deposito_portfolio d
		 WHERE p.uuid = $1 AND d.total_deposito_allocation = 10000000`,
	} {
		if _, err := e.pool.Exec(ctx, q, b.portfolio); err != nil {
			t.Fatal(err)
		}
	}

	sum := e.expect(e.do("GET", "/api/v1"+pf+"/summary", admin, nil), 200, "summary").data()
	if p := sum["portfolio"].(map[string]any); p["portfolio_code"] != "F1" || p["client_name"] != "Dana Pensiun" {
		t.Fatalf("summary portfolio: %v", p)
	}
	v = sum["valuation"].(map[string]any)
	want := map[string]any{"holding_count": float64(5), "cost_value": "162001500", "market_value": "162500000",
		"unrealized_profit_loss": "498500", "unrealized_return_pct": "0.328", "realized_profit_loss": "0", "total_profit_loss": "498500"}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("valuation.%s = %v, want %v", k, v[k], w)
		}
	}
	var classes []string
	for _, a := range sum["allocation"].([]any) {
		a := a.(map[string]any)
		classes = append(classes, a["asset_class"].(string)+":"+a["asset_class_label"].(string)+":"+a["weight_pct"].(string))
	}
	if got := strings.Join(classes, " "); got != "STOCK:Saham:0.7385 MF-ASSET:Reksa Dana:2.0308 CRYPTO:Kripto:30.7692 "+
		"BOND:Obligasi:60.3077 DEPOSITO:Deposito:6.1538" {
		t.Fatalf("allocation = %s", got)
	}
	if pl := e.scalar(t, "SELECT unrealized_profit_loss::text FROM portfolio WHERE uuid = $1", b.portfolio); pl != "498500" {
		t.Fatalf("stored unrealized = %s", pl)
	}

	hs := items(e.expect(e.do("GET", "/api/v1"+pf+"/holdings", admin, nil), 200, "holdings"))
	var order []string
	for _, h := range hs {
		order = append(order, h["asset_class"].(string))
	}
	if strings.Join(order, ",") != "STOCK,MF-ASSET,CRYPTO,BOND,DEPOSITO" {
		t.Fatalf("holdings order = %v", order)
	}
	st := hs[0]
	if st["instrument_code"] != "BBCA" || st["instrument_uuid"] != b.stock || st["quantity"] != "1000" || st["last_price"] != "1200" ||
		st["price_date"] != "2026-11-15" || st["unrealized_profit_loss"] != "198500" || st["unrealized_return_pct"] != "19.8203" ||
		!strings.HasPrefix(st["position_path"].(string), "/stock-portfolios/") {
		t.Fatalf("stock holding: %v", st)
	}
	e.expect(e.do("GET", "/api/v1"+st["position_path"].(string), admin, nil), 200, "position link")
	if bond := hs[3]; bond["instrument_uuid"] != nil || bond["instrument_code"] != "FR0100" || bond["market_value"] != "98000000" {
		t.Fatalf("bond holding: %v", bond)
	}
	if dep := hs[4]; dep["unrealized_return_pct"] != nil || dep["unrealized_profit_loss"] != "0" || dep["weight_pct"] != "6.1538" {
		t.Fatalf("deposito holding: %v", dep)
	}
	only := items(e.expect(e.do("GET", "/api/v1"+pf+"/holdings?asset_class=MF-ASSET", admin, nil), 200, "filtered"))
	if len(only) != 1 || only[0]["market_value"] != "3300000" || only[0]["weight_pct"] != "2.0308" {
		t.Fatalf("filtered holdings: %v", only)
	}
	e.expect(e.do("GET", "/api/v1"+pf+"/holdings?asset_class=GOLD", admin, nil), 422, "unknown asset class")
	e.expect(e.do("GET", "/api/v1"+pf+"/holdings?page=2", admin, nil), 422, "unknown parameter")
	e.expect(e.do("GET", "/api/v1/portfolios/00000000-0000-4000-8000-000000000000/summary", admin, nil), 404, "missing portfolio")

	// Overview: GET /portfolios filters and sorts, plus valuation.
	empty := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "Kosong", "portfolio_code": "F0", "client_uuid": b.client})
	ov := e.expect(e.do("GET", "/api/v1/portfolio-overviews?client_uuid="+b.client, admin, nil), 200, "overview")
	rows := items(ov)
	if len(rows) != 2 || rows[0]["uuid"] != empty["uuid"] || rows[1]["uuid"] != b.portfolio {
		t.Fatalf("overview rows: %v", rows)
	}
	if v := rows[1]["valuation"].(map[string]any); v["market_value"] != "162500000" || rows[1]["client_name"] != "Dana Pensiun" {
		t.Fatalf("overview valuation: %v", rows[1])
	}
	if v := rows[0]["valuation"].(map[string]any); v["holding_count"] != float64(0) || v["market_value"] != "0" ||
		v["unrealized_return_pct"] != nil || len(rows[0]["allocation"].([]any)) != 0 {
		t.Fatalf("empty portfolio overview: %v", rows[0])
	}
	if m := ov.body["meta"].(map[string]any); m["total"] != float64(2) {
		t.Fatalf("overview meta: %v", m)
	}
	e.expect(e.do("GET", "/api/v1/portfolio-overviews?sort=market_value", admin, nil), 422, "sort outside GET /portfolios")

	// Selling everything removes the holding; reversing brings it back.
	sell := e.create(admin, "/stock-trades", map[string]any{"portfolio_uuid": b.portfolio, "stock_uuid": b.stock,
		"broker_uuid": b.broker, "trade_type_uuid": b.sell, "stock_trade_date": "2026-11-16", "price": "1200", "quantity_lot": "10"})
	e.act(admin, "/stock-trades/"+sell["uuid"].(string), "settle", 200)
	v = e.expect(e.do("GET", "/api/v1"+pf+"/summary", admin, nil), 200, "summary after sell").data()["valuation"].(map[string]any)
	if v["holding_count"] != float64(4) || v["unrealized_profit_loss"] != "300000" || v["realized_profit_loss"] == "0" {
		t.Fatalf("valuation after selling all shares: %v", v)
	}
	if pl := e.scalar(t, "SELECT unrealized_profit_loss::text FROM portfolio WHERE uuid = $1", b.portfolio); pl != "300000" {
		t.Fatalf("stored unrealized after sell = %s", pl)
	}
	e.act(admin, "/stock-trades/"+sell["uuid"].(string), "reverse", 200)
	if pl := e.scalar(t, "SELECT unrealized_profit_loss::text FROM portfolio WHERE uuid = $1", b.portfolio); pl != "498500" {
		t.Fatalf("stored unrealized after reverse = %s", pl)
	}

	// Another client's user with portfolio:read sees neither the portfolio nor its valuation.
	other := e.create(admin, "/clients", map[string]any{"client_name": "Lain", "client_code": "OTH"})["uuid"].(string)
	outsider := e.userWith(admin, "PFREAD", "pf.lain", "Outsider-Pass-1", other, "portfolio:read")
	e.expect(e.do("GET", "/api/v1"+pf+"/summary", outsider, nil), 404, "foreign summary")
	e.expect(e.do("GET", "/api/v1"+pf+"/holdings", outsider, nil), 404, "foreign holdings")
	if n := len(items(e.expect(e.do("GET", "/api/v1/portfolio-overviews", outsider, nil), 200, "foreign overview"))); n != 0 {
		t.Fatalf("outsider sees %d portfolios", n)
	}
	noPerm := e.userWith(admin, "NOPF", "no.pf", "Outsider-Pass-2", b.client, "stock_trade:read")
	e.expect(e.do("GET", "/api/v1"+pf+"/summary", noPerm, nil), 403, "summary without portfolio:read")
}
