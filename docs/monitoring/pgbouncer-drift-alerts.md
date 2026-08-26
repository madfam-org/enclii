# PgBouncer userlist drift — alert rule snippet

Availability remediation program, Tier 0.6 (`internal-devops/roadmaps/2026-08-26-availability-remediation-9999.md`).

## Status: INTEGRATED (2026-08-26)

The three rules below are live in the `enclii-platform` group of
`infra/k8s/production/monitoring/prometheus.yaml` (added after the Tier-0.3
branch, #440, landed — it owned that file while this checker was built). The
YAML in this doc is retained as rationale + reference; **the config file is
the source of truth** if the two ever diverge.

The live Alertmanager config (`infra/k8s/production/monitoring/alertmanager.yaml`)
routes `critical-receiver` and `warning-receiver` to both `telegram_configs`
and `slack_configs`; the Telegram side stays inert until Tier 0.1's arming
steps (Vault token + chat_id) complete.

## What this closes

`internal/provisioning/userlist_reconcile.go` (enclii#436) shipped a
detect-only method — `PgBouncerUpdater.ReconcileUserlist` — whose own doc
comment says:

> This method is the detector the outage lacked; wire it into a periodic
> check that pages when HasMissing() is true.

Nothing called it on a schedule before this checker (`internal/reconciler
/pgbouncer_drift_checker.go`, wired in `cmd/api/main.go` behind
`ENCLII_PGBOUNCER_DRIFT_CHECK_ENABLED`, default true). The checker runs the
detector every 5 minutes and exports:

| Metric | Type | Meaning |
|---|---|---|
| `enclii_pgbouncer_userlist_missing_users` | gauge | Count of Postgres login roles absent from the pgbouncer userlist right now. Zero = in sync. |
| `enclii_pgbouncer_userlist_check_errors_total` | counter | Cumulative check runs that errored (DB/k8s access failure, misconfiguration, or a recovered panic) instead of completing a comparison. |
| `enclii_pgbouncer_userlist_last_check_timestamp_seconds` | gauge | Unix timestamp of the last **successful** check. Only advances on success — an erroring or crashed checker leaves this stale. |

The 2026-08-24 outage (a hand-applied userlist Secret silently dropped four
login roles; pgbouncer rejected auth for `fortuna`/`bloom-scroll`/`ceq` while
the same credentials worked against Postgres directly, hard-down for days
behind a silent 502) is the incident class rule (a) below turns into a page
instead of nothing.

## The three rules

```yaml
# Append to the `enclii-platform` group's `rules:` list in
# infra/k8s/production/monitoring/prometheus.yaml, alongside the existing
# PostgresDown / SwitchyardAPIDown rules. Indentation shown matches that
# file's existing style (rules nested under `groups: - name: enclii-platform`).

          # PgBouncer userlist drift (availability remediation Tier 0.6, enclii#436)
          - alert: PgbouncerUserlistDrift
            expr: enclii_pgbouncer_userlist_missing_users > 0
            for: 10m
            labels:
              severity: critical
              service: pgbouncer
            annotations:
              summary: "pgbouncer userlist drift — pooler auth diverged from DB truth (the 2026-08-24 outage class)"
              description: "{{ $value }} Postgres login role(s) exist that pgbouncer's userlist does not have a line for. Every pooled connection as those roles is failing auth right now — this is the exact drift class that took fortuna/bloom-scroll/ceq hard-down for days on 2026-08-24. Check GET /v1/admin/provision/pgbouncer/reconcile on switchyard-api for the current missing-role list; repair requires the role's password from the consuming service's own secret (see internal-devops/scripts/restore-pgbouncer-users.py) — this alert does NOT self-heal."

          - alert: PgbouncerUserlistCheckErrors
            expr: increase(enclii_pgbouncer_userlist_check_errors_total[15m]) > 0
            for: 5m
            labels:
              severity: warning
              service: pgbouncer
            annotations:
              summary: "pgbouncer userlist drift checker is erroring"
              description: "The periodic userlist drift checker (switchyard-api, ENCLII_PGBOUNCER_DRIFT_CHECK_ENABLED) failed {{ $value }} time(s) in the last 15 minutes instead of completing a comparison. Likely causes: Postgres admin connection down, k8s API access lost, or ENCLII_POSTGRES_ADMIN_URL/k8s client not configured. Missing_users may be stale while this fires."

          - alert: PgbouncerUserlistCheckerDead
            expr: time() - enclii_pgbouncer_userlist_last_check_timestamp_seconds > 900
            for: 5m
            labels:
              severity: warning
              service: pgbouncer
            annotations:
              summary: "pgbouncer userlist drift detector itself is dead"
              description: "No successful userlist drift check has completed in over 15 minutes (checker runs every 5m). Per the availability audit's green-lies doctrine, treat this as UNKNOWN, not healthy — PgbouncerUserlistDrift staying quiet right now proves nothing. Check whether the switchyard-api process is up and whether the checker goroutine panicked/exited; see internal/reconciler/pgbouncer_drift_checker.go."
```

### Why these three, together

Rule (a) alone can lie: if the checker process dies, `missing_users` freezes
at its last value (possibly zero) and rule (a) never fires even though drift
could be actively happening and unobserved. Rule (c) is the dead-man switch
for the detector itself — the same lesson the 2026-08-24 outage taught about
the userlist detector applies recursively to whatever watches it. Rule (b)
catches the middle case: the checker is alive and ticking but every run is
failing (e.g. a Postgres connection issue), so `missing_users` is stale
without the timestamp gauge having gone stale yet (an error still counts as
the loop being alive, just not successfully completing).

## Verifying the wiring before relying on it

1. `curl <switchyard-api>/metrics | grep enclii_pgbouncer_userlist_` — all
   three series should be present once the service has been up for one
   check interval (5 min; the checker also runs an immediate pass on
   startup, so this should appear within seconds of boot).
2. Synthetic test per the tracker's acceptance criterion (0.6): temporarily
   remove a non-critical login role's line from the pgbouncer userlist
   Secret in a non-prod environment (or point `ENCLII_POSTGRES_ADMIN_URL` at
   a scratch Postgres with an extra login role the userlist doesn't have)
   and confirm `enclii_pgbouncer_userlist_missing_users` goes nonzero within
   one check interval, then confirm rule (a) fires end-to-end to a human
   after the `for: 10m` window once integrated.
