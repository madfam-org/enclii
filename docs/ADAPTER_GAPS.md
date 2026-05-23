# Enclii adapter gaps registry

Track operations that still require break-glass (`kubectl`, provider CLIs, manual secrets) until an Enclii web/API/CLI adapter exists.

| Gap | Current workaround | Target adapter | Priority |
|-----|-------------------|----------------|----------|
| Production build secrets | Manual `kubectl apply` per `infra/k8s/production/kustomization.yaml` comments | `enclii secrets` + ExternalSecrets sync | P1 |
| Cloudflare optional secrets | Manual kubectl when not in git | `enclii providers cloudflare` | P2 |
| Policy-only kubectl comment | `infra/k8s/policies/enclii-default-deny.yaml` header | ArgoCD app docs only | P3 |
| Makefile `deploy-prod` | Raw `kubectl apply -k` | `enclii deploy` / GitOps-only path | P2 |

When closing a gap, remove the row and link the PR that added the adapter.
