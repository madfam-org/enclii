# Redis Sentinel Migration Runbook

> [!IMPORTANT]
> MADFAM-ENCLII-FIRST-LEGACY-RAW v1: This document contains legacy raw infrastructure command examples.
> Routine production operations must use Enclii web, API, or CLI. Treat raw
> `kubectl`, `helm`, SSH, provider CLI/API, `docker exec`, and direct container
> access as platform bootstrap or documented break-glass only, and record any
> missing Enclii adapter gap.


> Last Updated: 2026-04-17
> Owner: Platform Infra
> Related: [P1.3 in 2026-04 Enclii remediation plan](../../../internal-devops/roadmaps/2026-04-enclii-remediation-plan.md)

## What this runbook covers

P1.3 deploys a 3-node Redis Sentinel cluster **in parallel** with the existing
single-instance Redis in the `data` namespace. Nothing is removed. Consumers
cut over service-by-service at their own pace, and can fall back to the
single-instance Service if the new cluster misbehaves.

The two stacks:

| Stack | Service | Address (in-cluster) | Status |
|---|---|---|---|
| **Single-instance (existing)** | `redis` | `redis.data.svc.cluster.local:6379` | Stays live during and after migration. Single point of failure. |
| **Sentinel HA (new, P1.3)** | `redis-sentinel` (discovery) | `redis-sentinel.data.svc.cluster.local:26379` | Sentinel-aware clients connect here. |
| | `redis-master` (fallback) | `redis-master.data.svc.cluster.local:6379` | Clients without Sentinel support. See caveat in [Non-Sentinel clients](#non-sentinel-clients). |
| | `redis-ha-headless` | `redis-ha-0/1/2.redis-ha-headless.data.svc.cluster.local` | Per-pod DNS, used by Sentinel for master discovery. |

The `redis-auth` Secret (key: `redis-password`) is **shared** between the two
stacks. We did NOT rotate the password in this PR. Password rotation is a
separate operation (see `runbooks/secret-rotation.md`).

---

## Consumer inventory

Grep-based inventory from `infra/k8s/base/external-secrets/vault-secrets/`:

| Service | Namespace | Client library (known/assumed) | Sentinel-capable? |
|---|---|---|---|
| dhanam-api / web / admin | `dhanam` | ioredis (Node) | Yes |
| janua | `janua` | ioredis (Node) | Yes |
| enclii (switchyard-api) | `enclii` | go-redis/v9 | Yes — but uses its own `redis.enclii.svc.cluster.local`, NOT migrated in P1.3 |
| karafiel-api | `karafiel` | django-redis (redis-py) | Yes |
| tezca-api | `tezca` | redis-py | Yes |
| forgesight-api | `forgesight` | ioredis (Node) | Yes |
| yantra4d-backend | `yantra4d` | ioredis (Node) | Yes |
| autoswarm-nexus-api | `madfam` | ioredis | Yes |
| pravara-api | `pravara-mes` | go-redis | Yes |

> **Important:** `switchyard-api` uses `redis.enclii.svc.cluster.local:6379` in
> the `enclii` namespace (ENV: `ENCLII_REDIS_HOST`), which is a **different**
> Redis instance. P1.3 does NOT migrate that one. It stays single-instance.
> Rationale: switchyard-api's Redis is a per-service cache, not multi-tenant
> shared state; the blast radius is small.

### Cutover order

Start with the smallest-traffic services so we learn the migration dance before
touching the money path.

1. **forgesight-api** (lowest Redis traffic — project planning cache only)
2. **yantra4d-backend** (3D engine cache — recoverable from source of truth)
3. **pravara-api** (MES cache — short-TTL only)
4. **autoswarm-nexus-api** (agent orchestration queue — tolerates short outage)
5. **tezca-api** (session cache — lose a session = user re-auths, acceptable)
6. **janua** (SSO session store — users may re-auth on cutover)
7. **karafiel-api** (marketplace cache)
8. **dhanam-api** (billing cache — last; highest-value path)

Each cutover is a standalone PR that only changes that repo's `enclii.yaml` or
`REDIS_URL` secret. Monitor for 24h before moving to the next service.

---

## Connection strings

### Sentinel-capable clients (preferred)

**Node.js / ioredis:**

```js
import Redis from 'ioredis';
const redis = new Redis({
  sentinels: [{ host: 'redis-sentinel.data.svc.cluster.local', port: 26379 }],
  name: 'mymaster',
  password: process.env.REDIS_PASSWORD,
  sentinelPassword: process.env.REDIS_PASSWORD,  // same password
  db: 0,
});
```

**Python / redis-py ≥ 4.x:**

```py
from redis.sentinel import Sentinel
sentinel = Sentinel(
    [('redis-sentinel.data.svc.cluster.local', 26379)],
    sentinel_kwargs={'password': os.environ['REDIS_PASSWORD']},
    password=os.environ['REDIS_PASSWORD'],
    socket_timeout=0.5,
)
redis = sentinel.master_for('mymaster', db=0)
```

**django-redis (karafiel):**

```py
CACHES = {
    'default': {
        'BACKEND': 'django_redis.cache.RedisCache',
        'LOCATION': 'redis-sentinel://:PASSWORD@redis-sentinel.data.svc.cluster.local:26379/mymaster/0',
        'OPTIONS': {
            'CLIENT_CLASS': 'django_redis.client.SentinelClient',
            'SENTINEL_KWARGS': {'password': 'PASSWORD'},
        },
    }
}
```

**Go / go-redis v9:**

```go
rdb := redis.NewFailoverClient(&redis.FailoverOptions{
    MasterName:       "mymaster",
    SentinelAddrs:    []string{"redis-sentinel.data.svc.cluster.local:26379"},
    Password:         os.Getenv("REDIS_PASSWORD"),
    SentinelPassword: os.Getenv("REDIS_PASSWORD"),
    DB:               0,
})
```

### Non-Sentinel clients (fallback)

Some legacy clients (or simple URL-based configs) can't speak Sentinel. They
point at `redis-master.data.svc.cluster.local:6379`:

```
REDIS_URL=redis://:PASSWORD@redis-master.data.svc.cluster.local:6379/0
```

**Caveat — known limitation of the `redis-master` Service:**

Kubernetes Services select pods by label, but there is no built-in way to
auto-update the selector to point at "whichever pod is currently master."
Today, `redis-master` selects `app=redis-ha` without filtering by role, so
connections round-robin across all 3 pods. This works for **reads** but a
client doing `SET` on a replica will get `READONLY You can't write against a
read only replica`.

**Options for non-Sentinel clients:**

1. Have the client accept the READONLY error and retry — ioredis/redis-py do
   this automatically if configured for it, but most code doesn't.
2. Upgrade the client library to a Sentinel-capable version (strongly preferred).
3. Keep the client on the **single-instance** `redis.data.svc.cluster.local:6379`
   until we install a proper `redis-failover-operator` (spec.io/redis-operator)
   that rewrites the Service selector on failover. Tracked as follow-up work.

For P1.3, document which consumers cannot use Sentinel and keep them on
single-instance; revisit in a follow-up sprint.

---

## Cutover procedure (per service)

Repeat for each service in the [cutover order](#cutover-order) above.

### 1. Pre-cutover checks

```bash
# Sentinel sees 3 Sentinels + 2 replicas + 1 master
kubectl exec -n data redis-ha-0 -c sentinel -- \
    redis-cli -p 26379 SENTINEL master mymaster | grep -E 'num-other-sentinels|num-slaves'
# Expect: num-other-sentinels = 2, num-slaves = 2

# Verify password works from outside the StatefulSet
kubectl run -n data redis-test --rm -it --restart=Never \
    --image=docker.io/library/redis:7-alpine -- \
    redis-cli -h redis-sentinel.data.svc.cluster.local -p 26379 \
    SENTINEL get-master-addr-by-name mymaster
# Expect: returns [IP, 6379]
```

### 2. Update the service's secret

For services using Vault/ExternalSecrets, update the Vault KV path:

```bash
# Example: karafiel
vault kv patch secret/karafiel \
    REDIS_URL='redis-sentinel://:PASSWORD@redis-sentinel.data.svc.cluster.local:26379/mymaster/0'
```

The ExternalSecret controller reconciles within 1 minute; the consuming
Deployment picks up the change on the next pod restart.

### 3. Roll the Deployment

```bash
enclii deploy karafiel-api --env production --strategy canary --canary-percent 25
# or
kubectl rollout restart -n karafiel deployment/karafiel-api
```

### 4. Validate

```bash
# Tail logs for Redis connection errors
enclii logs karafiel-api -f --level error

# Check the consumer is reaching the HA cluster
kubectl exec -n data redis-ha-0 -c redis -- redis-cli -a "$REDIS_PASSWORD" \
    CLIENT LIST | grep -c 'karafiel'
# Expect: ≥1 connection
```

### 5. Soak for 24h

Keep the consumer on the Sentinel cluster for 24h. If errors spike, see
[Rollback](#rollback).

---

## Rollback

If a consumer can't reach the Sentinel cluster, or observes errors that don't
reproduce against single-instance:

```bash
# Revert the Vault key to the single-instance URL
vault kv patch secret/karafiel \
    REDIS_URL='redis://:PASSWORD@redis.data.svc.cluster.local:6379/0'

# Roll the pod
kubectl rollout restart -n karafiel deployment/karafiel-api
```

Then open a ticket describing what broke — we want to fix the HA side, not
leave the consumer on single-instance indefinitely.

---

## Troubleshooting

### Sentinel quorum failing

Symptom: alert `RedisSentinelDown` firing.

```bash
# Check pod health
kubectl get pods -n data -l app=redis-ha

# Check Sentinel state on each pod
for i in 0 1 2; do
  echo "--- redis-ha-$i ---"
  kubectl exec -n data redis-ha-$i -c sentinel -- \
    redis-cli -p 26379 SENTINEL master mymaster | head -10
done
```

### Master loss recovery

Symptom: alert `RedisMasterDown` firing for > 30s.

Sentinel should failover automatically. If it hasn't:

```bash
# Force failover from any Sentinel
kubectl exec -n data redis-ha-0 -c sentinel -- \
    redis-cli -p 26379 SENTINEL failover mymaster

# Verify new master
kubectl exec -n data redis-ha-0 -c sentinel -- \
    redis-cli -p 26379 SENTINEL get-master-addr-by-name mymaster
```

If forced failover fails, the cluster may be split-brained. Page platform-infra
on-call; recovery may require manual reset-master + resync.

### Replication lag

Symptom: alert `RedisReplicationLag` firing.

```bash
# Inspect replica state
kubectl exec -n data redis-ha-0 -c redis -- redis-cli -a "$REDIS_PASSWORD" INFO replication

# Check replica pod CPU/IO
kubectl top pod -n data -l app=redis-ha
```

Common causes: replica pod OOM, Longhorn PV disk pressure, network saturation
during a large write burst.

---

## Validation — post-deploy

Run the chaos script:

```bash
./scripts/redis-failover-chaos.sh
```

Expected: failover completes in < 20s, master re-elects, exit code 0.
Result appended to `docs/runbooks/redis-failover-log.md`.

## See also

- Chaos validation: [`scripts/redis-failover-chaos.sh`](../../scripts/redis-failover-chaos.sh)
- Chaos log schema: [`docs/runbooks/redis-failover-log.md`](./redis-failover-log.md)
- Manifests: [`infra/k8s/redis-sentinel/`](../../infra/k8s/redis-sentinel/)
- ArgoCD Application: [`infra/argocd/apps/redis-sentinel.yaml`](../../infra/argocd/apps/redis-sentinel.yaml)
- Prometheus rules: [`infra/k8s/production/monitoring/prometheus.yaml`](../../infra/k8s/production/monitoring/prometheus.yaml) (RedisSentinelDown, RedisMasterDown, RedisReplicationLag)
