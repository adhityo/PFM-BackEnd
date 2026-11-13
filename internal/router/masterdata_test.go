package router

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// create posts body and returns the created resource.
func (e *env) create(token, path string, body map[string]any) map[string]any {
	e.t.Helper()
	return e.expect(e.do("POST", "/api/v1"+path, token, body), 201, "create "+path).data()
}

// etag fetches the current ETag of a resource.
func (e *env) etag(token, path string) string {
	e.t.Helper()
	return e.expect(e.do("GET", "/api/v1"+path, token, nil), 200, "get "+path).header.Get("ETag")
}

type mdCase struct {
	table, path string
	body        func(parents map[string]string) map[string]any
}

func named(table string, extra map[string]any) func(map[string]string) map[string]any {
	return func(p map[string]string) map[string]any {
		b := map[string]any{table + "_name": "Nama " + table, table + "_code": strings.ToUpper(table[:3]) + "1"}
		for k, v := range extra {
			if s, ok := v.(string); ok && strings.HasPrefix(s, "$") {
				v = p[s[1:]]
			}
			b[k] = v
		}
		return b
	}
}

func masterdataCases() []mdCase {
	return []mdCase{
		{"asset_class", "/asset-classes", named("asset_class", nil)},
		{"bank_class", "/bank-classes", named("bank_class", nil)},
		{"bank", "/banks", func(p map[string]string) map[string]any {
			return map[string]any{"bank_name": "Bank Syariah", "bank_clearing_code": "451", "bank_swift_code": "BSMDIDJA",
				"bank_class_uuid": p["bank_class"]}
		}},
		{"bond_periode_option", "/bond-periode-options", named("bond_periode_option", map[string]any{
			"bond_number_template": "{bondCode}.A{series}.{counter}", "default_coupon_rate": "8.5",
			"investment_length_month": 6, "periode_duration": 180})},
		{"bond_type", "/bond-types", named("bond_type", map[string]any{"default_maturity_period": 12})},
		{"country", "/countries", named("country", map[string]any{"country_code": "ID"})},
		{"province", "/provinces", named("province", map[string]any{"country_uuid": "$country"})},
		{"city", "/cities", named("city", map[string]any{"province_uuid": "$province", "latitude": "-6.200000", "longitude": "106.816666"})},
		{"document_category", "/document-categories", named("document_category", nil)},
		{"finance_account_type", "/finance-account-types", func(map[string]string) map[string]any {
			return map[string]any{"finance_account_type_name": "Aset", "description": "akun aset"}
		}},
		{"investor_category", "/investor-categories", named("investor_category", nil)},
		{"mutual_fund_category", "/mutual-fund-categories", named("mutual_fund_category", nil)},
		{"occupation", "/occupations", named("occupation", map[string]any{"status": 1})},
		{"placement_type", "/placement-types", named("placement_type", nil)},
		{"premium_payment_type", "/premium-payment-types", named("premium_payment_type", map[string]any{"type": 1})},
		{"property_asset_type", "/property-asset-types", named("property_asset_type", map[string]any{"weighted_value": "1.2500"})},
		{"rating_agency", "/rating-agencies", named("rating_agency", nil)},
		{"rating", "/ratings", named("rating", map[string]any{"rating_code": "idAA+", "rating_agency_uuid": "$rating_agency"})},
		{"sector", "/sectors", named("sector", nil)},
		{"sub_sector", "/sub-sectors", named("sub_sector", map[string]any{"sector_uuid": "$sector"})},
		{"stock_size_classification", "/stock-size-classifications", named("stock_size_classification",
			map[string]any{"start_quartile_range": "0", "end_quartile_range": "25.5"})},
		{"stock_type", "/stock-types", named("stock_type", nil)},
		{"tax_rate", "/tax-rates", named("tax_rate", nil)},
		{"trade_type", "/trade-types", named("trade_type", nil)},
	}
}

