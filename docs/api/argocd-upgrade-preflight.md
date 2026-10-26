# Argo controller upgrade inventory

`GET /v1/admin/argocd/upgrade-preflight` is a human platform-only, read-only
inventory. It verifies the authenticated user's current active platform rank
in SQL even when tenant-scope enforcement is disabled. The request accepts no
query parameters or body. Responses, including refusals, use `Cache-Control:
no-store`.

The response schema is `enclii-argocd-controller-upgrade-inventory-v1`. Its
phase is always `HELD_READ_ONLY_INVENTORY`. A successful HTTP read establishes
neither upgrade readiness nor authority to apply or adopt resources.

The fixed profile covers the union of the upstream Argo CD v3.3.17 non-HA and
HA install identities at commit
`30abed52c30dbb5cf8fdd32076c6bcc01799087c`, excluding Secrets, plus the
`argocd` Namespace. The only CRDs requested are the three Argo definitions;
fixed install ClusterRole/ClusterRoleBinding identities are also read.
Deployment and StatefulSet lists are confined to `argocd`, with at most 16
objects per kind. Continuation, remaining items, additional or missing workload
identities, and mixed Redis layouts produce unknown-layout holds. This
classifies controller-template layout; it does not inspect running Pods or
prove a runtime version. Unknown non-workload installation extensions are not
enumerated or adopted.

Each available object carries its UID and resourceVersion, known ownership
identities, tracking-annotation presence, and bounded field-manager metadata.
Object and known-owner UIDs must be canonical nonnil UUIDs. Unknown owners'
UIDs, manager names and owner names are withheld. A known owner requires an
exact fixed apiVersion, kind, name and valid scope. Managed-field paths expose
only allowlisted structural fields; private config keys and associative-list
selectors are withheld. A structural path digest covers only that sanitized
projection. It cannot substitute for an SSA conflict dry-run or identify every
private conflicting path. The reads are fresh individual observations, not an
atomic installation-wide snapshot or a mutation precondition.

ConfigMap values are limited to explicit tracking-mode, default instance-label,
log-level, boolean and numeric settings. Container templates expose only exact
public image references from the frozen Argo 3.2.5/3.3.17 cohorts; custom tags
and unqualified digest references are withheld. They also expose a small fixed command/argument list,
and explicit nonsecret log settings. Unknown values and credential references
produce counts, presence indicators and holds. No unknown inline value, config
value, node-selection value, or raw managed-field selector is fingerprinted.
Secret, Pod and Node resources are never requested. Provider, OIDC, RBAC policy,
trust, placement, mounts and security-context bodies are never returned.
Transport errors are reduced to fixed classifications.

The three CRDs expose bounded served/storage/stored-version and Established
metadata. Full schemas, conversion URLs and provider data remain private.
Unknown or oversized projections remain held; no truncated projection counts
as complete evidence. The reader adds no RBAC grant. Missing existing read
permission remains an explicit per-object hold.

Before an actual upgrade, separate native mechanisms still need to establish:

- Protected complete configuration and credential custody, a fresh encrypted
  backup, and a successful isolated restore with identity qualification.
- Immutable multi-architecture OCI digests, signatures/provenance and policy
  admission for the complete selected install cohort.
- Actual CRD schema compatibility and restored-resource roundtrip without
  field loss or conversion surprises.
- A reviewed actual override overlay and reduced RBAC preservation, followed
  by no-force SSA admission and separately reviewed ownership transfers.
- An authorized fixed-source apply/status operation with atomic UID/RV
  refusal, durable per-object outcomes and same-operation recovery after an
  uncertain outcome. No apply, force, delete, upgrade or rollback route exists
  in this adapter.
- Patched runtime cohort, isolated forbidden delete-hook tests, and normal
  root/ApplicationSet/auth/fleet reconciliation acceptance. Self-management
  adoption requires a separate supported native proof.

This reader does not activate financial or fiscal execution and makes no
compliance-completion claim.
