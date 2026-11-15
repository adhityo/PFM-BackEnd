-- Hand-written pension queries (service/pension_hooks.go, pension_tx.go).

-- ---------------------------------------------------------------- lookups

-- name: GetMemberClientID :one
SELECT client_id FROM member WHERE id = $1 AND deleted_at IS NULL;

-- name: GetDependantMemberID :one
SELECT member_id FROM dependant WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPremiumMemberID :one
SELECT member_id FROM member_premium_payment WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPayoutMemberID :one
SELECT member_id FROM member_benefit_payout WHERE id = $1 AND deleted_at IS NULL;

-- name: GetTaxReportMemberID :one
SELECT member_id FROM tax_report WHERE id = $1 AND deleted_at IS NULL;

-- name: GetInvoiceState :one
SELECT invoice_status, client_id FROM invoice WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPayoutBatchStatus :one
SELECT payout_batch_status FROM benefit_payout_batch WHERE id = $1 AND deleted_at IS NULL;

-- name: SetMemberNumber :exec
UPDATE member SET member_number = sqlc.arg('number') WHERE id = sqlc.arg('id');

-- name: AdvisoryLock :exec
-- Serialises "find or create" of a period document (invoice, payout batch) until commit.
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg('key')::text, 0));

-- ---------------------------------------------------------------- invoice

-- name: AssignInvoiceDefaults :exec
UPDATE invoice
SET invoice_number = COALESCE(invoice_number, sqlc.arg('number')::text),
    invoice_date = COALESCE(invoice_date, sqlc.arg('invoice_date')::date),
    invoice_due_date = COALESCE(invoice_due_date, sqlc.arg('due_date')::date)
WHERE id = sqlc.arg('id');

-- name: RefreshInvoiceTotals :one
UPDATE invoice i
SET invoice_value = s.total, total_member = s.members,
    invoice_total_value = s.total + COALESCE(i.invoice_tax_value, 0)
FROM (SELECT COALESCE(sum(p.payment_nominal), 0)::numeric AS total, count(DISTINCT p.member_id)::integer AS members
      FROM member_premium_payment p WHERE p.invoice_id = $1 AND p.deleted_at IS NULL) s
WHERE i.id = $1
RETURNING i.invoice_total_value;

-- name: SetInvoiceBillingDescription :exec
UPDATE invoice SET billing_description = sqlc.arg('description') WHERE id = sqlc.arg('id');

-- name: FindActiveInvoice :one
SELECT id, uuid, invoice_status FROM invoice
WHERE client_id = sqlc.arg('client_id') AND year = sqlc.arg('year') AND month = sqlc.arg('month')
  AND deleted_at IS NULL AND COALESCE(invoice_status, 0) <> 3
FOR UPDATE;

