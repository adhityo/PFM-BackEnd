package main

import (
	"fmt"
	"slices"
	"strings"
)

type reqField struct {
	Col                        string // column name (sqlc param name)
	Name, Type, JSON, Validate string
	Param                      string // expression producing the sqlc param value
	TrimString                 bool
}

type respField struct {
	Name, Type, JSON, Expr string
}

// plan is everything the three generated files of a table need.
type plan struct {
	sp        *Spec
	t         *Table
	camel     string // sqlc naming, e.g. AssetClass
	gn        string // Go naming of our own types, e.g. AssetClass
	lc        string // unexported prefix, e.g. assetClass
	rowType   string // sqlc.GetAssetClassByUUIDRow
	reqs      []reqField
	resps     []respField
	fks       []Column // writable foreign keys
	filterFKs []Column // every foreign key (list filters)
}

func newPlan(sp *Spec, t *Table) (*plan, error) {
	p := &plan{sp: sp, t: t, camel: sqlcName(sp.Table), gn: goName(sp.Table)}
	p.lc = lowerFirst(p.gn)
	p.rowType = "sqlc.Get" + p.camel + "ByUUIDRow"
	for _, c := range sp.apiColumns(t) {
		rq, rs, err := columnPlan(sp, t, c)
		if err != nil {
			return nil, err
		}
		p.resps = append(p.resps, rs)
		if rq != nil {
			p.reqs = append(p.reqs, *rq)
		}
		if _, ok := t.FKs[c.Name]; ok {
			p.filterFKs = append(p.filterFKs, c)
			if sp.writable(c) {
				p.fks = append(p.fks, c)
			}
		}
	}
	return p, nil
}

