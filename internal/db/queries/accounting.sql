-- Hand-written accounting queries (service/journal.go, finance_transaction.go, cash_settlement.go).

-- name: NextNumber :one
-- Gapless counter: the row lock taken by ON CONFLICT DO UPDATE lasts until
-- the caller's transaction ends, and a rollback also rolls the number back.
INSERT INTO number_sequence (uuid, name, period, last_value)
VALUES (sqlc.arg('uuid'), sqlc.arg('name'), sqlc.arg('period'), 1)
ON CONFLICT (name, period) DO UPDATE SET last_value = number_sequence.last_value + 1
RETURNING last_value;

-- name: InsertJournal :one
INSERT INTO finance_transaction (uuid, transaction_date, transaction_number, transaction_value, description,
                                 transaction_status, portfolio_id, stock_trade_id, mutual_fund_trade_id, crypto_trade_id,
                                 cash_settlement_id, reversal_of_id, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('transaction_date'), sqlc.arg('transaction_number'), sqlc.arg('transaction_value'),
        sqlc.narg('description'), 1, sqlc.narg('portfolio_id'), sqlc.narg('stock_trade_id'), sqlc.narg('mutual_fund_trade_id'),
        sqlc.narg('crypto_trade_id'), sqlc.narg('cash_settlement_id'), sqlc.narg('reversal_of_id'), sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING id, uuid;

-- name: InsertJournalLine :exec
INSERT INTO finance_transaction_detail (uuid, finance_transaction_id, finance_account_id, debit, credit, description,
                                        created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('finance_transaction_id'), sqlc.arg('finance_account_id'), sqlc.arg('debit'),
        sqlc.arg('credit'), sqlc.narg('description'), sqlc.arg('actor'), sqlc.arg('actor'));

-- name: GetJournal :one
SELECT t.id, t.uuid, t.transaction_date, t.transaction_number, t.transaction_value, t.description, t.transaction_status,
       t.portfolio_id, p.uuid AS portfolio_uuid, st.uuid AS stock_trade_uuid, mt.uuid AS mutual_fund_trade_uuid,
       ct.uuid AS crypto_trade_uuid, cs.uuid AS cash_settlement_uuid, rv.uuid AS reversal_of_uuid,
       t.stock_trade_id, t.mutual_fund_trade_id, t.crypto_trade_id, t.cash_settlement_id, t.reversal_of_id,
       t.created_by, t.created_at, t.updated_at
FROM finance_transaction t
LEFT JOIN portfolio p ON p.id = t.portfolio_id
LEFT JOIN stock_trade st ON st.id = t.stock_trade_id
LEFT JOIN mutual_fund_trade mt ON mt.id = t.mutual_fund_trade_id
LEFT JOIN crypto_trade ct ON ct.id = t.crypto_trade_id
LEFT JOIN cash_settlement cs ON cs.id = t.cash_settlement_id
LEFT JOIN finance_transaction rv ON rv.id = t.reversal_of_id
WHERE t.uuid = sqlc.arg('uuid') AND t.deleted_at IS NULL
  AND (sqlc.narg('scope_client_id')::bigint IS NULL OR p.client_id = sqlc.narg('scope_client_id'));

-- name: ListJournals :many
SELECT t.uuid, t.transaction_date, t.transaction_number, t.transaction_value, t.description, t.transaction_status,
       p.uuid AS portfolio_uuid, rv.uuid AS reversal_of_uuid, t.created_at
FROM finance_transaction t
LEFT JOIN portfolio p ON p.id = t.portfolio_id
LEFT JOIN finance_transaction rv ON rv.id = t.reversal_of_id
WHERE t.deleted_at IS NULL
  AND (sqlc.narg('scope_client_id')::bigint IS NULL OR p.client_id = sqlc.narg('scope_client_id'))
  AND (sqlc.narg('portfolio_id')::bigint IS NULL OR t.portfolio_id = sqlc.narg('portfolio_id'))
  AND (sqlc.narg('from')::date IS NULL OR t.transaction_date >= sqlc.narg('from'))
  AND (sqlc.narg('to')::date IS NULL OR t.transaction_date <= sqlc.narg('to'))
  AND (sqlc.narg('q')::text IS NULL OR t.transaction_number ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR t.description ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\')
ORDER BY
  CASE WHEN sqlc.arg('sort')::text = 'transaction_date' THEN t.transaction_date END ASC,
  CASE WHEN sqlc.arg('sort')::text = 'transaction_number' THEN t.transaction_number END ASC,
  CASE WHEN sqlc.arg('sort')::text = '-transaction_number' THEN t.transaction_number END DESC,
  t.transaction_date DESC, t.id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountJournals :one
SELECT count(*)
FROM finance_transaction t
LEFT JOIN portfolio p ON p.id = t.portfolio_id
WHERE t.deleted_at IS NULL
  AND (sqlc.narg('scope_client_id')::bigint IS NULL OR p.client_id = sqlc.narg('scope_client_id'))
  AND (sqlc.narg('portfolio_id')::bigint IS NULL OR t.portfolio_id = sqlc.narg('portfolio_id'))
  AND (sqlc.narg('from')::date IS NULL OR t.transaction_date >= sqlc.narg('from'))
  AND (sqlc.narg('to')::date IS NULL OR t.transaction_date <= sqlc.narg('to'))
  AND (sqlc.narg('q')::text IS NULL OR t.transaction_number ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\'
       OR t.description ILIKE '%' || sqlc.narg('q') || '%' ESCAPE '\');

-- name: ListJournalLines :many
SELECT d.uuid, d.finance_account_id, a.uuid AS finance_account_uuid, a.finance_account_number, a.finance_account_name,
       d.debit, d.credit, d.description
FROM finance_transaction_detail d
JOIN finance_account a ON a.id = d.finance_account_id
WHERE d.finance_transaction_id = $1 AND d.deleted_at IS NULL
ORDER BY d.id;

-- name: LockJournal :one
SELECT id, transaction_status, updated_at FROM finance_transaction WHERE id = $1 FOR UPDATE;

-- name: SetJournalStatus :exec
UPDATE finance_transaction SET transaction_status = sqlc.arg('status'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: FindPostedJournal :one
-- The live (not yet reversed) journal posted for a source document.
SELECT id FROM finance_transaction
WHERE deleted_at IS NULL AND transaction_status = 1 AND reversal_of_id IS NULL
  AND (sqlc.narg('stock_trade_id')::bigint IS NULL OR stock_trade_id = sqlc.narg('stock_trade_id'))
  AND (sqlc.narg('mutual_fund_trade_id')::bigint IS NULL OR mutual_fund_trade_id = sqlc.narg('mutual_fund_trade_id'))
  AND (sqlc.narg('crypto_trade_id')::bigint IS NULL OR crypto_trade_id = sqlc.narg('crypto_trade_id'))
  AND (sqlc.narg('cash_settlement_id')::bigint IS NULL OR cash_settlement_id = sqlc.narg('cash_settlement_id'))
ORDER BY id DESC
LIMIT 1;

-- name: ResolveAccounts :many
SELECT id, uuid FROM finance_account WHERE uuid = ANY(sqlc.arg('uuids')::uuid[]) AND deleted_at IS NULL;

-- name: GetAssetClassAccounts :one
SELECT m.buy_account_id, m.sell_account_id, m.capital_gain_account_id, m.capital_loss_account_id, m.interest_coupon_account_id
FROM asset_class_finance_account m
JOIN asset_class ac ON ac.id = m.asset_class_id AND ac.deleted_at IS NULL
WHERE m.portfolio_id = sqlc.arg('portfolio_id') AND ac.asset_class_code = sqlc.arg('asset_class_code') AND m.deleted_at IS NULL;

-- name: GetSettlementAccount :one
SELECT s.finance_account_id
FROM portfolio_accounting_structure s
JOIN asset_class ac ON ac.id = s.asset_class_id AND ac.deleted_at IS NULL
JOIN trade_type tt ON tt.id = s.trade_type_id AND tt.deleted_at IS NULL
WHERE s.portfolio_id = sqlc.arg('portfolio_id') AND ac.asset_class_code = sqlc.arg('asset_class_code')
  AND tt.trade_type_code = sqlc.arg('trade_type_code') AND s.deleted_at IS NULL;

-- name: GetTradeTypeCode :one
SELECT trade_type_code FROM trade_type WHERE id = $1;

-- name: AddRealizedProfitLoss :exec
UPDATE portfolio SET realized_profit_loss = COALESCE(realized_profit_loss, 0) + sqlc.arg('delta')::numeric
WHERE id = sqlc.arg('id');

-- name: GetCashSettlementCategoryRules :one
SELECT inbound_settlement, outbound_settlement, settlement_fee_percentage, settlement_fee_account_id,
       cash_settlement_account_id, cash_settlement_counter_account_id
FROM cash_settlement_category WHERE id = $1 AND deleted_at IS NULL;

-- name: LockCashSettlement :one
SELECT cs.id, cs.uuid, cs.cash_settlement_status, cs.cash_settlement_category_id, cs.cash_settlement_value,
       cs.cash_settlement_subject, cs.created_by, cs.updated_at,
       sp.portfolio_id AS stock_position_portfolio_id, bp.portfolio_id AS bond_position_portfolio_id,
       dp.portfolio_id AS deposito_position_portfolio_id, mp.portfolio_id AS fund_position_portfolio_id
FROM cash_settlement cs
LEFT JOIN stock_portfolio sp ON sp.id = cs.stock_portfolio_id
LEFT JOIN bond_portfolio bp ON bp.id = cs.bond_portfolio_id
LEFT JOIN deposito_portfolio dp ON dp.id = cs.deposito_portfolio_id
LEFT JOIN mutual_fund_portfolio mp ON mp.id = cs.mutual_fund_portfolio_id
WHERE cs.uuid = $1 AND cs.deleted_at IS NULL
FOR UPDATE OF cs;

-- name: SetCashSettlementStatus :exec
UPDATE cash_settlement SET cash_settlement_status = sqlc.arg('status'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: GetJournalLinks :one
SELECT portfolio_id, stock_trade_id, mutual_fund_trade_id, crypto_trade_id, cash_settlement_id
FROM finance_transaction WHERE id = $1;
