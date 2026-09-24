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
| Image signatures (cosign) | Enforce | Active for namespaces labeled `enclii.dev/verify-signatures: "true"`; issuer-only (see below) |
| Registry restrictions | Enforce | Active outside infrastructure namespaces |
| Resource limits | Audit | Active |
| Required labels | Audit | Active |
| Health probes | Audit | Active |

## Image signature verification (`verify-image-signatures`)

### What the policy checks today

The `check-signature` rule in `image-policies.yaml` verifies
`ghcr.io/madfam-org/*` images in namespaces labeled
`enclii.dev/verify-signatures: "true"`. It requires one keyless Cosign
signature whose certificate was issued for the GitHub Actions OIDC issuer
`https://token.actions.githubusercontent.com` and logged in the public Rekor
instance. It also requires the image to be referenced by digest
(`verifyDigest: true`).

The policy is **issuer-only**. It does not check the certificate subject, so
it does not look at which repository, workflow, or ref produced the signature.

### Why it is issuer-only

Commit `c7205639` (2026-05-17, "align image signature policy with installed
CRD") removed the `subjectRegExp` the rule used to carry:

```text
^https://github\.com/madfam-org/[A-Za-z0-9_.-]+/\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml@refs/(heads/main|tags/v[0-9].*)$
```

The reason recorded in the policy is that the installed Kyverno (chart
`3.1.4`, pinned in `infra/argocd/apps/kyverno.yaml`) only supports exact
`keyless.subject` values. Enforcing a repo/workflow pattern needs either a
Kyverno upgrade or an explicit list of exact subjects.

### What that means

Any signature produced under the GitHub Actions OIDC issuer satisfies the
rule, including one from a workflow outside `madfam-org`, provided it is
stored next to the image in `ghcr.io/madfam-org` (by default, Cosign stores
signatures in the same repository as the image). In practice, the
effective control is **write access to the `ghcr.io/madfam-org` packages**:
whoever can push an image and its signature there can get it admitted. The
registry allowlist (`restrict-image-registries`) and the workflow-side checks
are separate controls; this rule does not add a workflow or ref constraint on
top of them.

### Restoring subject enforcement: accept SHA-pinned callers

Since #620, callers may pin the reusable workflow
`.github/workflows/build-publish.yml` by 40-hex commit SHA (see
[Reusable Workflows](../guides/reusable-workflows.md#pinning-the-reusable-workflow)).
Their signatures carry the identity:

```text
https://github.com/madfam-org/enclii/.github/workflows/build-publish.yml@<40-hex sha>
```

The pre-`c7205639` pattern above ends in `@refs/(heads/main|tags/v[0-9].*)$`
and does not match that identity. Restoring it unchanged would make admission
refuse every SHA-pinned caller's pods with `no matching signatures`, and
ArgoCD would keep retrying the sync.

When subject enforcement comes back, the ref part of the pattern must also
accept a SHA, for example:

```text
^https://github\.com/madfam-org/[A-Za-z0-9_.-]+/\.github/workflows/[A-Za-z0-9_.-]+\.ya?ml@(refs/(heads/main|tags/v[0-9].*)|[0-9a-f]{40})$
```

Notes on that pattern:

- It is broader than the workflow's own check. The pin job's "Resolve trusted
  signer identity" step in `build-publish.yml` is the matching rule: it
  accepts the tag/`main` pattern, plus exactly one
  `build-publish.yml@<sha>` identity, and only after checking that the SHA is
  on `madfam-org/enclii` `main` or is a `v*` tag commit. A static admission
  pattern cannot do that per-SHA check. A narrower option is to accept SHA
  refs only for `madfam-org/enclii/.github/workflows/build-publish.yml`.
- A SHA-pinned `uses:` line can resolve a commit from a fork in the repository
  network, and the identity still names `madfam-org/enclii`. A subject pattern
  does not distinguish those commits; see "What that check does not protect
  against" in the reusable-workflows guide.
- Keep the policy comment in `image-policies.yaml` and this section in step
  with the pattern that is actually deployed.

## Troubleshooting

If Kyverno reports `no matching signatures` for an image whose CI sign step
succeeded, check that the signature was pushed to the same
`ghcr.io/madfam-org/*` repository as the image and that its certificate issuer
is `https://token.actions.githubusercontent.com`. If subject enforcement has
been restored, also check the certificate identity against the deployed
pattern (including the SHA-ref case above) before creating a PolicyException.

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
- [Reusable Workflows](../guides/reusable-workflows.md) — `build-publish.yml` signing, digest pinning, and SHA pins (#620)