// columnPlan derives request/response handling for one column.
func columnPlan(sp *Spec, t *Table, c Column) (*reqField, respField, error) {
	sq := sqlcName(c.Name)
	rf := respField{Name: goName(c.Name), JSON: c.Name, Expr: "r." + sq}
	var req *reqField
	presence := "omitempty"
	if slices.Contains(sp.Required, c.Name) {
		presence = "required"
	}
	extra := sp.Validate[c.Name]
	tags := func(parts ...string) string {
		out := append([]string{presence}, parts...)
		if extra != "" {
			out = append(out, extra)
		}
		return strings.Join(out, ",")
	}

	if _, ok := t.FKs[c.Name]; ok {
		base := fkBase(c.Name)
		rf = respField{Name: goName(base + "_uuid"), Type: "*uuid.UUID", JSON: base + "_uuid", Expr: "r." + sqlcName(base+"_uuid")}
		if sp.writable(c) {
			req = &reqField{Col: c.Name, Name: goName(base + "_uuid"), Type: "string", JSON: base + "_uuid", Validate: tags("uuid"),
				Param: "refs." + lowerFirst(goName(c.Name)), TrimString: true}
		}
		return req, rf, nil
	}

	if names, ok := sp.Enum[c.Name]; ok {
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = fmt.Sprintf("%q", n)
		}
		rf.Type, rf.Expr = "*string", fmt.Sprintf("crud.EnumName(r.%s, []string{%s})", sq, strings.Join(quoted, ", "))
		return nil, rf, nil // enum columns are state, changed by actions only
	}

	switch c.DataType {
	case "character varying", "text":
		rf.Type = ptrIf(c.Nullable, "string")
		maxLen := c.MaxLen
		if c.DataType == "text" {
			maxLen = 10000
		}
		parts := []string{fmt.Sprintf("max=%d", maxLen)}
		if strings.HasSuffix(c.Name, "_code") {
			parts = append(parts, "code")
		} else {
			parts = append(parts, "safe_text")
		}
		param := "crud.Text(req." + goName(c.Name) + ")"
		if !c.Nullable {
			param = "req." + goName(c.Name)
		}
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "string", JSON: c.Name, Validate: tags(parts...), Param: param, TrimString: true}
	case "numeric":
		rf.Type = ptrIf(c.Nullable, "decimal.Decimal")
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "string", JSON: c.Name,
			Validate: tags(fmt.Sprintf("decimal=%d.%d", c.Precision, c.Scale)), Param: "crud.Decimal(req." + goName(c.Name) + ")", TrimString: true}
	case "integer":
		rf.Type = ptrIf(c.Nullable, "int32")
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "*int32", JSON: c.Name, Validate: tags("min=0"), Param: "req." + goName(c.Name)}
	case "smallint":
		rf.Type = ptrIf(c.Nullable, "int16")
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "*int16", JSON: c.Name, Validate: tags("min=0"), Param: "req." + goName(c.Name)}
	case "bigint":
		rf.Type = ptrIf(c.Nullable, "int64")
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "*int64", JSON: c.Name, Validate: tags("min=0"), Param: "req." + goName(c.Name)}
	case "boolean":
		rf.Type = ptrIf(c.Nullable, "bool")
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: ptrIf(c.Nullable, "bool"), JSON: c.Name, Param: "req." + goName(c.Name), Validate: extra}
	case "date":
		rf.Type, rf.Expr = "*httpx.Date", "httpx.DateOf(r."+sq+")"
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "string", JSON: c.Name, Validate: tags("datetime=2006-01-02"),
			Param: "crud.Date(req." + goName(c.Name) + ")", TrimString: true}
	case "timestamp with time zone":
		rf.Type = ptrIf(c.Nullable, "time.Time")
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "string", JSON: c.Name, Validate: tags("datetime=2006-01-02T15:04:05Z07:00"),
			Param: "crud.Time(req." + goName(c.Name) + ")", TrimString: true}
	case "inet":
		rf.Type = "*netip.Addr"
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "string", JSON: c.Name, Validate: tags("ip"),
			Param: "crud.IP(req." + goName(c.Name) + ")", TrimString: true}
	case "ARRAY":
		if c.UDT != "_cidr" {
			return nil, rf, fmt.Errorf("%s.%s: array %s belum didukung generator", t.Name, c.Name, c.UDT)
		}
		rf.Type = "[]netip.Prefix"
		req = &reqField{Col: c.Name, Name: goName(c.Name), Type: "[]string", JSON: c.Name, Validate: tags("max=100", "cidr_list"),
			Param: "crud.Prefixes(req." + goName(c.Name) + ")"}
	default:
		return nil, rf, fmt.Errorf("%s.%s: tipe %s belum didukung generator", t.Name, c.Name, c.DataType)
	}
	if !sp.writable(c) {
		req = nil
	}
	return req, rf, nil
}

func ptrIf(nullable bool, typ string) string {
	if nullable {
		return "*" + typ
	}
	return typ
}

const genHeader = "// Code generated by cmd/scaffold; DO NOT EDIT.\n\n"

func imports(groups ...[]string) string {
	var b strings.Builder
	b.WriteString("import (\n")
	first := true
	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		for _, imp := range g {
			fmt.Fprintf(&b, "\t%q\n", imp)
		}
	}
	b.WriteString(")\n\n")
	return b.String()
}

