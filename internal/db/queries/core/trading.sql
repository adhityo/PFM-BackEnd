-- Hand-written trading queries (service/trading.go). Positions are rebuilt by
-- replaying settled trades in (date, id) order.

-- ---------------------------------------------------------------- stock

-- name: LockStockTrade :one
SELECT t.id, t.uuid, t.portfolio_id, t.stock_id, t.trade_type_id, t.stock_trade_date, t.price, t.quantity_share,
       t.trade_value, t.transaction_fee_amount, t.stock_trade_status, t.stock_code, t.updated_at
FROM stock_trade t WHERE t.uuid = $1 AND t.deleted_at IS NULL FOR UPDATE;

-- name: SetStockTradeStatus :exec
UPDATE stock_trade SET stock_trade_status = sqlc.arg('status'), updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');

-- name: SettledStockTrades :many
SELECT t.id, t.stock_trade_date AS trade_date, tt.trade_type_code, t.price, t.quantity_share AS quantity,
       t.trade_value, t.transaction_fee_amount AS fee
FROM stock_trade t JOIN trade_type tt ON tt.id = t.trade_type_id
WHERE t.portfolio_id = sqlc.arg('portfolio_id') AND t.stock_id = sqlc.arg('instrument_id')
  AND t.stock_trade_status = 1 AND t.deleted_at IS NULL
ORDER BY t.stock_trade_date, t.id;

-- name: UpsertStockPosition :one
INSERT INTO stock_portfolio (uuid, portfolio_id, stock_id, inception_date, average_price, stock_amount, market_value,
                             created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('portfolio_id'), sqlc.arg('instrument_id'), sqlc.narg('inception_date'),
        sqlc.arg('average_price'), sqlc.arg('quantity'),
        round(sqlc.arg('quantity')::numeric * COALESCE((SELECT s.latest_closing_price FROM stock s WHERE s.id = sqlc.arg('instrument_id')), sqlc.arg('last_price')::numeric), 4),
        sqlc.arg('actor'), sqlc.arg('actor'))
ON CONFLICT (portfolio_id, stock_id) WHERE deleted_at IS NULL DO UPDATE
SET inception_date = EXCLUDED.inception_date, average_price = EXCLUDED.average_price,
    stock_amount = EXCLUDED.stock_amount, market_value = EXCLUDED.market_value, updated_by = EXCLUDED.updated_by
RETURNING id;

-- name: SnapshotStockPosition :exec
INSERT INTO stock_portfolio_history (uuid, stock_portfolio_id, stock_code, total_stock_amount, closing_price,
                                     total_investment_value, closing_date, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('position_id'), sqlc.narg('code'), sqlc.arg('quantity'), sqlc.narg('price'),
        sqlc.arg('cost'), sqlc.narg('trade_date'), sqlc.arg('actor'), sqlc.arg('actor'));

-- ---------------------------------------------------------------- mutual fund

-- name: LockMutualFundTrade :one
SELECT t.id, t.uuid, t.portfolio_id, t.mutual_fund_id, t.trade_type_id, t.transaction_date, t.net_asset_value,
       t.total_unit, t.total_investment_value, t.transaction_fee, t.status, t.mutual_fund_name, t.updated_at
FROM mutual_fund_trade t WHERE t.uuid = $1 AND t.deleted_at IS NULL FOR UPDATE;

-- name: SetMutualFundTradeStatus :exec
UPDATE mutual_fund_trade SET status = sqlc.arg('status'), updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');

-- name: SettledMutualFundTrades :many
SELECT t.id, t.transaction_date AS trade_date, tt.trade_type_code, t.net_asset_value AS price, t.total_unit AS quantity,
       t.total_investment_value AS trade_value, t.transaction_fee AS fee
FROM mutual_fund_trade t JOIN trade_type tt ON tt.id = t.trade_type_id
WHERE t.portfolio_id = sqlc.arg('portfolio_id') AND t.mutual_fund_id = sqlc.arg('instrument_id')
  AND t.status = 1 AND t.deleted_at IS NULL
ORDER BY t.transaction_date, t.id;

-- name: UpsertMutualFundPosition :one
INSERT INTO mutual_fund_portfolio (uuid, portfolio_id, mutual_fund_id, inception_date, average_nav, total_unit,
                                   latest_nav, total_investment_value, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('portfolio_id'), sqlc.arg('instrument_id'), sqlc.narg('inception_date'),
        sqlc.arg('average_price'), sqlc.arg('quantity'), sqlc.narg('last_price'), sqlc.arg('cost'),
        sqlc.arg('actor'), sqlc.arg('actor'))
ON CONFLICT (portfolio_id, mutual_fund_id) WHERE deleted_at IS NULL DO UPDATE
SET inception_date = EXCLUDED.inception_date, average_nav = EXCLUDED.average_nav, total_unit = EXCLUDED.total_unit,
    latest_nav = EXCLUDED.latest_nav, total_investment_value = EXCLUDED.total_investment_value,
    updated_by = EXCLUDED.updated_by
RETURNING id;

-- name: SnapshotMutualFundPosition :exec
INSERT INTO mutual_fund_portfolio_history (uuid, mutual_fund_portfolio_id, net_asset_value, closing_date, total_unit,
                                           total_investment_value, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('position_id'), sqlc.narg('price'), sqlc.narg('trade_date'), sqlc.arg('quantity'),
        sqlc.arg('cost'), sqlc.arg('actor'), sqlc.arg('actor'));

-- ---------------------------------------------------------------- crypto

-- name: LockCryptoTrade :one
SELECT t.id, t.uuid, t.portfolio_id, t.crypto_id, t.trade_type_id, t.trade_date, t.price, t.quantity, t.trade_value,
       t.transaction_fee_amount, t.status, t.crypto_code, t.updated_at
FROM crypto_trade t WHERE t.uuid = $1 AND t.deleted_at IS NULL FOR UPDATE;

-- name: SetCryptoTradeStatus :exec
UPDATE crypto_trade SET status = sqlc.arg('status'), updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');

-- name: SettledCryptoTrades :many
SELECT t.id, t.trade_date, tt.trade_type_code, t.price, t.quantity, t.trade_value, t.transaction_fee_amount AS fee
FROM crypto_trade t JOIN trade_type tt ON tt.id = t.trade_type_id
WHERE t.portfolio_id = sqlc.arg('portfolio_id') AND t.crypto_id = sqlc.arg('instrument_id')
  AND t.status = 'settled' AND t.deleted_at IS NULL
ORDER BY t.trade_date, t.id;

-- name: UpsertCryptoPosition :one
INSERT INTO crypto_portfolio (uuid, portfolio_id, crypto_id, total_average_price, total_crypto_quantity, total_value,
                              status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('portfolio_id'), sqlc.arg('instrument_id'), sqlc.arg('average_price'),
        sqlc.arg('quantity'), sqlc.arg('cost'), 'active', sqlc.arg('actor'), sqlc.arg('actor'))
ON CONFLICT (portfolio_id, crypto_id) WHERE deleted_at IS NULL DO UPDATE
SET total_average_price = EXCLUDED.total_average_price, total_crypto_quantity = EXCLUDED.total_crypto_quantity,
    total_value = EXCLUDED.total_value, updated_by = EXCLUDED.updated_by
RETURNING id;

-- name: GetMutualFundNAV :one
SELECT net_asset_value FROM mutual_fund WHERE id = $1;
