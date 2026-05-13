# Enclii Production Deployment Roadmap

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.

**Date:** January 2026 (Updated with Jan 2026 Infrastructure Deployments)
**Current Production Readiness:** 95%
**Target Production Readiness:** 100%
**Remaining:** Load testing, final security audit
**Estimated Monthly Cost:** See internal-devops for cost breakdown

> ⚠️ **Documentation Notice (Jan 2026):**
> This document was originally a planning roadmap. **Actual current infrastructure:**
> - **Single Hetzner dedicated server**, not 3x CPX31 VMs
> - **Self-hosted PostgreSQL** in-cluster, not Ubicloud managed
> - **Self-hosted Redis** in-cluster, not Redis Sentinel HA
> - **Single-node k3s**, not multi-node cluster (Longhorn ready for scaling)
>
> Multi-node architecture described below is the **planned future state**, not current.

---

## Executive Summary

This roadmap outlines the path to deploying Enclii to production with **validated, research-backed infrastructure** that maximizes cost savings while maintaining production-grade reliability.

**Recommended Infrastructure:** Hetzner Cloud + Cloudflare + Ubicloud

### The Winning Stack

```
┌─────────────────────────────────────────────────────┐
│           Cloudflare Edge (Global)                  │
│  ┌──────────────────────────────────────────────┐   │
│  │ • Tunnel (FREE - replaces load balancer)     │   │
│  │ • R2 Object Storage ($0-5/mo, zero egress)   │   │
│  │ • For SaaS (100 domains FREE)                │   │
│  │ • DDoS protection (FREE)                     │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────┬───────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────┐
│        Hetzner Cloud (Europe or US)                 │
│  ┌──────────────────────────────────────────────┐   │
│  │  Kubernetes Cluster (3x CPX31)               │   │
│  │  • 4 vCPU AMD EPYC, 8GB RAM each             │   │
│  │  • Private network only (no public IPs)      │   │
│  │  • €41/mo (~$45/mo)                          │   │
│  └──────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────┐   │
│  │  Ubicloud Managed PostgreSQL                 │   │
│  │  • Runs ON Hetzner infrastructure            │   │
│  │  • Managed HA, backups, monitoring           │   │
│  │  • See internal-devops for pricing            │   │
│  └──────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────┐   │
│  │  Self-Hosted Redis with Sentinel             │   │
│  │  • High availability (3 replicas)            │   │
│  │  • Automatic failover                        │   │
│  │  • ~$0 (included in compute)                 │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘

Total: ~$100/month
Features: Multi-tenant SaaS ready with 100 free custom domains
```

### Why This Stack Wins

**Cost Savings:**
- **vs DigitalOcean:** $341/mo → $100/mo = **$2,900/year saved**
- **vs Railway + Auth0:** SaaS equivalent/mo → $100/mo = **$25,440/year saved**
- **5-year savings:** **$127,200** vs Railway + Auth0

**Superior Features:**
- ✅ 100 custom domains FREE (Cloudflare for SaaS) - critical for multi-tenant
- ✅ Zero egress fees (Cloudflare R2) - no bandwidth surprises
- ✅ Built-in DDoS protection (Cloudflare Tunnel)
- ✅ No load balancer costs ($0 vs $6-12/mo)
- ✅ No public IP costs ($0 vs €0.50/node)
- ✅ Better hardware (AMD EPYC vs older Intel)
- ✅ Managed database without DigitalOcean prices

**Validated by Research:**
- Hetzner: Proven reliability, best price/performance
- Cloudflare: Industry-standard edge infrastructure
- Ubicloud: Managed PostgreSQL at bare-metal prices

---

## Part 1: Current State Assessment

### What We Have ✅

**Platform Features (95% Production Ready):**
- JWT authentication infrastructure with RBAC
- CSRF protection middleware
- Security headers (CSP, HSTS, X-Frame-Options)
- Pagination to prevent DoS attacks
- Rate limiting with memory-bounded LRU cache
- Audit logging infrastructure (async with fallback)
- 11 security tests (100% middleware coverage)
- ✅ GitHub webhook CI/CD with Buildpacks (Jan 2026)
- ✅ Container registry push (ghcr.io) (Jan 2026)
- ✅ SSO Logout with RP-Initiated flow (Jan 2026)

**Infrastructure (95% Production Ready):**
- Complete Kubernetes manifests for all services
- PostgreSQL deployment with health checks
- Redis deployment with persistence (AOF + RDB)
- NetworkPolicies for service isolation
- Pod security contexts (non-root, read-only FS)
- RBAC with ClusterRole/ServiceAccount
- Jaeger tracing deployment
- Prometheus ServiceMonitor definitions
- Environment-specific overlays (dev/staging/production)
- ✅ ArgoCD GitOps with App-of-Apps pattern (Jan 2026)
- ✅ Longhorn CSI for multi-node HA storage (Jan 2026)
- ✅ Cloudflare Tunnel route automation via API (Jan 2026)
- ✅ External Secrets Operator integration (Jan 2026)

### Completed (Jan 2026) ✅

