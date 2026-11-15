package router

import (
	"sync"
	"testing"
)

// generateConcurrently fires n identical generate requests and returns the
// status codes and the document UUIDs they answered with.
func (e *env) generateConcurrently(token, path string, body map[string]any, n int) (codes map[int]int, ids map[string]bool) {
	e.t.Helper()
	var mu sync.Mutex
	var wg sync.WaitGroup
	codes, ids = map[int]int{}, map[string]bool{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.do("POST", "/api/v1"+path, token, body)
			mu.Lock()
			defer mu.Unlock()
			codes[r.status]++
			if d := r.data(); d != nil {
				ids[d["uuid"].(string)] = true
			}
		}()
	}
	wg.Wait()
	return codes, ids
}

func TestPensionInvoicesAndPremiums(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	ca := e.create(admin, "/clients", map[string]any{"client_name": "PT Pemberi Kerja A", "client_code": "PKA"})["uuid"].(string)
	cb := e.create(admin, "/clients", map[string]any{"client_name": "PT Pemberi Kerja B", "client_code": "PKB"})["uuid"].(string)
	member := func(client, name string, status int, premium, start string) map[string]any {
		return e.create(admin, "/members", map[string]any{"member_name": name, "client_uuid": client, "status": status,
			"latest_pension_premium": premium, "latest_salary": "10000000", "start_working_date": start,
			"identity_number": "3171012345678901"})
	}
	m1 := member(ca, "Andi", 1, "500000", "2020-01-01")
	if m1["member_number"] != "MBR000001" || m1["identity_number"] != "3171012345678901" {
		t.Fatalf("member: %v", m1)
	}
	member(ca, "Budi", 1, "250000", "2021-03-01")
	member(ca, "Citra (nonaktif)", 0, "300000", "2019-01-01")
	member(ca, "Dodi (mulai Desember)", 1, "400000", "2026-12-01")
	member(cb, "Eka", 1, "100000", "2022-01-01")
	e.expect(e.do("POST", "/api/v1/members", admin, map[string]any{"member_name": "X", "client_uuid": ca,
		"start_working_date": "2026-01-01", "retired_date": "2025-01-01"}), 422, "retired before start")

	gen := func(client string, month int) resp {
		return e.do("POST", "/api/v1/invoices/generate", admin, map[string]any{"client_uuid": client, "year": 2026, "month": month})
	}
	inv := e.expect(gen(ca, 11), 201, "generate invoice").data()
	if inv["invoice_number"] != "01/DP/11/2026" || inv["invoice_value"] != "750000" || inv["total_member"] != float64(2) ||
		inv["invoice_status"] != "draft" || inv["invoice_date"] != "2026-11-01" || inv["invoice_due_date"] != "2026-11-10" ||
		inv["billing_description"] != "Tujuh Ratus Lima Puluh Ribu Rupiah" {
		t.Fatalf("generated invoice: %v", inv)
	}
	invPath := "/invoices/" + inv["uuid"].(string)
	if again := e.expect(gen(ca, 11), 200, "generate again").data(); again["uuid"] != inv["uuid"] || again["invoice_value"] != "750000" {
		t.Fatalf("second generate must return the same invoice: %v", again)
	}
	member(ca, "Fajar", 1, "50000", "2026-11-15")
	if again := e.expect(gen(ca, 11), 200, "complete draft").data(); again["invoice_value"] != "800000" || again["total_member"] != float64(3) {
		t.Fatalf("draft must pick up new members: %v", again)
	}
	if b := e.expect(gen(cb, 11), 201, "invoice client B").data(); b["invoice_number"] != "02/DP/11/2026" {
		t.Fatalf("invoice numbers are per month: %v", b["invoice_number"])
	}
	codes, ids := e.generateConcurrently(admin, "/invoices/generate", map[string]any{"client_uuid": ca, "year": 2026, "month": 12}, 6)
	if codes[201] != 1 || codes[200] != 5 || len(ids) != 1 {
		t.Fatalf("concurrent generate: codes=%v ids=%v", codes, ids)
	}

	lines := items(e.expect(e.do("GET", "/api/v1"+invPath+"/member-premium-payments", admin, nil), 200, "lines"))
	if len(lines) != 3 {
		t.Fatalf("invoice lines = %d", len(lines))
	}
	var l1 map[string]any
	for _, l := range lines {
		if l["member_uuid"] == m1["uuid"] {
			l1 = l
		}
	}
	if l1["payment_nominal"] != "500000" || l1["percentage"] != "5" || l1["month"] != float64(11) || l1["status"] != "unpaid" {
		t.Fatalf("line of m1: %v", l1)
	}
	l1Path := "/member-premium-payments/" + l1["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+l1Path, admin, map[string]any{"member_uuid": m1["uuid"], "invoice_uuid": inv["uuid"],
		"payment_nominal": "550000"}, "If-Match", e.etag(admin, l1Path)), 200, "edit draft line")
	if v := e.expect(e.do("GET", "/api/v1"+invPath, admin, nil), 200, "invoice").data()["invoice_value"]; v != "850000" {
		t.Fatalf("invoice total after line edit = %v", v)
	}
	e.expect(e.do("POST", "/api/v1/member-premium-payments", admin, map[string]any{"member_uuid": m1["uuid"],
		"invoice_uuid": inv["uuid"], "payment_nominal": "1"}), 409, "duplicate line for member")
	other := e.create(admin, "/members", map[string]any{"member_name": "Gita", "client_uuid": cb})
	e.expect(e.do("POST", "/api/v1/member-premium-payments", admin, map[string]any{"member_uuid": other["uuid"],
		"invoice_uuid": inv["uuid"], "payment_nominal": "1"}), 422, "member of another client")

	pay := func(path string, want int) resp {
		return e.actWith(admin, path, "pay", map[string]any{"payment_date": "2026-11-20"}, want)
	}
	pay(l1Path, 409) // invoice not issued yet
	e.act(admin, invPath, "issue", 200)
	e.expect(e.do("PUT", "/api/v1"+l1Path, admin, map[string]any{"member_uuid": m1["uuid"], "invoice_uuid": inv["uuid"],
		"payment_nominal": "1"}, "If-Match", e.etag(admin, l1Path)), 409, "edit line of issued invoice")
	member(ca, "Hana", 1, "75000", "2026-01-01")
	if again := e.expect(gen(ca, 11), 200, "generate issued").data(); again["invoice_value"] != "850000" {
		t.Fatalf("issued invoice must not change: %v", again)
	}
	if p := pay(l1Path, 200).data(); p["status"] != "paid" || p["paid_nominal"] != "550000" || p["payment_date"] != "2026-11-20" {
		t.Fatalf("paid line: %v", p)
	}
	pay(l1Path, 409)
	if v := e.scalar(t, "SELECT total_pension_premium_value::text FROM member WHERE uuid = $1", m1["uuid"]); v != "550000" {
		t.Fatalf("member premium total = %s", v)
	}
	if s := e.expect(e.do("GET", "/api/v1"+invPath, admin, nil), 200, "invoice").data()["invoice_status"]; s != "issued" {
		t.Fatalf("partly paid invoice = %v", s)
	}
	e.act(admin, invPath, "cancel", 409) // has a paid line
	for _, l := range lines {
		if l["uuid"] != l1["uuid"] {
			pay("/member-premium-payments/"+l["uuid"].(string), 200)
		}
	}
	if s := e.expect(e.do("GET", "/api/v1"+invPath, admin, nil), 200, "invoice").data()["invoice_status"]; s != "paid" {
		t.Fatalf("fully paid invoice = %v", s)
	}

	// A cancelled period can be generated again.
	var dec string
	for id := range ids {
		dec = "/invoices/" + id
	}
	e.act(admin, dec, "cancel", 200)
	if again := e.expect(gen(ca, 12), 201, "regenerate cancelled period").data(); again["invoice_number"] != "02/DP/12/2026" ||
		again["total_member"] != float64(5) || again["invoice_value"] != "1275000" {
		t.Fatalf("regenerated December invoice: %v", again)
	}
	if got := e.scalar(t, "SELECT concat_ws('/', latest_generated_invoice_month, latest_generated_invoice_year) FROM configuration"); got != "12/2026" {
		t.Fatalf("configuration invoice period = %s", got)
	}

	// Tenant scope: client B sees only its own invoices and members, PII masked.
	ub := e.userWith(admin, "CLIENT-B", "client.b", "Client-B-Pass-1", cb, "client:read", "member:read", "invoice:read", "invoice:generate")
	if n := len(items(e.expect(e.do("GET", "/api/v1/invoices", ub, nil), 200, "scoped invoices"))); n != 1 {
		t.Fatalf("client B sees %d invoices", n)
	}
	e.expect(e.do("GET", "/api/v1"+invPath, ub, nil), 404, "other client's invoice")
	e.expect(e.do("POST", "/api/v1/invoices/generate", ub, map[string]any{"client_uuid": ca, "year": 2027, "month": 1}), 422, "generate for other client")
	ms := items(e.expect(e.do("GET", "/api/v1/members", ub, nil), 200, "scoped members"))
	if len(ms) != 2 || ms[0]["identity_number"] == "3171012345678901" {
		t.Fatalf("client B members (PII must be masked): %v", ms)
	}
}

