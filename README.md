# PFM Backend

REST API Portfolio & Fund Management: Go + Fiber v3 + PostgreSQL. Spesifikasi lengkap ada di [prd.md](prd.md).

Status: **M0 (Fondasi), M1 (Master Data, 25 tabel), M2 (Instrumen Pasar & Portofolio, 43 tabel) selesai.** M3–M7 belum.

## Prasyarat

- Go 1.26 (toolchain `go1.26.11` diunduh otomatis lewat `go.mod`)
- PostgreSQL ≥ 14 dan S3/MinIO — atau Docker untuk `make dev-up`
- `sqlc` 1.29, `golangci-lint` v2, `govulncheck`, `openssl`

## Menjalankan secara lokal

```bash
cp .env.example .env          # isi ADMIN_PASSWORD (min 12 karakter)
make keys                     # kunci JWT Ed25519 -> secrets/jwt_ed25519.pem
make dev-up                   # PostgreSQL 17 + MinIO (bucket pfm-documents)
make migrate-up               # 123 migrasi, dijalankan sebagai pfm_migrator
make db-grants                # hak akses minimum untuk pfm_app
make seed                     # katalog permission (tabel action) + 7 role default
make seed-admin               # admin pertama dari ADMIN_USERNAME/EMAIL/PASSWORD
make run                      # API di :8080
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
| `make sqlc` | Generate ulang `internal/db/sqlc` setelah mengubah `internal/db/queries/*.sql` |
| `make migration name=alter_x` | Migrasi baru bernomor berurutan (jangan edit migrasi yang sudah ter-apply) |
| `make worker` | Job pembersihan upload pending > 24 jam dan refresh token kedaluwarsa |
| `make scaffold module=<nama>` | Generate query sqlc + adapter CRUD dari spec di `cmd/scaffold/specs_*.go` |

## Struktur

Layer datar: satu package per layer, satu file per entitas.

```
cmd/api | cmd/migrate | cmd/worker | cmd/scaffold (generator, dev)
internal/
  model/        struct domain (User, Role, Credential, …)
  dto/          request/response + mapper           (<tabel>_gen.go = hasil generator)
  repository/   interface + implementasi sqlc        (<tabel>_gen.go = hasil generator)
  service/      logika bisnis, transaksi, audit      (<tabel>_gen.go = definisi resource CRUD)
  handler/      HTTP handler per entitas + route
  permission/   kode permission (tabel action)
  router/       middleware + wiring repository -> service -> handler
  db/           migrations/ (goose), queries/ (sqlc), sqlc/ (generated), schema/ (generated)
  platform/     database (TxManager, lookup), crud (service+handler generik), httpx, auth,
                security, storage, audit, validatorx, logger
  apperr/ config/ testutil/
deploy/         Dockerfile (distroless), docker-compose (dev), SQL role DB
```

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

## Menambah resource CRUD

1. Tambah spec di `cmd/scaffold/specs_*.go`: kolom wajib/read-only, snapshot, `Check`/`AfterWrite` hook, nested,
   `Kind: "read"`, `Import`, `Scope` (ekspresi SQL ke `client_id`).
2. `make scaffold module=<module>` (butuh DB ter-migrate di `SCAFFOLD_DATABASE_URL`): menulis
   `dto/`, `repository/`, `service/<tabel>_gen.go` dan `service/resources_<module>_gen.go`.
3. Daftarkan sekali di `crudResources()` pada `router/router.go`.
4. Aturan bisnis ditulis di file manual `service/<module>_hooks.go`, bukan di `*_gen.go`.

## Catatan keamanan

- Password legacy (SHA-256 tanpa salt) tidak dimigrasi; user lama wajib aktivasi ulang.
- Dump legacy berisi data pribadi dan hash password: jangan simpan di repo. `secrets/` dan `.env` masuk `.gitignore`.
- Rate limiter memakai memori proses; untuk lebih dari satu instance, pindahkan storage limiter ke Redis (PRD §5.1).
- Unggahan PDF dicek secara heuristik (tanpa dekompresi stream); pasang ClamAV sebelum production (PRD §8.4).
