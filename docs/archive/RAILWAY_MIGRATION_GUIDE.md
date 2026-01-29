# Railway to Enclii Migration Guide

**Version**: 1.0
**Last Updated**: 2025-11-20
**Estimated Migration Time**: 2-4 hours per service

---

## Table of Contents

1. [Pre-Migration Checklist](#pre-migration-checklist)
2. [Migration Strategy Overview](#migration-strategy-overview)
3. [Database Migration](#database-migration)
4. [Service Configuration](#service-configuration)
5. [Environment Variables & Secrets](#environment-variables--secrets)
6. [Custom Domains & Routing](#custom-domains--routing)
7. [Deployment & Verification](#deployment--verification)
8. [Rollback Procedures](#rollback-procedures)
9. [Common Issues](#common-issues)
10. [Migration Examples](#migration-examples)

---

## Pre-Migration Checklist

### 1. Assess Your Railway Setup

Run this audit on your Railway project:

```bash
# List all services
railway status

# Export environment variables
railway variables

# Check database connections
railway run env | grep DATABASE_URL
railway run env | grep REDIS_URL
```

**Document:**
- [ ] Service names and types (web, worker, cron)
- [ ] Database types (Postgres, Redis, MongoDB)
- [ ] Environment variables (especially secrets)
- [ ] Custom domains
- [ ] Volume mounts (if any)
- [ ] Service dependencies
- [ ] Health check endpoints
- [ ] Resource usage (CPU, RAM)

### 2. Prepare Enclii Environment

```bash
# Install Enclii CLI
curl -fsSL https://install.enclii.dev | sh

# Login
enclii login

# Create project
enclii project create my-app --region us-central1

# Create environments
enclii env create production
enclii env create staging
```

### 3. Choose Database Strategy

| Railway Setup | Recommended Enclii Approach | Migration Complexity |
|--------------|----------------------------|---------------------|
| **Railway Postgres** | AWS RDS / GCP CloudSQL | 🟡 Medium (requires dump/restore) |
| **Railway Redis** | AWS ElastiCache / GCP Memorystore | 🟢 Low (minimal data migration) |
| **Railway MongoDB** | MongoDB Atlas | 🟡 Medium (requires dump/restore) |
| **No database** | N/A | 🟢 Low |

---

## Migration Strategy Overview

### High-Level Process

```
┌─────────────────────────────────────────────────────────────────┐
│                     Migration Timeline                          │
├─────────────────────────────────────────────────────────────────┤
│ Week 1: Preparation                                             │
│   ├─ Audit Railway setup                                        │
│   ├─ Provision cloud databases (if needed)                      │
│   └─ Create Enclii project and environments                     │
│                                                                  │
│ Week 2: Staging Migration                                       │
│   ├─ Migrate staging database                                   │
│   ├─ Deploy services to Enclii staging                          │
│   ├─ Test thoroughly                                            │
│   └─ Fix issues                                                 │
│                                                                  │
│ Week 3: Production Migration (Blue-Green)                       │
│   ├─ Deploy to Enclii production (parallel to Railway)          │
│   ├─ Test with subset of traffic                                │
│   ├─ Migrate database with minimal downtime                     │
│   ├─ Switch DNS to Enclii                                       │
│   └─ Monitor for 48 hours                                       │
│                                                                  │
│ Week 4: Cleanup                                                 │
│   └─ Decommission Railway services                              │
└─────────────────────────────────────────────────────────────────┘
```

### Migration Approaches

**Option A: Blue-Green (Zero Downtime)**
- Deploy Enclii stack in parallel to Railway
- Use database replication for sync
- Switch DNS atomically
- Recommended for: Production services with high uptime requirements

**Option B: Direct Cutover (Brief Downtime)**
- Schedule maintenance window (5-30 minutes)
- Export Railway database
- Import to Enclii database
- Update DNS
- Recommended for: Internal tools, staging environments

**Option C: Gradual Migration (Service-by-Service)**
- Migrate non-critical services first
- Keep Railway database, update connection strings
- Migrate database last
- Recommended for: Microservices architectures

---

## Database Migration

### Strategy 1: Cloud-Managed Databases (Recommended)

#### AWS RDS (PostgreSQL)

**1. Provision RDS Instance**

```bash
# Create RDS PostgreSQL instance
aws rds create-db-instance \
  --db-instance-identifier myapp-prod \
  --db-instance-class db.t3.medium \
  --engine postgres \
  --engine-version 15.4 \
  --master-username postgres \
  --master-user-password <strong-password> \
  --allocated-storage 20 \
  --vpc-security-group-ids sg-xxxxx \
  --db-subnet-group-name myapp-subnet-group \
  --backup-retention-period 7 \
  --preferred-backup-window 03:00-04:00 \
  --preferred-maintenance-window sun:04:00-sun:05:00 \
  --storage-encrypted \
  --enable-performance-insights \
  --tags Key=Environment,Value=production
```

**2. Export from Railway**

```bash
# Connect to Railway database
railway run bash

# Export database
pg_dump $DATABASE_URL > railway_export.sql

# Or use compressed backup
pg_dump $DATABASE_URL | gzip > railway_export.sql.gz

# Download to local machine
railway run -- cat railway_export.sql.gz > railway_export.sql.gz
```

**3. Import to RDS**

```bash
# Prepare RDS connection string
export RDS_URL="postgresql://postgres:<password>@myapp-prod.xxxxx.us-east-1.rds.amazonaws.com:5432/postgres"

# Import data
gunzip -c railway_export.sql.gz | psql $RDS_URL

# Verify row counts
psql $RDS_URL -c "SELECT schemaname, tablename, n_live_tup FROM pg_stat_user_tables ORDER BY n_live_tup DESC;"
```

**4. Configure Security Group**

```bash
# Allow Kubernetes cluster to access RDS
aws ec2 authorize-security-group-ingress \
  --group-id sg-xxxxx \
  --protocol tcp \
  --port 5432 \
  --source-group sg-k8s-cluster
```

#### GCP CloudSQL (PostgreSQL)

**1. Provision CloudSQL Instance**

```bash
# Create CloudSQL instance
gcloud sql instances create myapp-prod \
  --database-version=POSTGRES_15 \
  --tier=db-custom-2-7680 \
  --region=us-central1 \
  --network=default \
  --backup \
  --backup-start-time=03:00 \
  --maintenance-window-day=SUN \
  --maintenance-window-hour=4 \
  --storage-auto-increase \
  --storage-size=20GB

# Set password
gcloud sql users set-password postgres \
  --instance=myapp-prod \
  --password=<strong-password>
```

**2. Export from Railway**

```bash
# Same as RDS export above
railway run -- pg_dump $DATABASE_URL | gzip > railway_export.sql.gz
```

**3. Import to CloudSQL**

```bash
# Get connection name
gcloud sql instances describe myapp-prod --format='value(connectionName)'

# Import using cloud_sql_proxy
./cloud_sql_proxy -instances=<connection-name>=tcp:5432 &

# Import data
gunzip -c railway_export.sql.gz | psql "host=localhost user=postgres dbname=postgres"
```

#### AWS ElastiCache / GCP Memorystore (Redis)

Redis typically doesn't require data migration for most use cases (caching).

**If you need to migrate Redis data:**

```bash
# Export from Railway Redis
railway run bash
redis-cli --rdb railway_redis.rdb
exit

# Import to ElastiCache/Memorystore
# Note: AWS ElastiCache and GCP Memorystore don't support direct RDB imports
# You'll need to use redis-dump-go or similar tools

# Install redis-dump-go
go install github.com/yannh/redis-dump-go@latest

# Export from Railway
redis-dump-go -host <railway-redis-host> -port 6379 > redis_data.json

# Import to new Redis
redis-dump-go -host <elasticache-host> -port 6379 -input redis_data.json -restore
```

### Strategy 2: In-Cluster Databases (Not Recommended for Production)

If you absolutely need in-cluster databases:

**PostgreSQL StatefulSet**

```yaml
# Create file: postgresql-statefulset.yaml
apiVersion: v1
kind: Service
metadata:
  name: postgresql
  namespace: myapp-production
spec:
  ports:
    - port: 5432
  selector:
    app: postgresql
  clusterIP: None
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgresql
  namespace: myapp-production
spec:
  serviceName: postgresql
  replicas: 1
  selector:
    matchLabels:
      app: postgresql
  template:
    metadata:
      labels:
        app: postgresql
    spec:
      containers:
        - name: postgresql
          image: postgres:15-alpine
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-credentials
                  key: password
            - name: POSTGRES_DB
              value: myapp
            - name: PGDATA
              value: /var/lib/postgresql/data/pgdata
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
          resources:
            requests:
              memory: "256Mi"
              cpu: "100m"
            limits:
              memory: "1Gi"
              cpu: "500m"
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: fast-ssd
        resources:
          requests:
            storage: 20Gi
```

Apply:

```bash
kubectl apply -f postgresql-statefulset.yaml
```

---

## Service Configuration

### Railway Service → Enclii Service Mapping

#### Railway `railway.json`

```json
{
  "build": {
    "builder": "NIXPACKS"
  },
  "deploy": {
    "startCommand": "npm start",
    "healthcheckPath": "/health",
    "restartPolicyType": "ON_FAILURE"
  }
}
```

#### Enclii Service Spec

Create `enclii.yaml`:

```yaml
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web-api
  project: myapp
spec:
  # Build configuration
  build:
    context: .
    # Auto-detect with Nixpacks (like Railway)
    buildpack: auto
    # Or use Dockerfile
    # dockerfile: Dockerfile

  # Runtime configuration
  runtime:
    port: 3000
    replicas: 2
    command: ["npm", "start"]

    # Health checks
    healthCheck:
      type: http
      path: /health
      port: 3000
      initialDelaySeconds: 30
      periodSeconds: 10
      timeoutSeconds: 5
      failureThreshold: 3

    # Resources (Railway defaults: 512MB RAM, 0.5 CPU)
    resources:
      requests:
        memory: "512Mi"
        cpu: "500m"
      limits:
        memory: "1Gi"
        cpu: "1000m"

    # Auto-scaling (Railway auto-scales based on traffic)
    autoscaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 10
      targetCPU: 70
      targetMemory: 80

  # Environment variables (see next section)
  env:
    - name: NODE_ENV
      value: production
    - name: PORT
      value: "3000"

  # Secrets (injected from Enclii Lockbox)
  secrets:
    - DATABASE_URL
    - REDIS_URL
    - API_KEY

  # Volumes (if your Railway service uses volumes)
  volumes:
    - name: uploads
      mountPath: /app/uploads
      size: 50Gi
      storageClassName: standard
    - name: cache
      mountPath: /app/.cache
      size: 10Gi
      storageClassName: fast-ssd

  # Routes (custom domains)
  routes:
    - domain: api.myapp.com
      path: /
      pathType: Prefix
      tlsEnabled: true
      tlsIssuer: letsencrypt-prod
```

### Initialize Service

```bash
# Create service
enclii service create -f enclii.yaml --env production

# Or use CLI directly
enclii init \
  --name web-api \
  --buildpack auto \
  --port 3000 \
  --health-check /health \
  --replicas 2
```

---

## Environment Variables & Secrets

### Export from Railway

```bash
# List all variables
railway variables

# Export to .env format
railway variables > railway.env

# Or use Railway API
curl -H "Authorization: Bearer $RAILWAY_TOKEN" \
  "https://backboard.railway.app/graphql/v2" \
  -d '{"query": "{ variables { edges { node { name value } } } }"}'
```

### Import to Enclii

**Option 1: Enclii Lockbox (Recommended)**

```bash
# Create secrets in Lockbox
enclii secret create DATABASE_URL "postgresql://user:pass@rds.amazonaws.com:5432/mydb" --env production
enclii secret create REDIS_URL "redis://elasticache.amazonaws.com:6379" --env production
enclii secret create API_KEY "sk_live_xxxxx" --env production
enclii secret create JWT_SECRET "super-secret-key" --env production

# Verify secrets
enclii secret list --env production
```

**Option 2: Kubernetes Secrets (Direct)**

```bash
# Create secret from file
kubectl create secret generic app-secrets \
  --from-env-file=railway.env \
  --namespace=myapp-production

# Or create inline
kubectl create secret generic app-secrets \
  --from-literal=DATABASE_URL="postgresql://..." \
  --from-literal=REDIS_URL="redis://..." \
  --namespace=myapp-production
```

**Option 3: Bulk Import Script**

```bash
#!/bin/bash
# import-secrets.sh

# Read Railway environment file
while IFS='=' read -r key value; do
  # Skip comments and empty lines
  [[ $key =~ ^#.*$ ]] && continue
  [[ -z $key ]] && continue

  # Remove quotes from value
  value=$(echo $value | sed 's/^"//; s/"$//')

  # Create Enclii secret
  echo "Importing $key..."
  enclii secret create "$key" "$value" --env production
done < railway.env

echo "✅ Import complete!"
```

Run:

```bash
chmod +x import-secrets.sh
./import-secrets.sh
```

### Verify Secret Injection

Update `enclii.yaml` to reference secrets:

```yaml
spec:
  secrets:
    - DATABASE_URL
    - REDIS_URL
    - API_KEY
    - JWT_SECRET
```

Test:

```bash
# Deploy with secrets
enclii deploy --env production

# Verify secrets are injected
enclii exec web-api --env production -- env | grep DATABASE_URL
```

---

## Custom Domains & Routing

### Railway Domain Setup

Railway typically provides:
- `your-service.railway.app` (automatic)
- Custom domain with automatic HTTPS

### Enclii Domain Setup

**1. Add Custom Domain**

```bash
# Add custom domain
enclii domain add api.myapp.com \
  --service web-api \
  --env production \
  --tls-enabled \
  --tls-issuer letsencrypt-prod

# Verify domain
enclii domain list --service web-api --env production
```

**2. Configure DNS**

Set DNS records with your provider:

```dns
# A record
api.myapp.com.  300  IN  A  <enclii-ingress-ip>

# Or CNAME record
api.myapp.com.  300  IN  CNAME  <cluster-domain>
```

Get ingress IP:

```bash
# Get ingress IP
kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

**3. Verify DNS Ownership**

Enclii requires DNS TXT record verification:

```bash
# Get verification token
enclii domain verify api.myapp.com --service web-api --env production

# Output:
# Add this TXT record to your DNS:
# _enclii-verification.api.myapp.com.  TXT  "enclii-verification=abc123xyz"
```

Add TXT record:

```dns
_enclii-verification.api.myapp.com.  300  IN  TXT  "enclii-verification=abc123xyz"
```

Verify:

```bash
# Trigger verification
enclii domain verify api.myapp.com --service web-api --env production

# Check status
enclii domain status api.myapp.com --service web-api --env production
```

**4. Configure Routes (Path-Based Routing)**

If you have multiple paths on Railway, configure routes:

```bash
# Add routes
enclii route add /api/v1 \
  --service web-api \
  --env production \
  --path-type Prefix \
  --port 3000

enclii route add /api/v2 \
  --service web-api-v2 \
  --env production \
  --path-type Prefix \
  --port 3000

enclii route add /health \
  --service web-api \
  --env production \
  --path-type Exact \
  --port 3000
```

**5. Wait for TLS Certificate**

```bash
# Check certificate status
kubectl get certificate -n myapp-production

# Watch certificate issuance
kubectl describe certificate web-api-api-myapp-com-tls -n myapp-production

# When ready, test HTTPS
curl https://api.myapp.com/health
```

---

## Deployment & Verification

### Deploy to Staging First

```bash
# Deploy to staging
enclii deploy --env staging

# Monitor deployment
enclii status --env staging --follow

# Check logs
enclii logs web-api --env staging --follow

# Run smoke tests
curl https://staging-api.myapp.com/health
curl https://staging-api.myapp.com/api/v1/users
```

### Deploy to Production (Blue-Green)

**1. Deploy New Stack (Green)**

```bash
# Deploy to production (parallel to Railway)
enclii deploy --env production

# Wait for healthy status
enclii status --env production

# Get Enclii service URL
enclii info --env production
```

**2. Test with Subset of Traffic**

Option A: Use Railway and Enclii in parallel with DNS weighted routing

```dns
# Weighted DNS (send 10% traffic to Enclii)
api.myapp.com.  60  IN  A  <railway-ip>  ; weight 90
api.myapp.com.  60  IN  A  <enclii-ip>   ; weight 10
```

Option B: Use a specific test subdomain

```bash
# Test via direct ingress
curl -H "Host: api.myapp.com" http://<enclii-ingress-ip>/health

# Or create test subdomain
enclii domain add test-api.myapp.com --service web-api --env production
```

**3. Migrate Database (Blue-Green Strategy)**

For PostgreSQL with minimal downtime:

```bash
# Set up logical replication from Railway to RDS
# (requires Railway to be source, RDS as replica)

# On Railway PostgreSQL (if you have access)
railway run bash

# Create publication
psql $DATABASE_URL -c "CREATE PUBLICATION railway_pub FOR ALL TABLES;"

# On RDS
psql $RDS_URL -c "CREATE SUBSCRIPTION rds_sub CONNECTION '$RAILWAY_DATABASE_URL' PUBLICATION railway_pub;"

# Wait for initial sync
psql $RDS_URL -c "SELECT * FROM pg_stat_subscription;"

# When lag is minimal (<1 second), proceed to cutover
```

**4. Cutover (Update DNS)**

```bash
# Update DNS to point to Enclii
# Change A record from Railway IP to Enclii IP
api.myapp.com.  60  IN  A  <enclii-ingress-ip>

# Flush DNS cache (if testing locally)
sudo dnsmasq --clear-cache  # Linux
sudo killall -HUP mDNSResponder  # macOS
```

**5. Monitor After Cutover**

```bash
# Watch metrics
enclii metrics --env production

# Monitor error rate
enclii logs web-api --env production --follow | grep ERROR

# Check health
watch -n 5 'curl -s https://api.myapp.com/health'

# Monitor database connections
psql $RDS_URL -c "SELECT count(*) FROM pg_stat_activity;"
```

### Verification Checklist

After migration, verify:

- [ ] All services are healthy: `enclii status --env production`
- [ ] HTTPS works: `curl https://api.myapp.com`
- [ ] Database connectivity: Test critical API endpoints
- [ ] Secret injection: Verify env vars are set correctly
- [ ] Logs are flowing: `enclii logs --env production`
- [ ] Metrics are reporting: Check Prometheus/Grafana
- [ ] Custom domains resolve: `nslookup api.myapp.com`
- [ ] Health checks pass: `curl https://api.myapp.com/health`
- [ ] Error rates are normal: Compare to Railway baseline
- [ ] Response times are acceptable: Compare to Railway baseline

---

## Rollback Procedures

### Immediate Rollback (DNS Switch)

If issues are detected within first 24 hours:

```bash
# 1. Switch DNS back to Railway
api.myapp.com.  60  IN  A  <railway-ip>

# 2. Wait for DNS propagation (TTL was set to 60 seconds)
# Most traffic will switch back within 1-2 minutes

# 3. Investigate issues on Enclii
enclii logs web-api --env production --tail 1000 > enclii_error.log

# 4. Keep Railway running until issues are resolved
```

### Database Rollback

If database migration failed:

```bash
# If using RDS replica
# 1. Stop replication
psql $RDS_URL -c "DROP SUBSCRIPTION rds_sub;"

# 2. Update connection string back to Railway
enclii secret update DATABASE_URL "$RAILWAY_DATABASE_URL" --env production

# 3. Redeploy services
enclii deploy --env production --force

# 4. Verify connectivity
enclii exec web-api --env production -- psql $DATABASE_URL -c "SELECT 1;"
```

### Complete Rollback to Railway

If fundamental issues require full rollback:

```bash
# 1. Update DNS
api.myapp.com.  60  IN  A  <railway-ip>

# 2. Notify team
echo "Rolled back to Railway at $(date)" | mail -s "Migration Rollback" team@myapp.com

# 3. Document issues
cat > rollback_report.md <<EOF
## Rollback Report
- **Time**: $(date)
- **Reason**: <describe issues>
- **Impact**: <user impact>
- **Next Steps**: <investigation plan>
EOF

# 4. Keep Enclii environment for investigation
# DO NOT delete Enclii resources yet
```

---

## Common Issues

### Issue 1: Database Connection Refused

**Symptom:**
```
Error: connect ECONNREFUSED
```

**Diagnosis:**

```bash
# Check if database is accessible from pod
enclii exec web-api --env production -- nc -zv <db-host> 5432

# Check security group rules
aws ec2 describe-security-groups --group-ids sg-xxxxx
```

**Fix:**

```bash
# Update security group to allow K8s cluster
aws ec2 authorize-security-group-ingress \
  --group-id sg-xxxxx \
  --protocol tcp \
  --port 5432 \
  --source-group sg-k8s-cluster

# Or use VPC peering if different VPCs
```

### Issue 2: Secrets Not Injected

**Symptom:**
```
Error: DATABASE_URL is not defined
```

**Diagnosis:**

```bash
# Check if secret exists
enclii secret list --env production | grep DATABASE_URL

# Check pod environment
kubectl exec -n myapp-production deploy/web-api -- env | grep DATABASE_URL
```

**Fix:**

```bash
# Recreate secret
enclii secret create DATABASE_URL "postgresql://..." --env production

# Force redeploy
enclii deploy --env production --force
```

### Issue 3: TLS Certificate Not Issued

**Symptom:**
```
curl: (60) SSL certificate problem: unable to get local issuer certificate
```

**Diagnosis:**

```bash
# Check certificate status
kubectl get certificate -n myapp-production

# Check cert-manager logs
kubectl logs -n cert-manager -l app=cert-manager --tail=100

# Describe certificate
kubectl describe certificate web-api-api-myapp-com-tls -n myapp-production
```

**Fix:**

```bash
# Delete and recreate certificate
kubectl delete certificate web-api-api-myapp-com-tls -n myapp-production

# Trigger domain reconciliation
enclii domain verify api.myapp.com --service web-api --env production --force

# Wait for issuance (can take 2-5 minutes)
kubectl get certificate -n myapp-production -w
```

### Issue 4: High Memory Usage

**Symptom:**
```
OOMKilled - container exceeded memory limit
```

**Diagnosis:**

```bash
# Check memory usage
kubectl top pods -n myapp-production

# Check resource limits
kubectl describe pod -n myapp-production -l app=web-api | grep -A 5 Limits
```

**Fix:**

```bash
# Update resource limits in enclii.yaml
spec:
  runtime:
    resources:
      limits:
        memory: "2Gi"  # Increased from 1Gi

# Redeploy
enclii deploy --env production
```

### Issue 5: Slow Performance

**Symptom:**
Response times 2-3x slower than Railway

**Diagnosis:**

```bash
# Check if database is in same region as K8s cluster
aws rds describe-db-instances --db-instance-identifier myapp-prod --query 'DBInstances[0].AvailabilityZone'

kubectl get nodes -o wide

# Check network latency
enclii exec web-api --env production -- ping -c 5 <db-host>
```

**Fix:**

```bash
# Use RDS in same VPC/region as K8s cluster
# Or use RDS read replicas in each region

# Enable connection pooling
# Update DATABASE_URL to use pgBouncer or configure pooling in app
```

---

## Migration Examples

### Example 1: Simple Node.js API (Express)

**Railway Setup:**
- Service: `web-api` (Express app)
- Database: Railway Postgres
- Domain: `api.myapp.com`

**Migration Steps:**

```bash
# 1. Export database
railway run -- pg_dump $DATABASE_URL | gzip > railway_db.sql.gz

# 2. Create RDS instance
aws rds create-db-instance \
  --db-instance-identifier myapp-prod \
  --db-instance-class db.t3.small \
  --engine postgres \
  --master-username postgres \
  --master-user-password <password> \
  --allocated-storage 20

# 3. Import database
gunzip -c railway_db.sql.gz | psql $RDS_URL

# 4. Create Enclii service
cat > enclii.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web-api
spec:
  build:
    buildpack: auto
  runtime:
    port: 3000
    replicas: 2
    healthCheck:
      path: /health
  secrets:
    - DATABASE_URL
  routes:
    - domain: api.myapp.com
      tlsEnabled: true
EOF

# 5. Create secrets
enclii secret create DATABASE_URL "$RDS_URL" --env production

# 6. Deploy
enclii service create -f enclii.yaml --env production
enclii deploy --env production

# 7. Add domain and verify
enclii domain add api.myapp.com --service web-api --env production
# (Follow DNS verification steps)

# 8. Test
curl https://api.myapp.com/health

# 9. Update DNS to Enclii
# (Update A record)

# 10. Monitor
enclii logs web-api --env production --follow
```

**Time:** ~2 hours

### Example 2: Next.js + Railway Postgres + Redis

**Railway Setup:**
- Service: `web-app` (Next.js SSR)
- Database: Railway Postgres
- Cache: Railway Redis
- Domain: `app.myapp.com`

**Migration Steps:**

```bash
# 1. Provision cloud databases
# PostgreSQL (RDS)
aws rds create-db-instance --db-instance-identifier myapp-prod ...

# Redis (ElastiCache)
aws elasticache create-cache-cluster \
  --cache-cluster-id myapp-prod \
  --cache-node-type cache.t3.small \
  --engine redis \
  --num-cache-nodes 1

# 2. Export and import Postgres
railway run -- pg_dump $DATABASE_URL | gzip > railway_db.sql.gz
gunzip -c railway_db.sql.gz | psql $RDS_URL

# 3. Create Enclii service
cat > enclii.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: web-app
spec:
  build:
    buildpack: auto
  runtime:
    port: 3000
    replicas: 3
    healthCheck:
      path: /api/health
    resources:
      limits:
        memory: "2Gi"
  env:
    - name: NODE_ENV
      value: production
  secrets:
    - DATABASE_URL
    - REDIS_URL
    - NEXTAUTH_SECRET
  routes:
    - domain: app.myapp.com
      tlsEnabled: true
EOF

# 4. Create secrets
enclii secret create DATABASE_URL "$RDS_URL" --env production
enclii secret create REDIS_URL "redis://$ELASTICACHE_HOST:6379" --env production
enclii secret create NEXTAUTH_SECRET "$(openssl rand -base64 32)" --env production

# 5. Deploy
enclii service create -f enclii.yaml --env production
enclii deploy --env production

# 6. Configure domain
enclii domain add app.myapp.com --service web-app --env production
# (Verify DNS)

# 7. Test thoroughly
curl https://app.myapp.com
# Test auth, database queries, Redis caching

# 8. Switch DNS
# Update A record to Enclii

# 9. Monitor
enclii metrics --env production
```

**Time:** ~3 hours

### Example 3: Microservices (Multiple Services)

**Railway Setup:**
- Service: `api-gateway` (Node.js)
- Service: `user-service` (Go)
- Service: `order-service` (Python)
- Database: Railway Postgres (shared)
- Cache: Railway Redis (shared)

**Migration Strategy: Gradual**

```bash
# Week 1: Migrate non-critical service first
# Start with user-service

# 1. Deploy user-service to Enclii
cat > user-service.yaml <<EOF
apiVersion: enclii.dev/v1
kind: Service
metadata:
  name: user-service
spec:
  build:
    buildpack: auto
  runtime:
    port: 8080
    replicas: 2
  secrets:
    - DATABASE_URL  # Still pointing to Railway Postgres
  routes:
    - domain: users-api.myapp.com
      tlsEnabled: true
EOF

enclii service create -f user-service.yaml --env production
enclii secret create DATABASE_URL "$RAILWAY_DATABASE_URL" --env production
enclii deploy --env production

# 2. Update api-gateway to call Enclii user-service
# Update USER_SERVICE_URL to users-api.myapp.com

# Week 2: Migrate order-service
# (Same process)

# Week 3: Migrate api-gateway

# Week 4: Migrate database
# Now all services are on Enclii, migrate database last
```

**Time:** ~4 weeks (gradual)

---

## Post-Migration Optimization

### 1. Enable Auto-Scaling

```yaml
spec:
  runtime:
    autoscaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 20
      targetCPU: 70
      targetMemory: 80
```

### 2. Configure Resource Requests/Limits

Monitor actual usage for 1 week, then adjust:

```bash
# Check actual usage
kubectl top pods -n myapp-production --containers

# Update enclii.yaml based on findings
spec:
  runtime:
    resources:
      requests:
        memory: "256Mi"  # P50 usage
        cpu: "200m"
      limits:
        memory: "1Gi"    # P99 usage + buffer
        cpu: "1000m"
```

### 3. Set Up Monitoring Alerts

```bash
# Create alerts for key metrics
enclii alert create high-error-rate \
  --metric http_requests_total \
  --condition 'rate[5m] > 0.05' \
  --severity critical \
  --notify slack-channel

enclii alert create high-latency \
  --metric http_request_duration_seconds \
  --condition 'p95 > 1.0' \
  --severity warning

enclii alert create low-replica-count \
  --metric deployment_replicas_available \
  --condition '< 2' \
  --severity critical
```

### 4. Configure Backups

```bash
# Set up automated database backups
enclii backup create \
  --database myapp-prod \
  --schedule "0 2 * * *" \
  --retention 30days \
  --storage s3://myapp-backups/

# Test restore
enclii backup restore \
  --backup myapp-prod-2025-11-20 \
  --target myapp-staging
```

---

## Decommission Railway

After 2-4 weeks of successful Enclii operation:

```bash
# 1. Final verification
# Ensure zero traffic on Railway
railway status

# 2. Export final backup (just in case)
railway run -- pg_dump $DATABASE_URL > final_railway_backup_$(date +%Y%m%d).sql

# 3. Download any logs or metrics you want to keep
railway logs --output railway_logs_archive.txt

# 4. Delete Railway services
railway delete --service web-api

# 5. Cancel Railway subscription
# (via Railway dashboard)

# 6. Document migration completion
cat > migration_complete.md <<EOF
# Migration Completed

- **Date**: $(date)
- **Services Migrated**: web-api, user-service, order-service
- **Database**: Railway Postgres → AWS RDS
- **Downtime**: 0 minutes (blue-green)
- **Issues**: None
- **Cost Savings**: $XXX/month

## Metrics Comparison

### Railway (Last 30 days)
- P50 latency: XXXms
- P95 latency: XXXms
- Error rate: X.XX%
- Uptime: XX.XX%

### Enclii (First 30 days)
- P50 latency: XXXms
- P95 latency: XXXms
- Error rate: X.XX%
- Uptime: XX.XX%
EOF
```

---

## Support & Resources

### Documentation
- Enclii Docs: https://docs.enclii.dev
- Railway Migration FAQ: https://docs.enclii.dev/migration/railway
- Enclii CLI Reference: https://docs.enclii.dev/cli

### Community
- Discord: https://discord.gg/enclii
- GitHub Issues: https://github.com/madfam-org/enclii/issues
- Stack Overflow: Tag `enclii`

### Professional Services
For complex migrations or hands-on support:
- Email: support@enclii.dev
- Migration Consulting: https://enclii.dev/services/migration

---

## Conclusion

Migrating from Railway to Enclii provides:

✅ **Production-ready persistent storage** (no data loss)
✅ **Custom domains with automatic HTTPS** (Let's Encrypt)
✅ **Superior compliance and audit trails** (SBOM, signing, provenance)
✅ **Kubernetes-native flexibility** (NetworkPolicies, PVCs, quotas)
✅ **Cost optimization** (pay for actual usage, not per-service pricing)

**Recommended Timeline:**
- **Staging Migration**: 1 week
- **Production Migration**: 2-3 weeks (gradual)
- **Decommission Railway**: 4 weeks post-migration

Good luck with your migration! 🚀
