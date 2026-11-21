# PFM Backend

REST API Portfolio & Fund Management: Go + Fiber v3 + PostgreSQL. Spesifikasi lengkap ada di [prd.md](prd.md).

Status: **M0–M5 selesai** (fondasi, master data, instrumen & portofolio, trading & akuntansi, obligasi, dana pensiun). M6–M7 belum.

## Prasyarat

- Go 1.26 (toolchain `go1.26.11` diunduh otomatis lewat `go.mod`)
- PostgreSQL ≥ 14 dan S3/MinIO — atau Docker untuk `make dev-up`
- `sqlc` 1.29, `golangci-lint` v2, `govulncheck`, `openssl`

## Menjalankan secara lokal

```bash
cp .env.example .env          # isi ADMIN_PASSWORD (min 12 karakter)
make keys                     # kunci JWT Ed25519 -> secrets/jwt_ed25519.pem
make dev-up                   # PostgreSQL 17 (4 database + role, deploy/postgres/roles.sql) + MinIO + Mailpit
make migrate-up               # migrasi ke pfm_core, pfm_security, pfm_log, pfm_document (pfm_migrator)
make db-grants                # hak akses minimum per database
make seed                     # katalog permission & 7 role (security) + konfigurasi (core)
make seed-admin               # admin pertama dari ADMIN_USERNAME/EMAIL/PASSWORD
make run                      # API (HTTP_ADDR di .env, default :8080); `go run ./cmd/api` juga membaca .env
make dev                      # API dengan live reload (air) + worker (email & log audit); Ctrl+C menghentikan keduanya
make worker                   # (terminal lain, user DB worker) kirim audit ke pfm_log + pembersihan
```

```bash
curl -s localhost:8080/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"login":"admin","password":"<ADMIN_PASSWORD>"}'
```

## Perintah

| Perintah | Fungsi |
|---|---|
| `make test` | Unit test (integration dilewati tanpa `TEST_DATABASE_URL`) |
| `make test-integration` | Semua test termasuk DB: e2e HTTP, rollback transaksi, migrasi up→down→up |
| `make lint` / `make vuln` | golangci-lint (+gosec) / govulncheck |
| `make sqlc` | Generate ulang `internal/db/sqlc` setelah mengubah `internal/db/queries/<database>/*.sql` |
| `make migration db=core name=alter_x` | Migrasi baru di folder database-nya; nomor global berurutan (jangan edit migrasi yang sudah ter-apply) |
| `make worker` | Kirim audit outbox ke `pfm_log` dan email antrean lewat SMTP (±5 detik), bersihkan upload pending > 24 jam dan refresh token kedaluwarsa |
| `make schema-db` | Siapkan database gabungan (`migrate -db all`) untuk generator |
| `make bruno` | Generate koleksi Bruno (dokumentasi + contoh request semua endpoint) ke `../bruno-pfm` |
| `make scaffold module=<nama>` | Generate query sqlc + adapter CRUD dari spec di `cmd/scaffold/specs_*.go` |

## Struktur

Layer datar: satu package per layer, satu file per entitas.

```
cmd/api | cmd/migrate | cmd/worker | cmd/scaffold & cmd/apidoc (generator, dev)
internal/
  model/        struct domain (User, Role, Credential, …)
  dto/          request/response + mapper           (<tabel>_gen.go = hasil generator)
  repository/   interface + implementasi sqlc        (<tabel>_gen.go = hasil generator)
  service/      logika bisnis, transaksi, audit      (<tabel>_gen.go = definisi resource CRUD)
  handler/      HTTP handler per entitas + route
  permission/   kode permission (tabel action)
  router/       middleware + wiring repository -> service -> handler
  db/           migrations/<shared|outbox|core|security|log|document>/ (goose),
                queries/<core|security|log|document|outbox>/ (sqlc), sqlc/ & schema/ (generated)
  platform/     database (TxManager, lookup), crud (service+handler generik), httpx, auth,
                security, storage, audit, validatorx, logger
  apperr/ config/ testutil/
deploy/         Dockerfile (distroless), docker-compose (dev), SQL role & grants per database
```

