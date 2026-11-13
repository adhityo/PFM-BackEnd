-- Hand-written queries used by portfolio hooks and transactions.

-- name: GetPortfolioClientID :one
SELECT client_id FROM portfolio WHERE id = $1;

-- name: SnapshotDepositoPortfolio :exec
INSERT INTO deposito_portfolio_history (uuid, deposito_portfolio_id, total_deposito_allocation, portfolio_year,
                                        portfolio_start_date, portfolio_end_date, portfolio_update_date, tenor,
                                        net_interest_rate, yield_percentage, interest_value, created_by, updated_by)
SELECT sqlc.arg('uuid'), p.id, p.total_deposito_allocation, p.portfolio_year, p.portfolio_start_date,
       p.portfolio_end_date, current_date, p.tenor, p.net_interest_rate, p.yield_percentage, p.interest_value,
       sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM deposito_portfolio p
WHERE p.id = sqlc.arg('id');

-- name: GetDepositoPortfolioForUpdate :one
SELECT p.id, p.uuid, p.deposito_id, p.portfolio_id, p.total_deposito_allocation, p.tenor, p.net_interest_rate,
       p.yield_percentage, p.interest_value, p.next_portfolio_id, p.portfolio_start_date, p.portfolio_end_date,
       p.updated_at
FROM deposito_portfolio p
WHERE p.uuid = $1 AND p.deleted_at IS NULL
FOR UPDATE;

-- name: InsertDepositoRollover :one
INSERT INTO deposito_portfolio (uuid, deposito_id, portfolio_id, total_deposito_allocation, tenor, net_interest_rate,
                                yield_percentage, previous_portfolio_id, portfolio_year, portfolio_start_date,
                                portfolio_end_date, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.narg('deposito_id'), sqlc.narg('portfolio_id'), sqlc.narg('total_deposito_allocation'),
        sqlc.narg('tenor'), sqlc.narg('net_interest_rate'), sqlc.narg('yield_percentage'), sqlc.arg('previous_id'),
        sqlc.narg('portfolio_year'), sqlc.narg('portfolio_start_date'), sqlc.narg('portfolio_end_date'),
        sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING id, uuid;

-- name: LinkDepositoRollover :exec
UPDATE deposito_portfolio
SET next_portfolio_id = sqlc.arg('next_id'), portfolio_end_date = sqlc.narg('end_date'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: CloseDepositoPortfolio :exec
UPDATE deposito_portfolio
SET portfolio_end_date = sqlc.arg('end_date'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: ListPlacementDetails :many
SELECT d.uuid, d.description, d.placement_detail_date, d.placement_value, d.status, d.created_at, d.updated_at
FROM placement_detail d
WHERE d.placement_id = $1 AND d.deleted_at IS NULL
ORDER BY d.placement_detail_date NULLS LAST, d.id;

-- name: SoftDeletePlacementDetails :exec
UPDATE placement_detail
SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE placement_id = sqlc.arg('placement_id') AND deleted_at IS NULL;

-- name: InsertPlacementDetail :exec
INSERT INTO placement_detail (uuid, placement_id, description, placement_detail_date, placement_value, status,
                              created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('placement_id'), sqlc.narg('description'), sqlc.narg('placement_detail_date'),
        sqlc.narg('placement_value'), sqlc.narg('status'), sqlc.arg('actor'), sqlc.arg('actor'));

-- name: TouchPlacement :exec
UPDATE placement SET updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');
