# Service health observation

`enclii observe health --service <SERVICE_ID>` reads the same runtime evidence as
`GET /v1/observability/health`. A historical running deployment no longer counts
as evidence of live health when the Kubernetes observer is unavailable.

The observer uses the service's recorded namespace, falling back to its project
slug and then `default` as before. It tries the exact deployment name first. Only
a NotFound permits discovery within that namespace. Deployment or pod-template
`enclii.dev/service` labels bind a differently named workload to the service.
Legacy `app` labels are considered only when no explicit binding matches. An
explicit label for another service excludes the workload even if its app label
matches. Multiple candidates return unknown; the observer never picks the first.
An exact-name workload with conflicting explicit ownership also returns unknown.

The response exposes `deployment_name` on a successful observation and a stable
`observation_reason` on failure. Codes distinguish missing, ambiguous or
conflicting workload bindings, denied access, timeout and unavailable runtime.
Raw Kubernetes error messages are not exposed. Unknown services remain in the
existing `degraded_count` aggregate for compatibility; clients must use each
service's `status`. Zero pod counters in an unknown response are unavailable
measurements, not a confirmed outage. The operator view and `enclii ps` display
unavailable; an observed unhealthy zero-pod deployment still displays `0/0`.

The existing 20-second cache and bounded fan-out still apply. The API's uptime is
a deployment-history estimate. Response-time and error-rate zero fields are not
proof of measured performance. This endpoint describes Deployments; it does not
establish readiness for StatefulSets, tenant configuration, public routes or
business transactions. A ready public route and unknown inventory can coexist.

This resolver is read-only. Restart, scale, delete and exec continue to require
their explicit native targets. Use Enclii to diagnose the recorded binding and
runtime access; do not bypass missing adapters with direct production access.

Validation covers exact/aliased bindings, namespace isolation, ambiguity,
conflicting ownership, Kubernetes refusal/transport failures, the repository
fan-out and operator rendering with synthetic fixtures.

Boundary checkpoint: 2026-09-23; owner: platform engineering. Public API behavior
only; client topology and production incident evidence remain in internal-devops.
See [public repository policy](../PUBLIC_REPO_BOUNDARY.md).
