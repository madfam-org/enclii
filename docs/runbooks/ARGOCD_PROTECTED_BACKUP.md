# Fixed Argo CD protected backup

This source prepares a native backup adapter. Activation, actual Vault/R2
custody, retention, SQL migration acceptance, and isolated restore remain held.
`ARGOCD_PROTECTED_BACKUP_ENABLED` defaults to false. This change does not enable
it, create keys, call providers, upgrade a controller, or enable financial,
fiscal, or allocation execution.

The backup-only typed key reader uses existing native Vault config without
enabling the generic writer or rotation workers. Key initialization uses
the reviewed fixed SecretIntake profile through a separate typed binding.
Preparation and apply never create keys.

The adapter reuses the existing server-held R2 client with the separate fixed
`argocd-backups/v1` prefix and the fixed native SecretIntake envelope profile.
This source reuse does not qualify a backup-purpose ACL or retention binding.
The tenant-export upload/delete/presign surface refuses that protected prefix;
normal export roots and the underlying tenant storage transport are unchanged.

## Fixed native API

Every route requires a currently active, SQL-resolved human platform operator.
API tokens, cached role claims alone, and tenant-scope rollback cannot authorize
it. Bodies are closed, bounded JSON; there are no caller manifests, images,
commands, namespace/resource selectors, provider URLs, paths, buckets, partial
capture switches, rotation, or overwrite controls. Responses use no-store headers
and contain bounded metadata only.

| Method | `/v1/admin/argocd/protected-backups` suffix | Contract |
|---|---|---|
| POST | `/plan` | SQL-writing preparation: `key_origin_id`, exact positive `key_version`, and a bounded reason; server mints operation/plan/backup IDs |
| GET | `/current` | Fixed open-operation metadata after a lost preparation response; no guessed operation ID or provider mutation |
| POST | `/:operation_id/apply` | Exact retained `plan_id`, current authorization, unchanged private capture, and an acknowledged durable dispatch claim before one conditional upload |
| GET | `/:operation_id` | Actor-bound retained metadata; an attempted unresolved operation may perform same-object GET/authentication recovery only |
| POST | `/:operation_id/retire` | Exact `plan_id` and closed `reason_code`: `expired`, `source_drift`, or `operator_withdrew_before_dispatch` |

Preparation reads the exact retained SecretIntake origin/KV version, captures
and encrypts the complete supported private scope in memory, then stores the
immutable plan, ciphertext and reference in SQL. The review response follows
only an acknowledged commit. Preparation is not a read-only dry run. It does
not upload anything or modify Kubernetes. Recover a lost preparation response
through `/current`; an unacknowledged first transaction that truly rolled back
cannot have dispatched an external write.

Apply rechecks the retained private object graph before dispatch. The SQL
journal's reservation and claim commits must both be acknowledged to the same
execution before a single `If-None-Match: *` R2 PUT. An existing reservation,
claim, replay, or uncertain commit permits only same-ID GET/authentication;
there is no automatic PUT replay, new operation, new key, or replacement nonce.
An unknown or missing provider acknowledgement never releases the fence.

The installation-wide active-plan fence also survives Namespace recreation.
It blocks replacement of an unresolved operation. A stale/expired unclaimed
plan can be explicitly retired only under the same SQL lock with exact plan
binding and a PREPARED-only history. Any reserve, claim, provider attempt,
uncertainty, or unknown event refuses retirement. An acknowledged retirement
closes only that fence, preserves the immutable ciphertext/history, permanently
prevents that plan from applying, and creates no replacement. An uncertain
retirement commit must be resolved through status.

## Fixed key initialization and exact recovery

`POST /v1/secrets/intake/argocd-backup-envelope` accepts only a bounded reason
from an active SQL human platform operator, behind the same default-off backup
flag. It calls the fixed NativeSecretIntake registry/generator/opaque-origin and
bounded whole-group CAS. It enables no generic writer/rotation route and accepts
no supplied values, profile, destination, partial group, entropy or rotation
input. The generic intake endpoint refuses this target before generation or
Vault dispatch, even when its broad writer is enabled; it cannot bypass the
backup-purpose gate. Existing complete, partial or normalized-alias custody
refuses replacement.

