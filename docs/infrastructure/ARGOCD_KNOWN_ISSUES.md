---
title: ArgoCD Known Issues
description: Known bugs, workarounds, and proposed upstream fixes for ArgoCD
sidebar_position: 6
tags: [argocd, oci, helm, bugs]
---

# ArgoCD Known Issues

## Multi-Source OCI Helm Revision Resolution Bug

**Status:** Open (ArgoCD v3.2.5)
**Affected Apps:** `arc-runners`, `arc-runners-blue`
**Impact:** Sync status shows `Unknown`, auto-sync disabled. Pods are Healthy and functional.

### Symptom

Two ARC (Actions Runner Controller) applications show `Unknown` sync status with `ComparisonError`:

```
ComparisonError: failed to load target state: failed to generate manifest for source 1 of 2:
rpc error: code = Unknown desc = OCI Helm: failed to get chart version for
oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller:
403 Forbidden
```

The apps remain `Healthy` — the deployed resources work correctly. Only the sync comparison fails.

### Root Cause

ArgoCD v3.2.5 has a bug in multi-source OCI Helm revision resolution. When an Application uses `sources` (multi-source) with an OCI Helm chart, the revision resolver sends a HEAD request to the **base repo URL** instead of the **full chart path**.

**What happens:**

1. Application spec defines:
   ```yaml
   sources:
     - repoURL: oci://ghcr.io/actions/actions-runner-controller-charts
       chart: gha-runner-scale-set-controller
       targetRevision: 0.10.1
   ```

2. ArgoCD's revision resolver constructs the HEAD request URL as:
   ```
   HEAD https://ghcr.io/v2/actions/actions-runner-controller-charts/manifests/0.10.1
   ```

3. The **correct** URL should be:
   ```
   HEAD https://ghcr.io/v2/actions/actions-runner-controller-charts/gha-runner-scale-set-controller/manifests/0.10.1
   ```

4. The base URL (`actions-runner-controller-charts`) is a namespace, not a chart — GHCR returns 403.

**Key detail:** This only affects multi-source (`sources[]`) applications. Single-source OCI Helm apps work correctly because the code path that concatenates `repoURL + chart` is different.

### Reproduction Steps

1. Create an ArgoCD Application with multi-source OCI Helm:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-oci-multisource
  namespace: argocd
spec:
  project: default
  sources:
    - repoURL: oci://ghcr.io/actions/actions-runner-controller-charts
      chart: gha-runner-scale-set-controller
      targetRevision: 0.10.1
      helm:
        releaseName: test
        valueFiles:
          - $values/values.yaml
    - repoURL: https://github.com/your-org/your-repo.git
      targetRevision: main
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: test
```

2. Apply and observe sync status:
```bash
kubectl apply -f test-oci-multisource.yaml
kubectl get application test-oci-multisource -n argocd -o jsonpath='{.status.conditions}'
```

3. Expected: `ComparisonError` with 403 from GHCR.

4. Verify the single-source equivalent works:
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: test-oci-singlesource
  namespace: argocd
spec:
  project: default
  source:
    repoURL: oci://ghcr.io/actions/actions-runner-controller-charts
    chart: gha-runner-scale-set-controller
    targetRevision: 0.10.1
    helm:
      releaseName: test
  destination:
    server: https://kubernetes.default.svc
    namespace: test
```

### What We Tried

| Attempt | Result |
|---------|--------|
| Embed chart name in `repoURL` (`oci://ghcr.io/.../gha-runner-scale-set-controller`) | ArgoCD appends chart name again, double path |
| GHCR credentials (`ghcr-oci-creds` secret in argocd namespace) | Auth succeeds but URL is still wrong — 404 instead of 403 |
| Anonymous access (no credentials) | Same 403, confirms it's a URL construction issue |
| Different `repoURL` formats (with/without `oci://` prefix) | No change in behavior |

### Workaround

Manage ARC charts directly via Helm CLI, bypassing ArgoCD sync:

