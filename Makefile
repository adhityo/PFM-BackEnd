SHELL := /bin/bash
include $(wildcard .env)
export

GO ?= go
TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable

.PHONY: help
help: ## Tampilkan daftar target
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

.PHONY: keys
keys: ## Buat kunci JWT Ed25519 untuk development (secrets/jwt_ed25519.pem)
	@mkdir -p secrets && chmod 700 secrets
	@test -f secrets/jwt_ed25519.pem || openssl genpkey -algorithm ed25519 -out secrets/jwt_ed25519.pem
	@chmod 600 secrets/jwt_ed25519.pem && echo "secrets/jwt_ed25519.pem siap"

.PHONY: sqlc
sqlc: ## Generate kode query dari internal/db/queries
	sqlc generate

# Database gabungan (semua set migrasi) khusus generator: make schema-db
SCAFFOLD_DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/pfm_schema?sslmode=disable

.PHONY: schema-db
schema-db: ## Siapkan database gabungan untuk generator (migrate -db all)
	SCAFFOLD_DATABASE_URL="$(SCAFFOLD_DATABASE_URL)" $(GO) run ./cmd/migrate -db all up

.PHONY: scaffold
scaffold: ## Generate CRUD dari spec: make scaffold module=masterdata (butuh DB ter-migrate)
	@test -n "$(module)" || (echo "pakai: make scaffold module=<nama>" && exit 1)
	SCAFFOLD_DATABASE_URL="$(SCAFFOLD_DATABASE_URL)" $(GO) run ./cmd/scaffold schema
	SCAFFOLD_DATABASE_URL="$(SCAFFOLD_DATABASE_URL)" $(GO) run ./cmd/scaffold module $(module)
	sqlc generate
	$(GO) build ./...

.PHONY: build
build: ## Build semua binary ke ./bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o bin/ ./cmd/...

.PHONY: run
run: ## Jalankan API (butuh .env)
	$(GO) run ./cmd/api

.PHONY: dev
dev: ## Jalankan API dengan live reload (air: go install github.com/air-verse/air@latest)
	@command -v air >/dev/null || (echo "air belum terpasang: go install github.com/air-verse/air@latest" && exit 1)
	air -c .air.toml

.PHONY: worker
worker: ## Jalankan worker: kirim audit outbox ke pfm_log + pembersihan (pakai user DB worker)
	$(GO) run ./cmd/worker

.PHONY: migrate-up migrate-down migrate-status seed seed-admin
migrate-up: ## Terapkan migrasi ke keempat database
	$(GO) run ./cmd/migrate up
migrate-down: ## Rollback satu migrasi: make migrate-down db=core
	@test -n "$(db)" || (echo "pakai: make migrate-down db=<core|security|log|document>" && exit 1)
	$(GO) run ./cmd/migrate -db $(db) down
migrate-status: ## Status migrasi
	$(GO) run ./cmd/migrate status
seed: ## Sinkronkan katalog permission & role default
	$(GO) run ./cmd/migrate seed
seed-admin: ## Buat admin pertama (ADMIN_USERNAME, ADMIN_EMAIL, ADMIN_PASSWORD)
	$(GO) run ./cmd/migrate seed-admin

.PHONY: db-grants
db-grants: ## Terapkan hak akses minimum ke 4 database setelah migrasi (PRD §7.1, §12.5)
	psql "$(MIGRATION_DATABASE_URL_CORE)"     -v ON_ERROR_STOP=1 -f deploy/postgres/grants-core.sql
	psql "$(MIGRATION_DATABASE_URL_SECURITY)" -v ON_ERROR_STOP=1 -f deploy/postgres/grants-security.sql
	psql "$(MIGRATION_DATABASE_URL_LOG)"      -v ON_ERROR_STOP=1 -f deploy/postgres/grants-log.sql
	psql "$(MIGRATION_DATABASE_URL_DOCUMENT)" -v ON_ERROR_STOP=1 -f deploy/postgres/grants-document.sql

.PHONY: migration
migration: ## Buat migrasi baru: make migration db=core name=alter_bond_add_isin (nomor global berurutan)
	@test -n "$(name)" -a -n "$(db)" || (echo "pakai: make migration db=<core|security|log|document> name=<nama>" && exit 1)
	@test -d internal/db/migrations/$(db) || (echo "db tidak dikenal: $(db)" && exit 1)
	@next=$$(ls internal/db/migrations/*/*.sql | sed -E 's#.*/([0-9]+)_.*#\1#' | sort -n | tail -1); \
	 file=internal/db/migrations/$(db)/$$(printf '%05d' $$((10#$$next + 1)))_$(name).sql; \
	 printf -- '-- +goose Up\n-- +goose StatementBegin\n\n-- +goose StatementEnd\n\n-- +goose Down\n-- +goose StatementBegin\n\n-- +goose StatementEnd\n' > $$file; \
	 echo "dibuat: $$file"

.PHONY: test
test: ## Unit test (integration test dilewati tanpa TEST_DATABASE_URL)
	$(GO) test ./... -race -count=1

.PHONY: test-integration
test-integration: ## Semua test termasuk integration (butuh PostgreSQL)
	TEST_DATABASE_URL="$(TEST_DATABASE_URL)" $(GO) test ./... -race -count=1 -cover

.PHONY: lint
lint: ## golangci-lint (termasuk gosec)
	golangci-lint run ./...

.PHONY: vuln
vuln: ## Scan kerentanan dependency & stdlib
	govulncheck ./...

.PHONY: check
check: lint vuln test-integration ## Semua pemeriksaan CI

.PHONY: dev-up dev-down
dev-up: ## Jalankan PostgreSQL + MinIO untuk development
	docker compose -f deploy/docker-compose.yml up -d
dev-down: ## Hentikan service development
	docker compose -f deploy/docker-compose.yml down
