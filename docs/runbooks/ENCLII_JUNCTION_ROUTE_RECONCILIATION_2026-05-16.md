# Enclii junction route reconciliation

> **Boundary checkpoint (2026-10-01, platform on-call):** Public-safe runbook:
> reconcile semantics, guard behaviour and generic example commands only, with
> no tunnel ids, node identity or production output. Incident timelines and
> per-project repair records stay in `internal-devops`; see
> [`docs/PUBLIC_REPO_BOUNDARY.md`](../PUBLIC_REPO_BOUNDARY.md).

Date: 2026-05-16

## Context

Tulana exposed a control-plane drift case where Enclii junction rows existed for `tulana.madfam.io`, `tulana-app.madfam.io`, and `tulana-api.madfam.io`, but the Cloudflare tunnel route initially contained only the root and API hostnames. DNS was already correct; the missing app hostname was a tunnel-route reconciliation gap.

## Required behavior

- Operators must create public routing through Enclii, not direct Cloudflare or Kubernetes edits.
- `enclii junctions add` must create the junction row, ensure the production environment exists, ensure the DNS CNAME, and reconcile the Cloudflare tunnel route.
- Custom-domain provisioning must route to the workload namespace resolved from the service or project, not a synthetic `enclii-production` namespace.
- Reconciliation must be safe after concurrent junction creation because Cloudflare tunnel config updates are read-modify-write operations.

## Verification

Use Enclii readbacks first:

```bash
enclii junctions list --project tulana
enclii providers cloudflare tunnels tulana-app.madfam.io --project tulana --service tulana-web --json
enclii providers cloudflare dns-apply tulana-app.madfam.io --json
```

Then verify the public surface:

```bash
curl -k -sS -o /tmp/tulana-app.out -w 'app %{http_code}\n' https://tulana-app.madfam.io
curl -k -sS -o /tmp/tulana-api.out -w 'api %{http_code}\n' https://tulana-api.madfam.io/api/v1/health/
curl -fsS https://status.madfam.io/api/status
```

## Remediation path

If a junction exists but the tunnel route is missing or points at the wrong backend:

1. **Preferred:** reconcile tunnel ingress through Enclii without deleting the junction row. Run the dry run, read the plan, and stop there unless every row is what you expect:

```bash
enclii providers cloudflare tunnels-apply --project tulana
```

Single-host form (dry run):

```bash
enclii providers cloudflare tunnels-apply tulana-app.madfam.io --project tulana
```

When the plan is clean, its `Next` line is the apply command for exactly that plan, carrying `--expect-plan <fingerprint>`. The apply re-plans server-side and refuses, writing nothing, if the plan changed since the dry run. Never paste a dry run and its apply as one block: the apply is a separate decision taken after reading the plan.

The procedure, in order (full text in the [CLI reference](../cli/commands/providers.md#cloudflare-tunnels-apply-operator-procedure); needs CLI `v1.0.0-alpha.14` or later):

   1. Dry run.
   2. Review every row: its label, `reason` and `environment_source`.
   3. Apply with `--expect-plan` bound to the plan you reviewed.
   4. When a project's hostnames are served by more than one service or environment, rebind the junctions first (`enclii ops junctions rebind`, see [below](#correcting-a-junctions-binding)), then start again at step 1.

2. Re-run `enclii junctions add` only if the junction row is absent.
3. If the row exists and reconciliation still fails, cycle the junction with `enclii junctions delete <id> --force` followed by `enclii junctions add <domain> --service-id <service-id> --project <project>`.
4. Confirm the tunnel route appears in `enclii providers cloudflare tunnels ...`.
5. Confirm the status monitor no longer lists the domain.

Direct Cloudflare tunnel mutation remains break-glass only when `tunnels-apply` is unavailable or unconfigured.

## Repoint guard and environment-aware backends (2026-10-01)

A `tunnels-apply --project <p> --apply` once executed five UPDATEs it had
labelled drift: a project's production API and admin hostnames were moved onto
its web service, and its staging hostnames onto the production web service.
Every live route was correct; every junction was wrong (all bound to the web
service, none to an environment), and the planner derived every backend in the
production namespace.

What the planner does now:

- **Environment-aware backend.** Each junction's backend is
  `<service>.<namespace of the junction's environment>`. The environment comes
  from the junction itself (migration 041, `junctions.environment_id`), else
  from the hostname's domain record, else production. A non-production
  environment uses its own namespace (`enclii-<project>-<env>` by convention),
  never the service's production namespace. Every plan row reports
  `environment` and `environment_source` (`junction`, `domain-record`,
  `default`).
- **Repoint guard.** An UPDATE that changes the target service or namespace of
  a hostname whose live backend is serving (the Service exists, exposes the
  port, and selects a Ready pod; "could not tell" counts as serving) is a
  repoint, labelled `REPOINT (blocked)`. A route is also never moved between
  production and another environment's namespace by inference, even when its
  backend is down (`guard: cross-environment`). The summary leads with
  `REFUSED: ...`.
- **All-or-nothing apply.** While any row in scope is blocked, the apply writes
  nothing and answers HTTP 409.
- **Explicit override, per hostname.** `--allow-repoint <hostname>`
  (repeatable) permits one intended move. A desired namespace that belongs to a
  different environment than the junction's is a data error and is never
  overridable.
- **Unresolvable backends.** A row whose desired Service does not resolve is
  `BLOCKED`, for creates and updates alike.
- **Automated paths.** The push reconcile, the junction reconcile that runs on
  every junction create, `ops domains reconcile` and `domains add` refuse the
  same repoints (no override); the refusal is recorded on the domain record.

### Correcting a junction's binding

Fix the data, not the route. `enclii ops junctions rebind` rewrites a
junction's service and environment and nothing else; it is a dry run unless
`--apply --reason` is given, idempotent, and refuses unknown hostnames,
services and environments.

```bash
# 1. Preview the binding change (dry run). The row reports backend_after and
#    live_matches: true means the live route already serves that binding.
enclii ops junctions rebind api.example.com --project example \
  --to-service example-api --environment production

# 2. After applying the rebind, confirm the route plan for that host is SKIP.
enclii providers cloudflare tunnels-apply api.example.com --project example
```

A blocked `tunnels-apply` dry run lists, under `Next`, the rebind dry run that
would bind each blocked hostname to the backend serving it now.

### Recorded service namespace

The planners read a service's recorded namespace (`services.k8s_namespace`)
for production routes, before the environment's namespace and the project
slug. Services loaded by id did not carry that value until the follow-up to
the change above, so a service adopted into a namespace of its own was
planned in the project namespace.

- **tunnels-apply** plans production rows in the recorded namespace. A row
  whose namespace comes from the service record says so in its `reason`
  (`namespace <ns> is the service's recorded namespace (derived from the
  project: <slug>)`). The repoint guard, the all-or-nothing apply and
  `--expect-plan` apply as above.
- **The junction reconcile that runs on every junction create** never moves a
  route because of the recorded namespace. When the recorded namespace changes
  a hostname's backend and the live route does not already target it, the
  reconcile logs `REFUSED: the automatic junction reconcile does not move a
  route onto a service's recorded namespace`, lists the hostname under
  `refused` in its summary, and writes nothing for it (no DNS, no route).
  Move such a route with `tunnels-apply`: dry run, review every row, then
  apply with `--expect-plan`.
- **`domains add`** writes a new hostname's route through the same guarded
  path as every other writer: the incumbent rule is read, the backend is
  resolved (port from the live Service) before writing, a serving route is
  not repointed, and the write is canaried when the canary is enabled.