```bash
# Controller
helm upgrade arc-controller \
  oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller \
  --namespace arc-system \
  --version 0.10.1 \
  -f infra/helm/arc/values-controller.yaml

# Runner Scale Set
helm upgrade arc-runner-blue \
  oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set \
  --namespace arc-runners \
  --version 0.10.1 \
  -f infra/helm/arc/values-runner-set.yaml \
  -f infra/helm/arc/values-runner-set-blue.yaml
```

Auto-sync is disabled on both ARC apps to prevent ArgoCD from interfering with Helm-managed releases. See `infra/argocd/apps/arc-runners.yaml`.

### Proposed Upstream Fix

The bug is in ArgoCD's OCI revision resolver, likely in `util/oci/` or the Helm source generator's multi-source path.

**Expected behavior:** When resolving a chart revision for a multi-source OCI Helm entry, the resolver should concatenate `repoURL` + `chart` before issuing the HEAD/GET request — the same way it does for single-source applications.

**Pseudocode of the fix:**

```go
// Current (broken) behavior in multi-source path:
// Uses repoURL directly for version resolution
func resolveRevision(repoURL string, chart string, version string) {
    // BUG: HEAD https://ghcr.io/v2/<repoURL-path>/manifests/<version>
    ref := repoURL + "/manifests/" + version
    head(ref)
}

// Fixed behavior:
func resolveRevision(repoURL string, chart string, version string) {
    // Concatenate chart name into the OCI reference path
    fullRef := repoURL + "/" + chart
    ref := fullRef + "/manifests/" + version
    head(ref)
}
```

The single-source code path already does this concatenation correctly. The multi-source path skips it.

### GitHub Issue Template

Use the following to file at https://github.com/argoproj/argo-cd/issues:

---

**Title:** Multi-source OCI Helm: revision resolver uses base repoURL without chart name, causing ComparisonError

**Labels:** `bug`, `component:helm`, `component:oci`

**Body:**

#### Summary

When using multi-source (`sources[]`) with an OCI Helm chart, ArgoCD's revision resolver sends HEAD requests to the base `repoURL` without appending the `chart` name. This causes 403/404 errors from OCI registries (GHCR) and results in `ComparisonError` / `Unknown` sync status.

Single-source OCI Helm applications work correctly — the chart name is properly concatenated in that code path.

#### Version

- ArgoCD: v3.2.5
- Kubernetes: v1.33.7+k3s3

#### Application Spec (minimal reproduction)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  sources:
    - repoURL: oci://ghcr.io/actions/actions-runner-controller-charts
      chart: gha-runner-scale-set-controller
      targetRevision: 0.10.1
      helm:
        releaseName: test
        valueFiles:
          - $values/values.yaml
    - repoURL: https://github.com/example/repo.git
      targetRevision: main
      ref: values
  destination:
    server: https://kubernetes.default.svc
    namespace: test
```

#### Expected Behavior

ArgoCD resolves the chart version by issuing:
```
HEAD https://ghcr.io/v2/actions/actions-runner-controller-charts/gha-runner-scale-set-controller/manifests/0.10.1
```

#### Actual Behavior

ArgoCD resolves the chart version by issuing:
```
HEAD https://ghcr.io/v2/actions/actions-runner-controller-charts/manifests/0.10.1
```

The `chart` field (`gha-runner-scale-set-controller`) is not appended to the URL path, causing GHCR to return 403 (the base path is a namespace, not a chart).

#### Error

```
ComparisonError: failed to load target state: failed to generate manifest for source 1 of 2:
rpc error: code = Unknown desc = OCI Helm: failed to get chart version for
oci://ghcr.io/actions/actions-runner-controller-charts/gha-runner-scale-set-controller:
403 Forbidden
```

#### Workaround

Manage affected charts via `helm upgrade` directly. Disable auto-sync on the ArgoCD Application.

#### Suggested Fix

In the multi-source revision resolution path, concatenate `repoURL + "/" + chart` before constructing the OCI reference URL — matching the behavior of the single-source code path.

---

## Other Known Issues

_No other known issues at this time._
