# Switchyard API — production settings

Settings of the control-plane API (`apps/switchyard-api`) that change its
behaviour in production. A partial list, kept to the ones an operator has to
know about; the production values live in
[`infra/k8s/components/environment/production.env`](../../infra/k8s/components/environment/production.env)
and
[`infra/k8s/production/environment-patch.yaml`](../../infra/k8s/production/environment-patch.yaml).
Local development settings are in
[`apps/switchyard-api/README.md`](../../apps/switchyard-api/README.md#configuration).

State as of 2026-10-18.

| Variable | Default | Production | What it does |
|----------|---------|------------|--------------|
| `ENCLII_TENANT_SCOPE_ENFORCE` | on | default (enforced) since 2026-10-18 | ADR-003 tenant scope. `false` is a rollback lever only, and an open P0 while set ([runbook](../runbooks/TENANT_SCOPE_ENFORCEMENT_ROLLOUT.md)) |
| `ENCLII_PLATFORM_ADMIN_EMAILS` | falls back to `ENCLII_ADMIN_EMAILS` | set explicitly (stage 2, #798) | Allow-list for the cross-tenant platform rank, reconciled at pod start; an address grants nothing until its user has logged in once |
| `ENCLII_TRUSTED_PROXY_CIDRS` | unset (trust no proxy) | the cluster pod CIDR (set 2026-10-18) | Peers whose forwarded client-IP headers are believed, by the security middleware's limiter and filters (#783) and by gin's `ClientIP` for audit rows and logs ([SECURITY.md](../../SECURITY.md#client-address-behind-the-tunnel)) |
| `ENCLII_OPS_RESTART_NAMESPACES` | empty (refuse all) | set in `infra/k8s/base/switchyard-api.yaml` | Namespaces `enclii ops pods restart` may act in; `kube-*` is always refused ([ops](../cli/commands/ops.md#ops-pods-restart)) |

## Startup

The startup database ping retries up to 10 times (waits of 1, 2 and 4
seconds, then 8; 3 seconds per attempt) and exits only when every attempt has
failed (#781). The worst case stays inside the startup probe's window, so a
rollout that lands while PgBouncer is still settling no longer restarts the
pod, and a restart that does happen points at a real fault.