// TestMasterdataCRUDAllResources runs the full lifecycle on every scaffolded table.
func TestMasterdataCRUDAllResources(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	parents := map[string]string{}

	cases := masterdataCases()
	if len(cases) != 24 {
		t.Fatalf("expected 24 scaffolded resources, got %d", len(cases))
	}
	for _, tc := range cases {
		body := tc.body(parents)
		created := e.create(admin, tc.path, body)
		id := created["uuid"].(string)
		parents[tc.table] = id
		for k, v := range body {
			if s, ok := v.(string); ok && created[k] != s && !strings.HasSuffix(k, "_uuid") {
				// numerics are normalised by PostgreSQL (e.g. "8.5" -> "8.500000")
				if !strings.HasPrefix(fmt.Sprint(created[k]), strings.TrimRight(s, "0")) {
					t.Fatalf("%s: field %s = %v, want %v", tc.table, k, created[k], s)
				}
			}
		}

		item := tc.path + "/" + id
		tag := e.etag(admin, item)
		list := e.expect(e.do("GET", "/api/v1"+tc.path+"?per_page=5", admin, nil), 200, "list "+tc.path)
		if list.body["meta"].(map[string]any)["total"].(float64) < 1 {
			t.Fatalf("%s: list empty", tc.table)
		}

		nameKey := tc.table + "_name"
		body[nameKey] = "Diubah " + tc.table
		updated := e.expect(e.do("PUT", "/api/v1"+item, admin, body, "If-Match", tag), 200, "update "+tc.path).data()
		if updated[nameKey] != body[nameKey] {
			t.Fatalf("%s: update not applied: %v", tc.table, updated)
		}
		e.expect(e.do("PUT", "/api/v1"+item, admin, body, "If-Match", tag), 412, "stale update "+tc.path)
	}

	// Delete children before parents; every delete needs the current ETag.
	for i := len(cases) - 1; i >= 0; i-- {
		tc := cases[i]
		item := tc.path + "/" + parents[tc.table]
		e.expect(e.do("DELETE", "/api/v1"+item, admin, nil, "If-Match", `"0"`), 412, "stale delete "+tc.path)
		e.expect(e.do("DELETE", "/api/v1"+item, admin, nil, "If-Match", e.etag(admin, item)), 204, "delete "+tc.path)
		e.expect(e.do("GET", "/api/v1"+item, admin, nil), 404, "deleted "+tc.path)
	}
}

func TestMasterdataRules(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)

	// Referenced rows cannot be deleted; nested route filters by parent.
	bc := e.create(admin, "/bank-classes", map[string]any{"bank_class_name": "BUMN", "bank_class_code": "BUMN"})
	bank := e.create(admin, "/banks", map[string]any{"bank_name": "Bank A", "bank_class_uuid": bc["uuid"]})
	r := e.expect(e.do("DELETE", "/api/v1/bank-classes/"+bc["uuid"].(string), admin, nil, "If-Match",
		e.etag(admin, "/bank-classes/"+bc["uuid"].(string))), 409, "delete referenced parent")
	if !strings.Contains(string(r.raw), "bank") {
		t.Fatalf("conflict should name the referencing table: %s", r.raw)
	}
	nested := e.expect(e.do("GET", "/api/v1/bank-classes/"+bc["uuid"].(string)+"/banks", admin, nil), 200, "nested list")
	if items := nested.body["data"].([]any); len(items) != 1 || items[0].(map[string]any)["uuid"] != bank["uuid"] {
		t.Fatalf("nested list: %s", nested.raw)
	}
	empty := e.expect(e.do("GET", "/api/v1/bank-classes/"+uuid.NewString()+"/banks", admin, nil), 200, "unknown parent")
	if len(empty.body["data"].([]any)) != 0 {
		t.Fatal("unknown parent must yield an empty list")
	}
	e.expect(e.do("GET", "/api/v1/banks?bank_class_uuid=nope", admin, nil), 422, "invalid filter uuid")
	e.expect(e.do("POST", "/api/v1/banks", admin, map[string]any{"bank_name": "X", "bank_class_uuid": uuid.NewString()}), 422, "unknown FK")
	e.expect(e.do("POST", "/api/v1/banks", admin, map[string]any{"bank_name": "X", "bank_swift_code": "not-bic"}), 422, "invalid swift")

	// Snapshot columns are copied from the referenced sector; computed columns are read-only.
	sec := e.create(admin, "/sectors", map[string]any{"sector_name": "Keuangan", "sector_code": "FIN"})
	sub := e.create(admin, "/sub-sectors", map[string]any{"sub_sector_name": "Bank", "sub_sector_code": "BNK", "sector_uuid": sec["uuid"]})
	if sub["sector_code"] != "FIN" || sub["sector_name"] != "Keuangan" {
		t.Fatalf("snapshot not filled: %v", sub)
	}
	e.expect(e.do("POST", "/api/v1/sectors", admin, map[string]any{"sector_name": "X", "sector_code": "X", "total_stock": "5"}), 400, "read-only field")

	// Trees reject cycles.
	a := e.create(admin, "/asset-classes", map[string]any{"asset_class_name": "Saham", "asset_class_code": "STOCK"})
	b := e.create(admin, "/asset-classes", map[string]any{"asset_class_name": "Saham LQ45", "asset_class_code": "LQ45", "parent_asset_class_uuid": a["uuid"]})
	aPath := "/asset-classes/" + a["uuid"].(string)
	e.expect(e.do("PUT", "/api/v1"+aPath, admin, map[string]any{"asset_class_name": "Saham", "asset_class_code": "STOCK",
		"parent_asset_class_uuid": b["uuid"]}, "If-Match", e.etag(admin, aPath)), 422, "cycle via child")
	e.expect(e.do("PUT", "/api/v1"+aPath, admin, map[string]any{"asset_class_name": "Saham", "asset_class_code": "STOCK",
		"parent_asset_class_uuid": a["uuid"]}, "If-Match", e.etag(admin, aPath)), 422, "self parent")

	// Codes are unique among active rows only.
	c1 := e.create(admin, "/countries", map[string]any{"country_name": "Indonesia", "country_code": "ID"})
	e.expect(e.do("POST", "/api/v1/countries", admin, map[string]any{"country_name": "Dup", "country_code": "ID"}), 409, "duplicate code")
	cPath := "/countries/" + c1["uuid"].(string)
	e.expect(e.do("DELETE", "/api/v1"+cPath, admin, nil, "If-Match", e.etag(admin, cPath)), 204, "delete country")
	e.create(admin, "/countries", map[string]any{"country_name": "Indonesia", "country_code": "ID"})

	// Field validation.
	e.expect(e.do("POST", "/api/v1/bond-periode-options", admin, map[string]any{"bond_periode_option_name": "X",
		"bond_periode_option_code": "X", "bond_number_template": "{password}{counter}"}), 422, "bad template")
	e.expect(e.do("POST", "/api/v1/bond-periode-options", admin, map[string]any{"bond_periode_option_name": "X",
		"bond_periode_option_code": "X", "default_coupon_rate": "101"}), 422, "rate over 100")
	r = e.expect(e.do("GET", "/api/v1/countries?sort=-country_name&q=indo", admin, nil), 200, "sorted search")
	if r.body["meta"].(map[string]any)["total"].(float64) != 1 {
		t.Fatalf("search: %s", r.raw)
	}
}

