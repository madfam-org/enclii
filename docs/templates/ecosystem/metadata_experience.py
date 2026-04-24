"""Per-repo metadata for the `experience` pillar of the MADFAM ecosystem.

Consumed by the ECOSYSTEM.md generator. See `metadata/__init__.py` for
the aggregated `REPOS_FULL` dict and `generator.py` for render logic.
"""

REPOS = {
    'madfam-site': {
        'tagline': 'Official MADFAM corporate website — madfam.io + cms.madfam.io.',
        'description': 'The public face of MADFAM: platform map, ecosystem vision, blog, investor-facing material. Built on Next.js 14 monorepo. Separately ships the CMS (`cms.madfam.io`) that editors use to manage content.',
        'pillar': 'Brand / Corporate',
        'type': 'site',
        'status': 'production',
        'production': {
            'services': [
                ('madfam-web', 'madfam.io', 3000),
                ('madfam-cms', 'cms.madfam.io', 3001),
            ],
            'namespace': 'madfam-site',
        },
        'upstream_deps': [
            'cloudflare (CDN)',
            'cms database (postgres)',
            'phyne-crm (contact form → lead webhook)',
        ],
        'downstream_consumers': [
            'public visitors',
            'phyne-crm (inbound leads)',
        ],
        'key_env': [
            'DATABASE_URL — CMS',
            'CRM_WEBHOOK_URL / CRM_WEBHOOK_SECRET — lead routing',
        ],
        'service_name_for_ops': 'madfam-web',
    },
    'ceq': {
        'tagline': 'Creative Entropy Quantized — ComfyUI wrapper + /v1/render asset pillar for MADFAM.',
        'description': "CEQ is MADFAM's internal content generation platform: a streamlined hacker-centric ComfyUI wrapper. Ships `/v1/render/*` deterministic R2-cached render endpoints + `@ceq/sdk` as the asset pillar consumed across the ecosystem. First consumer: Rondelio's stratum-tcg. Audio/3D rendering stubs exist but return 501. Domain: `ceq.lol`.",
        'pillar': 'Brand / Asset pillar',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('ceq-studio', 'ceq.lol', 3000),
                ('ceq-api', 'api.ceq.lol', 8000),
                ('ceq-workers', '(ComfyUI worker — GPU node)', 8188),
            ],
            'namespace': 'ceq',
        },
        'upstream_deps': [
            'ComfyUI (embedded in worker image)',
            'nvidia-container-toolkit + GPU nodes',
            'cloudflare R2 (deterministic render cache)',
            'postgres (render jobs, manifests)',
            'selva (LLM prompt augmentation)',
            'janua (auth)',
        ],
        'downstream_consumers': [
            '`@ceq/sdk` consumers (ecosystem asset pipeline)',
            'stratum-tcg (rondelio) — first consumer',
            'ui-components + marketing sites via rendered assets',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'R2_* — render cache',
            'JANUA_JWKS_URI — auth',
            'COMFYUI_PATH / MODELS_PATH — worker config',
            'SELVA_BASE_URL — prompt augmentation',
        ],
        'service_name_for_ops': 'ceq-api',
    },
    'nuit-one': {
        'tagline': 'Audio platform — demucs/basic-pitch/yt-dlp pipeline at nuit.one.',
        'description': "Nuit One is MADFAM's audio-processing platform: stem separation (demucs), pitch detection (basic-pitch), YouTube download (yt-dlp) served behind a SvelteKit-style web + Node API. Primary use: music education + remix tooling. Domain: `nuit.one`.",
        'pillar': 'Brand / Audio',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('nuit-one-web', 'nuit.one', 3000),
                ('nuit-one-api', 'api.nuit.one', 3001),
            ],
            'namespace': 'nuit-one',
        },
        'upstream_deps': [
            'demucs + basic-pitch + yt-dlp (Python AI tools)',
            'ffmpeg (audio processing)',
            'cloudflare R2 (audio storage)',
            'postgres (user workspaces, jobs)',
            'janua (auth)',
        ],
        'downstream_consumers': [
            'end users (web app)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'R2_* — audio storage',
            'JANUA_JWKS_URI — auth',
            'TORCH_HOME — demucs model cache path (required for non-root Docker)',
        ],
        'service_name_for_ops': 'nuit-one-api',
    },
    'bloom-scroll': {
        'tagline': 'From doom-scrolling to bloom-scrolling — finite, serendipitous content aggregator.',
        'description': 'Bloom Scroll is a perspective-driven content aggregator optimized for serendipity, finite feeds, and raw data instead of engagement outrage. Synthesizes statistical truth + frontier science + visual culture into curated daily digests. Core principle: "The End" is the product — every feed has a definitive stopping point. Domain: `almanac.solar`.',
        'pillar': 'Brand / Media',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('bloom-scroll-web', 'almanac.solar', 3000),
                ('bloom-scroll-api', 'api.almanac.solar', 8000),
                ('bloom-scroll-ingest', '(background ingest)', None),
            ],
            'namespace': 'bloom-scroll',
        },
        'upstream_deps': [
            'postgres + pgvector (content embedding search)',
            'madfam-crawler (source ingestion)',
            'selva (LLM classification + summarization)',
            'janua (auth)',
        ],
        'downstream_consumers': [
            'public readers',
        ],
        'key_env': [
            'DATABASE_URL — Postgres + pgvector',
            'JANUA_JWKS_URI — auth',
            'SELVA_BASE_URL — LLM classification',
            'CRAWLER_API_URL — source ingestion',
            'CORS_ALLOWED_ORIGINS — explicit allowlist',
        ],
        'service_name_for_ops': 'bloom-scroll-web',
    },
    'coforma-studio': {
        'tagline': 'Multi-tenant Customer Advisory Board (CAB) platform — "Advisory-as-a-Service".',
        'description': 'Coforma Studio is the SaaS platform for companies to create, manage, and scale Customer Advisory Boards. Structured feedback loops + engagement hubs + incentive mechanisms. LATAM-first, designed for global scalability. Defines the new category "Advisory-as-a-Service". **Not to be confused with Cotiza Studio** (`digifab-quoting`) — unrelated. Domain: `coforma.studio`.',
        'pillar': 'Brand / SaaS (advisory)',
        'type': 'service',
        'status': 'foundation (40% complete)',
        'production': {
            'services': [
                ('coforma-studio-web', 'coforma.studio', 3000),
                ('coforma-studio-api', 'api.coforma.studio', 8000),
            ],
            'namespace': 'coforma-studio',
        },
        'upstream_deps': [
            'postgres (tenants, CABs, feedback)',
            'janua (tenant SSO)',
            'dhanam (tenant billing)',
            'selva (LLM-assisted feedback synthesis)',
        ],
        'downstream_consumers': [
            'tenant brand ops',
            'phyne-crm (feedback signals federated into client portal)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'DHANAM_WEBHOOK_SECRET — billing',
            'SELVA_BASE_URL — LLM routing',
        ],
        'service_name_for_ops': 'coforma-studio-api',
    },
    'stratum-tcg': {
        'tagline': 'STRATUM: The Fab Wars — hybrid TCG / Eurogame simulating the fabrication economy.',
        'description': "Physical-first hybrid TCG + network-building Eurogame for 2–4 players. Thematically grounded in MADFAM's fabrication pillar (manufacturing syndicates, polymer supply chains, logistics). First consumer of CEQ's `/v1/render` asset pillar for card art generation. Deployed alongside the `turnbased-engine` game engine and the `rondelio` platform UI.",
        'pillar': 'Games / TCG',
        'type': 'product (physical + digital)',
        'status': 'in development',
        'production': {
            'services': [],
            'namespace': 'stratum-tcg',
        },
        'upstream_deps': [
            'turnbased-engine (game engine)',
            'rondelio (platform UI, game lobby)',
            'ceq (card art rendering via `@ceq/sdk`)',
            'janua (player auth)',
        ],
        'downstream_consumers': [
            'rondelio (game listing)',
            'external players',
        ],
        'key_env': [
            '(cartridge config — see `turnbased-engine` docs)',
        ],
        'service_name_for_ops': '(n/a — cartridge for turnbased-engine)',
    },
    'turnbased-engine': {
        'tagline': 'Generic turn-based card game engine — FastAPI server + cartridge plugin system.',
        'description': 'Reusable turn-based game engine consumed by `rondelio` + `stratum-tcg`. Core engine (`packages/turnbased`) covers state models, turn loop, stack, effects, serialization. Server (`packages/server`) ships FastAPI REST + WebSocket + auth + matchmaking + persistence. TUI viewer for headless games. Rondelio is the 8-bit-themed platform UI layered on top.',
        'pillar': 'Games / Engine library',
        'type': 'library + server',
        'status': 'active',
        'production': {
            'services': [
                ('turnbased-server', '(consumed by rondelio)', 8000),
            ],
            'namespace': 'turnbased',
        },
        'upstream_deps': [
            'postgres (games, players, matches)',
            'redis (realtime)',
            'janua (player auth)',
        ],
        'downstream_consumers': [
            'rondelio (platform UI)',
            'stratum-tcg (TCG cartridge)',
            'any future cartridge-compatible game',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'REDIS_URL — realtime',
            'JANUA_JWKS_URI — auth',
        ],
        'service_name_for_ops': 'turnbased-server',
    },
    'solarpunk-foundry': {
        'tagline': 'Ecosystem orchestration hub — port registry, @madfam/* shared packages, architecture narrative.',
        'description': 'The solarpunk-foundry repo is the ecosystem-level blueprint: the canonical port registry (`docs/PORT_ALLOCATION.md`) that every service looks up its port block in, the `@madfam/core` package + other shared packages, local dogfooding scaffolds, and the public-safe architecture narrative for the MADFAM vision. Reference this first when reasoning about ecosystem-level decisions.',
        'pillar': 'Ecosystem blueprint',
        'type': 'docs + shared packages',
        'status': 'active',
        'production': {
            'services': [],
            'namespace': '(not deployed — blueprint + shared libs)',
        },
        'upstream_deps': [
            '(none at runtime)',
        ],
        'downstream_consumers': [
            'every ecosystem repo — reads PORT_ALLOCATION.md for port assignment',
            '`@madfam/core` package consumers',
        ],
        'key_env': [
            '(library — no runtime env)',
        ],
        'service_name_for_ops': '(n/a — blueprint repo)',
    },
}
