# Zero-Touch Deployment Execution Guide

> **Client**: ${CLIENT_NAME} (admin@example.com)
> **Workload**: LinkStack (links.example-app.dev)
> **Model**: Agency (MADFAM manages client infrastructure)

---

## Pre-Flight Checklist

### 1. Porkbun Nameserver Configuration ✅

The user must point `example-app.dev` nameservers to Cloudflare:

```
Nameserver 1: adam.ns.cloudflare.com
Nameserver 2: debbie.ns.cloudflare.com
```

**Porkbun Steps:**
1. Login to [porkbun.com](https://porkbun.com)
2. Navigate to Domain Management → `example-app.dev`
3. Click "Edit" next to Nameservers
4. Select "Custom nameservers"
5. Enter the Cloudflare nameservers above
6. Save changes

**Propagation**: Allow 15-60 minutes for DNS propagation.

---

## Required Environment Variables

Export these before running the deployment:

```bash
# ============================================================================
# Cloudflare API Configuration
# ============================================================================
# Get from: https://dash.cloudflare.com/profile/api-tokens
# Required permissions: Zone:Read, Zone:Edit, DNS:Edit
export CLOUDFLARE_API_TOKEN="your-cloudflare-api-token"

# Get from: Cloudflare Dashboard → Account Home → Account ID (right sidebar)
export CLOUDFLARE_ACCOUNT_ID="your-cloudflare-account-id"

# Get from: Cloudflare Dashboard → Zero Trust → Networks → Tunnels
# The UUID of your existing tunnel (e.g., "a1b2c3d4-e5f6-7890-abcd-ef1234567890")
export TUNNEL_ID="your-tunnel-uuid"

# ============================================================================
# Janua API Configuration
# ============================================================================
# Janua API endpoint
export JANUA_API="http://localhost:4100"  # Or production URL

# Admin JWT token for admin@madfam.io
# Get by: POST /api/v1/auth/login with admin credentials
export JANUA_ADMIN_TOKEN="your-janua-admin-jwt-token"

# ============================================================================
# Kubernetes Configuration
# ============================================================================
# Ensure kubectl is configured to the correct cluster
# Verify with: kubectl cluster-info
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"

# ============================================================================
# Application Secrets
# ============================================================================
# Generate a Laravel APP_KEY for LinkStack
# Generate with: openssl rand -base64 32
export LINKSTACK_APP_KEY="$(openssl rand -base64 32)"
```

---

## Quick Export Template

Copy-paste this block and fill in your values:

```bash
# Cloudflare
export CLOUDFLARE_API_TOKEN=""
export CLOUDFLARE_ACCOUNT_ID=""
export TUNNEL_ID=""

# Janua
export JANUA_API="http://localhost:4100"
export JANUA_ADMIN_TOKEN=""

# App Secrets
export LINKSTACK_APP_KEY="$(openssl rand -base64 32)"
```

---

## Execution Command

### Full Zero-Touch Deployment

```bash
cd ~/labspace/enclii

# Make scripts executable (first time only)
chmod +x scripts/deploy-client.sh
chmod +x scripts/provision-domain.sh
chmod +x scripts/onboard-${APP_NAME}.sh

# Execute the full deployment chain
./scripts/deploy-client.sh ${APP_NAME}
```

### What It Does (4 Phases)

| Phase | Action | Script |
|-------|--------|--------|
| 1. Identity | Create ${CLIENT_NAME} org, roles, invites in Janua | `onboard-${APP_NAME}.sh` |
| 2. Namespace | Create `${APP_NAME}-production` K8s namespace | `kubectl create ns` |
| 3. Network | Cloudflare Zone, DNS, Tunnel ConfigMap | `provision-domain.sh` |
| 4. Application | Deploy LinkStack pods, service, PVC | `kubectl apply -f` |

---

## Manual Phase Execution (If Needed)

### Phase 1: Identity Only
```bash
./scripts/onboard-${APP_NAME}.sh
```

### Phase 2: Namespace Only

> **Note**: For Enclii-managed apps, use `POST /v1/admin/onboard` instead — `EnsureNamespace()` auto-applies all required labels (`enclii.dev/data-access=true`, `enclii.dev/type=application`) which grant access to shared data services and Janua SSO via label-based NetworkPolicies.

```bash
kubectl create namespace ${APP_NAME}-production --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace ${APP_NAME}-production client=${APP_NAME} managed-by=madfam
```

### Phase 3: Network Only
```bash
./scripts/provision-domain.sh \
  --domain "example-app.dev" \
  --subdomain "links" \
  --service "linkstack" \
  --namespace "${APP_NAME}-production"
```

### Phase 4: Application Only
```bash
# First, update the APP_KEY secret
kubectl create secret generic linkstack-secrets \
  --namespace ${APP_NAME}-production \
  --from-literal=APP_KEY="base64:${LINKSTACK_APP_KEY}" \
  --dry-run=client -o yaml | kubectl apply -f -

# Deploy the application
kubectl apply -f clients/${APP_NAME}-linkstack.k8s.yaml
```

---

## Verification Commands

### Check Deployment Status
```bash
# All resources in namespace
kubectl get all -n ${APP_NAME}-production

# Pod logs
kubectl logs -n ${APP_NAME}-production -l app=linkstack -f

# Detailed pod status
kubectl describe pod -n ${APP_NAME}-production -l app=linkstack
```

### Check Cloudflare Tunnel
```bash
# Verify ConfigMap has new ingress
kubectl get configmap cloudflared-config -n foundry -o yaml | grep -A5 "links.example-app.dev"

# Check cloudflared logs
kubectl logs -n foundry -l app=cloudflared --tail=50
```

### Check DNS Propagation
```bash
# DNS lookup
dig links.example-app.dev CNAME +short

# Expected output: ${TUNNEL_ID}.cfargotunnel.com.

# HTTP test (after propagation)
curl -I https://links.example-app.dev
```

### Check Janua RBAC
```bash
# List organizations
curl -H "Authorization: Bearer $JANUA_ADMIN_TOKEN" \
  "$JANUA_API/api/v1/organizations/" | jq '.[] | select(.slug=="${APP_NAME}")'

# List org members
ORG_ID=$(curl -s -H "Authorization: Bearer $JANUA_ADMIN_TOKEN" \
  "$JANUA_API/api/v1/organizations/" | jq -r '.[] | select(.slug=="${APP_NAME}") | .id')

curl -H "Authorization: Bearer $JANUA_ADMIN_TOKEN" \
  "$JANUA_API/api/v1/organizations/$ORG_ID/members" | jq
```

---

## Troubleshooting

### Pod Not Starting
```bash
# Check events
kubectl get events -n ${APP_NAME}-production --sort-by='.lastTimestamp'

# Check PVC status (Longhorn must be available)
kubectl get pvc -n ${APP_NAME}-production
```

### DNS Not Resolving
```bash
# Check zone status in Cloudflare
curl -s -X GET "https://api.cloudflare.com/client/v4/zones?name=example-app.dev" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" | jq '.result[0].status'

# Should return: "active"
# If "pending", nameservers haven't propagated yet
```

### Tunnel Not Routing
```bash
# Restart cloudflared to pick up ConfigMap changes
kubectl rollout restart deployment/cloudflared -n foundry

# Check tunnel health
kubectl exec -n foundry -it deploy/cloudflared -- cloudflared tunnel info
```

### Janua API Errors
```bash
# Check if Janua is running
curl -s "$JANUA_API/health" | jq

# Test auth
curl -s -H "Authorization: Bearer $JANUA_ADMIN_TOKEN" \
  "$JANUA_API/api/v1/users/me" | jq
```

---

## Post-Deployment Steps

### 1. Client Onboarding Email

Send to `example-app.dev@gmail.com`:

```
Subject: Your LinkStack Instance is Ready!

Hi ${CLIENT_NAME},

Your self-hosted LinkStack is now live at:
https://links.example-app.dev

You've been invited to the ${CLIENT_NAME} organization in our management portal.
Check your email for the invitation link.

Login to manage your account:
- Profile settings
- Custom themes
- Link analytics

Support: admin@madfam.io

- MADFAM Team
```

### 2. Agency Model Verification

Login as `admin@madfam.io` and verify:
- [ ] Can access ${CLIENT_NAME} organization
- [ ] Has Admin role via `Managed_Services`
- [ ] Can view infrastructure without client credentials

### 3. Monitoring Setup (Optional)

```bash
# Add Prometheus ServiceMonitor if monitoring is enabled
kubectl apply -f - <<EOF
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: linkstack
  namespace: ${APP_NAME}-production
  labels:
    app: linkstack
spec:
  selector:
    matchLabels:
      app: linkstack
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
EOF
```

---

## Files Created

| File | Purpose |
|------|---------|
| `scripts/provision-domain.sh` | Cloudflare Zone/DNS/Tunnel automation |
| `scripts/deploy-client.sh` | Master deployment orchestrator |
| `scripts/onboard-${APP_NAME}.sh` | Janua RBAC setup |
| `clients/${APP_NAME}-linkstack.yaml` | Enclii service spec |
| `clients/${APP_NAME}-linkstack.k8s.yaml` | Raw K8s manifest |
| `docs/guides/AGENCY_MODEL_DEPLOYMENT.md` | Full deployment guide |
| `docs/guides/ZERO_TOUCH_EXECUTION.md` | This file |

---

*Zero-Touch Deployment | Agency Model Validation | MADFAM Platform*