## Empat database

Data dipisah agar kebocoran satu kredensial, dump, atau celah SQL injection tidak membuka data lain (PRD §7.1, §12.5).

| Database | Isi | User API | Catatan |
|---|---|---|---|
| `pfm_core` | Seluruh data bisnis | `pfm_core_app` | Tanpa DELETE (soft delete) |
| `pfm_security` | `app_user`, `role`, `action`, `role_action`, `auth_refresh_token` | `pfm_security_app` | Hash password & token terpisah dari data bisnis |
| `pfm_log` | `activity_log` | `pfm_log_reader` (baca saja) | Worker menulis dengan `pfm_log_writer` (INSERT saja); tidak ada yang bisa mengubah/menghapus log |
| `pfm_document` | `file_object` + modul dokumen | `pfm_document_app` | Metadata & key file S3/lokal |

- Tidak ada FK atau transaksi lintas database. Referensi lintas database memakai kolom `*_uuid` (mis. `app_user.client_uuid`, `cash_settlement.document_file_uuid`) yang dijaga aplikasi: divalidasi saat menulis, ditolak saat dihapus bila masih dirujuk, dan fail-closed bila referensi hilang.
- Audit ditulis ke `audit_outbox` di database asal, dalam transaksi bisnis yang sama. `make worker` memindahkannya ke `pfm_log`, sehingga `/activity-logs` baru terisi setelah worker berjalan.
- Satu `TxManager` per database (`internal/platform/database`), dan setiap database punya DSN sendiri (`DATABASE_URL_CORE`, `_SECURITY`, `_LOG`, `_DOCUMENT`).

## Endpoint M0

| Endpoint | Keterangan |
|---|---|
| `POST /api/v1/auth/login`, `/refresh`, `/logout`, `/activate` | Publik, rate limit 10/menit/IP |
| `POST /api/v1/auth/logout-all`, `/change-password`, `GET /api/v1/me` | Butuh token |
| `/api/v1/users` (+ `/reset-password`, `/lock`, `/unlock`) | CRUD, `If-Match` wajib untuk PUT/DELETE/aksi |
| `/api/v1/roles` (+ `GET/PUT /{uuid}/actions`) | CRUD + set permission per role |
| `/api/v1/actions`, `/api/v1/activity-logs` | Katalog permission, audit trail |
| `POST /api/v1/files/images`, `/files/documents`; `GET /files/{uuid}`, `/content`, `/download` | Upload 2 fase, download dokumen via presigned URL |
| `GET /healthz`, `GET /readyz` | Liveness / readiness (DB, disk image, S3) |

## Endpoint M1 (Master Data)

24 resource CRUD dengan pola yang sama (`GET` list/detail, `POST`, `PUT` + `If-Match`, `DELETE` + `If-Match`):
`/asset-classes`, `/bank-classes`, `/banks`, `/bond-periode-options`, `/bond-types`, `/countries`, `/provinces`,
`/cities`, `/document-categories`, `/finance-account-types`, `/investor-categories`, `/mutual-fund-categories`,
`/occupations`, `/placement-types`, `/premium-payment-types`, `/property-asset-types`, `/rating-agencies`,
`/ratings`, `/sectors`, `/sub-sectors`, `/stock-size-classifications`, `/stock-types`, `/tax-rates`, `/trade-types`.

- Filter: `?q=` (nama/kode), `?<relasi>_uuid=`, `?sort=` (kode, nama, created_at; prefiks `-` untuk menurun).
- Nested: `/countries/{uuid}/provinces`, `/provinces/{uuid}/cities`, `/bank-classes/{uuid}/banks`,
  `/rating-agencies/{uuid}/ratings`, `/sectors/{uuid}/sub-sectors`.
