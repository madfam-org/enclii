# Enclii Service Examples

This directory contains example service configurations demonstrating Enclii platform features.

## Quick Start

```bash
# Deploy a stateful service with persistent volumes
enclii deploy -f examples/stateful-service.yaml

# Deploy a service with custom domain and HTTPS
enclii deploy -f examples/custom-domain-service.yaml

# Deploy a complete production-ready service
enclii deploy -f examples/complete-example.yaml
```

## Examples

### 1. [stateful-service.yaml](./stateful-service.yaml)
**Persistent Volumes for Stateful Applications**

Demonstrates:
- Multiple persistent volumes attached to a service
- Different storage classes (standard, fast-ssd)
- Volume lifecycle management (automatic PVC creation/deletion)

Use cases:
- File upload services
- Database-backed applications
- Cache with persistence

---

### 2. [custom-domain-service.yaml](./custom-domain-service.yaml)
**Custom Domains with Automatic TLS**

Demonstrates:
- Adding custom domains via API
- Automatic Let's Encrypt TLS certificate issuance
- DNS configuration
- Domain verification

Use cases:
- Production APIs with branded domains
- Multi-tenant SaaS applications
- White-label solutions

---

### 3. [multi-route-service.yaml](./multi-route-service.yaml)
**Advanced Routing with Multiple Paths**

Demonstrates:
- Path-based routing (`/api/v1`, `/api/v2`, `/docs`)
- Multiple domains pointing to the same service
- Different path types (Prefix, Exact)

Use cases:
- API versioning
- Microservices with different endpoints
- Documentation hosting
- A/B testing with path-based routing

---

### 4. [complete-example.yaml](./complete-example.yaml)
**Production-Ready Service Configuration**

Demonstrates:
- All features combined (volumes + domains + routes)
- Secrets management for database credentials
- Multiple replicas with health checks
- Regional persistent disks for high availability

Use cases:
- Production-grade CMS
- E-commerce backend
- SaaS platforms
- Content delivery services

---

## Feature Documentation

### Persistent Volumes

```yaml
volumes:
  - name: data
    mountPath: /app/data
    size: 50Gi
    storageClassName: standard  # optional, defaults to "standard"
    accessMode: ReadWriteOnce   # optional, defaults to "ReadWriteOnce"
```

**Storage Classes**:
- `standard`: HDD-based persistent disks (cost-effective)
- `fast-ssd`: SSD-based persistent disks (high IOPS)
- `regional-pd`: Multi-zone replication (high availability)

**Access Modes**:
- `ReadWriteOnce` (RWO): Single node mount, read-write
- `ReadOnlyMany` (ROX): Multiple nodes mount, read-only
- `ReadWriteMany` (RWX): Multiple nodes mount, read-write

---

### Custom Domains

**Add a custom domain**:
```bash
POST /v1/services/{service_id}/domains
{
  "domain": "api.example.com",
  "environment": "production",
  "tls_enabled": true,
  "tls_issuer": "letsencrypt-prod"
}
```

**List domains**:
```bash
GET /v1/services/{service_id}/domains
```

**Verify domain**:
```bash
POST /v1/services/{service_id}/domains/{domain_id}/verify
```

**TLS Issuers**:
- `letsencrypt-prod`: Production certificates (trusted, rate limited)
- `letsencrypt-staging`: Staging certificates (untrusted, no rate limit)
- `selfsigned-issuer`: Self-signed certificates (development only)

---

### Routes

**Add a route**:
```bash
POST /v1/services/{service_id}/routes
{
  "path": "/api/v1",
  "path_type": "Prefix",
  "port": 8080,
  "environment": "production"
}
```

**Path Types**:
- `Prefix`: Matches path prefix (e.g., `/api` matches `/api/users`)
- `Exact`: Exact path match only (e.g., `/docs` does not match `/docs/index`)
- `ImplementationSpecific`: Ingress controller-specific behavior

---

## Common Workflows

### Deploy Stateful Service

1. Create service with volumes:
   ```yaml
   volumes:
     - name: uploads
       mountPath: /data/uploads
       size: 100Gi
   ```

