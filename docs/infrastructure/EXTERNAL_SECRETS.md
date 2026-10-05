# External Secrets Operator

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Last Updated:** 2026-10-05
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
| `janua-secrets` | janua | `secret/janua` + **`secret/comms`** (Resend fan-out) + `secret/dhanam` + `secret/coupler` | 16 (git-only — see [below](#janua-secrets-key-map)) |
| `data-secrets` | data | `secret/data` | 8 |
| `pgbackrest-r2-credentials` | data | `secret/pgbackrest-r2` | 4 |
| `cloudflare-secrets` | cloudflare-tunnel | `secret/cloudflare` | 1 |
| `dhanam-secrets` | dhanam | `secret/dhanam` | 11 (core) |
| `dhanam-secrets-extended` | dhanam | `secret/dhanam` | 13 (merges into `dhanam-secrets`) |
| `selva-secrets` | selva | `secret/selva` | 3 |
| `tezca-secrets` | tezca | `secret/tezca` | 11 |
| `karafiel-secrets` | karafiel | `secret/karafiel` | 15 |
| `forgesight-secrets` | forgesight | `secret/forgesight` | 9 |
| `pravara-mes-secrets` | pravara-mes | `secret/pravara-mes` | 11 |
| `monitoring-secrets` | monitoring | `secret/monitoring` | 3 |
| `arc-runners-secrets` | arc-runners | `secret/arc-runners` | 3 |
| `enclii-builds-secrets` | enclii-builds | `secret/enclii-builds` | 3 |
| `npm-registry-secrets` | npm-registry | `secret/npm-registry` | 1 |
| `verdaccio-auth` | npm-registry | `secret/npm-registry` (`htpasswd`) | 1 |
| `madfam-site-secrets` | madfam-site | `secret/madfam-site` + **`secret/comms`** (Resend fan-out) | 3 |
| `longhorn-secrets` | longhorn-system | `secret/longhorn-system` | 1 |
| `kyverno-secrets` | kyverno | `secret/kyverno` | 1 |

Files located at `infra/k8s/base/external-secrets/vault-secrets/`.

**Two readers of `secret/npm-registry` (2026-09-07).** `verdaccio-auth` lives
with the workload it serves (`infra/k8s/base/verdaccio/auth-externalsecret.yaml`),
not in `vault-secrets/`, because the Verdaccio deployment mounts that exact
Secret name and key. It reads the same Vault path as `npm-registry-secrets`
but writes a **different** target Secret, so the multi-writer rule does not
apply — each target has exactly one writer. `npm-registry-secrets`
(target `npm-registry-secrets`, key `HTPASSWD`) is consumed by nothing today
and is not present in the live cluster; it is left in place because
`scripts/check-zero-touch-boundaries.sh` allowlists the filename. Retiring it
is a separate change.

**Two readers of `secret/monitoring:alertmanager_smtp_password` (2026-10-05).**
`monitoring-secrets` (git-only, target `monitoring-secrets`, key
`ALERTMANAGER_SMTP_PASSWORD`) has always read it, and nothing mounts that key.
`alertmanager-smtp` (`infra/k8s/production/monitoring/alertmanager-smtp.externalsecret.yaml`,
synced by the `monitoring` Argo app) reads the same property into a
**different** target Secret, key `smtp-password`, which the Alertmanager
StatefulSet mounts as the file behind `smtp_auth_password_file`. Each target
has one writer, so the multi-writer rule does not apply. The property's only
writer is the intake target `monitoring/alertmanager-smtp`
([Secret Intake](../runbooks/SECRET_INTAKE.md#alertmanager-smtp-credential-2026-10-05)).
It replaced the hand-made `alertmanager-smtp-secret`.

**Dhanam merge model (2026-06-16, corrected 2026-08-06):** `dhanam-secrets`
(core), `dhanam-secrets-extended` and the platform
`dhanam-ecosystem-service-auth` (kubernetes-store) all target the same K8s
Secret. **All three are now `creationPolicy: Merge`** — the service-auth bridge
was `Orphan` until enclii#356 and was wiping the other two every 15m (see
[the rule above](#rule-one-externalsecret-per-secret--and-if-not-every-writer-must-be-merge)).
Merged total: 23 keys. Optional keys (R2, Cloudflare, Sentry, SendGrid) are
intentionally omitted until intake — see
[recovery session](https://github.com/madfam-org/internal-devops/blob/main/runbooks/2026-06-16-dhanam-secrets-recovery-session.md).

### `janua-secrets` key map

`vault-secrets/janua-secrets.yaml` (target `janua/janua-secrets`,
`creationPolicy: Merge`, `deletionPolicy: Retain`) maps 16 keys from four Vault
paths. `Merge` means the Secret can also carry operator break-glass keys that
this map does not list; ESO manages only the keys below.

| Target key | Vault path | Vault property |
|------------|------------|----------------|
| `database-url` | `secret/janua` | `database_url` |
| `redis-url` | `secret/janua` | `redis_url` |
| `secret-key` | `secret/janua` | `secret_key` |
| `field-encryption-key` | `secret/janua` | `field_encryption_key` |
| `jwt-secret` | `secret/janua` | `jwt_secret` |
| `jwt-private-key` | `secret/janua` | `jwt_private_key` |
| `jwt-public-key` | `secret/janua` | `jwt_public_key` |
| `dhanam-webhook-secret` | `secret/janua` | `dhanam_webhook_secret` |
| `oauth-github-client-id` | `secret/janua` | `oauth_github_client_id` |
| `oauth-github-client-secret` | `secret/janua` | `oauth_github_client_secret` |
| `internal-api-key` | `secret/janua` | `internal_api_key` |
| `resend-api-key` | `secret/comms` | `resend_api_key` |
| `<tenant>-resend-api-key` | `secret/janua` | `<tenant>_resend_api_key` ([#535](https://github.com/madfam-org/enclii/pull/535), 2026-09-07) |
| `resend-webhook-secret-<tenant>` | `secret/janua` | `resend_webhook_secret_<tenant>` ([#634](https://github.com/madfam-org/enclii/pull/634), 2026-09-26) |
| `federation-api-token` | `secret/dhanam` | `federation_api_token` |
| `janua-service-token` | `secret/coupler` | `janua_service_token` |

`resend-webhook-secret-<tenant>` is the signing secret for a client tenant's
Resend webhook (the real `<tenant>` rows are in `vault-secrets/janua-secrets.yaml`).
janua-api reads it as the optional env `RESEND_WEBHOOK_SECRET_<TENANT>`;
without it that tenant's webhook receiver answers 404 and its mail records no
delivery, bounce or open events.

Both tenant rows were merged to git and then made live **by hand**, because this
file is not synced by ArgoCD — see the next section.

## What ArgoCD actually syncs — and what it does not

**A merged change to a file under `vault-secrets/` does not reach the cluster.**
There is no OutOfSync, no drift alert, and no CI failure: the file is not part
of any Application's manifest set at all.

The `external-secrets-config` Application
(`infra/argocd/apps/external-secrets-operator.yaml`) syncs a five-entry
allowlist, not the directory:

```yaml
directory:
  include: '{cluster-secret-store.yaml,vault-cluster-secret-store.yaml,external-secrets-tokenreview-rbac.yaml,ecosystem-service-auth-external-secrets.yaml,README.md}'
```

| Path | Synced by ArgoCD? |
|------|-------------------|
| `external-secrets/cluster-secret-store.yaml`, `vault-cluster-secret-store.yaml` | yes |
| `external-secrets/external-secrets-tokenreview-rbac.yaml` | yes |
| `external-secrets/ecosystem-service-auth-external-secrets.yaml` | yes |
| **`external-secrets/vault-secrets/*.yaml`** (all 18 per-app manifests) | **no — excluded** |
| `verdaccio/auth-externalsecret.yaml` (lives with its workload) | yes, via `npm-registry-services` |

The exclusion is deliberate, and the in-repo comment says why: several legacy
mirror manifests in that directory do not match current production Secret
shapes, and syncing a stale mirror over a working Secret is worse than not
syncing it. But the consequence is a silent no-op on merge, and that has to be
stated rather than rediscovered.

### Why no Application picks these files up

Two independent mechanisms keep `vault-secrets/` out of ArgoCD, so fixing only
one of them would not bring the files under sync:

1. **The allowlist.** `directory.include` above names five files at the root of
   `infra/k8s/base/external-secrets/`. No `vault-secrets/*.yaml` file is on it.
2. **No recursion.** The Application's `directory` source does not set
   `recurse: true`, and ArgoCD directory sources are non-recursive by default,
   so the `vault-secrets/` subdirectory is skipped even before the allowlist
   applies.

No other Application references these files either. None of the
`project-applications` ApplicationSet sources (`infra/argocd/projects/*/config.json`)
points at `infra/k8s/base/external-secrets/`; `infra/k8s/production/kustomization.yaml`
mentions a `cloudflare-secrets.yaml` only in a comment; and product
repositories synced by their `<project>-services` Application (the janua
repository's `k8s/overlays/production`, for example) declare no ExternalSecret
of their own for these targets.

**How the live objects got there.** The only in-repo writer is the bootstrap
script `scripts/cluster-ops-deploy.sh` (phase 6, "Applying ExternalSecrets"),
which runs a client-side `kubectl apply -f` on every file in `vault-secrets/`,
one namespace at a time. After bootstrap, nothing in this repository writes
these objects again.

That is what the live object's field managers show. On 2026-09-26 the owner
found `janua/janua-secrets` managed by exactly three writers, and not by the
ArgoCD controller:

| Field manager | Where it comes from |
|---------------|---------------------|
| `kubectl-client-side-apply` | the bootstrap `kubectl apply` above |
| `kubectl-patch` | hand break-glass patches, such as the ones that made #535 (2026-09-07) and #634 (2026-09-26) live |
| `switchyard-api` | Enclii secret operations (`secrets sync`/`refresh`, `sync-sweep`, `rotate`, `vault-backfill`), which merge-patch **annotations only** (`force-sync` and the `enclii.dev/*` audit keys), never `spec` |

### Which ExternalSecrets are git-only and which are synced

| ExternalSecret manifest | Changes reach the cluster by |
|-------------------------|------------------------------|
| All 18 files in `external-secrets/vault-secrets/` (`.yaml`): `arc-runners-secrets`, `cloudflare-secrets`, `data-secrets`, `dhanam-secrets`, `dhanam-secrets-extended`, `enclii-builds-secrets`, `enclii-secrets`, `forgesight-secrets`, `janua-secrets`, `karafiel-secrets`, `kyverno-secrets`, `longhorn-secrets`, `madfam-site-secrets`, `monitoring-secrets`, `npm-registry-secrets`, `pravara-mes-secrets`, `selva-secrets`, `tezca-secrets` (yantra4d's ExternalSecrets live in the yantra4d repository, `k8s/production`, synced by `yantra4d-services`) | **hand patch of the live object only** (git-only) |
| `external-secrets/ecosystem-service-auth-external-secrets.yaml` | ArgoCD, `external-secrets-config` (automated, self-heal) |
| `verdaccio/auth-externalsecret.yaml` | ArgoCD, `npm-registry-services` |
| ExternalSecrets declared in a product repository's synced manifest path | ArgoCD, that project's `<project>-services` Application |

Before you change an ExternalSecret, find its row here. If it is git-only,
follow the procedure below; a merge alone does nothing.

### Current break-glass, and how it bit (2026-09-07)

[#535](https://github.com/madfam-org/enclii/pull/535) added a
tenant Resend key entry to `vault-secrets/janua-secrets.yaml`. It merged
green, and the key did **not** appear in `janua/janua-secrets`. It was made live
by a raw JSON patch against the live ExternalSecret object.

Until [#539](https://github.com/madfam-org/enclii/issues/539) lands a sanctioned
path (an `enclii secrets es-apply <app>` op, or reconciling the manifests to
live and widening the allowlist), that patch **is** the break-glass — and like
any break-glass it must record actor, reason, target, commands and result. Note
what it costs: the cluster and the repo now agree only by coincidence. Nothing
reconciles them, and the next hand-edit of that object drops the key with no
signal.

### It happened again (2026-09-26, #634)

[#634](https://github.com/madfam-org/enclii/pull/634) added
a tenant webhook signing secret (from `secret/janua#resend_webhook_secret_<tenant>`) to
`vault-secrets/janua-secrets.yaml` and merged green on 2026-09-26. The live
ExternalSecret did not change. The owner patched the live object by hand the
same day, and the key then synced. Same cause as #535, same fix — which is why
the procedure below is written down rather than rediscovered a third time.

### Procedure: changing a git-only ExternalSecret (until #539)

Do **both** halves — patch the live object **and** keep git in sync. A live-only
patch is invisible to the next reader of the repo; a git-only change is a no-op.

1. **Vault first.** Write the Vault property before anything references it
   (see the [ordering rule](#ordering-rule-vault-first-manifest-second)). The
   map is all-or-nothing: a missing property fails the whole ExternalSecret.
2. **Open the PR** against `vault-secrets/<app>-secrets.yaml`. Merging keeps
   git truthful; it does not deploy.
3. **Read the live object before patching it** (break-glass read of the
   ExternalSecret spec; it holds Vault references, not values). Legacy files in
   this directory can differ from live, so compare the live `spec.data` with
   the file.
4. **Patch additively; do not re-apply the file.** For a custom resource,
   `kubectl apply -f` replaces the whole `spec.data` list, so applying a stale
   file silently drops any live-only entry. Append exactly the entry the PR
   adds:

   ```bash
   kubectl patch externalsecret <name> -n <namespace> --type=json -p \
     '[{"op":"add","path":"/spec/data/-","value":{"secretKey":"<target-key>","remoteRef":{"key":"secret/<path>","property":"<property>"}}}]'
   ```

   This is break-glass: record actor, reason, target, command and result.
   To remove or rename an entry, read the live list first and `remove` it by
   index.
5. **Force a reconcile through Enclii**, not a raw annotation:

   ```bash
   enclii secrets sync <name> --namespace <namespace> --apply --reason "<why>"
   # same operation: enclii ops secrets sync <name> -n <namespace> --apply --reason "<why>"
   ```

   It merge-patches the `force-sync` annotation plus `enclii.dev/last-ops-*`
   audit annotations. It never reads, prints or writes a secret value, and it
   does **not** apply manifest changes — step 4 is still required.
6. **Verify** the ExternalSecret reports `SecretSynced` and the target Secret
   has the new key (compare key names or count, never values).
7. **Roll the consumer.** Env vars sourced from the Secret are read at pod
   start, so the running pods keep the old environment until they restart.
   `enclii ops pods restart` is **not implemented yet** (the apply returns
   HTTP 501, see [ops.md](../cli/commands/ops.md#remaining-adapter-work)).
   Use the Enclii service restart API (`POST /v1/services/{id}/restart`,
   admin role; SDK `services.restart()`) when the workload is a registered
   Enclii service whose Deployment carries the service name in the project
   namespace; otherwise do a rollout restart of the consuming Deployment as a
   recorded break-glass step.
8. **Confirm git and live agree** on the entry you added before you close the
   change.

### Ordering rule: Vault first, manifest second

An ExternalSecret is **all-or-nothing**. One missing Vault property fails the
whole sync, and the target Secret keeps its old data — or, on a first sync, is
never created at all.

1. Write the Vault property **first**. Use `vault kv patch secret/<path>
   key=value` when the path exists, and `vault kv put` when it does not: KV v2
   answers **404** to a patch on a path that has never been written.
2. Only then apply the manifest change.

That 404 is not hypothetical: it is exactly how the first Verdaccio htpasswd
rotation failed on 2026-09-07, because
[#529](https://github.com/madfam-org/enclii/pull/529) shipped the
`verdaccio-auth` ExternalSecret before `secret/npm-registry` had ever been
written. See
[npm-registry.md](./npm-registry.md#2026-09-07-the-secret-was-pruned-before-the-vault-key-existed).

---

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

Enclii-first (audited; annotations only, no secret values):

```bash
enclii secrets sync <name> --namespace <namespace> --apply --reason "<why>"
```

Break-glass equivalent:

```bash
kubectl annotate externalsecret <name> -n <namespace> \
  force-sync=$(date +%s) --overwrite
```

A refresh re-reads Vault for the entries the **live** object already maps. It
does not pick up a merged change to a git-only manifest.

### Add a New Secret

1. Write to Vault **first** — `vault kv patch secret/<namespace> key=value` if
   the path exists, `vault kv put` if it has never been written (KV v2 answers
   404 to a patch on a new path). The manifest is all-or-nothing: a property
   the manifest references but Vault lacks fails the entire sync.
2. Add the entry to the namespace's ExternalSecret YAML in
   `infra/k8s/base/external-secrets/vault-secrets/`.
3. Get the change onto the cluster. **Merging is not enough** —
   `vault-secrets/` is excluded from the `external-secrets-config` Application,
   so a merged manifest change is a silent no-op. Follow
   [Procedure: changing a git-only ExternalSecret](#procedure-changing-a-git-only-externalsecret-until-539);
   [#539](https://github.com/madfam-org/enclii/issues/539) tracks the sanctioned
   path being built.
4. Confirm it landed: the ExternalSecret reports `SecretSynced`, and the target
   Secret's key count went up by what you added —
   `kubectl get secret <name> -n <ns> -o jsonpath='{.data}' | jq 'keys | length'`.

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
