# PRD — PFM Backend (Portfolio & Fund Management API)

| Item | Nilai |
|---|---|
| Versi dokumen | 1.0 (draft) |
| Tanggal | 13 November 2026 |
| Status | Draft — menunggu review |
| Sumber skema | `database` (dump MySQL/MariaDB 10.3, database `revio`, 120 tabel) |
| Lampiran skema | [`prd-schema-postgres.sql`](prd-schema-postgres.sql) — DDL PostgreSQL lengkap, 123 migrasi goose |

---

## 1. Ringkasan

PFM Backend adalah REST API baru untuk sistem manajemen portofolio dan dana yang saat ini berjalan di atas database MySQL `revio`. Sistem mencakup:

- pengelolaan instrumen pasar (saham, obligasi, reksa dana, deposito, kripto)
- portofolio dan trading
- penerbitan obligasi beserta investor dan agen pemasarnya
- dana pensiun (DPLK)
- akuntansi (jurnal)
- manajemen dokumen, task, dan notifikasi

Keputusan utama:

| Area | Keputusan |
|---|---|
| Bahasa & framework | Go 1.25+, Fiber v3 |
| Database | PostgreSQL ≥ 14 (target produksi 16/17), driver `pgx/v5` |
| Akses data | Repository pattern di atas kode hasil `sqlc`, transaksi lewat `TxManager` |
| Migrasi | `pressly/goose` v3, **satu file per tabel**, nomor berurutan `00001`–`00123` sesuai dependensi FK |
| Identitas | `id BIGINT` (internal, untuk join) + `uuid UUID` (publik, satu-satunya ID yang keluar di API) di **setiap tabel** |
| Validasi | `go-playground/validator/v10` + validator kustom + validasi bisnis di service |
| File | Image → filesystem lokal; dokumen (PDF, dll.) → S3 (private bucket, presigned URL) |
| Keamanan | JWT akses berumur pendek + refresh token yang dirotasi, RBAC dari `role`/`action`/`role_action`, scope tenant per `client`, argon2id, audit log |

Skema PostgreSQL di lampiran **sudah diuji**: seluruh 123 migrasi dijalankan `up → down → up` tanpa error di PostgreSQL 14. Hasilnya 122 tabel, 238 foreign key, kolom `uuid` di semua tabel, dan setiap FK punya index.

---

## 2. Latar Belakang & Sumber Data

Hasil analisis file `database`:

| Temuan | Detail |
|---|---|
| Engine | MariaDB 10.3, `ENGINE=InnoDB`, `CHARSET=latin1` |
| Jumlah tabel | 120 |
| Tabel berisi data | 39 tabel (`INSERT`), contoh: `bond`, `bond_trade`, `investor`, `document`, `stock`, `role`, `user` |
| Foreign key | 260 constraint, sekitar setengahnya duplikat (FK yang sama didefinisikan dua kali) |
| Kolom audit ganda | 52 tabel memiliki `created_by`/`created_time`/… **dan** `createdBy`/`createdTime`/… (sisa mapping Hibernate) |
| Uang | Disimpan sebagai `double` (rawan pembulatan) |
| Soft delete | `deleted_status int` + `deleted_time` |
| File | Disimpan sebagai path lokal server lama, mis. `/usr/local/document/revio/subscription/2019-04-15/…` |
| Secret di DB | `user.password`, `user.token`, `user.activation_key`, `configuration.proxy_api_password`, `configuration.proxy_current_token` |
| Reserved word | Tabel `user` (reserved di PostgreSQL) |

Peran yang sudah ada di data (`role`): `ADMIN`, `FM`, `MEMBER`, `AGENT`, `CLIENT-MNG`, `CLIENT-ADMIN`, `CSPV`.

---

## 3. Tujuan, Non-Tujuan & Metrik Keberhasilan

### 3.1 Tujuan

1. Menyediakan REST API yang bersih (clean architecture), aman, dan teruji untuk **seluruh 120 tabel legacy** plus 2 tabel pendukung baru (`file_object`, `auth_refresh_token`).
2. Memindahkan skema dari MySQL ke PostgreSQL dengan tipe data yang benar (NUMERIC untuk uang, TIMESTAMPTZ, BOOLEAN, INET, JSONB), UUID di setiap tabel, dan FK yang lengkap.
3. Menjadikan migrasi skema dapat direproduksi: satu tabel per file goose, berurutan sesuai dependensi, dan setiap file punya `Down`.
4. Menjaga operasi multi-tabel tetap konsisten lewat transaksi database. Contoh: settle trade → update posisi → snapshot history → jurnal, semuanya atomik.
5. Memisahkan penyimpanan file: image di storage lokal, dokumen di S3.

### 3.2 Non-Tujuan (v1)

- Frontend / UI.
- Cetak sertifikat dan perjanjian (template Jasper `certificate_print_template`, `agreement_print_template`). Kolomnya tetap dimigrasi, tetapi rendering-nya masuk fase berikutnya.
- Integrasi feed harga pasar real-time. Data harga masuk lewat input atau import CSV.
- Pemilihan penyedia email/SMS. v1 cukup menyediakan interface `Notifier`.

### 3.3 Metrik Keberhasilan

| Metrik | Target |
|---|---|
| Migrasi skema | 123 migrasi lulus `up → down-to 0 → up` di CI |
| Cakupan API | 100% tabel memiliki endpoint sesuai matriks CRUD (§10.5) |
| Validasi | 100% request body melewati validasi DTO; 0 endpoint menerima field tak dikenal |
| Keamanan | 0 temuan High/Critical dari `gosec`, `govulncheck`, dan pentest sebelum go-live |
| Otorisasi | Test matriks authz (endpoint × role × tenant) lulus 100% |
| Rekonsiliasi ETL | Jumlah baris per tabel sama (kecuali dedup yang terdokumentasi); Σ nilai finansial per tabel sama |
| Performa | p95 < 300 ms untuk GET detail/list (≤ 100 item) pada 100 RPS |
| Test | Coverage layer service ≥ 80% |

---

## 4. Pengguna & Peran

| Role | Kode | Cakupan default (diusulkan, final di `role_action`) | Scope data |
|---|---|---|---|
| Super Administrator | `ADMIN` | Semua modul, IAM, konfigurasi, `include_deleted` | Global |
| Fund Manager | `FM` | Instrumen pasar, portofolio, trading, akuntansi | Global / client |
| Peserta DPLK / Subscriber | `MEMBER` | Baca data miliknya: portofolio, premi, benefit, dokumen | Diri sendiri |
| Marketing Agent | `AGENT` | Investor & `bond_trade` binaannya, fee miliknya | Diri sendiri + downline |
| Client Management | `CLIENT-MNG` | Obligasi, investor, laporan milik client | `client_id` |
| Client Administration | `CLIENT-ADMIN` | Input operasional harian (maker) | `client_id` |
| Client Supervisor | `CSPV` | Approval maker-checker (checker) | `client_id` |

Scope tenant: user dengan `app_user.client_id` terisi **hanya** dapat melihat dan mengubah data milik client tersebut (lihat §12.3).

---

## 5. Arsitektur & Tech Stack

### 5.1 Stack

| Komponen | Pilihan | Alasan |
|---|---|---|
| Bahasa | Go 1.25+ | `os.Root` (akses file aman dari traversal), generics, `log/slog` |
| HTTP | `github.com/gofiber/fiber/v3` | Sesuai permintaan; middleware lengkap (helmet, limiter, requestid, cors) |
| DB driver | `github.com/jackc/pgx/v5` + `pgxpool` | Driver native PostgreSQL tercepat; dukungan `pgtype` |
| Query | `sqlc` (mode `pgx/v5`) | Query SQL ditulis eksplisit lalu di-generate type-safe; selalu parameterized |
| Migrasi | `github.com/pressly/goose/v3` | Sesuai permintaan; file SQL, sequential numbering, `embed.FS` |
| Validasi | `github.com/go-playground/validator/v10` | Tag deklaratif dan validator kustom |
| Decimal | `github.com/shopspring/decimal` | Uang tidak pernah memakai `float64` |
| UUID | `github.com/google/uuid` | UUIDv7 dibuat di aplikasi (urutan waktu, ramah index); DB default `gen_random_uuid()` sebagai fallback |
| Auth | `github.com/golang-jwt/jwt/v5` (EdDSA), `golang.org/x/crypto/argon2` | Token pendek dan hash password modern |
| Object storage | `github.com/aws/aws-sdk-go-v2/service/s3` | Untuk S3 dan S3-compatible (MinIO untuk dev) |
| Cache / rate limit | Redis (wajib bila > 1 instance) | Storage limiter dan idempotency key |
| Config | `github.com/caarlos0/env/v11` | 12-factor, konfigurasi divalidasi saat start |
| Test | `testing`, `testify`, `testcontainers-go` | Integration test dengan PostgreSQL nyata |
| Kualitas | `golangci-lint`, `gosec`, `govulncheck` | Dijalankan di CI |

### 5.2 Layer (Clean Architecture)

```
HTTP (Fiber handler) ──► Service / Use case ──► Repository (interface) ──► PostgreSQL (sqlc)
        │                       │                                          ▲
        │                       └──► TxManager ────────────────────────────┘
        │                       └──► Storage (ImageStorage lokal / DocumentStorage S3)
        └──► DTO + validator
```

Aturan dependensi:

1. **Handler** hanya mem-parse request, memanggil service, dan memetakan response. Tidak ada logika bisnis atau SQL.
2. **Service** memegang aturan bisnis, transisi status, dan batas transaksi. Service tidak mengenal Fiber.
3. **Repository** berada di package `repository`: interface (dipakai service) beserta implementasinya yang memakai `sqlc`. Repository tidak berisi logika bisnis.
4. **Domain/entity** tidak bergantung pada Fiber, pgx, maupun AWS SDK.
5. Wiring dilakukan manual di `cmd/api` (constructor injection). Tidak ada global state dan tidak ada DI berbasis reflection.
6. Error dibungkus `fmt.Errorf("...: %w", err)`. Error domain (`ErrNotFound`, `ErrConflict`, …) dipetakan ke HTTP di satu tempat, yaitu error handler global.

### 5.3 Struktur Folder

Layer datar (satu package per layer, satu file per entitas), mengikuti pola registrar-api:

```
pfm-backend/
├── cmd/
│   ├── api/main.go              # HTTP server
│   ├── migrate/main.go          # goose (embed): up, down, status, version, seed, seed-admin
│   ├── worker/main.go           # job: cleanup file orphan & refresh token kedaluwarsa (berikutnya: invoice, sinkronisasi)
│   └── scaffold/                # generator CRUD (dev tool)
├── internal/
│   ├── model/                   # struct domain
│   ├── dto/                     # request/response + mapper (<tabel>_gen.go dari generator)
│   ├── repository/              # interface + implementasi sqlc (<tabel>_gen.go dari generator)
│   ├── service/                 # logika bisnis, transaksi, audit, hook
│   ├── handler/                 # HTTP handler per entitas + registrasi route
│   ├── permission/              # kode permission (tabel action)
│   ├── router/                  # middleware + wiring repository -> service -> handler
│   ├── db/
│   │   ├── migrations/          # 00001_…sql s/d 00123_…sql (go:embed)
│   │   ├── queries/             # <tabel>.sql untuk sqlc
│   │   ├── sqlc/                # hasil generate sqlc — jangan diedit manual
│   │   └── schema/              # peta FK hasil generator (cek referensi soft delete)
│   ├── platform/                # database, crud, httpx, auth, security, storage, audit, validatorx, logger
│   └── apperr/, config/, testutil/
├── deploy/                      # Dockerfile (distroless, non-root), docker-compose (postgres, minio), SQL role DB
├── sqlc.yaml
├── Makefile
└── .env.example
```

Arah dependensi: `handler → service → repository → db/sqlc`, dengan `model`/`dto` sebagai tipe bersama. `platform/*` tidak bergantung pada layer mana pun di atasnya.

Karena ada 120+ entitas, disediakan generator scaffold (`make scaffold module=masterdata`). Generator membaca skema dari database yang sudah di-migrate, lalu membuat query sqlc dan adapter repository + DTO per tabel (`*_gen.go`, jangan diedit). Service dan handler CRUD-nya generik (`internal/platform/crud`): transaksi, row lock, cek referensi sebelum soft delete, audit, dan ETag ditangani di satu tempat. Logika bisnis khusus ditulis manual sebagai hook.

---

## 6. Desain Database PostgreSQL

DDL lengkap ada di [`prd-schema-postgres.sql`](prd-schema-postgres.sql). Setiap blok `-- ===== 000NN_create_<tabel>.sql =====` adalah isi satu file migrasi goose.

### 6.1 Strategi UUID

Setiap tabel (122 tabel) memiliki:

```sql
id    BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,   -- internal: join, FK, paginasi keyset
uuid  UUID NOT NULL DEFAULT gen_random_uuid(),                -- publik: satu-satunya ID di URL & JSON
CONSTRAINT uq_<tabel>_uuid UNIQUE (uuid)
```

- **Kenapa dua kolom.** FK `BIGINT` lebih kecil dan cepat untuk join. UUID mencegah enumerasi ID (`/bonds/1`, `/bonds/2`, …) dan tidak membocorkan volume data.
- **`BY DEFAULT`, bukan `ALWAYS`.** ETL dapat mempertahankan ID legacy, sehingga referensi antar tabel tetap utuh. Setelah ETL, sequence di-`setval` ke `max(id)`.
- **Pembuatan UUID.** Aplikasi mengisi UUIDv7 (`uuid.NewV7()`). `gen_random_uuid()` hanya fallback untuk insert manual/ETL.
- **API.** Request memakai `<relasi>_uuid` (mis. `bank_class_uuid`). Service me-resolve UUID → `id` di dalam transaksi yang sama sekaligus memeriksa keberadaan data dan scope tenant. Kolom `id` **tidak pernah** muncul di response.

### 6.2 Aturan Konversi Tipe MySQL → PostgreSQL

| MySQL | PostgreSQL | Catatan |
|---|---|---|
| `int(11) AUTO_INCREMENT` (PK) | `id BIGINT GENERATED BY DEFAULT AS IDENTITY` | Nama PK diseragamkan menjadi `id` |
| `int(11)` FK | `BIGINT` + `FOREIGN KEY … REFERENCES <t>(id)` + index | PostgreSQL tidak membuat index FK otomatis |
| `int` status / tipe | `SMALLINT` | Pemetaan kode ke enum string di API (lihat §17) |
| `int` flag: `is_*`, `certificate_uploaded`, `password_changed`, `inbound_settlement`, `outbound_settlement` | `BOOLEAN NOT NULL DEFAULT FALSE` | |
| `int` lain (bulan, tahun, counter, durasi) | `INTEGER` | |
| `double` nilai uang / harga / nominal | `NUMERIC(20,4)` | Di Go memakai `decimal.Decimal`, di JSON berupa string |
| `double` persen / rate / rasio / yield / bobot / beta | `NUMERIC(12,6)` | |
| `double` unit / lembar / lot | `NUMERIC(24,8)` | |
| `double`/`decimal` di tabel `crypto*` | `NUMERIC(28,8)` | Presisi aset kripto |
| `double` latitude/longitude | `NUMERIC(9,6)` | |
| `varchar(n)` | `VARCHAR(n)` | Database UTF-8 (legacy latin1) |
| `text`, `longtext` | `TEXT` | |
| `datetime`, `timestamp` | `TIMESTAMPTZ` | Disimpan UTC |
| `date` | `DATE` | |
| `ip_address`, `approval_ip_address` | `INET` | |
| `broker_api_setting.ip_address_list` | `CIDR[]` | |
| `synchronized_data.data_json` | `JSONB` | |
| `bond_trade_data_history.bondTradeId tinyblob` | dihapus → `bond_trade_id BIGINT` FK | |
| `DEFAULT '0000-00-00 00:00:00'` | `NULL` | Tidak valid di PostgreSQL |
| Kolom `*_url` file | `*_file_id BIGINT` FK → `file_object` di `pfm_document`; tabel di `pfm_core` memakai `*_file_uuid UUID` tanpa FK (§7.1) | Lihat §8 |

