---
title: ops
description: Audited MADFAM operator workflows for Kubernetes, Argo, jobs, storage, policy, and runners
---

# `enclii ops`

`enclii ops` is the contract-first replacement layer for routine direct
`kubectl`, Argo, Longhorn, ExternalSecrets, Kyverno, CronJob, and ARC operations.

Mutating commands are dry-run by default. Use `--apply --reason "..."` only
when the corresponding server-side adapter is wired and the audit reason is
clear.

Read-only commands call live Switchyard adapters when configured. Current first
coverage includes Argo/Application status and drift summaries, pod
diagnosis/logs, CronJob inventory and manual triggers, PVC/PV inventory,
Longhorn CRDs, ExternalSecrets, Vault pod readiness, Kyverno
reports/exceptions, and ARC runner-scale-set inventory. If an
adapter is missing, the API returns `adapter_unconfigured` instead of
encouraging direct `kubectl`.

## Commands

| Command | Purpose |
|---------|---------|
| `enclii ops capabilities` | List server-supported operator capabilities |
| `enclii ops apps status|sync|sync-sweep|diff|retire|rollback` | Argo app inspection and remediation |
| `enclii ops pods diagnose|logs|restart` | Pod diagnosis, logs, and safe restarts |
| `enclii ops jobs list|trigger` | CronJob inspection and audited one-off execution from an existing template |
| `enclii ops storage volumes|pvc|longhorn|repair-plan|settings-apply|prune-detached|storageclass-apply` | PVC/PV/Longhorn inspection, repair planning, CPU settings (O-5), orphan prune (O-4), StorageClass reconcile |
| `enclii ops secrets external|vault|refresh|sync|sync-sweep|rotate|vault-backfill` | ExternalSecrets and Vault readiness workflows |
| `enclii secrets intake` | Chat-safe operator credential handoff into Vault ([secrets.md](./secrets.md#enclii-secrets-intake)) |
| `enclii ops policy violations|exceptions|waiver-plan|cosign-enable` | Kyverno policy visibility, waivers, cosign namespace enforce (O-11) |
| `enclii ops runners arc|drain` | ARC runner-set inspection and drain planning |

## Examples

```bash
enclii ops capabilities
enclii ops apps status digifab-quoting-services --json
enclii ops apps diff monitoring -n argocd --json
enclii ops apps retire legacy-app -n argocd --apply --reason "retire reviewed legacy Argo application"
enclii ops jobs list -n forgesight --json
enclii ops jobs trigger forgesight-mexico-wave-seed -n forgesight --apply --reason "populate verified market data"
enclii ops storage settings-apply
enclii ops storage settings-apply --apply --reason "GA O-5 Longhorn CPU"
enclii ops storage prune-detached
enclii ops storage prune-detached --apply --reason "GA O-4 orphan cleanup"
enclii ops storage storageclass-apply
enclii ops storage storageclass-apply --apply --reason "GA Longhorn StorageClass reconcile"
enclii ops policy cosign-enable
enclii ops policy cosign-enable --apply --reason "GA O-11 Cosign enforce"
./scripts/wave0-ga-ops.sh
./scripts/wave0-ga-ops.sh --apply --disk-prune --reason "GA Wave 0"
enclii ops pods diagnose switchyard-api -n enclii --json
enclii ops pods logs forgesight-pipeline-manual-abc123 -n forgesight --tail 500 --limit-bytes 524288 --json
enclii ops apps sync monitoring --apply --reason "clear Argo drift after reviewed manifest patch"
enclii ops apps sync-sweep -n argocd
enclii ops apps sync-sweep -n argocd --apply --reason "GA O-8 Argo sweep"
enclii ops secrets sync-sweep
enclii ops secrets sync-sweep --apply --reason "GA O-10 ESO reconcile"
enclii secrets vault-backfill enclii-secrets --namespace enclii --vault-path secret/enclii --external-secret enclii-internal-api-key --apply --reason "GA O-10 Vault backfill"
./scripts/post-deploy-ga-adapters.sh
./scripts/wave1-ga-ops.sh --apply --backup-drill --reason "GA Wave 1"
```

## Pod Log Controls

`enclii ops pods logs` is the default production log-inspection path. It reads
through Switchyard's audited Kubernetes adapter and supports bounded retrieval
without direct `kubectl` access:

| Flag | Description |
|------|-------------|
| `--tail` | Recent lines to request; default `400`; use `0` for all lines within `--limit-bytes` |
| `--limit-bytes` | Maximum bytes to return; default `262144`; server-capped at 2 MiB |
| `--container` | Optional container name for multi-container pods |

## Required Mutation Flags

| Flag | Description |
|------|-------------|
| `--apply` | Execute instead of returning a dry-run plan |
| `--reason` | Audit reason; required with `--apply` |
| `--idempotency-key` | Optional retry key for safely repeating an operation |
| `--namespace`, `--project`, `--service` | Scope selectors passed to the operation contract |

## `ops domains reconcile`

Provisions every hostname a service declares in its `enclii.yaml`: the proxied
DNS record, the junction, the tunnel ingress rule, and TLS. Idempotent — an
already-provisioned hostname is re-asserted, never duplicated.

Every credential is held server-side. You supply a service name and a reason;
you never supply a Cloudflare token, a zone id, or a tunnel id, and none is
returned.

```bash
# Plan: what is declared but not provisioned
enclii ops domains reconcile nauta-web

# Provision everything the manifest declares
enclii ops domains reconcile nauta-web \
  --apply --reason "crea-erp.madfam.io declared but never provisioned"

# Provision ONE hostname, leaving every other declared hostname untouched
enclii ops domains reconcile nauta-web --domain crea-erp.madfam.io \
  --apply --reason "route the declared ERP host"
```

`--domain` is the safe form when only one hostname needs attention. Unlike
`providers cloudflare tunnels-apply --project X`, which reconciles every
junction in the project and can rewrite unrelated backends, a scoped reconcile
touches exactly the hostname named. A hostname the manifest does not declare
plans nothing rather than falling back to all of them.

`enclii domains reconcile` is an alias for the same operation.

## `ops secrets provision-kalya-feed`

Mints a kalya standing-feed token and files it into its consumers' Vault paths,
entirely server-side.

```bash
enclii ops secrets provision-kalya-feed --tenant crea --consumers crea-map,nauta
enclii ops secrets provision-kalya-feed --tenant crea --consumers crea-map,nauta \
  --apply --reason "wire the crea standing feed"
```

Switchyard reads kalya's internal API key from `secret/kalya`, asks kalya to
mint the token, and writes:

| Consumer | Vault path | Properties |
|----------|-----------|------------|
| `crea-map` | `secret/crea-map` | `kalya_occupancy_feed_url`, `kalya_capacity_feed_url` |
| `nauta` | `secret/nauta` | `kalya_feed_tokens` (merged, so other tenants survive) |

The token is never returned by the API, never logged, and never reaches the
machine running the CLI. Idempotent: consumers already carrying this tenant's
properties are skipped and nothing is minted. `--rotate` is the explicit opt-in
to replace a live token.

`enclii secrets provision kalya-feed` is an alias for the same operation.

## Remaining Adapter Work

- `apps rollback`, `pods restart`, `storage repair-plan`, `secrets refresh`,
  `policy waiver-plan`, and `runners drain` are contract-only until guarded
  apply adapters are wired.
- `apps retire` deletes only the Argo Application by default and uses orphan
  propagation so live resources are not pruned during routine legacy-app
  retirement. Destructive cascade retirement must be explicitly requested by
  API callers.
- `jobs trigger` creates a Kubernetes Job from the existing CronJob
  `jobTemplate`; it does not edit the CronJob schedule, image, command, env, or
  secret references.
- `apps diff` now reads Argo Application sync/resource/condition drift; full
  desired-vs-live patch hunks remain a follow-up.
- Pod logs currently require a pod target; workload/deployment selector
  resolution is a follow-up, but tail depth and bounded byte retrieval are
  wired.
- Missing CRDs currently return failed read responses rather than soft-empty
  inventories.
