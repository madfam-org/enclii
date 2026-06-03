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
        'tagline': 'Federated "single pane of glass" CRM — virtualizes data from 6 MADFAM platforms without ETL.',
        'description': 'Phynd is the client-facing deliverables portal: per-client (possibly branded) dashboards showing a complete history with MADFAM — quotes, signed proposals, active projects, deliverables, invoices. Owns CRM-native entities (contacts, leads, opportunities, pipelines) and *virtualizes* everything else (identity from Janua, billing from Dhanam, custom orders from Cotiza, fab status from Pravara, 3D assets) through a federation layer with caching, circuit breaking, and partial-failure tolerance. **Project execution tracking lives here**, not in the upstream platforms.',
        'pillar': 'Financial / CRM (client portal)',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('phynd-crm-web', '(per-client branded domains)', 3000),
                ('phynd-crm-api', '(internal federation API)', 8000),
                ('phynd-crm-worker', '(background jobs)', None),
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
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            "FEDERATION_ENDPOINTS_* — each upstream platform's API + M2M creds",
            'DHANAM_WEBHOOK_SECRET — inbound billing events',
            'CRM_WEBHOOK_SECRET — inbound interest-capture events from e.g. tezca',
        ],
        'service_name_for_ops': 'phynd-crm-api',
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
