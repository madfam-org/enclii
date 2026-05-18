# Zero-Touch Onboarding Contract

How to deploy a new application on the Enclii platform **without modifying any core repository** (enclii, janua, or dhanam).

## Principle

Core repos define the platform. Client repos define themselves. Onboarding a new app should **never** require a commit to enclii, janua, or dhanam.

## What Client Apps Provide

### 1. `enclii.yaml` (repo root)

Service definition consumed by the Enclii control plane:

```yaml
version: "2"
project: ${APP_NAME}
services:
  - name: ${SERVICE_NAME}
    type: web
    dockerfile: ./Dockerfile
    port: 8080
    health_check: /health
    domains:
      - ${DOMAIN}
environments:
  production:
    branch: main
    auto_deploy: true
```

### 2. K8s Manifests (`k8s/production/`)

Kustomize-based deployment manifests:

```
k8s/production/
  kustomization.yaml        # Image transformer (CI commits digests here)
  ${SERVICE}-deployment.yaml
  ${SERVICE}-service.yaml
  network-policies.yaml     # Optional: namespace NetworkPolicies
```

### 3. CI Workflow (`.github/workflows/deploy.yml`)

Must include:
- Docker build + push to GHCR
- `kustomize edit set image` to commit digest
- Lifecycle event callback to `POST /v1/callbacks/lifecycle-event`

See [ONBOARDING_GUIDE.md](./ONBOARDING_GUIDE.md) for the full CI template.

## Self-Service APIs

### ArgoCD Registration

```bash
curl -X POST "https://api.enclii.dev/v1/admin/onboard" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"repo_full_name": "madfam-org/${APP_NAME}", "branch": "main"}'
```

Current legacy implementation still writes an ArgoCD registration file into the
Enclii repo. That path is adopted legacy state, not the target zero-touch
contract. New onboarding work must move toward runtime ArgoCD reconciliation
from client repo desired state and must not add new app-specific Enclii catalog
entries. The runtime path is selected with
`ENCLII_ARGOCD_REGISTRATION_MODE=runtime`; the default remains `gitops` until
legacy ApplicationSet ownership has been migrated safely.

### Status Projection

Product status entries belong in the client repo's `enclii.yaml` under
`status.entries[]`. Enclii stores those entries in the onboarding DB snapshot
and projects the public status ConfigMaps from DB/core state. The legacy
projection path commits regenerated ConfigMaps to this repo; the zero-touch
path updates `status-config-enclii` and `status-config-madfam` directly in
Kubernetes with `ENCLII_STATUS_PROJECTION_MODE=runtime`.

### OAuth Client Registration

```bash
curl -X POST "https://auth.madfam.io/v1/oauth/clients" \
  -H "Authorization: Bearer $JANUA_API_KEY" \
  -d '{
    "name": "${APP_NAME}",
    "redirect_uris": ["https://${DOMAIN}/auth/callback"],
    "allowed_scopes": ["openid", "profile", "email"]
  }'
```

CORS origins are auto-derived from `redirect_uris` by Janua's `DynamicCORSMiddleware`.

### Domain Provisioning

Handled automatically by the onboarding API. Custom domains in `enclii.yaml` trigger:
1. Cloudflare DNS CNAME creation
2. Cloudflare tunnel route addition
3. Zone creation (if the domain's zone doesn't exist yet)

## What is Forbidden

Do **not** modify these core repos to onboard a new app:

| Repo | What NOT to do |
|------|----------------|
| `enclii` | Add routes to tunnel config, add hardcoded namespace entries to NetworkPolicies, add CORS origins, add status monitors |
| `janua` | Add OAuth clients to seed script, add CORS origins to K8s deployment, create OAuth client YAML files |
| `dhanam` | Add hardcoded URLs or fallbacks referencing your app |

## Infrastructure Ops (One-Time Setup)

These are infra ops performed by a platform operator, not code changes:

1. **Namespace labels**: Automatic — `EnsureNamespace()` applies `enclii.dev/data-access=true` and `enclii.dev/type=application` during onboarding. Platform NetworkPolicies use label selectors, so new namespaces auto-gain access to shared data (PostgreSQL, Redis, PgBouncer) and Janua SSO with no manual steps.
2. **K8s secrets**: Create secrets for DB credentials, API keys, etc.
3. **GitHub secrets**: Add `ENCLII_CALLBACK_TOKEN`, `MADFAM_BOT_PAT` to the repo
4. **GitHub team access**: Grant `automation` team write access for CI digest commits

## OAuth Client Registration CI Template

Add this step to your deploy workflow for automatic client registration:

```yaml
- name: Register OAuth Client
  if: ${{ env.REGISTER_OAUTH == 'true' }}
  run: |
    curl -sf -X POST "${{ vars.JANUA_API_URL }}/v1/oauth/clients" \
      -H "Authorization: Bearer ${{ secrets.JANUA_API_KEY }}" \
      -H "Content-Type: application/json" \
      -d '{
        "name": "${{ env.APP_NAME }}",
        "redirect_uris": ["https://${{ env.DOMAIN }}/auth/callback"],
        "allowed_scopes": ["openid", "profile", "email"]
      }'
```

## See Also

- [ONBOARDING_GUIDE.md](./ONBOARDING_GUIDE.md) — Step-by-step onboarding walkthrough
- [EXTERNAL_REPO_DEPLOY.md](./EXTERNAL_REPO_DEPLOY.md) — GitOps auto-deploy pattern
- [DEPLOYMENT_TRACKING.md](./DEPLOYMENT_TRACKING.md) — Lifecycle event API
