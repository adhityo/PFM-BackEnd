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

.PHONY: build
build: ## Build semua binary ke ./bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o bin/ ./cmd/...

.PHONY: run
run: ## Jalankan API (butuh .env)
	$(GO) run ./cmd/api

.PHONY: worker
worker: ## Jalankan worker pembersihan
	$(GO) run ./cmd/worker

.PHONY: migrate-up migrate-down migrate-status seed seed-admin
migrate-up: ## Terapkan semua migrasi
	$(GO) run ./cmd/migrate up
migrate-down: ## Rollback satu migrasi
	$(GO) run ./cmd/migrate down
migrate-status: ## Status migrasi
	$(GO) run ./cmd/migrate status
seed: ## Sinkronkan katalog permission & role default
	$(GO) run ./cmd/migrate seed
seed-admin: ## Buat admin pertama (ADMIN_USERNAME, ADMIN_EMAIL, ADMIN_PASSWORD)
	$(GO) run ./cmd/migrate seed-admin

.PHONY: db-grants
db-grants: ## Terapkan hak akses pfm_app setelah migrasi (PRD §12.5)
	psql "$(MIGRATION_DATABASE_URL)" -v ON_ERROR_STOP=1 -f deploy/postgres/grants-after-migrate.sql

.PHONY: migration
migration: ## Buat file migrasi baru berurutan: make migration name=alter_bond_add_isin
	@test -n "$(name)" || (echo "pakai: make migration name=<nama>" && exit 1)
	$(GO) run github.com/pressly/goose/v3/cmd/goose@v3.30.1 -s -dir internal/db/migrations create $(name) sql

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