- Hapus ditolak (409) bila data masih dipakai baris aktif lain; `asset_class`/`sector` menolak induk yang membentuk siklus.
- `GET/PUT /configuration`: singleton; counter penomoran read-only; `proxy_api_url` wajib https; logo QR via upload `configuration.qr_logo`.

## Endpoint M2 (Instrumen Pasar & Portofolio)

- **CRUD market:** `/stocks`, `/indices`, `/emitents`, `/brokers` (+ `/brokers/{uuid}/broker-api-settings`),
  `/investment-managers`, `/mutual-funds`, `/depositos`, `/cryptos`, `/crypto-exchanges`, `/crypto-listing-exchanges`,
  `/stock-type-details`, `/indices-stocks`.
- **Time-series** (insert/ubah/hapus memperbarui `latest_*` induk dalam transaksi yang sama; `POST …/import` menerima
  CSV `text/csv`, header = nama field JSON, maks 10.000 baris, semua-atau-tidak-sama-sekali):
  `/stock-histories`, `/stock-beta-histories`, `/stock-fundamental-histories`, `/indices-histories`,
  `/mutual-fund-histories`; serta `/stock-size-classification-histories`, `/deposito-interest-histories`.
- **CRUD portofolio** (scope per client: user milik client hanya melihat/mengubah data client-nya):
  `/clients`, `/portfolios`, `/portfolio-banks`, `/portfolio-budgets`, `/portfolio-cost-structures`,
  `/portfolio-accounting-structures`, `/portfolio-notes`, `/asset-class-finance-accounts`, `/deposito-portfolios`,
  `/property-assets`, `/crypto-trade-plans`, `/placements`.
- **Read-only:** posisi `/stock-portfolios`, `/bond-portfolios`, `/mutual-fund-portfolios`, `/crypto-portfolios`;
  riwayat `/…-portfolio-histories`, `/portfolio-budget-histories`, `/deposito-histories`.
- **Transaksi:** `GET/PUT /placements/{uuid}/details` (ganti seluruh detail dalam 1 transaksi),
  `POST /deposito-portfolios/{uuid}/rollover` dan `/withdraw` (`If-Match` wajib).
- **Setup portofolio** `POST /portfolios/setup`: portofolio + mapping akun per kelas aset + akun kas per jenis transaksi
  + rekening bank dalam 1 transaksi (semua atau tidak sama sekali), dengan aturan, scope, hook dan audit yang sama seperti
  endpoint CRUD masing-masing. Butuh `portfolio:create` dan izin create untuk tiap bagian yang diisi.
- **Valuasi untuk UI** (permission `portfolio:read`, scope sama dengan `/portfolios`):
  `GET /portfolio-overviews` (parameter sama dengan `GET /portfolios` + `valuation` dan `allocation` per baris),
  `GET /portfolios/{uuid}/summary` (data portofolio, nilai perolehan, nilai pasar, laba/rugi belum terealisasi &
  realisasi, alokasi per kelas aset dengan bobot %), `GET /portfolios/{uuid}/holdings?asset_class=` (semua posisi
  aktif dengan harga terakhir dan `position_path`). Dihitung langsung dari view `portfolio_holding`: harga saham =
  penutupan terakhir, reksa dana = NAV terakhir, obligasi = `bond_price` (% nominal), kripto = harga trade settled
  terakhir; tanpa harga dipakai harga trade terakhir lalu harga rata-rata. Deposito aktif dinilai pada pokok.
  Kolom tersimpan (`portfolio.unrealized_profit_loss`, `market_value` posisi) diperbarui otomatis saat harga
  (riwayat harga/NAV, harga obligasi) berubah dan saat trade di-settle/dibalik.

## Endpoint M3 (Trading & Akuntansi)

