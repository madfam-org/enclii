---
title: Kyverno Policies
description: Kubernetes admission control policies for cluster security
sidebar_position: 9
---

# Kyverno Policies

Enclii uses [Kyverno](https://kyverno.io/) for Kubernetes-native policy enforcement. Policies are managed via ArgoCD and defined in `infra/k8s/base/kyverno/policies/`.

## Policy Files

| File | Purpose |
|------|---------|
| `security-policies.yaml` | Enforce: no privileged containers, run-as-nonroot, no host namespaces, restrict capabilities |
| `best-practices.yaml` | Audit: require resource limits, labels, health probes, disallow `:latest` tag |
| `image-policies.yaml` | Audit/Enforce: image signature verification (cosign), restrict registries to approved list |
| `kyverno-namespace-exception.yaml` | Exemptions for system namespaces (kube-system, kyverno) |

## Policy Modes

- **Enforce** — Blocks non-compliant resources from being created
- **Audit** — Allows resources but generates policy violation reports

## Current State

| Policy Category | Mode | Status |
|-----------------|------|--------|
| Privileged containers | Enforce | Active |
| Run-as-nonroot | Enforce | Active |
| Host namespaces | Enforce | Active |
| Capabilities | Enforce | Active |
| Image signatures (cosign) | Enforce | Active for namespaces labeled `enclii.dev/verify-signatures: "true"` |
| Registry restrictions | Enforce | Active outside infrastructure namespaces |
| Resource limits | Audit | Active |
| Required labels | Audit | Active |
| Health probes | Audit | Active |

## Troubleshooting

Image signature verification is keyless and must match MADFAM GitHub Actions
OIDC identities. The verifier accepts identities matching
`^https://github\.com/madfam-org/[A-Za-z0-9_.-]+/\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml@refs/(heads/main|tags/v[0-9].*)$`
with issuer `https://token.actions.githubusercontent.com`. If Kyverno reports
`no matching signatures` for an image whose CI sign step succeeded, first check
the certificate identity pattern before creating a PolicyException.

```bash
# View cluster-wide policy reports
kubectl get clusterpolicyreport

# View namespace-specific reports
kubectl get policyreport -A

# Check admission controller logs
kubectl logs -n kyverno -l app.kubernetes.io/component=admission-controller -f

# Inspect policy status
kubectl get clusterpolicy <name> -o yaml
```

## Related Documentation

- Kyverno Setup README: `infra/k8s/base/kyverno/README.md` — installation and detailed policy tables
- [Image Versioning](./IMAGE_VERSIONING.md) — digest pinning and image management
- [GitOps](./GITOPS.md) — ArgoCD manages Kyverno app via `infra/argocd/apps/kyverno.yaml`
