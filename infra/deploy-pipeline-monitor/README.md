# deploy-pipeline-monitor

GitHub-API CI freshness probe. Fires when a tracked repo's `main` branch is
stuck on a failing CI run, or when commits are accumulating on `main`
without a successful build.

## Why this exists

On 2026-05-04 selva-office's CI failed on `main` at 02:29 UTC. No image was
rebuilt → ArgoCD had nothing new to deploy → live pods stayed 8h-old.
Nothing alerted. A human noticed at 05:30 — three hours of silent drift.

This monitor turns that 3-hour window into a **30-minute** alert.

## How it closes the gap

A Kubernetes CronJob (`*/5 * * * *`) in the `monitoring` namespace runs
this script. For each `(repo, branch)` in
`/etc/deploy-pipeline-monitor/repos.json` it:

1. Queries `GET /repos/{repo}/branches/{branch}` for HEAD commit + push time.
2. Queries `GET /repos/{repo}/actions/runs?branch={branch}&status=completed&per_page=5`
   for the most recent terminal CI runs.
3. Computes freshness, status, failure-streak, and (if last run failed)
   `blocked_seconds = now - failing_commit_pushed_at`.
4. Pushes the gauge bundle to `pushgateway.monitoring.svc.cluster.local:9091`.

Per-repo failures are isolated — one repo's API hiccup never blocks the
other 13. They surface as `repo_main_probe_ok = 0` and trigger their own
warning alert.

## Metrics

Pushed under job `deploy_pipeline_monitor`:

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `repo_main_latest_commit_age_seconds` | gauge | repo, branch | Seconds since HEAD of `branch` was pushed |
| `repo_main_latest_ci_age_seconds` | gauge | repo, branch | Seconds since last completed CI run on `branch` finished |
| `repo_main_ci_status` | gauge | repo, branch, status | One-hot: `1` for current status, `0` for the other alerting statuses |
| `repo_main_ci_failure_streak_count` | gauge | repo, branch | Consecutive non-success terminal runs from newest backwards |
| `repo_main_blocked_seconds` | gauge | repo, branch | If latest run failed: seconds since the failing commit was pushed. `0` on success. |
| `repo_main_probe_ok` | gauge | repo, branch | `1` if this run successfully fetched repo state, `0` if the per-repo probe threw |
| `deploy_pipeline_monitor_last_run_timestamp_seconds` | gauge | — | Unix timestamp of last completed CronJob run; staleness detector |

## Alerts

Five PrometheusRules ship in
`infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml`:

| Alert | Severity | Threshold | Closes |
| --- | --- | --- | --- |
| `MainCIFailingForTooLong` | critical | `repo_main_blocked_seconds > 1800` | The 2026-05-04 selva-office case directly |
| `MainCommitsAccumulatingNotDeployed` | warning | `commit_age - ci_age > 1800` for 5m | Commits landing faster than CI can build |
| `MainCIConsecutiveFailures` | warning | `failure_streak >= 3` for 15m | Real bug vs. flake |
| `DeployPipelineMonitorNotRunning` | warning | no push in >15m | Monitor itself is broken |
| `DeployPipelineMonitorRepoProbeFailing` | warning | `probe_ok == 0` for 30m | PAT scope wrong / repo renamed |

## Adding a repo

Edit the `deploy-pipeline-monitor-repos` ConfigMap in
`infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml`:

```json
{"repo": "madfam-org/<new-repo>"}
```

`branch` defaults to `main`; pass it explicitly if the repo's deploy branch
differs.

GitHub API budget at 14 repos × 2 calls × 12 runs/hr = **336 req/hr**, well
under the 5000/hr authenticated limit. Adding 30 more repos still leaves
~80% headroom.

## GitHub auth

`GITHUB_TOKEN` is loaded from the `deploy-pipeline-monitor-creds` Secret,
synced from Vault under `secret/monitoring/deploy-pipeline-monitor` via
ExternalSecret. Use a fine-scoped PAT (or PAT-app installation token) with
**`repo: read` only**. Rotation: standard 90d via the secret-rotation
runbook.

## Sibling probes

This monitor watches the **outside world** (GitHub Actions). It complements:

- `infra/cloudflared-probe/` — in-cluster network reachability (PR #231).
- `infra/synthetic-flow-probe/` — user-flow E2E (in flight).

Together: full-stack observability of the deploy + serve path.

## Local development

```bash
cd infra/deploy-pipeline-monitor
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt pytest
.venv/bin/python -m pytest tests/ -v
```

## Operational scope

**Will:** detect when a tracked repo's `main` branch CI is failing or
stalled and the live deploy is therefore drifting from `main`.

**Will not:**
- Re-run failing workflows. The alert routes to humans / on-call.
- Verify that ArgoCD actually deployed the latest successful image — that
  is in scope for **Phase 2** (correlate `argocd_app_info{revision}` with
  HEAD on `main`).
- Replace per-repo CI status checks. Repo authors should still gate merges
  on green CI; this monitor catches **post-merge** silent failure.
