-- Dukungan M5 (dana pensiun):
--  * member.client_id [USULAN]: peserta milik satu client (pemberi kerja). Dasar scope tenant dan
--    penagihan iuran per client; legacy tidak punya relasi ini.
--  * status default 0 (draft/belum dibayar) untuk invoice, iuran, batch & baris payout, transaksi unit.
--  * generate idempoten: satu invoice aktif per client per bulan, satu baris iuran per peserta per
--    invoice, satu batch payout aktif per bulan, satu baris payout per penerima per batch.
--  * satu rekening unit (member_portfolio) per peserta per portofolio; nomor unik.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE member ADD COLUMN client_id BIGINT REFERENCES client (id);
CREATE INDEX ix_member_client_id ON member (client_id);

ALTER TABLE invoice ALTER COLUMN invoice_status SET DEFAULT 0;
ALTER TABLE member_premium_payment ALTER COLUMN status SET DEFAULT 0;
ALTER TABLE benefit_payout_batch ALTER COLUMN payout_batch_status SET DEFAULT 0;
ALTER TABLE member_benefit_payout ALTER COLUMN member_benefit_payout_status SET DEFAULT 0;
ALTER TABLE member_portfolio_transaction ALTER COLUMN transaction_status SET DEFAULT 0;

CREATE UNIQUE INDEX uq_member_number ON member (member_number)
    WHERE deleted_at IS NULL AND member_number IS NOT NULL;
CREATE UNIQUE INDEX uq_member_group_number ON member_group (member_group_number)
    WHERE deleted_at IS NULL AND member_group_number IS NOT NULL;
CREATE UNIQUE INDEX uq_invoice_number ON invoice (invoice_number)
    WHERE deleted_at IS NULL AND invoice_number IS NOT NULL;
CREATE UNIQUE INDEX uq_invoice_client_period ON invoice (client_id, year, month)
    WHERE deleted_at IS NULL AND COALESCE(invoice_status, 0) <> 3;
CREATE UNIQUE INDEX uq_member_premium_payment_invoice_member ON member_premium_payment (invoice_id, member_id)
    WHERE deleted_at IS NULL AND invoice_id IS NOT NULL;
CREATE UNIQUE INDEX uq_benefit_payout_batch_number ON benefit_payout_batch (batch_number)
    WHERE deleted_at IS NULL AND batch_number IS NOT NULL;
CREATE UNIQUE INDEX uq_benefit_payout_batch_period ON benefit_payout_batch (year, month)
    WHERE deleted_at IS NULL AND COALESCE(payout_batch_status, 0) <> 3;
CREATE UNIQUE INDEX uq_member_benefit_payout_recipient
    ON member_benefit_payout (benefit_payout_batch_id, member_id, COALESCE(dependant_id, 0))
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_member_portfolio_pair ON member_portfolio (member_id, portfolio_id)
    WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS uq_member_portfolio_pair;
DROP INDEX IF EXISTS uq_member_benefit_payout_recipient;
DROP INDEX IF EXISTS uq_benefit_payout_batch_period;
DROP INDEX IF EXISTS uq_benefit_payout_batch_number;
DROP INDEX IF EXISTS uq_member_premium_payment_invoice_member;
DROP INDEX IF EXISTS uq_invoice_client_period;
DROP INDEX IF EXISTS uq_invoice_number;
DROP INDEX IF EXISTS uq_member_group_number;
DROP INDEX IF EXISTS uq_member_number;
ALTER TABLE member_portfolio_transaction ALTER COLUMN transaction_status DROP DEFAULT;
ALTER TABLE member_benefit_payout ALTER COLUMN member_benefit_payout_status DROP DEFAULT;
ALTER TABLE benefit_payout_batch ALTER COLUMN payout_batch_status DROP DEFAULT;
ALTER TABLE member_premium_payment ALTER COLUMN status DROP DEFAULT;
ALTER TABLE invoice ALTER COLUMN invoice_status DROP DEFAULT;
DROP INDEX IF EXISTS ix_member_client_id;
ALTER TABLE member DROP COLUMN IF EXISTS client_id;
-- +goose StatementEnd
