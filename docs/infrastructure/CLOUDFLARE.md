# Cloudflare Integration

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


**Last Updated:** August 7, 2026
**Status:** Tunnel + zone/CNAME provisioning operational (single unified tunnel, 28+ domains, 2 replicas, HTTPS enforced on all zones). Cloudflare for SaaS custom hostnames implemented in code; **not yet exercised against a live client domain** and inert until `ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_ZONE_ID` / `ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_HOSTNAME` are set.

> [!WARNING]
> Earlier revisions of this document described Cloudflare for SaaS as shipped
> while no code called `/custom_hostnames`. The API client and provisioning
> path now exist; what remains is operator configuration (fallback origin) and
> a first real client domain. Do not read "implemented" as "verified in
> production".

---

## Overview

Enclii uses Cloudflare for zero-trust ingress via Cloudflare Tunnel, DNS management, and multi-tenant SSL via Cloudflare for SaaS custom hostnames. This provides enterprise-grade security without exposing cluster nodes to the public internet.

Two domain provisioning mechanisms exist, chosen per domain:

| | Zone + CNAME | Cloudflare for SaaS custom hostname |
|---|---|---|
| Applies to | Domains whose nameservers are delegated to our Cloudflare account | Client-owned domains that keep their own registrar/nameservers |
| Certificate | Cloudflare Universal SSL (apex + one subdomain level) | Per-hostname DV certificate (any depth) |
| We create the DNS record | Yes | No — the client does |
| Ready when | The CNAME exists | Cloudflare reports hostname + certificate active |

## Architecture

```
Internet
    │
    ▼
┌─────────────────────────────────────────┐
│       Cloudflare Edge Network            │
│  ┌─────────────────────────────────────┐ │
│  │ • TLS Termination                   │ │
│  │ • DDoS Protection                   │ │
│  │ • WAF Rules                         │ │
│  │ • Geographic Load Balancing         │ │
│  └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
                    │
            Encrypted Tunnel
                    │
                    ▼
┌─────────────────────────────────────────┐
│         cloudflared pods (2 replicas)    │
│         (cloudflare-tunnel namespace)    │
└─────────────────────────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│     Kubernetes Services (ClusterIP)      │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  │
│  │api:80   │  │app:80   │  │docs:80  │  │
│  └─────────┘  └─────────┘  └─────────┘  │
└─────────────────────────────────────────┘
```

## Components

### 1. Cloudflare Tunnel

Zero-trust ingress that replaces traditional load balancers.

**Benefits:**
- No public IPs needed on nodes
- No firewall port configuration
- Built-in DDoS protection
- Automatic failover

**Configuration:** `infra/k8s/production/cloudflared-unified.yaml`

### 2. Tunnel Route Automation

Routes are managed via Cloudflare API, not ConfigMap.

**Source Code:**
- Client: `apps/switchyard-api/internal/cloudflare/`
- Service: `apps/switchyard-api/internal/services/tunnel_routes_cloudflare.go`
- Types: `apps/switchyard-api/internal/cloudflare/types.go`

### 3. Cloudflare for SaaS (client-owned domains)

Cloudflare custom hostnames let a client keep their own registrar and
nameservers and still be served by us. This is the provisioning path for any
domain whose apex is **not** a zone in our Cloudflare account.

**Source Code:**
- Client: `apps/switchyard-api/internal/cloudflare/custom_hostnames.go`
- Provisioning: `apps/switchyard-api/internal/api/domain_provisioner_custom_hostname.go`
- Mechanism dispatch: `apps/switchyard-api/internal/api/domain_provisioner.go`
  (`ensureDomainRouting`)

**Plan limits:** first 100 custom hostnames free, then $0.10/hostname/month.

#### What is implemented

- `POST/GET/LIST/DELETE /zones/{zone}/custom_hostnames` against the
  fallback-origin zone, with `ssl: {type: "dv", method: "txt",
  settings: {min_tls_version: "1.2"}}`.
- Idempotent `EnsureCustomHostname` (safe to re-run on every deploy).
- Per-domain mechanism selection during provisioning: a domain whose apex is
  already a zone we control keeps the zone+CNAME path; anything else becomes a
  custom hostname. `external: true` / `external: false` on an `enclii.yaml`
  domain forces the choice.
