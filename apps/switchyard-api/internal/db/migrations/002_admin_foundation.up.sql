-- Admin Foundation Migration
-- Universal Control Plane tables for bare metal fleet, infrastructure composition,
-- multi-tenancy, governance, and cost tracking.

-- Helper function for auto-updating updated_at timestamps
CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- 1. Clusters
CREATE TABLE IF NOT EXISTS clusters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    type VARCHAR(50) NOT NULL DEFAULT 'k3s' CHECK (type IN ('k3s', 'k8s', 'vcluster')),
    endpoint VARCHAR(512),
    kubeconfig_secret_ref VARCHAR(512),
    region VARCHAR(100),
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'degraded', 'offline', 'deleting')),
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_clusters_updated_at
    BEFORE UPDATE ON clusters
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- 2. Bare Metal Hosts
CREATE TABLE IF NOT EXISTS bare_metal_hosts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    cluster_id UUID REFERENCES clusters(id) ON DELETE SET NULL,
    bmc_address VARCHAR(512) NOT NULL,
    bmc_credentials_ref VARCHAR(512) NOT NULL,
    mac_address VARCHAR(17),
    boot_mode VARCHAR(20) DEFAULT 'UEFI' CHECK (boot_mode IN ('UEFI', 'Legacy', 'UEFISecureBoot')),
    state VARCHAR(50) NOT NULL DEFAULT 'discovered' CHECK (state IN ('discovered', 'inspecting', 'available', 'provisioning', 'provisioned', 'deprovisioning', 'error')),
    power_state VARCHAR(20) DEFAULT 'unknown' CHECK (power_state IN ('on', 'off', 'unknown')),
    hardware_profile JSONB DEFAULT '{}',
    firmware_version VARCHAR(100),
    root_device_hints JSONB DEFAULT '{}',
    raid_config JSONB DEFAULT '{}',
    cost_per_hour_cents INTEGER NOT NULL DEFAULT 0,
    last_inspection_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bmh_cluster_id ON bare_metal_hosts(cluster_id);
CREATE INDEX idx_bmh_state ON bare_metal_hosts(state);

CREATE TRIGGER set_bare_metal_hosts_updated_at
    BEFORE UPDATE ON bare_metal_hosts
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- 3. Managed Resources (Crossplane)
CREATE TABLE IF NOT EXISTS managed_resources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    api_version VARCHAR(255) NOT NULL,
    kind VARCHAR(255) NOT NULL,
    provider VARCHAR(100) NOT NULL,
    cluster_id UUID REFERENCES clusters(id) ON DELETE CASCADE,
    management_policy VARCHAR(50) NOT NULL DEFAULT 'FullControl' CHECK (management_policy IN ('FullControl', 'ObserveOnly', 'OrphanOnDelete')),
    sync_status VARCHAR(50) NOT NULL DEFAULT 'Unknown' CHECK (sync_status IN ('Synced', 'OutOfSync', 'Unknown', 'Error')),
    conditions JSONB DEFAULT '[]',
    spec_hash VARCHAR(64),
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mr_cluster_id ON managed_resources(cluster_id);
CREATE INDEX idx_mr_provider ON managed_resources(provider);
CREATE INDEX idx_mr_sync_status ON managed_resources(sync_status);

CREATE TRIGGER set_managed_resources_updated_at
    BEFORE UPDATE ON managed_resources
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- 4. Virtual Clusters
CREATE TABLE IF NOT EXISTS virtual_clusters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    host_cluster_id UUID NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
    tenant_id VARCHAR(255),
    namespace VARCHAR(255) NOT NULL,
    k8s_version VARCHAR(50),
    status VARCHAR(50) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'creating', 'running', 'paused', 'deleting', 'error')),
    helm_release_name VARCHAR(255),
    resource_quota JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_vc_host_cluster_id ON virtual_clusters(host_cluster_id);
CREATE INDEX idx_vc_tenant_id ON virtual_clusters(tenant_id);

CREATE TRIGGER set_virtual_clusters_updated_at
    BEFORE UPDATE ON virtual_clusters
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- 5. Propagation Policies
CREATE TABLE IF NOT EXISTS propagation_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL UNIQUE,
    cluster_ids UUID[] NOT NULL DEFAULT '{}',
    resource_selectors JSONB DEFAULT '[]',
    placement_strategy VARCHAR(50) NOT NULL DEFAULT 'Spread' CHECK (placement_strategy IN ('Spread', 'Binpack', 'GPUAffinity')),
    gpu_required BOOLEAN NOT NULL DEFAULT FALSE,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER set_propagation_policies_updated_at
    BEFORE UPDATE ON propagation_policies
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

-- 6. Drift Events
CREATE TABLE IF NOT EXISTS drift_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source VARCHAR(50) NOT NULL CHECK (source IN ('argocd', 'crossplane', 'manual')),
    resource_type VARCHAR(255) NOT NULL,
    resource_name VARCHAR(255) NOT NULL,
    cluster_id UUID REFERENCES clusters(id) ON DELETE SET NULL,
    drift_details JSONB DEFAULT '{}',
    severity VARCHAR(20) NOT NULL DEFAULT 'medium' CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    resolved BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at TIMESTAMPTZ,
    detected_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_drift_cluster_id ON drift_events(cluster_id);
CREATE INDEX idx_drift_resolved ON drift_events(resolved);
CREATE INDEX idx_drift_severity ON drift_events(severity);

-- 7. Cost Allocations
CREATE TABLE IF NOT EXISTS cost_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bare_metal_host_id UUID NOT NULL REFERENCES bare_metal_hosts(id) ON DELETE CASCADE,
    tenant_id VARCHAR(255) NOT NULL,
    allocation_percent NUMERIC(5,2) NOT NULL CHECK (allocation_percent >= 0 AND allocation_percent <= 100),
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    cost_cents INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_cost_bmh_id ON cost_allocations(bare_metal_host_id);
CREATE INDEX idx_cost_tenant ON cost_allocations(tenant_id);
CREATE INDEX idx_cost_period ON cost_allocations(period_start, period_end);
