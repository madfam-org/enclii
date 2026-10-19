# Independent Jobs for verified service targets

`POST /v1/ops/jobs/run-target` creates a deadline-bounded independent Job from
the explicitly selected container of an already verified Deployment binding.
It requires the server-resolved platform operator rank, regardless of the
tenant-scope rollout flag. Apply rechecks that rank in SQL under the same
service/intent locks used by target binding and workload mutations.

Use this when an observer must retain the service's runtime context and remain
running after the web is stopped. Required pod affinity places the Job with the
verified Deployment's selector in its exact namespace. The affinity is ignored
after scheduling; stopping the web does not delete the independent Job. An
empty target has no matching pod for initial scheduling. This is a placement
requirement, not proof of database connectivity or a maintenance window.

## Plan and apply

Send a JSON request through an authenticated Enclii API client:

```json
{
  "operation": "ops.jobs.run-target",
  "dry_run": true,
  "scope": {
    "project": "fixture",
    "environment": "production",
    "service": "11000000-0000-4000-8000-000000000002"
  },
  "args": {
    "namespace": "fixture",
    "deployment": "fixture-web",
    "container": "web",
    "command": ["node", "-e", "console.log('fixture observer')"],
    "timeout": 60
  }
}
```

Review the returned target identity, immutable source revision, container,
command fingerprint, deadline and `plan_sha256`. Apply the same scope/args with
`dry_run: false`, `expect_plan_sha256` set to the reviewed hash, a fresh canonical
UUID `idempotency_key` and a bounded nonempty `reason`. Apply rereads source and
native identity after obtaining intent locks and refuses a changed plan.

The body is limited to 64 KiB, rejects unknown and duplicate keys, and requires
1–32 command arguments and a deadline of 1–1800 seconds. Do not put credential
values in commands or reasons. Refer to the selected container's existing
runtime environment. The new operation is API-only; the generic CLI's string
argument decoder cannot express this closed array request.

## Runtime boundary

The Job retains the selected image, working directory, environment references,
resources, security context, pull credentials and service account identity.
It retains node selectors, tolerations, affinity, topology spread, scheduler,
priority/runtime class and DNS configuration. It adds required colocation; it
does not relax those constraints. Unsupported constraints can leave it pending;
inspect scheduling through Enclii rather than weakening them automatically.

It has no workload routing labels, ports, probes, volume mounts, service-account
token mount, init containers or Deployment owner reference. Host namespace
sharing, an explicit node name, privileged containers, init dependencies,
volume mounts/devices, resource claims and scheduling gates are refused.
Existing admission and NetworkPolicy still apply. A policy selecting only the
web's labels may reject the Job; no ownership labels or network/RBAC exceptions
are added to bypass it. No `pods/exec` permission is required or granted.

This is an operator command runner. It does not certify that an arbitrary
command is read-only. An observation command must separately enforce its
expected database identity, bounded READ ONLY transaction, aggregate-only
output and conservative activity visibility. A snapshot cannot prove that an
independent writer will stay paused. Source tests do not prove live placement,
transport or survival of a later workload stop.

## Submission receipts and uncertain outcomes

The exact UUID determines `target-job-<uuid>`. Before submission, an immutable
`<job>-intent` ConfigMap records request/plan fingerprints. After creation, an
immutable `<job>-receipt` records the actual Job UID and intent UID. The Job is
owned by the intent, so a later web stop does not collect it. Receipts contain
no commands, environment values or credentials. They use existing ConfigMap
create/read permissions; this operation adds no cluster permission.

The Job has no retries and expires one day after completion. Intent/result
receipts have no automatic deletion or expiry. Reusing the key acknowledges
only an exact request/actor with both immutable receipts, matching owner and
actual Job UID, and a confirmed reconciliation audit transaction. A missing,
expired or recreated Job, changed request, missing receipt or uncertain native
result is refused; it is never resubmitted automatically.

Treat HTTP acceptance as submission, not command success. Verify actual image,
placement, terminal exit status and bounded logs through Enclii. If native
creation or the SQL audit/commit is uncertain, inspect the same deterministic
Job and receipts. Do not change keys or repeat execution until the outcome has
been reconciled. Keep verified submission receipts when cleaning up completed
Jobs.

See [verified targets](ENCLII_EXPLICIT_DEPLOYMENT_TARGETS.md) for the binding's
source and immutable ownership prerequisites.

**Boundary checkpoint — 2026-10-19; owner: platform maintainer.** This public
runbook describes the API, safeguards and synthetic proof limits. Deployment
identity, custody and runtime receipts remain in private internal-devops and
the labspace audit lane. Policy: [public repository boundary](../PUBLIC_REPO_BOUNDARY.md).
