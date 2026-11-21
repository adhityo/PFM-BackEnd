-- Hand-written bond queries (service/bond_hooks.go, bond_tx.go, change_request.go).

-- ---------------------------------------------------------------- bond

-- name: SnapshotBond :exec
INSERT INTO bond_history (uuid, bond_id, bond_price, bond_code, rating_id, description, created_by, updated_by)
SELECT sqlc.arg('uuid'), b.id, b.bond_price, b.bond_code, b.rating_id, sqlc.narg('description')::text,
       sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM bond b WHERE b.id = sqlc.arg('id');

-- name: RefreshBondRating :exec
UPDATE bond SET rating_id = (
    SELECT r.rating_id FROM bond_rating r WHERE r.bond_id = $1 AND r.deleted_at IS NULL
    ORDER BY r.rating_date DESC NULLS LAST, r.id DESC LIMIT 1)
WHERE id = $1;

-- name: LockBond :one
SELECT id, uuid, bond_status, issuance_date, maturity_date, coupon, coupon_payment_periode, updated_at
FROM bond WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: GetBondState :one
SELECT bond_status, minimum_denomination FROM bond WHERE id = $1 AND deleted_at IS NULL;

-- name: SetBondStatus :exec
UPDATE bond SET bond_status = sqlc.arg('status'), updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');

-- name: CountApprovedBondTrades :one
SELECT count(*) FROM bond_trade WHERE bond_id = $1 AND bond_trade_status = 1 AND deleted_at IS NULL;

-- name: CountCouponSchedules :one
SELECT count(*) FROM bond_coupon_schedule WHERE bond_id = $1 AND deleted_at IS NULL;

-- name: ClearUnpaidCouponSchedules :exec
UPDATE bond_coupon_schedule SET deleted_at = now(), deleted_by = sqlc.arg('actor')
WHERE bond_id = sqlc.arg('bond_id') AND deleted_at IS NULL AND COALESCE(status, 0) = 0;

-- name: InsertCouponSchedule :exec
INSERT INTO bond_coupon_schedule (uuid, bond_id, coupon_date, coupon_rate, transaction_type, status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('bond_id'), sqlc.arg('coupon_date'), sqlc.arg('coupon_rate'), sqlc.arg('transaction_type'), 0,
        sqlc.arg('actor'), sqlc.arg('actor'));

-- name: RefreshBondCouponDates :exec
UPDATE bond
SET next_coupon_date = (SELECT min(s.coupon_date) FROM bond_coupon_schedule s
                        WHERE s.bond_id = $1 AND s.deleted_at IS NULL AND COALESCE(s.status, 0) = 0),
    last_coupon_date = (SELECT max(s.coupon_date) FROM bond_coupon_schedule s
                        WHERE s.bond_id = $1 AND s.deleted_at IS NULL AND s.status = 1)
WHERE id = $1;

-- ---------------------------------------------------------------- numbering

-- name: NextInvestorNumber :one
UPDATE configuration SET latest_investor_counter = COALESCE(latest_investor_counter, 0) + 1
WHERE id = (SELECT c.id FROM configuration c WHERE c.deleted_at IS NULL ORDER BY c.id LIMIT 1)
RETURNING latest_investor_counter, investor_number_template;

-- name: NextAgentNumber :one
UPDATE configuration SET latest_agent_counter = COALESCE(latest_agent_counter, 0) + 1
WHERE id = (SELECT c.id FROM configuration c WHERE c.deleted_at IS NULL ORDER BY c.id LIMIT 1)
RETURNING latest_agent_counter, agent_number_template;

-- name: SetInvestorNumber :exec
UPDATE investor SET investor_number = sqlc.arg('number'), investor_number_counter = sqlc.arg('counter') WHERE id = sqlc.arg('id');

-- name: SetAgentNumber :exec
UPDATE marketing_agent SET marketing_agent_number = sqlc.arg('number') WHERE id = sqlc.arg('id');

