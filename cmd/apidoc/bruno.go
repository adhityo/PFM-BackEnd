package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"pfm-backend/internal/permission"
)

// kept are the entries of the collection folder apidoc never deletes.
var kept = []string{"environments", "_custom", ".git", ".gitignore"}

var unsafeName = regexp.MustCompile(`[/\\:*?"<>|]+`)

func fileName(name string) string { return strings.TrimSpace(unsafeName.ReplaceAllString(name, "-")) }

func titleFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}

func block(name, content string) string { return name + " {\n" + indent(content) + "\n}\n" }

func moduleFolder(module string) (string, int) {
	if module == "auth" {
		return "01 Autentikasi", 1
	}
	for i, m := range permission.Modules {
		if m.Code == module {
			return fmt.Sprintf("%02d %s", i+2, m.Label), i + 2
		}
	}
	return "99 Lainnya", 99
}

func accessText(r *request) string {
	switch r.access {
	case "public":
		return "publik (tanpa token)"
	case "authenticated":
		return "login (tanpa permission khusus)"
	}
	return "permission `" + r.perm + "`"
}

func pathParams(path string, collections map[string]string) [][2]string {
	var out [][2]string
	segs := strings.Split(path, "/")
	for i, s := range segs {
		if !strings.HasPrefix(s, ":") || i == 0 {
			continue
		}
		v := "{{uuid}}"
		if t, ok := collections["/"+segs[i-1]]; ok {
			v = uuidVar(t)
		}
		out = append(out, [2]string{s[1:], v})
	}
	return out
}

func requestDocs(r *request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n`%s /api/v1%s`\n\n**Akses:** %s\n", r.name, r.method, r.path, accessText(r))
	if r.desc != "" {
		b.WriteString("\n" + r.desc + "\n")
	}
	if r.ifMatch {
		b.WriteString("\n**Header `If-Match` wajib:** jalankan request Detail lebih dulu; ETag respons tersimpan otomatis ke `{{etag}}`. Data yang sudah berubah ditolak 412, tanpa header ditolak 428.\n")
	}
	if len(r.query) > 0 {
		b.WriteString("\n### Query\n\n| Param | Contoh | Keterangan |\n|---|---|---|\n")
		for _, q := range r.query {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", q.Name, q.Value, q.Doc)
		}
	}
	if len(r.form) > 0 {
		b.WriteString("\n### Body (multipart/form-data)\n\n| Field | Contoh |\n|---|---|\n")
		for _, f := range r.form {
			fmt.Fprintf(&b, "| `%s` | `%s` |\n", f.Name, f.Value)
		}
	}
	if r.req != nil {
		b.WriteString("\n" + fieldTable("Body", r.req, true))
	}
	if r.resp != nil {
		resp, title := r.resp, "Respons (`data`)"
		switch t := reflect.TypeOf(resp); {
		case r.list:
			title = "Respons (`data[]`, `meta`: `page`, `per_page`, `total`)"
		case t.Kind() == reflect.Slice: // unpaged array
			resp, title = reflect.Zero(t.Elem()).Interface(), "Respons (`data[]`)"
		}
		b.WriteString("\n" + fieldTable(title, resp, false))
	}
	return b.String()
}

