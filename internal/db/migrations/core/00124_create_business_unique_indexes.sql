-- Unique index bisnis (data aktif saja), ditambahkan untuk M2:
-- satu nilai time-series per entitas per tanggal, dan mapping tanpa duplikat.
-- +goose Up
-- +goose StatementBegin
CREATE UNIQUE INDEX uq_stock_history_stock_date ON stock_history (stock_id, closing_date) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_stock_beta_history_stock_date ON stock_beta_history (stock_id, register_date) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_stock_fundamental_history_stock_date ON stock_fundamental_history (stock_id, register_date) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_indices_history_indices_date ON indices_history (indices_id, closing_date) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_mutual_fund_history_fund_date ON mutual_fund_history (mutual_fund_id, closing_date) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_indices_stock_pair ON indices_stock (indices_id, stock_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_stock_type_detail_pair ON stock_type_detail (stock_id, stock_type_id) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_portfolio_budget_year ON portfolio_budget (portfolio_id, asset_class_id, budget_year) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS uq_portfolio_budget_year;
DROP INDEX IF EXISTS uq_stock_type_detail_pair;
DROP INDEX IF EXISTS uq_indices_stock_pair;
DROP INDEX IF EXISTS uq_mutual_fund_history_fund_date;
DROP INDEX IF EXISTS uq_indices_history_indices_date;
DROP INDEX IF EXISTS uq_stock_fundamental_history_stock_date;
DROP INDEX IF EXISTS uq_stock_beta_history_stock_date;
DROP INDEX IF EXISTS uq_stock_history_stock_date;
-- +goose StatementEnd
