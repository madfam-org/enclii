-- Cloudflare for SaaS custom hostnames.
--
-- Client-owned domains (the client keeps their registrar and nameservers) are
-- provisioned as Cloudflare custom hostnames on our fallback-origin zone
-- instead of as a zone + CNAME. Such a domain is NOT ready until the client
-- creates the records we hand them, so the record has to carry both the
-- Cloudflare-reported state and the outstanding client action.

ALTER TABLE custom_domains
    ADD COLUMN IF NOT EXISTS custom_hostname_id TEXT,
    ADD COLUMN IF NOT EXISTS custom_hostname_status TEXT,
    ADD COLUMN IF NOT EXISTS custom_hostname_ssl_status TEXT,
    ADD COLUMN IF NOT EXISTS pending_dns_records JSONB,
    ADD COLUMN IF NOT EXISTS provisioning_error TEXT,
    ADD COLUMN IF NOT EXISTS provisioning_checked_at TIMESTAMPTZ;

COMMENT ON COLUMN custom_domains.custom_hostname_id IS
    'Cloudflare for SaaS custom hostname id on the fallback-origin zone. NULL for zone+CNAME domains.';
COMMENT ON COLUMN custom_domains.custom_hostname_status IS
    'Hostname status as last reported by Cloudflare (pending, pending_validation, active, moved, deleted, blocked). Never inferred locally.';
COMMENT ON COLUMN custom_domains.custom_hostname_ssl_status IS
    'Certificate status as last reported by Cloudflare (pending_validation, pending_issuance, active, ...). Never inferred locally.';
COMMENT ON COLUMN custom_domains.pending_dns_records IS
    'JSON array of records the domain owner must still create ({purpose,type,name,value}). Non-empty means we are waiting on the client.';
COMMENT ON COLUMN custom_domains.provisioning_error IS
    'Last provisioning failure. Deploy-path provisioning does not block the deploy, so the failure is stored here to stay legible.';
COMMENT ON COLUMN custom_domains.provisioning_checked_at IS
    'When custom_hostname_status/ssl_status were last read from Cloudflare.';

-- Reverse lookup from a Cloudflare custom hostname id back to the domain row.
CREATE INDEX IF NOT EXISTS idx_custom_domains_custom_hostname_id
    ON custom_domains (custom_hostname_id)
    WHERE custom_hostname_id IS NOT NULL;