- **Trade** `/stock-trades`, `/mutual-fund-trades`, `/crypto-trades`: CRUD untuk draft (nilai, unit, dan fee dihitung
  server), lalu `POST /{uuid}/settle`, `/cancel`, `/reverse` (`If-Match` wajib). Settle memperbarui posisi
  (metode rata-rata, fee masuk biaya perolehan), snapshot riwayat, laba/rugi realisasi portofolio, dan memposting jurnal
  dalam satu transaksi. Settle harus berurutan per tanggal; reverse hanya untuk trade ter-settle terakhir pada posisinya.
- **Mapping akun** per portofolio: `asset-class-finance-accounts` (akun investasi, laba, rugi per kelas aset
  `STOCK`/`MF-ASSET`/`CRYPTO`) dan `portfolio-accounting-structures` (akun kas per kelas aset × `BUY`/`SELL`).
- **Jurnal** `/finance-transactions`: `POST` jurnal manual (langsung posted, Σdebit = Σkredit, nomor `JV-YYYY-NNNNNN`
  tanpa celah), `GET` list/detail beserta baris, `POST /{uuid}/reverse`. Jurnal dari trade/settlement hanya dibalik
  lewat dokumen sumbernya.
- **Akun & settlement** `/finance-accounts` (tree), `/cash-settlement-categories`, `/cash-settlements` (draft + lampiran
  dokumen), lalu `/approve` (pembuat tidak boleh menyetujui), `/cancel`, `/reverse`.

## Endpoint M4 (Obligasi)

- **Obligasi** `/bonds` (scope per client; status `draft` → `offering` → `active` → `matured`):
  `POST /{uuid}/generate-coupon-schedule` (kupon per `coupon_payment_periode` hari, tarif = kupon × hari / 360, pokok 100%
  saat jatuh tempo; hanya sebelum ada subscription disetujui), `/offer`, `/activate` (butuh jadwal kupon). Kupon, periode,
  tanggal terbit dan jatuh tempo terkunci setelah aktif. Turunan: `/bond-ratings` (rating terbaru → `bond.rating_id`),
  `/bond-histories` (snapshot harga/rating), `/bond-coupon-schedules`, `/bond-periode-distributions`, `/payment-documents`.
- **Investor & agen** `/investors`, `/marketing-agents` (nomor dari template + counter di `configuration`),
  `/bank-accounts` (milik tepat satu investor **atau** agen). Data pribadi (NIK, NPWP, rekening, HP, tanggal lahir)
  di-mask kecuali punya `pii:read`; akses tanpa mask dicatat di audit.
- **Subscription** `/bond-trades`: draft hanya untuk obligasi `offering`/`active`, nominal ≥ denominasi minimum, rekening
  harus milik investor; setiap edit draft tersimpan di `/bond-trade-data-histories`. `POST /{uuid}/approve`
  (maker-checker; nomor sertifikat dari counter per obligasi yang dikunci baris, template `bond_periode_option` →
  `configuration` → `{bondCode}-{series}{counter}`; membuat `/investor-coupon-schedules` dan memperbarui total obligasi
  dalam satu transaksi) dan `/cancel`.
- **Pembayaran:** `POST /bond-coupon-schedules/{uuid}/pay` (semua kupon investor yang belum dibayar),
  `POST /investor-coupon-schedules/{uuid}/pay` (satu investor, bukti transfer PDF wajib),
  `POST /agent-fee-transactions/{uuid}/pay` (bukti bayar harus sudah dilampirkan). Semua butuh `If-Match`.
- **Perubahan data (maker-checker):** `POST /investors/{uuid}/change-requests` dan
  `/marketing-agents/{uuid}/change-requests` (satu pengajuan pending per data), lalu
  `POST /investor-data-histories/{uuid}/approve|reject` dan `/marketing-agent-data-histories/{uuid}/approve|reject`.
  Approve menerapkan perubahan + mencatat approver & IP; ditolak bila data sudah diubah langsung setelah pengajuan.
  Berikan `investor:update`/`marketing_agent:update` hanya ke ADMIN agar perubahan lain lewat jalur ini.