-- name: InsertInvoice :one
INSERT INTO invoice (uuid, client_id, year, month, invoice_status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('client_id'), sqlc.arg('year'), sqlc.arg('month'), 0, sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING id;

-- name: InsertInvoiceLines :execrows
-- One premium line per active member of the client employed during the period.
INSERT INTO member_premium_payment (uuid, invoice_id, member_id, month, year, payment_nominal, self_payment_nominal,
                                    allowance_payment_nominal, percentage, status, created_by, updated_by)
SELECT gen_random_uuid(), sqlc.arg('invoice_id'), m.id, sqlc.arg('month'), sqlc.arg('year'), m.latest_pension_premium,
       m.latest_pension_premium, 0, round(m.latest_pension_premium * 100 / NULLIF(m.latest_salary, 0), 6), 0,
       sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM member m
WHERE m.client_id = sqlc.arg('client_id') AND m.deleted_at IS NULL AND m.status = 1
  AND COALESCE(m.latest_pension_premium, 0) > 0
  AND (m.start_working_date IS NULL OR m.start_working_date <= sqlc.arg('period_end')::date)
  AND (m.retired_date IS NULL OR m.retired_date >= sqlc.arg('period_start')::date)
  AND NOT EXISTS (SELECT 1 FROM member_premium_payment p
                  WHERE p.invoice_id = sqlc.arg('invoice_id') AND p.member_id = m.id AND p.deleted_at IS NULL);

-- name: LockInvoice :one
SELECT id, uuid, invoice_status, client_id, updated_at FROM invoice WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: CountInvoiceLines :one
SELECT count(*) AS lines, count(*) FILTER (WHERE COALESCE(status, 0) = 1) AS paid
FROM member_premium_payment WHERE invoice_id = $1 AND deleted_at IS NULL;

-- name: SetInvoiceStatus :exec
UPDATE invoice SET invoice_status = sqlc.arg('status'), updated_by = sqlc.arg('actor') WHERE id = sqlc.arg('id');

-- name: MarkInvoicePaidIfSettled :execrows
UPDATE invoice i SET invoice_status = 2, updated_by = sqlc.arg('actor')::text
WHERE i.id = sqlc.arg('id') AND i.invoice_status = 1 AND NOT EXISTS (
    SELECT 1 FROM member_premium_payment p
    WHERE p.invoice_id = i.id AND p.deleted_at IS NULL AND COALESCE(p.status, 0) = 0);

-- name: LockPremium :one
SELECT id, uuid, status, invoice_id, member_id, payment_nominal, updated_at
FROM member_premium_payment WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: PayPremium :exec
UPDATE member_premium_payment
SET status = 1, payment_date = sqlc.arg('payment_date'), paid_nominal = sqlc.arg('paid')::numeric,
    updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: AddMemberPremium :exec
UPDATE member SET total_pension_premium_value = COALESCE(total_pension_premium_value, 0) + sqlc.arg('amount')::numeric
WHERE id = sqlc.arg('id');

-- name: MarkInvoicePeriodGenerated :exec
UPDATE configuration
SET latest_generated_invoice_year = sqlc.arg('year'), latest_generated_invoice_month = sqlc.arg('month')
WHERE id = (SELECT c.id FROM configuration c WHERE c.deleted_at IS NULL ORDER BY c.id LIMIT 1)
  AND COALESCE(latest_generated_invoice_year, 0) * 100 + COALESCE(latest_generated_invoice_month, 0)
      < sqlc.arg('year')::integer * 100 + sqlc.arg('month')::integer;

-- ---------------------------------------------------------------- benefit payout

-- name: AssignPayoutBatchDefaults :exec
UPDATE benefit_payout_batch
SET batch_number = COALESCE(batch_number, sqlc.arg('number')::text),
    payout_generated_date = COALESCE(payout_generated_date, sqlc.arg('generated_date')::date),
    benefit_payout_batch_name = COALESCE(benefit_payout_batch_name, sqlc.arg('name')::text)
WHERE id = sqlc.arg('id');

-- name: RefreshPayoutBatchTotals :one
UPDATE benefit_payout_batch b
SET benefit_payout_total = s.total, total_beneficiaries = s.recipients
FROM (SELECT COALESCE(sum(p.final_payout_value), 0)::numeric AS total, count(*)::integer AS recipients
      FROM member_benefit_payout p WHERE p.benefit_payout_batch_id = $1 AND p.deleted_at IS NULL) s
WHERE b.id = $1
RETURNING b.benefit_payout_total;

-- name: SetPayoutBatchValueDescription :exec
UPDATE benefit_payout_batch SET value_description = sqlc.arg('description') WHERE id = sqlc.arg('id');

-- name: FindActivePayoutBatch :one
SELECT id, uuid, payout_batch_status FROM benefit_payout_batch
WHERE year = sqlc.arg('year') AND month = sqlc.arg('month') AND deleted_at IS NULL
  AND COALESCE(payout_batch_status, 0) <> 3
FOR UPDATE;

-- name: InsertPayoutBatch :one
INSERT INTO benefit_payout_batch (uuid, year, month, payout_batch_status, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('year'), sqlc.arg('month'), 0, sqlc.arg('actor'), sqlc.arg('actor'))
RETURNING id;

-- name: InsertPayoutLines :execrows
-- Retired members (status 2) and dependants with a monthly benefit.
INSERT INTO member_benefit_payout (uuid, benefit_payout_batch_id, member_id, dependant_id, month, year,
                                   benefit_payout_value, benefit_payout_tax_value, final_payout_value, payment_method,
                                   member_benefit_payout_status, created_by, updated_by)
SELECT gen_random_uuid(), sqlc.arg('batch_id'), r.member_id, r.dependant_id, sqlc.arg('month'), sqlc.arg('year'),
       r.amount, 0, r.amount, r.payment_method, 0, sqlc.arg('actor')::text, sqlc.arg('actor')::text
FROM (
    SELECT m.id AS member_id, NULL::bigint AS dependant_id, m.pension_benefit_value AS amount, m.payment_method
    FROM member m
    WHERE m.deleted_at IS NULL AND m.status = 2 AND COALESCE(m.pension_benefit_value, 0) > 0
      AND (m.retired_date IS NULL OR m.retired_date <= sqlc.arg('period_end')::date)
    UNION ALL
    SELECT d.member_id, d.id, d.pension_benefit_value, m.payment_method
    FROM dependant d JOIN member m ON m.id = d.member_id AND m.deleted_at IS NULL
    WHERE d.deleted_at IS NULL AND COALESCE(d.pension_benefit_value, 0) > 0
) r
WHERE NOT EXISTS (SELECT 1 FROM member_benefit_payout p
                  WHERE p.benefit_payout_batch_id = sqlc.arg('batch_id') AND p.member_id = r.member_id
                    AND COALESCE(p.dependant_id, 0) = COALESCE(r.dependant_id, 0) AND p.deleted_at IS NULL);

-- name: LockPayoutBatch :one
SELECT id, uuid, payout_batch_status, total_beneficiaries, created_by, updated_at
FROM benefit_payout_batch WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: ApprovePayoutBatch :exec
UPDATE benefit_payout_batch
SET payout_batch_status = 1, payout_approval_date = sqlc.arg('approval_date'), approval_description = sqlc.narg('note'),
    updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: SetPayoutBatchStatus :exec
UPDATE benefit_payout_batch SET payout_batch_status = sqlc.arg('status'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: SetPayoutLinesStatus :exec
UPDATE member_benefit_payout
SET member_benefit_payout_status = sqlc.arg('status'),
    benefit_payout_date = COALESCE(sqlc.narg('payout_date')::date, benefit_payout_date), updated_by = sqlc.arg('actor')
WHERE benefit_payout_batch_id = sqlc.arg('batch_id') AND deleted_at IS NULL;

-- name: MarkPayoutPeriodGenerated :exec
UPDATE configuration
SET latest_generated_payment_year = sqlc.arg('year'), latest_generated_payment_month = sqlc.arg('month')
WHERE id = (SELECT c.id FROM configuration c WHERE c.deleted_at IS NULL ORDER BY c.id LIMIT 1)
  AND COALESCE(latest_generated_payment_year, 0) * 100 + COALESCE(latest_generated_payment_month, 0)
      < sqlc.arg('year')::integer * 100 + sqlc.arg('month')::integer;

-- ---------------------------------------------------------------- member units

-- name: LockMemberTransaction :one
SELECT id, uuid, member_portfolio_id, trade_type_id, transaction_status, transaction_date, total_unit,
       total_transaction_value, effective_nav, updated_at
FROM member_portfolio_transaction WHERE uuid = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: LockMemberPortfolio :one
SELECT id, total_unit, total_investment, latest_transaction_date FROM member_portfolio
WHERE id = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: ApplyMemberPortfolio :exec
UPDATE member_portfolio
SET total_unit = sqlc.arg('total_unit'), total_investment = sqlc.arg('total_investment'),
    current_net_asset_value = sqlc.arg('nav'), latest_transaction_date = sqlc.arg('date'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: InsertMemberPortfolioHistory :exec
INSERT INTO member_portfolio_history (uuid, member_portfolio_id, total_unit, net_asset_value, total_investment_value,
                                      portfolio_date, portfolio_status, description, created_by, updated_by)
VALUES (sqlc.arg('uuid'), sqlc.arg('member_portfolio_id'), sqlc.arg('total_unit'), sqlc.arg('nav'),
        sqlc.arg('total_investment'), sqlc.arg('date'), 1, sqlc.narg('description'), sqlc.arg('actor'), sqlc.arg('actor'));

-- name: SetMemberTransactionStatus :exec
UPDATE member_portfolio_transaction SET transaction_status = sqlc.arg('status'), updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id');

-- name: LockInvoiceByID :one
SELECT invoice_status FROM invoice WHERE id = $1 FOR UPDATE;