- The Cloudflare-reported hostname status, certificate status and the DNS
  records the client still owes are persisted on the `custom_domains` row
  (migration `033`) and returned on the domain read path and in the junction
  create response.
- `POST /v1/services/:id/domains/:domain_id/verify` re-reads the real state
  from Cloudflare for custom-hostname domains. A domain is marked verified
  **only** when Cloudflare reports the hostname active *and* its certificate
  active — never because an API call returned 200.

#### What still requires the client to act

Provisioning cannot finish without the domain owner, because we do not control
their DNS. After the domain is declared, the API reports the exact records:

| Purpose | Type | Name | Value |
|---------|------|------|-------|
| routing | CNAME | `<their hostname>` | the configured fallback-origin hostname |
| ownership | TXT | `_cf-custom-hostname.<their hostname>` | value returned by Cloudflare |
| ssl validation | TXT | `_acme-challenge.<their hostname>` | value returned by Cloudflare |

Until those exist the domain stays `status = pending` with a populated
`pending_dns_records`, which reads as "waiting on the client", not "broken".

#### Configuration

Both variables are required to enable the path. When either is empty the
custom-hostname path is disabled and domain provisioning behaves exactly as it
did before (zone + CNAME only).

| Variable | Meaning |
|----------|---------|
| `ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_ZONE_ID` | Zone id of a zone we own that has a Cloudflare for SaaS fallback origin configured |
| `ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_HOSTNAME` | Hostname clients CNAME to (e.g. `proxy.enclii.dev`); must resolve inside that zone |

#### enclii.yaml

```yaml
spec:
  domains:
    - name: api.madfam.io        # absent → auto-detect (zone exists → zone path)
      environment: production
    - name: cto.creatumundo.mx   # client-owned: custom hostname
      environment: production
      external: true
    - name: legacy.example.com   # pin the zone+CNAME path explicitly
      environment: production
      external: false
```

> [!NOTE]
> Cloudflare Universal SSL on the zone path covers the apex and **one**
> subdomain level. A nested host (`a.b.example.com`) is rejected at
> declaration time unless it is declared `external: true`, because Cloudflare
> for SaaS issues a certificate for the exact hostname and can serve it.

## Credentials

Stored in Kubernetes secret: `enclii-cloudflare-credentials`

| Key | Description |
|-----|-------------|
| `api-token` | Cloudflare API token with Zone/Tunnel permissions |
| `account-id` | Cloudflare account identifier |
| `zone-id` | Zone identifier for enclii.dev |
| `tunnel-id` | Tunnel identifier (required for auto-provisioning) |
| `fallback-origin-zone-id` | Zone id of the Cloudflare for SaaS fallback-origin zone (optional; empty disables custom hostnames) |
| `fallback-origin-hostname` | Hostname clients CNAME to, e.g. `proxy.enclii.dev` (optional; empty disables custom hostnames) |

### Environment Variables

```yaml
env:
  - name: ENCLII_CLOUDFLARE_API_TOKEN
    valueFrom:
      secretKeyRef:
        name: enclii-cloudflare-credentials
        key: api-token
  - name: ENCLII_CLOUDFLARE_ACCOUNT_ID
    valueFrom:
      secretKeyRef:
        name: enclii-cloudflare-credentials
        key: account-id
  - name: ENCLII_CLOUDFLARE_ZONE_ID
    valueFrom:
      secretKeyRef:
        name: enclii-cloudflare-credentials
        key: zone-id
  - name: ENCLII_CLOUDFLARE_TUNNEL_ID
    valueFrom:
      secretKeyRef:
        name: enclii-cloudflare-credentials
        key: tunnel-id
  # Optional — both required together to enable Cloudflare for SaaS.
  - name: ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_ZONE_ID
    valueFrom:
      secretKeyRef:
        name: enclii-cloudflare-credentials
        key: fallback-origin-zone-id
        optional: true
  - name: ENCLII_CLOUDFLARE_FALLBACK_ORIGIN_HOSTNAME
    valueFrom:
      secretKeyRef:
        name: enclii-cloudflare-credentials
        key: fallback-origin-hostname
        optional: true
```