func jsonText(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func requestBru(r *request, collections map[string]string) (string, error) {
	var b strings.Builder
	b.WriteString(block("meta", fmt.Sprintf("name: %s\ntype: http\nseq: %d", r.name, r.seq)))
	bodyMode := "none"
	switch {
	case r.body != nil:
		bodyMode = "json"
	case r.text != "":
		bodyMode = "text"
	case len(r.form) > 0:
		bodyMode = "multipartForm"
	}
	authMode := "inherit"
	if r.access == "public" {
		authMode = "none"
	}
	url := "{{baseUrl}}" + r.path
	var qs, qlines []string
	for _, q := range r.query {
		if q.Enabled {
			qs = append(qs, q.Name+"="+q.Value)
			qlines = append(qlines, q.Name+": "+q.Value)
		} else {
			qlines = append(qlines, "~"+q.Name+": "+q.Value)
		}
	}
	if len(qs) > 0 {
		url += "?" + strings.Join(qs, "&")
	}
	b.WriteString("\n" + block(strings.ToLower(r.method), fmt.Sprintf("url: %s\nbody: %s\nauth: %s", url, bodyMode, authMode)))
	if len(qlines) > 0 {
		b.WriteString("\n" + block("params:query", strings.Join(qlines, "\n")))
	}
	if pp := pathParams(r.path, collections); len(pp) > 0 {
		var lines []string
		for _, p := range pp {
			lines = append(lines, p[0]+": "+p[1])
		}
		b.WriteString("\n" + block("params:path", strings.Join(lines, "\n")))
	}
	var headers []string
	if r.ifMatch {
		headers = append(headers, "If-Match: {{etag}}")
	}
	if r.textType != "" {
		headers = append(headers, "Content-Type: "+r.textType)
	}
	if len(headers) > 0 {
		b.WriteString("\n" + block("headers", strings.Join(headers, "\n")))
	}
	switch bodyMode {
	case "json":
		j, err := jsonText(r.body)
		if err != nil {
			return "", err
		}
		b.WriteString("\n" + block("body:json", j))
	case "text":
		b.WriteString("\n" + block("body:text", r.text))
	case "multipartForm":
		var lines []string
		for _, f := range r.form {
			v := f.Value
			if f.File {
				v = "@file(" + v + ")"
			}
			lines = append(lines, f.Name+": "+v)
		}
		b.WriteString("\n" + block("body:multipart-form", strings.Join(lines, "\n")))
	}
	if r.script != "" {
		b.WriteString("\n" + block("script:post-response", r.script))
	}
	b.WriteString("\n" + block("docs", requestDocs(r)))
	return b.String(), nil
}

const collectionScript = `const etag = res.getHeader("etag");
if (etag) {
  bru.setVar("etag", etag);
}`

const overview = `# PFM Backend API

Koleksi ini **dihasilkan dari kode backend** (` + "`make bruno`" + ` di repo pfm-backend): setiap route, permission, contoh body dan tabel field diambil dari router dan DTO. Jangan mengedit file hasil generate; simpan request tambahan di folder ` + "`_custom`" + ` (tidak disentuh generator).

## Mulai

1. Jalankan API (` + "`make run`" + ` / ` + "`make dev`" + `) dan pilih environment **Local**. Sesuaikan ` + "`baseUrl`" + ` dengan ` + "`HTTP_ADDR`" + `.
2. Isi variabel rahasia ` + "`adminPassword`" + ` (dan ` + "`newPassword`" + ` untuk aktivasi) di environment.
3. Jalankan **01 Autentikasi → Login**. Token tersimpan otomatis dan dipakai semua request (auth koleksi: Bearer ` + "`{{accessToken}}`" + `). Token akses berlaku 15 menit; jalankan **Refresh token** bila 401.

## Konvensi

- **Variabel UUID:** request *Tambah* menyimpan UUID baru ke ` + "`{{<resource>Uuid}}`" + ` (mis. ` + "`{{bondUuid}}`" + `, ` + "`{{roleUuid}}`" + `), dipakai request lain sebagai path param atau field relasi.
- **ETag / If-Match:** setiap respons ber-ETag (Detail, Ubah, aksi) menyimpan ` + "`{{etag}}`" + `. Ubah, Hapus dan aksi mewajibkan ` + "`If-Match`" + `: jalankan Detail data yang sama lebih dulu. 412 = data sudah berubah, 428 = header tidak ada.
- **Daftar:** ` + "`page`" + `, ` + "`per_page`" + ` (maks 100), ` + "`sort`" + ` (` + "`field`" + ` naik, ` + "`-field`" + ` turun) dan filter; respons ` + "`{data: [...], meta: {page, per_page, total}}`" + `.
- **Permission:** setiap request mencantumkan permission yang dibutuhkan. ADMIN memegang semuanya; role lain diatur di **02 Sistem & Keamanan → Permission role**. Header ` + "`X-Permissions-Version`" + ` berubah bila permission Anda berubah.
- **File:** unggah dulu (**Sistem & Keamanan → File**), lalu isi UUID-nya (` + "`{{fileUuid}}`" + `) ke field ` + "`*_file_uuid`" + `.

## Format error

` + "```json" + `
{"error": {"code": "VALIDATION_ERROR", "message": "input tidak valid",
           "details": [{"field": "bond_code", "rule": "required", "message": "wajib diisi"}]},
 "meta": {"request_id": "..."}}
` + "```" + `

| Status | Arti |
|---|---|
| 400 | Body tidak dapat dibaca |
| 401 | Token tidak ada / kedaluwarsa / akun nonaktif |
| 403 | Permission kurang (` + "`details[].field = \"permission\"`" + `, ` + "`rule`" + ` = kode) atau aturan maker-checker / anti-eskalasi |
| 404 | Data tidak ada atau di luar client Anda |
| 409 | Konflik aturan bisnis (status, data masih dirujuk, duplikat) |
| 412 / 428 | ETag usang / header ` + "`If-Match`" + ` tidak ada |
| 422 | Validasi gagal (` + "`details[]`" + ` per field) |
| 429 | Terlalu banyak request |
`

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // inside the chosen collection folder
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644) //nolint:gosec // documentation files
}

