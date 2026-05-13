-- 025_junctions
--
-- Adds the routing/junction persistence table used by the Switchyard
-- junction API. The API has shipped handlers and repository code that expect
-- this table, so the migration is fully idempotent to repair live databases
-- that were deployed without it.

CREATE TABLE IF NOT EXISTS public.junctions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL REFERENCES public.projects(id) ON DELETE CASCADE,
    service_id uuid NOT NULL REFERENCES public.services(id) ON DELETE CASCADE,
    domain character varying(255) NOT NULL,
    path character varying(500) DEFAULT '/'::character varying NOT NULL,
    protocol character varying(20) DEFAULT 'https'::character varying NOT NULL,
    tls_enabled boolean DEFAULT true NOT NULL,
    tls_issuer character varying(100) DEFAULT 'letsencrypt-prod'::character varying NOT NULL,
    tls_cert_secret character varying(255),
    tls_min_version character varying(10) DEFAULT '1.2'::character varying,
    tls_force_redirect boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    CONSTRAINT junctions_pkey PRIMARY KEY (id),
    CONSTRAINT junctions_protocol_check CHECK ((protocol)::text = ANY ((ARRAY['http'::character varying, 'https'::character varying, 'grpc'::character varying])::text[]))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_junctions_domain_path
    ON public.junctions USING btree (domain, path);

CREATE INDEX IF NOT EXISTS idx_junctions_project_id
    ON public.junctions USING btree (project_id);

CREATE INDEX IF NOT EXISTS idx_junctions_service_id
    ON public.junctions USING btree (service_id);
