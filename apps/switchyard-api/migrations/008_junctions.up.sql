-- Junctions: Routing, ingress, and certificate management for services.

CREATE TABLE junctions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE CASCADE,
    domain VARCHAR(255) NOT NULL,
    path VARCHAR(255) NOT NULL DEFAULT '/',
    protocol VARCHAR(10) NOT NULL DEFAULT 'https',
    tls_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    tls_issuer VARCHAR(50) NOT NULL DEFAULT 'letsencrypt-prod',
    tls_cert_secret VARCHAR(255),
    tls_min_version VARCHAR(5) DEFAULT '1.2',
    tls_force_redirect BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(domain, path)
);

CREATE INDEX idx_junctions_project ON junctions(project_id);
CREATE INDEX idx_junctions_service ON junctions(service_id);
CREATE INDEX idx_junctions_domain ON junctions(domain);
