package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"pfm-backend/internal/db/schema"
)

// fkTargets maps table -> FK column -> referenced table (from the schema).
var fkTargets = func() map[string]map[string]string {
	out := map[string]map[string]string{}
	for target, refs := range schema.ReferencedBy {
		for _, r := range refs {
			if out[r.Table] == nil {
				out[r.Table] = map[string]string{}
			}
			out[r.Table][r.Column] = target
		}
	}
	return out
}()

// varAlias shortens a few variable names used across the collection.
var varAlias = map[string]string{"app_user": "user", "file_object": "file"}

func camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// uuidVar is the collection variable holding the UUID of a row of table.
func uuidVar(table string) string {
	if a, ok := varAlias[table]; ok {
		table = a
	}
	return "{{" + camel(table) + "Uuid}}"
}

// refVar resolves a `<x>_uuid` request field of table to its variable.
func refVar(table, field string, files map[string]string) string {
	if _, ok := files[field]; ok || strings.HasSuffix(field, "_file_uuid") {
		return "{{fileUuid}}"
	}
	base := strings.TrimSuffix(field, "_uuid")
	if t, ok := fkTargets[table][base+"_id"]; ok {
		return uuidVar(t)
	}
	if _, ok := schema.SoftDelete[base]; ok {
		return uuidVar(base)
	}
	return "{{" + camel(base) + "Uuid}}"
}

type rules map[string]string

func parseRules(tag string) rules {
	r := rules{}
	for _, part := range strings.Split(tag, ",") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		r[k] = v
	}
	return r
}

func (r rules) has(k string) bool { _, ok := r[k]; return ok }

type field struct {
	JSON  string
	Type  reflect.Type
	Rules rules
	Tag   string
}

// fields lists the JSON fields of a struct type (embedded structs flattened).
func fields(t reflect.Type) []field {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []field
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" {
			out = append(out, fields(f.Type)...)
			continue
		}
		if name == "-" || name == "" {
			continue
		}
		tag := f.Tag.Get("validate")
		out = append(out, field{JSON: name, Type: f.Type, Rules: parseRules(tag), Tag: tag})
	}
	return out
}

// sampleCtx resolves references while building samples.
type sampleCtx struct {
	table string
	files map[string]string
}

// sampleObject builds a JSON sample of a request DTO with its required fields.
func sampleObject(t reflect.Type, ctx sampleCtx) map[string]any {
	out := map[string]any{}
	for _, f := range fields(t) {
		if f.Rules.has("required") {
			out[f.JSON] = sampleValue(f, ctx)
		}
	}
	return out
}

func sampleValue(f field, ctx sampleCtx) any {
	t := f.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	r := f.Rules
	switch t.Kind() {
	case reflect.Bool:
		return false
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch {
		case f.JSON == "year":
			return 2026
		case f.JSON == "month":
			return 11
		case r.has("min"):
			n, _ := strconv.Atoi(r["min"])
			return max(n, 1)
		}
		return 1
	case reflect.Slice:
		if r.has("cidr_list") {
			return []string{"10.0.0.0/8"}
		}
		if t.Elem().Kind() == reflect.Struct {
			return []any{sampleObject(t.Elem(), ctx)}
		}
		if t.Elem().Kind() == reflect.String && r.has("dive") && strings.Contains(f.Tag, "uuid") {
			return []string{refVar(ctx.table, strings.TrimSuffix(f.JSON, "s"), ctx.files)}
		}
		return []any{}
	case reflect.Struct:
		return sampleObject(t, ctx)
	}
	switch {
	case r.has("uuid"):
		return refVar(ctx.table, f.JSON, ctx.files)
	case r["datetime"] == "2006-01-02":
		return sampleDate(f.JSON)
	case r.has("datetime"):
		return "2026-11-20T09:00:00+07:00"
	case r.has("decimal"):
		if r.has("dlte") {
			return "5"
		}
		return "1000000"
	case r.has("numeric"):
		return "1234567890"
	case r.has("email"):
		return "contoh@example.test"
	case r.has("phone_id"):
		return "081234567890"
	case r.has("npwp"):
		return "012345678901000"
	case r.has("nik"):
		return "3171012345678901"
	case r.has("stock_code"):
		return "BBCA"
	case r.has("perm_code"):
		return "contoh:lihat"
	case r.has("number_template"):
		return "{counter}"
	case r.has("username"):
		return "user.{{$timestamp}}"
	case r.has("https_url"), r.has("http_url"), r.has("url"):
		return "https://example.test"
	case r.has("ip"):
		return "10.0.0.1"
	case r.has("oneof"):
		return strings.Fields(r["oneof"])[0]
	case r.has("code"):
		return "CONTOH-{{$timestamp}}" // unique per send: codes have unique indexes
	case r.has("startswith"):
		return r["startswith"] + "contoh"
	}
	return "Contoh " + strings.ReplaceAll(f.JSON, "_", " ")
}