2. Deploy:
   ```bash
   enclii deploy -f service.yaml --env production
   ```

3. Verify PVCs created:
   ```bash
   kubectl get pvc -n enclii-{project-id}
   # myapp-uploads   Bound   100Gi
   ```

---

### Add Custom Domain

1. Deploy service
2. Add domain via API:
   ```bash
   curl -X POST https://api.enclii.io/v1/services/{id}/domains \
     -H "Authorization: Bearer $TOKEN" \
     -d '{"domain": "api.example.com", "environment": "production", "tls_enabled": true}'
   ```

3. Configure DNS:
   ```
   api.example.com  CNAME  ingress.cluster.enclii.io
   ```

4. Wait for cert-manager to issue Let's Encrypt certificate (~2 minutes)

5. Verify HTTPS:
   ```bash
   curl https://api.example.com/health
   ```

---

### Set Up Path-Based Routing

1. Add custom domain (see above)

2. Add routes:
   ```bash
   # API v1
   curl -X POST https://api.enclii.io/v1/services/{id}/routes \
     -d '{"path": "/api/v1", "path_type": "Prefix", "port": 8080}'

   # API v2
   curl -X POST https://api.enclii.io/v1/services/{id}/routes \
     -d '{"path": "/api/v2", "path_type": "Prefix", "port": 8080}'

   # Docs (exact match)
   curl -X POST https://api.enclii.io/v1/services/{id}/routes \
     -d '{"path": "/docs", "path_type": "Exact", "port": 8080}'
   ```

3. Test routes:
   ```bash
   curl https://api.example.com/api/v1/users   # → service:8080
   curl https://api.example.com/api/v2/users   # → service:8080
   curl https://api.example.com/docs            # → service:8080
   curl https://api.example.com/docs/index      # → 404 (Exact match only)
   ```

---

## Troubleshooting

### PVC Not Created

**Symptom**: Pod stuck in `Pending` state with event `FailedScheduling: persistentvolumeclaim not found`

**Solution**:
1. Check if volumes are defined in service spec
2. Verify storage class exists:
   ```bash
   kubectl get storageclass
   ```
3. Check PVC status:
   ```bash
   kubectl get pvc -n enclii-{project-id}
   kubectl describe pvc {service}-{volume-name}
   ```

---

### TLS Certificate Not Issued

**Symptom**: HTTPS returns self-signed certificate warning

**Solution**:
1. Check cert-manager logs:
   ```bash
   kubectl logs -n cert-manager deployment/cert-manager
   ```

2. Check Certificate resource:
   ```bash
   kubectl get certificate -n enclii-{project-id}
   kubectl describe certificate {service}-{domain}-tls
   ```

3. Verify DNS is configured correctly:
   ```bash
   dig api.example.com
   # Should resolve to ingress IP
   ```

4. Check Let's Encrypt rate limits (5 certs/week for production)

---

### Custom Domain Not Routing

**Symptom**: `curl https://api.example.com` returns 404

**Solution**:
1. Verify Ingress created:
   ```bash
   kubectl get ingress -n enclii-{project-id}
   ```

2. Check Ingress rules:
   ```bash
   kubectl describe ingress {service} -n enclii-{project-id}
   ```

3. Verify service exists:
   ```bash
   kubectl get svc {service} -n enclii-{project-id}
   ```

4. Test service directly:
   ```bash
   kubectl port-forward svc/{service} 8080:80
   curl http://localhost:8080/health
   ```

---

## Advanced Topics

### Multi-Region Deployments

(Future feature - not yet implemented)

### Canary Deployments

(Future feature - not yet implemented)

### Database Addon Provisioning

(Future feature - not yet implemented)

---

## Contributing

To add new examples:
1. Create a new YAML file in `examples/`
2. Include comprehensive comments
3. Test the example in a dev environment
4. Update this README with the example
5. Submit a pull request

---

## Support

- Documentation: https://docs.enclii.dev
- Issues: https://github.com/madfam-org/enclii/issues
- Discussions: https://github.com/madfam-org/enclii/discussions
