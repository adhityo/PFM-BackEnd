package router

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// userWith creates an active user whose role has exactly the given permissions.
func (e *env) userWith(admin, roleCode, username, password, client string, codes ...string) string {
	e.t.Helper()
	role := e.create(admin, "/roles", map[string]any{"role_code": roleCode, "role_name": roleCode, "status": "active"})
	var ids []string
	for _, code := range codes {
		var id string
		if err := e.secPool.QueryRow(context.Background(), "SELECT uuid::text FROM action WHERE action_code=$1", code).Scan(&id); err != nil {
			e.t.Fatal(code, err)
		}
		ids = append(ids, id)
	}
	rolePath := "/roles/" + role["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+rolePath+"/actions", admin, map[string]any{"action_uuids": ids}, "If-Match", e.etag(admin, rolePath)), 200, "grant "+roleCode)
	body := map[string]any{"username": username, "email": username + "@example.test", "role_uuid": role["uuid"], "status": "active"}
	if client != "" {
		body["client_uuid"] = client
	}
	u := e.create(admin, "/users", body)
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": u["activation_key"].(string),
		"new_password": password}), 204, "activate "+username)
	token, _ := e.login(username, password)
	return token
}

// actWith runs an If-Match guarded action with a JSON body.
func (e *env) actWith(token, path, action string, body map[string]any, want int) resp {
	e.t.Helper()
	return e.expect(e.do("POST", "/api/v1"+path+"/"+action, token, body, "If-Match", e.etag(token, path)), want, action+" "+path)
}

