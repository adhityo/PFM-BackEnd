// Command apidoc generates API documentation from the router (dev tool).
//
//	apidoc bruno <dir>   -> Bruno collection (.bru) with every route
//
// Requests, access rules, sample bodies and field tables come from the code:
// the routes recorded by auth.Router, the CRUD resource descriptions and the
// request/response DTOs. Hand-written endpoints are described in specs.go;
// the command fails when a route has no description (or a description has no
// route), so the collection cannot fall behind the API.
package main

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/router"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "bruno" {
		fmt.Fprintln(os.Stderr, "pakai: apidoc bruno <folder-koleksi>")
		os.Exit(2)
	}
	reqs, err := build()
	if err == nil {
		err = writeBruno(os.Args[2], reqs)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "apidoc:", err)
		os.Exit(1)
	}
	fmt.Printf("koleksi Bruno: %d request di %s\n", len(reqs), os.Args[2])
}

// Request kinds, in folder order.
const (
	kindCreate = 1
	kindList   = 2
	kindDetail = 3
	kindUpdate = 4
	kindAction = 5
	kindNested = 6
	kindImport = 7
	kindDelete = 9
)

type request struct {
	module, table, label string // folder: module -> resource
	name                 string
	kind, order, seq     int
	method, path         string
	access, perm         string
	query                []param
	ifMatch              bool
	body                 any
	text, textType       string
	form                 []formField
	req, resp            any
	list                 bool
	script, desc         string
}

func joinCode(items []string) string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = "`" + s + "`"
	}
	return strings.Join(out, ", ")
}

