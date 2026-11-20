package main

import (
	"pfm-backend/internal/dto"
	"pfm-backend/internal/service"
)

// spec documents a hand-written (non-CRUD) route. Every such route must have
// an entry here: `apidoc` fails otherwise, so the docs follow the router.
type spec struct {
	Name    string
	Seq     int    // absolute order inside its folder; 0 = automatic
	Order   int    // order among the actions of its folder (follows the business flow); 0 = route order
	Folder  string // "auth" = authentication folder; "" = folder of the permission's resource
	Table   string // resource folder for routes without permission (e.g. files)
	Desc    string
	Body    any // JSON sample; nil = no body
	Req     any // request DTO for the field table
	Resp    any // response DTO; nil = response of the folder's resource
	IfMatch bool
	Query   []param
	Script  string
	Form    []formField
}

type param struct {
	Name, Value string
	Enabled     bool
	Doc         string
}

type formField struct {
	Name, Value string
	File        bool
}

func listQuery(sorts []string, def string, filters ...param) []param {
	q := []param{{Name: "page", Value: "1", Enabled: true, Doc: "halaman, mulai 1"},
		{Name: "per_page", Value: "20", Enabled: true, Doc: "1–100, default 20"},
		{Name: "sort", Value: def, Doc: "pilihan: " + joinCode(sorts)}}
	return append(q, filters...)
}

func filter(name, value, doc string) param { return param{Name: name, Value: value, Doc: doc} }

// saveUUID stores data.uuid (or a deeper path) of a successful response.
func saveUUID(variable, path string) string {
	return "if (res.status < 300 && res.body && res.body.data) {\n  bru.setVar(\"" + variable + "\", res.body.data" + path + ".uuid);\n}"
}

//nolint:gosec // Bruno script reading tokens from the response, not a credential
const saveTokens = `if (res.status === 200) {
  bru.setEnvVar("accessToken", res.body.data.access_token);
  bru.setEnvVar("refreshToken", res.body.data.refresh_token);
}`

const saveActivation = `if (res.status < 300) {
  bru.setVar("userUuid", res.body.data.user.uuid);
  bru.setVar("activationKey", res.body.data.activation_key);
}`

var note = map[string]any{"note": "Catatan keputusan (opsional)"}

func trade(kind, label string) map[string]spec {
	path := map[string]string{"stock": "/stock-trades", "mutual_fund": "/mutual-fund-trades", "crypto": "/crypto-trades"}[kind]
	return map[string]spec{
		"POST " + path + "/:uuid/settle": {Name: "Settle " + label, IfMatch: true,
			Desc: "Draft → settled: memperbarui posisi (rata-rata tertimbang), riwayat, laba/rugi realisasi, dan memposting jurnal dalam 1 transaksi. Settle wajib berurutan per tanggal per posisi."},
		"POST " + path + "/:uuid/cancel": {Name: "Batalkan " + label, IfMatch: true, Desc: "Draft → cancelled."},
		"POST " + path + "/:uuid/reverse": {Name: "Balik " + label, IfMatch: true,
			Desc: "Settled → reversed: membalik posisi dan jurnal. Hanya trade ter-settle terakhir pada posisinya (LIFO)."},
	}
}