// genDTO renders internal/dto/<table>_gen.go: request, response and mapper.
func genDTO(p *plan) string {
	usesStrings, usesDecimal, usesHTTPX, usesNetip, usesCrud := false, false, false, false, false
	for _, f := range p.reqs {
		usesStrings = usesStrings || f.TrimString
	}
	for _, f := range p.resps {
		usesDecimal = usesDecimal || strings.Contains(f.Type, "decimal.")
		usesHTTPX = usesHTTPX || strings.Contains(f.Type, "httpx.")
		usesNetip = usesNetip || strings.Contains(f.Type, "netip.")
		usesCrud = usesCrud || strings.Contains(f.Expr, "crud.")
	}
	std := []string{"time"}
	if usesNetip {
		std = append([]string{"net/netip"}, std...)
	}
	if usesStrings {
		std = append([]string{"strings"}, std...)
	}
	third := []string{"github.com/google/uuid"}
	if usesDecimal {
		third = append(third, "github.com/shopspring/decimal")
	}
	local := []string{"pfm-backend/internal/db/sqlc"}
	if usesCrud {
		local = append(local, "pfm-backend/internal/platform/crud")
	}
	if usesHTTPX {
		local = append(local, "pfm-backend/internal/platform/httpx")
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("%s", genHeader+"package dto\n\n"+imports(std, third, local))
	if !p.sp.readOnly() {
		w("// %sRequest is the create/update body for %s.\ntype %sRequest struct {\n", p.gn, p.sp.Table, p.gn)
		for _, f := range p.reqs {
			tag := fmt.Sprintf("json:%q", f.JSON)
			if f.Validate != "" {
				tag += fmt.Sprintf(" validate:%q", f.Validate)
			}
			w("\t%s %s `%s`\n", f.Name, f.Type, tag)
		}
		w("}\n\n// Normalize trims string input.\nfunc (r *%sRequest) Normalize() {\n", p.gn)
		for _, f := range p.reqs {
			if f.TrimString {
				w("\tr.%[1]s = strings.TrimSpace(r.%[1]s)\n", f.Name)
			}
		}
		w("}\n\n")
	}
	w("// %sResponse is the public representation of %s.\ntype %sResponse struct {\n\tUUID uuid.UUID `json:\"uuid\"`\n", p.gn, p.sp.Table, p.gn)
	for _, f := range p.resps {
		w("\t%s %s `json:%q`\n", f.Name, f.Type, f.JSON)
	}
	w("\tCreatedAt time.Time `json:\"created_at\"`\n\tUpdatedAt time.Time `json:\"updated_at\"`\n}\n\n")
	w("// %sFromRow maps a %s row to its response.\nfunc %sFromRow(r %s) %sResponse {\n\treturn %sResponse{\n\t\tUUID: r.Uuid,\n",
		p.gn, p.sp.Table, p.gn, p.rowType, p.gn, p.gn)
	for _, f := range p.resps {
		w("\t\t%s: %s,\n", f.Name, f.Expr)
	}
	w("\t\tCreatedAt: r.CreatedAt,\n\t\tUpdatedAt: r.UpdatedAt,\n\t}\n}\n")
	return b.String()
}

// genRepository renders internal/repository/<table>_gen.go.
func genRepository(p *plan) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	ro := p.sp.readOnly()
	std := []string{"context", "time"}
	local := []string{"pfm-backend/internal/db/sqlc", "pfm-backend/internal/dto", "pfm-backend/internal/platform/crud",
		"pfm-backend/internal/platform/database", "pfm-backend/internal/platform/httpx"}
	if ro {
		std = []string{"context"}
		local = slices.DeleteFunc(local, func(s string) bool { return strings.HasSuffix(s, "/dto") })
	}
	w("%s", genHeader+"package repository\n\n"+imports(std, []string{"github.com/google/uuid"}, local))

	req := "dto." + p.gn + "Request"
	impl := p.lc + "Repository"
	if ro {
		w("// %sRepository reads %s (read-only resource).\ntype %sRepository = crud.Reader[%s]\n\n", p.gn, p.sp.Table, p.gn, p.rowType)
	} else {
		w("// %sRepository persists %s.\ntype %sRepository = crud.Store[%s, %s]\n\n", p.gn, p.sp.Table, p.gn, p.rowType, req)
	}
	w("type %s struct{ tx *database.TxManager }\n\n", impl)
	w("func New%sRepository(tx *database.TxManager) %sRepository { return %s{tx: tx} }\n\n", p.gn, p.gn, impl)

	if len(p.fks) > 0 {
		w("type %sRefs struct {\n", p.lc)
		for _, c := range p.fks {
			w("\t%s *int64\n", lowerFirst(goName(c.Name)))
		}
		w("}\n\n// resolve turns *_uuid fields into internal ids (FOR SHARE inside the transaction).\n")
		w("func (r %s) resolve(ctx context.Context, req %s) (%sRefs, error) {\n\tvar refs %sRefs\n\tvar err error\n", impl, req, p.lc, p.lc)
		for _, c := range p.fks {
			base := fkBase(c.Name)
			w("\tif refs.%s, err = database.RefID(ctx, r.tx, %q, req.%s, %q); err != nil {\n\t\treturn refs, err\n\t}\n",
				lowerFirst(goName(c.Name)), p.t.FKs[c.Name], goName(base+"_uuid"), base+"_uuid")
		}
		w("\treturn refs, nil\n}\n\n")
	}

	w("func (r %s) Get(ctx context.Context, id uuid.UUID) (%s, error) {\n", impl, p.rowType)
	if p.sp.Scope != "" {
		w("\trow, err := r.tx.Queries(ctx).Get%sByUUID(ctx, sqlc.Get%sByUUIDParams{Uuid: id, ScopeClientID: crud.ScopeClientID(ctx)})\n", p.camel, p.camel)
	} else {
		w("\trow, err := r.tx.Queries(ctx).Get%sByUUID(ctx, id)\n", p.camel)
	}
	w("\treturn row, database.MapError(\"get %s\", err)\n}\n\n", p.sp.Table)

	w("func (r %s) List(ctx context.Context, q httpx.ListQuery) ([]%s, int64, error) {\n", impl, p.rowType)
	var listParams, countArgs []string
	if p.sp.Scope != "" {
		w("\tscope := crud.ScopeClientID(ctx)\n")
		listParams = append(listParams, "ScopeClientID: scope")
		countArgs = append(countArgs, "ScopeClientID:scope")
	}
	if len(p.sp.Search) > 0 {
		listParams = append(listParams, `Q: q.Like("q")`)
		countArgs = append(countArgs, `Q:q.Like("q")`)
	}
	for _, c := range p.filterFKs {
		base := fkBase(c.Name)
		v := lowerFirst(goName(c.Name))
		w("\t%s, err := database.FilterRefID(ctx, r.tx, %q, %q, q.Text(%q))\n\tif err != nil {\n\t\treturn nil, 0, err\n\t}\n",
			v, p.t.FKs[c.Name], base+"_uuid", base+"_uuid")
		listParams = append(listParams, sqlcName(c.Name)+": "+v)
		countArgs = append(countArgs, sqlcName(c.Name)+":"+v)
	}
	listParams = append(listParams, "Sort: q.Sort", "Limit: q.Page.Limit()", "Offset: q.Page.Offset()")
	w("\tqs := r.tx.Queries(ctx)\n\trows, err := qs.List%s(ctx, sqlc.List%sParams{%s})\n", p.camel, p.camel, strings.Join(listParams, ", "))
	w("\tif err != nil {\n\t\treturn nil, 0, database.MapError(\"list %s\", err)\n\t}\n", p.sp.Table)
	switch len(countArgs) {
	case 0:
		w("\ttotal, err := qs.Count%s(ctx)\n", p.camel)
	case 1:
		w("\ttotal, err := qs.Count%s(ctx, %s)\n", p.camel, strings.SplitN(countArgs[0], ":", 2)[1])
	default:
		w("\ttotal, err := qs.Count%s(ctx, sqlc.Count%sParams{%s})\n", p.camel, p.camel, strings.Join(countArgs, ", "))
	}
	w("\tif err != nil {\n\t\treturn nil, 0, database.MapError(\"count %s\", err)\n\t}\n", p.sp.Table)
	w("\tout := make([]%s, len(rows))\n\tfor i, row := range rows {\n\t\tout[i] = %s(row)\n\t}\n\treturn out, total, nil\n}\n\n", p.rowType, p.rowType)

	if ro {
		w("func (%s) Meta(row %s) crud.Meta {\n\treturn crud.Meta{ID: row.ID, UUID: row.Uuid, UpdatedAt: row.UpdatedAt}\n}\n", impl, p.rowType)
		return b.String()
	}

	params := func() string {
		var lines []string
		for _, f := range p.reqs {
			lines = append(lines, fmt.Sprintf("%s: %s", sqlcName(f.Col), f.Param))
		}
		return strings.Join(lines, ",\n\t\t")
	}
	resolve := func(ret string) {
		if len(p.fks) > 0 {
			w("\trefs, err := r.resolve(ctx, req)\n\tif err != nil {\n\t\treturn %s\n\t}\n", ret)
		}
	}
	w("func (r %s) Create(ctx context.Context, req %s, actor string) (uuid.UUID, error) {\n", impl, req)
	resolve("uuid.Nil, err")
	w("\tid, err := r.tx.Queries(ctx).Create%s(ctx, sqlc.Create%sParams{\n\t\tUuid: crud.NewID(),\n\t\t%s,\n\t\tActor: &actor,\n\t})\n", p.camel, p.camel, params())
	w("\treturn id, database.MapError(\"create %s\", err)\n}\n\n", p.sp.Table)

	w("func (r %s) Update(ctx context.Context, id uuid.UUID, req %s, actor string, expected time.Time) error {\n", impl, req)
	resolve("err")
	w("\tn, err := r.tx.Queries(ctx).Update%s(ctx, sqlc.Update%sParams{\n\t\t%s,\n\t\tActor: &actor,\n\t\tUuid: id,\n\t\tExpectedUpdatedAt: expected,\n\t})\n", p.camel, p.camel, params())
	w("\treturn crud.Affected(\"update %s\", n, err)\n}\n\n", p.sp.Table)

	w("func (r %s) SoftDelete(ctx context.Context, id uuid.UUID, actor string, expected time.Time) error {\n", impl)
	w("\tn, err := r.tx.Queries(ctx).SoftDelete%s(ctx, sqlc.SoftDelete%sParams{Actor: &actor, Uuid: id, ExpectedUpdatedAt: expected})\n", p.camel, p.camel)
	w("\treturn crud.Affected(\"delete %s\", n, err)\n}\n\n", p.sp.Table)

	w("func (%s) Meta(row %s) crud.Meta {\n\treturn crud.Meta{ID: row.ID, UUID: row.Uuid, UpdatedAt: row.UpdatedAt}\n}\n", impl, p.rowType)
	return b.String()
}

