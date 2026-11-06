-- 047_resource_daily
--
-- Per-workload compute usage history (RFC 0049 Phase 2, the Waybill daily
-- ledger; RFC 0050 digital COGS reads it later).
--
-- WHY
-- ===
-- Prometheus keeps 7 to 9 days. Waybill meters builds, addons and CI minutes,
-- but nothing kept what each workload actually used and requested, so that
-- history was lost every day. The waybill-aggregator now reads the previous
-- civil day (America/Mexico_City) from Prometheus once it has ended and writes
-- it here. Usage and requests only: no money, no allocation. Node cost
-- (cost_line, from the Hetzner invoice), the namespace -> product ->
-- engagement map and the allocation itself are later tables.
--
-- SHAPE
-- =====
-- One resource_daily row per (date, cluster, node, namespace, workload,
-- container). Node is part of the grain because node cost is allocated per
-- node; container because requests and limits are set per container, and
-- rightsizing (RFC 0049) changes one container's values. A per-workload view
-- is a GROUP BY away.
--
-- NULL means "not measured", never zero: a row whose pods had requests but
-- no usage sample keeps cpu_core_hours_used NULL. coverage is the fraction of
-- the day's 5-minute slots in which the row had at least one usage sample.
-- resource_daily_captures records, per day, that a capture ran and how much
-- of the day Prometheus held (usage from cadvisor, requests from
-- kube-state-metrics), so a NULL can be told apart from an outage.
--
-- A re-capture never degrades a day: the aggregator's upsert replaces a row
-- only when the new read saw at least as many samples, so a rerun after
-- Prometheus dropped part of the day keeps the earlier, fuller row.
--
-- Additive and idempotent (IF NOT EXISTS). Waybill shares this database and
-- owns no migrations of its own; until this runs the aggregator's capture
-- fails, logs, and retries on its next hourly check.
CREATE TABLE IF NOT EXISTS resource_daily (
    id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    date                      date NOT NULL,
    cluster                   text NOT NULL,
    node                      text NOT NULL,
    namespace                 text NOT NULL,
    workload_kind             text NOT NULL,
    workload_name             text NOT NULL,
    container                 text NOT NULL,
    pod_count                 integer NOT NULL,
    cpu_core_hours_used       numeric(20,6),
    cpu_core_hours_requested  numeric(20,6),
    cpu_limit_core_hours      numeric(20,6),
    cpu_cores_p95             numeric(20,6),
    cpu_cores_max             numeric(20,6),
    mem_gib_hours_used        numeric(20,6),
    mem_gib_hours_requested   numeric(20,6),
    mem_limit_gib_hours       numeric(20,6),
    mem_working_set_p95_bytes bigint,
    mem_working_set_max_bytes bigint,
    sample_count              integer NOT NULL,
    coverage                  numeric(5,4) NOT NULL,
    source                    text NOT NULL DEFAULT 'prometheus',
    captured_at               timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT resource_daily_coverage_range CHECK (coverage >= 0 AND coverage <= 1),
    CONSTRAINT resource_daily_grain UNIQUE (date, cluster, node, namespace, workload_kind, workload_name, container)
);

CREATE INDEX IF NOT EXISTS idx_resource_daily_namespace_date
    ON resource_daily (namespace, date);

COMMENT ON TABLE resource_daily IS
    'Daily per-workload, per-container, per-node CPU and memory usage and requests, captured from Prometheus by waybill-aggregator. date is the civil day in America/Mexico_City. NULL = not measured. No money (RFC 0049 Phase 2).';
COMMENT ON COLUMN resource_daily.workload_kind IS
    'Top-level owner: Deployment, StatefulSet, DaemonSet, CronJob, Job, ReplicaSet, a CRD kind (e.g. Cluster), Pod for unowned pods, or Namespace for pods aggregated per namespace on purpose (CI runners).';
COMMENT ON COLUMN resource_daily.coverage IS
    'Fraction of the day''s 5-minute slots in which this row had at least one usage sample (union over its pods).';
COMMENT ON COLUMN resource_daily.cpu_cores_p95 IS
    'Highest per-pod p95 of the 5-minute CPU rate during the day.';
COMMENT ON COLUMN resource_daily.mem_working_set_p95_bytes IS
    'Highest per-pod p95 of the 5-minute working-set samples during the day.';

-- Node capacity per day, usage only: allocatable vs requested vs used.
-- used is the node's root cgroup (pods plus system and k3s), so used minus
-- the sum of its resource_daily rows is the system share.
CREATE TABLE IF NOT EXISTS node_daily (
    id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    date                        date NOT NULL,
    cluster                     text NOT NULL,
    node                        text NOT NULL,
    pod_count                   integer NOT NULL,
    cpu_allocatable_core_hours  numeric(20,6),
    cpu_core_hours_requested    numeric(20,6),
    cpu_core_hours_used         numeric(20,6),
    cpu_cores_p95               numeric(20,6),
    cpu_cores_max               numeric(20,6),
    mem_allocatable_gib_hours   numeric(20,6),
    mem_gib_hours_requested     numeric(20,6),
    mem_gib_hours_used          numeric(20,6),
    mem_working_set_max_bytes   bigint,
    sample_count                integer NOT NULL,
    coverage                    numeric(5,4) NOT NULL,
    source                      text NOT NULL DEFAULT 'prometheus',
    captured_at                 timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT node_daily_coverage_range CHECK (coverage >= 0 AND coverage <= 1),
    CONSTRAINT node_daily_grain UNIQUE (date, cluster, node)
);

COMMENT ON TABLE node_daily IS
    'Daily per-node allocatable, requested and used CPU and memory, captured with resource_daily. Usage only, no money.';

-- One row per captured day: the capture ran and committed, and how much of
-- the day each source held.
CREATE TABLE IF NOT EXISTS resource_daily_captures (
    date               date NOT NULL,
    cluster            text NOT NULL,
    usage_coverage     numeric(5,4) NOT NULL,
    requests_coverage  numeric(5,4) NOT NULL,
    workload_rows      integer NOT NULL,
    node_rows          integer NOT NULL,
    source             text NOT NULL DEFAULT 'prometheus',
    captured_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (date, cluster),
    CONSTRAINT resource_daily_captures_coverage_range CHECK (
        usage_coverage >= 0 AND usage_coverage <= 1
        AND requests_coverage >= 0 AND requests_coverage <= 1
    )
);

COMMENT ON TABLE resource_daily_captures IS
    'Days waybill-aggregator captured into resource_daily/node_daily. usage_coverage: fraction of 5-minute slots with any cadvisor sample; requests_coverage: same for kube-state-metrics requests.';