## Endpoint M5 (Dana Pensiun)

- **Peserta** `/members` (milik satu client, nomor `MBR000001` dst.; status 0 nonaktif, 1 aktif, 2 pensiun; data pribadi
  di-mask), `/dependants`, `/tax-reports`, `/member-documents` (PDF ke S3), `/member-groups`.
- **Tagihan iuran** `/invoices`: `POST /invoices/generate` `{client_uuid, year, month}` membuat invoice bulanan client
  (nomor `NN/DP/MM/YYYY`, tanggal 1, jatuh tempo tanggal 10) dengan satu baris `/member-premium-payments` per peserta aktif
  (`latest_pension_premium`). Idempoten: panggilan ulang mengembalikan invoice yang sama (200) dan hanya melengkapi
  draft dengan peserta baru. Lalu `/issue` (baris terkunci), `/cancel` (bila belum ada yang dibayar; periode bisa
  di-generate ulang). `POST /member-premium-payments/{uuid}/pay` mencatat pembayaran; invoice otomatis `paid` saat
  baris terakhir dibayar. Total dan terbilang (`billing_description`) dihitung server.
- **Pembayaran manfaat** `/benefit-payout-batches`: `POST /generate` `{year, month}` (idempoten; satu baris per peserta
  pensiun dan per tanggungan dengan `pension_benefit_value`), edit pajak per baris saat draft, `/approve`
  (maker-checker), `/pay`, `/cancel`.
- **Unit peserta** `/member-portfolios` (rekening unit per peserta × portofolio client yang sama) dan
  `/member-portfolio-transactions` (draft BUY = subscribe berdasarkan nilai, SELL = redeem berdasarkan unit, pada NAV
  efektif), lalu `/settle` (saldo unit, biaya perolehan rata-rata, riwayat `/member-portfolio-histories`; urut tanggal)
  dan `/cancel`.

## Email akun (aktivasi & reset password)

| Kejadian | Email ke user | Tautan frontend |
|---|---|---|
| Admin membuat user (`POST /users`) | Aktivasi akun | `<FRONTEND_BASE_URL>/activate?key=…` (berlaku `ACTIVATION_KEY_TTL`) |
| Admin mereset (`POST /users/{uuid}/reset-password`) | Password direset; password lama dihapus dan sesi diakhiri | `<FRONTEND_BASE_URL>/reset-password?key=…` |
| User lupa password (`POST /auth/forgot-password` `{email}`) | Atur ulang password; password lama tetap berlaku sampai tautan dipakai | `<FRONTEND_BASE_URL>/reset-password?key=…` (berlaku `PASSWORD_RESET_TTL`) |

- Kedua halaman frontend mengirim `key` dari URL ke `POST /api/v1/auth/activate` `{activation_key, new_password}`. Tautan sekali pakai, dan semua sesi lama dicabut.
- `forgot-password` selalu menjawab 204, sehingga email terdaftar tidak bisa ditebak. Maksimal 1 email per menit per akun selama tautan belum dipakai.
- `activation_key` **tidak** dikembalikan API di production; di development tetap ada di respons untuk mempermudah uji.
- API hanya mengantrekan email ke `mail_outbox` dalam transaksi yang sama. **Worker** yang mengirim lewat SMTP, dengan percobaan ulang bertahap hingga 8 kali; setelah gagal terakhir, isi email (berisi tautan rahasia) dihapus. Kredensial SMTP hanya dibutuhkan worker.
- **Email hanya terkirim bila worker berjalan.** `make dev` menjalankan API dan worker bersama; `make run` hanya API, jadi worker perlu dijalankan terpisah dengan `make worker`. Worker memakai `WORKER_DATABASE_URL_*` bila diisi (user `pfm_worker`/`pfm_log_writer`), selain itu `DATABASE_URL_*`.
- **Mailtrap:** isi `SMTP_HOST=sandbox.smtp.mailtrap.io`, `SMTP_PORT=587` (atau 2525), `SMTP_TLS=starttls`, serta username dan password dari *Email Testing → Inbox → SMTP Settings*. Email sandbox tertangkap di inbox Mailtrap, tidak sampai ke alamat aslinya. Pengirim diatur dengan `SMTP_FROM` (alamat) dan `SMTP_FROM_NAME` (nama).
- **Mailpit (lokal):** `make dev-up`, lalu `SMTP_HOST=localhost`, `SMTP_PORT=1025`, `SMTP_TLS=none`. Kotak masuk ada di http://localhost:8025.

