# Dhanam Billing Infrastructure

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> Treasury for Galaxy ecosystem payment processing

## Strategy: Hybrid Router

| Market | Provider | Currency | Payment Methods |
|--------|----------|----------|-----------------|
| Mexico | Stripe MX | MXN | Cards, OXXO, SPEI |
| Global | Paddle | USD/EUR | Cards, PayPal, Apple Pay, Google Pay |

## Setup Instructions

### 1. Create Secrets File

```bash
# Copy template (NEVER commit the filled version)
cp secrets-template.yaml secrets.yaml

# Edit with actual credentials
vim secrets.yaml
```

### 2. Obtain Credentials

**Stripe MX:**
1. Login to [Stripe Dashboard](https://dashboard.stripe.com/mx)
2. Go to Developers > API Keys
3. Copy Publishable key (`pk_live_...`)
4. Copy Secret key (`sk_live_...`)
5. Go to Developers > Webhooks
6. Create endpoint: `https://api.janua.dev/webhooks/stripe`
7. Copy Signing secret (`whsec_...`)

**Paddle:**
1. Login to [Paddle Dashboard](https://vendors.paddle.com)
2. Go to Developer Tools > Authentication
3. Copy Vendor ID (numeric)
4. Generate API Key
5. Copy Client-side token
6. Go to Developer Tools > Notifications
7. Set webhook URL: `https://api.janua.dev/webhooks/paddle`
8. Copy Webhook secret

### 3. Apply to Cluster

```bash
# Apply secrets
kubectl apply -f secrets.yaml

# Verify
kubectl get secret dhanam-secrets -n janua
kubectl describe secret dhanam-secrets -n janua
```

### 4. Verify in Application

```bash
# Check if pods can access secrets
kubectl exec -n janua deploy/janua-api -- env | grep -E "STRIPE|PADDLE"
```

## Security Checklist

- [ ] `secrets.yaml` is in `.gitignore`
- [ ] Credentials are from LIVE mode (not test)
- [ ] Webhook secrets are unique per endpoint
- [ ] NetworkPolicy restricts access to billing pods only
- [ ] Credentials rotation scheduled (quarterly)

## Files

| File | Purpose | Git Tracked |
|------|---------|-------------|
| `secrets-template.yaml` | Template with placeholders | Yes |
| `secrets.yaml` | Actual credentials | **NO** |
| `README.md` | This documentation | Yes |

## Namespace Migration

Currently deployed to `janua` namespace. Will migrate to dedicated `dhanam` namespace when:

1. Dhanam service is production-ready
2. Service mesh is configured
3. Dedicated database is provisioned

```bash
# Future migration
kubectl create namespace dhanam
kubectl apply -f secrets.yaml -n dhanam
```
