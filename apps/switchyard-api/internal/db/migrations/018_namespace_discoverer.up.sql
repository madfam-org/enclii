-- Namespace Discoverer (Parity Audit Gap #2):
--   Closes the operator-visibility gap between K8s cluster reality and
--   the Enclii services table.
--
--   - discovered_orphans: workloads found in the cluster that have no
--     matching service record. Updated by the namespace_discoverer
--     reconciler every 5 minutes.
--   - services.zombie_since: set when a service has k8s_namespace pinned
--     but no live workload backing it. Cleared when the workload reappears.
--   - services.last_reconciled_at: timestamp of the last namespace_discoverer
--     pass that observed (orphan or healthy) the workload. Distinct from
--     last_health_check (which is set by deployment-status reconciler).
--
-- The pre-existing services columns (k8s_namespace, desired_replicas,
-- ready_replicas, last_health_check) are reused. We do NOT introduce
-- duplicate k8s_replicas_ready / k8s_replicas_desired columns — the
-- existing columns serve the same purpose and adding parallel ones would
-- create a write-fan-out hazard between the two reconcilers.

CREATE TABLE IF NOT EXISTS discovered_orphans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace VARCHAR(253) NOT NULL,
    name VARCHAR(253) NOT NULL,
    kind VARCHAR(64) NOT NULL,
    image VARCHAR(1024) NOT NULL DEFAULT '',
    replicas_desired INT NOT NULL DEFAULT 0,
    replicas_ready INT NOT NULL DEFAULT 0,
    first_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(namespace, name, kind)
);

CREATE INDEX IF NOT EXISTS idx_discovered_orphans_last_seen
    ON discovered_orphans(last_seen);

ALTER TABLE services
    ADD COLUMN IF NOT EXISTS zombie_since TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_reconciled_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_services_zombie_since
    ON services(zombie_since)
    WHERE zombie_since IS NOT NULL;

COMMENT ON TABLE discovered_orphans IS
    'Workloads observed in the cluster with no matching services row. '
    'Populated by reconciler/namespace_discoverer every RECONCILER_NAMESPACE_DISCOVERY_INTERVAL '
    '(default 5m). Rows older than 24h with no last_seen update are reaped.';

COMMENT ON COLUMN services.zombie_since IS
    'When non-null, the service has k8s_namespace pinned but the namespace '
    'discoverer found no matching Deployment/StatefulSet on the last pass.';

COMMENT ON COLUMN services.last_reconciled_at IS
    'Last time the namespace discoverer observed this service in the cluster '
    '(healthy match). Distinct from last_health_check which is updated by the '
    'deployment-status reconciler.';