func sortOptions(sorts []string) []string {
	var out []string
	for _, s := range sorts {
		if !strings.HasPrefix(s, "-") && !slices.Contains(out, s) {
			out = append(out, s, "-"+s)
		}
	}
	for _, s := range sorts {
		if strings.HasPrefix(s, "-") && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// crudRequests documents the standard routes of a resource (PRD §10.3).
func crudRequests(d crud.ResourceDoc, labels map[string]string) map[string]*request {
	out := map[string]*request{}
	add := func(r *request) {
		r.module, r.table, r.label, r.resp = d.Module, d.Table, d.Label, d.Response
		out[r.method+" "+r.path] = r
	}
	var filters []param
	for _, f := range d.Filters {
		switch {
		case f == "q":
			filters = append(filters, filter("q", "", "pencarian teks"))
		case strings.HasSuffix(f, "_uuid"):
			filters = append(filters, filter(f, refVar(d.Table, f, d.Files), "filter relasi"))
		default:
			filters = append(filters, filter(f, "", "filter"))
		}
	}
	pii := ""
	if d.PII {
		pii = " Field data pribadi dimasking tanpa permission `pii:read`."
	}
	add(&request{kind: kindList, name: "Daftar " + d.Label, method: "GET", path: d.Path, list: true,
		query: listQuery(sortOptions(d.Sorts), d.DefaultSort, filters...),
		desc:  "Daftar " + d.Label + " dengan paging dan urutan. Data otomatis dibatasi ke client Anda bila resource ini milik client." + pii})
	add(&request{kind: kindDetail, name: "Detail " + d.Label, method: "GET", path: d.Path + "/:uuid",
		desc: "ETag respons tersimpan otomatis ke `{{etag}}` untuk Ubah/Hapus/aksi." + pii})
	for _, n := range d.Nested {
		parent := labels[strings.TrimPrefix(n.ParentPath, "/")]
		add(&request{kind: kindNested, name: "Daftar " + d.Label + " per " + parent, method: "GET", list: true,
			path: n.ParentPath + "/:parent_uuid" + d.Path, query: listQuery(sortOptions(d.Sorts), d.DefaultSort),
			desc: "Daftar " + d.Label + " milik satu " + parent + " (sama dengan filter `" + n.Filter + "`)."})
	}
	if d.ReadOnly {
		return out
	}
	ctx := sampleCtx{table: d.Table, files: d.Files}
	body := sampleObject(reflect.TypeOf(d.Request), ctx)
	for k, v := range bodyOverrides[d.Table] {
		body[k] = v
	}
	fileNote := ""
	if len(d.Files) > 0 {
		var fs []string
		for f, purpose := range d.Files {
			fs = append(fs, "`"+f+"` (purpose `"+purpose+"`)")
		}
		slices.Sort(fs)
		fileNote = " File diunggah dulu lewat Sistem → File, lalu UUID-nya diisi ke " + strings.Join(fs, ", ") + "."
	}
	add(&request{kind: kindCreate, name: "Tambah " + d.Label, method: "POST", path: d.Path, body: body, req: d.Request,
		script: saveUUID(strings.Trim(uuidVar(d.Table), "{}"), ""),
		desc:   "Contoh body hanya berisi field wajib; field lain ada di tabel. UUID baru tersimpan ke `" + uuidVar(d.Table) + "`." + fileNote})
	add(&request{kind: kindUpdate, name: "Ubah " + d.Label, method: "PUT", path: d.Path + "/:uuid", body: body, req: d.Request,
		ifMatch: true, desc: "Mengganti seluruh field (PUT)." + fileNote})
	add(&request{kind: kindDelete, name: "Hapus " + d.Label, method: "DELETE", path: d.Path + "/:uuid", ifMatch: true,
		desc: "Soft delete. Ditolak 409 bila masih dirujuk data aktif lain."})
	if d.Import {
		var head, row []string
		for k, v := range body {
			head = append(head, k)
			row = append(row, fmt.Sprint(v))
		}
		add(&request{kind: kindImport, name: "Import CSV " + d.Label, method: "POST", path: d.Path + "/import",
			text: strings.Join(head, ",") + "\n" + strings.Join(row, ","), textType: "text/csv", req: d.Request,
			desc: "CSV (`Content-Type: text/csv`), header = nama field JSON, maks 10.000 baris, semua-atau-tidak-sama-sekali."})
	}
	return out
}

func specKind(method, path string, collections map[string]string) int {
	switch {
	case method == "GET" && !strings.Contains(path, ":"):
		return kindList
	case method == "GET" && strings.HasSuffix(path, "/:uuid"):
		return kindDetail
	case method == "PUT" && strings.HasSuffix(path, "/:uuid"):
		return kindUpdate
	case method == "DELETE" && strings.HasSuffix(path, "/:uuid"):
		return kindDelete
	case method == "POST" && collections[path] != "":
		return kindCreate
	}
	return kindAction
}

var kindVerb = map[int]string{kindList: "Daftar", kindDetail: "Detail", kindUpdate: "Ubah", kindDelete: "Hapus", kindCreate: "Tambah"}

func build() ([]*request, error) {
	routes, docs := router.Describe()
	catalog := map[string]auth.Permission{}
	for _, p := range router.Catalog() {
		catalog[p.Code] = p
	}
	byTable := map[string]crud.ResourceDoc{}
	collections := map[string]string{"/users": "app_user", "/roles": "role", "/actions": "action", "/files": "file_object",
		"/activity-logs": "activity_log", "/finance-transactions": "finance_transaction"}
	labels := map[string]string{}
	for _, d := range docs {
		byTable[d.Table] = d
		collections[d.Path] = d.Table
		labels[strings.TrimPrefix(d.Path, "/")] = d.Label
	}
	generated := map[string]*request{}
	for _, d := range docs {
		for k, r := range crudRequests(d, labels) {
			generated[k] = r
		}
	}

	var out []*request
	var problems []string
	used := map[string]bool{}
	for i, rt := range routes {
		key := rt.Method + " " + rt.Path
		if r, ok := generated[key]; ok {
			r.order, r.access, r.perm = i, rt.Access, rt.Permission
			out = append(out, r)
			used[key] = true
			continue
		}
		s, ok := specs[key]
		if !ok {
			problems = append(problems, "route tanpa dokumentasi (tambahkan di cmd/apidoc/specs.go): "+key)
			continue
		}
		used[key] = true
		r := &request{method: rt.Method, path: rt.Path, access: rt.Access, perm: rt.Permission, order: i, seq: s.Seq,
			kind: specKind(rt.Method, rt.Path, collections), name: s.Name, desc: s.Desc, body: s.Body, req: s.Req,
			resp: s.Resp, ifMatch: s.IfMatch, query: s.Query, script: s.Script, form: s.Form}
		switch {
		case s.Folder == "auth":
			r.module, r.table, r.label = "auth", "auth", "Autentikasi"
		case s.Table != "":
			r.module, r.table = permission.ModuleSystem, s.Table
			r.label = permission.ResourceLabels[s.Table]
		default:
			p := catalog[rt.Permission]
			r.module, r.table, r.label = p.Module, p.Resource(), p.ResourceLabel
		}
		if s.Order != 0 {
			r.order = s.Order
		}
		if d, ok := byTable[r.table]; ok && r.resp == nil && r.kind != kindDelete {
			r.resp = d.Response
		}
		r.list = r.kind == kindList && r.query != nil
		if r.name == "" {
			r.name = kindVerb[r.kind] + " " + r.label
		}
		out = append(out, r)
	}
	for k := range specs {
		if !used[k] {
			problems = append(problems, "dokumentasi tanpa route (hapus dari specs.go): "+k)
		}
	}
	for k := range generated {
		if !used[k] {
			problems = append(problems, "route CRUD tidak terdaftar di router: "+k)
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, errors.New(strings.Join(problems, "\n"))
	}
	return out, nil
}