## Otorisasi (RBAC) untuk frontend

Permission berformat `<resource>:<verb>` (mis. `bond:read`, `bond_trade:approve`). Backend memeriksa setiap request; frontend memakai daftar yang sama untuk menampilkan halaman dan tombol (PRD §12.2).

1. Setelah login, panggil `GET /api/v1/me`. Hasilnya: `user`, `role` (`uuid`, `code`, `name`, `menu_url`, `active`), `is_admin`, `permissions[]` (ADMIN: seluruh katalog), dan `permissions_version`.
2. **Halaman/menu** tampil bila user memegang `<resource>:read`. **Tombol** tampil bila user memegang kodenya, mis. `bond:create` atau `invoice:generate`.
3. Setiap respons terautentikasi membawa header `X-Permissions-Version`. Bila nilainya berbeda dari `permissions_version` yang disimpan (role diubah, user dipindah role, katalog berubah), muat ulang `/me`.
4. 403 menyebut permission yang kurang: `error.details[{field: "permission", rule: "bond:create"}]`.
5. **Editor role**: `GET /api/v1/permission-catalog` (modul → resource → aksi, untuk matriks checkbox), `GET`/`PUT /api/v1/roles/{uuid}/actions` (ganti seluruh set, `If-Match`). Role nonaktif tidak memberi permission apa pun.
6. **Permission kustom** untuk UI saja (mis. `dashboard_widget:view`): `POST /api/v1/actions`, masuk modul `custom`. Permission sistem (`is_system`) read-only; nama dan labelnya diatur kode dan disinkronkan `make seed`.

**Anti-eskalasi.** Pengguna non-ADMIN hanya dapat memberi atau mencabut permission yang ia miliki, dan hanya dapat mengelola role/user yang tidak melebihi dirinya. Role dan user ADMIN hanya dapat dikelola ADMIN.

## Menambah resource CRUD

1. Tambah spec di `cmd/scaffold/specs_*.go`: kolom wajib/read-only, snapshot, `Check`/`AfterWrite` hook, nested,
   `Kind: "read"`, `Import`, `Scope` (ekspresi SQL ke `client_id`).
2. `make scaffold module=<module>` (butuh DB ter-migrate di `SCAFFOLD_DATABASE_URL`): menulis
   `dto/`, `repository/`, `service/<tabel>_gen.go` dan `service/resources_<module>_gen.go`.
3. Daftarkan sekali di `crudResources()` pada `router/router.go`.
4. Aturan bisnis ditulis di file manual `service/<module>_hooks.go`, bukan di `*_gen.go`.

## Catatan keamanan

- Password legacy (SHA-256 tanpa salt) tidak dimigrasi; user lama wajib aktivasi ulang.
- Empat database dengan user terpisah (lihat di atas). Jalankan worker dengan user `pfm_worker`/`pfm_log_writer`, bukan user API.
- Dump legacy berisi data pribadi dan hash password: jangan simpan di repo. `secrets/` dan `.env` masuk `.gitignore`.
- Rate limiter memakai memori proses; untuk lebih dari satu instance, pindahkan storage limiter ke Redis (PRD §5.1).
- Unggahan PDF dicek secara heuristik (tanpa dekompresi stream); pasang ClamAV sebelum production (PRD §8.4).
