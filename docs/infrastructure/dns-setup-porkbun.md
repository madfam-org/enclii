---
title: DNS Setup (Porkbun)
description: Configure DNS records in Porkbun for Cloudflare Tunnel integration
sidebar_position: 20
tags: [infrastructure, dns, porkbun, cloudflare]
---

# DNS Setup Guide for npm.madfam.io (Porkbun)

## Related Documentation

- **Cloudflare**: [Cloudflare Integration](/infrastructure/CLOUDFLARE)
- **npm Registry**: [npm Registry Implementation](/infrastructure/npm-registry)
- **Troubleshooting**: [Networking Issues](/troubleshooting/networking)

## Overview

This guide walks through setting up DNS records in Porkbun for `npm.madfam.io` to point to the Verdaccio npm registry running on Enclii infrastructure.

## Architecture

```
User Request: npm install @madfam/ui
        │
        ▼
┌─────────────────┐
│  npm.madfam.io  │  DNS Query
│   (Porkbun)     │
└────────┬────────┘
         │ CNAME → Cloudflare Tunnel
         ▼
┌─────────────────┐
│   Cloudflare    │  TLS Termination + WAF + Caching
│   Tunnel        │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   Hetzner k3s   │  Verdaccio Service
│   Cluster       │
└─────────────────┘
```

## Option A: Cloudflare Tunnel (Recommended)

This approach uses Cloudflare Tunnel to expose the service without a public IP.

### Step 1: Create Cloudflare Tunnel

1. Go to Cloudflare Dashboard → Zero Trust → Access → Tunnels
2. Create a new tunnel named `madfam-infra`
3. Note the tunnel ID: `<tunnel-id>`

### Step 2: Configure Porkbun DNS

1. Log in to [Porkbun](https://porkbun.com)
2. Go to **Domain Management** → **madfam.io** → **DNS Records**
3. Add the following record:

| Type  | Host | Answer                              | TTL  |
|-------|------|-------------------------------------|------|
| CNAME | npm  | `<tunnel-id>.cfargotunnel.com`      | Auto |

### Step 3: Configure Tunnel Ingress

Add to your `cloudflared` config (running in k3s):

```yaml
# /etc/cloudflared/config.yml
tunnel: <tunnel-id>
credentials-file: /etc/cloudflared/credentials.json

ingress:
  - hostname: npm.madfam.io
    service: http://verdaccio.npm-registry.svc.cluster.local:4873
  - hostname: "*.madfam.io"
    service: http_status:404
  - service: http_status:404
```

### Step 4: Deploy Cloudflared in k3s

```yaml
# cloudflared-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudflared
  namespace: cloudflare
spec:
  replicas: 2
  selector:
    matchLabels:
      app: cloudflared
  template:
    metadata:
      labels:
        app: cloudflared
    spec:
      containers:
        - name: cloudflared
          image: cloudflare/cloudflared:latest
          args:
            - tunnel
            - --config
            - /etc/cloudflared/config.yml
            - run
          volumeMounts:
            - name: config
              mountPath: /etc/cloudflared
              readOnly: true
      volumes:
        - name: config
          secret:
            secretName: cloudflared-credentials
```

---

## Option B: Direct A Record (If using Load Balancer)

If you have a dedicated IP (e.g., Hetzner Load Balancer):

### Porkbun DNS Record

| Type | Host | Answer          | TTL  |
|------|------|-----------------|------|
| A    | npm  | `<server-ip>`   | 300  |

### Cloudflare Proxy (Optional but Recommended)

1. Add `madfam.io` to Cloudflare
2. Point Porkbun nameservers to Cloudflare:
   ```
   NS: ada.ns.cloudflare.com
   NS: greg.ns.cloudflare.com
   ```
3. In Cloudflare DNS, add:
   - Type: A, Name: npm, Content: `<server-ip>`, Proxy: ON

---

## Verification Steps

### 1. Check DNS Propagation

```bash
# Check DNS resolution
dig npm.madfam.io

# Expected output:
# npm.madfam.io.  300  IN  CNAME  <tunnel-id>.cfargotunnel.com.
```

### 2. Test HTTPS Connectivity

```bash
# Check TLS and endpoint
curl -I https://npm.madfam.io/-/ping

# Expected: HTTP/2 200
```

### 3. Test npm Registry

```bash
# Configure npm
npm config set @madfam:registry https://npm.madfam.io

# Test registry info
npm info @madfam/ui --registry https://npm.madfam.io
```

---

## Cloudflare Settings (if using Cloudflare)

### SSL/TLS
- Encryption mode: **Full (strict)**
- Always Use HTTPS: **On**
- Minimum TLS Version: **1.2**

### Caching
- Cache Level: **Standard**
- Browser Cache TTL: **4 hours**

### Page Rules (Optional)
```
npm.madfam.io/*
- Cache Level: Bypass (for authenticated requests)
- SSL: Full (strict)
```

### Firewall Rules
```javascript
// Rate limiting for abuse prevention
(http.request.uri.path contains "/-/" and not http.request.method in {"GET" "HEAD"})
// Action: Rate limit to 100 requests per minute
```

---

## Troubleshooting

### DNS Not Resolving

```bash
# Clear DNS cache
sudo dscacheutil -flushcache  # macOS
sudo systemd-resolve --flush-caches  # Linux

# Check with different DNS
dig @8.8.8.8 npm.madfam.io
dig @1.1.1.1 npm.madfam.io
```

### Cloudflare Tunnel Not Connecting

```bash
# Check tunnel status
cloudflared tunnel info <tunnel-id>

# Check logs in k3s
kubectl logs -n cloudflare -l app=cloudflared -f
```

### Certificate Issues

```bash
# Check certificate
openssl s_client -connect npm.madfam.io:443 -servername npm.madfam.io

# Verify certificate chain
curl -vI https://npm.madfam.io 2>&1 | grep -A5 "Server certificate"
```

---

## Rollback Plan

If issues occur:

1. **Quick rollback**: Update Porkbun CNAME to point to a fallback service
2. **Emergency**: Delete the DNS record to prevent requests
3. **Communication**: Update status page at status.madfam.io

---

## Maintenance

### DNS Record Updates

When updating DNS:
1. Lower TTL to 60 seconds 24 hours before change
2. Make the change
3. Wait for propagation (use whatsmydns.net)
4. Verify functionality
5. Restore normal TTL (300-3600 seconds)

### Cloudflare Tunnel Rotation

Rotate tunnel credentials quarterly:
1. Create new tunnel
2. Update k3s secret
3. Rollout cloudflared pods
4. Delete old tunnel

---

## Quick Reference

| Service | URL | Purpose |
|---------|-----|---------|
| npm Registry | https://npm.madfam.io | Package installation |
| Registry UI | https://npm.madfam.io/-/web | Web interface |
| Health Check | https://npm.madfam.io/-/ping | Monitoring |
| Search API | https://npm.madfam.io/-/v1/search | Package search |