### 6.3 Kolom Audit & Soft Delete (standar di semua tabel)

| Legacy | Baru |
|---|---|
| `created_time` / `createdTime` | `created_at TIMESTAMPTZ NOT NULL DEFAULT now()` |
| `created_by` / `createdBy` | `created_by VARCHAR(100)` (UUID user atau `system`) |
| `modified_time` / `modifiedTime` | `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()` + trigger `set_updated_at()` |
| `modified_by` / `modifiedBy` | `updated_by VARCHAR(100)` |
| `deleted_time` / `deletedTime` | `deleted_at TIMESTAMPTZ` (`NULL` = aktif) |
| `deleted_by` / `deletedBy` | `deleted_by VARCHAR(100)` |
| `deleted_status` / `deletedStatus` | dihapus; status hapus diturunkan dari `deleted_at` |

Pengecualian: `activity_log` dan `auth_refresh_token` hanya punya `created_at`, karena bersifat append-only.

`updated_at` juga berfungsi sebagai **token optimistic locking**: nilainya dikirim sebagai `ETag` dan diverifikasi saat `PUT`/`DELETE` (§10.3).

### 6.4 Normalisasi yang Diterapkan

| # | Masalah legacy | Perbaikan |
|---|---|---|
| 1 | Kolom audit ganda di 52 tabel | Digabung menjadi set standar §6.3 (ETL: `COALESCE(snake, camel)`) |
| 2 | FK duplikat | Satu constraint per kolom, dengan nama `fk_<tabel>_<kolom>` |
| 3 | FK hilang (kolom `*_id` tanpa constraint) | Ditambahkan: `asset_class_finance_account` (7 kolom akun/asset/portfolio), `cash_settlement.member_id/client_id`, `cash_settlement_category` (3 akun), `finance_transaction` (stock/bond/mutual_fund trade, portfolio), `portfolio_bank.portfolio_id/bank_id`, `portfolio_budget.portfolio_id/previous_budget_id`, `portfolio_budget_history` (2), `stock_size_classification_history` (2), `broker_api_setting.broker_id`, `member.city_id/province_id/country_id`, `stock_history.stock_fundamental_history_id` |
| 4 | `portfolio_cost_structure` punya dua kolom PK (`portfolio_cost_structure`, `portfolio_cost_structure_id`) | Digantikan `id` |
| 5 | Typo | PK `stock_size_clasification_history_id` → `id`; `agent_fee_transaction.bank_accoun_name` → `bank_account_name`; `tax_report.dependent_id` → `dependant_id` |
| 6 | camelCase non-audit | → snake_case (`previousBondCode` → `previous_bond_code`, `bondBuyingFeePercentage` → `bond_buying_fee_percentage`, dst.) |
| 7 | Status duplikat | `portfolio.portfolioStatus` dan `member_group.memberGroupStatus` dihapus (duplikat `portfolio_status`/`member_group_status`) |
| 8 | `unique_code`/`uniqueCode` varchar(36) di `crypto*`, `broker_api_setting`, `deposito` | Digantikan `uuid`. ETL memakai nilai lama bila berupa UUID valid |
| 9 | Tabel `user` (reserved word) | → `app_user` |
| 10 | Secret di DB | `password` → `password_hash` (argon2id); `token`/`token_expire_time` → tabel `auth_refresh_token` (hash); `activation_key` → `activation_key_hash`; `configuration.proxy_api_username/password/current_token` → env/secret manager |
| 11 | Path file di 11 kolom `*_url` | → `*_file_id` FK ke `file_object` (dalam `pfm_document`) atau `*_file_uuid` (tabel `pfm_core`) |
| 12 | Kolom snapshot terdenormalisasi (mis. `bond_trade.bond_code`, `investor_name`) | Dipertahankan sebagai snapshot saat transaksi. **Diisi service dari master**, bukan dari input klien |
| 13 | Unique code | Partial unique index `WHERE deleted_at IS NULL` pada `<tabel>_code`, `stock_code`, `bond_code`, `investor_number`, `marketing_agent_number`, `member_number`, `client_code`, `invoice_number`, `finance_account_number`, `app_user.username/email` |
| 14 | `[USULAN]` kolom yang hilang | `crypto.crypto_code/crypto_name`, `crypto_exchange.crypto_exchange_code/name`, `crypto_portfolio.portfolio_id/crypto_id`, `property_asset.property_asset_type_id` (ditandai `-- [USULAN]` di DDL; konfirmasi di §17) |

`app_user` juga mendapat kolom keamanan baru: `activation_key_expires_at`, `failed_login_count`, `locked_until`, `last_login_at`.

Constraint `NOT NULL` pada kolom bisnis **sengaja belum dipasang**, karena data legacy banyak berisi `NULL`. Field wajib ditegakkan di layer validasi (§11). Setelah data dibersihkan, constraint diperketat lewat migrasi lanjutan (`00124_…`).

### 6.5 Contoh Hasil Konversi

```sql
-- 00037_create_bank.sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE bank (
    id                 BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    uuid               UUID NOT NULL DEFAULT gen_random_uuid(),
    bank_name          VARCHAR(255),
    bank_clearing_code VARCHAR(25),
    bank_swift_code    VARCHAR(25),
    bank_class_id      BIGINT,
    description        VARCHAR(255),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by         VARCHAR(100),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by         VARCHAR(100),
    deleted_at         TIMESTAMPTZ,
    deleted_by         VARCHAR(100),
    CONSTRAINT uq_bank_uuid UNIQUE (uuid),
    CONSTRAINT fk_bank_bank_class_id FOREIGN KEY (bank_class_id) REFERENCES bank_class (id)
);
CREATE INDEX idx_bank_bank_class_id ON bank (bank_class_id);
CREATE TRIGGER trg_bank_updated_at BEFORE UPDATE ON bank
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bank;
-- +goose StatementEnd
```

### 6.6 Tabel Baru (non-legacy)

| Tabel | Migrasi | Fungsi |
|---|---|---|
| `file_object` | 00002 | Registry metadata file: storage (`local`/`s3`), `object_key`, MIME hasil deteksi, ukuran, SHA-256, status. CHECK memaksa `image ⇒ local` dan `document ⇒ s3` |
| `auth_refresh_token` | 00123 | Refresh token ter-hash, `family_id` untuk rotasi dan deteksi reuse, `revoked_at`, `replaced_by` |

---

## 7. Migrasi (Goose)

### 7.1 Empat Database

Data dipisah ke empat database PostgreSQL, masing-masing dengan kredensial sendiri. Bila satu kredensial, dump, atau celah SQL injection bocor, database lain tetap tertutup, dan jejak audit tidak bisa dihapus dengan kredensial aplikasi.

| Database | Isi | User runtime | Catatan |
|---|---|---|---|
| `pfm_core` | Seluruh data bisnis (master data, portofolio, trading, akuntansi, obligasi, dana pensiun, workflow) + `audit_outbox` | `pfm_core_app`: SELECT/INSERT/UPDATE; `audit_outbox` INSERT saja | Soft delete: tanpa DELETE |
| `pfm_security` | `app_user`, `role`, `action`, `role_action`, `auth_refresh_token` + `audit_outbox` | `pfm_security_app` | Hash password dan token tidak ikut bocor bila core bocor |
| `pfm_log` | `activity_log` | API: `pfm_log_reader` (SELECT); worker: `pfm_log_writer` (INSERT saja, tanpa SELECT) | Append-only: tidak ada role runtime yang boleh UPDATE/DELETE |
| `pfm_document` | `file_object`, `document`, `document_data_history`, `document_authorization_table`, `document_category_authorization_table` + `audit_outbox` | `pfm_document_app` | Metadata dan key penyimpanan file |

Role `pfm_worker` hanya boleh membaca dan menghapus `audit_outbox` (serta file pending/token kedaluwarsa), tanpa akses ke data bisnis. Setiap role hanya bisa CONNECT ke database miliknya (`deploy/postgres/roles.sql`, `grants-*.sql`).

Aturan:

1. **Tidak ada FK lintas database.** Referensi ke database lain disimpan sebagai kolom `<nama>_uuid UUID`, misalnya `cash_settlement.document_file_uuid`, `app_user.client_uuid`, dan `document.bond_uuid`. Referensi ini dijaga aplikasi dengan tiga cara:
   - keberadaannya divalidasi saat menulis;
   - data yang masih dirujuk tidak boleh dihapus (mis. client yang masih punya user);
   - fail-closed bila referensi hilang (user yang client-nya tidak ada ditolak saat mengakses API).
2. **Tidak ada transaksi lintas database.** Urutan ditetapkan agar aman: klaim file (`pending` → `active`) di `pfm_document` di-commit sebelum transaksi bisnis di `pfm_core`. Bila transaksi bisnis gagal, file hanya tertinggal aktif tanpa rujukan, dan itu tidak berbahaya.
3. **Audit lewat outbox.**
   - Event ditulis ke `audit_outbox` di database asal, dalam transaksi yang sama dengan perubahan bisnis (atomik).
   - `cmd/worker` memindahkannya ke `pfm_log.activity_log` setiap ±5 detik. Pengiriman idempoten lewat `uuid` dan aman dijalankan beberapa worker dengan `SKIP LOCKED`; kolom `source` mencatat database asal.
   - Log baru terlihat di `/activity-logs` setelah dikirim worker. Bila worker mati, event menumpuk di outbox dan tidak hilang.
4. **Satu `TxManager` per database.** Kunci transaksi di context dibuat per database, sehingga transaksi satu database tidak pernah dipakai database lain.
5. **Kredensial terpisah.** DSN diatur lewat `DATABASE_URL_CORE`, `DATABASE_URL_SECURITY`, `DATABASE_URL_LOG`, dan `DATABASE_URL_DOCUMENT`; konfigurasi menolak satu DSN dipakai dua database. Di production, `pfm_log` dan `pfm_security` sebaiknya berada di instance terpisah dengan backup terpisah.
6. **Batasan.** Bila proses API diambil alih seluruhnya (RCE), penyerang memegang semua kredensial yang dipegang API. Pemisahan membatasi dampak kebocoran satu DSN, dump, atau SQL injection, dan log tetap tidak bisa diubah atau dihapus dari kredensial API.

### 7.2 Aturan

1. **Satu tabel per file, di folder database-nya:** `internal/db/migrations/<core|security|log|document>/NNNNN_create_<tabel>.sql`. Nomor 5 digit berurutan, **global dan unik di semua folder** (bukan timestamp). File baru dibuat dengan `make migration db=<database> name=<nama>`. Karena nomornya global, gabungan semua folder tetap urutan goose yang valid; ini dipakai generator lewat `migrate -db all`. Folder `shared/` (fungsi `set_updated_at`) ikut ke semua database, sedangkan `outbox/` ke core, security, dan document.
2. **Urutan mengikuti dependensi FK di dalam satu database.** Tabel induk selalu bernomor lebih kecil daripada tabel anak. Urutan dihitung dengan topological sort atas graf FK (8 level, tanpa siklus). Self-reference (mis. `portfolio.parent_portfolio_id`) didefinisikan di file tabel itu sendiri.
3. **Isi setiap file:** `CREATE TABLE`, constraint (PK, UUID unique, FK), index FK, partial unique index, dan trigger `updated_at`. Semuanya diapit `-- +goose StatementBegin/End`.
4. **Setiap file wajib punya `-- +goose Down`** yang menghapus tabel tersebut, sehingga `down` berjalan terbalik tanpa melanggar FK.
5. **File yang sudah ter-apply di environment mana pun tidak boleh diedit.** Perubahan dibuat sebagai file baru, mis. `core/00130_alter_bond_add_isin.sql`.
6. `CREATE INDEX CONCURRENTLY` pada tabel besar memakai `-- +goose NO TRANSACTION` di file terpisah.
7. Data referensi (role, action/permission, trade_type, asset_class, dst.) dimuat oleh ETL/seed command, **bukan** dicampur ke migrasi DDL. User admin awal dibuat lewat `cmd/migrate seed-admin` dengan password dari env. Tidak ada kredensial default.
8. Migrasi dijalankan sebagai langkah deploy terpisah memakai role DB `pfm_migrator`, bukan saat API start. `cmd/migrate up` menjalankan keempat database berurutan; `-db <nama>` membatasi ke satu database, dan `down`/`reset` wajib menyebut satu database. Setelah migrasi, jalankan `make db-grants`. Gunakan `goose.NewProvider(..., goose.WithSessionLocker(...))` (advisory lock) agar dua runner tidak berjalan bersamaan.

### 7.3 Runner (embed)

```go
// internal/db/embed.go
//go:embed migrations
var migrations embed.FS

// Migrations(db.Core) menggabungkan shared/ + outbox/ + core/ menjadi satu
// direktori datar (nama file wajib unik); db.All = semua folder (generator).
func Migrations(name string) (fs.FS, error)
```

```go
// cmd/migrate: satu goose Provider per database, dengan advisory lock
p, err := db.NewMigrator(stdlib.OpenDBFromPool(pool), db.Core)
_, err = p.Up(ctx)
```

### 7.4 Pemeriksaan CI

- Untuk setiap database dan gabungan `all`: `up` → `down-to 0` → `up` di PostgreSQL ephemeral. Untuk `all`, nomor migrasi wajib tanpa celah.
- Test integrasi membuat empat database terpisah per test. Test lain memastikan setiap database hanya berisi tabelnya sendiri, outbox tidak pernah mengirim event dari transaksi yang gagal, dan user yang client-nya hilang ditolak.
- Query pemeriksa: setiap tabel punya kolom `uuid`, setiap FK punya index, dan tidak ada kolom `double precision` / `timestamp without time zone`.

Urutan lengkap ada di **Lampiran A**.

---

## 8. Penyimpanan File

### 8.1 Pembagian

| Kategori | Storage | Dipakai oleh | Tipe diizinkan | Ukuran maks |
|---|---|---|---|---|
| **Image** | Filesystem lokal (`STORAGE_IMAGE_ROOT`) | `configuration.qr_logo_file_uuid` (dan logo/foto di masa depan) | `image/png`, `image/jpeg`, `image/webp` | 2 MB, maks 4096×4096 px |
| **Dokumen** | S3 (`S3_BUCKET`) | `document.document_file_id`, `document_data_history.document_file_id`/`previous_document_file_id`, `member_document.document_file_uuid`, `payment_document.document_file_uuid`, `cash_settlement.document_file_uuid`, `investor_coupon_schedule.payment_document_file_uuid`, `agent_fee_transaction.payment_document_file_uuid`/`tax_document_file_uuid`, `newsletter.document_file_uuid` | `application/pdf` | 20 MB |
| **Dokumen (template)** | S3 | `newsletter.email_template_file_uuid` | `text/html` | 512 KB |

Allowlist didefinisikan **per field**, bukan global.

### 8.2 Alur Upload (dua fase)

