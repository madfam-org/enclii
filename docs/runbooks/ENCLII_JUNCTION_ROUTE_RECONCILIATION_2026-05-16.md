# Enclii junction route reconciliation

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

If a junction exists but the tunnel route is missing:

1. Re-run `enclii junctions add` only if the junction row is absent.
2. If the row exists and the route is missing, use Enclii reconciliation by cycling the affected junction with `enclii junctions delete <id> --force` followed by `enclii junctions add <domain> --service-id <service-id> --project <project>`.
3. Confirm the tunnel route appears in `enclii providers cloudflare tunnels ...`.
4. Confirm the status monitor no longer lists the domain.

Direct Cloudflare tunnel mutation remains break-glass only.
