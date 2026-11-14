package router

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
)

// books is a portfolio with a complete account mapping for STOCK, MF-ASSET and CRYPTO.
type books struct {
	portfolio, client                  string
	cash, stockAcc, fundAcc, cryptoAcc string
	gain, loss, fee, counter           string
	buy, sell                          string
	stock, broker, fund, crypto        string
}

func setupBooks(e *env, token string) books {
	b := books{}
	mk := func(path string, body map[string]any) string { return e.create(token, path, body)["uuid"].(string) }
	acc := func(no, name string) string {
		return mk("/finance-accounts", map[string]any{"finance_account_number": no, "finance_account_name": name})
	}
	b.cash, b.stockAcc, b.fundAcc, b.cryptoAcc = acc("1-100", "Kas"), acc("1-200", "Investasi Saham"), acc("1-210", "Investasi RD"), acc("1-220", "Investasi Kripto")
	b.gain, b.loss, b.fee, b.counter = acc("4-100", "Laba Realisasi"), acc("5-100", "Rugi Realisasi"), acc("5-200", "Biaya"), acc("2-100", "Utang")
	b.buy = mk("/trade-types", map[string]any{"trade_type_name": "Beli", "trade_type_code": "BUY"})
	b.sell = mk("/trade-types", map[string]any{"trade_type_name": "Jual", "trade_type_code": "SELL"})
	b.client = mk("/clients", map[string]any{"client_name": "Dana Pensiun", "client_code": "DP1"})
	b.portfolio = mk("/portfolios", map[string]any{"portfolio_name": "Fund", "portfolio_code": "F1", "client_uuid": b.client})
	for code, asset := range map[string]string{"STOCK": b.stockAcc, "MF-ASSET": b.fundAcc, "CRYPTO": b.cryptoAcc} {
		ac := mk("/asset-classes", map[string]any{"asset_class_name": code, "asset_class_code": code})
		mk("/asset-class-finance-accounts", map[string]any{"portfolio_uuid": b.portfolio, "asset_class_uuid": ac,
			"buy_account_uuid": asset, "capital_gain_account_uuid": b.gain, "capital_loss_account_uuid": b.loss})
		for _, tt := range []string{b.buy, b.sell} {
			mk("/portfolio-accounting-structures", map[string]any{"portfolio_uuid": b.portfolio, "asset_class_uuid": ac,
				"trade_type_uuid": tt, "finance_account_uuid": b.cash})
		}
	}
	b.stock = mk("/stocks", map[string]any{"stock_name": "Bank BCA", "stock_code": "BBCA"})
	b.broker = mk("/brokers", map[string]any{"broker_name": "Mirae", "broker_code": "YP",
		"stock_buying_fee_percentage": "0.15", "stock_selling_fee_percentage": "0.25"})
	b.fund = mk("/mutual-funds", map[string]any{"mutual_fund_name": "RD Saham", "mutual_fund_code": "RDS"})
	mk("/mutual-fund-histories", map[string]any{"mutual_fund_uuid": b.fund, "closing_date": "2026-11-01", "net_asset_value": "1500"})
	b.crypto = mk("/cryptos", map[string]any{"crypto_name": "Bitcoin", "crypto_code": "BTC"})
	return b
}

// act runs a trade/settlement action with the current ETag.
func (e *env) act(token, path, action string, want int) resp {
	e.t.Helper()
	return e.expect(e.do("POST", "/api/v1"+path+"/"+action, token, map[string]any{}, "If-Match", e.etag(token, path)), want, action+" "+path)
}

