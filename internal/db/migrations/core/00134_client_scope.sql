-- Isolasi data per client (temuan keamanan A3/A5): investor, agen pemasar, penempatan, aset properti,
-- rencana trading kripto dan batch manfaat pensiun kini milik satu client. Baris lama (client kosong)
-- hanya terlihat oleh pengguna global sampai client-nya diisi (ETL).
-- +goose Up
-- +goose StatementBegin
ALTER TABLE investor ADD COLUMN client_id BIGINT;
ALTER TABLE investor ADD CONSTRAINT fk_investor_client_id FOREIGN KEY (client_id) REFERENCES client (id);
CREATE INDEX idx_investor_client_id ON investor (client_id);

ALTER TABLE marketing_agent ADD COLUMN client_id BIGINT;
ALTER TABLE marketing_agent ADD CONSTRAINT fk_marketing_agent_client_id FOREIGN KEY (client_id) REFERENCES client (id);
CREATE INDEX idx_marketing_agent_client_id ON marketing_agent (client_id);

ALTER TABLE placement ADD COLUMN client_id BIGINT;
ALTER TABLE placement ADD CONSTRAINT fk_placement_client_id FOREIGN KEY (client_id) REFERENCES client (id);
CREATE INDEX idx_placement_client_id ON placement (client_id);

ALTER TABLE property_asset ADD COLUMN client_id BIGINT;
ALTER TABLE property_asset ADD CONSTRAINT fk_property_asset_client_id FOREIGN KEY (client_id) REFERENCES client (id);
CREATE INDEX idx_property_asset_client_id ON property_asset (client_id);

ALTER TABLE crypto_trade_plan ADD COLUMN client_id BIGINT;
ALTER TABLE crypto_trade_plan ADD CONSTRAINT fk_crypto_trade_plan_client_id FOREIGN KEY (client_id) REFERENCES client (id);
CREATE INDEX idx_crypto_trade_plan_client_id ON crypto_trade_plan (client_id);

-- Batch manfaat dibuat per client per bulan (seperti invoice iuran).
ALTER TABLE benefit_payout_batch ADD COLUMN client_id BIGINT;
ALTER TABLE benefit_payout_batch ADD CONSTRAINT fk_benefit_payout_batch_client_id FOREIGN KEY (client_id) REFERENCES client (id);
CREATE INDEX idx_benefit_payout_batch_client_id ON benefit_payout_batch (client_id);
DROP INDEX IF EXISTS uq_benefit_payout_batch_period;
CREATE UNIQUE INDEX uq_benefit_payout_batch_period ON benefit_payout_batch (COALESCE(client_id, 0), year, month)
    WHERE deleted_at IS NULL AND COALESCE(payout_batch_status, 0) <> 3;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS uq_benefit_payout_batch_period;
CREATE UNIQUE INDEX uq_benefit_payout_batch_period ON benefit_payout_batch (year, month)
    WHERE deleted_at IS NULL AND COALESCE(payout_batch_status, 0) <> 3;
ALTER TABLE benefit_payout_batch DROP COLUMN IF EXISTS client_id;
ALTER TABLE crypto_trade_plan DROP COLUMN IF EXISTS client_id;
ALTER TABLE property_asset DROP COLUMN IF EXISTS client_id;
ALTER TABLE placement DROP COLUMN IF EXISTS client_id;
ALTER TABLE marketing_agent DROP COLUMN IF EXISTS client_id;
ALTER TABLE investor DROP COLUMN IF EXISTS client_id;
-- +goose StatementEnd