-- name: NextCertificate :one
UPDATE bond SET latest_certificate_counter = COALESCE(latest_certificate_counter, 0) + 1
WHERE id = $1
RETURNING latest_certificate_counter, bond_code, bond_series_number, bond_status, bond_periode_option_id;

-- name: CertificateTemplate :one
SELECT COALESCE(
    (SELECT o.bond_number_template FROM bond_periode_option o WHERE o.id = sqlc.narg('option_id') AND o.deleted_at IS NULL),
    (SELECT c.certificate_number_template FROM configuration c WHERE c.deleted_at IS NULL ORDER BY c.id LIMIT 1),
    '')::text AS template;

-- ---------------------------------------------------------------- bond trade

-- name: BankAccountInvestor :one
SELECT investor_id FROM bank_account WHERE id = $1 AND deleted_at IS NULL;

-- name: SnapshotBondTrade :exec
INSERT INTO bond_trade_data_history (
    uuid, bond_trade_id, accrued_interest, bond_code, bond_name, bond_trade_status, broker_name, certificate_counter,
    certificate_number, investment_length, investor_name, investor_number, marketing_agent_code, marketing_agent_name,
    nominal, number_of_days_coupon, purchased_nominal, purchased_price, settlement_date, settlement_days,
    subscription_number, total_proceed, trade_date, trade_price, trade_size, trade_value, transaction_fee,
    transaction_fee_percentage, transaction_type, validation_number, bond_id, broker_id, custodian_bank_id, investor_id,
    marketing_agent_id, portfolio_id, trade_type_id,
    previous_bond_code, previous_bond_name, previous_investment_length, previous_investor_name, previous_investor_number,
    previous_marketing_agent_code, previous_marketing_agent_name, previous_nominal, previous_settlement_date,
    previous_subscription_number, previous_trade_date, previous_transaction_fee, created_by, updated_by)
SELECT sqlc.arg('uuid'), t.id, t.accrued_interest, t.bond_code, t.bond_name, t.bond_trade_status, t.broker_name,
       t.certificate_counter, t.certificate_number, t.investment_length, t.investor_name, t.investor_number,
       t.marketing_agent_code, t.marketing_agent_name, t.nominal, t.number_of_days_coupon, t.purchased_nominal,
       t.purchased_price, t.settlement_date, t.settlement_days, t.subscription_number, t.total_proceed, t.trade_date,
       t.trade_price, t.trade_size, t.trade_value, t.transaction_fee, t.transaction_fee_percentage, t.transaction_type,
       t.validation_number, t.bond_id, t.broker_id, t.custodian_bank_id, t.investor_id, t.marketing_agent_id,
       t.portfolio_id, t.trade_type_id,
       sqlc.narg('previous_bond_code')::text, sqlc.narg('previous_bond_name')::text,
       sqlc.narg('previous_investment_length')::integer, sqlc.narg('previous_investor_name')::text,
       sqlc.narg('previous_investor_number')::text, sqlc.narg('previous_marketing_agent_code')::text,
       sqlc.narg('previous_marketing_agent_name')::text, sqlc.narg('previous_nominal')::numeric,
       sqlc.narg('previous_settlement_date')::date, sqlc.narg('previous_subscription_number')::text,
       sqlc.narg('previous_trade_date')::date, sqlc.narg('previous_transaction_fee')::numeric,
       sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM bond_trade t WHERE t.id = sqlc.arg('id');

-- name: LockBondTrade :one
SELECT id, uuid, bond_id, nominal, bond_trade_status, bond_periode_option_id, created_by, updated_by, updated_at
FROM bond_trade WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: ApproveBondTrade :exec
UPDATE bond_trade
SET bond_trade_status = 1, certificate_counter = sqlc.arg('counter'), certificate_number = sqlc.arg('number'),
    approved_by = sqlc.arg('actor'), approved_time = now(), approval_description = sqlc.narg('note'),
    updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: CancelBondTrade :exec
