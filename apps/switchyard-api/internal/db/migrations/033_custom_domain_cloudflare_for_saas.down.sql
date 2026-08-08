DROP INDEX IF EXISTS idx_custom_domains_custom_hostname_id;

ALTER TABLE custom_domains
    DROP COLUMN IF EXISTS custom_hostname_id,
    DROP COLUMN IF EXISTS custom_hostname_status,
    DROP COLUMN IF EXISTS custom_hostname_ssl_status,
    DROP COLUMN IF EXISTS pending_dns_records,
    DROP COLUMN IF EXISTS provisioning_error,
    DROP COLUMN IF EXISTS provisioning_checked_at;