func items(r resp) []map[string]any {
	var out []map[string]any
	for _, it := range r.body["data"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

func TestBondSubscriptionAndCoupons(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	pdf := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF")
	ca := e.create(admin, "/clients", map[string]any{"client_name": "Emiten A", "client_code": "EA"})["uuid"].(string)
	cb := e.create(admin, "/clients", map[string]any{"client_name": "Emiten B", "client_code": "EB"})["uuid"].(string)
	bondBody := map[string]any{"bond_name": "Obligasi A 2026", "bond_code": "BND1", "bond_series_number": "A",
		"issuance_date": "2026-01-01", "maturity_date": "2027-01-01", "coupon": "10", "coupon_payment_periode": 90,
		"minimum_denomination": "1000000", "client_uuid": ca}
	bond := e.create(admin, "/bonds", bondBody)
	bondPath := "/bonds/" + bond["uuid"].(string)
	if bond["bond_status"] != "draft" {
		t.Fatalf("new bond: %v", bond)
	}
	bad := map[string]any{}
	for k, v := range bondBody {
		bad[k] = v
	}
	bad["bond_code"], bad["maturity_date"] = "BND2", "2025-12-31"
	e.expect(e.do("POST", "/api/v1/bonds", admin, bad), 422, "maturity before issuance")
	e.act(admin, bondPath, "activate", 422) // no coupon schedule yet

	// Coupon schedule: 4 × 90 days, a 5-day stub and the principal (same-date ties list newest first).
	gen := e.act(admin, bondPath, "generate-coupon-schedule", 200).data()
	if gen["next_coupon_date"] != "2026-04-01" {
		t.Fatalf("next coupon date: %v", gen["next_coupon_date"])
	}
	e.act(admin, bondPath, "generate-coupon-schedule", 200) // regenerating an unused schedule is allowed
	sched := items(e.expect(e.do("GET", "/api/v1"+bondPath+"/bond-coupon-schedules", admin, nil), 200, "schedule"))
	var got []string
	for _, s := range sched {
		got = append(got, fmt.Sprintf("%s:%s:%v", s["coupon_date"], s["coupon_rate"], s["transaction_type"]))
	}
	want := "2026-04-01:2.5:1 2026-06-30:2.5:1 2026-09-28:2.5:1 2026-12-27:2.5:1 2027-01-01:100:2 2027-01-01:0.138889:1"
	if strings.Join(got, " ") != want {
		t.Fatalf("schedule = %v", got)
	}

	inv := e.create(admin, "/investors", map[string]any{"investor_name": "Budi Santoso", "identity_number": "3171012345678901",
		"mobile_phone": "081234567890"})
	if want := fmt.Sprintf("INV%d00001", time.Now().UTC().Year()); inv["investor_number"] != want {
		t.Fatalf("investor number = %v, want %s", inv["investor_number"], want)
	}
	inv2 := e.create(admin, "/investors", map[string]any{"investor_name": "Siti"})
	acc := e.create(admin, "/bank-accounts", map[string]any{"bank_account_name": "Budi Santoso", "bank_account_number": "1234567890",
		"bank_name": "BCA", "investor_uuid": inv["uuid"]})
	acc2 := e.create(admin, "/bank-accounts", map[string]any{"bank_account_name": "Siti", "bank_account_number": "5555000011",
		"bank_name": "BRI", "investor_uuid": inv2["uuid"]})
	e.expect(e.do("POST", "/api/v1/bank-accounts", admin, map[string]any{"bank_account_name": "X", "bank_account_number": "1",
		"bank_name": "X"}), 422, "bank account without owner")

	trade := func(investor, account any, nominal string) resp {
		return e.do("POST", "/api/v1/bond-trades", admin, map[string]any{"bond_uuid": bond["uuid"], "investor_uuid": investor,
			"bank_account_uuid": account, "nominal": nominal, "trade_date": "2026-01-05"})
	}
	e.expect(trade(inv["uuid"], acc["uuid"], "50000000"), 422, "subscription before offering")
	e.act(admin, bondPath, "offer", 200)
	e.expect(trade(inv["uuid"], acc["uuid"], "500000"), 422, "below minimum denomination")
	e.expect(trade(inv["uuid"], acc2["uuid"], "50000000"), 422, "bank account of another investor")
	t1 := e.expect(trade(inv["uuid"], acc["uuid"], "50000000"), 201, "subscription").data()
	if t1["bond_trade_status"] != "draft" || t1["bond_code"] != "BND1" || t1["investor_name"] != "Budi Santoso" ||
		t1["bank_account_number"] != "1234567890" {
		t.Fatalf("draft subscription: %v", t1)
	}
	t1Path := "/bond-trades/" + t1["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+t1Path, admin, map[string]any{"bond_uuid": bond["uuid"], "investor_uuid": inv["uuid"],
		"bank_account_uuid": acc["uuid"], "nominal": "100000000", "trade_date": "2026-01-05"}, "If-Match", e.etag(admin, t1Path)), 200, "edit draft")
	if h := e.scalar(t, "SELECT previous_nominal::text FROM bond_trade_data_history"); h != "50000000" {
		t.Fatalf("trade history previous nominal = %s", h)
	}

	// Maker-checker approval.
	e.act(admin, t1Path, "approve", 403)
	checker := e.userWith(admin, "BOND-CHK", "checker", "Approver-Pass-1", "", "bond:read", "bond_trade:read", "bond_trade:approve",
		"bond_trade:cancel", "investor_data_history:read", "investor_data_history:approve", "investor_data_history:reject",
		"marketing_agent_data_history:approve")
	approved := e.act(checker, t1Path, "approve", 200).data()
	if approved["bond_trade_status"] != "approved" || approved["certificate_number"] != "BND1-A0000001" {
		t.Fatalf("approved subscription: %v", approved)
	}
	if approved["bank_account_number"] == "1234567890" {
		t.Fatal("bank account number must be masked without pii:read")
	}
	e.act(checker, t1Path, "approve", 409)
	e.expect(e.do("DELETE", "/api/v1"+t1Path, admin, nil, "If-Match", e.etag(admin, t1Path)), 409, "delete approved")
	e.act(admin, bondPath, "generate-coupon-schedule", 409)
	if n := e.scalar(t, "SELECT count(*)::text FROM investor_coupon_schedule"); n != "6" {
		t.Fatalf("investor coupons = %s", n)
	}

	// Concurrent approvals get unique, consecutive certificate numbers.
	var paths, etags []string
	for i := 0; i < 5; i++ {
		p := "/bond-trades/" + e.expect(trade(inv2["uuid"], acc2["uuid"], "1000000"), 201, "small subscription").data()["uuid"].(string)
		paths, etags = append(paths, p), append(etags, e.etag(checker, p))
	}
	var wg sync.WaitGroup
	certs := make(chan string, len(paths))
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := e.do("POST", "/api/v1"+paths[i]+"/approve", checker, map[string]any{}, "If-Match", etags[i])
			if r.status == 200 {
				certs <- r.data()["certificate_number"].(string)
			}
		}(i)
	}
	wg.Wait()
	close(certs)
	var numbers []string
	for c := range certs {
		numbers = append(numbers, c)
	}
	sort.Strings(numbers)
	if len(numbers) != 5 || numbers[0] != "BND1-A0000002" || numbers[4] != "BND1-A0000006" {
		t.Fatalf("concurrent certificates = %v", numbers)
	}
	cancelled := e.expect(trade(inv2["uuid"], acc2["uuid"], "1000000"), 201, "to cancel").data()
	cPath := "/bond-trades/" + cancelled["uuid"].(string)
	if s := e.actWith(checker, cPath, "cancel", map[string]any{"note": "dana belum masuk"}, 200).data()["bond_trade_status"]; s != "cancelled" {
		t.Fatalf("cancelled status = %v", s)
	}
	totals := func() string {
		return e.scalar(t, "SELECT concat_ws('|', total_fund, total_coupon, COALESCE(total_paid_coupon, 0), latest_certificate_counter, 'end') FROM bond")
	}
	if got := totals(); got != "105000000.0000|10645833.4500|0|6|end" {
		t.Fatalf("bond totals = %s", got)
	}

	e.act(admin, bondPath, "activate", 200)
	upd := map[string]any{}
	for k, v := range bondBody {
		upd[k] = v
	}
	upd["coupon"] = "11"
	e.expect(e.do("PUT", "/api/v1"+bondPath, admin, upd, "If-Match", e.etag(admin, bondPath)), 409, "change coupon of active bond")

	// Coupon payments: whole schedule, then one investor coupon, then the rest.
	s1, s2 := "/bond-coupon-schedules/"+sched[0]["uuid"].(string), "/bond-coupon-schedules/"+sched[1]["uuid"].(string)
	p1 := e.actWith(admin, s1, "pay", map[string]any{"settlement_date": "2026-04-01"}, 200).data()
	if p1["status"] != "paid" || p1["coupon_paid_value"] != "2625000" {
		t.Fatalf("paid schedule: %v", p1)
	}
	e.actWith(admin, s1, "pay", map[string]any{"settlement_date": "2026-04-01"}, 409)
	var ic string
	_ = e.pool.QueryRow(context.Background(), `SELECT c.uuid::text FROM investor_coupon_schedule c
		JOIN bond_trade t ON t.id = c.bond_trade_id JOIN bond_coupon_schedule s ON s.id = c.bond_coupon_schedule_id
		WHERE t.uuid = $1 AND s.uuid = $2`, t1["uuid"], sched[1]["uuid"]).Scan(&ic)
	icPath := "/investor-coupon-schedules/" + ic
	proof := e.expect(e.upload("/api/v1/files/documents", admin, "investor_coupon_schedule.payment_document", "transfer.pdf", pdf), 201, "upload").data()
	e.actWith(admin, icPath, "pay", map[string]any{"payment_settlement_date": "2026-06-30"}, 422)
	paid := e.actWith(admin, icPath, "pay", map[string]any{"payment_settlement_date": "2026-06-30",
		"payment_reference_number": "TRF-001", "payment_document_file_uuid": proof["uuid"]}, 200).data()
	if paid["coupon_status"] != "paid" || paid["coupon_paid_value"] != "2500000" {
		t.Fatalf("paid investor coupon: %v", paid)
	}
	e.actWith(admin, icPath, "pay", map[string]any{"payment_settlement_date": "2026-06-30", "payment_document_file_uuid": proof["uuid"]}, 409)
	if s := e.expect(e.do("GET", "/api/v1"+s2, admin, nil), 200, "schedule 2").data()["status"]; s != "unpaid" {
		t.Fatalf("partly paid schedule must stay unpaid, got %v", s)
	}
	if p := e.actWith(admin, s2, "pay", map[string]any{"settlement_date": "2026-06-30"}, 200).data(); p["status"] != "paid" || p["coupon_paid_value"] != "2625000" {
		t.Fatalf("schedule 2 after paying the rest: %v", p)
	}
	if got := e.scalar(t, "SELECT concat_ws('|', total_paid_coupon, next_coupon_date, last_coupon_date, 'end') FROM bond"); got != "5250000.0000|2026-09-28|2026-06-30|end" {
		t.Fatalf("bond after coupon payments = %s", got)
	}

	// Tenant scope through the bond's client.
	other := e.userWith(admin, "CLIENT-B", "client.b", "Scoped-Pass-1", cb, "bond:read", "bond_trade:read", "investor_coupon_schedule:read")
	if n := len(items(e.expect(e.do("GET", "/api/v1/bonds", other, nil), 200, "scoped bonds"))); n != 0 {
		t.Fatalf("client B sees %d bonds of client A", n)
	}
	e.expect(e.do("GET", "/api/v1"+bondPath, other, nil), 404, "other client's bond")
	e.expect(e.do("GET", "/api/v1"+t1Path, other, nil), 404, "other client's subscription")
	if n := len(items(e.expect(e.do("GET", "/api/v1/investor-coupon-schedules", other, nil), 200, "scoped coupons"))); n != 0 {
		t.Fatalf("client B sees %d investor coupons of client A", n)
	}

	// Agent fee: payment proof required, paid fees are frozen.
	agent := e.create(admin, "/marketing-agents", map[string]any{"marketing_agent_name": "Agen Satu", "handphone": "081211112222"})
	if agent["marketing_agent_number"] != "AGT00001" {
		t.Fatalf("agent number = %v", agent["marketing_agent_number"])
	}
	feeBody := map[string]any{"marketing_agent_uuid": agent["uuid"], "bond_uuid": bond["uuid"], "outstanding_value": "500000"}
	fee := e.create(admin, "/agent-fee-transactions", feeBody)
	if fee["status"] != "draft" || fee["agent_name"] != "Agen Satu" || fee["agent_number"] != "AGT00001" {
		t.Fatalf("agent fee: %v", fee)
	}
	feePath := "/agent-fee-transactions/" + fee["uuid"].(string)
	payFee := map[string]any{"payment_date": "2026-11-14", "paid_value": "500000", "tax_paid_value": "10000"}
	e.actWith(admin, feePath, "pay", payFee, 422)
	feeDoc := e.expect(e.upload("/api/v1/files/documents", admin, "agent_fee_transaction.payment_document", "fee.pdf", pdf), 201, "upload fee proof").data()
	feeBody["payment_document_file_uuid"] = feeDoc["uuid"]
	e.expect(e.do("PUT", "/api/v1"+feePath, admin, feeBody, "If-Match", e.etag(admin, feePath)), 200, "attach proof")
	if f := e.actWith(admin, feePath, "pay", payFee, 200).data(); f["status"] != "paid" || f["outstanding_value"] != "0" || f["paid_value"] != "500000" {
		t.Fatalf("paid fee: %v", f)
	}
	e.expect(e.do("PUT", "/api/v1"+feePath, admin, feeBody, "If-Match", e.etag(admin, feePath)), 409, "edit paid fee")

	// Change requests (maker-checker) for investor and agent data.
	crPath := "/api/v1/investors/" + inv["uuid"].(string) + "/change-requests"
	change := map[string]any{"investor_name": "Budi Santoso Wijaya", "identity_number": "3171012345678901", "mobile_phone": "081298765432"}
	cr := e.expect(e.do("POST", crPath, admin, change), 201, "investor change request").data()
	if cr["approval_status"] != "pending" || cr["investor_name"] != "Budi Santoso Wijaya" || cr["previous_investor_name"] != "Budi Santoso" {
		t.Fatalf("change request: %v", cr)
	}
	e.expect(e.do("POST", crPath, admin, change), 409, "second pending request")
	crAct := "/api/v1/investor-data-histories/" + cr["uuid"].(string)
	e.expect(e.do("POST", crAct+"/approve", admin, nil), 403, "maker approves own request")
	dec := e.expect(e.do("POST", crAct+"/approve", checker, map[string]any{"note": "sesuai KTP"}), 200, "approve change").data()
	if dec["approval_status"] != "approved" || dec["approval_notes"] != "sesuai KTP" || dec["identity_number"] == "3171012345678901" {
		t.Fatalf("decided change request (PII must be masked for checker): %v", dec)
	}
	e.expect(e.do("POST", crAct+"/reject", checker, nil), 409, "decide twice")
	invNow := e.expect(e.do("GET", "/api/v1/investors/"+inv["uuid"].(string), admin, nil), 200, "investor").data()
	if invNow["investor_name"] != "Budi Santoso Wijaya" || invNow["mobile_phone"] != "081298765432" || invNow["investor_number"] != inv["investor_number"] {
		t.Fatalf("investor after approval: %v", invNow)
	}
	// A request is stale once the investor is edited directly; it can only be rejected.
	cr2 := e.expect(e.do("POST", crPath, admin, map[string]any{"investor_name": "Nama Lama"}), 201, "second request").data()
	invPath := "/investors/" + inv["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+invPath, admin, map[string]any{"investor_name": "Budi S. Wijaya"}, "If-Match", e.etag(admin, invPath)), 200, "direct edit")
	cr2Act := "/api/v1/investor-data-histories/" + cr2["uuid"].(string)
	e.expect(e.do("POST", cr2Act+"/approve", checker, nil), 409, "approve stale request")
	if s := e.expect(e.do("POST", cr2Act+"/reject", checker, nil), 200, "reject").data()["approval_status"]; s != "rejected" {
		t.Fatalf("rejected status = %v", s)
	}

	acr := e.expect(e.do("POST", "/api/v1/marketing-agents/"+agent["uuid"].(string)+"/change-requests", admin,
		map[string]any{"marketing_agent_name": "Agen Satu Baru", "handphone": "081299990000"}), 201, "agent change request").data()
	e.expect(e.do("POST", "/api/v1/marketing-agent-data-histories/"+acr["uuid"].(string)+"/approve", checker, nil), 200, "approve agent change")
	if n := e.scalar(t, "SELECT marketing_agent_name FROM marketing_agent"); n != "Agen Satu Baru" {
		t.Fatalf("agent after approval = %s", n)
	}
	e.expect(e.do("POST", "/api/v1/marketing-agent-data-histories/"+acr["uuid"].(string)+"/reject", checker, nil), 403, "reject without permission")
}

