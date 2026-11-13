-- name: GetConfiguration :one
SELECT c.id, c.uuid, c.system_usage_type, c.company_name, c.company_address, c.default_prorate_days,
       c.agent_number_template, c.certificate_number_template, c.investor_number_template,
       c.certificate_latest_counter, c.latest_agent_counter, c.latest_investor_counter, c.latest_agreement_counter,
       c.latest_generated_invoice_month, c.latest_generated_invoice_year,
       c.latest_generated_payment_month, c.latest_generated_payment_year,
       c.emitent_code, c.proxy_api_url, c.is_using_qr_logo, c.qr_logo_file_id, f.uuid AS qr_logo_file_uuid,
       c.is_using_agreement, c.updated_at
FROM configuration c
LEFT JOIN file_object f ON f.id = c.qr_logo_file_id AND f.deleted_at IS NULL
WHERE c.deleted_at IS NULL
ORDER BY c.id
LIMIT 1;

-- name: LockConfiguration :one
SELECT id, updated_at FROM configuration WHERE deleted_at IS NULL ORDER BY id LIMIT 1 FOR UPDATE;

-- name: UpdateConfiguration :execrows
UPDATE configuration
SET system_usage_type = sqlc.narg('system_usage_type'), company_name = sqlc.narg('company_name'),
    company_address = sqlc.narg('company_address'), default_prorate_days = sqlc.narg('default_prorate_days'),
    agent_number_template = sqlc.narg('agent_number_template'),
    certificate_number_template = sqlc.narg('certificate_number_template'),
    investor_number_template = sqlc.narg('investor_number_template'), emitent_code = sqlc.narg('emitent_code'),
    proxy_api_url = sqlc.narg('proxy_api_url'), is_using_qr_logo = sqlc.arg('is_using_qr_logo'),
    qr_logo_file_id = sqlc.narg('qr_logo_file_id'), is_using_agreement = sqlc.arg('is_using_agreement'),
    updated_by = sqlc.arg('actor')
WHERE id = sqlc.arg('id') AND updated_at = sqlc.arg('expected_updated_at');

-- name: EnsureConfiguration :exec
INSERT INTO configuration (uuid, created_by, updated_by)
SELECT sqlc.arg('uuid'), 'system', 'system'
WHERE NOT EXISTS (SELECT 1 FROM configuration WHERE deleted_at IS NULL);