1. `POST /api/v1/files/images` atau `POST /api/v1/files/documents` (multipart, field `file`, plus `purpose` = nama field target, mis. `member_document.document`).
2. Server memvalidasi file (§8.4), menyimpannya ke storage, lalu insert `file_object` dengan `status='pending'`. Response berisi `file_uuid`.
3. Klien mengirim `document_file_uuid` di body create/update entitas. Di dalam **transaksi entitas**, service memastikan file berstatus `pending`, dibuat oleh user yang sama, dan `purpose`-nya cocok. Setelah itu service mengisi FK dan mengubah status menjadi `active`.
4. Job worker menghapus file `pending` yang berumur > 24 jam, baik di storage maupun DB.

S3 tidak ikut transaksi database. Karena itu urutannya selalu **storage dulu, DB kemudian**. Jika transaksi DB gagal, file yatim dibersihkan oleh job pada langkah 4.

### 8.3 Interface

```go
// internal/platform/storage/storage.go
type ImageStorage interface {
    Put(ctx context.Context, key string, r io.Reader, contentType string) error
    Open(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
}

type DocumentStorage interface {
    Put(ctx context.Context, key string, r io.Reader, size int64, contentType, sha256 string) error
    PresignGet(ctx context.Context, key, downloadName string, ttl time.Duration) (string, error)
    Delete(ctx context.Context, key string) error
}
```

Implementasi: `storage/local` (memakai `os.Root`) dan `storage/s3` (aws-sdk-go-v2). Untuk development dipakai MinIO (`S3_ENDPOINT`, `S3_USE_PATH_STYLE=true`).

### 8.4 Keamanan File

| Kontrol | Image (lokal) | Dokumen (S3) |
|---|---|---|
| Deteksi tipe | Magic bytes (`http.DetectContentType` + decoder), **bukan** ekstensi atau header `Content-Type` klien | Magic bytes `%PDF-`; template dicek UTF-8 valid |
| Batas ukuran | `fiber.Config.BodyLimit` khusus route upload + `io.LimitReader` | Sama |
| Sanitasi konten | Decode → re-encode (menghapus EXIF dan payload polyglot); cek dimensi lewat `image.DecodeConfig` sebelum decode penuh (anti decompression bomb) | Tolak PDF berisi `/JavaScript`, `/JS`, `/Launch`, `/EmbeddedFile`; scan ClamAV (opsional, SHOULD) → `status='quarantined'` |
| Nama objek | `images/{yyyy}/{mm}/{uuid}.{ext}`, dibangkitkan server | `documents/{modul}/{yyyy}/{mm}/{uuid}.pdf`, dibangkitkan server |
| Nama asli | Hanya disimpan di `file_object.original_name` setelah disanitasi | Sama |
| Path traversal | `os.Root` (Go 1.24+) | Tidak relevan (key dibuat server) |
| Akses baca | `GET /files/{uuid}/content` dengan auth + authz; header `X-Content-Type-Options: nosniff`, `Content-Security-Policy: default-src 'none'`, `Cache-Control: private` | `GET /files/{uuid}/download` → cek authz (pemilik entitas + `document_authorization_table`) → **302** ke presigned URL (TTL 5 menit, `Content-Disposition: attachment`) |
| At rest | Volume terenkripsi, permission `0750`/`0640`, di luar web root | Bucket private (Block Public Access), SSE-KMS, versioning aktif |
| Integritas | SHA-256 disimpan | SHA-256 disimpan + `ChecksumAlgorithm=SHA256` saat upload |
| Audit | Upload/hapus dicatat di `activity_log` | Upload/download/hapus dicatat di `activity_log` |

Catatan deployment: image lokal membutuhkan volume persisten. Jika API berjalan lebih dari satu instance, volume harus shared (NFS/EFS) atau upload image diarahkan ke satu instance.

---

## 9. Repository & Transaction

### 9.1 Executor yang sadar transaksi

```go
// internal/platform/database/tx.go
type DBTX interface {
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txKey struct{}

type TxManager struct{ pool *pgxpool.Pool }

func NewTxManager(pool *pgxpool.Pool) *TxManager { return &TxManager{pool: pool} }

// WithinTx menjalankan fn dalam satu transaksi. Bila ctx sudah membawa transaksi,
// fn ikut transaksi tersebut (tidak membuka transaksi bersarang).
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
    return m.WithinTxOpts(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted}, fn)
}

func (m *TxManager) WithinTxOpts(ctx context.Context, opts pgx.TxOptions, fn func(ctx context.Context) error) error {
    if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
        return fn(ctx)
    }
    return pgx.BeginTxFunc(ctx, m.pool, opts, func(tx pgx.Tx) error {
        return fn(context.WithValue(ctx, txKey{}, tx))
    })
}

// Executor mengembalikan transaksi aktif bila ada, selain itu pool.
func (m *TxManager) Executor(ctx context.Context) DBTX {
    if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
        return tx
    }
    return m.pool
}
```

`pgx.BeginTxFunc` otomatis melakukan commit bila `fn` mengembalikan `nil`, dan rollback bila error atau panic.

### 9.2 Repository

```go
// internal/module/masterdata/bank_repository.go
type BankRepository interface {
    GetByUUID(ctx context.Context, id uuid.UUID) (Bank, error)
    List(ctx context.Context, f BankFilter, p Page) ([]Bank, int64, error)
    Create(ctx context.Context, b *Bank) error
    Update(ctx context.Context, b *Bank, expectedUpdatedAt time.Time) error // ErrPreconditionFailed bila 0 baris
    SoftDelete(ctx context.Context, id uuid.UUID, by string, expectedUpdatedAt time.Time) error
    HasActiveReferences(ctx context.Context, id int64) (bool, error)
}

type bankRepo struct{ tx *database.TxManager }

func (r *bankRepo) q(ctx context.Context) *sqlc.Queries { return sqlc.New(r.tx.Executor(ctx)) }

func (r *bankRepo) GetByUUID(ctx context.Context, id uuid.UUID) (Bank, error) {
    row, err := r.q(ctx).GetBankByUUID(ctx, id)
    if errors.Is(err, pgx.ErrNoRows) {
        return Bank{}, apperr.ErrNotFound
    }
    if err != nil {
        return Bank{}, fmt.Errorf("get bank: %w", err)
    }
    return toBank(row), nil
}
```

Query sqlc (`internal/db/queries/bank.sql`):

```sql
-- name: GetBankByUUID :one
SELECT * FROM bank WHERE uuid = $1 AND deleted_at IS NULL;

-- name: ListBanks :many
SELECT * FROM bank
WHERE deleted_at IS NULL
  AND (sqlc.narg('q')::text IS NULL OR bank_name ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
  AND (sqlc.narg('bank_class_id')::bigint IS NULL OR bank_class_id = sqlc.narg('bank_class_id'))
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'bank_name'   THEN bank_name END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-bank_name'  THEN bank_name END DESC,
  CASE WHEN sqlc.arg('sort')::text = '-created_at' THEN created_at END DESC,
  id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: UpdateBank :execrows
UPDATE bank
SET bank_name = $2, bank_clearing_code = $3, bank_swift_code = $4,
    bank_class_id = $5, description = $6, updated_by = $7
WHERE uuid = $1 AND updated_at = $8 AND deleted_at IS NULL;

-- name: SoftDeleteBank :execrows
UPDATE bank SET deleted_at = now(), deleted_by = $2
WHERE uuid = $1 AND updated_at = $3 AND deleted_at IS NULL;
```

Aturan repository:

- Semua query bersifat parameterized. **Tidak ada** konkatenasi string dari input user.
- Nilai `sort` dipetakan dari whitelist di service; nilai di luar whitelist ditolak dengan 422.
- Input pencarian `q` di-escape (`\`, `%`, `_`) sebelum dipakai di `ILIKE`.
- Semua query baca menyaring `deleted_at IS NULL`.
- Query untuk tabel ber-tenant menerima parameter `client_id` dari scope, bukan dari input.
- `SoftDelete` tidak memicu FK. Karena itu service wajib memanggil `HasActiveReferences` dan menolak dengan 409 bila data masih dipakai.

### 9.3 Service dengan Transaksi

```go
// internal/module/bond/bond_trade_service.go
func (s *BondTradeService) Approve(ctx context.Context, actor auth.Actor, id uuid.UUID, etag time.Time) (BondTrade, error) {
    var out BondTrade
    err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
        trade, err := s.trades.GetForUpdate(ctx, actor.Scope(), id) // SELECT … FOR UPDATE
        if err != nil {
            return err
        }
        if !trade.UpdatedAt.Equal(etag) {
            return apperr.ErrPreconditionFailed
        }
        if trade.Status != TradeStatusDraft {
            return apperr.Conflict("bond_trade bukan draft")
        }
        if trade.CreatedBy == actor.UserUUID.String() {
            return apperr.Forbidden("maker tidak boleh menjadi checker")
        }

        // UPDATE configuration SET certificate_latest_counter = certificate_latest_counter + 1 RETURNING …
        counter, err := s.config.NextCertificateCounter(ctx)
        if err != nil {
            return err
        }
        trade.Approve(actor, counter)

        if err := s.trades.Update(ctx, &trade); err != nil {
            return err
        }
        if err := s.coupons.GenerateForTrade(ctx, trade); err != nil { // insert investor_coupon_schedule
            return err
        }
        if err := s.bonds.AddFund(ctx, trade.BondID, trade.Nominal); err != nil {
            return err
        }
        out = trade
        return s.audit.Record(ctx, actor, "bond_trade.approve", trade.UUID)
    })
    return out, err
}
```

### 9.4 Kebijakan Transaksi

| Kasus | Kebijakan |
|---|---|
| CRUD tunggal | Tanpa `WithinTx` eksplisit (satu statement sudah atomik), kecuali ada audit/history yang ikut ditulis |
| Header-detail (`finance_transaction` + detail, `placement` + detail, `benefit_payout_batch` + payout) | Satu transaksi. Detail divalidasi seluruhnya sebelum insert |
| Mutasi posisi (`*_trade` settle, `member_portfolio_transaction`) | Satu transaksi; baris posisi dikunci dengan `SELECT … FOR UPDATE` (urutan penguncian konsisten: portfolio → posisi → akun) |
| Penomoran (investor, agen, sertifikat, agreement, invoice) | `UPDATE configuration SET x = x + 1 RETURNING x` di dalam transaksi pemakai nomor (row lock otomatis, tanpa gap jika rollback) |
| Snapshot history (`*_history`, `*_data_history`) | Ditulis di transaksi yang sama dengan perubahan sumber |
| Isolation | `READ COMMITTED` + row lock. `SERIALIZABLE` hanya untuk proses batch (generate invoice/payout) dengan retry maksimal 3× pada SQLSTATE `40001`/`40P01` |
| Timeout | `SET LOCAL statement_timeout = '5s'` untuk request API; job batch memakai timeout sendiri |
| Panggilan eksternal (S3, email, API sinkronisasi) | **Tidak boleh** dilakukan di dalam transaksi DB. Panggilan dilakukan sebelum transaksi (upload) atau sesudahnya (kirim notifikasi lewat outbox/job) |

Pemetaan error PostgreSQL di satu tempat (`database.MapError`):

| SQLSTATE | Arti | HTTP |
|---|---|---|
| `23505` unique_violation | Kode/nomor sudah ada | 409 `CONFLICT` |
| `23503` foreign_key_violation | Relasi tidak valid | 422 `VALIDATION_ERROR` |
| `23514` check_violation | Nilai melanggar constraint | 422 `VALIDATION_ERROR` |
| `40001`, `40P01` | Serialization / deadlock | retry, lalu 409 |
| `57014` query_canceled | Timeout | 503 |

---

## 10. Desain API & CRUD

### 10.1 Konvensi

| Aspek | Aturan |
|---|---|
| Base path | `/api/v1` |
| Resource | kebab-case jamak dari nama tabel (`bond_coupon_schedule` → `bond-coupon-schedules`) |
| ID di URL | Selalu `uuid` |
| Format | JSON UTF-8; field `snake_case` |
| Uang / desimal | String (`"1500000.0000"`) agar tidak kehilangan presisi |
| Tanggal | `DATE` → `"2026-11-13"`; timestamp → RFC 3339 UTC |
| Relasi | Request: `<relasi>_uuid`. Response: `<relasi>_uuid` + opsi `?expand=<relasi>` untuk objek ringkas |
| Status | Enum string di API (mis. `draft`, `approved`), dipetakan ke `SMALLINT` di DB |
| Paginasi | `?page=1&per_page=20` (maks 100). Tabel time-series/log besar memakai keyset `?cursor=…` |
| Sort | `?sort=-created_at,bank_name` (whitelist per resource) |
| Filter | Query param eksplisit per resource (mis. `?bank_class_uuid=…&q=…`); param tak dikenal → 422 |
| Concurrency | Response detail membawa `ETag`; `PUT`/`DELETE`/aksi wajib `If-Match` (428 bila tidak ada, 412 bila tidak cocok) |
| Idempotency | `POST` pada kelas TRX wajib membawa header `Idempotency-Key` (disimpan di Redis 24 jam) |

### 10.2 Envelope Response

```json
// sukses (detail)
{ "data": { "uuid": "0193…", "bank_name": "BANK SYARIAH MANDIRI", "bank_class_uuid": "0193…" },
  "meta": { "request_id": "01JC…" } }

// sukses (list)
{ "data": [ … ],
  "meta": { "request_id": "01JC…", "page": 1, "per_page": 20, "total": 135, "total_pages": 7 } }

// error
{ "error": { "code": "VALIDATION_ERROR", "message": "Input tidak valid",
             "details": [ { "field": "bank_swift_code", "rule": "bic", "message": "format SWIFT/BIC tidak valid" } ] },
  "meta": { "request_id": "01JC…" } }
