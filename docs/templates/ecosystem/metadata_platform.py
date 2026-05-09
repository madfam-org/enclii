"""Per-repo metadata for the `platform` pillar of the MADFAM ecosystem.

Consumed by the ECOSYSTEM.md generator. See `metadata/__init__.py` for
the aggregated `REPOS_FULL` dict and `generator.py` for render logic.
"""

REPOS = {
    'enclii': {
        'tagline': "MADFAM's self-hosted PaaS control plane.",
        'description': "Enclii is the open-source DevOps platform that deploys, scales, and operates every other MADFAM service. It's the Switchyard API + Web UI + Dispatch admin console + Roundhouse build workers + Waybill cost metering + Signal observability, all running on our own bare-metal k3s cluster behind Cloudflare Tunnel. Every other repo in the ecosystem deploys *through* Enclii — K8s manifests and CI live in each repo, ArgoCD reconciles, Switchyard tracks lifecycle events.",
        'pillar': 'Infrastructure (PaaS control plane)',
        'type': 'platform',
        'status': 'production',
        'production': {
            'services': [
                ('switchyard-api', 'api.enclii.dev', 4200),
                ('switchyard-ui', 'app.enclii.dev', 4201),
                ('dispatch', 'admin.enclii.dev', 4203),
                ('status-page', 'status.enclii.dev, status.madfam.io', 4204),
            ],
            'namespace': 'enclii',
        },
        'upstream_deps': [
            'janua (SSO — RS256 JWKS at auth.madfam.io/.well-known/jwks.json)',
            'cloudflare (tunnel ingress + DNS)',
            'hetzner (bare-metal k3s nodes)',
            'ghcr.io/madfam-org (container registry)',
            'argocd (GitOps reconciliation — in-cluster)',
        ],
        'downstream_consumers': [
            'every ecosystem repo — Enclii is the control plane for all deploys',
            'the enclii CLI binary consumed across all repos for ops',
        ],
        'key_env': [
            'ENCLII_DB_URL — control plane Postgres',
            'ENCLII_REGISTRY — ghcr.io/madfam-org',
            'ENCLII_OIDC_ISSUER — https://auth.madfam.io',
            'GITHUB_WEBHOOK_SECRET — HMAC verification for push events',
        ],
        'service_name_for_ops': 'switchyard-api',
    },
    'internal-devops': {
        'tagline': 'Private operational documentation — server IPs, SSH keys, kubeconfigs, cost ledger.',
        'description': 'Centralizes all MADFAM operational secrets that must not leak into public repos: bare-metal server IPs and hardware specs, SSH access, kubeconfigs (`~/.kube/config-hetzner`), ArgoCD admin credentials, cost ledger, RFCs, and capacity planning docs. Created in response to the 2026-03-13 security audit that found 18 public repos leaking these. **Clone this first on a fresh machine** to get kubeconfigs + SSH keys before doing any cluster ops.',
        'pillar': 'Infrastructure (ops-only)',
        'type': 'docs + secrets',
        'status': 'private, active',
        'production': {
            'services': [],
            'namespace': '(not deployed — docs/secrets repo)',
        },
        'upstream_deps': [
            'nothing at runtime — pure docs/secrets storage',
        ],
        'downstream_consumers': [
            'operators on fresh machines (pull to get kubeconfigs + SSH)',
            'RFC references from enclii, dhanam, karafiel, tulana, others',
        ],
        'key_env': [
            '(repo contains credentials itself — no runtime env)',
        ],
        'service_name_for_ops': '(n/a — not deployed)',
    },
    'server-auction-tracker': {
        'tagline': 'Hetzner Server Auction intelligence — automated scoring, price history, cluster simulation.',
        'description': "Monitors Hetzner's server auction in real time, scoring listings with a cluster-aware weighted formula (PassMark CPU benchmarks + RAM/drives/price/datacenter + ECC/NVMe). Produces deal badges, time-on-market urgency signals, and cluster-expansion simulations. Drives all MADFAM hardware procurement decisions — this is how we found `foundry-cp`, `foundry-worker-01`, and the builder VPS. CLI binary is `foundry-scout`; web dashboard at `sniper.madfam.io` is `deal-sniper`.",
        'pillar': 'Infrastructure (procurement intelligence)',
        'type': 'service + CLI',
        'status': 'production',
        'production': {
            'services': [
                ('deal-sniper', 'sniper.madfam.io', 3000),
            ],
            'namespace': 'deal-sniper',
        },
        'upstream_deps': [
            'hetzner server auction HTML (scraped)',
            'sqlite (price history, trends)',
            'janua (admin auth)',
        ],
        'downstream_consumers': [
            'MADFAM operators making hardware buys (enclii cluster expansion)',
        ],
        'key_env': [
            'DATABASE_URL — sqlite path for price history',
            'JANUA_JWKS_URI — admin auth',
        ],
        'service_name_for_ops': 'deal-sniper',
    },
    'madfam-crawler': {
        'tagline': 'Scraping-as-a-Service — centralized Playwright/OpenAI crawler for the ecosystem.',
        'description': 'Standalone REST API that offloads headless-browser and LLM-scrape overhead from downstream apps. Consumers (Fortuna, Tezca, Forgesight, Social Sentiment Monitor) enqueue jobs via `POST /v1/crawl` and retrieve results asynchronously. The stack is 3 services: `crawler-api` (FastAPI), `crawler-worker` (Celery + Playwright + ScrapeGraphAI), and `redis-broker` (pub/sub queue). Native enclii deployment on the Microsoft Playwright Python base image.',
        'pillar': 'Infrastructure (shared crawler)',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('crawler-api', '(internal — no public domain)', 8000),
                ('crawler-worker', '(internal worker)', None),
                ('redis-broker', '(internal broker)', 6379),
            ],
            'namespace': 'madfam-crawler',
        },
        'upstream_deps': [
            'playwright (headless chromium)',
            'openai / selva (ScrapeGraphAI LLM extraction)',
            'redis (Celery broker)',
            'janua (API auth)',
        ],
        'downstream_consumers': [
            'fortuna (problem-intel scrapes)',
            'tezca (DOF + legal feed scraping)',
            'forgesight (vendor pricing scrapes)',
            'social-sentiment-monitor (social scrapes)',
        ],
        'key_env': [
            'REDIS_URL — Celery broker',
            'OPENAI_API_KEY or SELVA_BASE_URL — LLM extraction',
            'JANUA_JWKS_URI — API auth',
            'CRAWLER_API_KEY_SALT — per-consumer API keys',
        ],
        'service_name_for_ops': 'crawler-api',
    },
    'routecraft': {
        'tagline': 'Evidence-based trip planning — scores city-dates on business, culture, and aesthetic dimensions.',
        'description': 'RouteCraft transforms business travel into strategic advantage by scoring candidate city-date windows against three dimensions (business opportunity, cultural relevance, aesthetic/experiential value) sourced from multiple data connectors. Ships a React + Next.js web app + a Python service-mesh of connector sync jobs. Published `@routecraft/daas-sdk` for downstream integration. Domain: `routecraft.app`.',
        'pillar': 'Infrastructure (data connectors / decision intelligence)',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('routecraft-web', 'routecraft.app', 3000),
                ('routecraft-api', 'api.routecraft.app', 8000),
            ],
            'namespace': 'routecraft',
        },
        'upstream_deps': [
            'postgres (scores, city catalog, user trips)',
            'external connector APIs (varies per connector)',
            'janua (user + API auth — RS256 via `@janua/react-sdk` and backend verifier in `packages/auth`)',
        ],
        'downstream_consumers': [
            '`@routecraft/daas-sdk` consumers (external integrators)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres scores + catalog',
            'JANUA_JWKS_URI — RS256 token verification (post-2026-04-17 compliance pass)',
            'CONNECTOR_* — per-connector credentials (check `docs/MADFAM_COMPLIANCE.md`)',
            'CORS_ALLOWED_ORIGINS — explicit allowlist (no wildcards post-H5 audit)',
        ],
        'service_name_for_ops': 'routecraft-api',
    },
    'proton-bridge-pipeline': {
        'tagline': 'Headless classification engine for a ProtonMail inbox — deployed on the MADFAM cluster.',
        'description': 'Connects to a ProtonMail inbox via IMAP IDLE (through Proton Bridge) and runs every email through a 3-phase classification pipeline: (1) deterministic regex rules (zero I/O), (2) spaCy NER + HTML sanitization (CPU only), (3) LLM swarm dispatch to Selva via Janua M2M auth (external LLM call). Used internally to automate inbox triage for business-critical email routing.',
        'pillar': 'Infrastructure (ops automation)',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('proton-bridge-engine', '(internal — no public domain)', None),
                ('proton-bridge-dashboard', '(internal — no public domain)', 3000),
            ],
            'namespace': 'proton-bridge',
        },
        'upstream_deps': [
            'proton bridge (IMAP gateway — runs as sidecar)',
            'selva (LLM classification via M2M Janua auth)',
            'spacy (NER models)',
            'supervisord (process manager)',
        ],
        'downstream_consumers': [
            'internal operators (inbox classification outputs)',
        ],
        'key_env': [
            'IMAP_HOST / IMAP_USER / IMAP_PASSWORD — Proton Bridge connection',
            'SELVA_BASE_URL / JANUA_M2M_CLIENT_ID / JANUA_M2M_CLIENT_SECRET — M2M LLM call',
        ],
        'service_name_for_ops': 'proton-bridge-engine',
    },
    'janua': {
        'tagline': 'Self-hosted OIDC/OAuth2 provider — the identity backbone for every MADFAM service.',
        'description': "Janua is MADFAM's Auth0 replacement: OIDC/OAuth 2.0 with RS256 JWTs, multi-tenant orgs, GitHub OAuth, and a JWKS endpoint every other service verifies tokens against. It's hard-wired into Enclii (switchyard-api auth), and every ecosystem service that needs an authenticated caller validates against `auth.madfam.io/.well-known/jwks.json`.",
        'pillar': 'Identity / Auth',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('janua-api', 'auth.madfam.io', 8000),
                ('janua-admin', 'admin.janua.dev', None),
                ('janua-app', 'app.janua.dev', None),
                ('janua-docs', 'docs.janua.dev', None),
                ('janua-web', 'janua.dev', None),
            ],
            'namespace': 'janua',
        },
        'upstream_deps': [
            'postgres (users, orgs, sessions, tokens)',
            'redis (session store)',
            'cloudflare (tunnel + DNS + SSL)',
            'enclii (deploy target)',
        ],
        'downstream_consumers': [
            'enclii (switchyard-api, dispatch) — OIDC SSO',
            'dhanam, karafiel, forgesight, tezca, fortuna, digifab-quoting, autoswarm-office, pravara-mes, yantra4d, avala, phynd-crm, routecraft, symbiosis-hcm — all verify tokens via JWKS',
        ],
        'key_env': [
            'JANUA_DATABASE_URL — Postgres connection',
            'JANUA_REDIS_URL — session + rate-limit store',
            'JANUA_JWT_PRIVATE_KEY / PUBLIC_KEY — RS256 signing material',
            'GITHUB_OAUTH_CLIENT_ID / SECRET — GitHub identity federation',
        ],
        'service_name_for_ops': 'janua-api',
    },
    'karafiel': {
        'tagline': 'Active tax defense + operational compliance — CFDI, NOM-151, SAT-adjacent, contract generation.',
        'description': "Karafiel is the operational compliance platform between MADFAM (and its tenants) and the Mexican government. It's the only component that interfaces with external government systems (SAT, blacklists, PSCs). Owns legal-ops / contract-template generation (absorbed the legacy `legal-ops` repo), CFDI emission, NOM-151 timestamping, and e.firma flows. Consumes law-feed updates from Tezca and operationalizes them into template changes. **Does NOT** own fabrication nodes, project management, or client-facing signature UX (that belongs to PhyndCRM/Cotiza).",
        'pillar': 'Compliance / Legal-ops',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('karafiel-web', 'karafiel.mx', 3000),
                ('karafiel-api', 'api.karafiel.mx', 8000),
                ('karafiel-admin', 'admin.karafiel.mx', 3001),
            ],
            'namespace': 'karafiel',
        },
        'upstream_deps': [
            'tezca (law/ruling feeds — Karafiel consumes, operationalizes)',
            'sat / pscs / blacklists (external government integrations)',
            'dhanam (billing for customer tiers)',
            'janua (SSO + multi-tenant)',
            'postgres (customers, documents, audit trail)',
        ],
        'downstream_consumers': [
            'phynd-crm (contract documents flow to client portal for signature)',
            'digifab-quoting / cotiza (CFDI emission for completed quotes)',
            'symbiosis-hcm (CFDI for payroll)',
            'external customers (marketplace at karafiel.mx)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'DHANAM_WEBHOOK_SECRET — billing integration',
            'SAT_CERTIFICATE_PATH / SAT_CERTIFICATE_PASSWORD — CFDI signing',
            'TEZCA_FEED_URL — law/ruling ingestion',
        ],
        'service_name_for_ops': 'karafiel-api',
    },
    'tezca': {
        'tagline': 'Mexican law oracle — machine-readable federal/state/municipal laws, rulings, and DOF feeds.',
        'description': "Tezca is the informational source of truth for Mexican law. It ingests and indexes DOF publications, SCJN rulings, SAT guidance, and federal/state/municipal statutes into a machine-readable catalog. Tezca is purely an oracle — it does NOT generate contracts, sign documents, or touch client engagements. **Karafiel** consumes Tezca's feeds and operationalizes them (template updates, CFDI rule changes, compliance checks).",
        'pillar': 'Legal / Intelligence (informational)',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('tezca-web', 'tezca.mx', 3000),
                ('tezca-api', 'api.tezca.mx', 8000),
                ('tezca-admin', 'admin.tezca.mx', 3001),
            ],
            'namespace': 'tezca',
        },
        'upstream_deps': [
            'postgres (catalog — laws, rulings, DOF feed)',
            'elasticsearch (full-text search)',
            'celery + redis (ingestion workers)',
            'madfam-crawler (scraping-as-a-service for scrapers)',
            'external: DOF publisher feeds, SCJN, SAT',
            'janua (admin/api auth)',
        ],
        'downstream_consumers': [
            'karafiel (operationalizes rulings into CFDI/NOM-151 templates)',
            'tezca-mcp (MCP server for AI agents under `packages/mcp-server`)',
            'public web portal at tezca.mx',
        ],
        'key_env': [
            'DATABASE_URL — Postgres catalog',
            'ELASTICSEARCH_URL — search index',
            'REDIS_URL — Celery broker',
            'JANUA_JWKS_URI — auth token verification',
            'CORS_ALLOWED_ORIGINS — explicit allowlist (no wildcards post-H2 audit)',
        ],
        'service_name_for_ops': 'tezca-api',
    },
}
