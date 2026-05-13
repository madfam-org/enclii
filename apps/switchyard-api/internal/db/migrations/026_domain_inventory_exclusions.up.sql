-- 026_domain_inventory_exclusions
--
-- Persists reviewed domain-inventory exclusions so /v1/domains/reconcile can
-- distinguish actionable route drift from catalog/system hostnames that are
-- intentionally not custom_domains rows.

CREATE TABLE IF NOT EXISTS public.domain_inventory_exclusions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    hostname_pattern character varying(255) DEFAULT '*'::character varying NOT NULL,
    source character varying(100) DEFAULT ''::character varying NOT NULL,
    route_target character varying(255) DEFAULT ''::character varying NOT NULL,
    classification character varying(100) NOT NULL,
    reason text NOT NULL,
    active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT domain_inventory_exclusions_pkey PRIMARY KEY (id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_domain_inventory_exclusions_unique
    ON public.domain_inventory_exclusions USING btree (hostname_pattern, source, route_target);

CREATE INDEX IF NOT EXISTS idx_domain_inventory_exclusions_active
    ON public.domain_inventory_exclusions USING btree (active);

INSERT INTO public.domain_inventory_exclusions
    (hostname_pattern, source, route_target, classification, reason, active)
VALUES
    (
        '*',
        'kubernetes_configmap',
        'enclii/status-config-madfam',
        'status_page_catalog',
        'status-config-madfam is an observed service catalog, not proof of a live route',
        true
    )
ON CONFLICT (hostname_pattern, source, route_target) DO UPDATE
SET classification = EXCLUDED.classification,
    reason = EXCLUDED.reason,
    active = EXCLUDED.active,
    updated_at = now();