```

| HTTP | `code` | Kapan |
|---|---|---|
| 400 | `BAD_REQUEST` | JSON rusak, field tak dikenal, tipe salah |
| 401 | `UNAUTHENTICATED` | Token tidak ada, kedaluwarsa, atau tidak valid |
| 403 | `FORBIDDEN` | Permission tidak cukup / maker = checker |
| 404 | `NOT_FOUND` | Data tidak ada **atau di luar scope tenant** (tidak membocorkan keberadaan data) |
| 409 | `CONFLICT` | Duplikat, transisi status tidak sah, data masih direferensikan |
| 412 | `PRECONDITION_FAILED` | `If-Match` tidak cocok (data sudah diubah orang lain) |
| 413 | `PAYLOAD_TOO_LARGE` | Body/file melebihi batas |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Content-Type / jenis file tidak diizinkan |
| 422 | `VALIDATION_ERROR` | Gagal validasi field atau aturan bisnis |
| 428 | `PRECONDITION_REQUIRED` | `If-Match` / `Idempotency-Key` wajib tetapi tidak ada |
| 429 | `RATE_LIMITED` | Melebihi rate limit |
| 500 | `INTERNAL_ERROR` | Pesan generik + `request_id`; detail hanya di log |

### 10.3 Pola Endpoint CRUD Standar

Berlaku untuk setiap resource `{res}` dengan permission `{tabel}:{aksi}`:

| Method | Path | Fungsi | Permission | Sukses |
|---|---|---|---|---|
| `GET` | `/api/v1/{res}` | List + filter + sort + paginasi | `{tabel}:read` | 200 |
| `GET` | `/api/v1/{res}/{uuid}` | Detail | `{tabel}:read` | 200 + `ETag` |
| `POST` | `/api/v1/{res}` | Create | `{tabel}:create` | 201 + `Location` + `ETag` |
| `PUT` | `/api/v1/{res}/{uuid}` | Update seluruh field yang boleh diubah (`If-Match` wajib) | `{tabel}:update` | 200 + `ETag` |
| `DELETE` | `/api/v1/{res}/{uuid}` | Soft delete (`If-Match` wajib) | `{tabel}:delete` | 204 |
| `POST` | `/api/v1/{res}/{uuid}/{aksi}` | Aksi domain (`approve`, `settle`, `pay`, `cancel`, …) | `{tabel}:{aksi}` | 200 |
| `GET` | `/api/v1/{parent}/{uuid}/{child}` | Nested list untuk tabel anak | `{child}:read` | 200 |

Kolom yang **tidak pernah** dapat diisi klien: `id`, `uuid` (kecuali ETL), `created_*`, `updated_*`, `deleted_*`, kolom kalkulasi (`latest_*`, `avg_*`, `total_*`, P/L, saldo posisi), kolom snapshot, counter, dan `status` (diubah hanya lewat aksi). DTO request tidak memiliki field-field ini sama sekali, dan karena decoding berjalan strict, request yang menyertakannya ditolak dengan 400.

### 10.4 Kelas Operasi

| Kelas | Operasi | Perilaku |
|---|---|---|
| **MASTER** | L G C U D | CRUD penuh. Delete = soft delete; ditolak 409 bila masih direferensikan data aktif |
| **CHILD** | L G C U D | Seperti MASTER. Parent wajib ada, aktif, dan dalam scope tenant. Tersedia juga sebagai nested route |
| **TIMESERIES** | L G C U D + import | `POST /{res}/import` (CSV, maks 10.000 baris, all-or-nothing dalam 1 transaksi). Insert baris terbaru meng-update kolom `latest_*` di parent |
| **TRX** | L G C U\* D\* + aksi | Create = `draft`. U\*/D\* hanya saat `draft`. Transisi status lewat aksi. Semua efek samping dalam 1 transaksi DB. `Idempotency-Key` wajib. Setelah posted: koreksi lewat reversal, bukan edit |
| **APPROVAL** | L G C + approve/reject | Maker-checker: `POST` = ajukan perubahan, `/approve` atau `/reject` oleh user lain dengan permission checker |
| **POSITION** | L G | Read-only; hanya dimutasi oleh service TRX |
| **HIST** | L G | Snapshot read-only, ditulis service di transaksi perubahan sumber |
| **LOG** | L G | Append-only; role DB aplikasi tidak punya `UPDATE`/`DELETE` |
| **SINGLETON** | G U | Satu baris konfigurasi |
| **AUTH** | — | Dikelola endpoint `/auth/*` |
| **FILE** | C G D | Upload/download file |

Legenda operasi: L = list, G = get, C = create, U = update, D = delete (soft).

### 10.5 Matriks CRUD per Tabel

#### IAM & Akses — package `internal/module/iam` (6 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00003 | `action` | `actions` | MASTER | L G C U D | Katalog permission (`<resource>:<verb>`, `module`, `resource_label`, `is_system`); permission sistem di-seed dari kode dan read-only, permission kustom bebas. `GET /permission-catalog` = katalog terkelompok untuk editor role. |
| 00004 | `activity_log` | `activity-logs` | LOG | L G | Ditulis oleh middleware audit; tidak ada C/U/D lewat API. |
| 00028 | `role` | `roles` | MASTER | L G C U D | Hapus ditolak jika masih dipakai `app_user`. |
| 00051 | `role_action` | `role-actions` | CHILD | L G C U D | Nested `GET/PUT /roles/{uuid}/actions` — PUT mengganti seluruh set permission dalam 1 transaksi (delete+insert), dengan aturan anti-eskalasi §12.2. |
| 00063 | `app_user` | `users` | MASTER | L G C U D | `password_hash` tidak pernah dikembalikan. Aksi: `POST /{uuid}/reset-password`, `/lock`, `/unlock`. Soft delete + revoke semua refresh token dalam 1 transaksi. |
| 00123 | `auth_refresh_token` | — | AUTH | — | Tidak ada CRUD. Dikelola `/auth/login`, `/auth/refresh`, `/auth/logout`, `/auth/logout-all`. |

#### Master Data / Referensi — package `internal/module/masterdata` (25 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00005 | `asset_class` | `asset-classes` | MASTER | L G C U D | Tree (`parent_asset_class_id`); validasi tidak boleh siklus. |
| 00006 | `bank_class` | `bank-classes` | MASTER | L G C U D | — |
| 00008 | `bond_periode_option` | `bond-periode-options` | MASTER | L G C U D | Template nomor sertifikat divalidasi placeholder-nya (`{bondCode}`, `{series}`, `{counter}`). |
| 00009 | `bond_type` | `bond-types` | MASTER | L G C U D | — |
| 00011 | `configuration` | `configuration` | SINGLETON | G U | Hanya `GET` & `PUT /configuration`. Upload logo QR → image lokal. Counter (`latest_*_counter`) read-only, diubah service dengan `SELECT … FOR UPDATE`. Kredensial proxy dipindah ke env/secret manager. File: `qr_logo_file_uuid`. |
| 00012 | `country` | `countries` | MASTER | L G C U D | — |
| 00015 | `document_category` | `document-categories` | MASTER | L G C U D | — |
| 00016 | `finance_account_type` | `finance-account-types` | MASTER | L G C U D | — |
| 00019 | `investor_category` | `investor-categories` | MASTER | L G C U D | — |
| 00021 | `mutual_fund_category` | `mutual-fund-categories` | MASTER | L G C U D | — |
| 00023 | `occupation` | `occupations` | MASTER | L G C U D | — |
| 00024 | `placement_type` | `placement-types` | MASTER | L G C U D | — |
| 00025 | `premium_payment_type` | `premium-payment-types` | MASTER | L G C U D | — |
| 00026 | `property_asset_type` | `property-asset-types` | MASTER | L G C U D | — |
| 00027 | `rating_agency` | `rating-agencies` | MASTER | L G C U D | — |
| 00029 | `sector` | `sectors` | MASTER | L G C U D | Tree (`parent_sector_id`); validasi tidak boleh siklus. |
| 00030 | `stock_size_classification` | `stock-size-classifications` | MASTER | L G C U D | — |
| 00031 | `stock_type` | `stock-types` | MASTER | L G C U D | — |
| 00035 | `tax_rate` | `tax-rates` | MASTER | L G C U D | — |
| 00036 | `trade_type` | `trade-types` | MASTER | L G C U D | — |
| 00037 | `bank` | `banks` | MASTER | L G C U D | Filter `bank_class_uuid`. |
| 00049 | `province` | `provinces` | MASTER | L G C U D | Filter `country_uuid`; nested `GET /countries/{uuid}/provinces`. |
| 00050 | `rating` | `ratings` | MASTER | L G C U D | Filter `rating_agency_uuid`. |
| 00052 | `sub_sector` | `sub-sectors` | MASTER | L G C U D | Filter `sector_uuid`. |
| 00053 | `city` | `cities` | MASTER | L G C U D | Filter `province_uuid`. |

#### Instrumen Pasar — package `internal/module/market` (21 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00010 | `broker` | `brokers` | MASTER | L G C U D | — |
| 00013 | `crypto` | `cryptos` | MASTER | L G C U D | — |
| 00014 | `crypto_exchange` | `crypto-exchanges` | MASTER | L G C U D | — |
| 00017 | `indices` | `indices` | MASTER | L G C U D | — |
| 00018 | `investment_manager` | `investment-managers` | MASTER | L G C U D | — |
| 00038 | `broker_api_setting` | `broker-api-settings` | CHILD | L G C U D | Nested di broker. `ip_address_list` divalidasi CIDR. Hanya ADMIN. |
| 00040 | `crypto_listing_exchange` | `crypto-listing-exchanges` | CHILD | L G C U D | Mapping crypto ↔ exchange. |
| 00043 | `indices_history` | `indices-histories` | TIMESERIES | L G C U D + import | Insert terbaru meng-update `indices.closing_value`. |
| 00045 | `mutual_fund` | `mutual-funds` | MASTER | L G C U D | Kolom `net_asset_value`/`latest_update_date` diisi dari NAV terbaru. |
| 00055 | `deposito` | `depositos` | MASTER | L G C U D | Perubahan rate → insert `deposito_interest_history` (1 transaksi). |
| 00056 | `emitent` | `emitents` | MASTER | L G C U D | — |
| 00058 | `mutual_fund_history` | `mutual-fund-histories` | TIMESERIES | L G C U D + import | NAV harian; insert terbaru meng-update `mutual_fund.net_asset_value`. |
| 00062 | `stock` | `stocks` | MASTER | L G C U D | Kolom `latest_*`/`avg_*` read-only (dihitung dari time-series). |
| 00066 | `deposito_history` | `deposito-histories` | HIST | L G | Snapshot otomatis saat deposito di-rollover/cair. |
| 00067 | `deposito_interest_history` | `deposito-interest-histories` | TIMESERIES | L G C U D + import | Riwayat suku bunga deposito. |
| 00070 | `indices_stock` | `indices-stocks` | CHILD | L G C U D | Konstituen indeks; bobot 0–100. |
| 00079 | `stock_beta_history` | `stock-beta-histories` | TIMESERIES | L G C U D + import | Insert terbaru meng-update `stock.beta_value`. |
| 00080 | `stock_fundamental_history` | `stock-fundamental-histories` | TIMESERIES | L G C U D + import | Insert terbaru meng-update rasio `stock.latest_*`. |
| 00082 | `stock_size_classification_history` | `stock-size-classification-histories` | TIMESERIES | L G C U D + import | — |
| 00084 | `stock_type_detail` | `stock-type-details` | CHILD | L G C U D | Mapping stock ↔ stock_type; unik per pasangan aktif. |
| 00104 | `stock_history` | `stock-histories` | TIMESERIES | L G C U D + import | Harga penutupan harian. Bulk import CSV. Insert data terbaru meng-update `stock.latest_*` dalam 1 transaksi. |

#### Portofolio — package `internal/module/portfolio` (22 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00039 | `client` | `clients` | MASTER | L G C U D | Tenant. User dengan `client_id` hanya melihat data client-nya. |
| 00041 | `crypto_trade_plan` | `crypto-trade-plans` | MASTER | L G C U D | — |
| 00047 | `placement` | `placements` | TRX | L G C U* D* + aksi | Header-detail: header + `placement_detail` dibuat/diubah dalam 1 transaksi. |
| 00048 | `property_asset` | `property-assets` | MASTER | L G C U D | — |
| 00060 | `placement_detail` | `placement-details` | CHILD | L G C U D | Hanya lewat header `placement` (nested). |
| 00061 | `portfolio` | `portfolios` | MASTER | L G C U D | Tree (`parent_portfolio_id`). Kolom P/L & CAGR read-only (hasil kalkulasi). |
| 00065 | `crypto_portfolio` | `crypto-portfolios` | POSITION | L G | Hanya L/G. Diubah oleh `crypto_trade`. |
| 00068 | `deposito_portfolio` | `deposito-portfolios` | TRX | L G C U* D* + aksi | Penempatan deposito. Aksi `/rollover` (buat baris baru + set `next_portfolio_id`/`previous_portfolio_id`) & `/withdraw`, masing-masing 1 transaksi + snapshot history + jurnal. |
| 00074 | `mutual_fund_portfolio` | `mutual-fund-portfolios` | POSITION | L G | Hanya L/G. Diubah oleh `mutual_fund_trade`. |
| 00076 | `portfolio_bank` | `portfolio-banks` | CHILD | L G C U D | Nested `/portfolios/{uuid}/banks`. |
| 00077 | `portfolio_budget` | `portfolio-budgets` | CHILD | L G C U D | Unik per (portfolio, asset_class, budget_year). Kolom realisasi read-only. |
| 00078 | `portfolio_note` | `portfolio-notes` | CHILD | L G C U D | Nested `/portfolios/{uuid}/notes`. |
| 00081 | `stock_portfolio` | `stock-portfolios` | POSITION | L G | Hanya L/G. Diubah oleh `stock_trade` saat settle. |
| 00087 | `asset_class_finance_account` | `asset-class-finance-accounts` | CHILD | L G C U D | Mapping akun buy/sell/gain/loss/coupon per asset_class & portfolio. |
| 00092 | `bond_portfolio` | `bond-portfolios` | POSITION | L G | Hanya L/G. Diubah oleh transaksi obligasi. |
| 00096 | `deposito_portfolio_history` | `deposito-portfolio-histories` | HIST | L G | — |
| 00100 | `mutual_fund_portfolio_history` | `mutual-fund-portfolio-histories` | HIST | L G | — |
| 00101 | `portfolio_accounting_structure` | `portfolio-accounting-structures` | CHILD | L G C U D | Mapping akun jurnal per asset_class × trade_type; dipakai saat posting jurnal. |
| 00102 | `portfolio_budget_history` | `portfolio-budget-histories` | HIST | L G | Snapshot realisasi bulanan (job). |
| 00103 | `portfolio_cost_structure` | `portfolio-cost-structures` | CHILD | L G C U D | Konfigurasi biaya per trade_type. |
| 00105 | `stock_portfolio_history` | `stock-portfolio-histories` | HIST | L G | Snapshot posisi harian/saat trade. |
| 00106 | `bond_portfolio_history` | `bond-portfolio-histories` | HIST | L G | — |

#### Trading — package `internal/module/trading` (3 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00054 | `crypto_trade` | `crypto-trades` | TRX | L G C U* D* + aksi | Pola sama → `crypto_portfolio`. Presisi NUMERIC(28,8). |
| 00075 | `mutual_fund_trade` | `mutual-fund-trades` | TRX | L G C U* D* + aksi | Pola sama dengan stock_trade → `mutual_fund_portfolio`. |
| 00083 | `stock_trade` | `stock-trades` | TRX | L G C U* D* + aksi | Status draft → settled/cancelled. Settle: update `stock_portfolio` (avg price, lot), insert `stock_portfolio_history`, posting `finance_transaction`+detail — 1 transaksi. Delete hanya draft; setelah settle hanya reversal. |

#### Penerbitan Obligasi, Investor & Agen — package `internal/module/bond` (15 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00020 | `marketing_agent` | `marketing-agents` | MASTER | L G C U D | Tree leader (`leader_agent_id`). Perubahan data lewat `marketing_agent_data_history`. |
| 00044 | `marketing_agent_data_history` | `marketing-agent-data-histories` | APPROVAL | L G + approve/reject | Pola sama dengan investor_data_history; ajukan lewat `POST /marketing-agents/{uuid}/change-requests`. |
| 00064 | `bond` | `bonds` | MASTER | L G C U D | Aksi `/activate`, `/generate-coupon-schedule` (insert semua `bond_coupon_schedule` dalam 1 transaksi). Perubahan harga/rating → insert `bond_history`. |
| 00071 | `investor` | `investors` | MASTER | L G C U D | `investor_number` dari template+counter (`SELECT … FOR UPDATE` di `configuration`). Perubahan data oleh non-ADMIN lewat `investor_data_history` (maker-checker). |
| 00086 | `agent_fee_transaction` | `agent-fee-transactions` | TRX | L G C U* D* + aksi | Komisi agen. Upload bukti bayar & bukti pajak (PDF → S3). Aksi `/pay`. File: `payment_document_file_uuid`, `tax_document_file_uuid`. |
| 00088 | `bank_account` | `bank-accounts` | CHILD | L G C U D | Milik investor ATAU marketing_agent (tepat satu; CHECK di service + DB). |
| 00089 | `bond_coupon_schedule` | `bond-coupon-schedules` | CHILD | L G C U D | Digenerate dari bond. Aksi `/pay` → generate/bayar `investor_coupon_schedule` untuk semua `bond_trade` aktif (1 transaksi). |
| 00090 | `bond_history` | `bond-histories` | HIST | L G | — |
| 00091 | `bond_periode_distribution` | `bond-periode-distributions` | CHILD | L G C U D | — |
| 00093 | `bond_rating` | `bond-ratings` | CHILD | L G C U D | Insert rating baru meng-update `bond.rating_id` (1 transaksi). |
| 00097 | `investor_data_history` | `investor-data-histories` | APPROVAL | L G + approve/reject | Change request: ajukan lewat `POST /investors/{uuid}/change-requests`, lalu `/approve` & `/reject` (checker ≠ maker). Approve → update `investor` + simpan IP & approver, 1 transaksi. |
| 00107 | `bond_trade` | `bond-trades` | TRX | L G C U* D* + aksi | Subscription investor. Draft → approved → (redeemed/rollover). Approve: nomor sertifikat (counter + row lock), generate `investor_coupon_schedule`, update total di `bond` — 1 transaksi. Dokumen subscription/transfer → S3. Header `Idempotency-Key` wajib. |
| 00112 | `bond_trade_data_history` | `bond-trade-data-histories` | HIST | L G | Snapshot sebelum `bond_trade` diubah. |
| 00115 | `investor_coupon_schedule` | `investor-coupon-schedules` | CHILD | L G C U D | Aksi `/pay` + upload bukti bayar (PDF → S3). File: `payment_document_file_uuid`. |
| 00121 | `payment_document` | `payment-documents` | CHILD | L G C U D | Upload PDF → S3. File: `document_file_uuid`. |

#### Dana Pensiun (DPLK) — package `internal/module/pension` (12 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00007 | `benefit_payout_batch` | `benefit-payout-batches` | TRX | L G C U* D* + aksi | `POST /benefit-payout-batches/generate` (idempoten per bulan; baris untuk peserta pensiun & tanggungan), `/approve` (maker-checker), `/pay`, `/cancel`; masing-masing 1 transaksi. |
| 00057 | `invoice` | `invoices` | TRX | L G C U* D* + aksi | `POST /invoices/generate` per client per bulan (idempoten): invoice + `member_premium_payment` dalam 1 transaksi; periode terakhir dicatat di `configuration`. Aksi `/issue`, `/cancel`. |
| 00072 | `member` | `members` | MASTER | L G C U D | `member_number` dari counter. PII dimasking di response. |
| 00073 | `member_group` | `member-groups` | MASTER | L G C U D | — |
| 00095 | `dependant` | `dependants` | CHILD | L G C U D | Nested `/members/{uuid}/dependants`. |
| 00098 | `member_portfolio` | `member-portfolios` | POSITION | L G C D | C = buka rekening unit (peserta × portofolio client yang sama); saldo hanya diubah oleh settle `member_portfolio_transaction`. |
| 00099 | `member_premium_payment` | `member-premium-payments` | TRX | L G C U* D* + aksi | Pembayaran iuran; terkait `invoice`. Aksi `/pay`. |
| 00109 | `member_portfolio_history` | `member-portfolio-histories` | HIST | L G | — |
| 00110 | `member_portfolio_transaction` | `member-portfolio-transactions` | TRX | L G C U* D* + aksi | Subscribe/redeem unit pada NAV efektif; `/settle` update `member_portfolio` + insert history, 1 transaksi; `/cancel`. |
| 00111 | `tax_report` | `tax-reports` | CHILD | L G C U D | Milik member atau dependant. |
| 00116 | `member_benefit_payout` | `member-benefit-payouts` | CHILD | L G C U D | Baris dalam `benefit_payout_batch`. |
| 00120 | `member_document` | `member-documents` | CHILD | L G C U D | Upload PDF → S3; nested di member. File: `document_file_uuid`. |

#### Akuntansi & Settlement — package `internal/module/accounting` (5 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00069 | `finance_account` | `finance-accounts` | MASTER | L G C U D | Chart of account (tree). Tidak bisa dihapus bila punya transaksi. |
| 00094 | `cash_settlement_category` | `cash-settlement-categories` | MASTER | L G C U D | — |
| 00108 | `cash_settlement` | `cash-settlements` | TRX | L G C U* D* + aksi | Upload dokumen (PDF → S3). Aksi `/approve` memposting jurnal (1 transaksi). File: `document_file_uuid`. |
| 00114 | `finance_transaction` | `finance-transactions` | TRX | L G C U* D* + aksi | Jurnal header + detail dibuat dalam 1 transaksi; validasi Σdebit = Σcredit. Jurnal posted immutable — koreksi via `/reverse`. |
| 00119 | `finance_transaction_detail` | `finance-transaction-details` | CHILD | L G C U D | Hanya lewat header (nested, read-only setelah posted). |

#### Manajemen Dokumen — package `internal/module/document` (4 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00042 | `document_category_authorization_table` | `document-category-authorizations` | CHILD | L G C U D | Hak per role untuk kategori dokumen. |
| 00113 | `document` | `documents` | MASTER | L G C U D | Upload PDF/dokumen → S3, download via presigned URL setelah cek otorisasi. Ganti file → insert `document_data_history` (1 transaksi). File: `document_file_id`. |
| 00117 | `document_authorization_table` | `document-authorizations` | CHILD | L G C U D | Hak read/create/update/delete/download per role untuk dokumen. |
| 00118 | `document_data_history` | `document-data-histories` | HIST | L G | File: `document_file_id`, `previous_document_file_id`. |

#### Task & Notifikasi — package `internal/module/workflow` (6 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00022 | `newsletter` | `newsletters` | MASTER | L G C U D | PDF & template email → S3. Aksi `/send` membuat baris `notification` (1 transaksi) lalu kirim async. File: `document_file_uuid`, `email_template_file_uuid`. |
| 00034 | `task` | `tasks` | MASTER | L G C U D | — |
| 00046 | `notification` | `notifications` | LOG | L G | Dibuat sistem. Aksi `/resend`. |
| 00059 | `notification_download_counter` | `notification-download-counters` | LOG | L G | Ditulis endpoint tracking publik (rate-limited, tanpa PII). |
| 00085 | `task_assignment` | `task-assignments` | CHILD | L G C U D | Nested `/tasks/{uuid}/assignments`. |
| 00122 | `task_document` | `task-documents` | CHILD | L G C U D | Nested `/tasks/{uuid}/documents`. |

#### Integrasi / Sinkronisasi — package `internal/module/integration` (2 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00032 | `synchronization_request` | `synchronization-requests` | LOG | L G | `POST` memicu sinkronisasi (async job), aksi `/retry`. |
| 00033 | `synchronized_data` | `synchronized-data` | LOG | L G | Payload `data_json` (JSONB). |

#### Storage — package `internal/module/storage` (1 tabel)

| Mig | Tabel | Base path `/api/v1/…` | Kelas | Operasi | Aturan khusus |
|---|---|---|---|---|---|
| 00002 | `file_object` | `files` | FILE | C G D | `POST /files/images` (lokal), `POST /files/documents` (S3), `GET /files/{uuid}`, `GET /files/{uuid}/content` (image), `GET /files/{uuid}/download` (302 → presigned S3), `DELETE`. |


### 10.6 Endpoint Non-CRUD

| Method | Path | Fungsi |
|---|---|---|
| `POST` | `/api/v1/auth/login` | Login (rate-limited); response: access token + refresh token |
| `POST` | `/api/v1/auth/refresh` | Rotasi refresh token |
| `POST` | `/api/v1/auth/logout` | Revoke refresh token saat ini |
| `POST` | `/api/v1/auth/logout-all` | Revoke semua sesi user |
| `POST` | `/api/v1/auth/activate` | Aktivasi akun / set password pertama (activation key sekali pakai) |
| `POST` | `/api/v1/auth/change-password` | Ganti password (wajib password lama) |
| `GET` | `/api/v1/me` | Profil dan permission user saat ini |
| `POST` | `/api/v1/portfolios/setup` | Buat portofolio + mapping akun kelas aset, akun kas per jenis transaksi, rekening bank dalam 1 transaksi |
| `GET` | `/api/v1/portfolio-overviews` | Daftar portofolio (parameter = `GET /portfolios`) + valuasi & alokasi terkini |
| `GET` | `/api/v1/portfolios/{uuid}/summary` | Ringkasan portofolio: nilai perolehan, nilai pasar, laba/rugi, alokasi per kelas aset |
| `GET` | `/api/v1/portfolios/{uuid}/holdings` | Posisi aktif semua kelas aset dengan harga terakhir dan bobot (`?asset_class=`) |
| `GET` | `/healthz` | Liveness |
| `GET` | `/readyz` | Readiness (ping DB + head bucket S3 + cek direktori image) |
| `GET` | `/metrics` | Prometheus (port internal terpisah, tidak diekspos publik) |

---

## 11. Validasi Input

### 11.1 Lapisan Validasi

| Lapisan | Lokasi | Isi |
|---|---|---|
| 1. Transport | Middleware / config Fiber | `Content-Type` wajib `application/json` (atau `multipart/form-data` untuk upload), `BodyLimit` 1 MB (upload: sesuai §8.1), header wajib (`If-Match`, `Idempotency-Key`) |
| 2. Decoding strict | `fiber.Config.JSONDecoder` | `DisallowUnknownFields`, tepat satu objek JSON (tidak ada trailing data), panjang array dibatasi |
| 3. Sintaksis | Tag `validate` pada DTO | required, panjang, format, enum, rentang |
| 4. Normalisasi | Method `Normalize()` pada DTO | Trim spasi, uppercase kode, ambil digit saja untuk NIK/NPWP/telepon |
| 5. Semantik / bisnis | Service | UUID relasi ada dan dalam scope, unik, transisi status, urutan tanggal, Σdebit = Σcredit, saldo cukup untuk jual, maker ≠ checker |
| 6. Constraint DB | PostgreSQL | FK, unique, check — jaring pengaman terakhir (§9.4) |

### 11.2 Setup Fiber

```go
app := fiber.New(fiber.Config{
    BodyLimit:       1 << 20, // 1 MB; route upload memakai limit sendiri
    JSONDecoder:     httpx.StrictJSONDecode, // DisallowUnknownFields + tolak trailing data
    StructValidator: validatorx.New(),       // dipanggil otomatis oleh c.Bind().Body()
    ErrorHandler:    httpx.ErrorHandler,     // memetakan semua error ke envelope §10.2
    ProxyHeader:     fiber.HeaderXForwardedFor,
    TrustProxy:      true,
    TrustProxyConfig: fiber.TrustProxyConfig{Proxies: cfg.TrustedProxies},
})
```

```go
func StrictJSONDecode(data []byte, v any) error {
    dec := json.NewDecoder(bytes.NewReader(data))
    dec.DisallowUnknownFields()
    if err := dec.Decode(v); err != nil {
        return apperr.BadRequest("JSON tidak valid", err)
    }
    if dec.More() {
        return apperr.BadRequest("body harus berisi satu objek JSON", nil)
    }
    return nil
}
```

### 11.3 Contoh DTO

```go
type CreateBankRequest struct {
    BankName         string `json:"bank_name"          validate:"required,max=255,safe_text"`
    BankClearingCode string `json:"bank_clearing_code" validate:"omitempty,numeric,len=3|len=7"`
    BankSwiftCode    string `json:"bank_swift_code"    validate:"omitempty,bic"`
    BankClassUUID    string `json:"bank_class_uuid"    validate:"required,uuid"`
    Description      string `json:"description"        validate:"omitempty,max=255,safe_text"`
}

type CreateStockTradeRequest struct {
    PortfolioUUID  string `json:"portfolio_uuid"   validate:"required,uuid"`
    StockUUID      string `json:"stock_uuid"       validate:"required,uuid"`
    BrokerUUID     string `json:"broker_uuid"      validate:"required,uuid"`
    TradeType      string `json:"trade_type"       validate:"required,oneof=BUY SELL"`
    StockTradeDate string `json:"stock_trade_date" validate:"required,datetime=2006-01-02"`
    SettlementDate string `json:"settlement_date"  validate:"required,datetime=2006-01-02"`
    Price          string `json:"price"            validate:"required,decimal=20.4,dgt=0"`
    QuantityLot    string `json:"quantity_lot"     validate:"required,decimal=24.8,dgt=0"`
}
// Validasi level struct: settlement_date >= stock_trade_date.
// Validasi service: portfolio & stock dalam scope, saldo lot cukup untuk SELL.
```

```go
func (h *BankHandler) Create(c fiber.Ctx) error {
    var req CreateBankRequest
    if err := c.Bind().Body(&req); err != nil { // decode strict + validate
        return err
    }
    req.Normalize()
    res, err := h.svc.Create(c.Context(), auth.ActorFrom(c), req)
    if err != nil {
        return err
    }
    c.Set(fiber.HeaderETag, httpx.ETag(res.UpdatedAt))
    c.Location("/api/v1/banks/" + res.UUID.String())
    return c.Status(fiber.StatusCreated).JSON(httpx.Data(c, res))
}
```

### 11.4 Validator Kustom

| Tag | Aturan | Dipakai di |
|---|---|---|
| `nik` | Tepat 16 digit (setelah normalisasi) | `identity_number` (member, investor, dependant, marketing_agent) |
| `npwp` | 15 digit (format lama) atau 16 digit (NIK/NPWP16) | `npwp`, `beneficiary_tax_number` |
| `phone_id` | `^(\+62\|62\|0)[0-9]{8,13}$` | `telephone`, `mobile_phone`, `handphone` |
| `bic` (bawaan) | SWIFT/BIC 8 atau 11 karakter | `bank_swift_code` |
| `decimal=P.S` | String desimal dengan total digit ≤ P dan skala ≤ S, sesuai tipe NUMERIC kolom | Semua kolom uang, rate, dan unit |
| `dgt=N`, `dgte=N`, `dlte=N` | Perbandingan desimal | Harga > 0, persen 0–100 |
| `code` | `^[A-Z0-9][A-Z0-9_.-]{0,44}$` | Semua kolom `*_code` |
| `stock_code` | `^[A-Z0-9]{1,6}$` | `stock_code` |
| `safe_text` | UTF-8 valid, tanpa karakter kontrol (kecuali `\n`, `\t` pada field TEXT) | Semua teks bebas |
| `number_template` | Hanya placeholder yang dikenal (`{bondCode}`, `{series}`, `{counter}`, …) | `*_number_template` |
| `cidr_list` | Setiap elemen berupa IP/CIDR valid | `broker_api_setting.ip_address_list` |
| `month` / `year` | 1–12 / 2000–2100 | Kolom `month`, `year` |
| `password` | Min 12 karakter, maks 128, bukan bagian dari username/email | Ganti/aktivasi password |

Pesan error validasi dikembalikan per field dalam Bahasa Indonesia (lewat translator validator). Nilai input **tidak** dipantulkan kembali di pesan error.

---

## 12. Keamanan

### 12.1 Autentikasi

| Kontrol | Spesifikasi |
|---|---|
| Access token | JWT EdDSA (Ed25519), TTL 15 menit; klaim: `sub` (UUID user), `role`, `client_uuid`, `jti`, `iat`, `exp`, `iss`, `aud`. Algoritma di-pin saat verifikasi (tolak `none`/HS*) |
| Refresh token | Opaque, 256-bit acak, TTL 7 hari; hanya `sha256(token)` yang disimpan di `auth_refresh_token`. **Rotasi setiap pemakaian**; reuse token lama → seluruh `family_id` di-revoke |
| Password | argon2id (`m=64 MiB, t=3, p=2`, salt 16 byte, disimpan dalam format PHC); parameter bisa diatur dan di-rehash otomatis saat login bila parameter berubah |
| Lockout | 5 kali gagal → `locked_until = now() + 15 menit`; respons login selalu generik ("username atau password salah") |
| Rate limit login | 10 req/menit per IP + 5 req/menit per username (Redis) |
| Activation key | Acak 256-bit, sekali pakai, TTL 24 jam, disimpan sebagai hash |
| Password legacy | Format hash lama tidak diketahui → semua user legacy dipaksa reset lewat alur aktivasi |

### 12.2 Otorisasi (RBAC)

- **Format permission.** Permission berupa `action.action_code` dengan format `<resource>:<verb>`, mis. `bond_trade:approve`. Role ↔ permission diatur lewat `role_action`. ADMIN memegang seluruh katalog secara implisit.
- **Katalog bawaan dari kode.** Katalog bawaan (`is_system`) berasal dari kode dan disinkronkan oleh `migrate seed`, beserta modul dan label resource-nya. Permission sistem tidak dapat diubah atau dihapus lewat API. Permission kustom (modul `custom`, mis. `dashboard_widget:view`) boleh dibuat untuk tombol atau widget khusus frontend; backend tidak pernah mewajibkannya.
- **Setiap route wajib permission.** `RequirePermission("bond_trade:approve")` dipasang di **setiap** route. Route tanpa permission, atau dengan permission yang tidak ada di katalog, gagal saat startup (fail closed). Respons 403 menyebut permission yang kurang di `error.details[].rule` dengan `field = "permission"`.
- **Role nonaktif tidak memberi izin.** Role berstatus nonaktif (atau terhapus) tidak memberi permission apa pun.
- **Cache dan versi.** Permission per role di-cache di memori (TTL 60 detik) dan langsung di-invalidate saat `role_action`, status role, atau katalog berubah. Setiap set punya versi (hash daftar kode) yang dikirim di header `X-Permissions-Version` pada setiap respons terautentikasi.
- **Anti-eskalasi.** Pengguna non-ADMIN hanya dapat:
  - memberi atau mencabut permission yang ia miliki;
  - mengubah, menghapus, atau mengatur permission role yang seluruh permission-nya ia miliki;
  - membuat, mengubah, me-reset, mengunci, atau menghapus user yang role-nya tidak melebihi dirinya;
  - menetapkan role seperti itu.

  Pengecekan memakai `role_action` mentah, termasuk milik role nonaktif. Role dan user ADMIN hanya dapat dikelola ADMIN.
- **PII.** PII penuh (NIK, NPWP, nomor rekening, tanggal lahir) hanya dapat dibaca dengan permission `pii:read`. Tanpa permission ini, nilai dimasking (mis. `3271********0001`), dan akses PII penuh dicatat di `activity_log`.

**Kontrak untuk frontend:**

| Kebutuhan | Endpoint / aturan |
|---|---|
| Bootstrap sesi | `GET /api/v1/me` → `user`, `role {uuid, code, name, menu_url, active}`, `is_admin`, `permissions[]` (ADMIN: seluruh katalog), `permissions_version` |
| Halaman / menu | Tampilkan bila user memegang `<resource>:read` (mis. `bond:read` untuk halaman Obligasi) |
| Tombol | Tampilkan bila user memegang kodenya: `bond:create`, `bond_trade:approve`, `invoice:generate`, … |
| Sinkronisasi | Bila `X-Permissions-Version` pada respons mana pun berbeda dari `permissions_version` yang disimpan, muat ulang `/me` |
| Editor role | `GET /api/v1/permission-catalog` (modul → resource → aksi; CRUD lebih dulu), `GET/PUT /api/v1/roles/{uuid}/actions` (`If-Match`) |
| Penolakan | 403 dengan `details[{field: "permission", rule: "<kode>"}]` |

Backend tetap sumber kebenaran: menyembunyikan tombol di frontend hanya soal UX, karena setiap request diperiksa ulang di server.

### 12.3 Isolasi Data (anti-IDOR)

- UUID **bukan** kontrol keamanan. Setiap akses objek tetap dicek kepemilikannya.
- `Actor.Scope()` dibentuk dari token (`client_id`, `role`, identitas member/agent) dan diteruskan ke repository. Query tabel ber-tenant (`client`, `portfolio` dan turunannya, `bond`, `invoice`, `cash_settlement`, `app_user`, …) selalu menambahkan filter scope.
- Akses di luar scope menghasilkan **404**, bukan 403.
- `MEMBER` dan `AGENT` hanya dapat mengakses data miliknya (relasi user ↔ member/agent: lihat §17).

### 12.4 Hardening HTTP (middleware Fiber)

Urutan middleware: `recover` → `requestid` → logger (dengan redaksi) → `helmet` (HSTS, `X-Content-Type-Options`, `X-Frame-Options: DENY`, `Referrer-Policy`, CSP `default-src 'none'`) → `cors` (allowlist origin dari env, tanpa wildcard bila memakai kredensial) → `limiter` (global 300 req/menit per user/IP) → auth JWT → RBAC → `timeout` (10 detik).

Tambahan:

- `TrustProxy` hanya untuk IP load balancer, agar IP yang tercatat di audit dan rate limit tidak bisa dipalsukan.
- TLS 1.2+ di load balancer/ingress. Koneksi DB produksi memakai `sslmode=verify-full`.
- Tidak ada stack trace atau pesan SQL di response.

### 12.5 Database

Empat database dengan role hak minimum (§7.1). `deploy/postgres/roles.sql` dijalankan sekali oleh superuser; `deploy/postgres/grants-<database>.sql` dijalankan oleh `pfm_migrator` setelah setiap migrasi (`make db-grants`).

| Role | Database | Hak |
|---|---|---|
| `pfm_migrator` | semua (owner) | DDL saat migrasi |
| `pfm_core_app` | `pfm_core` | SELECT/INSERT/UPDATE; `audit_outbox` INSERT saja |
| `pfm_security_app` | `pfm_security` | SELECT/INSERT/UPDATE; `audit_outbox` INSERT saja |
| `pfm_document_app` | `pfm_document` | SELECT/INSERT/UPDATE; `audit_outbox` INSERT saja |
| `pfm_log_reader` | `pfm_log` | SELECT `activity_log` (endpoint `/activity-logs`) |
| `pfm_log_writer` | `pfm_log` | INSERT `activity_log` saja (worker) |
| `pfm_worker` | core, security, document | SELECT/UPDATE/DELETE `audit_outbox` (klaim `FOR UPDATE SKIP LOCKED`); DELETE `file_object` pending dan `auth_refresh_token` kedaluwarsa |

`CONNECT` ke keempat database dicabut dari `PUBLIC`, dan `CREATE` pada schema `public` hanya untuk `pfm_migrator`. Tidak ada role runtime dengan DELETE pada data bisnis maupun UPDATE/DELETE pada `activity_log`.

Kredensial DB, kunci JWT, kredensial S3, dan kredensial proxy API (dulu di tabel `configuration`) disimpan di secret manager/env. Tidak ada yang di repo maupun di DB.

### 12.6 Audit Log

Setiap mutasi dan aksi sensitif (login, akses PII, download dokumen) ditulis ke `audit_outbox` di database asal, dalam transaksi yang sama. Worker lalu memindahkannya ke `pfm_log.activity_log` (§7.1), dengan kolom berikut:

- `username` — UUID user
- `role_code`
- `ip_address`
- `log_action` — mis. `bond_trade.approve`
- `log_title` — `<tabel>:<uuid>`
- `log_description` — ringkasan field yang berubah, **tanpa** nilai PII/secret
- `log_session_code` — `request_id`
- `log_time`
- `source` — database asal (`core`, `security`, `document`)

Kolom `action_query` **tidak** berisi SQL mentah; isinya route dan method.

### 12.6a Email Akun

- **Aktivasi user baru** (dibuat admin), **reset oleh admin**, dan **lupa password** (`POST /auth/forgot-password`, publik, rate limit auth) mengirim tautan ke email user: `<FRONTEND_BASE_URL>/activate?key=…` atau `/reset-password?key=…`. Halaman frontend menukar `key` lewat `POST /auth/activate`.
- **Kunci tautan.** Kuncinya acak 256-bit dan hanya hash-nya yang disimpan. Kunci sekali pakai, kedaluwarsa (aktivasi `ACTIVATION_KEY_TTL`, lupa password `PASSWORD_RESET_TTL`), dan semua sesi dicabut saat password diatur.
- **Lupa password.** Respons selalu sama (anti-enumerasi), password lama tetap berlaku sampai tautan dipakai, dan maksimal 1 email per menit per akun selama tautan belum dipakai.
- **`activation_key` hanya di luar production.** Kunci tidak dikembalikan API di production, sehingga admin tidak memegang kunci aktivasi milik user.
- **Pengiriman.** Email ditulis ke `mail_outbox` (migrasi 00131; ada di core, security, document) dalam transaksi bisnis yang sama. Worker mengirimnya lewat SMTP di luar transaksi (STARTTLS/TLS; `none` dilarang di production), dengan percobaan ulang bertahap maksimal 8 kali. Baris dihapus setelah terkirim; setelah gagal terakhir, isi email dikosongkan. Role aplikasi hanya boleh INSERT `mail_outbox`.

### 12.7 Supply Chain & Operasional

- CI menjalankan `golangci-lint` (termasuk `gosec`), `govulncheck`, `go test -race`, dan scan image container.
- Renovate/Dependabot untuk update dependency.
- Image `distroless` non-root, root filesystem read-only (kecuali volume image).
- Log JSON dengan redaksi otomatis untuk `Authorization`, `password`, `token`, `identity_number`, `npwp`, `bank_account_number`.

---

## 13. Kebutuhan Non-Fungsional

| Area | Kebutuhan |
|---|---|
| Performa | p95 < 300 ms (GET detail/list ≤ 100 item) pada 100 RPS; import CSV 10.000 baris < 30 detik |
| Ketersediaan | 99,5% bulanan; graceful shutdown 30 detik (selesaikan request dan transaksi berjalan) |
| Skalabilitas | API stateless (kecuali volume image); pool DB `max_conns` dapat dikonfigurasi |
| Observability | Log `slog` JSON dengan `request_id`; metrik Prometheus (latensi per route, error rate, pool DB, durasi transaksi); tracing OpenTelemetry (SHOULD) |
| Konfigurasi | Hanya lewat env (lihat §13.1); aplikasi menolak start bila konfigurasi tidak valid |
| Dokumentasi API | `api/openapi.yaml` (OpenAPI 3.1) dijaga sinkron; diverifikasi di CI terhadap route terdaftar |
| Zona waktu | DB dan API memakai UTC; konversi ke `Asia/Jakarta` dilakukan di klien/laporan |
| Backup | PostgreSQL PITR (WAL archiving), retensi 30 hari; S3 versioning; snapshot volume image harian |

### 13.1 Variabel Lingkungan

| Variabel | Contoh | Keterangan |
|---|---|---|
| `APP_ENV` | `production` | |
| `HTTP_ADDR` | `:8080` | |
| `METRICS_ADDR` | `:9090` | Internal |
| `DATABASE_URL_CORE` | `postgres://pfm_core_app@…/pfm_core?sslmode=verify-full` | Worker: `pfm_worker` |
| `DATABASE_URL_SECURITY` | `postgres://pfm_security_app@…/pfm_security?sslmode=verify-full` | Worker: `pfm_worker` |
| `DATABASE_URL_LOG` | `postgres://pfm_log_reader@…/pfm_log?sslmode=verify-full` | Worker: `pfm_log_writer` |
| `DATABASE_URL_DOCUMENT` | `postgres://pfm_document_app@…/pfm_document?sslmode=verify-full` | Worker: `pfm_worker` |
| `DB_MAX_CONNS` | `20` | Per database |
| `MIGRATION_DATABASE_URL_<CORE\|SECURITY\|LOG\|DOCUMENT>` | `postgres://pfm_migrator@…/pfm_<db>` | Hanya untuk `cmd/migrate` |
| `JWT_PRIVATE_KEY_FILE` / `JWT_PUBLIC_KEY_FILE` | `/run/secrets/jwt_ed25519` | |
| `JWT_ACCESS_TTL` / `JWT_REFRESH_TTL` | `15m` / `168h` | |
| `CORS_ALLOWED_ORIGINS` | `https://pfm.example.com` | |
| `TRUSTED_PROXIES` | `10.0.0.0/8` | |
| `REDIS_URL` | `redis://…` | |
| `STORAGE_IMAGE_ROOT` | `/var/lib/pfm/images` | |
| `S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT`, `S3_USE_PATH_STYLE`, `S3_KMS_KEY_ID` | | `S3_ENDPOINT` hanya untuk MinIO/dev |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `SMTP_TIMEOUT` | `sandbox.smtp.mailtrap.io`, `2525`, `starttls` | Hanya worker; wajib di production |
| `FRONTEND_BASE_URL` | `https://pfm.example.com` | Basis tautan email; https di production |
| `PASSWORD_RESET_TTL` | `1h` | Masa berlaku tautan lupa password (5m–24h) |
| `S3_PRESIGN_TTL` | `5m` | |
| `PROXY_API_URL`, `PROXY_API_USERNAME`, `PROXY_API_PASSWORD` | | Pindahan dari tabel `configuration` |
| `LOG_LEVEL` | `info` | |

---

## 14. Migrasi Data dari MySQL Legacy (ETL)

1. **Staging.** Restore dump ke MariaDB sementara (container), lalu baca dengan tool ETL Go (`cmd/etl`).
2. **Urutan.** Muat data per tabel mengikuti nomor migrasi (Lampiran A), sehingga induk selalu dimuat sebelum anak.
3. **Transformasi per kolom:**
   - `id` = PK legacy (dipertahankan); `uuid` = `unique_code` legacy bila berupa UUID valid, selain itu UUIDv7 baru.
   - Kolom audit = `COALESCE(snake_case, camelCase)`; `deleted_status = 1` → `deleted_at = COALESCE(deleted_time, migration_time)`.
   - `'0000-00-00'` → `NULL`; latin1 → UTF-8 (deteksi mojibake untuk karakter non-ASCII).
   - `double` → `NUMERIC` dibulatkan ke skala kolom; selisih pembulatan dilaporkan.
   - FK yang menunjuk ke baris yang tidak ada → `NULL` + dicatat di laporan (FK baru di §6.4 #3 bisa menemukan data yatim).
4. **File.** Setiap path `/usr/local/document/...` di kolom `*_url` diunggah ke S3 (atau ke storage lokal untuk image), lalu dibuatkan baris `file_object` (di `pfm_document`) dan kolom `*_file_id` / `*_file_uuid` diisi. Path yang filenya hilang dicatat di laporan.
5. **Duplikat kode.** Duplikat pada kolom yang memiliki partial unique index (§6.4 #13) dideteksi **sebelum** load, lalu diselesaikan manual atau diberi suffix sesuai keputusan bisnis.
6. **User.** `password_hash = NULL`; semua user menerima alur aktivasi. Kolom `token` legacy dibuang.
7. **Sequence.** Setelah load, jalankan `SELECT setval(pg_get_serial_sequence('<t>', 'id'), COALESCE(max(id), 1)) FROM <t>;` untuk setiap tabel.
8. **Rekonsiliasi.** Untuk setiap tabel: jumlah baris, Σ kolom uang utama (mis. `bond_trade.nominal`, `finance_transaction_detail.debit/credit`), dan sampel 1% baris dibandingkan dengan legacy. Hasilnya menjadi laporan wajib sebelum cutover.

---

## 15. Strategi Testing

| Jenis | Cakupan | Tool |
|---|---|---|
| Unit | Service (aturan bisnis, transisi status, kalkulasi posisi/jurnal) dengan repository mock dan TxManager fake | `testing`, `testify`, `mockery` |
| Integrasi repository | Query sqlc terhadap PostgreSQL nyata setelah `goose up`; soft delete, optimistic lock, filter scope | `testcontainers-go` |
| Transaksi | Kegagalan di tengah proses (mis. gagal di langkah jurnal) → **tidak ada** baris parsial (posisi, history, jurnal tetap utuh) | `testcontainers-go` |
| Konkurensi | Dua approve paralel pada `bond_trade` yang sama → satu sukses, satu 409/412; counter sertifikat tanpa duplikat | Goroutine + DB nyata |
| Migrasi | `up → down-to 0 → up`; cek uuid/index FK | CI |
| API | Handler via `app.Test()`: validasi (field tak dikenal, tipe salah, batas), envelope, kode HTTP | Fiber |
| Otorisasi | Matriks endpoint × role × tenant (cross-client → 404) | Table-driven test |
| Upload | File polyglot, ekstensi palsu, oversize, decompression bomb, PDF berisi JavaScript, nama file traversal | Fixture |
| Keamanan | `gosec`, `govulncheck`, OWASP ZAP baseline di staging, pentest sebelum go-live | CI / manual |

---

## 16. Rencana Rilis

| Milestone | Isi | Kriteria selesai |
|---|---|---|
| **M0 — Fondasi** | Skeleton, config, logger, pgxpool, TxManager, error handler, validator, migrasi 00001–00002 + 00123, storage lokal/S3, IAM (`action`, `role`, `role_action`, `app_user`, `activity_log`), auth, CI | Login/refresh/logout jalan; CI hijau; upload/download file teruji |
| **M1 — Master Data** | 25 tabel master data + generator scaffold | CRUD + test authz semua tabel modul |
| **M2 — Instrumen & Portofolio** | Modul `market` & `portfolio` | Import time-series; posisi read-only |
| **M3 — Trading & Akuntansi** | `stock_trade`, `mutual_fund_trade`, `crypto_trade`, jurnal | Settle → posisi + history + jurnal atomik; jurnal seimbang |
| **M4 — Obligasi** | `bond`, `bond_trade`, kupon, investor, agen, maker-checker | Approve trade + nomor sertifikat + jadwal kupon atomik; uji konkurensi lulus |
| **M5 — Dana Pensiun** | Member, premi, invoice, benefit payout | Generate invoice & payout batch idempoten |
| **M6 — Dokumen & Workflow** | Dokumen + otorisasi, task, newsletter/notifikasi, sinkronisasi | Download dokumen sesuai `document_authorization_table` |
| **M7 — ETL & Go-live** | ETL data + file legacy, hardening, pentest, cutover | Laporan rekonsiliasi disetujui; 0 temuan High/Critical |

---

## 17. Asumsi & Pertanyaan Terbuka

| # | Topik | Asumsi saat ini | Perlu konfirmasi |
|---|---|---|---|
| 1 | Arti kode status `SMALLINT` (mis. `bond_trade_status`, `coupon_status`, `invoice_status`, `status`) | Dipertahankan apa adanya; enum string di API didefinisikan setelah mapping dikonfirmasi | Daftar nilai dan arti setiap kode per tabel |
| 2 | `deleted_status` legacy | `0` = aktif, `≠ 0` = terhapus | Benar? |
| 3 | Kolom `*_id` tanpa tabel referensi: `bond.bond_classification_id`, `bond.remote_product_id`, `bond_trade.remote_subscription_id`, `member.region_id`, `portfolio_bank.currency_type_id`, `finance_transaction.deposito_trade_id` | Disimpan `BIGINT` tanpa FK | Apakah perlu tabel referensi baru, atau kolomnya dihapus? |
| 4 | Kolom `[USULAN]` pada `crypto`, `crypto_exchange`, `crypto_portfolio`, `property_asset` | Ditambahkan agar tabel dapat dipakai (legacy tidak punya nama/kode/relasi) | Setujui atau hapus dari DDL |
| 5 | Relasi `app_user` ↔ `member` / `marketing_agent` / `investor` untuk role `MEMBER` dan `AGENT` | Belum ada di legacy. Usulan: migrasi `00124` menambah `member_id`, `marketing_agent_id`, `investor_id` (nullable, FK) di `app_user` | Bagaimana legacy memetakan login ke member/agent? |
| 6 | `portfolio_cost_structure` punya `cost_structure_status` **dan** `status` | Keduanya dipertahankan | Mana yang dipakai? |
| 7 | `portfolio_status` (varchar, mis. `inactive`) | Tetap `VARCHAR(45)` | Boleh diubah ke enum/SMALLINT? |
| 8 | Image lokal pada deployment multi-instance | Volume shared atau satu instance untuk upload | Topologi deploy target? |
| 9 | Penyedia S3 | AWS S3 (dev: MinIO) | Region/bucket dan KMS key? |
| 10 | Template cetak (`*_print_template`, `trade_confirmation_template`) | Hanya disimpan sebagai string; rendering di luar v1 | Kapan dibutuhkan, dan formatnya? |
| 11 | Unique index kode (§6.4 #13) | Ditegakkan; data duplikat diselesaikan saat ETL | Apakah ada kode yang memang boleh duplikat? |
| 12 | Satuan `tenor` deposito | Bulan; rollover menghitung tanggal akhir = mulai + tenor bulan | Benar? |
| 13 | Total detail placement | Σ `placement_detail.placement_value` ≤ `placement.placement_value`; tanggal detail di dalam periode placement | Benar? |
| 14 | Jurnal deposito & placement | Rollover/withdraw belum memposting jurnal; ditambahkan bersama modul akuntansi (M3) | — |
| 15 | Tabel snapshot riwayat (`*_history` audit, `*_data_history`) | Tidak menghalangi soft delete data sumbernya; data time-series (harga, NAV) tetap menghalangi | Setuju? |
| 16 | Kode status transaksi | `0` draft, `1` settled/approved/posted, `2` cancelled, `3` reversed (API: string). `crypto_trade.status` memakai teks yang sama | Cocok dengan arti status legacy? |
| 17 | Mapping akun trade | `asset_class_finance_account`: `buy_account` = akun investasi (`sell_account` bila diisi dipakai saat jual), `capital_gain/loss_account` untuk laba/rugi; `portfolio_accounting_structure` (kelas aset × BUY/SELL) = akun kas | Sesuai praktik akuntansi dana? |
| 18 | Metode posisi | Rata-rata tertimbang; fee beli masuk biaya perolehan, fee jual mengurangi hasil; 1 lot saham = 100 lembar | Benar? |
| 19 | Urutan settle/reverse | Settle wajib berurutan per tanggal per posisi; reverse hanya trade ter-settle terakhir (LIFO) | Perlu backdate? |
| 20 | `crypto_trade.portfolio_id` [USULAN] | Ditambahkan (migrasi 00125) karena posisi kripto butuh portofolio | Setuju? |
| 21 | Maker-checker | Diterapkan pada approve cash settlement, approve subscription obligasi, dan perubahan data investor/agen; settle trade saham/RD/kripto belum | Perlu juga untuk trade? |
| 22 | Jurnal deposito | Penempatan/rollover/withdraw deposito belum memposting jurnal (perlu keputusan perlakuan bunga) | Perlakuan akuntansi bunga deposito? |
| 23 | `Idempotency-Key` | Belum diimplementasi; aksi TRX dilindungi `If-Match` (kirim ulang mendapat 412) | Tetap perlu Redis idempotency? |
| 24 | Kode status obligasi (M4) | `bond_status` 0 draft, 1 offering, 2 active, 3 matured; `bond_trade_status` mengikuti data legacy: 0 draft/pending, 1 approved, -1 cancelled; kupon 0 belum/1 sudah dibayar; fee agen 0 draft, 1 paid, 2 cancelled; change request 0 pending, 1 approved, 2 rejected | Cocok dengan arti status legacy? |
| 25 | Jadwal kupon | Kupon tiap `coupon_payment_periode` hari sejak tanggal terbit, periode terakhir dipotong di jatuh tempo; tarif = kupon × hari / 360 (dibulatkan 6 desimal); pokok 100% (`transaction_type` 2) saat jatuh tempo; `transaction_type` 1 = kupon | Basis hari 360 atau Actual/365? Arti `transaction_type` 3? |
| 26 | Nomor sertifikat | Counter per obligasi (`bond.latest_certificate_counter`, dikunci baris); template `bond_periode_option.bond_number_template` → `configuration.certificate_number_template` → `{bondCode}-{series}{counter}` (counter 7 digit) | Format resmi nomor sertifikat? |
| 27 | Scope investor & agen | `investor`, `marketing_agent`, `bank_account` belum di-scope per client (global); obligasi & turunannya di-scope lewat `bond.client_id` | Apakah investor/agen milik client tertentu? |
| 28 | Perubahan data investor/agen | Pengajuan = penggantian penuh data pribadi; satu pengajuan pending per data; approve ditolak bila data diubah langsung setelah pengajuan; `leader_agent_id`/`agent_level` hanya diubah ADMIN | Benar? |
| 29 | Pembayaran kupon & fee | Bayar kupon belum memposting jurnal; fee agen dibayar sekaligus (`outstanding_value` dikurangi `paid_value`) | Perlu jurnal & pembayaran sebagian? |
| 30 | Jatuh tempo obligasi | Status `matured` belum diset otomatis (rencana: worker M7 setelah pokok dibayar) | Kapan obligasi dianggap matured? |
| 31 | `member.client_id` [USULAN] (M5) | Ditambahkan (migrasi 00127): peserta milik satu client; dasar scope dan penagihan per client. Client peserta tidak dapat dipindah | Bagaimana legacy memetakan peserta ke pemberi kerja? Perlu relasi ke `member_group`? |
| 32 | Kode status peserta & dana pensiun | `member.status` 0 nonaktif, 1 aktif (ditagih iuran), 2 pensiun (menerima manfaat); invoice 0 draft, 1 issued, 2 paid, 3 cancelled; iuran 0 belum/1 sudah dibayar; payout 0 draft, 1 approved, 2 paid, 3 cancelled | Cocok dengan arti status legacy? |
| 33 | Nominal iuran | Baris invoice = `member.latest_pension_premium` (seluruhnya iuran peserta; `allowance_payment_nominal` 0; `percentage` = iuran / gaji × 100); peserta ditagih bila aktif di bulan tsb (`start_working_date` ≤ akhir bulan, `retired_date` ≥ awal bulan); pajak invoice diisi manual | Rumus resmi iuran (porsi pemberi kerja vs peserta, `premium_payment_type`)? |
| 34 | Penomoran M5 | Invoice `NN/DP/MM/YYYY` dan batch `NN/PAYMENT/MM/YYYY` (pola data legacy; NN berurutan per bulan), peserta `MBR000001`; terbilang dalam Title Case + "Rupiah" | Format nomor peserta resmi? |
| 35 | Penerima manfaat | Peserta status pensiun dengan `pension_benefit_value` > 0 dan setiap tanggungan dengan `pension_benefit_value` > 0; pajak (PPh) diisi manual per baris sebelum approve | Aturan ahli waris & perhitungan PPh 21 manfaat pensiun? |
| 36 | Jurnal M5 | Pembayaran iuran, payout, dan settle unit peserta belum memposting jurnal; unit peserta tidak terhubung ke posisi portofolio | Perlakuan akuntansi dana pensiun? |
| 37 | Job bulanan | Generate invoice/payout dipanggil lewat API (idempoten); penjadwalan otomatis menyusul di worker (M7) | Tanggal eksekusi job? |
| 38 | Pemisahan 4 database | Integritas lintas database dijaga aplikasi (tanpa FK/transaksi lintas DB). Backup/restore keempat database harus pada titik waktu yang sama, atau referensi UUID direkonsiliasi setelah restore | Strategi backup & PITR per database? |
| 39 | Log eventual | `/activity-logs` tertinggal ±5 detik (worker); bila worker mati, event menumpuk di outbox (tidak hilang) | Perlu alert jumlah outbox? |
| 40 | Integritas event audit | Kredensial aplikasi dapat menambah event ke outbox, tetapi tidak dapat membaca, mengubah, atau menghapus log | Perlu tanda tangan event (HMAC) atau WORM storage? |
| 41 | Topologi production | 4 database boleh satu cluster di dev; production disarankan `pfm_log` dan `pfm_security` di instance terpisah | Topologi final? |
| 42 | Peta UI ↔ permission | Halaman memakai `<resource>:read`, tombol memakai kode aksinya; peta rute frontend → permission didefinisikan di frontend (`dashboard-nav`) | Perlu menu dinamis dari backend (`role.menu_url`)? |
| 43 | Anti-eskalasi | Non-ADMIN tidak dapat mengelola role/user yang permission-nya melebihi dirinya, termasuk mencabut permission yang tidak ia miliki | Setuju, atau perlu peran "security officer" khusus? |
| 44 | Email akun | Aktivasi, reset admin dan lupa password lewat email; tidak ada pendaftaran mandiri (user dibuat admin). Notifikasi "password diubah" belum dikirim | Perlu pendaftaran mandiri atau notifikasi perubahan password? |

---

## Lampiran A — Urutan Migrasi per Tabel

133 file migrasi dalam 6 folder: `shared/` (1, fungsi bersama), `outbox/` (2), `core/` (116), `security/` (7), `log/` (2), dan `document/` (5). Isinya: 1 init, 120 tabel legacy, 2 tabel baru, migrasi pendukung M2–M5, outbox audit (00128), kolom `source` log (00129), metadata katalog permission (00130), antrean email (00131), waktu terbit kunci aktivasi (00132) dan view valuasi `portfolio_holding` (00133). Kolom "Bergantung pada" berisi tabel yang di-referensikan FK di database yang sama, dan nomornya selalu lebih kecil. Referensi ke database lain ditandai *(UUID, pfm_x)* dan tidak memakai FK.

| No | File migrasi | Tabel | Modul | Bergantung pada |
|---|---|---|---|---|
| 00001 | `shared/00001_init_functions.sql` | — (fungsi `set_updated_at`) | — | — |
| 00002 | `document/00002_create_file_object.sql` | `file_object` | storage | — |
| 00003 | `security/00003_create_action.sql` | `action` | iam | — |
| 00004 | `log/00004_create_activity_log.sql` | `activity_log` | iam | — |
| 00005 | `core/00005_create_asset_class.sql` | `asset_class` | masterdata | — |
| 00006 | `core/00006_create_bank_class.sql` | `bank_class` | masterdata | — |
| 00007 | `core/00007_create_benefit_payout_batch.sql` | `benefit_payout_batch` | pension | — |
| 00008 | `core/00008_create_bond_periode_option.sql` | `bond_periode_option` | masterdata | — |
| 00009 | `core/00009_create_bond_type.sql` | `bond_type` | masterdata | — |
| 00010 | `core/00010_create_broker.sql` | `broker` | market | — |
| 00011 | `core/00011_create_configuration.sql` | `configuration` | masterdata | file_object *(UUID, pfm_document)* |
| 00012 | `core/00012_create_country.sql` | `country` | masterdata | — |
| 00013 | `core/00013_create_crypto.sql` | `crypto` | market | — |
| 00014 | `core/00014_create_crypto_exchange.sql` | `crypto_exchange` | market | — |
| 00015 | `core/00015_create_document_category.sql` | `document_category` | masterdata | — |
| 00016 | `core/00016_create_finance_account_type.sql` | `finance_account_type` | masterdata | — |
| 00017 | `core/00017_create_indices.sql` | `indices` | market | — |
| 00018 | `core/00018_create_investment_manager.sql` | `investment_manager` | market | — |
| 00019 | `core/00019_create_investor_category.sql` | `investor_category` | masterdata | — |
| 00020 | `core/00020_create_marketing_agent.sql` | `marketing_agent` | bond | — |
| 00021 | `core/00021_create_mutual_fund_category.sql` | `mutual_fund_category` | masterdata | — |
| 00022 | `core/00022_create_newsletter.sql` | `newsletter` | workflow | file_object *(UUID, pfm_document)* |
| 00023 | `core/00023_create_occupation.sql` | `occupation` | masterdata | — |
| 00024 | `core/00024_create_placement_type.sql` | `placement_type` | masterdata | — |
| 00025 | `core/00025_create_premium_payment_type.sql` | `premium_payment_type` | masterdata | — |
| 00026 | `core/00026_create_property_asset_type.sql` | `property_asset_type` | masterdata | — |
| 00027 | `core/00027_create_rating_agency.sql` | `rating_agency` | masterdata | — |
| 00028 | `security/00028_create_role.sql` | `role` | iam | — |
| 00029 | `core/00029_create_sector.sql` | `sector` | masterdata | — |
| 00030 | `core/00030_create_stock_size_classification.sql` | `stock_size_classification` | masterdata | — |
| 00031 | `core/00031_create_stock_type.sql` | `stock_type` | masterdata | — |
| 00032 | `core/00032_create_synchronization_request.sql` | `synchronization_request` | integration | — |
| 00033 | `core/00033_create_synchronized_data.sql` | `synchronized_data` | integration | — |
| 00034 | `core/00034_create_task.sql` | `task` | workflow | — |
| 00035 | `core/00035_create_tax_rate.sql` | `tax_rate` | masterdata | — |
| 00036 | `core/00036_create_trade_type.sql` | `trade_type` | masterdata | — |
| 00037 | `core/00037_create_bank.sql` | `bank` | masterdata | bank_class |
| 00038 | `core/00038_create_broker_api_setting.sql` | `broker_api_setting` | market | broker |
| 00039 | `core/00039_create_client.sql` | `client` | portfolio | sector |
| 00040 | `core/00040_create_crypto_listing_exchange.sql` | `crypto_listing_exchange` | market | crypto_exchange |
| 00041 | `core/00041_create_crypto_trade_plan.sql` | `crypto_trade_plan` | portfolio | crypto, crypto_exchange |
| 00042 | `document/00042_create_document_category_authorization_table.sql` | `document_category_authorization_table` | document | document_category *(UUID, pfm_core)*, role *(UUID, pfm_security)* |
| 00043 | `core/00043_create_indices_history.sql` | `indices_history` | market | indices |
| 00044 | `core/00044_create_marketing_agent_data_history.sql` | `marketing_agent_data_history` | bond | marketing_agent |
| 00045 | `core/00045_create_mutual_fund.sql` | `mutual_fund` | market | investment_manager, mutual_fund_category |
| 00046 | `core/00046_create_notification.sql` | `notification` | workflow | newsletter |
| 00047 | `core/00047_create_placement.sql` | `placement` | portfolio | investment_manager |
| 00048 | `core/00048_create_property_asset.sql` | `property_asset` | portfolio | property_asset_type |
| 00049 | `core/00049_create_province.sql` | `province` | masterdata | country |
| 00050 | `core/00050_create_rating.sql` | `rating` | masterdata | rating_agency |
| 00051 | `security/00051_create_role_action.sql` | `role_action` | iam | action, role |
| 00052 | `core/00052_create_sub_sector.sql` | `sub_sector` | masterdata | sector |
| 00053 | `core/00053_create_city.sql` | `city` | masterdata | province |
| 00054 | `core/00054_create_crypto_trade.sql` | `crypto_trade` | trading | crypto, crypto_trade_plan, trade_type |
| 00055 | `core/00055_create_deposito.sql` | `deposito` | market | bank |
| 00056 | `core/00056_create_emitent.sql` | `emitent` | market | sector, sub_sector |
| 00057 | `core/00057_create_invoice.sql` | `invoice` | pension | client |
| 00058 | `core/00058_create_mutual_fund_history.sql` | `mutual_fund_history` | market | mutual_fund |
| 00059 | `core/00059_create_notification_download_counter.sql` | `notification_download_counter` | workflow | notification |
| 00060 | `core/00060_create_placement_detail.sql` | `placement_detail` | portfolio | placement |
| 00061 | `core/00061_create_portfolio.sql` | `portfolio` | portfolio | client, investment_manager |
| 00062 | `core/00062_create_stock.sql` | `stock` | market | sector, stock_type, sub_sector |
| 00063 | `security/00063_create_app_user.sql` | `app_user` | iam | client *(UUID, pfm_core)*, role |
| 00064 | `core/00064_create_bond.sql` | `bond` | bond | bond_periode_option, bond_type, client, emitent, rating, sector |
| 00065 | `core/00065_create_crypto_portfolio.sql` | `crypto_portfolio` | portfolio | crypto, portfolio |
| 00066 | `core/00066_create_deposito_history.sql` | `deposito_history` | market | deposito |
| 00067 | `core/00067_create_deposito_interest_history.sql` | `deposito_interest_history` | market | deposito |
| 00068 | `core/00068_create_deposito_portfolio.sql` | `deposito_portfolio` | portfolio | deposito, portfolio |
| 00069 | `core/00069_create_finance_account.sql` | `finance_account` | accounting | finance_account_type, portfolio |
| 00070 | `core/00070_create_indices_stock.sql` | `indices_stock` | market | indices, stock |
| 00071 | `core/00071_create_investor.sql` | `investor` | bond | city, country, investor_category, province |
| 00072 | `core/00072_create_member.sql` | `member` | pension | bank, city, country, occupation, province |
| 00073 | `core/00073_create_member_group.sql` | `member_group` | pension | city, country, province |
| 00074 | `core/00074_create_mutual_fund_portfolio.sql` | `mutual_fund_portfolio` | portfolio | mutual_fund, portfolio |
| 00075 | `core/00075_create_mutual_fund_trade.sql` | `mutual_fund_trade` | trading | mutual_fund, portfolio, trade_type |
| 00076 | `core/00076_create_portfolio_bank.sql` | `portfolio_bank` | portfolio | bank, portfolio |
| 00077 | `core/00077_create_portfolio_budget.sql` | `portfolio_budget` | portfolio | asset_class, portfolio |
| 00078 | `core/00078_create_portfolio_note.sql` | `portfolio_note` | portfolio | portfolio |
| 00079 | `core/00079_create_stock_beta_history.sql` | `stock_beta_history` | market | rating_agency, stock |
| 00080 | `core/00080_create_stock_fundamental_history.sql` | `stock_fundamental_history` | market | stock |
| 00081 | `core/00081_create_stock_portfolio.sql` | `stock_portfolio` | portfolio | portfolio, stock |
| 00082 | `core/00082_create_stock_size_classification_history.sql` | `stock_size_classification_history` | market | stock, stock_size_classification |
| 00083 | `core/00083_create_stock_trade.sql` | `stock_trade` | trading | broker, investment_manager, portfolio, stock, trade_type |
| 00084 | `core/00084_create_stock_type_detail.sql` | `stock_type_detail` | market | stock, stock_type |
| 00085 | `core/00085_create_task_assignment.sql` | `task_assignment` | workflow | app_user *(UUID, pfm_security)*, task |
| 00086 | `core/00086_create_agent_fee_transaction.sql` | `agent_fee_transaction` | bond | bond, marketing_agent, file_object *(UUID, pfm_document)* |
| 00087 | `core/00087_create_asset_class_finance_account.sql` | `asset_class_finance_account` | portfolio | asset_class, finance_account, portfolio |
| 00088 | `core/00088_create_bank_account.sql` | `bank_account` | bond | investor, marketing_agent |
| 00089 | `core/00089_create_bond_coupon_schedule.sql` | `bond_coupon_schedule` | bond | bond |
| 00090 | `core/00090_create_bond_history.sql` | `bond_history` | bond | bond, rating |
| 00091 | `core/00091_create_bond_periode_distribution.sql` | `bond_periode_distribution` | bond | bond |
| 00092 | `core/00092_create_bond_portfolio.sql` | `bond_portfolio` | portfolio | bond, portfolio, rating |
| 00093 | `core/00093_create_bond_rating.sql` | `bond_rating` | bond | bond, rating, rating_agency |
| 00094 | `core/00094_create_cash_settlement_category.sql` | `cash_settlement_category` | accounting | finance_account |
| 00095 | `core/00095_create_dependant.sql` | `dependant` | pension | member |
| 00096 | `core/00096_create_deposito_portfolio_history.sql` | `deposito_portfolio_history` | portfolio | deposito_portfolio |
| 00097 | `core/00097_create_investor_data_history.sql` | `investor_data_history` | bond | city, country, investor, investor_category, province |
| 00098 | `core/00098_create_member_portfolio.sql` | `member_portfolio` | pension | member, portfolio |
| 00099 | `core/00099_create_member_premium_payment.sql` | `member_premium_payment` | pension | invoice, member, premium_payment_type |
| 00100 | `core/00100_create_mutual_fund_portfolio_history.sql` | `mutual_fund_portfolio_history` | portfolio | mutual_fund_portfolio |
| 00101 | `core/00101_create_portfolio_accounting_structure.sql` | `portfolio_accounting_structure` | portfolio | asset_class, finance_account, finance_account_type, portfolio, trade_type |
| 00102 | `core/00102_create_portfolio_budget_history.sql` | `portfolio_budget_history` | portfolio | asset_class, portfolio_budget |
| 00103 | `core/00103_create_portfolio_cost_structure.sql` | `portfolio_cost_structure` | portfolio | finance_account, portfolio, trade_type |
| 00104 | `core/00104_create_stock_history.sql` | `stock_history` | market | stock, stock_fundamental_history, stock_type |
| 00105 | `core/00105_create_stock_portfolio_history.sql` | `stock_portfolio_history` | portfolio | stock_portfolio |
| 00106 | `core/00106_create_bond_portfolio_history.sql` | `bond_portfolio_history` | portfolio | bond, bond_portfolio, portfolio |
| 00107 | `core/00107_create_bond_trade.sql` | `bond_trade` | bond | bank, bank_account, bond, bond_periode_option, broker, investor, marketing_agent, portfolio, trade_type |
| 00108 | `core/00108_create_cash_settlement.sql` | `cash_settlement` | accounting | bond_portfolio, cash_settlement_category, client, deposito_portfolio, member, mutual_fund_portfolio, stock_portfolio, file_object *(UUID, pfm_document)* |
| 00109 | `core/00109_create_member_portfolio_history.sql` | `member_portfolio_history` | pension | member_portfolio |
| 00110 | `core/00110_create_member_portfolio_transaction.sql` | `member_portfolio_transaction` | pension | member_portfolio, trade_type |
| 00111 | `core/00111_create_tax_report.sql` | `tax_report` | pension | dependant, member |
| 00112 | `core/00112_create_bond_trade_data_history.sql` | `bond_trade_data_history` | bond | bank, bond, bond_trade, broker, investor, marketing_agent, portfolio, trade_type |
| 00113 | `document/00113_create_document.sql` | `document` | document | bond *(UUID, pfm_core)*, bond_trade *(UUID, pfm_core)*, document_category *(UUID, pfm_core)*, investor *(UUID, pfm_core)*, marketing_agent *(UUID, pfm_core)*, mutual_fund_trade *(UUID, pfm_core)*, stock_trade *(UUID, pfm_core)*, file_object |
| 00114 | `core/00114_create_finance_transaction.sql` | `finance_transaction` | accounting | bond_trade, mutual_fund_trade, portfolio, stock_trade |
| 00115 | `core/00115_create_investor_coupon_schedule.sql` | `investor_coupon_schedule` | bond | bond_coupon_schedule, bond_trade, file_object *(UUID, pfm_document)* |
| 00116 | `core/00116_create_member_benefit_payout.sql` | `member_benefit_payout` | pension | benefit_payout_batch, dependant, member, tax_report |
| 00117 | `document/00117_create_document_authorization_table.sql` | `document_authorization_table` | document | document, role *(UUID, pfm_security)* |
| 00118 | `document/00118_create_document_data_history.sql` | `document_data_history` | document | bond_trade *(UUID, pfm_core)*, document, document_category *(UUID, pfm_core)*, investor *(UUID, pfm_core)*, marketing_agent *(UUID, pfm_core)*, mutual_fund_trade *(UUID, pfm_core)*, stock_trade *(UUID, pfm_core)*, file_object |
| 00119 | `core/00119_create_finance_transaction_detail.sql` | `finance_transaction_detail` | accounting | finance_account, finance_transaction |
| 00120 | `core/00120_create_member_document.sql` | `member_document` | pension | document_category, member, member_benefit_payout, member_premium_payment, file_object *(UUID, pfm_document)* |
| 00121 | `core/00121_create_payment_document.sql` | `payment_document` | bond | bond_coupon_schedule, document_category, investor_coupon_schedule, file_object *(UUID, pfm_document)* |
| 00122 | `core/00122_create_task_document.sql` | `task_document` | workflow | document *(UUID, pfm_document)*, document_category, task |
| 00123 | `security/00123_create_auth_refresh_token.sql` | `auth_refresh_token` | iam | app_user |
| 00124 | `core/00124_create_business_unique_indexes.sql` | — (unique index bisnis M2) | market, portfolio | stock_history, indices_history, mutual_fund_history, … |
| 00125 | `core/00125_trading_accounting_support.sql` | `number_sequence` + kolom/index pendukung M3 | accounting, trading | crypto_trade, finance_transaction, cash_settlement, … |
| 00126 | `core/00126_bond_support.sql` | default status, unique sertifikat & kupon investor, CHECK pemilik rekening (M4) | bond | bond, bond_trade, bond_coupon_schedule, investor_coupon_schedule, bank_account, … |
| 00127 | `core/00127_pension_support.sql` | `member.client_id` [USULAN], default status, unique periode invoice/payout & baris per peserta (M5) | pension | member, client, invoice, member_premium_payment, benefit_payout_batch, member_benefit_payout, member_portfolio |
| 00128 | `outbox/00128_create_audit_outbox.sql` | `audit_outbox` | audit | — |
| 00129 | `log/00129_activity_log_source.sql` | kolom `activity_log.source` + index | audit | activity_log |
| 00130 | `security/00130_action_catalog_metadata.sql` | kolom `action.module`, `resource_label`, `is_system` | iam | action |
| 00131 | `outbox/00131_create_mail_outbox.sql` | `mail_outbox` | mail | — |
| 00132 | `security/00132_app_user_activation_issued_at.sql` | kolom `app_user.activation_key_issued_at` | iam | app_user |
| 00133 | `core/00133_create_portfolio_holding_view.sql` | view `portfolio_holding` (kepemilikan aktif + valuasi semua kelas aset) | portfolio | stock_portfolio, mutual_fund_portfolio, crypto_portfolio, bond_portfolio, deposito_portfolio, stock, mutual_fund, crypto, bond, deposito, stock_trade, crypto_trade |


## Lampiran B — DDL PostgreSQL Lengkap

Lihat [`prd-schema-postgres.sql`](prd-schema-postgres.sql). Setiap blok diawali penanda `-- ===================== 000NN_create_<tabel>.sql =====================`; isi di bawah penanda tersebut adalah isi file migrasi goose dan dapat dipecah otomatis:

```bash
mkdir -p internal/db/migrations
awk '/^-- ===================== [0-9]+_.*\.sql =====================$/{f="internal/db/migrations/"$3; next} f{print > f}' prd-schema-postgres.sql
```

Status validasi: seluruh blok dijalankan berurutan pada PostgreSQL 14 (`up` → `down` semua → `up`) tanpa error. Hasil akhirnya 122 tabel, 238 foreign key (semuanya ber-index), 122 kolom `uuid`, dan 120 trigger `updated_at`.