| Component | Status | Implementation |
|-----------|--------|----------------|
| **Hetzner Cloud cluster** | ✅ Deployed | 3x CPX31 k3s nodes |
| **Cloudflare Tunnel ingress** | ✅ Deployed | Zero-trust with route automation |
| **Ubicloud PostgreSQL** | ✅ Deployed | Managed HA on Hetzner |
| **External Secrets** | ✅ Deployed | Replaces Sealed Secrets |
| **ArgoCD GitOps** | ✅ Deployed | App-of-Apps with self-heal |
| **Longhorn CSI** | ✅ Deployed | Multi-node replicated storage |
| **Janua SSO** | ✅ Deployed | auth.madfam.io with RP-Initiated Logout |
| **Cloudflare for SaaS** | ✅ Configured | 100 free custom domains |

### Remaining Gaps (5%)

| Gap | Impact | Priority | Solution |
|-----|--------|----------|----------|
| **Load testing validation** | Cannot confirm capacity | 🟠 High | k6 load testing to 1000 RPS |
| **Final security audit** | SOC 2 documentation | 🟠 High | Third-party penetration test |

---

## Part 2: Infrastructure Deep Dive

### Component 1: Hetzner Cloud Compute

**Why Hetzner:**
- ✅ Best price/performance ratio (validated by research)
- ✅ AMD EPYC processors (modern, fast)
- ✅ NVMe storage (3x faster than standard SSD)
- ✅ Transparent pricing (no surprise fees)
- ✅ GDPR-friendly (EU company)
- ✅ Proven reliability for production workloads

**Configuration:**
```
3x CPX31 instances:
- CPU: 4 vCPU AMD EPYC (shared but dedicated threads)
- RAM: 8GB DDR4
- Storage: 160GB NVMe SSD
- Network: 20TB bandwidth included
- Location: Falkenstein/Nuremberg (EU) or Ashburn (US)
- Cost: €13.79/mo each × 3 = €41.37/mo (~$45/mo)
```

**⚠️ Critical: Hetzner US Bandwidth Caps**
- US nodes have 3TB/month cap (vs 20TB in EU)
- Overage: €1/TB ($1.10/TB)
- **Solution:** Use Cloudflare R2 for all media/assets (zero egress)

**Setup:**
```bash
# Install hcloud CLI
brew install hcloud

# Create private network
hcloud network create --name enclii-private --ip-range 10.0.0.0/16

# Create 3 nodes
for i in {1..3}; do
  hcloud server create \
    --name enclii-node-$i \
    --type cpx31 \
    --image ubuntu-22.04 \
    --location fsn1 \
    --network enclii-private \
    --ssh-key ~/.ssh/id_rsa.pub
done

# Install k3s (lightweight Kubernetes)
# ... (detailed in Phase 3)
```

### Component 2: Cloudflare Tunnel (Ingress)

**Why Cloudflare Tunnel:**
- ✅ Replaces Load Balancer entirely ($0 vs $6-12/mo)
- ✅ No public IPs needed ($0 vs €0.50/node × 3 = €1.50/mo)
- ✅ Built-in DDoS protection (enterprise-grade)
- ✅ Global edge network (faster than direct access)
- ✅ Automatic failover between replicas
- ✅ No firewall port opening required (more secure)

**How It Works:**
```
User Request
    ↓
Cloudflare Edge (Nearest POP)
    ↓
Encrypted Tunnel (cloudflared)
    ↓
Your Kubernetes Service (private network)
```

**Setup:**
```bash
# Install cloudflared in Kubernetes
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudflared
  namespace: ingress
spec:
  replicas: 3  # High availability
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
        - --no-autoupdate
        - run
        - --token=$(TUNNEL_TOKEN)
        env:
        - name: TUNNEL_TOKEN
          valueFrom:
            secretKeyRef:
              name: cloudflared-secret
              key: token
EOF

# Configure routing
cloudflare-cli tunnel route dns <tunnel-id> api.enclii.dev
cloudflare-cli tunnel route dns <tunnel-id> auth.enclii.dev
cloudflare-cli tunnel route dns <tunnel-id> app.enclii.dev
```

**Cost Savings:**
- Load Balancer: $0 (saved $72/year)
- Public IPs: $0 (saved $18/year)
- **Total savings: $90/year**

### Component 3: Cloudflare for SaaS (Multi-Domain SSL)

**Why This is Critical for Enclii:**

Enclii is a multi-tenant platform. Each customer needs their own custom domain:
- `customer1.theirsite.com` → their Enclii apps
- `customer2.anotherdomain.io` → their Enclii apps

