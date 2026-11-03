# Inert Janua staging materialization

Last updated: 2026-11-02

Boundary checkpoint (2026-11-02, platform engineering): public-safe capability and synthetic fixtures only. Reviewer/owner: platform engineering. See the [public repository boundary](../PUBLIC_REPO_BOUNDARY.md). Actors,
native identities, custody and receipts remain in the
[private operations record](https://github.com/madfam-org/internal-devops).

## Fixed scope

This adapter retains held source/native identity **outside active Services**.
It never retargets global repository onboarding, creates a native043 binding,
projects secrets, allocates storage, grants a principal access, creates a DB,
migrates, activates pods or authorizes promotion. `auto_deploy=false` alone would
not exclude singular repository lookups, preview and background consumers.

It requires an existing same-project staging environment, completed repository
ownership and separately recorded immutable held intent. The actual namespace UID
must be owned, signature admission enabled, and the exact owned DNS-only baseline
UID/spec must match. No shared-data-consumer labels, pods, foreign workloads,
autoscalers, Jobs/CronJobs, foreign services or inherited Argo global permissions
are allowed. Any zero ReplicaSets must belong to this operation's Deployments.

Source is an exact Janua repository revision and the reviewed flat v1 profile.
Four resource-file hashes pin the public held source from Janua `96545d96`.
Resource changes require a reviewed profile change; allowed image aliases accept
immutable digest pins only. Source validation does not qualify image signatures.

## Operator API and effects

`POST /v1/admin/projects/janua/environments/staging/inert-materialization`
requires platform operator and project access. Review JSON contains exactly
`operation_id`, `held_intent_id`, `revision` and `dry_run: true`. Apply adds
`expect_plan_sha256` and a bounded private `reason`, with `dry_run: false`.
Namespace, image, SQL, path, role, UID, credential and activation selectors refuse.
Apply rechecks current active platform admin, owned metadata, absent active-service
collisions and exact plan CAS.

Only six native objects are selected:

- `janua-staging` AppProject: one repo/destination, no cluster resources,
  Deployment/Service whitelist only.
- `janua-staging` Application: exact source SHA; automatic sync/prune/self-heal off.
- API and Redis Deployments: zero replicas, reviewed source and operation ownership.
- API and Redis Services: namespace-local ClusterIP selectors and fixed ports.

The source's ESO resources and Redis PVC remain deferred. The AppProject refuses
those kinds. The Application is expected to remain held/out of sync; this is not
a healthy staging claim. No broad Argo sync is invoked.

Migration046 adds immutable operation, native-object and completed-authority
history only. It never inserts active services/addons/onboarding/targets. Rollback
locks all history before checking emptiness and refuses retained history.

Prepared intent commits before any create. Execution locks the operation/project
and current operator/ownership metadata. Each create has fresh namespace/inventory
checks. There is no update/delete/repair/replica-release branch. Existing objects
must match the same retained operation and source projection. Actual API-assigned
defaults are captured in full canonical readbacks; subsequent hashes/UIDs must
match. Foreign ownership, drift, extra containers and host namespaces refuse.

Uncertain writes retain prepared intent and any zero-replica partial objects.
Inspect/retry the **same UUID**; no automatic deletion or invented UID is allowed.
Completion requires six actual object receipts and final fresh readbacks.

`GET .../inert-materialization/{operation_id}` independently rechecks current
ownership and native UID/spec hashes. Partial/unconfirmed operations have no
completed authority. Results retain `activation_held=true`,
`active_service_registered=false`, `native_043_binding_proven=false` and
`runtime_qualified=false`.

This is an identity observer. Later DB steps independently qualify their exact
additional network policies, capacity/budget, image admission, owner/runtime/CA,
role flags and migration. Additional policies do not become network qualification
through this reader.

## Release gates

Source/CI does not apply migration046 or materialize anything. Deployment and
explicit operator native apply remain separate. DB/custody/fixed migration,
Secret/PVC projection, deliberate activation, synthetic auth/recovery and
continuous native runtime soak are separate increments. Promotion cannot consume
an inert receipt as runtime readiness. The canonical pending-work list remains
[ROADMAP](../../ROADMAP.md#engineering-backlog-platform-and-fleet-wide).