## Route Automation

### Self-Service Domain Auto-Provisioning

Domains declared in `enclii.yaml` are automatically provisioned on each push to main. No manual infrastructure edits required.

**Flow:**
1. GitHub push webhook received by Switchyard API
2. `enclii.yaml` fetched and parsed from the repo (`enclii_yaml.go`)
3. For each declared domain (`domain_provisioner.go`):
   - Create `CustomDomain` record in database (if not exists)
   - Add Cloudflare tunnel route via `TunnelRoutesManager.AddRoute()`
   - Provision edge routing via `ensureDomainRouting()`, which picks the
     mechanism per domain:
     - zone we control → `Client.EnsureDNSRecord()` (proxied CNAME)
     - client-owned domain → `Client.EnsureCustomHostname()` on the
       fallback-origin zone, then store the records the client must add
4. DNS records point to `tunnel.enclii.dev` (the tunnel endpoint)

**Multi-Zone Support:** `FindZoneForDomain()` uses longest-suffix matching — `api.qubic.quest` matches zone `qubic.quest` rather than `quest`.

**Auto Zone Creation:** If the domain's zone doesn't exist in Cloudflare (e.g., onboarding `tezca.mx` when only `madfam.io` is configured), the provisioner automatically creates the zone via `EnsureZoneForDomain()`. This requires the API token to have account-level Zone:Edit permissions (not zone-scoped). New zones start in `pending` status until nameserver delegation is verified by Cloudflare.

**Cleanup:** When a service is deleted, `cleanupDomainsForService()` removes tunnel routes and DNS records for all associated domains.

**Source Code:**
- Parser: `apps/switchyard-api/internal/api/enclii_yaml.go`
- Provisioner: `apps/switchyard-api/internal/api/domain_provisioner.go`
- DNS operations: `apps/switchyard-api/internal/cloudflare/dns.go`

### Manual Route Addition (CLI)

When `enclii domains add` is called:

1. **Domain record created** in database
2. **TunnelRoutesServiceCloudflare.AddRoute()** called
3. **Cloudflare API** updates tunnel configuration
4. **No pod restart needed** (API-based, not ConfigMap)