Successful acknowledgement returns `initialized_held`, opaque origin and exact
KV version; ordinary intake metadata is cached for 24 hours. Ambiguous write
acknowledgement returns `UNKNOWN` plus origin and `expected_version`, explicitly
not a committed `vault_version` or initialization success. Do not rerun
initialization. The fixed no-body
`GET /v1/secrets/intake/argocd-backup-envelope/:origin_id/:version` reads that
canonical exact reference, clears key bytes and returns held metadata only.
Missing or mismatched custody holds without a latest-value or new-key fallback.
Generic/staging initialization retains its original zero-on-error semantics.

An existing `/v1/secrets/intake/:id` status can recover a successful reference
only if its intake ID remains known within the cache lifetime. A completely lost
ID/origin receipt has no native discovery surface in this slice; a protected
operator custody/catalog qualification remains necessary. Retrying create-only
intake cannot recover or rotate an existing group.

## Protected capture boundary

The profile supports the observed non-HA installation only. All 60 fixed
upstream identities are mandatory, including the two Secrets that metadata
inventory intentionally never read. Full actual RBAC rules, config, credential
payloads, CRDs, workload commands/environment/storage/scheduling overrides and
metadata remain inside the protected payload. Upstream defaults never replace
actual objects, and inventory projections are not backup payloads.

The fixed `argocd` namespace lists must be complete, nonempty and bounded:
Secrets 128, ConfigMaps 128, Applications 512, ApplicationSets 128, AppProjects
128. Additional bounded discovery lists check workloads, Services,
NetworkPolicies, ServiceAccounts, Roles and RoleBindings. Truncation,
continuations, remaining items, duplicate identities/UIDs, foreign namespace,
unknown extensions, unresolved references, deletion, denied reads, or payload
limits hold the operation. No error is interpreted as an empty successful list.

The optional auxiliary allowlist is SA/default plus the exact source-governed
Role/RoleBinding pairs for `switchyard-argocd-application-manager`,
`switchyard-argocd-tracking-declarer`, `switchyard-argocd-inventory-reader`, and
`switchyard-argocd-protected-backup-reader`. Present payloads are preserved and
counted separately; absent objects are never synthesized. Bindings require the
source-bound Role and singleton `enclii/switchyard-api` ServiceAccount subject.
Other bindings/SA references must resolve in the captured fixed graph. The
legacy CronJob backup reader/binding remains an explicit extension review hold.

Responses are byte bounded before JSON parsing on success/error/redirect paths.
A fixed object is capped at 2 MiB, a list and complete plaintext at 32 MiB; error
bodies at 64 KiB. Full membership, UID/resourceVersion and canonical protected
object bytes must match before/after capture and against the reviewed plan.
Observed list descriptors are retained privately; list revisions can advance
for unrelated global writes. Repeated equality does not establish an atomic
cluster snapshot or restore readiness.

## Receipts and remaining gates

Public rows contain opaque operation/plan IDs, structural counts, ciphertext
size/digest, expiry and a closed phase. They never contain plaintext, private
value hashes, key bytes, raw object maps, provider errors, URLs or presigned
backup access. Keys and plaintext remain in backend memory; their byte buffers are cleared
when the operation returns. No plaintext files are created. SQL retains ciphertext, exact key origin/version,
NamespaceUID, actor and immutable plan/history. Its fingerprint binds only
identity/count metadata and ciphertext, never a protected-value digest.

Authenticated readback means the retained object was read and authenticated
against that exact plan at the recorded attempt. Terminal status is historical
journal evidence, not a fresh provider retention claim. Every phase continues
to report `native_ready: false` and `restore_qualified: false`.

Activation still requires normal reviewed release acceptance, actual SQL
migration/concurrency proof, purpose-bound R2 prefix ACL/retention, exact Vault
version retention/access authority, and resource/transport qualification.
Isolated restore needs a separately qualified fixed executor with a real
loopback-only apiserver/etcd, no workers/controllers/Argo, no ambient production
cluster/Vault/R2 credentials, enforced egress isolation and memory-backed cleanup.
The existing vCluster service is metadata-only. Redacted substitutions prove
schema compatibility only; exact private roundtrip proof must remain inside
that isolated custody boundary. No restore or upgrade executor is provided here.

## Source qualification

Focused unit/fake-transport and owned real PostgreSQL integration evidence are
separate. The `argocdbackup/journal_postgres_test.go` integration tag requires
its exact disposable owned fixture environment and fails without it; there is
no ambient/default DSN fallback or skipped proof. Default unit CI does not run
that tagged proof. Target-server migration acceptance and normal affected-context
CI remain release gates even when the local owned PostgreSQL proof passes.
