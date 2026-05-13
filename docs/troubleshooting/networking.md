---
title: Networking Issues
description: Troubleshoot DNS, SSL, Cloudflare tunnel, and routing problems
sidebar_position: 6
tags: [troubleshooting, networking, dns, ssl, cloudflare]
---

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


# Networking Issues Troubleshooting

This guide helps resolve DNS, SSL, Cloudflare tunnel, and service routing issues.

## Prerequisites

- Domain configured for your service
- Access to DNS provider (Porkbun, Cloudflare, etc.)

## Quick Diagnosis

```bash
# Check DNS resolution
dig <your-domain>

# Test HTTPS connectivity
curl -vI https://<your-domain>

# Check certificate
openssl s_client -connect <your-domain>:443 -servername <your-domain>

# Via CLI
enclii domains verify <domain>
```

## Common Networking Issues

### DNS Not Resolving

**Symptom**: Domain doesn't resolve, `dig` returns no results

**Causes**:
- DNS record not created
- Propagation delay (up to 48 hours)
- Wrong record type

**Solutions**:

1. **Verify DNS record exists**:
```bash
# Check with authoritative nameserver
dig @8.8.8.8 <your-domain>
dig @1.1.1.1 <your-domain>
```

2. **Check correct record type**:

For Cloudflare Tunnel:
```
Type: CNAME
Name: www
Value: <tunnel-id>.cfargotunnel.com
```

For direct IP:
```
Type: A
Name: www
Value: <server-ip>
```

3. **Clear local DNS cache**:
```bash
# macOS
sudo dscacheutil -flushcache
sudo killall -HUP mDNSResponder

# Linux
sudo systemd-resolve --flush-caches
```

4. **Wait for propagation** or lower TTL:
```bash
# Check propagation status
# Visit: https://www.whatsmydns.net/#A/<your-domain>
```

### SSL Certificate Errors

**Symptom**: Browser shows "Not Secure" or certificate warning

**Causes**:
- Certificate not issued
- Certificate expired
- Wrong domain on certificate
- Mixed HTTP/HTTPS content

**Solutions**:

1. **Check certificate details**:
```bash
echo | openssl s_client -connect <domain>:443 -servername <domain> 2>/dev/null | \
  openssl x509 -text -noout | grep -A2 "Validity"
```

2. **Verify certificate matches domain**:
```bash
echo | openssl s_client -connect <domain>:443 -servername <domain> 2>/dev/null | \
  openssl x509 -noout -subject -issuer
```

3. **Force certificate renewal** (via cert-manager):
```bash
kubectl delete certificate <cert-name> -n <namespace>
# cert-manager will automatically create new certificate
```

4. **Check cert-manager status** (admin):
```bash
kubectl get certificates -A
kubectl describe certificate <cert-name> -n <namespace>
```

### Cloudflare Tunnel Issues

**Symptom**: 502/503 errors or "Connection refused"

**Causes**:
- cloudflared pod not running
- Wrong tunnel configuration
- Service not reachable internally

**Solutions**:

1. **Check cloudflared pods**:
```bash
kubectl get pods -n cloudflare -l app=cloudflared
kubectl logs -n cloudflare -l app=cloudflared --tail=50
```

2. **Verify tunnel configuration**:
```yaml
# Expected ingress config
ingress:
  - hostname: <your-domain>
    service: http://<service>.<namespace>.svc.cluster.local:80
  - service: http_status:404
```

3. **Test internal service connectivity**:
```bash
kubectl run test --rm -it --image=curlimages/curl -- \
  curl http://<service>.<namespace>.svc.cluster.local:80/health
```

4. **Check tunnel status** on Cloudflare dashboard:
   - Go to: Zero Trust → Access → Tunnels
   - Verify tunnel shows "Healthy"

### Service Not Accessible

**Symptom**: DNS resolves but connection times out or refuses

**Causes**:
- Service not running
- Wrong port configuration
- Network policy blocking traffic
- Ingress misconfigured

**Solutions**:

1. **Verify service is running**:
```bash
enclii ps --service <service-id>
kubectl get pods -n <namespace> -l app=<service>
```

2. **Check service port mapping**:
```yaml
# Service should expose port 80 to tunnel
spec:
  ports:
    - port: 80
      targetPort: 3000  # Your container's port
```

3. **Test from inside cluster**:
```bash
kubectl run test --rm -it --image=curlimages/curl -- \
  curl -v http://<service>.<namespace>.svc.cluster.local:80
```