UPDATE bond_trade SET bond_trade_status = -1, approval_description = sqlc.narg('note'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: CreateInvestorCoupons :execrows
INSERT INTO investor_coupon_schedule (uuid, bond_coupon_schedule_id, bond_trade_id, coupon_value, coupon_status,
                                      transaction_type, created_by, updated_by)
SELECT gen_random_uuid(), s.id, $1, round($2::numeric * s.coupon_rate / 100, 4), 0, s.transaction_type, $4::text, $4::text
FROM bond_coupon_schedule s
WHERE s.bond_id = $3 AND s.deleted_at IS NULL AND COALESCE(s.status, 0) = 0;

-- name: AddScheduleCouponValues :exec
UPDATE bond_coupon_schedule s SET coupon_value = COALESCE(s.coupon_value, 0) + round($1::numeric * s.coupon_rate / 100, 4)
WHERE s.bond_id = $2 AND s.deleted_at IS NULL AND COALESCE(s.status, 0) = 0;

-- name: AddBondSubscription :exec
UPDATE bond b
SET total_fund = COALESCE(b.total_fund, 0) + $1::numeric,
    total_coupon = COALESCE(b.total_coupon, 0) + COALESCE((
        SELECT sum(round($1::numeric * s.coupon_rate / 100, 4)) FROM bond_coupon_schedule s
        WHERE s.bond_id = b.id AND s.deleted_at IS NULL AND COALESCE(s.status, 0) = 0 AND s.transaction_type <> 2), 0)
WHERE b.id = $2;

-- ---------------------------------------------------------------- coupons

-- name: LockCouponSchedule :one
SELECT id, uuid, bond_id, status, transaction_type, coupon_date, updated_at
FROM bond_coupon_schedule WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: PayScheduleInvestorCoupons :one
WITH paid AS (
    UPDATE investor_coupon_schedule
    SET coupon_status = 1, coupon_paid_value = coupon_value, payment_settlement_date = $2, updated_by = $3::text
    WHERE bond_coupon_schedule_id = $1 AND deleted_at IS NULL AND COALESCE(coupon_status, 0) = 0
    RETURNING coupon_value)
SELECT COALESCE(sum(coupon_value), 0)::numeric AS total FROM paid;

-- name: AddSchedulePaid :exec
UPDATE bond_coupon_schedule SET coupon_paid_value = COALESCE(coupon_paid_value, 0) + $2::numeric WHERE id = $1;

-- name: CloseScheduleIfSettled :execrows
UPDATE bond_coupon_schedule s SET status = 1, settlement_date = $2, updated_by = $3::text
WHERE s.id = $1 AND COALESCE(s.status, 0) = 0 AND NOT EXISTS (
    SELECT 1 FROM investor_coupon_schedule c
    WHERE c.bond_coupon_schedule_id = s.id AND c.deleted_at IS NULL AND COALESCE(c.coupon_status, 0) = 0);

-- name: AddBondPaid :exec
UPDATE bond
SET total_paid_coupon = COALESCE(total_paid_coupon, 0) + sqlc.arg('coupon')::numeric,
    total_matured_value = COALESCE(total_matured_value, 0) + sqlc.arg('principal')::numeric
WHERE id = sqlc.arg('id');

-- name: LockInvestorCoupon :one
SELECT c.id, c.uuid, c.bond_coupon_schedule_id, c.coupon_value, c.coupon_status, c.transaction_type, c.updated_at,
       s.bond_id
FROM investor_coupon_schedule c JOIN bond_coupon_schedule s ON s.id = c.bond_coupon_schedule_id
WHERE c.uuid = $1 AND c.deleted_at IS NULL FOR UPDATE OF c;

-- name: PayInvestorCoupon :exec
UPDATE investor_coupon_schedule
SET coupon_status = 1, coupon_paid_value = coupon_value, payment_settlement_date = sqlc.arg('settlement_date'),
    payment_reference_number = sqlc.narg('reference'), payment_document_file_uuid = sqlc.narg('file_uuid'),
    updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- ---------------------------------------------------------------- agent fee

-- name: LockAgentFee :one
SELECT id, uuid, status, outstanding_value, payment_document_file_uuid, updated_at
FROM agent_fee_transaction WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: PayAgentFee :exec
UPDATE agent_fee_transaction
SET status = 1, payment_date = $2, paid_value = $3::numeric, tax_paid_value = $4,
    outstanding_value = GREATEST(COALESCE(outstanding_value, 0) - $3::numeric, 0), updated_by = $5::text
WHERE id = $1;

-- ---------------------------------------------------------------- change requests (maker-checker)

-- name: CreateInvestorChangeRequest :one
INSERT INTO investor_data_history (
    uuid, investor_id, approval_status, status, address, address_city, address_postal_code, bank, bank_account_name,
    bank_account_number, bank_branch, corresponding_address, corresponding_address_city, corresponding_address_postal_code,
    description, email, identity_number, investor_name, investor_number, mobile_phone, npwp, telephone,
    marketing_agent_code, city_id, country_id, investor_category_id, province_id,
    previous_address, previous_address_city, previous_address_postal_code, previous_bank, previous_bank_account_name,
    previous_bank_account_number, previous_bank_branch, previous_corresponding_address, previous_corresponding_address_city,
    previous_corresponding_address_postal_code, previous_email, previous_identity_number, previous_investor_category_id,
    previous_investor_name, previous_mobile_phone, previous_npwp, previous_telephone, created_by, updated_by)
SELECT sqlc.arg('uuid'), i.id, 0, sqlc.narg('status')::smallint, sqlc.narg('address')::text, sqlc.narg('address_city')::text,
       sqlc.narg('address_postal_code')::text, sqlc.narg('bank')::text, sqlc.narg('bank_account_name')::text,
       sqlc.narg('bank_account_number')::text, sqlc.narg('bank_branch')::text, sqlc.narg('corresponding_address')::text,
       sqlc.narg('corresponding_address_city')::text, sqlc.narg('corresponding_address_postal_code')::text,
       sqlc.narg('description')::text, sqlc.narg('email')::text, sqlc.narg('identity_number')::text,
       sqlc.narg('investor_name')::text, i.investor_number, sqlc.narg('mobile_phone')::text, sqlc.narg('npwp')::text,
       sqlc.narg('telephone')::text, sqlc.narg('marketing_agent_code')::text, sqlc.narg('city_id')::bigint,
       sqlc.narg('country_id')::bigint, sqlc.narg('investor_category_id')::bigint, sqlc.narg('province_id')::bigint,
       i.address, i.address_city, i.address_postal_code, i.bank, i.bank_account_name, i.bank_account_number, i.bank_branch,
       i.corresponding_address, i.corresponding_address_city, i.corresponding_address_postal_code, i.email,
       i.identity_number, i.investor_category_id, i.investor_name, i.mobile_phone, i.npwp, i.telephone,
       sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM investor i WHERE i.id = sqlc.arg('investor_id')
RETURNING uuid;

-- name: LockInvestorChangeRequest :one
-- Locks the request and its investor; TargetUpdatedAt detects edits made after the request.
SELECT h.id, h.approval_status, h.created_by, h.created_at, i.updated_at AS target_updated_at
FROM investor_data_history h JOIN investor i ON i.id = h.investor_id AND i.deleted_at IS NULL
WHERE h.uuid = $1 AND h.deleted_at IS NULL FOR UPDATE;

-- name: ApplyInvestorChangeRequest :exec
UPDATE investor i
SET status = h.status, address = h.address, address_city = h.address_city, address_postal_code = h.address_postal_code,
    bank = h.bank, bank_account_name = h.bank_account_name, bank_account_number = h.bank_account_number,
    bank_branch = h.bank_branch, corresponding_address = h.corresponding_address,
    corresponding_address_city = h.corresponding_address_city,
    corresponding_address_postal_code = h.corresponding_address_postal_code, description = h.description,
    email = h.email, identity_number = h.identity_number, investor_name = h.investor_name, mobile_phone = h.mobile_phone,
    npwp = h.npwp, telephone = h.telephone, marketing_agent_code = h.marketing_agent_code, city_id = h.city_id,
    country_id = h.country_id, investor_category_id = h.investor_category_id, province_id = h.province_id,
    updated_by = sqlc.arg('actor')
FROM investor_data_history h
WHERE h.id = sqlc.arg('id') AND i.id = h.investor_id;

-- name: DecideInvestorChangeRequest :exec
UPDATE investor_data_history
SET approval_status = sqlc.arg('decision'), approval_notes = sqlc.narg('notes'), approved_by = sqlc.arg('actor'),
    approved_time = now(), approval_ip_address = sqlc.narg('ip'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: CreateAgentChangeRequest :one
INSERT INTO marketing_agent_data_history (
    uuid, marketing_agent_id, approval_status, status, address, bank, bank_account_name, bank_account_number, bank_branch,
    birthdate, description, email, handphone, identity_number, location, marketing_agent_name, marketing_agent_number,
    npwp, telephone,
    previous_address, previous_agent_name, previous_birthdate, previous_description, previous_email, previous_handphone,
    previous_identity_number, previous_location, previous_marketing_agent_number, previous_npwp, previous_telephone,
    created_by, updated_by)
SELECT sqlc.arg('uuid'), a.id, 0, sqlc.narg('status')::smallint, sqlc.narg('address')::text, sqlc.narg('bank')::text,
       sqlc.narg('bank_account_name')::text, sqlc.narg('bank_account_number')::text, sqlc.narg('bank_branch')::text,
       sqlc.narg('birthdate')::date, sqlc.narg('description')::text, sqlc.narg('email')::text, sqlc.narg('handphone')::text,
       sqlc.narg('identity_number')::text, sqlc.narg('location')::text, sqlc.narg('marketing_agent_name')::text,
       a.marketing_agent_number, sqlc.narg('npwp')::text, sqlc.narg('telephone')::text,
       a.address, a.marketing_agent_name, a.birthdate, a.description, a.email, a.handphone, a.identity_number, a.location,
       a.marketing_agent_number, a.npwp, a.telephone, sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM marketing_agent a WHERE a.id = sqlc.arg('agent_id')
RETURNING uuid;

-- name: LockAgentChangeRequest :one
SELECT h.id, h.approval_status, h.created_by, h.created_at, a.updated_at AS target_updated_at
FROM marketing_agent_data_history h JOIN marketing_agent a ON a.id = h.marketing_agent_id AND a.deleted_at IS NULL
WHERE h.uuid = $1 AND h.deleted_at IS NULL FOR UPDATE;

-- name: ApplyAgentChangeRequest :exec
UPDATE marketing_agent a
SET status = h.status, address = h.address, bank = h.bank, bank_account_name = h.bank_account_name,
    bank_account_number = h.bank_account_number, bank_branch = h.bank_branch, birthdate = h.birthdate,
    description = h.description, email = h.email, handphone = h.handphone, identity_number = h.identity_number,
    location = h.location, marketing_agent_name = h.marketing_agent_name, npwp = h.npwp, telephone = h.telephone,
    updated_by = sqlc.arg('actor')
FROM marketing_agent_data_history h
WHERE h.id = sqlc.arg('id') AND a.id = h.marketing_agent_id;

-- name: DecideAgentChangeRequest :exec
UPDATE marketing_agent_data_history
SET approval_status = sqlc.arg('decision'), approval_notes = sqlc.narg('notes'), approved_by = sqlc.arg('actor'),
    approved_time = now(), approval_ip_address = sqlc.narg('ip'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: LockInvestor :one
SELECT id FROM investor WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: CountPendingInvestorChangeRequests :one
SELECT count(*) FROM investor_data_history WHERE investor_id = $1 AND approval_status = 0 AND deleted_at IS NULL;

-- name: LockMarketingAgent :one
SELECT id FROM marketing_agent WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: CountPendingAgentChangeRequests :one
SELECT count(*) FROM marketing_agent_data_history WHERE marketing_agent_id = $1 AND approval_status = 0 AND deleted_at IS NULL;
