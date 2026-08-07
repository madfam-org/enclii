# External Secrets Operator

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Last Updated:** 2026-06-16
**Status:** Operational (Vault-backed)
**Active Providers:** `vault-store` (HashiCorp Vault KV v2) + `kubernetes-store` (legacy, cross-namespace)

---

## Overview

Enclii uses the External Secrets Operator (ESO) to synchronize secrets from HashiCorp Vault into Kubernetes namespaces. All ~160 production secrets across 16 namespaces are stored in Vault and synced via ExternalSecret resources.

For secrets management strategy and Vault deployment details, see [SECRETS_MANAGEMENT.md](./SECRETS_MANAGEMENT.md).
For Vault operations (unseal, rotation, backup), see [Vault Operations Runbook](../runbooks/VAULT_OPERATIONS.md).

## Architecture

```
┌─────────────────────────────────────────┐
│    HashiCorp Vault (vault namespace)    │
│    KV v2 engine at secret/              │
│    UI: https://vault.madfam.io          │
└─────────────────────────────────────────┘
                    │
          Vault token bridge
                    │
                    ▼
┌─────────────────────────────────────────┐
│     External Secrets Operator           │
│     (external-secrets namespace)        │
│  ┌─────────────────────────────────────┐│
│  │ ClusterSecretStore: vault-store     ││
│  │ Provider: vault (KV v2)            ││
│  └─────────────────────────────────────┘│
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│   16 namespaces (via ExternalSecret)    │
│  • enclii, janua, data, dhanam, tezca  │
│  • yantra4d, karafiel, forgesight, ... │
└─────────────────────────────────────────┘
```

## Configuration

### ClusterSecretStore (Vault)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: vault-store
spec:
  provider:
    vault:
      server: "http://vault.vault.svc.cluster.local:8200"
      path: "secret"
      version: "v2"
      auth:
        tokenSecretRef:
          name: vault-eso-token
          namespace: external-secrets
          key: token
```

> [!NOTE]
> `vault-store` is temporarily using a scoped `eso-reader` token in
> `external-secrets/vault-eso-token` after the 2026-05-18 Vault rebootstrap.
> Vault Kubernetes auth remains the desired steady state, but the Vault pod
> currently cannot reach the Kubernetes API for TokenReview. Repair that
> reachability before moving this store back to service-account auth.

### ClusterSecretStore (Legacy kubernetes-store)

Still available for backward compatibility at `infra/k8s/base/external-secrets/cluster-secret-store.yaml`.

### Creating an ExternalSecret (Vault-backed)

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: my-service-secrets
  namespace: target-namespace
  labels:
    app.kubernetes.io/managed-by: enclii
spec:
  refreshInterval: 15m
  secretStoreRef:
    name: vault-store
    kind: ClusterSecretStore
  target:
    name: my-service-secrets
    creationPolicy: Owner
    deletionPolicy: Retain
  data:
    - secretKey: DATABASE_URL
      remoteRef:
        key: secret/target-namespace
        property: database_url
```

## Rule: one ExternalSecret per Secret — and if not, EVERY writer must be `Merge`

**Prefer exactly one ExternalSecret per target Secret.**

Where multiple writers are unavoidable, **every** writer of that Secret must set
`spec.target.creationPolicy: Merge`, and the target Secret **must already
exist** — no `Merge` writer will create it.

