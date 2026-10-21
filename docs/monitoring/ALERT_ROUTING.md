# Alert routing — who gets paged, and how

> **Boundary checkpoint (2026-10-18, platform close-out):** public-safe
> summary of the live alert path: severities, routes, receivers and the
> Courier contract. No recipients, credentials or node identity. Policy:
> [`docs/PUBLIC_REPO_BOUNDARY.md`](../PUBLIC_REPO_BOUNDARY.md).

**Source of truth:** the config itself,
[`infra/k8s/production/monitoring/alertmanager.yaml`](../../infra/k8s/production/monitoring/alertmanager.yaml)
(routes and receivers, with the reasoning for every timing) and the rules in
[`prometheus.yaml`](../../infra/k8s/production/monitoring/prometheus.yaml).
This page is the map; if the two disagree, the config wins and this page is
stale.

State as of 2026-10-18 (times UTC), after the pager-noise changes of
2026-10-17/18 (#801, #802, #803, angelia#40, angelia#41).

## Severity model

| Severity | Reaches | Timing |
|---|---|---|
| `critical` (production target) | a phone through Angelia Courier (Telegram), **and** email | first notice after 10s; email re-sends every 4h while it fires (revenue probes: hourly per product) |
| `critical` (staging target) | email only, one digest (`staging-receiver`) | warning timings: 3h group interval, 24h repeat |
| `warning` | one email digest (`warning-receiver`); Courier receives it and skips it by severity | 3h group interval, 24h repeat |
| `info` / `none` | nobody (`informational-sink`); visible in the Alertmanager UI and API | — |

A critical alert is treated as staging when its `namespace`, `application`,
`name` or `dest_namespace` matches the staging pattern the Argo rules use and
it carries no `product` label (#801). Product-labelled alerts always page.

What pages critical: client-facing outages (client SLO rules, revenue probes,
geometry-factory probes, Argo health, workload availability) and the pager's
own health. What does not
(warnings since #801/#802): `MainCIFailingForTooLong` (main CI red on push
runs), `ArgoMainRolloutNotLanded`, `ArgoMainRolloutStuckForTooLong`, and
`MainCanaryFailing` (a scheduled canary workflow on `main` failing two runs in
a row for 30m; push CI and scheduled runs are read separately since #802, see
[`infra/deploy-pipeline-monitor/README.md`](../../infra/deploy-pipeline-monitor/README.md)).

## Geometry-factory probes

The `blackbox-geometry` scrape job probes the 13 public hosts of Yantra4D,
Fashion Cabinet, Pliego, Cotiza and Blueprint.Tube every 60s on their health
paths, through Cloudflare and the tunnel (targets in
[`prometheus.yaml`](../../infra/k8s/production/monitoring/prometheus.yaml),
modules in
[`blackbox-exporter.yaml`](../../infra/k8s/production/monitoring/blackbox-exporter.yaml)).
Three critical rules read it, all through the generic critical route
(Courier and email; email every 4h):

- `GeometryHostDown`: a host's probe has failed for 3 minutes;
- `GeometryTLSCertExpiring`: the certificate a host serves expires within 14
  days, for 1h;
- `GeometryProbesNotRunning`: the job has not been scraped for 10 minutes.

Each target's `product` label is Courier's page identity, so each failing
product pages once per episode.

## The pager's own failures

- `CourierReceiverDown` (Courier cannot be reached) goes to **email only**, so
  an alert about a dead pager is not sent through it.
- `AlertmanagerEmailDeliveryFailing` goes to **Courier only**, so an alert
  about dead email is not sent by email.
- `Watchdog` always fires and goes to a sink with no integrations; it proves
  the pipeline, it pages nobody.

## Courier: one page per episode

Every message to a person on a third-party network goes through Angelia
Courier's `POST /v1/courier/alertmanager`, never through a direct Slack or
Telegram integration in Alertmanager. Since angelia#41 (live 2026-10-18 03:00)
Courier keys each alert

```
alarm:<alertname>[:<identity>]:<hour>[:<phase>]
```

- `<hour>` is the UTC hour the alert's **episode** began (its `startsAt`), so
  a condition pages **once** when it starts, however often Alertmanager
  re-sends it;
- `<phase>` is empty for that page, `day<N>` for the **reminder every 24h**
  while it still fires, `resolved` for the **one resolved notice**
  (`COURIER_ALERTMANAGER_INCLUDE_RESOLVED`, default true), and `reopened` when
  it fires again inside the hour of its resolved notice; a target that flaps
  within one hour does not page again;
- `<identity>` (angelia#40) is the `product` label, else
  `<namespace>/<object>` (the first of name, application, repo, deployment,
  statefulset, daemonset, cronjob, app), else the namespace, else `job`, so
  a second target failing under the same alertname pages on its own.

Courier pages only `critical` (`COURIER_ALERTMANAGER_SEVERITIES`, default
`critical`). Every Courier webhook carries `max_alerts` so a batch stays under
Courier's 1 MiB body limit; `scripts/check-alertmanager-config.py` fails a
webhook without it.

## Page budget

Courier counts what it sends (`courier_alarm_pages_total`, angelia#41).
Two warnings read it (#803):

- `CourierPageBudgetExceeded`: more than 5 critical pages sent in 24h
  (opened, reminder and reopened phases; resolved notices are not pages);
- `CourierPageCounterMissing`: the counter has been absent for 1h, so the
  budget is not measured.

Both are warnings on purpose: a page about too many pages must not be one
more page.

## On the Angelia side

- [ADR-0010 — Courier, the outbound comms substrate](https://github.com/madfam-org/angelia/blob/main/docs/adr/0010-courier-outbound-comms-substrate.md)
- [Courier provisioning runbook](https://github.com/madfam-org/angelia/blob/main/docs/runbooks/courier-provisioning.md),
  section «Step 4-bis — the ALERTMANAGER receiver»: the receiver's
  credential, recipient, severity filter, dedup and resolved notices.
- Key logic: `apps/api/src/courier/alertmanager.ts` in angelia
  (`alertIdempotencyKey`, `alertIdentity`).

## Tests

`tests/scripts/test_check_alertmanager_config.py` (config loadability and the
route walk, including the staging routes), `test_alert_noise_rules.py` (the
#801/#802 demotions), `test_pager_path_drill.py` (the weekly drill and the
page-budget rules), `test_revenue_probe_rules.py`, `test_geometry_probe_rules.py`
and `test_client_slo_rules.py`.
Some cases need `promtool`; locally they skip and say so.