> For self-service provisioning, declare domains in your `enclii.yaml` instead. See the [Service Spec Reference](../reference/service-spec.md#domains-custom-domain-auto-provisioning).

### API Flow

```go
// apps/switchyard-api/internal/services/tunnel_routes_cloudflare.go
func (s *TunnelRoutesServiceCloudflare) AddRoute(ctx context.Context, spec *RouteSpec) error {
    // 1. Get current configuration from Cloudflare API
    config, err := s.cfClient.GetTunnelConfiguration(ctx, s.tunnelID)

    // 2. Check if route exists, update or insert
    // 3. Call Cloudflare API to update configuration
    err = s.cfClient.UpdateTunnelConfiguration(ctx, s.tunnelID, config)

    // No restart needed - changes are immediate
}
```

### Route Structure

```yaml
# Cloudflare Tunnel ingress configuration
ingress:
  - hostname: api.enclii.dev
    service: http://switchyard-api.enclii.svc.cluster.local:80
  - hostname: app.enclii.dev
    service: http://switchyard-ui.enclii.svc.cluster.local:80
  - hostname: docs.enclii.dev
    service: http://docs-site.enclii.svc.cluster.local:80
  - service: http_status:404  # Catch-all (must be last)
```

## Tunnel Configuration

### Production Tunnel (Consolidated Jan 2026)

A single unified tunnel handles all traffic. Legacy systemd tunnels and dual K8s deployments were consolidated during the Jan 25-26 ecosystem audit.

| Tunnel | ID | Purpose | Status |
|--------|-----|---------|--------|
| enclii-production | (token-based auth) | All services (28 domains) | ✅ Active |

**History**: Previously had 3 tunnel instances (2 systemd + 1 K8s). systemd tunnels disabled Jan 17. Legacy K8s deployment (v2024.12.0) deleted Jan 25. Single unified deployment (v2025.11.1, 2 replicas) now handles everything.

**Important**: The cloudflared deployment requires `privileged: false` in its securityContext to pass Kyverno's `disallow-privileged-containers` policy.

### Port Mapping

**Critical:** Cloudflare routes to K8s Service port (80), NOT container port.

```
Cloudflare Route → K8s Service:80 → Container:4xxx
                    (ClusterIP)     (targetPort)
```

| Service | Container Port | Service Port | Route Target |
|---------|---------------|--------------|--------------|
| switchyard-api | 4200 | 80 | http://svc:80 |
| switchyard-ui | 4201 | 80 | http://svc:80 |
| docs-site | 4203 | 80 | http://svc:80 |

## Operations

### Check Tunnel Status

```bash
# Check cloudflared pods
kubectl get pods -n cloudflare-tunnel

# View tunnel connections
kubectl logs -n cloudflare-tunnel -l app=cloudflared -f

# Check tunnel health via API
curl -s https://api.cloudflare.com/client/v4/accounts/{account_id}/cfd_tunnel/{tunnel_id} \
  -H "Authorization: Bearer $CF_TOKEN" | jq '.result.status'
```

### List Routes

```bash
# Via Cloudflare API
curl -s https://api.cloudflare.com/client/v4/accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations \
  -H "Authorization: Bearer $CF_TOKEN" | jq '.result.config.ingress'
```

### Add Route Manually

> **Preferred:** Declare domains in `enclii.yaml` for automatic provisioning on push. Manual route addition is still supported for ad-hoc needs.

```bash
# Via CLI
enclii domains add myapp.enclii.dev --service myapp --namespace default --port 80

# Via API directly
curl -X PUT "https://api.cloudflare.com/client/v4/accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations" \
  -H "Authorization: Bearer $CF_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{...updated config...}'
```

### Remove Route

```bash
# Via CLI
enclii domains remove myapp.enclii.dev
```

## Troubleshooting

### Tunnel Not Connected

```bash
# Check pod status
kubectl get pods -n cloudflare-tunnel

# Check pod logs for errors
kubectl logs -n cloudflare-tunnel -l app=cloudflared --tail=50

# Verify credentials
kubectl get secret enclii-cloudflare-credentials -n enclii -o yaml
```

### Route Not Working

```bash
# Verify route exists in Cloudflare
curl -s "https://api.cloudflare.com/client/v4/accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations" \
  -H "Authorization: Bearer $CF_TOKEN" | jq '.result.config.ingress[] | select(.hostname=="<hostname>")'

# Check if service exists
kubectl get svc -n enclii

# Test service connectivity from cloudflared pod
kubectl exec -n cloudflare-tunnel <cloudflared-pod> -- \
  curl -s http://switchyard-api.enclii.svc.cluster.local:80/health
```

### SSL Certificate Issues

For Cloudflare for SaaS custom domains, ask Enclii first — it stores the
Cloudflare-reported state and the outstanding client action:

```bash
# Re-reads the live state from Cloudflare and updates the record.
enclii api POST "/v1/services/<service-id>/domains/<domain-id>/verify"
# 200 => Cloudflare reports hostname + certificate active
# 400 => not yet: response carries `pending_dns_records` for the client
# 502 => we could not reach Cloudflare (this is our problem, not the client's)
```

Break-glass, when the API is unavailable (raw provider access — see the banner
at the top of this file):

```bash
curl -s "https://api.cloudflare.com/client/v4/zones/{fallback_origin_zone_id}/custom_hostnames?hostname=<hostname>" \
  -H "Authorization: Bearer $CF_TOKEN" | jq '.result[0] | {status, ssl: .ssl.status, ownership_verification}'
```

| Reported state | Meaning | Who acts |
|---|---|---|
| `status: pending`, `ssl: pending_validation` | Ownership TXT and/or DCV TXT missing | Client |
| `status: pending`, `ssl: active` | Certificate issued, CNAME not cut over yet | Client |
| `status: active`, `ssl: pending_issuance` | Cloudflare is issuing; wait | Nobody |
| `status: active`, `ssl: active` | Serving | Nobody |
| `status: moved` | The client's CNAME stopped pointing at the fallback origin | Client |
| `status: blocked` | Cloudflare refused the hostname | Operator (contact Cloudflare) |

## DNS Configuration

### Zone Records

| Record | Type | Target | Proxy |
|--------|------|--------|-------|
| api.enclii.dev | CNAME | <tunnel-id>.cfargotunnel.com | Proxied |
| app.enclii.dev | CNAME | <tunnel-id>.cfargotunnel.com | Proxied |
| docs.enclii.dev | CNAME | <tunnel-id>.cfargotunnel.com | Proxied |

### Customer Custom Domains

For a client-owned domain, Enclii registers a Cloudflare custom hostname on the
fallback-origin zone and then hands the client the records to create on their
own nameservers:

```
customer-app.customer-domain.com                CNAME → proxy.enclii.dev
_cf-custom-hostname.customer-app.customer-domain.com  TXT → <ownership value>
_acme-challenge.customer-app.customer-domain.com      TXT → <dcv value>
```

The certificate is issued by Cloudflare once those records resolve. Enclii
reports the domain as active only after Cloudflare reports both the hostname
and its certificate active. See
[Cloudflare for SaaS (client-owned domains)](#3-cloudflare-for-saas-client-owned-domains).

## Security

### Zero-Trust Architecture

- No public IPs on cluster nodes
- All traffic through encrypted tunnel
- DDoS protection at Cloudflare edge
- WAF rules applied before reaching cluster

### API Token Permissions

The "Enclii Platform Token" (All accounts, All zones) has these scopes:

| Permission | Scope | Purpose |
|------------|-------|---------|
| Account: Cloudflare Tunnel: Edit | All accounts | Tunnel config management |
| Zone: Zone: Edit | All zones | Zone creation |
| Zone: DNS: Edit | All zones | DNS record management |
| Zone: Zone Settings: Edit | All zones | `always_use_https`, `min_tls_version`, etc. |
| Zone: SSL and Certificates: Edit | All zones | SSL mode, certificate management, **custom hostnames** |

> **Note:** Must be **account-level** (not zone-scoped) to support auto zone creation.

> **Custom hostnames:** the `/zones/{zone}/custom_hostnames` endpoints are
> covered by **Zone: SSL and Certificates: Edit**, which the platform token
> already carries. That permission only has to hold on the fallback-origin
> zone; nothing extra is needed on the client's side, since the client's zone
> is not in our account at all. `Zone: Zone: Edit` is **not** sufficient on its
> own for custom hostnames.

### Zone Settings Management

Use the zone settings script to manage security-critical settings across all zones:

```bash
# List all zones
./scripts/cloudflare-zone-settings.sh list

# Check a setting across all zones
./scripts/cloudflare-zone-settings.sh get always_use_https

# Set a setting across all zones
./scripts/cloudflare-zone-settings.sh set always_use_https on
./scripts/cloudflare-zone-settings.sh set min_tls_version 1.2

# Audit security settings (color-coded output)
./scripts/cloudflare-zone-settings.sh audit
```

## Related Documentation

- [GitOps with ArgoCD](./GITOPS.md)
- [Storage with Longhorn](./STORAGE.md)
- Deployment Guide: `infra/DEPLOYMENT.md`
- [Production Deployment Roadmap](../production/PRODUCTION_DEPLOYMENT_ROADMAP.md)

## Verification

```bash
# Verify tunnel is healthy
kubectl get pods -n cloudflare-tunnel

# Expected:
NAME                           READY   STATUS    RESTARTS
cloudflared-xxxxxxxxxx-xxxxx   1/1     Running   0
cloudflared-xxxxxxxxxx-xxxxx   1/1     Running   0

# Verify API integration
curl https://api.enclii.dev/health

# Expected:
{"service":"switchyard-api","status":"healthy","version":"0.1.0"}

# Verify Cloudflare client in API logs
kubectl logs -n enclii -l app=switchyard-api --tail=20 | grep -i cloudflare

# Expected:
level=info msg="Cloudflare API client initialized"
```
