package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Spec configures code generation for one CRUD table. Columns are referenced
// by their database name; foreign keys by their *_id column.
type Spec struct {
	Table       string
	Module      string
	Label       string            // human label used in permission names
	Path        string            // URL path, default: kebab-case plural of Table
	Required    []string          // default: <table>_name and <table>_code when present
	Search      []string          // ILIKE columns for ?q=, default: *_name and *_code columns
	Sorts       []string          // sortable columns besides created_at
	DefaultSort string            // default: <table>_code, <table>_name, or -created_at
	ReadOnly    []string          // computed columns: returned, never written by clients
	Exclude     []string          // columns hidden from the API entirely
	Snapshot    map[string]SnapFK // column copied from a referenced row on write
	Validate    map[string]string // column -> extra validator tags
	Check       string            // hand-written hook run before create/update (service package)
	AfterWrite  string            // hand-written hook run after create/update/delete (service package)
	CheckDelete string            // hand-written hook run before delete (service package)
	// Computed gives an SQL expression (sqlc params allowed) used instead of the
	// plain parameter when inserting/updating the column.
	Computed    map[string]string
	Enum        map[string][]string         // SMALLINT column -> API names by code (0..n)
	EnumMap     map[string]map[int16]string // SMALLINT column -> API names by explicit code (e.g. -1)
	PII         []string                    // columns masked for actors without pii:read
	Files       map[string]string           // *_file_id column -> upload purpose
	NestedUnder []string                    // FK columns whose parent gets a nested list route
	Kind        string                      // "" = writable CRUD, "read" = list/detail only
	Import      bool                        // enable POST <path>/import (CSV)
	// Scope is an SQL expression (alias t) yielding the owning client_id; when
	// set, reads are limited to the actor's client (PRD §12.3).
	Scope string
}

func (sp *Spec) readOnly() bool { return sp.Kind == "read" }

// SnapFK fills a snapshot column from RefColumn of the row referenced by FKColumn.
type SnapFK struct {
	FKColumn  string
	RefColumn string
}

// audit and identity columns are managed by the platform.
var managedColumns = []string{"id", "uuid", "created_at", "created_by", "updated_at", "updated_by", "deleted_at", "deleted_by"}

func (sp *Spec) applyDefaults(t *Table) {
	if sp.Path == "" {
		sp.Path = "/" + kebabPlural(sp.Table)
	}
	if sp.Label == "" {
		sp.Label = strings.ReplaceAll(sp.Table, "_", " ")
	}
	name, code := sp.Table+"_name", sp.Table+"_code"
	_, hasName := t.Column(name)
	_, hasCode := t.Column(code)
	if sp.Required == nil {
		if hasName {
			sp.Required = append(sp.Required, name)
		}
		if hasCode {
			sp.Required = append(sp.Required, code)
		}
	}
	if sp.Search == nil {
		for _, c := range sp.apiColumns(t) {
			if (strings.HasSuffix(c.Name, "_name") || strings.HasSuffix(c.Name, "_code")) && !sp.isSnapshot(c.Name) {
				if _, fk := t.FKs[c.Name]; !fk && isText(c) {
					sp.Search = append(sp.Search, c.Name)
				}
			}
		}
	}
	if sp.Sorts == nil {
		if hasCode {
			sp.Sorts = append(sp.Sorts, code)
		}
		if hasName {
			sp.Sorts = append(sp.Sorts, name)
		}
	}
	if sp.DefaultSort == "" {
		switch {
		case hasCode:
			sp.DefaultSort = code
		case hasName:
			sp.DefaultSort = name
		default:
			sp.DefaultSort = "-created_at"
		}
	}
}

// schemaTables is the introspected schema (set by main), used to find the
// display columns of referenced tables.
var schemaTables Schema

// labelCol is a display column of a referenced row exposed next to its FK.
type labelCol struct{ Source, Alias string }

// labelColumns returns, for an FK column, the referenced table's <ref>_name
// and <ref>_code (or <ref>_number), exposed as <base>_name / <base>_code so
// clients can show relations without extra requests. A label is skipped when
// the table already has a column of that name (e.g. a snapshot column).
func labelColumns(t *Table, fkCol string) []labelCol {
	ref := t.FKs[fkCol]
	rt := schemaTables[ref]
	if rt == nil {
		return nil
	}
	base := fkBase(fkCol)
	var out []labelCol
	try := func(src, suffix string) bool {
		c, ok := rt.Column(src)
		if !ok || !isText(c) {
			return false
		}
		if _, clash := t.Column(base + suffix); !clash {
			out = append(out, labelCol{Source: src, Alias: base + suffix})
		}
		return true
	}
	try(ref+"_name", "_name")
	if !try(ref+"_code", "_code") {
		try(ref+"_number", "_number")
	}
	return out
}

func (sp *Spec) isSnapshot(col string) bool { _, ok := sp.Snapshot[col]; return ok }

// apiColumns are the business columns visible through the API, in table order.
func (sp *Spec) apiColumns(t *Table) []Column {
	var out []Column
	for _, c := range t.Columns {
		if slices.Contains(managedColumns, c.Name) || slices.Contains(sp.Exclude, c.Name) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// writable reports whether clients may set the column.
func (sp *Spec) writable(c Column) bool {
	_, enum := sp.Enum[c.Name]
	_, enumMap := sp.EnumMap[c.Name]
	return !sp.readOnly() && !enum && !enumMap && !slices.Contains(sp.ReadOnly, c.Name) && !sp.isSnapshot(c.Name)
}

func isText(c Column) bool { return c.DataType == "character varying" || c.DataType == "text" }

// ---------------------------------------------------------------- naming

var reWord = regexp.MustCompile(`[a-z0-9]+`)

// sqlcName reproduces sqlc's Go naming: snake parts title-cased, "id" -> "ID".
func sqlcName(s string) string {
	var b strings.Builder
	for _, p := range reWord.FindAllString(s, -1) {
		if p == "id" {
			b.WriteString("ID")
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// goName is like sqlcName but with Go initialisms for our own structs.
func goName(s string) string {
	var b strings.Builder
	for _, p := range reWord.FindAllString(s, -1) {
		switch p {
		case "id", "uuid", "url", "api", "ip", "npwp", "nav":
			b.WriteString(strings.ToUpper(p))
		default:
			b.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return b.String()
}

func lowerFirst(s string) string { return strings.ToLower(s[:1]) + s[1:] }

// fkBase turns bank_class_id into bank_class.
func fkBase(col string) string { return strings.TrimSuffix(col, "_id") }

// kebabPlural follows the PRD §10.1 resource naming.
func kebabPlural(table string) string {
	overrides := map[string]string{
		"indices": "indices", "synchronized_data": "synchronized-data", "configuration": "configuration",
		"app_user": "users", "file_object": "files",
		"document_authorization_table":          "document-authorizations",
		"document_category_authorization_table": "document-category-authorizations",
	}
	if o, ok := overrides[table]; ok {
		return o
	}
	w := table
	switch {
	case regexp.MustCompile(`[^aeiou]y$`).MatchString(w):
		w = w[:len(w)-1] + "ies"
	case regexp.MustCompile(`(s|x|ch|sh)$`).MatchString(w):
		w += "es"
	default:
		w += "s"
	}
	return strings.ReplaceAll(w, "_", "-")
}

func mustColumn(t *Table, name, ctx string) Column {
	c, ok := t.Column(name)
	if !ok {
		panic(fmt.Sprintf("%s: column %s.%s tidak ada", ctx, t.Name, name))
	}
	return c
}