Why: ESO has no create-if-missing-**and**-merge policy. `Owner` and `Orphan`
both reconcile the target Secret's `data` down to *exactly* the keys that one
ExternalSecret produces. Neither merges. A non-`Merge` writer therefore deletes
every key its co-writers produce that it does not produce itself, on each
refresh, and those keys only come back when each of the others next reconciles.
(A non-`Merge` writer whose keys happen to cover all the others' deletes
nothing — see [coverage](#what-actually-determines-whether-keys-are-lost-coverage-not-policy-alone)
below — but that is a coincidence to be removed, not a design.)

`creationPolicy` defaults to `Owner` when omitted — omitting it is choosing the
unsafe policy.

**Measured impact.** `dhanam/dhanam-secrets` is written by three
ExternalSecrets. Two used `Merge`; the service-auth bridge used `Orphan`.

- 2026-06-13 — first incident. The runbook
  (`internal-devops/runbooks/2026-06-13-dhanam-secrets-degradation-incident.md`)
  identified the cause and prescribed this exact fix, plus a key-count alert.
  Neither was applied.
- 2026-08-06 — still happening, two months later. Every 15m the 2-key `Orphan`
  bridge reset the Secret; `-extended` re-merged 10 keys and core re-merged 13
  about 2.5 minutes later, so the Secret cycled **23 → 2 → 10 → 23 keys**. A
  6-sample / 42-minute probe caught **1 sample at 10 keys with `DATABASE_URL`
  and `DIRECT_DATABASE_URL` absent**. Any pod starting inside that window came
  up with no database URL and CrashLooped.
- Throughout both incidents **every ExternalSecret reported `Ready=True`**. The
  health signal we had could not see the failure — which is why the alert
  counts keys on the Secret and never reads an ExternalSecret condition.

Fixed in enclii#356 (all three writers now `Merge`).

### What actually determines whether keys are lost: coverage, not policy alone

`enclii-dhanam-staging/dhanam-secrets` has the same *shape* — a 33-key `Owner`
writer plus a 10-key `Merge` writer — and is **not** degrading. Four live
samples: 33 keys every time, all 10 of the `Merge` writer's keys present,
resourceVersion unchanged.

The difference is key coverage. The staging `Owner` writer's 33 keys are a
strict superset of the `Merge` writer's 10, so reconciling the Secret down to
its own key set removes nothing. In prod there was no superset relationship
(2 keys against 13 and 10), so each refresh genuinely wiped 21 keys.

So the precise rule is:

| State | Condition | Meaning |
|-------|-----------|---------|
| **FAIL** | 2+ writers, at least one `Owner`/`Orphan`, and its key set does **not** cover the union of the others' keys | Keys are deleted on every refresh. The uncovered key list is the finding. |
| **WARN** | 2+ writers, at least one `Owner`/`Orphan`, but its key set **does** cover all the others | Benign today, fragile. |
| **PASS** | One writer, or every writer is `Merge` | — |

**The WARN case has a trap.** The instinct is to flip the non-`Merge` writer to
`Merge`. That is wrong: no `Merge` writer will ever *create* the Secret, so an
all-`Merge` set leaves the target with no creator. The remedy is to remove the
redundant writer so the Secret has exactly one — or to accept it knowing that
**adding a single key to a `Merge` writer that the non-`Merge` writer lacks
silently converts it into the FAIL case**, with no other warning. That trap is
why this lives in a tool instead of in someone's memory.

Staging is deliberately left as-is: it is healthy, and it is the gate for a
production promote. Its manifests live in the dhanam repo, so running the check
there is what classifies that pair.

### Controls that enforce this

| Control | Where | Catches |
|---------|-------|---------|
| `scripts/check-externalsecret-writers.py` | CI job `ExternalSecret multi-writer policy` | A non-`Merge` writer that does not cover its co-writers' keys — reported with the exact keys wiped. Covering writers are WARNed, not blocked. |
| `SecretKeyCountBelowExpected` alert | `prometheus-rules` ConfigMap, `secret-integrity-rules.yml` | The Secret actually losing keys, regardless of what the ExternalSecrets report |
| `DhanamSecretsKeyCountUnmonitored` alert | same | The key-count signal itself going missing |

Checklist when adding a writer to an existing Secret:

1. Compare key sets first. If the existing non-`Merge` writer does not produce
   every key your new writer does, you are creating the FAIL case.
2. Prefer removing the need for a second writer at all.
3. If multiple writers are genuinely required: set `creationPolicy: Merge` on
   **every** writer, and confirm the target Secret already exists
   (`kubectl get secret <name> -n <ns>`) — create it first if not, because no
   `Merge` writer will.
4. Use `deletionPolicy: Retain` so removing one writer does not delete the
   shared Secret.
5. Add the expected key count to
   `infra/k8s/production/monitoring/secret-key-count-exporter.yaml` (target +
   Role + RoleBinding) so a regression pages instead of silently CrashLooping.

Audit any Secret by hand with:

```bash
kubectl get externalsecret -n <namespace> \
  -o custom-columns=NAME:.metadata.name,TARGET:.spec.target.name,POLICY:.spec.target.creationPolicy
```

## ExternalSecret Inventory

| Resource | Namespace | Vault Path | Key Count |
|----------|-----------|------------|-----------|
| `enclii-secrets` | enclii | `secret/enclii` + **`secret/comms`** (Resend fan-out) | 23 |
| `janua-secrets` | janua | `secret/janua` + **`secret/comms`** (Resend fan-out) | 10 |
| `data-secrets` | data | `secret/data` | 8 |
| `pgbackrest-r2-credentials` | data | `secret/pgbackrest-r2` | 4 |
| `cloudflare-secrets` | cloudflare-tunnel | `secret/cloudflare` | 1 |
| `dhanam-secrets` | dhanam | `secret/dhanam` | 11 (core) |
| `dhanam-secrets-extended` | dhanam | `secret/dhanam` | 13 (merges into `dhanam-secrets`) |
| `selva-secrets` | selva | `secret/selva` | 3 |
| `tezca-secrets` | tezca | `secret/tezca` | 11 |
| `yantra4d-secrets` | yantra4d | `secret/yantra4d` | 3 |
| `karafiel-secrets` | karafiel | `secret/karafiel` | 15 |
| `forgesight-secrets` | forgesight | `secret/forgesight` | 9 |
| `pravara-mes-secrets` | pravara-mes | `secret/pravara-mes` | 11 |
| `monitoring-secrets` | monitoring | `secret/monitoring` | 3 |
| `arc-runners-secrets` | arc-runners | `secret/arc-runners` | 3 |
| `enclii-builds-secrets` | enclii-builds | `secret/enclii-builds` | 3 |
| `npm-registry-secrets` | npm-registry | `secret/npm-registry` | 1 |
| `madfam-site-secrets` | madfam-site | `secret/madfam-site` + **`secret/comms`** (Resend fan-out) | 3 |
| `longhorn-secrets` | longhorn-system | `secret/longhorn-system` | 1 |
| `kyverno-secrets` | kyverno | `secret/kyverno` | 1 |

Files located at `infra/k8s/base/external-secrets/vault-secrets/`.

**Dhanam merge model (2026-06-16, corrected 2026-08-06):** `dhanam-secrets`
(core), `dhanam-secrets-extended` and the platform
`dhanam-ecosystem-service-auth` (kubernetes-store) all target the same K8s
Secret. **All three are now `creationPolicy: Merge`** — the service-auth bridge
was `Orphan` until enclii#356 and was wiping the other two every 15m (see
[the rule above](#rule-one-externalsecret-per-secret--and-if-not-every-writer-must-be-merge)).
Merged total: 23 keys. Optional keys (R2, Cloudflare, Sentry, SendGrid) are
intentionally omitted until intake — see
[recovery session](https://github.com/madfam-org/internal-devops/blob/main/runbooks/2026-06-16-dhanam-secrets-recovery-session.md).

## Operations

### Check Status

```bash
# Verify ClusterSecretStore is valid
kubectl get clustersecretstores -o wide

# List all ExternalSecrets and their sync status
kubectl get externalsecrets -A

# Check operator health
kubectl get pods -n external-secrets

# Verify a specific secret synced
kubectl get secret <name> -n <namespace> -o jsonpath='{.data}' | jq 'keys'
```

### Force Refresh

```bash
kubectl annotate externalsecret <name> -n <namespace> \
  force-sync=$(date +%s) --overwrite
```

### Add a New Secret

1. Write to Vault: `vault kv put secret/<namespace> key=value`
2. Add entry to the namespace's ExternalSecret YAML in `infra/k8s/base/external-secrets/vault-secrets/`
3. Commit and let ArgoCD sync, or `kubectl apply -f` directly

## Troubleshooting

```bash
# Check ExternalSecret status
kubectl get externalsecret <name> -n <namespace> -o yaml | yq '.status'

# Check operator logs
kubectl logs -n external-secrets -l app.kubernetes.io/name=external-secrets -f

# Verify Vault connectivity from ESO
kubectl exec -n vault vault-0 -- vault kv get secret/<namespace>

# Verify service account can authenticate to Vault
kubectl exec -n vault vault-0 -- vault read auth/kubernetes/role/eso-reader
```

## Related Documentation

- [Secrets Management Strategy](./SECRETS_MANAGEMENT.md)
- [Vault Operations Runbook](../runbooks/VAULT_OPERATIONS.md)
- [GitOps with ArgoCD](./GITOPS.md)
- [Cloudflare Integration](./CLOUDFLARE.md)
