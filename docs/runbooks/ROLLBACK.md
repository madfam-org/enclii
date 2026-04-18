# Rollback Runbook

_Last updated: 2026-04-17 — P0.5 instant rollback shipped_

Enclii supports two rollback strategies. They coexist; pick based on urgency
and whether the change needs to be durably captured in git.

| Strategy                    | Time    | State-of-record  | When to use                                             |
| --------------------------- | ------- | ---------------- | ------------------------------------------------------- |
| **Instant (selector flip)** | <30–90s | K8s Service spec | Bad prod push, error rate spiking, need traffic away NOW |
| **Manifest commit (ArgoCD)**| 2–3 min | git repo         | Durable rollback, DR reversion, compliance-audited prod |

## Strategy 1: Instant rollback (Service-selector flip)

### How it works

1. Each enclii Deployment labels its pods with `enclii.dev/deployment=<uuid>`
   (applied in `apps/switchyard-api/internal/reconciler/manifest.go`).
2. Kubernetes retains the last `revisionHistoryLimit` (default: 10)
   ReplicaSets per Deployment, so old pods are usually still around — or
   scalable back up — for minutes-to-hours after a new push.
3. Instant rollback flips the K8s `Service.spec.selector` to include
   `enclii.dev/deployment=<target-uuid>`, pinning traffic to the target
   ReplicaSet's pods.
4. If the target ReplicaSet was scaled to 0, we scale it back first
   (takes the full pod startup time).
5. ArgoCD continues reconciling the Deployment's manifest in the
   background — the selector flip is an additive change that doesn't
   conflict with manifest reconciliation.

### Invocation

**UI (one-click):** open a service → Deployments tab → click
"Rollback to here" on the target row → fill optional reason (and change
ticket URL for prod) → "Flip traffic".

**CLI:**

```bash
# Roll back to the previous running deployment (auto-detected)
enclii rollback <service> --instant --env production \
  --change-ticket https://linear.app/...

# Roll back to a specific deployment UUID
enclii rollback <service> --instant --to <deployment-uuid> \
  --reason "500s after checkout refactor" \
  --change-ticket https://linear.app/...
```

**API:**

```bash
curl -X POST "$ENCLII_API/v1/services/$SERVICE_ID/rollback" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "target_deployment_id": "<uuid>",
    "reason": "regression in X",
    "change_ticket_url": "https://linear.app/..."
  }'
```

### Response

```json
{
  "message": "Traffic flipped successfully",
  "took_ms": 2400,
  "scaled_up": false,
  "from_deployment_id": "<uuid>",
  "to_deployment_id": "<uuid>",
  "ready_replicas": 2,
  "strategy": "instant_selector_flip",
  "namespace": "..."
}
```

### HITL guard

Production environments (`env.Name == "production" | "prod"`) require a
`change_ticket_url` — the API returns `403` with
`"change_ticket_url is required for production instant-rollback"` otherwise.
Staging/dev: direct, no ticket required.

Actor (Janua subject) is captured in the audit event and the
`enclii.dev/rollback-actor` annotation on the K8s Service.

### Audit

A `deploy.rolled_back` lifecycle event is written to
`deployment_lifecycle_events`. Metadata includes `took_ms`, `scaled_up`,
`from_deployment_id`, `to_deployment_id`, `ready_replicas`,
`change_ticket_url`, `reason`, and `strategy: "instant_selector_flip"`.

Query via:

```bash
curl "$ENCLII_API/v1/lifecycle-events?event_types=deploy.rolled_back"
```

### When NOT to use instant rollback

- **StatefulSets** — Service-selector flip doesn't map cleanly to
  ordered-pod semantics. Use `enclii rollback` (default manifest-commit
  strategy) instead.
- **DB schema migrations** — instant rollback reverts the app image, not
  the schema. If the newer release ran a migration, the older pods may
  500 against the new schema. Revert the migration first, then roll back.
- **Services with external LB type=LoadBalancer** — works, but the external
  LB sees normal endpoint churn as the Selector's pod set changes; expect
  a brief `502` window at the LB if clients hold connections <5s.
- **Cross-service coordinated rollback** — use deployment-groups.

## Strategy 2: Manifest-commit rollback (ArgoCD)

The original path. Writes a new image tag to the Deployment manifest (or
the ecosystem repo's kustomization.yaml) and lets the Deployment controller
do a rolling update. ArgoCD treats this as the state-of-record.

```bash
enclii rollback <service> --env production
```

Takes 2–3 minutes because it's a full rolling update: new pods have to
schedule, pull the image (cached, usually fast), pass readiness, and the
old pods are drained.

Use this when you want the rollback durably captured in git (the instant
flip is an ephemeral K8s-level change — ArgoCD will reconcile it away when
the manifest changes next).

## Verifying a rollback

```bash
# Check which deployment is currently serving traffic
kubectl get svc <service> -n <namespace> -o jsonpath='{.spec.selector.enclii\.dev/deployment}'

# Confirm pod labels match
kubectl get pods -n <namespace> -l enclii.dev/deployment=<uuid> -o wide

# Watch logs
enclii logs <service> -f --env <env>

# Re-check service health
enclii ps --env <env>
```

## Recovery from failed rollback

If an instant rollback errors:

1. The API writes a `deploy.rollback_failed` event with the error detail.
2. The K8s Service is left in its prior state (update is transactional
   from the API's perspective — it either flips and annotates, or errors
   out before the annotation).
3. Check ReplicaSet existence:
   ```bash
   kubectl get rs -n <namespace> -l enclii.dev/service=<service>
   ```
   If the target RS is gone (`revisionHistoryLimit` purged it), fall back
   to the manifest-commit strategy.
4. For ArgoCD drift concerns, force a resync:
   ```bash
   kubectl patch application <app> -n argocd --type merge \
     -p '{"operation":{"sync":{}}}'
   ```

## Telemetry

Prometheus metric `enclii_deployments_total{status="rollback_instant"}`
tracks adoption. Duration histograms available as
`enclii_deployment_duration_seconds{status="rollback_instant"}`.

## Related

- Incident response: [INCIDENT_RESPONSE.md](./INCIDENT_RESPONSE.md)
- Disaster recovery: [DISASTER_RECOVERY.md](./DISASTER_RECOVERY.md)
- Remediation plan: `internal-devops/roadmaps/2026-04-enclii-remediation-plan.md`
