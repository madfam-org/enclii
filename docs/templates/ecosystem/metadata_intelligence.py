"""Per-repo metadata for the `intelligence` pillar of the MADFAM ecosystem.

Consumed by the ECOSYSTEM.md generator. See `metadata/__init__.py` for
the aggregated `REPOS_FULL` dict and `generator.py` for render logic.
"""

REPOS = {
    'selva-office': {
        'tagline': 'Selva — gamified multi-agent business orchestration + OpenAI-compatible LLM inference routing.',
        'description': "Selva Office (branded externally as **Selva**) is MADFAM's AI inference + agent orchestration platform. Two roles: (1) **inference proxy** — OpenAI-compatible `/v1` endpoint (`nexus-api`) that every ecosystem service routes its LLM calls through; (2) **agent platform** — LangGraph workers + Colyseus real-time state + 2D Phaser office UI for drafting agents, assigning them to departments, and approving their actions via a gamepad. Target domain: `selva.town`.",
        'pillar': 'Intelligence / Agents + LLM routing',
        'type': 'platform',
        'status': 'production',
        'production': {
            'services': [
                ('selva-nexus-api', 'agents-api.madfam.io', 8000),
                ('selva-office-ui', 'agents.madfam.io', 3000),
                ('selva-admin', 'agents-admin.madfam.io', 3001),
                ('selva-colyseus', 'agents-ws.madfam.io', 2567),
                ('selva-gateway', '(background)', None),
                ('selva-workers', '(langgraph worker)', None),
            ],
            'namespace': 'selva-office',
        },
        'upstream_deps': [
            'LLM providers: openai, anthropic, deepinfra, groq, etc.',
            'postgres (agent state, tasks, approvals)',
            'redis (colyseus room state)',
            'janua (operator auth + M2M for downstream services)',
            'shared: @madfam/budget-gate, @madfam/revenue-loop-probe, factory-manifest',
        ],
        'downstream_consumers': [
            'every ecosystem service doing LLM inference — routes through `/v1`',
            'phynd-crm (digital-project execution updates)',
            'symbiosis-hcm (human-AI dyad orchestration)',
            'proton-bridge-pipeline (email classification via M2M)',
        ],
        'key_env': [
            'OPENAI_API_KEY / ANTHROPIC_API_KEY / DEEPINFRA_API_KEY — upstream providers',
            'DATABASE_URL — Postgres',
            'REDIS_URL — colyseus',
            'JANUA_JWKS_URI — auth',
            'ENCLII_API_URL — HITL budget gate callback',
        ],
        'service_name_for_ops': 'selva-nexus-api',
    },
    'fortuna': {
        'tagline': 'Problem intelligence + zeitgeist engine — evidence-linked discovery of real customer problems.',
        'description': 'Fortuna ingests large volumes of customer signals (forums, reviews, social, support transcripts) and produces evidence-linked problem discovery, scoring, and validation. Includes the Zeitgeist Thoughtform Engine: HDBSCAN clustering, GoEmotions, lexical novelty, 6-component coherence scorer, 12 API endpoints, NLP endpoints, background jobs. Gated by `FEATURE_ZEITGEIST` flag. Marketing at `fortuna.tube`, app at `app.fortuna.tube`, API at `api.fortuna.tube`.',
        'pillar': 'Intelligence / Problem discovery',
        'type': 'service',
        'status': 'production',
        'production': {
            'services': [
                ('fortuna-web', 'fortuna.tube', 3000),
                ('fortuna-app', 'app.fortuna.tube', 3001),
                ('fortuna-api', 'api.fortuna.tube', 8000),
            ],
            'namespace': 'fortuna',
        },
        'upstream_deps': [
            'madfam-crawler (scraping-as-a-service)',
            'selva (LLM routing — Fortuna deleted its direct LLM providers post-2026-04-16 "separation of concerns")',
            'postgres (problems, evidence, scores)',
            'elasticsearch (full-text search)',
            'janua (auth)',
        ],
        'downstream_consumers': [
            'MADFAM ops (product discovery insights)',
            'phynd-crm (customer-problem federation)',
            'external subscribers (API)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'ELASTICSEARCH_URL — search',
            'CRAWLER_API_URL / CRAWLER_API_KEY — scrape dispatch',
            'SELVA_BASE_URL — LLM routing (do not talk to OpenAI directly)',
            'JANUA_JWKS_URI — auth',
            'FEATURE_ZEITGEIST — feature flag',
        ],
        'service_name_for_ops': 'fortuna-api',
    },
    'subtext': {
        'tagline': 'Conversational intelligence — extracts emotional signals + psychological states from audio.',
        'description': 'Subtext is the Metacognitive Engine: analyzes audio (meetings, interviews, support calls) to extract emotional signals, psychological states, and conversation dynamics — not just the transcript. Targets "EQ augmentation" for remote teams. Open-source core + commercial SaaS tier.',
        'pillar': 'Intelligence / Conversational',
        'type': 'service',
        'status': 'alpha',
        'production': {
            'services': [
                ('subtext-web', '(internal)', 3000),
                ('subtext-api', '(internal)', 8000),
                ('subtext-worker', '(audio processing)', None),
            ],
            'namespace': 'subtext',
        },
        'upstream_deps': [
            'selva (LLM routing for NLU)',
            'audio-processing (pyannote, whisper, etc.)',
            'postgres (conversations, signals)',
            'janua (auth)',
        ],
        'downstream_consumers': [
            'symbiosis-hcm (wellbeing + burnout prevention inputs)',
            'phynd-crm (client-conversation intelligence)',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'SELVA_BASE_URL — LLM routing',
            'AUDIO_STORAGE_BUCKET — R2 bucket for uploads',
        ],
        'service_name_for_ops': 'subtext-api',
    },
    'factlas': {
        'tagline': 'Geographic Fact Graph™ — multi-modal geospatial signals → auditable facts with coordinates.',
        'description': 'Factlas turns satellite, drone photogrammetry, vectors/POIs, land records, and manual tags into auditable facts with coordinates. Exposes a standards-first catalog (STAC/OGC), a Fact Graph with provenance + bitemporal versioning, and APIs for downstream consumers. Domain: `factl.as` (domain temporarily flagged 525 in 2026-04-19 NS audit).',
        'pillar': 'Intelligence / Geospatial',
        'type': 'service',
        'status': 'pre-alpha (pilot)',
        'production': {
            'services': [
                ('factlas-web', 'factl.as', 3000),
                ('factlas-api', 'api.factl.as', 8000),
                ('factlas-tiles', '(map tiles)', 8080),
            ],
            'namespace': 'factlas',
        },
        'upstream_deps': [
            'postgres + postgis (fact graph, bitemporal)',
            'STAC catalog (external or in-cluster)',
            'cloudflare R2 (tile + asset storage)',
            'janua (auth)',
        ],
        'downstream_consumers': [
            'MADFAM ops (geospatial decisions)',
            'future external API subscribers',
        ],
        'key_env': [
            'DATABASE_URL — Postgres + postgis',
            'STAC_CATALOG_URL — STAC endpoint',
            'R2_* — tile storage',
            'JANUA_JWKS_URI — auth',
            'CORS_ALLOWED_ORIGINS — explicit allowlist',
        ],
        'service_name_for_ops': 'factlas-api',
    },
    # social-sentiment-monitor: ARCHIVED 2026-05-03 per RFC 0016.
    # Capability absorbed into:
    #   - fortuna (Perception Index + anomaly detector + dashboard + SSE realtime)
    #   - madfam-crawler (IG/YT/TT extractors)
    # The repo is read-only on GitHub. Removed from this metadata file so
    # ecosystem-doc generators stop emitting stale "production" entries
    # for it. Any historical reference goes through the archived repo's
    # README (which has a deprecation banner pointing here).
    'tulana': {
        'tagline': 'Private pricing intelligence — evidence-based MXN recommendations for the MADFAM catalogue.',
        'description': 'Tulana is MADFAM\'s internal pricing engine. Answers "what should we charge for X?" with defensible MXN recommendations built from competitor scrapes + willingness-to-pay surveys + cost-plus floors + MX tax math. Covers every MADFAM SaaS (Karafiel, Selva, Tezca, Dhanam, Avala, Cotiza, Fortuna, others). **Private repo — not for public exposure.** RFC at `internal-devops/rfcs/0004-tulana-pricing-intelligence-platform.md`.',
        'pillar': 'Intelligence / Pricing (internal)',
        'type': 'service (private)',
        'status': 'internal',
        'production': {
            'services': [
                ('tulana-api', '(internal-only)', 8000),
                ('tulana-web', '(internal-only)', 3000),
            ],
            'namespace': 'tulana',
        },
        'upstream_deps': [
            'madfam-crawler (competitor scrapes)',
            'postgres (scores, recommendations)',
            'selva (LLM-assisted price synthesis)',
            'janua (internal auth, whitelisted to MADFAM operator domains)',
        ],
        'downstream_consumers': [
            'MADFAM pricing ops (internal recommendations)',
            'karafiel / selva / tezca / dhanam / cotiza pricing updates',
        ],
        'key_env': [
            'DATABASE_URL — Postgres',
            'JANUA_JWKS_URI — auth',
            'CRAWLER_API_URL — competitor scrapes',
            'SELVA_BASE_URL — price synthesis',
        ],
        'service_name_for_ops': 'tulana-api',
    },
}
