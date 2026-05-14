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
| `enclii ops apps status|sync|diff|rollback` | Argo app inspection and remediation |
| `enclii ops pods diagnose|logs|restart` | Pod diagnosis, logs, and safe restarts |
| `enclii ops jobs list|trigger` | CronJob inspection and audited one-off execution from an existing template |
| `enclii ops storage volumes|pvc|longhorn|repair-plan` | PVC/PV/Longhorn inspection and repair planning |
| `enclii ops secrets external|vault|refresh` | ExternalSecrets and Vault readiness workflows |
| `enclii ops policy violations|exceptions|waiver-plan` | Kyverno policy visibility and waiver planning |
| `enclii ops runners arc|drain` | ARC runner-set inspection and drain planning |

## Examples

```bash
enclii ops capabilities
enclii ops apps status digifab-quoting-services --json
enclii ops apps diff monitoring -n argocd --json
enclii ops jobs list -n forgesight --json
enclii ops jobs trigger forgesight-mexico-wave-seed -n forgesight --apply --reason "populate verified market data"
enclii ops storage pvc redis-pvc -n enclii
enclii ops pods diagnose switchyard-api -n enclii --json
enclii ops apps sync monitoring --apply --reason "clear Argo drift after reviewed manifest patch"
```

## Required Mutation Flags

| Flag | Description |
|------|-------------|
| `--apply` | Execute instead of returning a dry-run plan |
| `--reason` | Audit reason; required with `--apply` |
| `--idempotency-key` | Optional retry key for safely repeating an operation |
| `--namespace`, `--project`, `--service` | Scope selectors passed to the operation contract |

## Remaining Adapter Work

- `apps rollback`, `pods restart`, `storage repair-plan`, `secrets refresh`,
  `policy waiver-plan`, and `runners drain` are contract-only until guarded
  apply adapters are wired.
- `jobs trigger` creates a Kubernetes Job from the existing CronJob
  `jobTemplate`; it does not edit the CronJob schedule, image, command, env, or
  secret references.
- `apps diff` now reads Argo Application sync/resource/condition drift; full
  desired-vs-live patch hunks remain a follow-up.
- Pod logs currently require a pod target; workload/deployment selector
  resolution is a follow-up.
- Missing CRDs currently return failed read responses rather than soft-empty
  inventories.