func TestConfigurationSingleton(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)

	cur := e.expect(e.do("GET", "/api/v1/configuration", admin, nil), 200, "get configuration")
	tag := cur.header.Get("ETag")
	body := map[string]any{"company_name": "PT Contoh", "certificate_number_template": "${bondCode}-${bondSeries}${counter}",
		"investor_number_template": "RID${year}${counter}", "proxy_api_url": "https://proxy.example.com/api", "is_using_qr_logo": false}
	e.expect(e.do("PUT", "/api/v1/configuration", admin, body), 428, "missing If-Match")

	bad := map[string]any{"company_name": "PT", "proxy_api_url": "http://169.254.169.254/latest/meta-data"}
	e.expect(e.do("PUT", "/api/v1/configuration", admin, bad, "If-Match", tag), 422, "non-https proxy url")
	e.expect(e.do("PUT", "/api/v1/configuration", admin, map[string]any{"company_name": "PT", "certificate_latest_counter": 99},
		"If-Match", tag), 400, "counter is read-only")
	e.expect(e.do("PUT", "/api/v1/configuration", admin, map[string]any{"company_name": "PT", "is_using_qr_logo": true},
		"If-Match", tag), 422, "qr logo required when enabled")

	r := e.expect(e.do("PUT", "/api/v1/configuration", admin, body, "If-Match", tag), 200, "update configuration")
	if r.data()["company_name"] != "PT Contoh" || r.data()["investor_number_template"] != "RID${year}${counter}" {
		t.Fatalf("configuration not updated: %s", r.raw)
	}

	// Attach an uploaded logo: the pending file becomes active and cannot be deleted afterwards.
	logo := e.expect(e.upload("/api/v1/files/images", admin, "configuration.qr_logo", "logo.png", pngFile(t)), 201, "upload logo").data()
	body["is_using_qr_logo"], body["qr_logo_file_uuid"] = true, logo["uuid"]
	r = e.expect(e.do("PUT", "/api/v1/configuration", admin, body, "If-Match", r.header.Get("ETag")), 200, "attach logo")
	if r.data()["qr_logo_file_uuid"] != logo["uuid"] {
		t.Fatalf("logo not attached: %s", r.raw)
	}
	f := e.expect(e.do("GET", "/api/v1/files/"+logo["uuid"].(string), admin, nil), 200, "file meta").data()
	if f["status"] != "active" {
		t.Fatalf("attached file status = %v", f["status"])
	}
	e.expect(e.do("DELETE", "/api/v1/files/"+logo["uuid"].(string), admin, nil), 409, "delete attached file")

	// A document upload cannot be used as the logo.
	doc := e.expect(e.upload("/api/v1/files/documents", admin, "member_document.document", "x.pdf",
		[]byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\n%%EOF")), 201, "upload pdf").data()
	body["qr_logo_file_uuid"] = doc["uuid"]
	e.expect(e.do("PUT", "/api/v1/configuration", admin, body, "If-Match", r.header.Get("ETag")), 422, "wrong purpose file")
}
