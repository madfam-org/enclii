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

```bash
# Set the root path for each service
enclii services create --name api --root-path apps/api
enclii services create --name web --root-path apps/web
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
- **Canary**: Test with 10% traffic before full rollout
- **Blue-green**: Deploy alongside existing, then switch
- **Recreate**: Stop old, start new (for stateful workloads)

### Can I rollback?

Yes, instantly:

```bash
enclii rollback <service>  # Rollback to previous release
enclii rollback <service> --release <id>  # Specific release
```

Enclii keeps the last 10 releases by default.

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

- **Quickstart**: [Deploy Your First App](/docs/getting-started/QUICKSTART)
- **Architecture**: [Platform Architecture](/docs/architecture/ARCHITECTURE)
- **Billing FAQ**: [Pricing Questions](/docs/faq/billing)
- **Migration**: [Migration FAQ](/docs/faq/migration)
