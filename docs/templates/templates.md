---
title: Template Catalog
description: Starter templates you can clone with enclii init
sidebar_position: 2
tags: [templates, quickstart, starters]
---

# Template Catalog

Every template here is a minimal, deployable starter. Clone it, run `enclii deploy`, and you're live.

> **Current status:** The CLI's `enclii init --template` flag accepts `auto`, `node`, `go`, and `python` today. Named framework templates below are provisioned as starter repos under [github.com/madfam-org](https://github.com/madfam-org). Entries marked **Coming soon** are tracked; the `enclii init <name>` shorthand lands with P3.3 (template gallery in dashboard).

## Quick usage

Clone a starter template:

```bash
# Generic by language (works today)
enclii init --template node    # or: go | python | auto

# Clone a named starter repo (works today via git)
git clone https://github.com/madfam-org/enclii-template-nextjs my-app
cd my-app
enclii init       # writes service.yaml
enclii deploy
```

## Available templates

| Slug | Stack | Description | Status |
|------|-------|-------------|--------|
| `nextjs` | Next.js 15 · TypeScript · App Router | SSR-ready Next.js with `/health` probe wired | Coming soon |
| `fastapi` | Python · FastAPI · SQLAlchemy | Async API with Pydantic models and OpenAPI docs | Coming soon |
| `django` | Python · Django 5 · DRF | Django 5 + Django REST Framework, Postgres-ready | Coming soon |
| `go-fiber` | Go 1.22 · Fiber v3 | Minimal Go HTTP API with structured logging | Coming soon |
| `rails` | Ruby on Rails 7 | Rails 7 API-only scaffold with Puma | Coming soon |
| `remix` | Remix · Vite | Remix on Vite with SSR and hot reload | Coming soon |
| `sveltekit` | SvelteKit · TypeScript | SvelteKit adapter-node with `/health` probe | Coming soon |
| `phoenix` | Elixir · Phoenix 1.7 | Phoenix with LiveView and Postgres | Coming soon |
| `astro` | Astro · Static site | Static Astro site deployed behind Cloudflare | Coming soon |

All templates ship with:

- A valid `service.yaml` at the repo root
- A `/health` endpoint on the runtime port
- Sane defaults: 2 replicas, rolling deploys, `$PORT` / `$ENCLII_PORT` binding
- A `README.md` with runtime-specific gotchas

## Request a template

Need a stack that isn't listed? Open an issue at [github.com/madfam-org/enclii/issues](https://github.com/madfam-org/enclii/issues) with the tag `template-request`. We prioritize templates by customer demand.

## See also

- [5-minute quickstart](../quickstart.md) — the shortest path to a live deploy
- [Service spec reference](../reference/service-spec.md) — what `service.yaml` accepts
- [`enclii init` command](../cli/commands/init.md) — all flags and behaviors