var specs = func() map[string]spec {
	m := map[string]spec{
		// ------------------------------------------------------------ auth
		"POST /auth/login": {Name: "Login", Seq: 1, Folder: "auth", Body: map[string]any{"login": "{{adminUsername}}", "password": "{{adminPassword}}"},
			Req: dto.LoginRequest{}, Resp: dto.TokenResponse{}, Script: saveTokens,
			Desc: "Login dengan username atau email. `access_token` (JWT, 15 menit) dan `refresh_token` otomatis disimpan ke environment; request lain memakainya lewat auth koleksi (Bearer). Akun terkunci sementara setelah 5 kali gagal; pesan error sama untuk user tidak dikenal dan password salah. Dibatasi 10 request/menit."},
		"GET /me": {Name: "Profil & permission saya", Seq: 2, Folder: "auth", Resp: dto.MeResponse{},
			Script: "if (res.status === 200) {\n  bru.setVar(\"permissionsVersion\", res.body.data.permissions_version);\n}",
			Desc:   "Bootstrap sesi frontend: user, role, `is_admin`, `permissions[]` (ADMIN: seluruh katalog) dan `permissions_version`. Halaman tampil bila memegang `<resource>:read`, tombol bila memegang kode aksinya. Muat ulang bila header `X-Permissions-Version` respons mana pun berbeda."},
		"POST /auth/refresh": { //nolint:gosec // {{refreshToken}} is a collection variable, not a credential
			Name: "Refresh token", Seq: 3, Folder: "auth", Body: map[string]any{"refresh_token": "{{refreshToken}}"},
			Req: dto.RefreshRequest{}, Resp: dto.TokenResponse{}, Script: saveTokens,
			Desc: "Menukar refresh token dengan pasangan token baru (rotasi). Refresh token lama yang dipakai ulang dianggap dicuri: seluruh keluarga token dicabut."},
		"POST /auth/change-password": {Name: "Ganti password", Seq: 4, Folder: "auth",
			Body: map[string]any{"old_password": "{{adminPassword}}", "new_password": "{{newPassword}}"}, Req: dto.ChangePasswordRequest{},
			Desc: "Ganti password akun sendiri. Minimal 12 karakter, memuat 3 dari 4 jenis (huruf kecil, huruf besar, angka, simbol), tidak mengandung username."},
		"POST /auth/activate": {Name: "Aktivasi akun / atur ulang password", Seq: 5, Folder: "auth",
			Body: map[string]any{"activation_key": "{{activationKey}}", "new_password": "{{newPassword}}"}, Req: dto.ActivateRequest{},
			Desc: "Dipakai halaman frontend `/activate?key=…` (user baru) dan `/reset-password?key=…` (reset admin / lupa password): kirim `key` dari tautan email sebagai `activation_key`. Kunci sekali pakai; semua sesi lama dicabut. Di luar production `activationKey` juga tersimpan otomatis dari respons **Tambah pengguna** / **Reset password**."},
		"POST /auth/forgot-password": {Name: "Lupa password", Seq: 8, Folder: "auth", Body: map[string]any{"email": "staf@example.test"},
			Req:  dto.ForgotPasswordRequest{},
			Desc: "Mengirim tautan atur ulang password ke email akun aktif (berlaku `PASSWORD_RESET_TTL`, default 1 jam). Respons selalu 204, terdaftar atau tidak, agar email tidak bisa ditebak. Password lama tetap berlaku sampai tautan dipakai. Maksimal 1 email per menit per akun; email dikirim worker (cek Mailpit/Mailtrap)."},
		"POST /auth/logout": {
			Name: "Logout", Seq: 6, Folder: "auth", Body: map[string]any{"refresh_token": "{{refreshToken}}"}, //nolint:gosec // collection variable, not a credential
			Req: dto.RefreshRequest{}, Desc: "Mencabut refresh token sesi ini. Access token tetap berlaku sampai kedaluwarsa (maks 15 menit)."},
		"POST /auth/logout-all": {Name: "Logout semua perangkat", Seq: 7, Folder: "auth", Desc: "Mencabut semua refresh token milik akun ini."},

		// ------------------------------------------------------------ users
		"GET /users": {Resp: dto.UserResponse{}, Query: listQuery(service.UserSorts, "-created_at",
			filter("q", "", "cari username/email/nama"), filter("role_uuid", "{{roleUuid}}", "filter role"), filter("status", "active", "active | inactive")),
			Desc: "Daftar pengguna. User yang terikat ke client hanya melihat pengguna client-nya."},
		"POST /users": {Body: map[string]any{"username": "staf.{{$timestamp}}", "email": "staf.{{$timestamp}}@example.test",
			"first_name": "Staf Operasional", "role_uuid": "{{roleUuid}}", "status": "active"},
			Req: dto.CreateUserRequest{}, Resp: dto.UserCredentialResponse{}, Script: saveActivation,
			Desc: "Membuat pengguna tanpa password dan mengirim tautan aktivasi ke email-nya (`/activate?key=…`, berlaku `ACTIVATION_KEY_TTL`). `activation_key` hanya ikut di respons di luar production. Non-ADMIN hanya dapat memberi role yang permission-nya ia miliki; role ADMIN hanya oleh ADMIN."},
		"GET /users/:uuid": {Resp: dto.UserResponse{}, Desc: "Detail pengguna. ETag respons tersimpan ke `{{etag}}` untuk Ubah/Hapus/aksi."},
		"PUT /users/:uuid": {IfMatch: true, Body: map[string]any{"email": "staf.ubah@example.test", "first_name": "Staf Operasional",
			"role_uuid": "{{roleUuid}}", "status": "active"}, Req: dto.UpdateUserRequest{}, Resp: dto.UserResponse{},
			Desc: "Mengubah pengguna (username tetap). Menonaktifkan pengguna mencabut semua sesinya. Tidak dapat mengubah role/menonaktifkan akun sendiri."},
		"DELETE /users/:uuid": {IfMatch: true, Desc: "Soft delete pengguna dan mencabut semua sesinya. Tidak dapat menghapus akun sendiri."},
		"POST /users/:uuid/reset-password": {Name: "Reset password", IfMatch: true, Resp: dto.UserCredentialResponse{}, Script: saveActivation,
			Desc: "Menghapus password, mengakhiri semua sesi, dan mengirim tautan atur ulang (`/reset-password?key=…`) ke email pengguna. `activation_key` hanya ikut di respons di luar production."},
		"POST /users/:uuid/lock":   {Name: "Kunci pengguna", IfMatch: true, Resp: dto.UserResponse{}, Desc: "Mengunci akun (tidak bisa login) sampai dibuka."},
		"POST /users/:uuid/unlock": {Name: "Buka kunci pengguna", IfMatch: true, Resp: dto.UserResponse{}, Desc: "Membuka kunci akun dan mereset hitungan gagal login."},

		// ------------------------------------------------------------ roles & permissions
		"GET /roles": {Resp: dto.RoleResponse{}, Query: listQuery(service.RoleSorts, "role_code", filter("q", "", "cari kode/nama"))},
		"POST /roles": {Body: map[string]any{"role_code": "OPS-{{$timestamp}}", "role_name": "Operasional", "description": "Staf operasional harian",
			"status": "active"}, Req: dto.RoleRequest{}, Resp: dto.RoleResponse{}, Script: saveUUID("roleUuid", ""),
			Desc: "Membuat role kosong. Isi permission-nya dengan **Atur permission role**."},
		"GET /roles/:uuid": {Resp: dto.RoleResponse{}, Desc: "Detail role; ETag tersimpan ke `{{etag}}`."},
		"PUT /roles/:uuid": {IfMatch: true, Body: map[string]any{"role_code": "OPS-UBAH", "role_name": "Operasional", "status": "active"},
			Req: dto.RoleRequest{}, Resp: dto.RoleResponse{},
			Desc: "Mengubah role. Role nonaktif tidak memberi permission apa pun (berlaku seketika). Kode/status ADMIN tidak dapat diubah."},
		"DELETE /roles/:uuid":      {IfMatch: true, Desc: "Soft delete role; ditolak 409 bila masih dipakai pengguna. ADMIN tidak dapat dihapus."},
		"GET /roles/:uuid/actions": {Name: "Permission role", Resp: dto.RoleActionsResponse{}, Desc: "Permission (action) yang dimiliki role."},
		"PUT /roles/:uuid/actions": {Name: "Atur permission role", IfMatch: true, Body: map[string]any{"action_uuids": []string{"{{actionUuid}}"}},
			Req: dto.RoleActionsRequest{}, Resp: dto.RoleActionsResponse{},
			Desc: "Mengganti **seluruh** set permission role dalam 1 transaksi (`If-Match` = ETag role dari **Detail role**). Ambil UUID dari **Katalog permission**. Non-ADMIN hanya dapat memberi/mencabut permission yang ia miliki dan hanya untuk role yang tidak melebihi dirinya."},
		"GET /actions": {Resp: dto.ActionResponse{}, Query: listQuery(service.ActionSorts, "action_code",
			filter("q", "", "cari kode/nama"), filter("module", "bond", "system, masterdata, market, portfolio, accounting, trading, bond, pension, custom"))},
		"POST /actions": {Body: map[string]any{"action_code": "dashboard_widget:view", "action_name": "Lihat widget dashboard",
			"resource_label": "widget dashboard"}, Req: dto.ActionRequest{}, Resp: dto.ActionResponse{}, Script: saveUUID("actionUuid", ""),
			Desc: "Membuat permission **kustom** (modul `custom`) untuk tombol/widget khusus frontend. Permission sistem berasal dari kode (`make seed`)."},
		"GET /actions/:uuid": {Resp: dto.ActionResponse{}},
		"PUT /actions/:uuid": {IfMatch: true, Body: map[string]any{"action_code": "dashboard_widget:view", "action_name": "Lihat widget dashboard"},
			Req: dto.ActionRequest{}, Resp: dto.ActionResponse{}, Desc: "Hanya permission kustom; permission sistem (`is_system`) ditolak 409."},
		"DELETE /actions/:uuid": {IfMatch: true, Desc: "Hanya permission kustom yang tidak dipakai role."},
		"GET /permission-catalog": {Name: "Katalog permission", Resp: dto.PermissionCatalogResponse{},
			Desc: "Seluruh permission terkelompok modul → resource → aksi (CRUD lebih dulu), untuk matriks checkbox editor role."},

		// ------------------------------------------------------------ system
		"GET /activity-logs": {Resp: dto.ActivityLogResponse{}, Query: listQuery(service.ActivityLogSorts, "-log_time",
			filter("username", "", "UUID pengguna"), filter("log_action", "app_user.create", "mis. bond_trade.approve"),
			filter("from", "2026-11-01T00:00:00+07:00", "RFC 3339"), filter("to", "2026-12-01T00:00:00+07:00", "RFC 3339")),
			Desc: "Jejak audit dari `pfm_log` (append-only). Event muncul setelah dikirim worker (±5 detik)."},
		"GET /activity-logs/:uuid": {Resp: dto.ActivityLogResponse{}},
		"POST /files/images": {Name: "Unggah gambar", Table: "file_object", Resp: dto.FileResponse{}, Script: saveUUID("fileUuid", ""),
			Form: []formField{{Name: "purpose", Value: "configuration.qr_logo"}, {Name: "file", Value: "fixtures/contoh.png", File: true}},
			Desc: "Multipart: `purpose` + `file`. Gambar (PNG/JPEG/WebP, maks 2 MB) disimpan di disk lokal dengan status `pending`, lalu dipakai lewat field `*_file_uuid`. Upload yang tidak dipakai dihapus worker setelah 24 jam."},
		"POST /files/documents": {Name: "Unggah dokumen", Table: "file_object", Resp: dto.FileResponse{}, Script: saveUUID("fileUuid", ""),
			Form: []formField{{Name: "purpose", Value: "cash_settlement.document"}, {Name: "file", Value: "fixtures/contoh.pdf", File: true}},
			Desc: "Multipart: `purpose` + `file`. Dokumen PDF (maks 20 MB) disimpan di S3. Purpose: `cash_settlement.document`, `payment_document.document`, `member_document.document`, `investor_coupon_schedule.payment_document`, `agent_fee_transaction.payment_document`, `agent_fee_transaction.tax_document`, `document.document`, `newsletter.document`."},
		"GET /files/:uuid":          {Name: "Metadata file", Table: "file_object", Resp: dto.FileResponse{}, Desc: "Hanya pengunggah atau pemegang `file_object:read`."},
		"GET /files/:uuid/content":  {Name: "Isi gambar", Table: "file_object", Desc: "Mengalirkan isi gambar (image lokal)."},
		"GET /files/:uuid/download": {Name: "URL unduh dokumen", Table: "file_object", Desc: "URL presigned S3 berumur pendek (default 5 menit); akses dicatat di log audit."},
		"DELETE /files/:uuid":       {Name: "Hapus file pending", Table: "file_object", Desc: "Hanya file `pending` (belum dipakai data lain)."},
		"GET /configuration":        {Name: "Lihat konfigurasi", Resp: dto.ConfigurationResponse{}, Desc: "Konfigurasi sistem (singleton); ETag tersimpan ke `{{etag}}`."},
		"PUT /configuration": {Name: "Ubah konfigurasi", IfMatch: true, Body: map[string]any{"company_name": "PT Contoh Dana Pensiun",
			"investor_number_template": "INV{year}{counter}", "is_using_qr_logo": false, "is_using_agreement": false},
			Req: dto.ConfigurationRequest{}, Resp: dto.ConfigurationResponse{}, Desc: "Counter penomoran bersifat read-only (diubah oleh transaksi)."},

		// ------------------------------------------------------------ portfolio
		"GET /placements/:uuid/details": {Name: "Detail penempatan", Resp: dto.PlacementDetailsResponse{}},
		"PUT /placements/:uuid/details": {Name: "Ganti detail penempatan", IfMatch: true, Req: dto.PlacementDetailsRequest{}, Resp: dto.PlacementDetailsResponse{},
			Body: map[string]any{"details": []any{map[string]any{"placement_detail_date": "2026-11-20", "placement_value": "1000000"}}},
			Desc: "Mengganti seluruh detail dalam 1 transaksi. Σ nilai detail ≤ nilai placement; tanggal di dalam periode placement."},
		"POST /deposito-portfolios/:uuid/rollover": {Name: "Rollover deposito", IfMatch: true, Body: map[string]any{"tenor": 3},
			Req: dto.DepositoRolloverRequest{}, Desc: "Membuat penempatan lanjutan (semua field opsional, default dari penempatan lama); yang lama menjadi riwayat."},
		"POST /deposito-portfolios/:uuid/withdraw": {Name: "Cairkan deposito", IfMatch: true, Body: map[string]any{"withdrawal_date": "2026-12-20"},
			Req: dto.DepositoWithdrawRequest{}},

		// ------------------------------------------------------------ accounting
		"GET /finance-transactions": {Resp: dto.JournalSummaryResponse{}, Query: listQuery(service.JournalSorts, "-transaction_date",
			filter("q", "", "cari nomor/deskripsi"), filter("portfolio_uuid", "{{portfolioUuid}}", "filter portofolio"),
			filter("from", "2026-11-01", "YYYY-MM-DD"), filter("to", "2026-11-30", "YYYY-MM-DD"))},
		"POST /finance-transactions": {Name: "Posting jurnal manual", Req: dto.JournalRequest{}, Resp: dto.JournalResponse{},
			Body: map[string]any{"transaction_date": "2026-11-20", "description": "Penyesuaian", "lines": []any{
				map[string]any{"finance_account_uuid": "{{financeAccountUuid}}", "debit": "100000"},
				map[string]any{"finance_account_uuid": "{{counterAccountUuid}}", "credit": "100000"}}},
			Script: saveUUID("financeTransactionUuid", ""),
			Desc:   "Langsung posted. Σdebit = Σkredit, minimal 2 baris, tiap baris hanya debit atau kredit. Nomor `JV-YYYY-NNNNNN` tanpa celah. Isi `counterAccountUuid` dengan akun lawan."},
		"GET /finance-transactions/:uuid": {Name: "Detail jurnal", Resp: dto.JournalResponse{}},
		"POST /finance-transactions/:uuid/reverse": {Name: "Balik jurnal", IfMatch: true, Resp: dto.JournalResponse{},
			Desc: "Membuat jurnal pembalik. Jurnal dari trade/settlement hanya dapat dibalik lewat dokumen sumbernya."},
		"POST /cash-settlements/:uuid/approve": {Name: "Setujui cash settlement", IfMatch: true,
			Desc: "Maker-checker: pembuat tidak boleh menyetujui. Memposting jurnal (termasuk fee) dalam 1 transaksi."},
		"POST /cash-settlements/:uuid/cancel":  {Name: "Batalkan cash settlement", IfMatch: true},
		"POST /cash-settlements/:uuid/reverse": {Name: "Balik cash settlement", IfMatch: true, Desc: "Membalik jurnal settlement yang sudah disetujui."},

		// ------------------------------------------------------------ bond
		"POST /bonds/:uuid/offer":    {Name: "Buka offering", Order: 2, IfMatch: true, Desc: "Draft → offering; subscription dapat dibuat."},
		"POST /bonds/:uuid/activate": {Name: "Aktifkan obligasi", Order: 3, IfMatch: true, Desc: "Draft/offering → active; butuh jadwal kupon. Kupon, periode, tanggal terbit & jatuh tempo terkunci."},
		"POST /bonds/:uuid/generate-coupon-schedule": {Name: "Buat jadwal kupon", Order: 1, IfMatch: true,
			Desc: "Kupon tiap `coupon_payment_periode` hari sejak terbit (tarif = kupon × hari / 360) + pokok 100% saat jatuh tempo. Hanya sebelum ada subscription disetujui."},
		"POST /bond-trades/:uuid/approve": {Name: "Setujui subscription", IfMatch: true, Body: note, Req: dto.DecisionRequest{},
			Desc: "Maker-checker. Menerbitkan nomor sertifikat (counter per obligasi), jadwal kupon investor dan total obligasi dalam 1 transaksi."},
		"POST /bond-trades/:uuid/cancel": {Name: "Batalkan subscription", IfMatch: true, Body: note, Req: dto.DecisionRequest{}},
		"POST /bond-coupon-schedules/:uuid/pay": {Name: "Bayar kupon (semua investor)", IfMatch: true,
			Body: map[string]any{"settlement_date": "2026-11-20"}, Req: dto.CouponPaymentRequest{}},
		"POST /investor-coupon-schedules/:uuid/pay": {Name: "Bayar kupon investor", IfMatch: true, Req: dto.InvestorCouponPaymentRequest{},
			Body: map[string]any{"payment_settlement_date": "2026-11-20", "payment_reference_number": "TRF-0001", "payment_document_file_uuid": "{{fileUuid}}"},
			Desc: "Bukti transfer PDF wajib: unggah dulu dengan purpose `investor_coupon_schedule.payment_document`."},
		"POST /agent-fee-transactions/:uuid/pay": {Name: "Bayar fee agen", IfMatch: true, Req: dto.AgentFeePaymentRequest{},
			Body: map[string]any{"payment_date": "2026-11-20", "paid_value": "500000", "tax_paid_value": "10000"},
			Desc: "Bukti bayar harus sudah dilampirkan (field `payment_document_file_uuid` lewat Ubah)."},
		"POST /investors/:uuid/change-requests": {Name: "Ajukan perubahan data investor", Req: dto.InvestorRequest{},
			Body: map[string]any{"investor_name": "Nama Investor Baru", "mobile_phone": "081234567890"}, Script: saveUUID("investorDataHistoryUuid", ""),
			Desc: "Penggantian penuh data pribadi investor, diterapkan setelah disetujui pengguna lain. Hanya satu pengajuan pending per investor."},
		"POST /marketing-agents/:uuid/change-requests": {Name: "Ajukan perubahan data agen", Req: dto.MarketingAgentChangeRequest{},
			Body: map[string]any{"marketing_agent_name": "Nama Agen Baru"}, Script: saveUUID("marketingAgentDataHistoryUuid", "")},
		"POST /investor-data-histories/:uuid/approve": {Name: "Setujui perubahan investor", Body: note, Req: dto.DecisionRequest{},
			Desc: "Maker-checker; menerapkan perubahan + mencatat approver & IP. Ditolak bila data investor diubah langsung setelah pengajuan."},
		"POST /investor-data-histories/:uuid/reject":         {Name: "Tolak perubahan investor", Body: note, Req: dto.DecisionRequest{}},
		"POST /marketing-agent-data-histories/:uuid/approve": {Name: "Setujui perubahan agen", Body: note, Req: dto.DecisionRequest{}},
		"POST /marketing-agent-data-histories/:uuid/reject":  {Name: "Tolak perubahan agen", Body: note, Req: dto.DecisionRequest{}},

		// ------------------------------------------------------------ pension
		"POST /invoices/generate": {Name: "Generate invoice bulanan", Req: dto.InvoiceGenerateRequest{},
			Body: map[string]any{"client_uuid": "{{clientUuid}}", "year": 2026, "month": 11}, Script: saveUUID("invoiceUuid", ""),
			Desc: "Idempoten: 201 bila invoice baru, 200 bila sudah ada (draft dilengkapi peserta baru). Satu baris iuran per peserta aktif client."},
		"POST /invoices/:uuid/issue":  {Name: "Terbitkan invoice", IfMatch: true, Desc: "Draft → issued; baris iuran terkunci dan dapat dibayar."},
		"POST /invoices/:uuid/cancel": {Name: "Batalkan invoice", IfMatch: true, Body: note, Req: dto.DecisionRequest{}, Desc: "Hanya bila belum ada iuran dibayar."},
		"POST /member-premium-payments/:uuid/pay": {Name: "Bayar iuran", IfMatch: true, Req: dto.PremiumPaymentRequest{},
			Body: map[string]any{"payment_date": "2026-11-20"}, Desc: "`paid_nominal` default = nominal tagihan. Invoice otomatis `paid` saat baris terakhir dibayar."},
		"POST /benefit-payout-batches/generate": {Name: "Generate batch payout", Req: dto.PeriodRequest{},
			Body: map[string]any{"year": 2026, "month": 11}, Script: saveUUID("benefitPayoutBatchUuid", ""),
			Desc: "Idempoten per bulan; satu baris per peserta pensiun dan tanggungan dengan `pension_benefit_value`."},
		"POST /benefit-payout-batches/:uuid/approve": {Name: "Setujui batch payout", IfMatch: true, Body: note, Req: dto.DecisionRequest{}, Desc: "Maker-checker."},
		"POST /benefit-payout-batches/:uuid/pay": {Name: "Bayar batch payout", IfMatch: true, Req: dto.PayoutPaymentRequest{},
			Body: map[string]any{"payout_date": "2026-11-25"}},
		"POST /benefit-payout-batches/:uuid/cancel": {Name: "Batalkan batch payout", IfMatch: true, Body: note, Req: dto.DecisionRequest{}},
		"POST /member-portfolio-transactions/:uuid/settle": {Name: "Settle transaksi unit", IfMatch: true,
			Desc: "BUY menambah unit & biaya perolehan; SELL mengurangi unit pada biaya rata-rata. Wajib urut tanggal."},
		"POST /member-portfolio-transactions/:uuid/cancel": {Name: "Batalkan transaksi unit", IfMatch: true},
	}
	for kind, label := range map[string]string{"stock": "transaksi saham", "mutual_fund": "transaksi reksa dana", "crypto": "transaksi kripto"} {
		for k, v := range trade(kind, label) {
			m[k] = v
		}
	}
	return m
}()