// writeBruno replaces the generated content of dir (the collection folder
// chosen by the developer on the command line).
func writeBruno(dir string, reqs []*request) error {
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // collection folder given on the command line
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !slices.Contains(kept, e.Name()) {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil { //nolint:gosec // entries of the chosen collection folder

				return err
			}
		}
	}
	files := map[string]string{
		"bruno.json": `{
  "version": "1",
  "name": "PFM Backend API",
  "type": "collection",
  "ignore": ["node_modules", ".git"]
}
`,
		"collection.bru": block("auth", "mode: bearer") + "\n" + block("auth:bearer", "token: {{accessToken}}") + "\n" +
			block("script:post-response", collectionScript) + "\n" + block("docs", overview),
		"README.md":           overview,
		"fixtures/contoh.pdf": "%PDF-1.4\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n2 0 obj << /Type /Pages /Kids [] /Count 0 >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n",
	}
	optional := map[string]string{
		"environments/Local.bru": block("vars", "baseUrl: http://localhost:8081/api/v1\nadminUsername: admin\naccessToken: \nrefreshToken: ") +
			"vars:secret [\n  adminPassword,\n  newPassword\n]\n",
		".gitignore": ".env\n",
	}
	for name, content := range files {
		if err := writeFile(filepath.Join(dir, name), content); err != nil {
			return err
		}
	}
	for name, content := range optional {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) { //nolint:gosec // inside the chosen collection folder
			if err := writeFile(filepath.Join(dir, name), content); err != nil {
				return err
			}
		}
	}
	var pngBuf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 37, G: 99, B: 235, A: 255})
	if err := png.Encode(&pngBuf, img); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures/contoh.png"), pngBuf.Bytes(), 0o644); err != nil { //nolint:gosec // fixture
		return err
	}

	collections := map[string]string{"/users": "app_user", "/roles": "role", "/actions": "action", "/files": "file_object",
		"/activity-logs": "activity_log", "/finance-transactions": "finance_transaction"}
	for _, r := range reqs {
		if r.kind == kindList && r.method == "GET" && !strings.Contains(r.path, ":") && r.table != "auth" {
			if _, ok := collections[r.path]; !ok {
				collections[r.path] = r.table
			}
		}
	}

	// Folders: module -> resource -> requests.
	type folder struct {
		label string
		reqs  []*request
	}
	modules := map[string]map[string]*folder{}
	for _, r := range reqs {
		if modules[r.module] == nil {
			modules[r.module] = map[string]*folder{}
		}
		f := modules[r.module][r.table]
		if f == nil {
			f = &folder{label: r.label}
			modules[r.module][r.table] = f
		}
		f.reqs = append(f.reqs, r)
	}
	for module, resources := range modules {
		modName, modSeq := moduleFolder(module)
		modDir := filepath.Join(dir, modName)
		tables := make([]string, 0, len(resources))
		for t := range resources {
			tables = append(tables, t)
		}
		slices.SortFunc(tables, func(a, b string) int { return strings.Compare(resources[a].label, resources[b].label) })
		modDocs := "# " + strings.TrimSpace(modName[3:]) + "\n\n| Resource | Kode permission |\n|---|---|\n"
		for _, t := range tables {
			modDocs += fmt.Sprintf("| %s | `%s:*` |\n", titleFirst(resources[t].label), t)
		}
		if module == "auth" {
			modDocs = "# Autentikasi\n\nLogin, token, aktivasi akun dan profil sesi (`/me`)."
		}
		if err := writeFile(filepath.Join(modDir, "folder.bru"),
			block("meta", fmt.Sprintf("name: %s\nseq: %d", modName, modSeq))+"\n"+block("docs", modDocs)); err != nil {
			return err
		}
		usedNames := map[string]bool{}
		for i, t := range tables {
			f := resources[t]
			name := titleFirst(f.label)
			if usedNames[name] {
				name += " (" + t + ")"
			}
			usedNames[name] = true
			resDir := modDir
			if module != "auth" {
				resDir = filepath.Join(modDir, fileName(name))
				var perms []string
				for _, r := range f.reqs {
					if r.perm != "" && !slices.Contains(perms, r.perm) {
						perms = append(perms, r.perm)
					}
				}
				docs := "# " + name + "\n\nResource `" + t + "`. Permission: " + joinCode(perms) + "."
				if err := writeFile(filepath.Join(resDir, "folder.bru"),
					block("meta", fmt.Sprintf("name: %s\nseq: %d", name, i+1))+"\n"+block("docs", docs)); err != nil {
					return err
				}
			}
			slices.SortStableFunc(f.reqs, func(a, b *request) int {
				ka, kb := a.seq, b.seq
				if ka == 0 {
					ka = 100 + a.kind*1000 + a.order
				}
				if kb == 0 {
					kb = 100 + b.kind*1000 + b.order
				}
				return ka - kb
			})
			seen := map[string]bool{}
			for n, r := range f.reqs {
				r.seq = n + 1
				fn := fileName(r.name)
				for seen[fn] {
					fn += " 2"
				}
				seen[fn] = true
				content, err := requestBru(r, collections)
				if err != nil {
					return err
				}
				if err := writeFile(filepath.Join(resDir, fn+".bru"), content); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
