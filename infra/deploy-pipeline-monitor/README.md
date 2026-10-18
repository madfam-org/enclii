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
2. Queries `GET /repos/{repo}/actions/runs?branch={branch}&status=completed&event=push&per_page=5`
   for the most recent terminal **push** CI runs: the build of what was merged.
3. Queries `GET /repos/{repo}/actions/runs?branch={branch}&status=completed&event=schedule&per_page=30`
   for the repo's **scheduled** runs (production canaries), tracked per workflow.
4. Computes freshness, status, failure-streak, and (if last push run failed)
   `blocked_seconds = now - failing_commit_pushed_at`.
5. Pushes the gauge bundle to `pushgateway.monitoring.svc.cluster.local:9091`.

**Why push and schedule are separate (2026-10-17, infra-stability W1O-1).**
Step 2 used to read the latest completed run of any event, so a red scheduled
canary (forgesight `Production Smoke`, rondelio `Production Canaries`), a
cancelled merge-bot `workflow_run` or a Dependabot `dynamic` run read as "main
CI failing" while push CI was green. A failing canary is a production signal
with its own alert (`MainCanaryFailing`), not a build failure.

Per-repo failures are isolated — one repo's API hiccup never blocks the
others. They surface as `repo_main_probe_ok = 0` (push) or
`repo_main_scheduled_probe_ok = 0` (schedule) and trigger their own warning
alert. A failed schedule query never blanks the push signal.

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
| `repo_main_scheduled_failure_streak_count` | gauge | repo, branch, workflow | Consecutive failed runs of one scheduled workflow, newest first; `cancelled`/`skipped` passed over |
| `repo_main_scheduled_latest_failed` | gauge | repo, branch, workflow | `1` if the newest completed run of that scheduled workflow failed or timed out |
| `repo_main_scheduled_probe_ok` | gauge | repo, branch | `1` if this run fetched the repo's scheduled runs, `0` if that query threw |
| `deploy_pipeline_monitor_last_run_timestamp_seconds` | gauge | — | Unix timestamp of last completed CronJob run; staleness detector |

## Alerts

The rules Prometheus evaluates live in the `deploy-pipeline-freshness-rules.yml`
key of `infra/k8s/production/monitoring/prometheus.yaml` (the PrometheusRule in
`deploy-pipeline-monitor.yaml` is inert: there is no prometheus-operator for
the main Prometheus):

| Alert | Severity | Threshold | Closes |
| --- | --- | --- | --- |
| `MainCIFailingForTooLong` | critical | `repo_main_blocked_seconds > 1800` | The 2026-05-04 selva-office case directly |
| `MainCommitsAccumulatingNotDeployed` | warning | `commit_age - ci_age > 1800` for 5m | Commits landing faster than CI can build |
| `MainCIConsecutiveFailures` | warning | `failure_streak >= 3` for 15m | Real bug vs. flake |
| `DeployPipelineMonitorNotRunning` | warning | no push in >15m | Monitor itself is broken |
| `DeployPipelineMonitorRepoProbeFailing` | warning | `probe_ok == 0` for 30m | PAT scope wrong / repo renamed |
| `MainCanaryFailing` | warning | `scheduled_failure_streak >= 2` for 30m | A scheduled production canary failing twice running |
| `DeployPipelineMonitorScheduledProbeFailing` | warning | `scheduled_probe_ok == 0` for 30m | The schedule query itself failing |

`MainCIFailingForTooLong`'s severity is set in `prometheus.yaml`; read it there
rather than from this table if the two ever disagree.

## Adding a repo

Edit the `deploy-pipeline-monitor-repos` ConfigMap in
`infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml`:

```json
{"repo": "madfam-org/<new-repo>"}
```

`branch` defaults to `main`; pass it explicitly if the repo's deploy branch
differs.

GitHub API budget at 19 repos × 3 calls × 12 runs/hr = **684 req/hr**, under
the 5000/hr authenticated limit (about 14% of it).

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