// genService renders internal/service/<table>_gen.go: the CRUD resource.
func genService(p *plan) (string, error) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	var std []string
	if p.sp.Check != "" || p.sp.AfterWrite != "" || p.sp.CheckDelete != "" {
		std = append(std, "context")
	}
	var third []string
	if len(p.sp.Files) > 0 {
		third = append(third, "github.com/google/uuid")
	}
	w("%s", genHeader+"package service\n\n"+imports(std, third,
		[]string{"pfm-backend/internal/db/sqlc", "pfm-backend/internal/dto", "pfm-backend/internal/platform/crud",
			"pfm-backend/internal/repository"}))

	typ := fmt.Sprintf("crud.Resource[%s, dto.%sRequest, dto.%sResponse]", p.rowType, p.gn, p.gn)
	if p.sp.readOnly() {
		typ = fmt.Sprintf("crud.ReadResource[%s, dto.%sResponse]", p.rowType, p.gn)
	}
	var sorts []string
	for _, s := range p.sp.Sorts {
		sorts = append(sorts, fmt.Sprintf("%q", s))
	}
	sorts = append(sorts, `"created_at"`)
	var filters []string
	if len(p.sp.Search) > 0 {
		filters = append(filters, `"q"`)
	}
	for _, c := range p.filterFKs {
		filters = append(filters, fmt.Sprintf("%q", fkBase(c.Name)+"_uuid"))
	}
	w("// %sResource is the CRUD resource of %s.\nfunc %sResource(d Deps) %s {\n\treturn %s{\n", p.gn, p.sp.Table, p.gn, typ, typ)
	w("\t\tTable: %q,\n\t\tLabel: %q,\n\t\tPath: %q,\n", p.sp.Table, p.sp.Label, p.sp.Path)
	w("\t\tSorts: []string{%s},\n\t\tDefaultSort: %q,\n\t\tFilters: []string{%s},\n", strings.Join(sorts, ", "), p.sp.DefaultSort, strings.Join(filters, ", "))
	w("\t\tStore: repository.New%sRepository(d.TX),\n\t\tResponse: dto.%sFromRow,\n", p.gn, p.gn)
	if len(p.sp.NestedUnder) > 0 {
		w("\t\tNested: []crud.Nested{\n")
		for _, col := range p.sp.NestedUnder {
			ref, ok := p.t.FKs[col]
			if !ok {
				return "", fmt.Errorf("%s: NestedUnder %s bukan FK", p.sp.Table, col)
			}
			w("\t\t\t{ParentPath: %q, Filter: %q},\n", "/"+kebabPlural(ref), fkBase(col)+"_uuid")
		}
		w("\t\t},\n")
	}
	if p.sp.Check != "" {
		w("\t\tCheck: func(ctx context.Context, req *dto.%sRequest, cur *%s) error {\n\t\t\treturn %s(ctx, d, req, cur)\n\t\t},\n",
			p.gn, p.rowType, p.sp.Check)
	}
	if p.sp.AfterWrite != "" {
		w("\t\tAfterWrite: func(ctx context.Context, before, after *%s) error {\n\t\t\treturn %s(ctx, d, before, after)\n\t\t},\n",
			p.rowType, p.sp.AfterWrite)
	}
	if p.sp.CheckDelete != "" {
		w("\t\tCheckDelete: func(ctx context.Context, cur *%s) error {\n\t\t\treturn %s(ctx, d, cur)\n\t\t},\n", p.rowType, p.sp.CheckDelete)
	}
	if len(p.sp.Files) > 0 {
		cols := make([]string, 0, len(p.sp.Files))
		for c := range p.sp.Files {
			cols = append(cols, c)
		}
		slices.Sort(cols)
		w("\t\tFiles: []crud.FileField[%s, dto.%sRequest]{\n", p.rowType, p.gn)
		for _, c := range cols {
			field := fkBase(c) + "_uuid"
			w("\t\t\t{Field: %q, Purpose: %q,\n\t\t\t\tGet: func(r *dto.%sRequest) string { return r.%s },\n\t\t\t\tCurrent: func(r *%s) *uuid.UUID { return r.%s }},\n",
				field, p.sp.Files[c], p.gn, goName(field), p.rowType, sqlcName(field))
		}
		w("\t\t},\n")
	}
	if p.sp.Import {
		w("\t\tImport: true,\n")
	}
	w("\t}\n}\n")
	return b.String(), nil
}

// genRegistry renders internal/service/resources_<module>_gen.go.
func genRegistry(module string, specs []*Spec) string {
	var b strings.Builder
	fn := goName(module) + "Resources"
	b.WriteString(genHeader + "package service\n\nimport \"pfm-backend/internal/platform/crud\"\n\n")
	fmt.Fprintf(&b, "// %s lists every scaffolded CRUD resource of the %s module.\nfunc %s(d Deps) []crud.Registrar {\n\treturn []crud.Registrar{\n", fn, module, fn)
	for _, sp := range specs {
		fmt.Fprintf(&b, "\t\t%sResource(d),\n", goName(sp.Table))
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}
