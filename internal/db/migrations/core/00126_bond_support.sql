-- Dukungan M4 (obligasi, investor, agen):
--  * status default: bond 0 (draft), bond_trade 0 (draft; legacy: 1 approved, -1 dibatalkan),
--    kupon 0 (belum dibayar), fee agen 0 (draft), change request 0 (menunggu).
--  * nomor sertifikat unik di antara trade aktif.
--  * rekening bank milik tepat satu pihak (investor ATAU agen). NOT VALID: baris legacy
--    tidak diperiksa, baris baru/diubah wajib memenuhi.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE bond ALTER COLUMN bond_status SET DEFAULT 0;
ALTER TABLE bond_trade ALTER COLUMN bond_trade_status SET DEFAULT 0;
ALTER TABLE bond_coupon_schedule ALTER COLUMN status SET DEFAULT 0;
ALTER TABLE investor_coupon_schedule ALTER COLUMN coupon_status SET DEFAULT 0;
ALTER TABLE agent_fee_transaction ALTER COLUMN status SET DEFAULT 0;
ALTER TABLE investor_data_history ALTER COLUMN approval_status SET DEFAULT 0;
ALTER TABLE marketing_agent_data_history ALTER COLUMN approval_status SET DEFAULT 0;

CREATE UNIQUE INDEX uq_bond_trade_certificate_number ON bond_trade (certificate_number)
    WHERE deleted_at IS NULL AND certificate_number IS NOT NULL;
CREATE UNIQUE INDEX uq_investor_coupon_schedule_pair ON investor_coupon_schedule (bond_coupon_schedule_id, bond_trade_id)
    WHERE deleted_at IS NULL;

ALTER TABLE bank_account ADD CONSTRAINT ck_bank_account_single_owner
    CHECK ((investor_id IS NULL) <> (marketing_agent_id IS NULL)) NOT VALID;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bank_account DROP CONSTRAINT IF EXISTS ck_bank_account_single_owner;
DROP INDEX IF EXISTS uq_investor_coupon_schedule_pair;
DROP INDEX IF EXISTS uq_bond_trade_certificate_number;
ALTER TABLE marketing_agent_data_history ALTER COLUMN approval_status DROP DEFAULT;
ALTER TABLE investor_data_history ALTER COLUMN approval_status DROP DEFAULT;
ALTER TABLE agent_fee_transaction ALTER COLUMN status DROP DEFAULT;
ALTER TABLE investor_coupon_schedule ALTER COLUMN coupon_status DROP DEFAULT;
ALTER TABLE bond_coupon_schedule ALTER COLUMN status DROP DEFAULT;
ALTER TABLE bond_trade ALTER COLUMN bond_trade_status DROP DEFAULT;
ALTER TABLE bond ALTER COLUMN bond_status DROP DEFAULT;
-- +goose StatementEnd
