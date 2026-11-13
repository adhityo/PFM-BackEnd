# PFM Backend

REST API Portfolio & Fund Management: Go + Fiber v3 + PostgreSQL. Spesifikasi lengkap ada di [prd.md](prd.md).

Status: **M0 (Fondasi) selesai** — skema 122 tabel, IAM/auth, storage file, CI. Modul bisnis (M1–M7) belum.

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

## Struktur

```
cmd/api | cmd/migrate | cmd/worker
internal/
  apperr/            error domain -> HTTP
  config/            konfigurasi dari env (divalidasi saat start)
  db/                migrations/ (goose, 1 tabel per file), queries/ (sqlc), sqlc/ (generated)
  platform/          database (TxManager), httpx, validatorx, security, auth (RBAC router), storage, audit
  module/iam         auth, users, roles, permissions, activity log
  module/file        upload image (lokal) & dokumen (S3)
  server/            perakitan middleware + route
deploy/              Dockerfile (distroless), docker-compose (dev), SQL role DB
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

## Catatan keamanan

- Password legacy (SHA-256 tanpa salt) tidak dimigrasi; user lama wajib aktivasi ulang.
- File `database` (dump legacy berisi data pribadi) dan `secrets/` masuk `.gitignore`.
- Rate limiter memakai memori proses; untuk lebih dari satu instance, pindahkan storage limiter ke Redis (PRD §5.1).
- Unggahan PDF dicek secara heuristik (tanpa dekompresi stream); pasang ClamAV sebelum production (PRD §8.4).
