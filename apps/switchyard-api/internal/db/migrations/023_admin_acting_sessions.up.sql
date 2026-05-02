-- 023_admin_acting_sessions
--
-- White-glove operator support: master admins (users with the global `admin`
-- role on their JWT) can enter a tenant ("act as") for a bounded session and
-- have every subsequent query in that session filtered to the tenant's team.
-- See claudedocs/master-admin-tenant-switching.md for the full design.
--
-- This migration is intentionally minimal: it adds the session bookkeeping
-- and the audit-trail enrichment column. The handlers + middleware that
-- consume it land in code, not in further migrations.

CREATE TABLE IF NOT EXISTS admin_acting_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_team_id  UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    ended_at        TIMESTAMPTZ,
    reason          TEXT,
    client_ip       INET,
    user_agent      TEXT
);

-- Hot path: "is there an active session for this admin?" — runs on every
-- authed request when the ax_acting_as cookie is present. Partial index
-- keeps it cheap even as ended sessions accumulate.
CREATE INDEX IF NOT EXISTS idx_admin_acting_sessions_active
    ON admin_acting_sessions (admin_user_id)
    WHERE ended_at IS NULL;

-- Lookup-by-tenant for "who is currently acting as this team?" displays.
CREATE INDEX IF NOT EXISTS idx_admin_acting_sessions_tenant
    ON admin_acting_sessions (tenant_team_id, started_at DESC);

-- Audit-trail enrichment so /v1/audit can show "acted on behalf of <tenant>".
ALTER TABLE audit_logs
    ADD COLUMN IF NOT EXISTS acting_on_behalf_of_team_id UUID REFERENCES teams(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_audit_logs_acting_team
    ON audit_logs (acting_on_behalf_of_team_id)
    WHERE acting_on_behalf_of_team_id IS NOT NULL;
