-- 041_junction_environment
--
-- Bind each junction to the ENVIRONMENT whose workload serves it, not only to
-- a service.
--
-- WHY
-- ===
-- A junction (hostname -> service) is the only model the tunnel-route
-- reconcilers read, and it carried no environment. Every reconciler therefore
-- derived the backend as `<service>.<production namespace>.svc`, so a staging
-- hostname could never be planned correctly: `tunnels-apply` proposed moving
-- live staging routes onto the production workload, and applied it when asked.
--
-- SHAPE
-- =====
-- NULLABLE. NULL keeps today's behaviour for every existing row: the planner
-- falls back to the environment recorded on the hostname's custom_domains row,
-- then to production. A value is written only by an explicit, audited rebind
-- (`enclii ops junctions rebind`) or by the provisioner that already knows the
-- environment it is provisioning for.
--
-- ON DELETE SET NULL, not CASCADE: deleting an environment must not silently
-- delete the junctions (and with them the hostname's routing record) that
-- pointed at it. The junction falls back to the default derivation, and the
-- repoint guard keeps that fallback from moving a live route.
ALTER TABLE public.junctions
    ADD COLUMN IF NOT EXISTS environment_id uuid
        REFERENCES public.environments(id) ON DELETE SET NULL;

COMMENT ON COLUMN public.junctions.environment_id IS
    'Environment whose workload serves this hostname. NULL = fall back to the hostname''s custom_domains environment, then production.';

CREATE INDEX IF NOT EXISTS idx_junctions_environment_id
    ON public.junctions USING btree (environment_id);
