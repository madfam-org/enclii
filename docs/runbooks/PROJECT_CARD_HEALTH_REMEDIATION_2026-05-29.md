# Project-card health remediation log

Date: 2026-05-29

Status: remediated incident, follow-up implementation required

## Direct surfaces

| Surface | URL |
| --- | --- |
| Projects UI | `https://app.enclii.dev/projects` |
| Project-card API | `https://api.enclii.dev/v1/projects/cards` |
| Enclii API health | `https://api.enclii.dev/health` |

## Incident summary

The `/projects` UI showed three failing projects even though the broader
`enclii observe health` aggregate reported all runtime services healthy.

Affected project cards:

| Project | Evidence source | Root cause |
| --- | --- | --- |
| `backup` | `data/postgres-backup` CronJob | Latest scheduled job failed before a later successful manual recovery |
| `platform-infra` | `data/postgres-backup` CronJob | Same shared backup evidence as `backup` |
| `tulana` | `tulana/tulana-submit-capture-targets` CronJob | Failed job logged missing `CRAWLER_API_URL`; current secrets later contained crawler configuration |

This was a project-card evidence issue, not a live service outage. The card
aggregate was correctly looking beyond pod health, but the UI did not make it
obvious that failed CronJob evidence was the only failing dimension.

## Recovery performed

Manual recovery jobs were created from the owning CronJobs:

```text
data/postgres-backup-manual-20260529-0344
tulana/tulana-submit-capture-targets-manual-20260529-0344
```

Both completed successfully.

Tulana capture recovery submitted three new crawler tasks and skipped existing
tasks that already had task metadata.

Final project-card verification showed:

```json
{
  "count": 59,
  "status_counts": [{"status": "healthy", "count": 59}],
  "failing": []
}
```

`enclii observe health --json` also remained all green:

```json
{
  "healthy_count": 105,
  "degraded_count": 0,
  "unhealthy_count": 0
}
```

Do not record secret values in this log. The remediation confirmed that secret
keys existed and matched where required, but the values themselves are not
operational documentation.

## Implementation requirements

### 1. Separate health dimensions

The card model should expose separate dimensions instead of collapsing all
evidence into one ambiguous red state:

| Dimension | Example |
| --- | --- |
| Serving health | Deployment ready, replicas healthy, public health check passes |
| Rollout state | Active rollout blocked, image pull error, readiness failure |
| Job evidence | Latest CronJob success/failure, age, failure reason |
| Alert evidence | Active alert, stale alert, silence/maintenance window |
| Evidence freshness | Timestamp and age for every dimension |

The aggregate can still produce `healthy`, `degraded`, or `failing`, but the API
and UI must show which dimension caused the result.

### 2. Add deterministic recovery semantics

For CronJobs:

1. Identify the CronJob owner by labels and name, not only namespace.
2. Consider the latest successful job for the same CronJob.
3. Clear a historical failure when a later success exists unless the CronJob is
   overdue, suspended unexpectedly, or has a still-active failing run.
4. Preserve the failed job in detail evidence even after the aggregate turns
   healthy.

This prevents stale failed jobs from keeping `backup`, `platform-infra`, or
`tulana` red after a verified recovery.

### 3. Improve card diagnostics

The `/v1/projects/cards` response should include job evidence similar to:

```json
{
  "job_evidence": [
    {
      "namespace": "tulana",
      "cronjob": "tulana-submit-capture-targets",
      "latest_job": "tulana-submit-capture-targets-29666230",
      "latest_job_status": "failed",
      "latest_successful_job": "tulana-submit-capture-targets-manual-20260529-0344",
      "latest_successful_at": "2026-05-29T03:45:00Z",
      "aggregate_effect": "cleared_by_later_success"
    }
  ]
}
```

The UI should link failing evidence to a detail drawer rather than leaving the
operator to inspect Kubernetes directly.

### 4. Retry and alerting policy

For critical platform CronJobs:

- set explicit `backoffLimit`;
- alert on repeated failure, missing latest success, and overdue schedule;
- distinguish warning-level failed job evidence from critical user-facing
  project failure;
- include runbook links in alert annotations;
- avoid shared namespace smear: a `data` namespace failure must attach only to
  projects that own or consume that CronJob.

### 5. Stale alert cleanup

The project-card remediation is separate from stale or unrelated observability
alerts. Follow-up work should audit alert generation and display for:

- alerts that continue firing after recovery;
- alert rules that do not include enough owning-project labels;
- UI alert summaries that conflate service runtime health with job history.

## Verification commands

Preferred Enclii-first checks:

```bash
enclii observe health --json
enclii observe alerts --json
```

Project-card API check:

```bash
curl -sS https://api.enclii.dev/v1/projects/cards \
  -H "Authorization: Bearer $ENCLII_TOKEN" \
  | jq '{generated_at, count, status_counts, failing: [.projects[] | select(.aggregate_status != "healthy") | .slug]}'
```

Break-glass cluster checks, only when the Enclii API does not expose enough
evidence:

```bash
kubectl -n data get cronjob postgres-backup
kubectl -n tulana get cronjob tulana-submit-capture-targets
kubectl -n data get jobs --sort-by=.metadata.creationTimestamp
kubectl -n tulana get jobs --sort-by=.metadata.creationTimestamp
```

## Definition of done

- `/projects` shows failures only for current actionable problems.
- Historical failed CronJobs remain visible as evidence but do not override a
  later verified success.
- Each failing card shows the namespace, resource, reason, timestamp, and
  recovery path.
- Tests cover shared namespace attribution and later-success recovery.
- Operators can diagnose card failures from Enclii API/UI without raw
  Kubernetes access in the normal path.