**Traditional Approach (DON'T DO THIS):**
```
cert-manager + Let's Encrypt in Kubernetes:
- High CPU usage (certificate generation)
- Rate limits (50 certs/week)
- Storage bloat (certificates in etcd)
- Complex lifecycle management
- Manual DNS validation for wildcards
```

**Cloudflare for SaaS Approach:**
```
100 custom domains FREE, then $0.10/domain/month:
- Automatic SSL certificate provisioning
- No rate limits
- Zero Kubernetes overhead
- Automatic renewal
- Edge SSL termination (faster)
```

**Setup:**
```bash
# Enable Cloudflare for SaaS
curl -X POST "https://api.cloudflare.com/client/v4/zones/{zone_id}/custom_hostnames" \
  -H "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{
    "hostname": "customer1.theirsite.com",
    "ssl": {
      "method": "txt",
      "type": "dv",
      "settings": {
        "min_tls_version": "1.2",
        "ciphers": ["ECDHE-RSA-AES128-GCM-SHA256"]
      }
    }
  }'

# Customer adds CNAME:
# customer1.theirsite.com → proxy.enclii.dev
# SSL auto-provisions in ~30 seconds
```

**Cost Impact:**
- First 100 domains: **FREE**
- Additional domains: **$0.10/mo each**
- At 1000 customers: $90/month
- **vs cert-manager:** Priceless (avoids complexity hell)

### Component 4: Cloudflare R2 (Object Storage)

**Why R2 Over Hetzner Storage Box:**
- ✅ **Zero egress fees** (critical for Hetzner US bandwidth caps)
- ✅ S3-compatible API (easy integration)
- ✅ Global CDN included
- ✅ Automatic replication

**Use Cases:**
- User-uploaded files (images, videos, documents)
- Build artifacts and container images
- Database backups (offsite)
- Static assets (CSS, JS, fonts)
- Log archives

**Pricing:**
```
Storage: $0.015/GB/month
Class A Operations (writes): $4.50/million
Class B Operations (reads): $0.36/million
Egress: $0 (!!!)

Example Monthly Cost (1000 users):
- 250GB storage: $3.75
- 10M reads: $3.60
- 1M writes: $4.50
- Egress: $0
- Total: ~$12/month

vs AWS S3:
- Storage: $5.75
- Reads: $3.60
- Writes: $5.00
- Egress (100GB): $9.00
- Total: ~$23.35/month
```

**Setup:**
```bash
# Create R2 bucket
wrangler r2 bucket create enclii-production

# Configure in application
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
R2_ACCESS_KEY_ID=<your-key>
R2_SECRET_ACCESS_KEY=<your-secret>
R2_BUCKET=enclii-production

# Use with any S3-compatible library
import boto3
s3 = boto3.client('s3',
    endpoint_url=R2_ENDPOINT,
    aws_access_key_id=R2_ACCESS_KEY_ID,
    aws_secret_access_key=R2_SECRET_ACCESS_KEY
)
```

### Component 5: Ubicloud Managed PostgreSQL

**Why Not Self-Host with Patroni:**
- ⚠️ Patroni setup: 8-12 hours initial + 2-4 hours/month maintenance
- ⚠️ You become the DBA (upgrades, failover, tuning, backups)
- ⚠️ 3am alerts when something breaks
- ⚠️ Complexity tax (etcd, HAProxy, replication monitoring)

**Why Not DigitalOcean Managed:**
- ⚠️ $120/month for db-s-2vcpu-4gb
- ⚠️ 3-4x more expensive than self-hosted

**Why Ubicloud is Perfect:**
- ✅ Managed PostgreSQL running ON Hetzner infrastructure
- ✅ Same reliability as DigitalOcean managed
- ✅ Cost-effective (see internal-devops for pricing)
- ✅ High availability with automated failover
- ✅ Automated backups with point-in-time recovery
- ✅ Monitoring and alerting included
- ✅ No operational overhead

**Configuration:**
```
Ubicloud PostgreSQL on Hetzner:
- CPU: 2 vCPU
- RAM: 4GB
- Storage: 80GB SSD
- HA: Primary + Standby (automatic failover)
- Backups: Daily with 7-day retention + PITR
- Monitoring: Built-in dashboards
- Cost: See internal-devops for pricing
```

**Setup:**
```bash
# Sign up at ubicloud.com
# Select "PostgreSQL on Hetzner"
# Choose region matching your Hetzner compute region
# Get connection string:
postgres://username:password@db.ubicloud.com:5432/enclii_production?sslmode=require

# Configure in Kubernetes
kubectl create secret generic postgres-credentials \
  --from-literal=url="postgres://..." \
  -n enclii-production
```

### Component 6: Self-Hosted Redis with Sentinel

**Why Self-Host Redis:**
- ✅ Redis is lightweight (memory-only)
- ✅ Sentinel provides HA with minimal setup
- ✅ Runs on existing Hetzner nodes (no extra cost)
- ✅ Much simpler than PostgreSQL HA

**Configuration:**
```yaml
# Redis with Sentinel (HA setup)
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis
spec:
  replicas: 3  # 1 master + 2 replicas
  template:
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        command:
        - redis-server
        - --appendonly yes
        - --replica-announce-ip $(POD_IP)
        volumeMounts:
        - name: data
          mountPath: /data
      - name: sentinel
        image: redis:7-alpine
        command:
        - redis-sentinel
        - /etc/redis/sentinel.conf
        # Sentinel monitors master and handles failover
```

**Failover Time:** 10-20 seconds (automatic)
**Cost:** $0 (runs on existing compute)

---

## Part 3: Cost Breakdown (Validated)

### Monthly Recurring Costs

| Component | Specification | Monthly Cost |
|-----------|---------------|--------------|
| **Hetzner Compute** | 3x CPX31 (AMD EPYC, 8GB each) | **$45** |
| **Ubicloud PostgreSQL** | Managed HA on Hetzner | **$50** |
| **Cloudflare Tunnel** | Ingress + DDoS protection | **$0** |
| **Cloudflare R2** | 250GB storage + 10M requests | **$5** |
| **Cloudflare for SaaS** | First 100 custom domains | **$0** |
| **Redis Sentinel** | Self-hosted HA | **$0** |
| **Monitoring** | Self-hosted Prometheus/Grafana | **$0** |
| **Janua Auth** | Self-hosted | **$0** |
| **Total** | | **$100/month** |

**Staging Environment:** See internal-devops for pricing
**Grand Total:** See internal-devops for cost breakdown

### One-Time Costs

| Item | Cost |
|------|------|
| **Infrastructure setup** | $0 (DIY) |
| **Third-party security audit** | $2,000 (optional but recommended) |
| **Domain registration** | $12/year |
| **Total One-Time** | **$2,012** |

### 5-Year Total Cost of Ownership

```
Year 1: $100/mo × 12 + $2,012 setup = $3,212
Year 2-5: $100/mo × 12 = $1,200/year × 4 = $4,800
Total: $8,012

At scale (1000 custom domains):
Year 1: ($100 + $90 domains)/mo × 12 + $2,012 = $4,292
Year 2-5: $190/mo × 12 = $2,280/year × 4 = $9,120
Total: $13,412
```

### Cost Comparison vs Alternatives

| Solution | Monthly | 5-Year Total | Savings vs Enclii |
|----------|---------|--------------|-------------------|
| **Enclii (Hetzner + Cloudflare + Ubicloud)** | $100 | $8,012 | $0 (baseline) |
| **Railway + Auth0** | SaaS equivalent | $133,200 | **$125,188** |
| **Vercel + Clerk** | $2,500 | $150,000 | **$141,988** |
| **DigitalOcean (managed services)** | $341 | $22,472 | **$14,460** |
| **AWS EKS** | $695 | $43,700 | **$35,688** |
| **Hetzner (pure self-hosted)** | $104 | $8,252 | **$240** |

**Key Takeaway:** This stack saves **$125K+ over 5 years** vs SaaS platforms while being easier to operate than pure self-hosted.

---

## Part 4: Janua Integration Strategy

### Deploy Janua Authentication Platform

**Timeline:** Week 1-2 (12-16 hours)

Janua provides:
- ✅ Complete login/signup UI (15 pre-built components)
- ✅ OAuth 2.0 + SAML 2.0 SSO
- ✅ Multi-factor authentication (TOTP/SMS/WebAuthn)
- ✅ Organization multi-tenancy with RBAC
- ✅ JWT tokens with RS256 (compatible with Enclii middleware)
- ✅ 202 REST API endpoints
- ✅ Python/Go/React/Next.js SDKs

**Deployment:**

```yaml
# infra/k8s/base/janua.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: janua
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: janua
        image: ghcr.io/madfam-org/janua:latest
        env:
        - name: DATABASE_URL
          value: "postgres://ubicloud-connection-string"
        - name: REDIS_URL
          value: "redis://redis-sentinel:26379"
        - name: JWT_PRIVATE_KEY
          valueFrom:
            secretKeyRef:
              name: janua-secret
              key: jwt-private-key
        - name: JANUA_BASE_URL
          value: "https://auth.enclii.dev"
```

**Integration with Switchyard:**

```go
// apps/switchyard-api/internal/middleware/auth.go
func (a *AuthMiddleware) Middleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Support both RS256 (Janua) and HS256 (internal)
        token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
            switch token.Method.(type) {
            case *jwt.SigningMethodRSA:
                return getJanuaPublicKey()  // Fetch from /.well-known/jwks.json
            case *jwt.SigningMethodHMAC:
                return a.jwtSecret, nil
            }
        })
        // ... rest of middleware
    }
}
```

**Cost:** $0 (shares Ubicloud PostgreSQL and Redis Sentinel)
**Savings vs Auth0:** $24,000-58,000/year

---

## Part 5: Production Deployment Phases

### Phase 3: Infrastructure & Cloud Setup (Weeks 1-2)

| Task | Effort | Cost |
|------|--------|------|
| **3.1: Set up Hetzner account & create 3 nodes** | 2h | $45/mo |
| **3.2: Configure private network (vSwitch)** | 1h | $0 |
| **3.3: Install k3s on all nodes** | 2h | $0 |
| **3.4: Deploy Cloudflare Tunnel** | 2h | $0 |
| **3.5: Configure Cloudflare for SaaS** | 2h | $0 |
| **3.6: Create Cloudflare R2 bucket** | 1h | $5/mo |
| **3.7: Set up Ubicloud PostgreSQL** | 1h | $50/mo |
| **3.8: Deploy Redis Sentinel** | 3h | $0 |
| **3.9: Install Sealed Secrets** | 2h | $0 |
| **3.10: Migrate secrets from dev** | 2h | $0 |
| **3.11: Configure DNS (Cloudflare)** | 1h | $0 |
| **3.12: Test SSL provisioning** | 1h | $0 |
| **Total** | **20h** | **$100/mo** |

**Deliverables:**
- ✅ Production Kubernetes cluster on Hetzner
- ✅ Cloudflare Tunnel ingress (no load balancer needed)
- ✅ Cloudflare for SaaS (100 free custom domains)
- ✅ Ubicloud managed PostgreSQL with HA
- ✅ Redis Sentinel with automatic failover
- ✅ Cloudflare R2 for object storage (zero egress)
- ✅ Encrypted secrets (Sealed Secrets)

### Phase 3B: Observability & Auth (Weeks 2-3)

| Task | Effort | Cost |
|------|--------|------|
| **3.13: Deploy Prometheus Operator** | 4h | $0 |
| **3.14: Deploy Grafana with dashboards** | 6h | $0 |
| **3.15: Deploy Loki for logs** | 4h | $0 |
| **3.16: Configure Jaeger** | 2h | $0 |
| **3.17: Set up alert rules** | 3h | $0 |
| **3.18: Configure PagerDuty/Opsgenie** | 2h | $10/mo |
| **3.19: Deploy Janua auth** | 8h | $0 |
| **3.20: Integrate Janua with Switchyard** | 6h | $0 |
| **3.21: Deploy Enclii applications** | 4h | $0 |
| **3.22: Test end-to-end** | 3h | $0 |
| **Total** | **42h** | **$10/mo** |

**Deliverables:**
- ✅ Complete observability (Prometheus, Grafana, Loki, Jaeger)
- ✅ Alert rules for SLO violations
- ✅ Janua authentication deployed
- ✅ Enclii applications running
- ✅ End-to-end tested

### Phase 4: Security Hardening (Weeks 3-4)

| Task | Effort | Cost |
|------|--------|------|
| **4.1: Namespace-scoped RBAC** | 3h | $0 |
| **4.2: Separate service accounts** | 2h | $0 |
| **4.3: Pod Disruption Budgets** | 2h | $0 |
| **4.4: Zero-trust NetworkPolicies** | 4h | $0 |
| **4.5: Kubernetes audit logging** | 3h | $0 |
| **4.6: Immutable audit log table** | 4h | $0 |
| **4.7: PgBouncer for connection pooling** | 3h | $0 |
| **4.8: Secret rotation automation** | 4h | $0 |
| **4.9: Resource quotas** | 2h | $0 |
| **4.10: Admission controllers (Kyverno)** | 6h | $0 |
| **4.11: Container scanning (Trivy)** | 3h | $0 |
| **4.12: Vulnerability automation** | 3h | $0 |
| **Total** | **39h** | **$0** |

**Deliverables:**
- ✅ SOC 2 CC6.1-6.8 controls
- ✅ Zero-trust architecture
- ✅ Immutable audit trails
- ✅ Automated vulnerability scanning

### Phase 5: Operational Excellence (Weeks 4-6)

| Task | Effort | Cost |
|------|--------|------|
| **5.1: Configure HPA** | 3h | $0 |
| **5.2: Configure VPA** | 2h | $0 |
| **5.3: GitHub Actions CI/CD** | 8h | $0 |
| **5.4: Blue-green deployments** | 6h | $0 |
| **5.5: Canary deployments (Flagger)** | 6h | $0 |
| **5.6: DR runbooks** | 8h | $0 |
| **5.7: Backup restoration tests** | 4h | $0 |
| **5.8: Staging environment** | 4h | $50/mo |
| **5.9: Load testing (k6)** | 6h | $0 |
| **5.10: Chaos engineering** | 6h | $0 |
| **5.11: Operational dashboards** | 4h | $0 |
| **Total** | **57h** | **$50/mo** |

**Deliverables:**
- ✅ Auto-scaling
- ✅ CI/CD with GitOps
- ✅ Zero-downtime deployments
- ✅ Tested DR procedures
- ✅ Chaos engineering validated

### Phase 6: Testing & Validation (Weeks 6-8)

| Task | Effort | Cost |
|------|--------|------|
| **6.1: Unit test coverage to 80%+** | 16h | $0 |
| **6.2: Integration tests** | 12h | $0 |
| **6.3: E2E tests (Playwright)** | 12h | $0 |
| **6.4: Load testing (1000 RPS)** | 6h | $0 |
| **6.5: Penetration testing** | 8h | $0 |
| **6.6: Security audit** | 16h | $2,000 |
| **6.7: Performance benchmarking** | 4h | $0 |
| **6.8: SLO validation** | 4h | $0 |
| **6.9: SOC 2 documentation** | 12h | $0 |
| **Total** | **90h** | **$2,000** |

**Deliverables:**
- ✅ 80%+ code coverage
- ✅ Load tested to 1000 RPS
- ✅ Security validated
- ✅ SOC 2 ready
- ✅ **Production readiness: 95%+**

---

## Part 6: Critical Implementation Details

### 1. Cloudflare Tunnel Setup (Replaces Load Balancer)

**What You're Replacing:**
```
❌ OLD: Kubernetes Load Balancer
   - Cost: $6-12/month
   - Exposes ports 80/443 publicly
   - Requires public IPs on nodes
   - No built-in DDoS protection
   - Single point of failure

✅ NEW: Cloudflare Tunnel
   - Cost: $0
   - No ports exposed (more secure)
   - No public IPs needed
   - Enterprise DDoS protection
   - Global edge network
   - High availability (3 replicas)
```

**Step-by-Step:**

```bash
# 1. Install cloudflared locally
brew install cloudflare/cloudflare/cloudflared

# 2. Authenticate
cloudflared tunnel login

# 3. Create tunnel
cloudflared tunnel create enclii-production
# Save tunnel ID and credentials

# 4. Configure routing
cloudflared tunnel route dns enclii-production api.enclii.dev
cloudflared tunnel route dns enclii-production auth.enclii.dev
cloudflared tunnel route dns enclii-production app.enclii.dev

# 5. Deploy to Kubernetes
kubectl create secret generic cloudflared-credentials \
  --from-file=credentials.json \
  -n ingress

kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cloudflared
  namespace: ingress
spec:
  replicas: 3  # HA
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
        - --no-autoupdate
        - run
        - --credentials-file=/etc/cloudflared/credentials.json
        - enclii-production
        volumeMounts:
        - name: credentials
          mountPath: /etc/cloudflared
          readOnly: true
        livenessProbe:
          httpGet:
            path: /ready
            port: 2000
          initialDelaySeconds: 10
          periodSeconds: 10
      volumes:
      - name: credentials
        secret:
          secretName: cloudflared-credentials
EOF

# 6. Configure ingress routing
# Edit tunnel configuration at dashboard.cloudflare.com
# Map domains to Kubernetes services:
# api.enclii.dev → http://switchyard-api.enclii-production.svc.cluster.local:8080
# auth.enclii.dev → http://janua.enclii-production.svc.cluster.local:8000
```

### 2. Cloudflare for SaaS (Multi-Tenant Domains)

**Why This is Game-Changing:**

When a customer deploys an app on Enclii, they want:
```
customer-app.customer-domain.com → their app
```

**Traditional cert-manager approach:**
```yaml
# ❌ Don't do this - causes problems at scale
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: customer-app-tls
spec:
  secretName: customer-app-tls
  issuerRef:
    name: letsencrypt-prod
  dnsNames:
  - customer-app.customer-domain.com

# Problems:
# - Let's Encrypt rate limit: 50 certs/week
# - High CPU usage for cert generation
# - Storage bloat in etcd
# - Complex DNS validation
# - Manual lifecycle management
```

**Cloudflare for SaaS approach:**
```bash
# ✅ Do this instead
curl -X POST "https://api.cloudflare.com/client/v4/zones/{zone_id}/custom_hostnames" \
  -H "Authorization: Bearer $CF_API_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{
    "hostname": "customer-app.customer-domain.com",
    "ssl": {
      "method": "txt",
      "type": "dv",
      "settings": {
        "min_tls_version": "1.2"
      }
    }
  }'

# Certificate provisions in ~30 seconds
# Automatic renewal
# No Kubernetes overhead
# No rate limits
# Edge SSL termination (faster)
```

**Customer Setup:**
```
Customer adds CNAME record:
customer-app.customer-domain.com → proxy.enclii.dev

That's it! SSL auto-provisions.
```

**Pricing:**
- First 100 custom hostnames: FREE
- Additional hostnames: $0.10/month each
- At 1000 customers: $90/month
- **Still cheaper than managing cert-manager at scale**

### 3. Hetzner US Bandwidth Trap (Critical!)

**The Problem:**
```
Hetzner EU nodes: 20TB bandwidth included
Hetzner US nodes: 3TB bandwidth cap

Overage: €1/TB ($1.10/TB)

If you serve 100GB/day of images:
- 100GB × 30 days = 3TB/month ← exactly at cap
- Any spike → overage charges
```

**The Solution: Cloudflare R2**
```
Store all media in R2:
- User uploads image → store in R2
- Image requests → serve from R2 (with Cloudflare CDN)
- Zero egress fees from R2
- Hetzner bandwidth used only for API responses (JSON, HTML)

Result:
- API traffic: ~500GB/month (well under cap)
- Media traffic: unlimited (R2 + CDN)
- No overage charges ever
```

**Implementation:**
```typescript
// apps/switchyard-api/internal/storage/r2.go
import (
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

type R2Storage struct {
    client *s3.Client
    bucket string
    cdnURL string  // e.g. https://cdn.enclii.dev
}

func (r *R2Storage) Upload(ctx context.Context, key string, data io.Reader) (string, error) {
    // Upload to R2
    _, err := r.client.PutObject(ctx, &s3.PutObjectInput{
        Bucket: &r.bucket,
        Key:    &key,
        Body:   data,
    })

    // Return CDN URL (not R2 direct URL)
    return fmt.Sprintf("%s/%s", r.cdnURL, key), err
}

// In customer app deployments:
// Environment variable: CDN_URL=https://cdn.enclii.dev
// All media URLs use CDN, bypassing Hetzner bandwidth
```

### 4. Connection Pooling (PgBouncer)

**The Problem:**
```
Kubernetes pods churn IPs constantly:
- Deployment rollout → new pods → new DB connections
- Auto-scaling → pods come/go → connection churn
- Pod restarts → reconnections

PostgreSQL has connection limits (default 100):
- 10 pods × 10 connections each = 100 connections
- Add Janua pods → over limit
- Add background workers → over limit
- Result: "too many clients" errors
```

**The Solution: PgBouncer**
```
PgBouncer sits between apps and database:
- Apps connect to PgBouncer (unlimited)
- PgBouncer pools connections to database
- Typical: 1000 app connections → 20 database connections
```

**Deployment:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pgbouncer
spec:
  replicas: 2  # HA
  template:
    spec:
      containers:
      - name: pgbouncer
        image: edoburu/pgbouncer:latest
        env:
        - name: DATABASE_URL
          value: "postgres://ubicloud-connection-string"
        - name: POOL_MODE
          value: "transaction"
        - name: MAX_CLIENT_CONN
          value: "1000"
        - name: DEFAULT_POOL_SIZE
          value: "25"
        ports:
        - containerPort: 5432

---
apiVersion: v1
kind: Service
metadata:
  name: postgres
spec:
  selector:
    app: pgbouncer
  ports:
  - port: 5432
    targetPort: 5432

# Apps connect to postgres.enclii-production.svc.cluster.local:5432
# (which is actually PgBouncer)
```

---

## Part 7: Production Readiness Checklist

### Infrastructure ✅

- [ ] Hetzner account created, 3x CPX31 nodes provisioned
- [ ] Private network (vSwitch) configured
- [ ] k3s installed on all nodes
- [ ] Cloudflare Tunnel deployed (3 replicas for HA)
- [ ] Cloudflare for SaaS enabled (100 free domains)
- [ ] Cloudflare R2 bucket created
- [ ] Ubicloud PostgreSQL provisioned (Hetzner region)
- [ ] Redis Sentinel deployed (3 replicas)
- [ ] PgBouncer deployed for connection pooling
- [ ] Sealed Secrets installed, all secrets encrypted
- [ ] DNS records configured (Cloudflare)

### Security ✅

- [ ] No public IPs on worker nodes (Cloudflare Tunnel only)
- [ ] TLS certificates auto-provisioning (Cloudflare for SaaS)
- [ ] Zero-trust NetworkPolicies enforced
- [ ] RBAC with least-privilege per service
- [ ] Pod Security Standards (restricted)
- [ ] Admission controllers (Kyverno) validating deployments
- [ ] Container scanning (Trivy) in CI
- [ ] Secrets rotation automated (90-day)
- [ ] Kubernetes audit logging enabled
- [ ] Immutable audit log table

### Authentication ✅

- [ ] Janua deployed (3 replicas)
- [ ] Janua connected to Ubicloud PostgreSQL
- [ ] Janua connected to Redis Sentinel
- [ ] JWT validation supports RS256 (Janua) + HS256 (internal)
- [ ] Frontend integrated with Janua React SDK
- [ ] OAuth providers configured (Google, GitHub)
- [ ] MFA enabled for admin accounts

### Observability ✅

- [ ] Prometheus deployed and scraping
- [ ] Grafana deployed with dashboards
- [ ] Loki deployed for logs
- [ ] Jaeger configured for tracing
- [ ] Alert rules configured
- [ ] PagerDuty/Opsgenie integration
- [ ] SLO compliance tracked (99.95% uptime)

### Multi-Tenancy ✅

- [ ] Cloudflare for SaaS configured
- [ ] First test domain SSL provisioned successfully
- [ ] Domain onboarding automated (API)
- [ ] Per-tenant resource isolation (namespaces)
- [ ] Per-tenant metrics and logging
- [ ] Custom domain documentation for customers

### Operations ✅

- [ ] CI/CD pipeline (GitHub Actions)
- [ ] Blue-green deployments configured
- [ ] Canary deployments (Flagger)
- [ ] HPA and VPA configured
- [ ] Pod Disruption Budgets
- [ ] Resource quotas per namespace
- [ ] Load tested (1000 RPS sustained)
- [ ] Chaos engineering validated
- [ ] DR runbooks documented and tested

### Self-Hosted Deployment ⭐ CRITICAL

- [ ] Service specs created for Enclii components
- [ ] Enclii API deployed via Enclii itself
- [ ] Enclii UI deployed via Enclii itself
- [ ] Janua deployed via Enclii (from separate repo)
- [ ] Landing page, docs, status page deployed via Enclii
- [ ] Continuous deployment enabled for all services
- [ ] Janua OAuth fully integrated (Enclii authenticates with Janua)
- [ ] Sales materials updated with self-hosted narrative
- [ ] Public status page shows Enclii services
- [ ] Team trained on deploying via `enclii deploy` command

**Why Critical:** Self-hosting provides customer confidence, validates product quality, and enables authentic sales narratives. See [ONBOARDING_GUIDE.md](../guides/ONBOARDING_GUIDE.md) for details.

---

## Part 8: Timeline

```
Week 1-2: Infrastructure & Cloud Setup
├─ Day 1-2:   Hetzner nodes + k3s
├─ Day 3-4:   Cloudflare Tunnel + for SaaS + R2
├─ Day 5-6:   Ubicloud PostgreSQL + Redis Sentinel
├─ Day 7-8:   Sealed Secrets + DNS
├─ Day 9-10:  Prometheus + Grafana + Loki
├─ Day 11-12: Janua deployment
└─ Day 13-14: Enclii applications deployed

Week 3-4: Security Hardening
├─ Day 15-17: Zero-trust networking + RBAC
├─ Day 18-20: Admission controllers + scanning
├─ Day 21-23: Audit logging + PgBouncer
└─ Day 24-28: Secret rotation + testing

Week 5-6: Operational Excellence & Self-Hosted Setup
├─ Day 29-31: Auto-scaling + CI/CD
├─ Day 32-34: Blue-green + canary
├─ Day 35-37: DR runbooks + testing
├─ Day 38-40: Staging environment + Service specs
└─ Day 41-42: Chaos engineering + Self-deployment migration

Week 7-8: Testing, Validation & Self-Deployment
├─ Day 43-46: Test coverage expansion + Deploy Enclii via Enclii
├─ Day 47-49: Load testing + Continuous deployment setup
├─ Day 50-52: Security audit + Janua OAuth integration testing
├─ Day 53-55: SOC 2 documentation + Sales material update
└─ Day 56:    🚀 PRODUCTION GO-LIVE (Fully Self-Hosted)
```

**Total Timeline:** 8 weeks (6 weeks with 2 engineers)

---

## Part 9: Success Metrics

### Platform Metrics

| Metric | Current | Target | Measurement |
|--------|---------|--------|-------------|
| **Production Readiness** | 95% | 100% | Checklist completion |
| **Security Score** | 8.5/10 | 9.5/10 | Audit findings |
| **Monthly Infrastructure Cost** | N/A | $100 | Billing |
| **SLO Compliance (Uptime)** | N/A | 99.95% | Prometheus |
| **P95 API Latency** | N/A | <200ms | Prometheus |
| **Error Rate** | N/A | <0.1% | Prometheus |
| **Custom Domains Supported** | 0 | 100+ | Cloudflare API |
| **Bandwidth Cost** | N/A | $0 overage | Cloudflare R2 |

### Business Metrics

| Metric | Value |
|--------|-------|
| **5-Year Infrastructure Savings** | $125,000+ (vs Railway + Auth0) |
| **Time to Production** | 8 weeks |
| **Operational Overhead** | 2-4 hours/week |
| **Multi-Tenant Ready** | Yes (100 domains free) |
| **Vendor Lock-In** | None (portable infrastructure) |

---

## Part 10: Next Steps

### Immediate Actions (This Week)

1. **Create Hetzner account** → https://console.hetzner.cloud/
2. **Create Cloudflare account** → https://dash.cloudflare.com/sign-up
3. **Create Ubicloud account** → https://www.ubicloud.com/
4. **Review this roadmap** → Confirm approach and budget

### Week 1 Actions (If Approved)

1. **Provision infrastructure:**
   ```bash
   # Hetzner
   hcloud server create --name enclii-node-{1,2,3} --type cpx31 --location fsn1

   # Ubicloud
   # Via web console: Create PostgreSQL on Hetzner

   # Cloudflare
   cloudflared tunnel create enclii-production
   wrangler r2 bucket create enclii-production
   ```

2. **Install Kubernetes:**
   ```bash
   # k3s on all nodes
   curl -sfL https://get.k3s.io | sh -
   ```

3. **Deploy core infrastructure:**
   ```bash
   # Cloudflare Tunnel, Redis Sentinel, Sealed Secrets
   kubectl apply -k infra/k8s/base
   ```

### Decision Points

**Confirm before proceeding:**

- [ ] Infrastructure choice: Hetzner + Cloudflare + Ubicloud?
- [ ] Budget: $100/month production + $50/month staging?
- [ ] Timeline: 8 weeks acceptable?
- [ ] Janua for auth (vs Auth0/Clerk)?
- [ ] Security audit: $2,000 third-party test?
- [ ] Self-hosted approach: Run Enclii on Enclii?

---

## Conclusion

This **research-validated architecture** provides:

1. ✅ **Unbeatable Cost:** $100/month (vs SaaS equivalent/month for Railway + Auth0)
2. ✅ **Superior Features:** 100 free custom domains (Cloudflare for SaaS)
3. ✅ **Zero Bandwidth Costs:** Cloudflare R2 with zero egress fees
4. ✅ **Enterprise Security:** DDoS protection, zero-trust networking
5. ✅ **Managed Database:** Ubicloud PostgreSQL at Hetzner prices
6. ✅ **No Vendor Lock-In:** Portable infrastructure (Kubernetes standard)
7. ✅ **Multi-Tenant Ready:** Built for SaaS from day one
8. ✅ **Production Grade:** 99.95% uptime SLA, auto-scaling, HA
9. ✅ **Fully Self-Hosted:** Enclii runs on Enclii, authenticated by Janua

**5-Year Savings: $125,000+** (vs Railway + Auth0)

**Confidence Signal:** "We run our entire platform on Enclii, authenticated by Janua. We're our own most demanding customer." — See [ONBOARDING_GUIDE.md](../guides/ONBOARDING_GUIDE.md)

**Recommended Next Step:** Approve budget and start Week 1 infrastructure provisioning.

---

**Document Version:** 3.0 (Jan 2026 Infrastructure Complete)
**Last Updated:** January 2026
**Validation Source:** Production deployment verification
**Status:** 95% Complete - Load testing and final security audit remaining
