-- Kepemilikan aktif semua kelas aset dalam satu bentuk dengan valuasi terkini (PRD §10.5):
-- dipakai ringkasan portofolio, daftar kepemilikan, overview dan revaluasi, sehingga rumusnya satu.
--   STOCK     unit = lembar; harga = stock.latest_closing_price, lalu harga trade settled terakhir
--   MF-ASSET  unit = unit penyertaan; harga = mutual_fund.net_asset_value (NAV terbaru), lalu NAV trade terakhir
--   CRYPTO    unit = koin; harga = harga trade settled terakhir (belum ada sumber harga pasar)
--   BOND      unit = nominal; harga = bond.bond_price dalam % dari nominal
--   DEPOSITO  nilai = pokok penempatan aktif (belum di-rollover, belum lewat tanggal akhir/cair)
-- Tanpa harga sama sekali, nilai pasar memakai harga rata-rata (laba/rugi belum terealisasi 0).
-- Status settled: stock_trade_status = 1, crypto_trade.status = 'settled' (service/trading.go).
-- +goose Up
-- +goose StatementBegin
CREATE VIEW portfolio_holding AS
SELECT 'STOCK'::text AS asset_class, sp.uuid, sp.portfolio_id,
       s.id AS instrument_id, s.uuid AS instrument_uuid, s.stock_code AS instrument_code, s.stock_name AS instrument_name,
       sp.stock_amount AS quantity, sp.average_price,
       round(sp.stock_amount * sp.average_price, 4) AS cost_value,
       COALESCE(s.latest_closing_price, lt.price, sp.average_price) AS last_price,
       CASE WHEN s.latest_closing_price IS NOT NULL THEN s.latest_closing_date ELSE lt.stock_trade_date END AS price_date,
       round(sp.stock_amount * COALESCE(s.latest_closing_price, lt.price, sp.average_price), 4) AS market_value,
       sp.inception_date
FROM stock_portfolio sp
JOIN stock s ON s.id = sp.stock_id
LEFT JOIN LATERAL (SELECT st.price, st.stock_trade_date FROM stock_trade st
                   WHERE st.stock_id = sp.stock_id AND st.stock_trade_status = 1 AND st.deleted_at IS NULL
                   ORDER BY st.stock_trade_date DESC, st.id DESC LIMIT 1) lt ON TRUE
WHERE sp.deleted_at IS NULL AND COALESCE(sp.stock_amount, 0) > 0
UNION ALL
SELECT 'MF-ASSET', mp.uuid, mp.portfolio_id,
       m.id, m.uuid, m.mutual_fund_code, m.mutual_fund_name,
       mp.total_unit, mp.average_nav,
       COALESCE(mp.total_investment_value, round(mp.total_unit * mp.average_nav, 4)),
       COALESCE(m.net_asset_value, mp.latest_nav, mp.average_nav), m.latest_update_date,
       round(mp.total_unit * COALESCE(m.net_asset_value, mp.latest_nav, mp.average_nav), 4),
       mp.inception_date
FROM mutual_fund_portfolio mp
JOIN mutual_fund m ON m.id = mp.mutual_fund_id
WHERE mp.deleted_at IS NULL AND COALESCE(mp.total_unit, 0) > 0
UNION ALL
SELECT 'CRYPTO', cp.uuid, cp.portfolio_id,
       c.id, c.uuid, c.crypto_code, c.crypto_name,
       cp.total_crypto_quantity, cp.total_average_price,
       COALESCE(cp.total_value, round(cp.total_crypto_quantity * cp.total_average_price, 4)),
       COALESCE(lt.price, cp.total_average_price), lt.trade_date,
       round(cp.total_crypto_quantity * COALESCE(lt.price, cp.total_average_price), 4),
       NULL::date
FROM crypto_portfolio cp
JOIN crypto c ON c.id = cp.crypto_id
LEFT JOIN LATERAL (SELECT ct.price, ct.trade_date FROM crypto_trade ct
                   WHERE ct.crypto_id = cp.crypto_id AND ct.status = 'settled' AND ct.deleted_at IS NULL
                   ORDER BY ct.trade_date DESC, ct.id DESC LIMIT 1) lt ON TRUE
WHERE cp.deleted_at IS NULL AND COALESCE(cp.total_crypto_quantity, 0) > 0
UNION ALL
SELECT 'BOND', bp.uuid, bp.portfolio_id,
       bp.bond_id, b.uuid, COALESCE(b.bond_code, bp.bond_code), COALESCE(b.bond_name, bp.bond_name),
       bp.nominal, COALESCE(bp.average_price, bp.purchased_price, 100),
       round(bp.nominal * COALESCE(bp.average_price, bp.purchased_price, 100) / 100, 4),
       COALESCE(b.bond_price, bp.average_price, bp.purchased_price, 100), NULL::date,
       round(bp.nominal * COALESCE(b.bond_price, bp.average_price, bp.purchased_price, 100) / 100, 4),
       bp.inception_date
FROM bond_portfolio bp
LEFT JOIN bond b ON b.id = bp.bond_id
WHERE bp.deleted_at IS NULL AND COALESCE(bp.nominal, 0) > 0
UNION ALL
SELECT 'DEPOSITO', dp.uuid, dp.portfolio_id,
       dp.deposito_id, d.uuid, d.deposito_code, d.deposito_name,
       NULL::numeric, NULL::numeric,
       dp.total_deposito_allocation,
       NULL::numeric, NULL::date,
       dp.total_deposito_allocation,
       dp.portfolio_start_date
FROM deposito_portfolio dp
LEFT JOIN deposito d ON d.id = dp.deposito_id
WHERE dp.deleted_at IS NULL AND dp.next_portfolio_id IS NULL
  AND (dp.portfolio_end_date IS NULL OR dp.portfolio_end_date >= current_date)
  AND COALESCE(dp.total_deposito_allocation, 0) > 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS portfolio_holding;
-- +goose StatementEnd