func TestPensionPayoutsAndUnits(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	ca := e.create(admin, "/clients", map[string]any{"client_name": "PT Pemberi Kerja A", "client_code": "PKA"})["uuid"].(string)
	cb := e.create(admin, "/clients", map[string]any{"client_name": "PT Pemberi Kerja B", "client_code": "PKB"})["uuid"].(string)
	retired := e.create(admin, "/members", map[string]any{"member_name": "Pensiunan", "client_uuid": ca, "status": 2,
		"pension_benefit_value": "2000000", "retired_date": "2025-01-01", "payment_method": 1})
	e.create(admin, "/members", map[string]any{"member_name": "Pensiun 2027", "client_uuid": ca, "status": 2,
		"pension_benefit_value": "3000000", "retired_date": "2027-01-01"})
	active := e.create(admin, "/members", map[string]any{"member_name": "Aktif", "client_uuid": ca, "status": 1})
	dep := e.create(admin, "/dependants", map[string]any{"member_uuid": retired["uuid"], "dependant_name": "Istri",
		"pension_benefit_value": "500000"})
	e.expect(e.do("POST", "/api/v1/tax-reports", admin, map[string]any{"member_uuid": active["uuid"], "tax_report_number": "TR-1",
		"dependant_uuid": dep["uuid"]}), 422, "tax report with another member's dependant")
	tr := e.create(admin, "/tax-reports", map[string]any{"member_uuid": retired["uuid"], "tax_report_number": "TR-2"})

	gen := func(month int) resp {
		return e.do("POST", "/api/v1/benefit-payout-batches/generate", admin, map[string]any{"year": 2026, "month": month})
	}
	batch := e.expect(gen(11), 201, "generate payout").data()
	if batch["batch_number"] != "01/PAYMENT/11/2026" || batch["total_beneficiaries"] != float64(2) ||
		batch["benefit_payout_total"] != "2500000" || batch["value_description"] != "Dua Juta Lima Ratus Ribu Rupiah" ||
		batch["payout_batch_status"] != "draft" || batch["payout_generated_date"] != "2026-11-01" {
		t.Fatalf("payout batch: %v", batch)
	}
	bPath := "/benefit-payout-batches/" + batch["uuid"].(string)
	if again := e.expect(gen(11), 200, "generate payout again").data(); again["uuid"] != batch["uuid"] || again["total_beneficiaries"] != float64(2) {
		t.Fatalf("second payout generate: %v", again)
	}
	var memberLine map[string]any
	for _, l := range items(e.expect(e.do("GET", "/api/v1"+bPath+"/member-benefit-payouts", admin, nil), 200, "payout lines")) {
		if l["dependant_uuid"] == nil {
			memberLine = l
		}
	}
	linePath := "/member-benefit-payouts/" + memberLine["uuid"].(string)
	upd := e.expect(e.do("PUT", "/api/v1"+linePath, admin, map[string]any{"benefit_payout_batch_uuid": batch["uuid"],
		"member_uuid": retired["uuid"], "benefit_payout_value": "2000000", "benefit_payout_tax_value": "100000",
		"tax_report_uuid": tr["uuid"]}, "If-Match", e.etag(admin, linePath)), 200, "set tax").data()
	if upd["final_payout_value"] != "1900000" || upd["month"] != float64(11) {
		t.Fatalf("payout line: %v", upd)
	}
	if v := e.expect(e.do("GET", "/api/v1"+bPath, admin, nil), 200, "batch").data()["benefit_payout_total"]; v != "2400000" {
		t.Fatalf("batch total after tax = %v", v)
	}

	e.act(admin, bPath, "approve", 403) // maker cannot approve
	checker := e.userWith(admin, "PAYOUT-CHK", "checker", "Approver-Pass-1", "", "benefit_payout_batch:read", "benefit_payout_batch:approve")
	if b := e.actWith(checker, bPath, "approve", map[string]any{"note": "sesuai SK"}, 200).data(); b["payout_batch_status"] != "approved" ||
		b["approval_description"] != "sesuai SK" || b["payout_approval_date"] == nil {
		t.Fatalf("approved batch: %v", b)
	}
	e.expect(e.do("PUT", "/api/v1"+linePath, admin, map[string]any{"benefit_payout_batch_uuid": batch["uuid"],
		"member_uuid": retired["uuid"], "benefit_payout_value": "1"}, "If-Match", e.etag(admin, linePath)), 409, "edit approved line")
	if b := e.actWith(admin, bPath, "pay", map[string]any{"payout_date": "2026-11-25"}, 200).data(); b["payout_batch_status"] != "paid" {
		t.Fatalf("paid batch: %v", b)
	}
	if got := e.scalar(t, "SELECT string_agg(DISTINCT member_benefit_payout_status::text || '@' || benefit_payout_date::text, ',') FROM member_benefit_payout"); got != "2@2026-11-25" {
		t.Fatalf("payout lines after pay = %s", got)
	}
	e.act(admin, bPath, "cancel", 409)
	codes, ids := e.generateConcurrently(admin, "/benefit-payout-batches/generate", map[string]any{"year": 2026, "month": 12}, 6)
	if codes[201] != 1 || codes[200] != 5 || len(ids) != 1 {
		t.Fatalf("concurrent payout generate: codes=%v ids=%v", codes, ids)
	}

	// Member unit account: subscribe, redeem at average cost, ordering rules.
	pa := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "DPPK A", "portfolio_code": "DA", "client_uuid": ca})["uuid"]
	pb := e.create(admin, "/portfolios", map[string]any{"portfolio_name": "DPPK B", "portfolio_code": "DB", "client_uuid": cb})["uuid"]
	e.expect(e.do("POST", "/api/v1/member-portfolios", admin, map[string]any{"member_uuid": active["uuid"], "portfolio_uuid": pb}), 422, "portfolio of another client")
	acc := e.create(admin, "/member-portfolios", map[string]any{"member_uuid": active["uuid"], "portfolio_uuid": pa})
	accPath := "/member-portfolios/" + acc["uuid"].(string)
	buy := e.create(admin, "/trade-types", map[string]any{"trade_type_name": "Beli", "trade_type_code": "BUY"})["uuid"]
	sell := e.create(admin, "/trade-types", map[string]any{"trade_type_name": "Jual", "trade_type_code": "SELL"})["uuid"]
	unitTx := func(tt any, date string, extra map[string]any) map[string]any {
		body := map[string]any{"member_portfolio_uuid": acc["uuid"], "trade_type_uuid": tt, "transaction_date": date}
		for k, v := range extra {
			body[k] = v
		}
		return e.create(admin, "/member-portfolio-transactions", body)
	}
	t1 := unitTx(buy, "2026-11-02", map[string]any{"total_transaction_value": "1000000", "effective_nav": "1000"})
	if t1["total_unit"] != "1000" || t1["transaction_status"] != "draft" {
		t.Fatalf("subscription draft: %v", t1)
	}
	e.act(admin, "/member-portfolio-transactions/"+t1["uuid"].(string), "settle", 200)
	t2 := unitTx(sell, "2026-11-10", map[string]any{"total_unit": "400", "effective_nav": "1250"})
	if t2["total_transaction_value"] != "500000" {
		t.Fatalf("redemption draft: %v", t2)
	}
	t2Path := "/member-portfolio-transactions/" + t2["uuid"].(string)
	if s := e.act(admin, t2Path, "settle", 200).data()["transaction_status"]; s != "settled" {
		t.Fatalf("settled status = %v", s)
	}
	a := e.expect(e.do("GET", "/api/v1"+accPath, admin, nil), 200, "unit account").data()
	if a["total_unit"] != "600" || a["total_investment"] != "600000" || a["current_net_asset_value"] != "1250" ||
		a["latest_transaction_date"] != "2026-11-10" {
		t.Fatalf("unit account: %v", a)
	}
	if n := len(items(e.expect(e.do("GET", "/api/v1"+accPath+"/member-portfolio-histories", admin, nil), 200, "history"))); n != 2 {
		t.Fatalf("unit history rows = %d", n)
	}
	tooMany := unitTx(sell, "2026-11-11", map[string]any{"total_unit": "700", "effective_nav": "1250"})
	e.act(admin, "/member-portfolio-transactions/"+tooMany["uuid"].(string), "settle", 422)
	early := unitTx(buy, "2026-11-01", map[string]any{"total_transaction_value": "1000", "effective_nav": "1000"})
	e.act(admin, "/member-portfolio-transactions/"+early["uuid"].(string), "settle", 422)
	if s := e.act(admin, "/member-portfolio-transactions/"+early["uuid"].(string), "cancel", 200).data()["transaction_status"]; s != "cancelled" {
		t.Fatalf("cancelled status = %v", s)
	}
	e.expect(e.do("DELETE", "/api/v1"+t2Path, admin, nil, "If-Match", e.etag(admin, t2Path)), 409, "delete settled")
	e.expect(e.do("POST", "/api/v1/member-portfolio-transactions", admin, map[string]any{"member_portfolio_uuid": acc["uuid"],
		"trade_type_uuid": buy, "transaction_date": "2026-11-12", "effective_nav": "1000"}), 422, "no amount")

	// Member documents: PDF on S3, linked records must belong to the member.
	pdf := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF")
	doc := e.expect(e.upload("/api/v1/files/documents", admin, "member_document.document", "sk.pdf", pdf), 201, "upload").data()
	e.expect(e.do("POST", "/api/v1/member-documents", admin, map[string]any{"member_uuid": active["uuid"],
		"document_file_uuid": doc["uuid"], "member_benefit_payout_uuid": memberLine["uuid"]}), 422, "payout of another member")
	md := e.create(admin, "/member-documents", map[string]any{"member_uuid": retired["uuid"], "document_file_uuid": doc["uuid"],
		"member_benefit_payout_uuid": memberLine["uuid"], "description": "SK pensiun"})
	if md["document_file_uuid"] != doc["uuid"] {
		t.Fatalf("member document: %v", md)
	}
	for _, path := range []string{"/member-groups", "/members/" + retired["uuid"].(string) + "/dependants", "/member-portfolio-histories"} {
		e.expect(e.do("GET", "/api/v1"+path, admin, nil), 200, "list "+path)
	}
}