func TestBondSupportingResources(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	bond := e.create(admin, "/bonds", map[string]any{"bond_name": "Obligasi B", "bond_code": "BND9", "issuance_date": "2026-01-01",
		"maturity_date": "2026-12-31", "coupon": "9", "coupon_payment_periode": 180, "bond_price": "100"})
	bondPath := "/bonds/" + bond["uuid"].(string)

	// Ratings: the latest rating becomes bond.rating_id and is snapshotted with the price.
	agency := e.create(admin, "/rating-agencies", map[string]any{"rating_agency_name": "Pefindo", "rating_agency_code": "PEF"})
	aa := e.create(admin, "/ratings", map[string]any{"rating_name": "AA", "rating_code": "idAA", "rating_agency_uuid": agency["uuid"]})
	a := e.create(admin, "/ratings", map[string]any{"rating_name": "A", "rating_code": "idA", "rating_agency_uuid": agency["uuid"]})
	r1 := e.create(admin, "/bond-ratings", map[string]any{"bond_uuid": bond["uuid"], "rating_uuid": aa["uuid"],
		"rating_agency_uuid": agency["uuid"], "rating_date": "2026-01-10"})
	if r1["rating_value"] != "idAA" || r1["rating_agency_name"] != "Pefindo" || r1["bond_name"] != "Obligasi B" {
		t.Fatalf("bond rating snapshot: %v", r1)
	}
	e.create(admin, "/bond-ratings", map[string]any{"bond_uuid": bond["uuid"], "rating_uuid": a["uuid"], "rating_date": "2026-06-10"})
	if got := e.expect(e.do("GET", "/api/v1"+bondPath, admin, nil), 200, "bond").data()["rating_uuid"]; got != a["uuid"] {
		t.Fatalf("bond rating = %v, want latest %v", got, a["uuid"])
	}
	hist := items(e.expect(e.do("GET", "/api/v1"+bondPath+"/bond-histories", admin, nil), 200, "bond history"))
	if len(hist) != 3 { // created + 2 ratings
		t.Fatalf("bond history rows = %d", len(hist))
	}

	// Certificate number from the periode option template.
	opt := e.create(admin, "/bond-periode-options", map[string]any{"bond_periode_option_code": "Y1", "bond_periode_option_name": "1 Tahun",
		"bond_number_template": "SRT/{bondCode}/{year}/{counter}"})
	e.create(admin, "/bond-periode-distributions", map[string]any{"bond_uuid": bond["uuid"], "bond_periode_name": "Tahap 1"})
	e.act(admin, bondPath, "generate-coupon-schedule", 200)
	e.act(admin, bondPath, "offer", 200)
	inv := e.create(admin, "/investors", map[string]any{"investor_name": "Andi"})
	tr := e.create(admin, "/bond-trades", map[string]any{"bond_uuid": bond["uuid"], "investor_uuid": inv["uuid"],
		"nominal": "10000000", "trade_date": "2026-01-15", "bond_periode_option_uuid": opt["uuid"]})
	checker := e.userWith(admin, "BOND-CHK", "checker", "Approver-Pass-1", "", "bond_trade:read", "bond_trade:approve")
	cert := e.act(checker, "/bond-trades/"+tr["uuid"].(string), "approve", 200).data()["certificate_number"]
	if want := fmt.Sprintf("SRT/BND9/%d/0000001", time.Now().UTC().Year()); cert != want {
		t.Fatalf("certificate = %v, want %s", cert, want)
	}

	// Paid schedules are frozen; payment documents attach a PDF to a schedule.
	sched := items(e.expect(e.do("GET", "/api/v1"+bondPath+"/bond-coupon-schedules?sort=coupon_date", admin, nil), 200, "schedule"))
	sPath := "/bond-coupon-schedules/" + sched[0]["uuid"].(string)
	e.actWith(admin, sPath, "pay", map[string]any{"settlement_date": "2026-06-30"}, 200)
	e.expect(e.do("PUT", "/api/v1"+sPath, admin, map[string]any{"bond_uuid": bond["uuid"], "coupon_date": "2026-07-01",
		"coupon_rate": "4.5", "transaction_type": 1}, "If-Match", e.etag(admin, sPath)), 409, "edit paid schedule")
	e.expect(e.do("DELETE", "/api/v1"+sPath, admin, nil, "If-Match", e.etag(admin, sPath)), 409, "delete paid schedule")
	pdf := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF")
	doc := e.expect(e.upload("/api/v1/files/documents", admin, "payment_document.document", "kupon.pdf", pdf), 201, "upload").data()
	pd := e.create(admin, "/payment-documents", map[string]any{"bond_coupon_schedule_uuid": sched[0]["uuid"], "document_name": "Bukti kupon 1",
		"document_file_uuid": doc["uuid"]})
	if pd["document_file_uuid"] != doc["uuid"] {
		t.Fatalf("payment document: %v", pd)
	}
	for _, path := range []string{"/bond-trade-data-histories", "/investor-data-histories", "/marketing-agent-data-histories",
		"/investor-coupon-schedules", "/bond-periode-distributions", "/bank-accounts"} {
		e.expect(e.do("GET", "/api/v1"+path, admin, nil), 200, "list "+path)
	}
	e.expect(e.do("POST", "/api/v1/investor-coupon-schedules", admin, map[string]any{}), 404, "investor coupons are read-only")
}