// journalLines returns "account:debit:credit" lines of the journals linked to a source.
func (e *env) journalLines(t *testing.T, filter string) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `
		SELECT a.finance_account_number, d.debit::text, d.credit::text
		FROM finance_transaction f
		JOIN finance_transaction_detail d ON d.finance_transaction_id = f.id
		JOIN finance_account a ON a.id = d.finance_account_id
		WHERE `+filter+` ORDER BY f.id, d.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var acc, dr, cr string
		_ = rows.Scan(&acc, &dr, &cr)
		out = append(out, fmt.Sprintf("%s:%s:%s", acc, trimDecimal(dr), trimDecimal(cr)))
	}
	return out
}

func (e *env) scalar(t *testing.T, q string, args ...any) string {
	t.Helper()
	var s *string
	if err := e.pool.QueryRow(context.Background(), q, args...).Scan(&s); err != nil {
		t.Fatal(q, err)
	}
	if s == nil {
		return "<nil>"
	}
	return trimDecimal(*s)
}

// trimDecimal drops trailing fractional zeros: "1000.5000" -> "1000.5", "10" stays "10".
func trimDecimal(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}

func TestStockTradingLifecycle(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	b := setupBooks(e, admin)
	trade := func(tt, date, price, lots string) map[string]any {
		return e.create(admin, "/stock-trades", map[string]any{"portfolio_uuid": b.portfolio, "stock_uuid": b.stock,
			"broker_uuid": b.broker, "trade_type_uuid": tt, "stock_trade_date": date, "price": price, "quantity_lot": lots})
	}
	position := func() (string, string) {
		return e.scalar(t, "SELECT stock_amount::text FROM stock_portfolio"), e.scalar(t, "SELECT average_price::text FROM stock_portfolio")
	}

	buy := trade(b.buy, "2026-11-10", "1000", "10")
	if buy["quantity_share"] != "1000" || buy["trade_value"] != "1000000" || buy["transaction_fee_amount"] != "1500" ||
		buy["transaction_fee_percentage"] != "0.15" || buy["stock_trade_status"] != "draft" || buy["stock_code"] != "BBCA" {
		t.Fatalf("computed draft: %v", buy)
	}
	buyPath := "/stock-trades/" + buy["uuid"].(string)
	settled := e.act(admin, buyPath, "settle", 200).data()
	if settled["stock_trade_status"] != "settled" {
		t.Fatalf("status after settle: %v", settled["stock_trade_status"])
	}
	if qty, avg := position(); qty != "1000" || avg != "1001.5" {
		t.Fatalf("position after buy = %s @ %s", qty, avg)
	}
	if got := e.journalLines(t, "f.stock_trade_id IS NOT NULL"); strings.Join(got, " ") != "1-200:1001500:0 1-100:0:1001500" {
		t.Fatalf("buy journal = %v", got)
	}
	e.expect(e.do("PUT", "/api/v1"+buyPath, admin, map[string]any{"portfolio_uuid": b.portfolio, "stock_uuid": b.stock,
		"broker_uuid": b.broker, "trade_type_uuid": b.buy, "stock_trade_date": "2026-11-10", "price": "1", "quantity_lot": "1"},
		"If-Match", e.etag(admin, buyPath)), 409, "edit settled")
	e.expect(e.do("DELETE", "/api/v1"+buyPath, admin, nil, "If-Match", e.etag(admin, buyPath)), 409, "delete settled")

	tooBig := trade(b.sell, "2026-11-11", "1200", "15")
	tooBigPath := "/stock-trades/" + tooBig["uuid"].(string)
	e.act(admin, tooBigPath, "settle", 422)
	if s := e.expect(e.do("GET", "/api/v1"+tooBigPath, admin, nil), 200, "after failed settle").data()["stock_trade_status"]; s != "draft" {
		t.Fatalf("failed settle must roll back, status = %v", s)
	}
	e.expect(e.do("DELETE", "/api/v1"+tooBigPath, admin, nil, "If-Match", e.etag(admin, tooBigPath)), 204, "delete draft")

	sell := trade(b.sell, "2026-11-12", "1200", "4")
	sellPath := "/stock-trades/" + sell["uuid"].(string)
	e.act(admin, sellPath, "settle", 200)
	if qty, avg := position(); qty != "600" || avg != "1001.5" {
		t.Fatalf("position after sell = %s @ %s", qty, avg)
	}
	if got := e.journalLines(t, "f.stock_trade_id = (SELECT id FROM stock_trade WHERE uuid = '"+sell["uuid"].(string)+"')"); strings.Join(got, " ") !=
		"1-100:478800:0 1-200:0:400600 4-100:0:78200" {
		t.Fatalf("sell journal = %v", got)
	}
	if pl := e.scalar(t, "SELECT realized_profit_loss::text FROM portfolio WHERE uuid = $1", b.portfolio); pl != "78200" {
		t.Fatalf("realized P/L = %s", pl)
	}

	early := trade(b.buy, "2026-11-01", "900", "1")
	e.act(admin, "/stock-trades/"+early["uuid"].(string), "settle", 409)
	e.act(admin, buyPath, "reverse", 409) // not the latest settled trade

	e.act(admin, sellPath, "reverse", 200)
	if qty, avg := position(); qty != "1000" || avg != "1001.5" {
		t.Fatalf("position after reverse = %s @ %s", qty, avg)
	}
	if pl := e.scalar(t, "SELECT realized_profit_loss::text FROM portfolio WHERE uuid = $1", b.portfolio); pl != "0" {
		t.Fatalf("realized P/L after reverse = %q", pl)
	}
	if n := e.scalar(t, "SELECT count(*)::text FROM finance_transaction WHERE transaction_status = 3"); n != "1" {
		t.Fatalf("reversed journals = %s", n)
	}
	e.act(admin, sellPath, "reverse", 409)

	e.act(admin, "/stock-trades/"+early["uuid"].(string), "cancel", 200)
	e.act(admin, "/stock-trades/"+early["uuid"].(string), "settle", 409)

	// A portfolio without account mapping cannot settle; nothing is written.
	p2 := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "P2", "portfolio_code": "P2", "client_uuid": b.client})["uuid"].(string)
	t2 := e.create(admin, "/stock-trades", map[string]any{"portfolio_uuid": p2, "stock_uuid": b.stock, "broker_uuid": b.broker,
		"trade_type_uuid": b.buy, "stock_trade_date": "2026-11-10", "price": "1000", "quantity_lot": "1"})
	r := e.act(admin, "/stock-trades/"+t2["uuid"].(string), "settle", 422)
	if !strings.Contains(string(r.raw), "STOCK") {
		t.Fatalf("missing mapping message: %s", r.raw)
	}
	if n := e.scalar(t, "SELECT count(*)::text FROM stock_portfolio p JOIN portfolio f ON f.id = p.portfolio_id WHERE f.uuid = $1", p2); n != "0" {
		t.Fatal("failed settle wrote a position")
	}

	// A user of another client cannot see or settle this portfolio's trades.
	other := e.create(admin, "/clients", map[string]any{"client_name": "Lain", "client_code": "OTH"})["uuid"].(string)
	role := e.create(admin, "/roles", map[string]any{"role_code": "TRADER", "role_name": "Trader", "status": "active"})
	var ids []string
	for _, code := range []string{"stock_trade:read", "stock_trade:settle", "stock_trade:cancel"} {
		var id string
		_ = e.pool.QueryRow(context.Background(), "SELECT uuid::text FROM action WHERE action_code=$1", code).Scan(&id)
		ids = append(ids, id)
	}
	rolePath := "/roles/" + role["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+rolePath+"/actions", admin, map[string]any{"action_uuids": ids}, "If-Match", e.etag(admin, rolePath)), 200, "grant")
	u := e.create(admin, "/users", map[string]any{"username": "trader.lain", "email": "t@lain.test", "role_uuid": role["uuid"],
		"client_uuid": other, "status": "active"})
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": u["activation_key"].(string),
		"new_password": "Outsider-Pass-1"}), 204, "activate")
	outsider, _ := e.login("trader.lain", "Outsider-Pass-1")
	t2Path := "/stock-trades/" + t2["uuid"].(string)
	e.expect(e.do("GET", "/api/v1"+t2Path, outsider, nil), 404, "foreign trade hidden")
	e.expect(e.do("POST", "/api/v1"+t2Path+"/cancel", outsider, map[string]any{}, "If-Match", e.etag(admin, t2Path)), 404, "foreign cancel")
	if s := e.expect(e.do("GET", "/api/v1"+t2Path, admin, nil), 200, "still draft").data()["stock_trade_status"]; s != "draft" {
		t.Fatalf("foreign action changed the trade: %v", s)
	}
}

func TestMutualFundAndCryptoTrading(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	b := setupBooks(e, admin)

	buy := e.create(admin, "/mutual-fund-trades", map[string]any{"portfolio_uuid": b.portfolio, "mutual_fund_uuid": b.fund,
		"trade_type_uuid": b.buy, "transaction_date": "2026-11-02", "total_investment_value": "3000000"})
	if buy["net_asset_value"] != "1500" || buy["total_unit"] != "2000" || buy["status"] != "draft" {
		t.Fatalf("mutual fund draft: %v", buy)
	}
	e.act(admin, "/mutual-fund-trades/"+buy["uuid"].(string), "settle", 200)
	e.expect(e.do("POST", "/api/v1/mutual-fund-trades", admin, map[string]any{"portfolio_uuid": b.portfolio, "mutual_fund_uuid": b.fund,
		"trade_type_uuid": b.buy, "transaction_date": "2026-11-02"}), 422, "units or value required")
	sell := e.create(admin, "/mutual-fund-trades", map[string]any{"portfolio_uuid": b.portfolio, "mutual_fund_uuid": b.fund,
		"trade_type_uuid": b.sell, "transaction_date": "2026-11-05", "total_unit": "500", "net_asset_value": "1600"})
	if sell["total_investment_value"] != "800000" {
		t.Fatalf("sell value: %v", sell)
	}
	e.act(admin, "/mutual-fund-trades/"+sell["uuid"].(string), "settle", 200)
	if u := e.scalar(t, "SELECT total_unit::text FROM mutual_fund_portfolio"); u != "1500" {
		t.Fatalf("fund units = %s", u)
	}
	if got := e.journalLines(t, "f.mutual_fund_trade_id = (SELECT id FROM mutual_fund_trade WHERE uuid = '"+sell["uuid"].(string)+"')"); strings.Join(got, " ") !=
		"1-100:800000:0 1-210:0:750000 4-100:0:50000" {
		t.Fatalf("fund sell journal = %v", got)
	}
	if n := e.scalar(t, "SELECT count(*)::text FROM mutual_fund_portfolio_history"); n != "2" {
		t.Fatalf("fund history snapshots = %s", n)
	}

	cbuy := e.create(admin, "/crypto-trades", map[string]any{"portfolio_uuid": b.portfolio, "crypto_uuid": b.crypto,
		"trade_type_uuid": b.buy, "trade_date": "2026-11-03", "price": "1000000000", "quantity": "0.5"})
	if cbuy["trade_value"] != "500000000" || cbuy["status"] != "draft" || cbuy["crypto_code"] != "BTC" {
		t.Fatalf("crypto draft: %v", cbuy)
	}
	e.act(admin, "/crypto-trades/"+cbuy["uuid"].(string), "settle", 200)
	if q := e.scalar(t, "SELECT total_crypto_quantity::text FROM crypto_portfolio"); q != "0.5" {
		t.Fatalf("crypto quantity = %s", q)
	}
	csell := e.create(admin, "/crypto-trades", map[string]any{"portfolio_uuid": b.portfolio, "crypto_uuid": b.crypto,
		"trade_type_uuid": b.sell, "trade_date": "2026-11-04", "price": "1100000000", "quantity": "1"})
	e.act(admin, "/crypto-trades/"+csell["uuid"].(string), "settle", 422)
}

func TestJournalsAndCashSettlement(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	b := setupBooks(e, admin)
	line := func(acc, dr, cr string) map[string]any {
		return map[string]any{"finance_account_uuid": acc, "debit": dr, "credit": cr}
	}
	post := func(lines ...map[string]any) resp {
		return e.do("POST", "/api/v1/finance-transactions", admin, map[string]any{"transaction_date": "2026-11-13",
			"description": "penyesuaian", "portfolio_uuid": b.portfolio, "lines": lines})
	}
	e.expect(post(line(b.cash, "100", ""), line(b.counter, "", "90")), 422, "unbalanced")
	e.expect(post(line(b.cash, "100", "100"), line(b.counter, "", "0")), 422, "both sides")
	e.expect(post(line(b.cash, "100", "")), 422, "single line")
	e.expect(post(line(b.cash, "100", ""), line("00000000-0000-0000-0000-000000000000", "", "100")), 422, "unknown account")
	j := e.expect(post(line(b.cash, "100", ""), line(b.counter, "", "100")), 201, "post journal").data()
	if j["status"] != "posted" || j["transaction_value"] != "100" || len(j["lines"].([]any)) != 2 {
		t.Fatalf("journal: %v", j)
	}
	j2 := e.expect(post(line(b.cash, "5", ""), line(b.counter, "", "5")), 201, "second journal").data()
	n1, n2 := j["transaction_number"].(string), j2["transaction_number"].(string)
	if n1 != "JV-2026-000001" || n2 != "JV-2026-000002" {
		t.Fatalf("numbering must be gapless despite failed postings: %s, %s", n1, n2)
	}
	jPath := "/finance-transactions/" + j["uuid"].(string)
	rev := e.expect(e.do("POST", "/api/v1"+jPath+"/reverse", admin, nil, "If-Match", e.etag(admin, jPath)), 201, "reverse journal").data()
	if rev["reversal_of_uuid"] != j["uuid"] || rev["lines"].([]any)[0].(map[string]any)["credit"] != "100" {
		t.Fatalf("reversal: %v", rev)
	}
	if s := e.expect(e.do("GET", "/api/v1"+jPath, admin, nil), 200, "original").data()["status"]; s != "reversed" {
		t.Fatalf("original status = %v", s)
	}
	e.expect(e.do("POST", "/api/v1"+jPath+"/reverse", admin, nil, "If-Match", e.etag(admin, jPath)), 409, "reverse twice")
	list := e.expect(e.do("GET", "/api/v1/finance-transactions?portfolio_uuid="+b.portfolio, admin, nil), 200, "list journals")
	if list.body["meta"].(map[string]any)["total"].(float64) != 3 {
		t.Fatalf("journals in portfolio: %s", list.raw)
	}

	// Concurrent postings get unique, consecutive numbers.
	var wg sync.WaitGroup
	numbers := make(chan string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := post(line(b.cash, "1", ""), line(b.counter, "", "1"))
			if r.status == 201 {
				numbers <- r.data()["transaction_number"].(string)
			}
		}()
	}
	wg.Wait()
	close(numbers)
	var got []string
	for n := range numbers {
		got = append(got, n)
	}
	sort.Strings(got)
	if len(got) != 10 || got[0] != "JV-2026-000004" || got[9] != "JV-2026-000013" {
		t.Fatalf("concurrent numbers = %v", got)
	}

	// Cash settlement: inbound with 1% fee, maker-checker.
	e.expect(e.do("POST", "/api/v1/cash-settlement-categories", admin, map[string]any{"cash_settlement_category_name": "X",
		"cash_settlement_category_code": "X", "inbound_settlement": true, "outbound_settlement": true}), 422, "inbound xor outbound")
	cat := e.create(admin, "/cash-settlement-categories", map[string]any{"cash_settlement_category_name": "Setoran",
		"cash_settlement_category_code": "IN", "inbound_settlement": true, "settlement_fee_percentage": "1",
		"cash_settlement_account_uuid": b.cash, "cash_settlement_counter_account_uuid": b.counter, "settlement_fee_account_uuid": b.fee})
	pdf := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF")
	doc := e.expect(e.upload("/api/v1/files/documents", admin, "cash_settlement.document", "bukti.pdf", pdf), 201, "upload").data()
	cs := e.create(admin, "/cash-settlements", map[string]any{"cash_settlement_category_uuid": cat["uuid"], "client_uuid": b.client,
		"cash_settlement_value": "1000000", "cash_settlement_subject": "Setoran iuran", "document_file_uuid": doc["uuid"]})
	if cs["document_file_uuid"] != doc["uuid"] || cs["cash_settlement_status"] != "draft" {
		t.Fatalf("cash settlement: %v", cs)
	}
	if st := e.expect(e.do("GET", "/api/v1/files/"+doc["uuid"].(string), admin, nil), 200, "file").data()["status"]; st != "active" {
		t.Fatalf("attached document status = %v", st)
	}
	csPath := "/cash-settlements/" + cs["uuid"].(string)
	e.act(admin, csPath, "approve", 403) // maker cannot approve

	role := e.create(admin, "/roles", map[string]any{"role_code": "CHECKER", "role_name": "Checker", "status": "active"})
	var ids []string
	for _, code := range []string{"cash_settlement:read", "cash_settlement:approve", "cash_settlement:reverse"} {
		var id string
		_ = e.pool.QueryRow(context.Background(), "SELECT uuid::text FROM action WHERE action_code=$1", code).Scan(&id)
		ids = append(ids, id)
	}
	rolePath := "/roles/" + role["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+rolePath+"/actions", admin, map[string]any{"action_uuids": ids}, "If-Match", e.etag(admin, rolePath)), 200, "grant")
	u := e.create(admin, "/users", map[string]any{"username": "checker", "email": "checker@example.test", "role_uuid": role["uuid"], "status": "active"})
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": u["activation_key"].(string),
		"new_password": "Approver-Pass-1"}), 204, "activate")
	checker, _ := e.login("checker", "Approver-Pass-1")

	if s := e.act(checker, csPath, "approve", 200).data()["cash_settlement_status"]; s != "approved" {
		t.Fatalf("status after approve = %v", s)
	}
	if got := e.journalLines(t, "f.cash_settlement_id IS NOT NULL"); strings.Join(got, " ") != "1-100:990000:0 2-100:0:1000000 5-200:10000:0" {
		t.Fatalf("settlement journal = %v", got)
	}
	e.expect(e.do("DELETE", "/api/v1"+csPath, admin, nil, "If-Match", e.etag(admin, csPath)), 409, "delete approved")
	if s := e.act(checker, csPath, "reverse", 200).data()["cash_settlement_status"]; s != "reversed" {
		t.Fatalf("status after reverse = %v", s)
	}
	// Journals created by a source document are reversed through it, not directly.
	var srcJournal string
	_ = e.pool.QueryRow(context.Background(), "SELECT uuid::text FROM finance_transaction WHERE cash_settlement_id IS NOT NULL AND reversal_of_id IS NULL").Scan(&srcJournal)
	e.expect(e.do("POST", "/api/v1/finance-transactions/"+srcJournal+"/reverse", admin, nil, "If-Match",
		e.etag(admin, "/finance-transactions/"+srcJournal)), 409, "reverse source journal directly")
}
