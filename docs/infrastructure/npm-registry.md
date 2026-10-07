---
title: npm Registry Implementation
description: Deploy Verdaccio as a private npm registry on Enclii infrastructure
sidebar_position: 21
tags: [infrastructure, npm, verdaccio, registry]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# npm.madfam.io Implementation Plan

## Related Documentation

- **DNS Setup**: [DNS Configuration (Porkbun)](/infrastructure/dns-setup-porkbun)
- **Cloudflare**: [Cloudflare Integration](/infrastructure/CLOUDFLARE)
- **Onboarding**: [Onboarding Guide](/guides/ONBOARDING_GUIDE)

## Overview

This document outlines the complete implementation plan for deploying Verdaccio as an Enclii-managed service at `npm.madfam.io`.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         CLOUDFLARE                                  │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────────┐ │
│  │ DNS             │  │ Tunnel          │  │ R2 Storage          │ │
│  │ npm.madfam.io   │──│ (Zero LB cost)  │  │ (Package backups)   │ │
│  └────────┬────────┘  └────────┬────────┘  └─────────────────────┘ │
└───────────┼────────────────────┼────────────────────────────────────┘
            │                    │
            ▼                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    HETZNER BARE METAL (k3s)                         │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                    ENCLII WORKLOADS NAMESPACE                │   │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │   │
│  │  │  VERDACCIO  │  │             │  │   PERSISTENT        │  │   │
│  │  │  Pod 1      │  │             │  │   VOLUME            │  │   │
│  │  │  (Single)   │  │             │  │   (Longhorn)        │  │   │
│  │  └──────┬──────┘  └──────┬──────┘  │   50Gi              │  │   │
│  │         │                │         └─────────────────────┘  │   │
│  │         └────────┬───────┘                                  │   │
│  │                  ▼                                          │   │
│  │         ┌─────────────┐                                     │   │
│  │         │   JANUA     │  (OAuth for npm login)              │   │
│  │         │   SSO       │                                     │   │
│  │         └─────────────┘                                     │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## Implementation Phases

### Phase 1: DNS & Cloudflare Setup
**Timeline: Day 1**
**Owner: DevOps**

1. **Add DNS record in Porkbun**
   ```
   Type: CNAME
   Name: npm
   Target: <cloudflare-tunnel-id>.cfargotunnel.com
   TTL: Auto
   ```

2. **Configure Cloudflare Tunnel**
   ```yaml
   # cloudflared config
   tunnel: madfam-tunnel
   ingress:
     - hostname: npm.madfam.io
       service: http://verdaccio:4873
     - service: http_status:404
   ```

3. **Cloudflare Settings**
   - SSL/TLS: Full (strict)
   - Always Use HTTPS: On
   - Minimum TLS Version: 1.2
   - Cache: Bypass for authenticated requests

### Phase 2: Kubernetes Manifests
**Timeline: Day 1-2**
**Owner: DevOps**

Files to create in `infra/k8s/base/`:

1. **verdaccio-pvc.yaml** - Persistent storage
2. **verdaccio-config.yaml** - ConfigMap with config.yaml
3. **verdaccio-secret.yaml** - htpasswd and auth tokens
4. **verdaccio-deployment.yaml** - Pod spec
5. **verdaccio-service.yaml** - ClusterIP service
6. **verdaccio-ingress.yaml** - Cloudflare tunnel ingress

### Phase 3: Enclii Service Definition
**Timeline: Day 2**
**Owner: DevOps**

Create `enclii.yaml` service definition following Enclii patterns.

### Phase 4: Janua API-key authentication

