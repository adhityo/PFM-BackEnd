-- Hand-written queries used by market hooks (service/market_hooks.go).
-- Parents are recomputed from the latest active history row so inserts,
-- corrections and deletions all leave the parent consistent.

-- name: RefreshStockLatestPrice :exec
UPDATE stock
SET (latest_closing_price, latest_closing_date, latest_market_capitalization, latest_listed_share_volume) = (
    SELECT h.closing_price, h.closing_date, h.total_market_capitalization, h.total_listed_share
    FROM stock_history h
    WHERE h.stock_id = $1 AND h.deleted_at IS NULL
    ORDER BY h.closing_date DESC NULLS LAST, h.id DESC
    LIMIT 1)
WHERE id = $1;

-- name: RefreshStockBeta :exec
UPDATE stock
SET beta_value = (
    SELECT h.beta_value FROM stock_beta_history h
    WHERE h.stock_id = $1 AND h.deleted_at IS NULL
    ORDER BY h.register_date DESC NULLS LAST, h.id DESC
    LIMIT 1)
WHERE id = $1;

-- name: RefreshStockFundamental :exec
UPDATE stock
SET (latest_return_on_equity, latest_debt_equity_ratio, latest_price_earning_ratio, latest_price_book_value,
     latest_earning_per_share, latest_sales_value, latest_net_profit, latest_asset_value, latest_liability_value,
     latest_equity_value) = (
    SELECT h.return_on_equity, h.debt_equity_ratio, h.price_earning_ratio, h.price_book_value, h.earning_per_share,
           h.sales_data, h.net_profit, h.asset, h.liability, h.equity
    FROM stock_fundamental_history h
    WHERE h.stock_id = $1 AND h.deleted_at IS NULL
    ORDER BY h.register_date DESC NULLS LAST, h.id DESC
    LIMIT 1)
WHERE id = $1;

-- name: RefreshIndicesLatest :exec
UPDATE indices
SET (closing_value, market_capitalization) = (
    SELECT h.closing_value, h.closing_market_capitalization FROM indices_history h
    WHERE h.indices_id = $1 AND h.deleted_at IS NULL
    ORDER BY h.closing_date DESC NULLS LAST, h.id DESC
    LIMIT 1)
WHERE id = $1;

-- name: RefreshMutualFundNAV :exec
UPDATE mutual_fund
SET (net_asset_value, latest_update_date) = (
    SELECT h.net_asset_value, h.closing_date FROM mutual_fund_history h
    WHERE h.mutual_fund_id = $1 AND h.deleted_at IS NULL
    ORDER BY h.closing_date DESC NULLS LAST, h.id DESC
    LIMIT 1)
WHERE id = $1;

-- name: RefreshDepositoRate :exec
UPDATE deposito
SET latest_interest_rate = (
    SELECT h.interest_rate FROM deposito_interest_history h
    WHERE h.deposito_id = $1 AND h.deleted_at IS NULL
    ORDER BY h.created_at DESC, h.id DESC
    LIMIT 1)
WHERE id = $1;

-- name: SnapshotDeposito :exec
INSERT INTO deposito_history (uuid, action_history_name, auto_rollover_status, bilyet_number, deposito_code,
                              deposito_name, description, interest_rate, maturity_date, placement_date,
                              placement_value, tenor, deposito_id, created_by, updated_by)
SELECT sqlc.arg('uuid'), sqlc.arg('action')::text, d.auto_rollover_status, d.bilyet_number, d.deposito_code,
       d.deposito_name, left(d.description, 255), d.latest_interest_rate, d.maturity_date, d.placement_date,
       d.placement_value, d.tenor, d.id, sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM deposito d
WHERE d.id = sqlc.arg('id');

-- name: GetSubSectorSectorID :one
SELECT sector_id FROM sub_sector WHERE id = $1;
