---
title: General FAQ
description: General questions about the Enclii platform
sidebar_position: 2
tags: [faq, general, platform]
---

# General FAQ

Common questions about what Enclii is and how it works.

## Platform Overview

### What is Enclii?

Enclii is a Platform-as-a-Service (PaaS) that provides streamlined developer experience on your own infrastructure. Deploy containerized services with enterprise-grade security, auto-scaling, and zero vendor lock-in.

**Key features**:
- Git-push deployment workflow
- Automatic builds via Buildpacks or Dockerfile
- Preview environments for pull requests
- Custom domains with automatic SSL
- Integrated secrets management
- Auto-scaling and health monitoring

### How is Enclii different from Railway, Vercel, or Heroku?

| Feature | Enclii | Railway | Vercel | Heroku |
|---------|--------|---------|--------|--------|
| Pricing model | Fixed infrastructure | Usage-based | Usage-based | Dyno-based |
| Vendor lock-in | None (standard K8s) | High | High | Moderate |
| Custom domains | Unlimited free | Limited free | Limited free | Paid |
| Data residency | EU (Hetzner) | Multi-region | Multi-region | Multi-region |
| Own infrastructure | Yes (optional) | No | No | No |

**Cost comparison**: Enclii runs on self-hosted infrastructure at significant savings vs. equivalent Railway + Auth0 setup. See internal-devops for cost breakdown.

### What can I deploy on Enclii?

Anything that runs in a container:

- **Web applications**: Node.js, Python, Go, Ruby, Java, PHP, Rust
- **APIs**: REST, GraphQL, gRPC
- **Static sites**: With build step support
- **Background workers**: Queue processors, cron jobs
- **Databases**: PostgreSQL, Redis, MySQL (as addons)

### Do I need Kubernetes knowledge?

No. Enclii abstracts away Kubernetes complexity. You work with:
- Services (your applications)
- Environments (staging, production)
- Domains (custom URLs)
- Secrets (environment variables)

Advanced users can access Kubernetes directly if needed.

## Languages and Frameworks

### What languages are supported?

Enclii supports any language that can run in a container. With Buildpacks, we automatically detect and build:

| Language | Detection | Build Tool |
|----------|-----------|------------|
| Node.js | `package.json` | npm, yarn, pnpm |
| Python | `requirements.txt`, `Pipfile` | pip, pipenv |
| Go | `go.mod` | go build |
| Ruby | `Gemfile` | bundler |
| Java | `pom.xml`, `build.gradle` | Maven, Gradle |
| Rust | `Cargo.toml` | cargo |
| PHP | `composer.json` | composer |

For other languages, provide a Dockerfile.

### What frameworks work out of the box?

**Frontend**:
- Next.js, Nuxt.js, SvelteKit
- React, Vue, Angular (with build step)
- Astro, Remix, Gatsby

**Backend**:
- Express, Fastify, NestJS (Node.js)
- Django, Flask, FastAPI (Python)
- Gin, Echo, Fiber (Go)
- Rails, Sinatra (Ruby)
- Spring Boot, Quarkus (Java)

### Can I use a monorepo?

Yes. Enclii has full monorepo support:

Give each service its own `service.yaml` and deploy it from, or point `--file` at, that spec:

```bash
enclii deploy -f apps/api/service.yaml --env staging
enclii deploy -f apps/web/service.yaml --env staging

# Or register every spec in a directory without deploying
enclii services-sync --dir apps/ --project <project-slug>
```

Each service builds from its own directory context.

## Deployment

### How does deployment work?

1. **Push to GitHub** - Your code triggers a webhook
2. **Build** - Enclii builds a container image
3. **Release** - Image is signed and stored
4. **Deploy** - Container rolls out to Kubernetes
5. **Verify** - Health checks confirm success

Typical deploy time: 2-5 minutes.

### What deployment strategies are available?

- **Rolling update** (default): Zero-downtime gradual replacement
- **Canary** (`enclii deploy --canary N`, N from 5 to 50): route N% of traffic to the new release, then auto-promote or auto-roll-back after the validation window. Needs at least 2 replicas; not supported for StatefulSets.

There is no blue-green or recreate strategy.

### Can I rollback?

Yes, instantly:

```bash
enclii rollback <service>            # Roll back to the previous deployment
enclii deploy ls <service>           # List deployments with their v-numbers
enclii rollback <service> v42        # Roll back to a specific deployment
enclii rollback <service> --instant  # Flip traffic at the routing layer (<30s)
```

### How do preview environments work?

When you open a pull request:
1. Enclii automatically creates a preview environment
2. Builds and deploys your PR branch
3. Provides a unique URL (e.g., `pr-123.preview.enclii.dev`)
4. Comments the URL on your PR
5. Tears down when PR is merged or closed

## Infrastructure

### Where does my code run?

On dedicated Hetzner bare-metal servers in Germany, managed by the MADFAM team, running k3s Kubernetes. See internal-devops for hardware specs.

### Is there a shared or dedicated option?

Currently, Enclii runs on shared infrastructure with namespace isolation. Dedicated nodes are available for enterprise customers.

### What about data residency?

All data is stored in EU (Germany) data centers. This helps with GDPR compliance. Additional regions available upon request.

### How does scaling work?

**Horizontal scaling** (more pods):
- Manual: Set replica count
- Automatic: Based on CPU/memory/custom metrics

**Vertical scaling** (bigger pods):
- Adjust resource requests/limits in service config

```yaml
autoscaling:
  enabled: true
  minReplicas: 2
  maxReplicas: 10
  targetCPUUtilizationPercentage: 70
```

## Integrations

### What CI/CD integrations are available?

- **GitHub** (native): Webhooks, Actions, Container Registry
- **GitLab**: Coming soon
- **API**: Use the REST API for custom CI/CD

### Can I use my own container registry?

Yes. While we default to GitHub Container Registry (ghcr.io), you can configure:
- Docker Hub
- AWS ECR
- Google Container Registry
- Self-hosted registries

### What about observability?

Built-in:
- Logs (aggregated and searchable)
- Metrics (CPU, memory, network)
- Health checks and alerts

Integrations available:
- Prometheus/Grafana
- Custom webhook alerts

## Related Documentation

- **Quickstart**: [Deploy Your First App](/getting-started/QUICKSTART)
- **Architecture**: [Platform Architecture](/architecture/)
- **Billing FAQ**: [Pricing Questions](/faq/billing)
- **Migration**: [Migration FAQ](/faq/migration)