Delivered by the `verdaccio-auth-janua` plugin, with htpasswd retained as a
fallback for CI service users. See
[Authentication](#authentication-janua-api-keys-htpasswd-fallback) below for
how to get a registry token — this supersedes the earlier
`verdaccio-auth-oauth2` sketch, which was never implemented.

### Phase 5: CI/CD Integration
**Timeline: Day 4-5**
**Owner: DevOps**

1. Add NPM_MADFAM_TOKEN to GitHub org secrets
2. Update all repo workflows for auto-publish
3. Create publish workflow template

### Phase 6: Migrate Existing Packages
**Timeline: Day 5-7**
**Owner: All teams**

1. Publish existing workspace packages
2. Update `.npmrc` files across repos
3. Test installations

---

## Detailed Implementation

### Kubernetes Manifests

#### verdaccio-pvc.yaml
```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: verdaccio-storage
  namespace: enclii-workloads
  labels:
    app: verdaccio
    service: npm-registry
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: longhorn
  resources:
    requests:
      storage: 50Gi
```

#### verdaccio-config.yaml
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: verdaccio-config
  namespace: enclii-workloads
data:
  config.yaml: |
    storage: /verdaccio/storage
    plugins: /verdaccio/plugins

    web:
      title: MADFAM Package Registry
      primary_color: "#6366f1"

    # Order matters: the Janua plugin claims only passwords with a Janua key
    # prefix (sk_live_, legacy jnk_) and falls through for everything else, so
    # htpasswd keeps working for CI users.
    auth:
      auth-janua:
        janua_url: https://auth.madfam.io
        cache_ttl_ms: 300000
        key_prefixes: [jnk_, sk_live_]
        timeout_ms: 5000
      htpasswd:
        file: /verdaccio/conf/htpasswd
        max_users: 100
        algorithm: bcrypt

    security:
      api:
        jwt:
          sign:
            expiresIn: 29d

    uplinks:
      npmjs:
        url: https://registry.npmjs.org/
        timeout: 30s
        cache: true

    packages:
      '@madfam/*':
        access: $authenticated
        publish: $authenticated
      '@janua/*':
        access: $all              # public read for SDK consumers
        publish: $authenticated
      '@dhanam/*':
        access: $authenticated
        publish: $authenticated
      '@cotiza/*':
        access: $authenticated
        publish: $authenticated
      '@fortuna/*':
        access: $authenticated
        publish: $authenticated
      '@avala/*':
        access: $authenticated
        publish: $authenticated
      '@forgesight/*':
        access: $authenticated
        publish: $authenticated
      '@coforma/*':
        access: $authenticated
        publish: $authenticated
      '@forj/*':
        access: $authenticated
        publish: $authenticated
      '@enclii/*':
        access: $all              # public read for SDK consumers
        publish: $authenticated
      '**':
        access: $all
        publish: $authenticated
        proxy: npmjs

    logs:
      type: stdout
      format: pretty
      level: info
```

#### verdaccio-deployment.yaml
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: verdaccio
  namespace: enclii-workloads
  labels:
    app: verdaccio
    service: npm-registry
spec:
  replicas: 1  # Single replica (RWO PVC)
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 1
      maxSurge: 1
  selector:
    matchLabels:
      app: verdaccio
  template:
    metadata:
      labels:
        app: verdaccio
        service: npm-registry
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        fsGroup: 10001
      containers:
        - name: verdaccio
          image: verdaccio/verdaccio:5
          ports:
            - containerPort: 4873
              name: http
          env:
            - name: VERDACCIO_PORT
              value: "4873"
            - name: VERDACCIO_PUBLIC_URL
              value: "https://npm.madfam.io"
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "500m"
              memory: "512Mi"
          volumeMounts:
            - name: config
              mountPath: /verdaccio/conf/config.yaml
              subPath: config.yaml
              readOnly: true
            - name: htpasswd
              mountPath: /verdaccio/conf/htpasswd
              subPath: htpasswd
            - name: storage
              mountPath: /verdaccio/storage
          livenessProbe:
            httpGet:
              path: /-/ping
              port: 4873
            initialDelaySeconds: 10
            periodSeconds: 30
          readinessProbe:
            httpGet:
              path: /-/ping
              port: 4873
            initialDelaySeconds: 5
            periodSeconds: 10
      volumes:
        - name: config
          configMap:
            name: verdaccio-config
        - name: htpasswd
          secret:
            secretName: verdaccio-auth
        - name: storage
          persistentVolumeClaim:
            claimName: verdaccio-storage
```

#### verdaccio-service.yaml
```yaml
apiVersion: v1
kind: Service
metadata:
  name: verdaccio
  namespace: enclii-workloads
  labels:
    app: verdaccio
spec:
  type: ClusterIP
  ports:
    - port: 4873
      targetPort: 4873
      protocol: TCP
      name: http
  selector:
    app: verdaccio
```

### Enclii Service Definition

#### npm-registry enclii.yaml
```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: npm-registry
  project: enclii-platform
  description: MADFAM private npm registry (npm.madfam.io)
  labels:
    tier: infrastructure
    criticality: high

spec:
  # Use official Verdaccio image
  image: verdaccio/verdaccio:5
  
  runtime:
    port: 4873
    replicas: 2
    resources:
      requests:
        cpu: "100m"
        memory: "128Mi"
      limits:
        cpu: "500m"
        memory: "512Mi"

  env:
    - name: VERDACCIO_PORT
      value: "4873"
    - name: VERDACCIO_PUBLIC_URL
      value: "https://npm.madfam.io"

  volumes:
    - name: storage
      mountPath: /verdaccio/storage
      size: 50Gi
      storageClassName: longhorn
    - name: config
      mountPath: /verdaccio/conf/config.yaml
      subPath: config.yaml
      configMapRef:
        name: verdaccio-config

  domains:
    - domain: npm.madfam.io
      tls: true
      tlsIssuer: cloudflare

  healthCheck: /-/ping
  
  readinessProbe:
    path: /-/ping
    initialDelaySeconds: 5
    periodSeconds: 10
    
  livenessProbe:
    path: /-/ping
    initialDelaySeconds: 10
    periodSeconds: 30

  autoscaling:
    enabled: true
    minReplicas: 1
    maxReplicas: 5
    targetCPUUtilizationPercentage: 70

  slo:
    availability: 99.9
    latencyP95: 100
    errorRate: 0.1

  backup:
    enabled: true
    schedule: "0 2 * * *"  # Daily at 2 AM
    retention: 30
    destination: r2://madfam-backups/npm-registry
```

### GitHub Actions Workflow (Enclii)

#### .github/workflows/publish-sdks.yml

Tag-triggered workflow that publishes individual `@enclii/*` packages. Push a tag like `shared-lib-v0.1.0` to trigger.

```yaml
name: Publish SDKs

on:
  push:
    tags:
      - "shared-lib-v*"
      - "ui-components-v*"
      - "config-v*"

jobs:
  publish:
    name: Publish @enclii SDK
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: "20"
          registry-url: "https://npm.madfam.io"
      - uses: pnpm/action-setup@v4
        with:
          version: 9
      - name: Extract package name and version
        id: extract
        run: |
          TAG=${GITHUB_REF#refs/tags/}
          PKG_NAME=${TAG%-v*}
          VERSION=${TAG#*-v}
          echo "pkg_name=$PKG_NAME" >> $GITHUB_OUTPUT
          echo "version=$VERSION" >> $GITHUB_OUTPUT
      - name: Install dependencies
        run: pnpm install
        working-directory: packages
      - name: Build
        run: pnpm run --if-present build
        working-directory: packages/${{ steps.extract.outputs.pkg_name }}
      - name: Publish to npm.madfam.io
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_MADFAM_TOKEN }}
        run: pnpm publish --no-git-checks --access public
        working-directory: packages/${{ steps.extract.outputs.pkg_name }}
      - name: Create GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          name: "@enclii/${{ steps.extract.outputs.pkg_name }} v${{ steps.extract.outputs.version }}"
```

**Publishing a new version:**
```bash
# 1. Bump version in packages/<name>/package.json
# 2. Commit and push
# 3. Tag and push
git tag shared-lib-v0.2.0
git push origin shared-lib-v0.2.0
```

#### Publishable packages (all repos)

| Package | Repo | Tag Pattern | Version | Status |
|---------|------|-------------|---------|--------|
| `@enclii/shared-lib` | enclii | `shared-lib-v*` | 0.1.0 | Published |
| `@enclii/ui-components` | enclii | `ui-components-v*` | 0.1.0 | Published |
| `@enclii/config` | enclii | `config-v*` | 0.1.0 | Published |
| `@janua/ui` | janua | `ui-v*` | 0.1.1 | Published |
| `@janua/react-sdk` | janua | `react-sdk-v*` | 0.1.1 | Published |
| `@janua/typescript-sdk` | janua | `typescript-sdk-v*` | 0.1.1 | Published |
| `@janua/nextjs` | janua | `nextjs-v*` | 0.1.2 | Published |
| `@dhanam/shared` | dhanam | `@dhanam/shared@*` | 0.1.0 | Published |
| `@dhanam/esg` | dhanam | `@dhanam/esg@*` | 0.1.0 | Published |
| `@dhanam/simulations` | dhanam | `@dhanam/simulations@*` | 0.1.0 | Published |
| `@dhanam/billing-sdk` | dhanam | `@dhanam/billing-sdk@*` | 0.2.0 | Published |
| `@tezca/api-client` | tezca | `api-client-v*` | 0.1.0 | Published |
| `@forgesight/client` | forgesight | `client-v*` | 0.1.0 | Published |

### .npmrc Template (for all MADFAM repos)
```ini
# MADFAM Private Registry
@madfam:registry=https://npm.madfam.io
@janua:registry=https://npm.madfam.io
@dhanam:registry=https://npm.madfam.io
@cotiza:registry=https://npm.madfam.io
@fortuna:registry=https://npm.madfam.io
@avala:registry=https://npm.madfam.io
@forgesight:registry=https://npm.madfam.io
@coforma:registry=https://npm.madfam.io
@forj:registry=https://npm.madfam.io
@enclii:registry=https://npm.madfam.io

# CI service token (a Verdaccio-issued JWT, rotated by the npm-token-rotation CronJob)
//npm.madfam.io/:_authToken=${NPM_MADFAM_TOKEN}
```

For a **person**, use a Janua API key instead of `_authToken` — it goes in
`_auth`, not `_authToken`. See
[Authentication](#authentication-janua-api-keys-htpasswd-fallback).

---

## Authentication (Janua API keys, htpasswd fallback)

The registry has two auth backends, tried in this order (see `auth:` in
`infra/k8s/base/verdaccio/configmap.yaml`):

| Order | Backend | Who it is for | Credential |
|-------|---------|---------------|------------|
| 1 | `auth-janua` (`verdaccio-auth-janua` plugin) | People | A Janua API key (`sk_live_…`, or a legacy `jnk_…`) in `_auth` |
| 2 | `htpasswd` | CI service users | The `NPM_MADFAM_TOKEN` JWT in `_authToken`, or a bcrypt username/password |

The plugin claims **only** passwords that start with a prefix in
`key_prefixes` — `sk_live_` (the only format Janua mints today) and `jnk_`
(legacy keys, still verified until revoked). Anything else returns
`callback(null, false)`, so htpasswd sees it unchanged. Existing CI
credentials are unaffected. A prefix list that names only `jnk_` refuses every
current-format key without asking Janua, which is what the list fixed on
2026-10-07.

### Getting a registry token (people)

1. Mint a key in the Janua dashboard: **Settings → API keys**
   (`apps/dashboard/app/settings/api-keys`).
2. Give it the scopes you need:
   - `npm:install` — required to install from private scopes.
   - `npm:publish` — additionally required to publish. Publishers need **both**.
3. Copy the key (`sk_live_…`); Janua shows it exactly once.
4. Configure npm with the key as **Basic** credentials:

```bash
npm config set //npm.madfam.io/:_auth "$(printf 'janua:%s' "$JANUA_API_KEY" | base64)"
npm config set //npm.madfam.io/:always-auth true
```

### Automated (workstations and agents)

`infra/scripts/npm/registry-login.py` does the four steps above from a Janua
session that is already open, so there is nothing to type and no key to copy:

```bash
python3 infra/scripts/npm/registry-login.py           # mint if missing or expiring
python3 infra/scripts/npm/registry-login.py --check   # verify only
```

- **Session:** `$JANUA_ACCESS_TOKEN`, otherwise the enclii CLI's own session
  (`enclii login` once; the script runs `enclii whoami` to refresh it).
- **Key:** scoped to the registry only (`npm:install`; `--scope publish` adds
  `npm:publish`), expiring after 90 days (`--days`). It is written to
  `~/.npmrc` as `_auth`, the registry's other credential lines for the host
  are removed (a stale `_authToken` would hide it), and `/-/whoami` must
  answer before the run counts as done. If the registry refuses the fresh
  key, the npmrc is put back and the key is revoked.
- **Rotation:** a run with more than 14 days left (`--renew-days`) does
  nothing, so it is safe at every login or in an agent's bootstrap; a run
  that mints revokes the key it minted before, and `--prune` revokes every
  other registry key the same host left behind.
- **Secrecy:** the key is never printed or logged. The npmrc and the state
  file (`~/.config/madfam/npm-registry-key.json`: id, prefix, scopes, expiry)
  are mode 600.

> [!NOTE]
> A repository whose `.npmrc` reads `_authToken=${NPM_MADFAM_TOKEN}` needs that
> variable **empty, not unset**, on a workstation: npm sends an unset
> `${VAR}` as its literal text, and that `_authToken` hides the `_auth` key
> (npm 10 has no `${VAR?}` form). `export NPM_MADFAM_TOKEN=` before `npm`
> does it; CI sets the variable and is unaffected.

### Do NOT use `_authToken`, and do NOT `npm login`

> [!IMPORTANT]
> A Janua API key in `_authToken` **silently does nothing**. `security.api.jwt`
> is configured on this registry, so Verdaccio treats an
> `Authorization: Bearer <token>` header as a **Verdaccio-signed JWT** and
> verifies its signature *before* any auth plugin runs. A Janua key fails
> that check, and Verdaccio's error handling converts the failure into an
> **anonymous** user rather than a 401 — so the request proceeds
> unauthenticated and you get a confusing "authorization required" on a private
> package, with no sign that your key was ever seen.
>
> `Authorization: Basic …` takes the other branch and calls the auth plugins,
> which is why the key belongs in `_auth`.

> [!IMPORTANT]
> `npm login --registry https://npm.madfam.io` sends your password to
> **htpasswd**, not Janua. Janua dashboard credentials are not htpasswd
> credentials, so login fails with `bad username/password, access denied`
> even when the account is perfectly valid. This is the single most common
> confusion with this registry. Mint an API key instead.

**The two credentials do not mix.** Which one you hold decides which command
you run — there is no combination that works both ways:

| You hold | Put it in | Command |
|---|---|---|
| A Janua API key (`sk_live_…`, or a legacy `jnk_…`) | `_auth` (HTTP Basic) | `npm config set //npm.madfam.io/:_auth "$(printf 'janua:%s' "$JANUA_API_KEY" \| base64)"` — never `npm login`, never `_authToken` |
| An htpasswd password (e.g. the rotated `admin@madfam.io`) | interactive login | `npm login --registry https://npm.madfam.io --auth-type=legacy` |
| The CI service token (a Verdaccio-issued JWT) | `_authToken` | set in CI; rotated by the `npm-token-rotation` CronJob |

`--auth-type=legacy` is required on that middle row: without it npm attempts the
web login flow, which this registry does not serve. See
[Rotating the admin password](#rotating-the-admin-password).

Verified against production on 2026-09-07:

| Credential | Result | Verdaccio log |
|---|---|---|
| `Bearer <opaque non-JWT>` | 401 | no `authenticating for user` line — plugins never ran |
| `Basic <user:pass>` | 401 | `authenticating for user someuser failed` — plugins ran |
| npm with `_auth` (Basic) | 401 | `authenticating for user testuser failed` — plugins ran |

### Scope enforcement

Once a key is verified, its Janua scopes become Verdaccio groups:

- `npm:install` missing → package access denied.
- `npm:publish` missing → publish denied (install still works).
- A key with neither is authenticated but has `$authenticated` only.

htpasswd users carry no `npm:*` groups, so the plugin falls through for them
and Verdaccio's own package rules apply — unchanged from before.

### CI service users (htpasswd, unchanged)

CI keeps using `NPM_MADFAM_TOKEN` in `_authToken`. That token is a
Verdaccio-issued JWT, so it passes JWT verification normally. It is
distributed to org repos by the `npm-token-rotation` CronJob (below).

### The plugin

Source of truth: `infra/k8s/base/verdaccio/plugins/verdaccio-auth-janua/`.
It is shipped to the pod as the `verdaccio-janua-plugin` ConfigMap
(`plugin-configmap.yaml`), which is **generated** — run
`python3 scripts/sync-verdaccio-janua-plugin.py --write` after editing the
source; CI fails on drift.

It calls `POST {janua_url}/api/v1/api-keys/verify` with `{"key": "<key>"}` and
reads `{valid, org_id, scopes, key_id}`. Results are cached in memory for
`cache_ttl_ms` (5 min). If Janua is unreachable the plugin falls through to
htpasswd rather than failing the request.

Janua's verify response has **no** `owner`/`name` field, so the registry
username is derived from the key id (`janua-key:<key_id>`) — registry logs stay
attributable to a specific key.

---

## Token rotation CronJob

`npm-token-rotation` (Sundays 02:00 UTC) validates `NPM_MADFAM_TOKEN`, and if
it is failing or within 30 days of expiry, renews it and propagates it to the
org repos in `NPM_ROTATION_REPO_ALLOWLIST`. It also pushes
`npm_token_expiry_days` to the Pushgateway for the alerts in
`npm-token-alert.yaml`.

### 2026-09-07: three consecutive failures, healthy token

The runs on **2026-08-23, 08-30 and 09-06** all failed
(`BackoffLimitExceeded`), while the token itself was fine — `/-/whoami`
returned 200 and it does not expire until 2027-04-25.

Root cause, in two parts, both introduced by
`bc573cdc` *fix(verdaccio): add publish smoke and safer npm token rotation (#249)*
on 2026-05-22:

1. That PR added an `npm publish --dry-run` smoke test to
   `verify_token_works()`, but the rotation image
   (`infra/docker/alpine-gh`: `ca-certificates curl github-cli jq`) has **no
   Node and no npm**. Under `set -euo pipefail` the missing binary failed the
   smoke on every run, so a healthy token was always judged invalid.
2. Having judged the token invalid, the script called `login_for_token()`,
   which requires `NPM_REGISTRY_PASSWORD`. The same PR documented a `password`
   key on `npm-token-rotation-creds` but never shipped an ExternalSecret for
   it. The live Secret carries only `token`, `gh_token` and `username`, so the
   script hit its guard and exited 1.

Evidence: job `npm-token-rotation-29811000` started 02:00:00Z and failed
02:00:30Z; Verdaccio logged three `GET /-/whoami` and **zero** publish or
`sec/login` requests — consistent with npm never executing.

**Fix (this change):** the publish check is now a `curl` PUT to
`/@madfam%2fnpm-publish-smoke` with an empty JSON body. Verdaccio authorizes
before parsing the payload, so an authorized token gets `422 bad incoming
package data` and an unauthorized one gets `401`/`403`. No npm binary, and
nothing is published — verified in production: the response was 422 and
`/verdaccio/storage/@madfam/npm-publish-smoke` was not created.

**Still open:** there is no `password` ExternalSecret, so unattended *renewal*
remains impossible; the job now fails with an explicit message saying so rather
than a bare exit. Renewal is not urgent (the token is valid until 2027-04-25),
but before then either add a password ExternalSecret or rotate by hand.

### Kyverno noise

Every rotation Job and the CronJob raised a `require-probes` PolicyViolation.
That policy is `validationFailureAction: Audit`, so it never blocked anything —
it was **not** the cause of the failures, only noise on the events an operator
reads when rotation breaks. Readiness probes are meaningless for a
run-to-completion batch Job, so
`infra/k8s/base/verdaccio/token-rotation-policy-exception.yaml` adds a
`PolicyException`, matching the existing `ceq-batch-job-probe-exceptions`
pattern.

---

## Canary procedure (enabling the plugin)

There is **no staging Verdaccio** — `npm-registry` runs a single production
instance on an RWO Longhorn PVC. The plugin is therefore validated offline
first, then rolled to production behind ArgoCD as a canary.

Offline (runs in CI, and locally with no cluster and no network):

```bash
node infra/k8s/base/verdaccio/plugins/verdaccio-auth-janua/test/plugin.test.js
python3 scripts/sync-verdaccio-janua-plugin.py
pytest tests/scripts/test_sync_verdaccio_janua_plugin.py -v
```

In the cluster, after ArgoCD syncs `npm-registry-services`:

1. **Confirm the plugin loaded.** The deployment is `strategy: Recreate` with a
   Reloader annotation, so the ConfigMap change restarts the pod (~30s):

   ```bash
   kubectl -n npm-registry logs deploy/verdaccio | grep 'plugin successfully loaded'
   ```

   Expect **three** lines — `verdaccio-auth-janua`, `verdaccio-htpasswd`,
   `verdaccio-audit`. Before this change only the last two appeared. If
   `verdaccio-auth-janua` is missing, the pod logs `plugin not found` and
   Verdaccio refuses to start; ArgoCD rollback restores the previous ConfigMap.

2. **Confirm htpasswd still works** (the regression that matters most) — an
   existing CI token must be unaffected:

   ```bash
   curl -s -o /dev/null -w '%{http_code}\n' \
     -H "Authorization: Bearer $NPM_MADFAM_TOKEN" \
     https://npm.madfam.io/@madfam%2fecosystem-banner
   ```

   Expect `200`.

3. **Install with a Janua key** carrying `npm:install`:

   ```bash
   npm config set //npm.madfam.io/:_auth "$(printf 'janua:%s' "$JANUA_API_KEY" | base64)"
   npm config set //npm.madfam.io/:always-auth true
   npm view @madfam/ecosystem-banner version --registry https://npm.madfam.io
   ```

   The pod should log `verdaccio-auth-janua: key verified`.

   > `npm whoami` is not a useful check here: it reports the username Verdaccio
   > derived, and `/-/whoami` is not access-gated on this registry (it returns
   > 200 even anonymously). Fetch a private package instead.

4. **Publish dry-run with a key carrying `npm:publish`:**

   ```bash
   npm publish --dry-run --registry https://npm.madfam.io
   ```

5. **Negative check** — a key with `npm:install` but not `npm:publish` must be
   denied publish while install still works. The pod logs
   `publish denied, missing npm:publish scope`.

Rollback is a git revert of the `auth:` block: htpasswd is listed second and is
untouched, so removing the `auth-janua` entry restores the previous behaviour
on the next sync.

---

## Deployment Checklist

### Pre-deployment
- [x] Porkbun DNS: Add CNAME for npm.madfam.io
- [x] Cloudflare: Configure tunnel ingress
- [x] Cloudflare: SSL settings configured
- [x] k3s cluster: Verify storage class exists
- [x] Secrets: Generate initial htpasswd

### Deployment
- [x] Apply PVC: `kubectl apply -f verdaccio-pvc.yaml`
- [x] Apply ConfigMap: `kubectl apply -f verdaccio-config.yaml`
- [x] Apply Secret: `kubectl apply -f verdaccio-secret.yaml`
- [x] Apply Deployment: `kubectl apply -f verdaccio-deployment.yaml`
- [x] Apply Service: `kubectl apply -f verdaccio-service.yaml`
- [x] Verify pods running: `kubectl get pods -l app=verdaccio`
- [x] Test health endpoint: `curl https://npm.madfam.io/-/ping`

### Post-deployment
- [x] Create admin user
- [x] Create CI bot user (for GitHub Actions)
- [x] Add NPM_MADFAM_TOKEN to GitHub org secrets
- [x] Update .npmrc in all repos
- [x] Publish initial @enclii packages (shared-lib, ui-components, config)
- [x] Test package installation
- [ ] Set up monitoring alerts

> **Status (Mar 2026):** Verdaccio running and healthy. ArgoCD-managed as `npm-registry-services` app. PVC reduced from 50Gi to 5Gi (Session 44). After PVC corruption + recreation (Mar 14, 2026), all 13 packages across 5 repos were republished. NPM_MADFAM_TOKEN rotated on all 5 repos (janua, enclii, dhanam, tezca, forgesight). Token expires ~Jun 12, 2026. CI publish workflows operational on enclii, dhanam, tezca, and forgesight.

---

## Monitoring & Alerts

### Health Checks
- Endpoint: `https://npm.madfam.io/-/ping`
- Expected: HTTP 200
- Check interval: 30s

### Alerts
| Metric | Threshold | Severity |
|--------|-----------|----------|
| Pod restarts | > 3/hour | Warning |
| Response time P95 | > 500ms | Warning |
| Error rate | > 1% | Critical |
| Storage usage | > 80% | Warning |
| Storage usage | > 95% | Critical |

### Grafana Dashboard
- Request rate
- Response time histogram
- Error rate
- Storage usage
- Active users

---

## Backup & Recovery

### Automated Backups
- Schedule: Daily at 2 AM UTC
- Retention: 30 days
- Destination: Cloudflare R2 (`r2://madfam-backups/npm-registry/`)

### Manual Backup
```bash
kubectl exec -n enclii-workloads deploy/verdaccio -- \
  tar czf - /verdaccio/storage | \
  aws s3 cp - s3://madfam-backups/npm-registry/manual-$(date +%Y%m%d).tar.gz
```

### Recovery Procedure
```bash
# 1. Scale down
kubectl scale deploy/verdaccio --replicas=0 -n enclii-workloads

# 2. Restore data
kubectl run restore --rm -it --image=alpine -- sh
# Inside pod: download and extract backup to PVC

# 3. Scale up
kubectl scale deploy/verdaccio --replicas=2 -n enclii-workloads
```

---

## htpasswd credential (Vault-sourced)

The registry's htpasswd file is **not in this repository**. It is materialized
from Vault by an ExternalSecret.

| | |
|---|---|
| Vault path | `secret/npm-registry`, property `htpasswd` |
| ClusterSecretStore | `vault-store` (the only store backed by real Vault) |
| ExternalSecret | `npm-registry/verdaccio-auth` — `infra/k8s/base/verdaccio/auth-externalsecret.yaml` |
| K8s Secret | `npm-registry/verdaccio-auth`, key `htpasswd` |
| Mounted at | `/verdaccio/conf/htpasswd` (`deployment.yaml`, unchanged) |
| Refresh | 15m, or immediately via a `force-sync` annotation |

`vault-store` is the right store here, and the `kubernetes-store` /
`enclii-builds-kubernetes-store` used by `token-rotation-externalsecrets.yaml`
are not: those are ESO *Kubernetes* providers that mirror Secrets out of the
`enclii` / `enclii-builds` namespaces. They hold no Vault data, so an
operator-rotated credential cannot live in them.

No Vault policy change is required to read this path — `eso-reader` already
grants `read` on `secret/data/*` (`scripts/cluster-ops-deploy.sh`). Writing it
needs a token with write on `secret/npm-registry`.

### 2026-09-07 incident: the hash was public

Until this change, `infra/k8s/base/verdaccio/secret.yaml` was a plain
`kind: Secret` committed to **madfam-org/enclii, a public repository**, holding
the bcrypt hash for `admin@madfam.io`. Two things made it worse than a bare
disclosure:

- The hash was **bcrypt cost 5** (`$2y$05$…`) — 32 rounds, about 1/32 the work
  of the cost-10 default. Cheap to attack offline.
- It was listed in `kustomization.yaml`, so ArgoCD applied it. Editing the live
  Secret by hand was reverted on the next sync, which is why the credential
  could not simply be changed in the cluster.

`git rm` does **not** remove a blob from history, so the published hash must be
treated as permanently disclosed. The remediation is **rotation** — after which
the published hash verifies nothing. History rewriting is deliberately out of
scope; see `docs/PUBLIC_REPO_BOUNDARY.md`.

Compounding it operationally: nobody held the plaintext, which is why
`npm login --registry https://npm.madfam.io` failed for everyone.

### Rotating the admin password

One shot, from anywhere, on macOS or Linux:

```bash
bash scripts/operator/npm-registry-admin-password-rotate.sh
```

It prompts silently for the new password and for a Vault token with write on
`secret/npm-registry`. Neither value is echoed, written to disk, or passed in
argv — both travel over stdin. The bcrypt hash is computed **locally** at cost
10, so the plaintext never leaves your machine.

The script then:

1. writes `secret/npm-registry #htpasswd` — `vault kv patch` when the path
   already exists, `vault kv put` when it does not, because **KV v2 answers 404
   to a patch on a path that has never been written** (#534);
2. reads the bcrypt **cost** back to prove the write landed — never the hash;
3. forces the `verdaccio-auth` ExternalSecret to resync;
4. waits for the pod (`strategy: Recreate` + `reloader.stakater.com/auto` on the
   **Deployment's own metadata**, ~30s), and compares the pod's
   `creationTimestamp` against the Secret's — if the pod is older it runs an
   explicit `rollout restart`, so the verification tests the new file even on a
   cluster where Reloader is absent or not firing (#534);
5. verifies with `GET /-/whoami`.

It performs no `kubectl apply` — the only cluster mutation is the Vault KV
write.

To rotate a different user: `REGISTRY_USER=someone@madfam.io bash scripts/…`.

Afterwards, log in with the password you chose:

```bash
npm login --registry https://npm.madfam.io --auth-type=legacy
```

`--auth-type=legacy` is required — without it npm attempts the web login flow,
which this registry does not serve.

> [!NOTE]
> The script refuses to run until the `verdaccio-auth` ExternalSecret exists in
> the cluster. Writing Vault before ArgoCD has synced would leave Vault ahead of
> the running pod, with nothing consuming the new value.

### 2026-09-07: the Secret was pruned before the Vault key existed

The first real rotation failed twice, in two different places. Both failures
were **ordering**, and both are fixed in
[#534](https://github.com/madfam-org/enclii/pull/534).

**1. Vault path did not exist — `404` on patch.**
[#529](https://github.com/madfam-org/enclii/pull/529) removed
`secret.yaml` from `kustomization.yaml` and shipped the `verdaccio-auth`
ExternalSecret in the same change. ArgoCD pruned the committed Secret on sync,
but **`secret/npm-registry #htpasswd` had never been written**, so the
ExternalSecret had nothing to materialize and the registry had no htpasswd file
at all. The rotation script then died on
`Error writing data to secret/data/npm-registry … Code: 404`: it used
`vault kv patch`, and KV v2 refuses to patch a path that has never existed.

> **The ordering rule.** Write the Vault property **before** the manifest change
> that consumes it reaches the cluster. An ExternalSecret is all-or-nothing: a
> property it references but Vault lacks fails the entire sync, and on a first
> sync the target Secret is never created. Retiring a committed Secret in the
> same change that introduces its ExternalSecret replacement inverts this — the
> old value is pruned before the new one can exist. Seed Vault first, merge
> second. The same rule governs every manifest under
> `vault-secrets/`; see
> [EXTERNAL_SECRETS.md](./EXTERNAL_SECRETS.md#ordering-rule-vault-first-manifest-second).

**2. The pod never restarted — the Reloader annotation was on the wrong object.**
With Vault seeded, ESO re-created `Secret/verdaccio-auth` and the pod kept
serving the kubelet's stale projection of `/verdaccio/conf/htpasswd` — dated
2026-09-05 — while reporting `Ready`. `/-/whoami` rejected the new password
until a manual `kubectl rollout restart deploy/verdaccio`.

The cause: `reloader.stakater.com/auto` sat on the **pod template**
(`spec.template.metadata.annotations`), where Reloader does not look. It had
been there since 2026-05 and had never fired.
`kubectl get deploy verdaccio -o jsonpath='{.metadata.annotations}'` returned
empty.

> **Reloader reads its annotations on the Deployment's own `metadata`, not on
> the pod template.** An annotation on the template is silently inert — there is
> no warning, and the Deployment looks correctly configured to anyone grepping
> the file for `reloader`. `auto: "true"` on the Deployment covers every
> ConfigMap and Secret it mounts (`verdaccio-config`,
> `verdaccio-janua-plugin`, `verdaccio-auth`).

Both symptoms present as "the new password does not work", and neither surfaces
an error on the object you would think to check: the ExternalSecret reported
`SecretSynced` and the pod reported `Ready` throughout the second one.

---

## Security Considerations

1. **Authentication**: Janua API keys (scoped `npm:install` / `npm:publish`)
   with htpasswd + bcrypt as the fallback for CI service users. See
   [Authentication](#authentication-janua-api-keys-htpasswd-fallback).
2. **TLS**: Enforced via Cloudflare (Full strict)
3. **Network Policy**: Only allow ingress from Cloudflare IPs
4. **Rate Limiting**: Cloudflare rate limiting rules
5. **Audit Logging**: All publish/unpublish actions logged
6. **Token Rotation**: CI tokens rotated quarterly (current token expires ~Jun 12, 2026)
7. **htpasswd provenance**: the htpasswd file is Vault-sourced, never committed
   — see [htpasswd credential (Vault-sourced)](#htpasswd-credential-vault-sourced)

---

## Cost Analysis

| Component | Monthly Cost |
|-----------|--------------|
| Hetzner storage (5Gi) | ~$0.25 |
| Cloudflare R2 backups | ~$0.50 |
| Cloudflare Tunnel | $0 |
| CPU/Memory (shared) | ~$2 |
| **Total** | **~$5/month** |

vs npmjs.com private packages: $7/user/month × 5 users = $35/month

**Savings: $30/month ($360/year)**

---

## Future Enhancements

1. **Janua browser SSO** - the web UI still uses htpasswd; API-key auth
   (delivered) covers the CLI, not an interactive login
2. **Password ExternalSecret for rotation** - unattended token *renewal* is
   still impossible; see [Token rotation CronJob](#token-rotation-cronjob)
3. **Package Signing** - Cosign for supply chain security
4. **Vulnerability Scanning** - Integrate with Snyk/Trivy
5. **Web UI Customization** - MADFAM branding
6. **Metrics Export** - Prometheus metrics for package downloads
