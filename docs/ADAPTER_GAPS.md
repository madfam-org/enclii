# Enclii adapter gaps registry

Track operations that still require break-glass (`kubectl`, provider CLIs, manual secrets) until an Enclii web/API/CLI adapter exists.

| Gap | Current workaround | Target adapter | Priority |
|-----|-------------------|----------------|----------|
| Production build secrets | Manual `kubectl apply` per `infra/k8s/production/kustomization.yaml` comments | `enclii secrets` + ExternalSecrets sync | P1 |
| Cloudflare optional secrets | Manual kubectl when not in git | `enclii providers cloudflare` | P2 |
| Policy-only kubectl comment | `infra/k8s/policies/enclii-default-deny.yaml` header | ArgoCD app docs only | P3 |
| Makefile `deploy-prod` | Raw `kubectl apply -k` | `enclii deploy` / GitOps-only path | P2 |
| `POST /v1/admin/projects/:slug/reconcile-services` | Admin API curl to register GitOps Deployments as Enclii services | `enclii projects reconcile-services` | P1 |
| Cloudflare tunnel route mutation | `enclii junctions add` (updates wrong targets via `AddRoute`) | `enclii providers cloudflare tunnels-apply` or junction reconcile job | P1 |
| Commercial GA staging secrets | Manual `workflow_dispatch` + repo secrets | GitHub Environment `commercial-ga-staging` documented in STAGING_SECRETS_SETUP | P1 |
| Prod DB migration verify (030) | Assume API startup `db.Migrate`; no read-back command | `enclii db schema` or migration status endpoint | P2 |

When closing a gap, remove the row and link the PR that added the adapter.