// sampleDate keeps date pairs consistent (end after start, birth before work).
func sampleDate(name string) string {
	switch {
	case name == "birthdate" || strings.HasSuffix(name, "_birthdate"):
		return "1990-05-17"
	case strings.Contains(name, "start_working"):
		return "2015-01-05"
	case strings.Contains(name, "maturity") || strings.Contains(name, "end_date") || strings.Contains(name, "due_date") ||
		strings.Contains(name, "retired") || strings.Contains(name, "expir"):
		return "2027-11-20"
	}
	return "2026-11-20"
}

// bodyOverrides complete sample bodies for rules that tags cannot express
// (one-of fields checked by service hooks).
var bodyOverrides = map[string]map[string]any{
	"cash_settlement_category":     {"inbound_settlement": true},
	"mutual_fund_trade":            {"total_investment_value": "1000000"},
	"member_portfolio_transaction": {"total_transaction_value": "1000000"},
	"bank_account":                 {"investor_uuid": "{{investorUuid}}"},
}

// typeName is the documented type of a field.
func typeName(f field) string {
	t := f.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	r := f.Rules
	switch {
	case t.String() == "uuid.UUID" || r.has("uuid"):
		return "uuid"
	case t.String() == "decimal.Decimal" || r.has("decimal"):
		return "decimal (string)"
	case t.String() == "time.Time":
		return "datetime"
	case t.String() == "httpx.Date" || r["datetime"] == "2006-01-02":
		return "date (YYYY-MM-DD)"
	case r.has("datetime"):
		return "datetime (RFC 3339)"
	case strings.HasPrefix(t.String(), "netip."):
		return "string (IP)"
	}
	switch t.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		el := t.Elem()
		if el.Kind() == reflect.Struct {
			return "array of object"
		}
		return "array of " + el.Kind().String()
	case reflect.Struct, reflect.Map:
		return "object"
	}
	return "string"
}

// ruleText shows the validation tag without the presence markers.
func ruleText(f field) string {
	var parts []string
	for _, p := range strings.Split(f.Tag, ",") {
		if p == "" || p == "omitempty" || p == "required" {
			continue
		}
		parts = append(parts, "`"+p+"`")
	}
	return strings.Join(parts, " ")
}

// fieldTable renders a markdown table of a DTO; nested structs get their own table.
func fieldTable(title string, v any, request bool) string {
	if v == nil {
		return ""
	}
	var b strings.Builder
	nested := map[string]reflect.Type{}
	b.WriteString("### " + title + "\n\n")
	if request {
		b.WriteString("| Field | Tipe | Wajib | Aturan |\n|---|---|---|---|\n")
	} else {
		b.WriteString("| Field | Tipe |\n|---|---|\n")
	}
	for _, f := range fields(reflect.TypeOf(v)) {
		typ := typeName(f)
		if request {
			req := "tidak"
			if f.Rules.has("required") {
				req = "**ya**"
			} else if f.Rules.has("required_if") {
				req = "kondisional"
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", f.JSON, typ, req, ruleText(f))
		} else {
			fmt.Fprintf(&b, "| `%s` | %s |\n", f.JSON, typ)
		}
		t := f.Type
		for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
			t = t.Elem()
		}
		if t.Kind() == reflect.Struct && !strings.Contains(t.PkgPath(), "uuid") && !strings.Contains(t.PkgPath(), "decimal") &&
			t.PkgPath() != "time" && t.Name() != "Date" {
			nested[f.JSON] = t
		}
	}
	for name, t := range nested {
		b.WriteString("\n" + fieldTable("`"+name+"` ("+t.Name()+")", reflect.New(t).Elem().Interface(), request))
	}
	return b.String()
}
