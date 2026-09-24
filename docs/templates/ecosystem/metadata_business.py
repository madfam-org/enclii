"""Per-repo metadata for the `business` pillar of the MADFAM ecosystem.

Consumed by the ECOSYSTEM.md generator. See `metadata/__init__.py` for
the aggregated `REPOS_FULL` dict and `generator.py` for render logic.
"""

REPOS = {
    'dhanam': {
        'tagline': "MADFAM's billing + payment-gateway platform — multi-tenant, LATAM-first, ESG crypto insights.",
        'description': "Dhanam is the ecosystem's billing backbone: 6 payment gateways (Stripe, Mercado Pago, Conekta, SPEI, Crypto, etc.), entitlement and credit metering, invoicing, subscription management, and customer portals. Every paid feature across the MADFAM ecosystem flows through Dhanam. Also runs the ESG crypto insight module and wealth-tracking features for end users. Ships a public-facing web app, an API, and an admin console.",
        'pillar': 'Financial / Billing',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('dhanam-web', 'dhan.am', 3000),
                ('dhanam-api', 'api.dhan.am', 8000),
                ('dhanam-admin', 'admin.dhan.am', 3001),
            ],
            'namespace': 'dhanam',
        },
        'upstream_deps': [
            'postgres (customers, invoices, credits, entitlements)',
            'janua (auth, multi-tenant)',
            'payment gateways: stripe, mercado-pago, conekta, SPEI, crypto',
            'belvo (bank-account insights)',
            'karafiel (CFDI emission for Mexican invoices)',
        ],
        'downstream_consumers': [
            'every ecosystem repo with paid tiers (webhooks route to downstream services)',
            'tezca, avala, forgesight, cotiza, karafiel, phynd-crm — all receive billing webhooks for tier up/downgrades',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'STRIPE_SECRET_KEY / STRIPE_WEBHOOK_SECRET',
            'MERCADOPAGO_ACCESS_TOKEN / CONEKTA_PRIVATE_KEY / BELVO_SECRET_ID',
            'DHANAM_WEBHOOK_SECRET — HMAC signing for outbound webhooks to downstream services',
        ],
        'service_name_for_ops': 'dhanam-api',
    },
    'phynd-crm': {
        'tagline': 'CRM — consent, campaigns, attribution; federates data from the MADFAM platforms without ETL.',
        'description': "PhyndCRM is MADFAM's CRM — consent, campaigns, attribution. It is the consent and attribution end of the commercial pipeline: system of record for marketing consent (LFPDPPP, per identifier and channel, audited), double opt-in, a cross-product suppression list that outranks any consent record, authorization before any outbound campaign, and attribution of each completed purchase back to the outreach and consent basis it came from. Owns CRM-native entities (contacts, leads, opportunities, pipelines) and _virtualizes_ everything else (identity from Janua, billing from Dhanam, custom orders from Cotiza, fab status from Pravara, 3D assets) through a federation layer with caching, circuit breaking, and partial-failure tolerance.",
        'pillar': 'Financial / CRM',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('phynd-crm-web', '`phynd.app`, `www.phynd.app`, `crm.madfam.io`; generic app host currently responds at `crm.phynd.app`', 3000),
                ('phynd-crm-worker', 'background jobs and health endpoint', 3001),
            ],
            'namespace': 'phynd-crm',
        },
        'upstream_deps': [
            'janua (identity federation)',
            'dhanam (billing federation)',
            'digifab-quoting / cotiza (quote + order federation)',
            'karafiel (contract document federation)',
            'pravara-mes (fab job status federation)',
            'selva-office / selva (digital-project execution updates)',
            'postgres (CRM-native entities)',
        ],
        'downstream_consumers': [
            'external clients (single pane of glass, per-client portal)',
            'MADFAM account managers / ops team',
        ],
        'key_env': [
            '`DATABASE_URL` — Postgres',
            '`REDIS_URL` — Redis/BullMQ/rate limiting',
            '`AUTH_JANUA_ISSUER`, `AUTH_JANUA_CLIENT_ID`, `AUTH_JANUA_CLIENT_SECRET` — Janua OIDC',
            '`JANUA_API_URL`, `JANUA_TELEMETRY_API_URL`, `DHANAM_API_URL`, `COTIZA_API_URL`, `PRAVARA_BASE_URL`, `SELVA_API_URL`, `FORJ_API_URL` — upstream federation/dispatch APIs',
            '`*_WEBHOOK_SECRET`, `PHYND_CRM_EVENTS_SECRET`, `PHYND_ENGAGEMENT_EVENTS_SECRET` — signed inbound ecosystem events',
            '`FEDERATION_API_TOKEN` — optional service-to-service tRPC + GraphQL read token (Selva agents)',
            '`FEDERATION_SERVICE_USER_ID` — machine principal for service auth (default `service:selva`)',
            '`PHYND_DEPLOYMENT_TIER` — `staging` | `production`; staging blocks outbound prod MADFAM URLs',
            '`WORKER_HEALTH_PORT` — worker health endpoint, default `3001`',
        ],
        'service_name_for_ops': 'phynd-crm-api',
        'production_truth': 'Latest repository and production evidence is recorded in\n[`docs/CODEBASE_AND_PROD_EVIDENCE_2026-05-27.md`](docs/CODEBASE_AND_PROD_EVIDENCE_2026-05-27.md).\n\n**Roadmap:** [`docs/ROADMAP.md`](docs/ROADMAP.md) · **Remediation plan:**\n[`docs/MADFAM_TRUTH_LAYER_REMEDIATION.md`](docs/MADFAM_TRUTH_LAYER_REMEDIATION.md)',
    },
    'symbiosis-hcm': {
        'tagline': 'Hybrid human-AI Human Capital Management platform — Mexican payroll + multi-agent systems.',
        'description': "Symbiosis is MADFAM's HCM platform for internal and tenant use. Core capabilities: multi-agent systems (human-AI dyads with MCP/A2A orchestration), organizational network analysis (ONA), Shapley-value compensation, native Mexican payroll (CFDI 4.0, IMSS, ISR, PTU, fondo de ahorro), REPSE + NOM-035 + LFPDPPP compliance automation, and wellbeing/burnout tracking.",
        'pillar': 'Financial / HCM (payroll)',
        'type': 'service',
        'status': 'alpha / production',
        'production': {
            'services': [
                ('symbiosis-hcm-web', '(internal)', 3000),
                ('symbiosis-hcm-api', '(internal)', 8000),
            ],
            'namespace': 'symbiosis-hcm',
        },
        'upstream_deps': [
            'janua (SSO + multi-tenant)',
            'karafiel (CFDI 4.0 emission for payroll)',
            'selva-office / selva (multi-agent orchestration)',
            'postgres (employees, agents, payroll runs)',
        ],
        'downstream_consumers': [
            'MADFAM internal ops',
            'future tenant orgs on symbiosis.hcm (multi-tenant)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'KARAFIEL_API_URL / KARAFIEL_API_KEY — CFDI emission',
            'SELVA_BASE_URL — agent orchestration',
        ],
        'service_name_for_ops': 'symbiosis-hcm-api',
    },
    'zavlo': {
        'tagline': 'Multi-tenant loyalty platform — CFDI invoices → NFT/VC credentials → gamified loyalty.',
        'description': 'Zavlo is a SaaS + IaaS loyalty infrastructure that transforms CFDI-compliant invoices into verifiable on-chain credentials (NFT / Verifiable Credential) and powers game-like loyalty experiences for businesses and their customers in Mexico. Tenant brands can run loyalty programs without writing smart contracts or CFDI integrations themselves.',
        'pillar': 'Financial / Loyalty',
        'type': 'service',
        'status': 'in development',
        'production': {
            'services': [],
            'namespace': 'zavlo',
        },
        'upstream_deps': [
            'karafiel (CFDI parsing + verification)',
            'dhanam (tenant billing)',
            'janua (tenant SSO)',
            'on-chain: EVM-compatible L2 for NFT minting',
        ],
        'downstream_consumers': [
            'tenant brand merchants',
            'end consumers (via tenant loyalty apps)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'KARAFIEL_API_URL — CFDI verification',
            'EVM_RPC_URL / MINTER_PRIVATE_KEY — NFT issuance',
        ],
        'service_name_for_ops': 'zavlo-api',
    },
    'avala': {
        'tagline': 'Trainee-first Learning & Competency Cloud — EC/CONOCER + DC-3/SIRCE + verifiable credentials.',
        'description': "AVALA is MADFAM's multi-tenant learning verification platform. Aligned to Mexican competency standards (EC/CONOCER, DC-3/SIRCE) with verifiable credentials output. Turborepo + pnpm monorepo. Trainee-first UX with multi-tenant org management. Domain: `avala.studio`.",
        'pillar': 'Learning / Credentialing',
        'type': 'service',
        'status': 'alpha',
        'production': {
            'services': [
                ('avala-web', 'avala.studio', 3000),
                ('avala-api', 'api.avala.studio', 8000),
            ],
            'namespace': 'avala',
        },
        'upstream_deps': [
            'postgres (trainees, competencies, evidence)',
            'janua (tenant SSO)',
            'dhanam (subscription billing)',
            'karafiel (CFDI for completed certifications)',
            'on-chain / VC issuer (verifiable credentials)',
        ],
        'downstream_consumers': [
            'phynd-crm (employee training federation)',
            'symbiosis-hcm (competency → compensation input)',
            'external tenants (training orgs, employers)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'DHANAM_WEBHOOK_SECRET — billing',
            'VC_ISSUER_PRIVATE_KEY — credential signing',
        ],
        'service_name_for_ops': 'avala-api',
    },
    'accionables-madlab': {
        'tagline': 'MADLAB — gamified science-and-tech educational events for Mexican primary schools.',
        'description': 'MADLAB is a live educational event product: 3-hour gamified science-and-tech presentations for primary schools (grupos of 20–100 students), aligned to Mexican national competency standards + UN SDGs (water, clean energy, recycling). This repo ships the client app (waitlist, stats, ND-profile signups) + content scripts. Not a major platform — ecosystem role is lead-gen + community engagement for the broader MADFAM learning pillar.',
        'pillar': 'Learning / Event product',
        'type': 'service',
        'status': 'production (limited)',
        'production': {
            'services': [
                ('madlab-client', '(internal/event)', 3000),
            ],
            'namespace': 'madlab',
        },
        'upstream_deps': [
            'postgres (waitlist, event signups)',
            'janua (admin auth)',
            'phynd-crm (lead webhook)',
        ],
        'downstream_consumers': [
            'phynd-crm (waitlist leads)',
            'MADFAM events team (scheduling + delivery)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — admin auth',
            'CRM_WEBHOOK_URL / CRM_WEBHOOK_SECRET — lead forwarding',
        ],
        'service_name_for_ops': 'madlab-client',
    },
}
