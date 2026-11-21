-- Valuasi portofolio dari view portfolio_holding (migrasi 00133).

-- name: ListPortfolioHoldings :many
-- Urutan kelas aset sama dengan dto.AssetClasses.
SELECT h.asset_class, h.uuid, h.instrument_uuid, h.instrument_code, h.instrument_name, h.quantity, h.average_price,
       h.cost_value, h.last_price, h.price_date, h.market_value, h.inception_date
FROM portfolio_holding h
WHERE h.portfolio_id = sqlc.arg('portfolio_id')
  AND (sqlc.narg('asset_class')::text IS NULL OR h.asset_class = sqlc.narg('asset_class'))
ORDER BY array_position(ARRAY['STOCK', 'MF-ASSET', 'CRYPTO', 'BOND', 'DEPOSITO'], h.asset_class),
         h.market_value DESC NULLS LAST, h.instrument_code;

-- name: PortfolioAllocations :many
-- Nilai per kelas aset untuk sekumpulan portofolio (ringkasan dan overview).
SELECT h.portfolio_id::bigint AS portfolio_id, h.asset_class::text AS asset_class, count(*)::int AS holdings,
       COALESCE(sum(h.cost_value), 0)::numeric AS cost_value,
       COALESCE(sum(h.market_value), 0)::numeric AS market_value
FROM portfolio_holding h
WHERE h.portfolio_id = ANY(sqlc.arg('portfolio_ids')::bigint[])
GROUP BY h.portfolio_id, h.asset_class
ORDER BY h.portfolio_id, h.asset_class;

-- name: PortfoliosHoldingInstrument :many
SELECT DISTINCT h.portfolio_id::bigint
FROM portfolio_holding h
WHERE h.asset_class = sqlc.arg('asset_class') AND h.instrument_id = sqlc.arg('instrument_id');

-- name: RevaluePortfolio :exec
-- Menyimpan valuasi terkini: nilai pasar posisi dan laba/rugi belum terealisasi
-- portofolio (deposito dinilai pada pokok, tidak menambah laba/rugi). Hanya
-- baris yang berubah yang ditulis.
WITH h AS (SELECT * FROM portfolio_holding WHERE portfolio_id = sqlc.arg('portfolio_id')),
stock_rows AS (
    UPDATE stock_portfolio t SET market_value = h.market_value
    FROM h WHERE h.asset_class = 'STOCK' AND t.uuid = h.uuid AND t.market_value IS DISTINCT FROM h.market_value
    RETURNING t.id),
fund_rows AS (
    UPDATE mutual_fund_portfolio t SET latest_nav = h.last_price
    FROM h WHERE h.asset_class = 'MF-ASSET' AND t.uuid = h.uuid AND t.latest_nav IS DISTINCT FROM h.last_price
    RETURNING t.id),
bond_rows AS (
    UPDATE bond_portfolio t SET market_value = h.market_value
    FROM h WHERE h.asset_class = 'BOND' AND t.uuid = h.uuid AND t.market_value IS DISTINCT FROM h.market_value
    RETURNING t.id)
UPDATE portfolio p
SET unrealized_profit_loss = v.pl
FROM (SELECT COALESCE(sum(h.market_value - h.cost_value) FILTER (WHERE h.asset_class <> 'DEPOSITO'), 0)::numeric AS pl FROM h) v
WHERE p.id = sqlc.arg('portfolio_id') AND p.unrealized_profit_loss IS DISTINCT FROM v.pl;