4. **Check network policies** (common root cause for ecosystem services):
```bash
# List all policies in the namespace
kubectl get networkpolicies -n <namespace>

# Inspect a specific policy's egress/ingress rules
kubectl describe networkpolicy <policy-name> -n <namespace>

# Check if the pod's labels match any allow policy's podSelector
kubectl get pod <pod-name> -n <namespace> --show-labels

# Common pitfalls:
# - Intra-namespace egress: use podSelector (no namespaceSelector)
# - Cross-namespace: must match BOTH namespaceSelector AND podSelector
# - Port must be container targetPort, NOT K8s service port
# - k3s sends TCP RST (Connection refused) for blocked traffic, not ETIMEDOUT
```

### 502 Bad Gateway

**Symptom**: Cloudflare returns 502 error page

**Causes**:
- Backend service crashed
- Health check timeout
- Connection to backend failed

**Solutions**:

1. **Check backend health**:
```bash
enclii logs <service> -f
kubectl logs -n <namespace> -l app=<service> --tail=100
```

2. **Verify health endpoint**:
```bash
kubectl exec -n <namespace> deploy/<service> -- curl localhost:3000/health
```

3. **Check if pods are ready**:
```bash
kubectl get pods -n <namespace> -l app=<service> -o wide
```

### 504 Gateway Timeout

**Symptom**: Request times out after 100 seconds

**Causes**:
- Backend processing too slow
- Upstream connection timeout
- Network latency

**Solutions**:

1. **Check response time** of backend:
```bash
time curl http://<internal-service-url>/api/slow-endpoint
```

2. **Increase timeout** in Cloudflare (if needed):
   - Enterprise plan required for >100s timeout

3. **Optimize slow endpoints**:
   - Add pagination
   - Use async processing
   - Add caching

### Custom Domain Setup

**Symptom**: Custom domain not working for service

**Solutions**:

1. **Add domain to service**:
```bash
enclii domains add --service <service-id> --domain <your-domain>
```

2. **Configure DNS** (CNAME to tunnel):
```
Type: CNAME
Name: app
Value: <tunnel-id>.cfargotunnel.com
TTL: Auto
```

3. **Add tunnel ingress rule** (admin):
```yaml
ingress:
  - hostname: <your-domain>
    service: http://<service>.<namespace>.svc.cluster.local:80
```

4. **Verify domain**:
```bash
enclii domains verify <your-domain>
```

## Port Configuration

### Understanding Port Hierarchy

```
Internet → Cloudflare (443) → Tunnel → K8s Service (80) → Pod (container port)
```

| Layer | Port | Configuration |
|-------|------|---------------|
| HTTPS | 443 | Cloudflare handles TLS |
| Service | 80 | K8s Service port |
| Container | varies | `targetPort` in Service, `EXPOSE` in Dockerfile |

### Common Port Misconfigurations

| Issue | Symptom | Fix |
|-------|---------|-----|
| Container listens on wrong port | Connection refused | Set correct `targetPort` |
| Service port not 80 | 404 from tunnel | Change Service to port 80 |
| PORT env not set | App starts on default | Set `PORT` environment variable |

## Debugging Tools

### From Outside Cluster

```bash
# DNS lookup
dig <domain> +short

# HTTP(S) test
curl -vvv https://<domain>

# SSL certificate check
openssl s_client -connect <domain>:443 -servername <domain>

# Trace route
traceroute <domain>

# HTTP headers only
curl -I https://<domain>
```

### From Inside Cluster

```bash
# Start debug pod
kubectl run debug --rm -it --image=nicolaka/netshoot -- /bin/bash

# Then inside pod:
curl http://<service>.<namespace>.svc.cluster.local
nslookup <service>.<namespace>.svc.cluster.local
nc -vz <service>.<namespace>.svc.cluster.local 80
```

### Cloudflare Logs

1. Go to Cloudflare Dashboard → Analytics → Logs
2. Filter by domain and status code
3. Check for error patterns

## Related Documentation

- **DNS Setup**: [DNS Setup (Porkbun)](/infrastructure/dns-setup-porkbun)
- **Cloudflare**: [Cloudflare Integration](/infrastructure/CLOUDFLARE)
- **Deployment Issues**: [Deployment Troubleshooting](./deployment-issues)
- **Architecture**: [Infrastructure Anatomy](/infrastructure/INFRA_ANATOMY)
