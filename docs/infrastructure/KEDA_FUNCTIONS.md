# KEDA Installation for Enclii Functions

This document covers the installation and configuration of KEDA (Kubernetes Event-Driven Autoscaling) for Enclii's serverless functions feature with scale-to-zero support.

## Overview

Enclii Functions uses KEDA HTTP Add-on to provide:
- **Scale-to-Zero**: Functions with no traffic scale down to 0 pods
- **Automatic Scaling**: Scales based on HTTP request rate
- **Cold Start Handling**: KEDA interceptor queues requests during scale-up
- **Cost Efficiency**: No compute costs when functions are idle

## Architecture

```
                              Internet
                                  │
                                  ▼
                        ┌──────────────────┐
                        │ Cloudflare Edge  │
                        └────────┬─────────┘
                                 │
                        ┌────────▼─────────┐
                        │ cloudflared      │
                        └────────┬─────────┘
                                 │
                        *.fn.enclii.dev
                                 │
                                 ▼
                    ┌────────────────────────┐
                    │ KEDA HTTP Interceptor  │
                    │    (keda namespace)    │
                    └────────────┬───────────┘
                                 │
            ┌────────────────────┼────────────────────┐
            ▼                    ▼                    ▼
    ┌───────────────┐    ┌───────────────┐    ┌───────────────┐
    │ fn-hello      │    │ fn-process    │    │ fn-webhook    │
    │ 0-N replicas  │    │ 0-N replicas  │    │ 0-N replicas  │
    │(fn-<project>) │    │(fn-<project>) │    │(fn-<project>) │
    └───────────────┘    └───────────────┘    └───────────────┘
```

## Prerequisites

- Kubernetes cluster (k3s/k8s) with kubectl access
- Helm 3.x installed
- Cloudflare Tunnel configured (for production)

## Installation

### 1. Add KEDA Helm Repository

```bash
helm repo add kedacore https://kedacore.github.io/charts
helm repo update
```

### 2. Install KEDA Core

```bash
# Create namespace
kubectl create namespace keda

# Install KEDA
helm install keda kedacore/keda \
  --namespace keda \
  --set resources.operator.limits.memory=256Mi \
  --set resources.operator.limits.cpu=200m \
  --set resources.operator.requests.memory=128Mi \
  --set resources.operator.requests.cpu=50m \
  --set resources.metricServer.limits.memory=128Mi \
  --set resources.metricServer.limits.cpu=100m \
  --set resources.metricServer.requests.memory=64Mi \
  --set resources.metricServer.requests.cpu=25m
```

### 3. Install KEDA HTTP Add-on

The HTTP Add-on provides the interceptor that handles scale-to-zero for HTTP workloads:

```bash
helm install http-add-on kedacore/keda-add-ons-http \
  --namespace keda \
  --set interceptor.replicas=2 \
  --set interceptor.resources.limits.memory=128Mi \
  --set interceptor.resources.limits.cpu=100m \
  --set interceptor.resources.requests.memory=64Mi \
  --set interceptor.resources.requests.cpu=25m \
  --set scaler.resources.limits.memory=128Mi \
  --set scaler.resources.limits.cpu=100m \
  --set scaler.resources.requests.memory=64Mi \
  --set scaler.resources.requests.cpu=25m
```

### 4. Verify Installation

```bash
# Check KEDA pods are running
kubectl get pods -n keda

# Expected output:
# NAME                                                  READY   STATUS    RESTARTS   AGE
# keda-operator-xxxxx                                   1/1     Running   0          5m
# keda-operator-metrics-apiserver-xxxxx                 1/1     Running   0          5m
# keda-add-ons-http-controller-manager-xxxxx            1/1     Running   0          3m
# keda-add-ons-http-interceptor-xxxxx                   1/1     Running   0          3m
# keda-add-ons-http-interceptor-xxxxx                   1/1     Running   0          3m
# keda-add-ons-http-external-scaler-xxxxx               1/1     Running   0          3m

# Check CRDs are installed
kubectl get crd | grep keda
# Expected: scaledobjects.keda.sh, httpscaledobjects.http.keda.sh, etc.
```

## Configuration

### Cloudflare Tunnel

The Cloudflare Tunnel must route `*.fn.enclii.dev` to the KEDA HTTP interceptor:

```yaml
# Already configured in infra/k8s/production/cloudflared-unified.yaml
ingress:
  - hostname: "*.fn.enclii.dev"
    service: http://keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local:8080
    originRequest:
      connectTimeout: 30s
      keepAliveTimeout: 90s
      noHappyEyeballs: true
```

### DNS Setup

Add a wildcard DNS record in Cloudflare:
- **Type**: CNAME
- **Name**: `*.fn`
- **Target**: Your tunnel ID (e.g., `<tunnel-id>.cfargotunnel.com`)
- **Proxy status**: Proxied (orange cloud)

## How It Works

### HTTPScaledObject

Each function creates an HTTPScaledObject that tells KEDA how to scale:

```yaml
apiVersion: http.keda.sh/v1alpha1
kind: HTTPScaledObject
metadata:
  name: fn-hello
  namespace: fn-my-project
spec:
  hosts:
    - hello.fn.enclii.dev
  scaleTargetRef:
    name: fn-hello              # Deployment name
    kind: Deployment
    apiVersion: apps/v1
  replicas:
    min: 0                       # Scale to zero
    max: 10                      # Maximum replicas
  scalingMetric:
    requestRate:
      targetValue: 100           # Requests per second per replica
  scaledownPeriod: 300           # 5 minutes before scale to zero
```

### Scale-to-Zero Flow

1. **Idle State**: Function has 0 replicas, KEDA interceptor is active
2. **Request Arrives**: Interceptor receives request, queues it
3. **Scale Up**: KEDA detects pending request, scales deployment 0 → 1
4. **Cold Start**: Pod starts, passes readiness probe (~2-5s depending on runtime)
5. **Request Forwarded**: Interceptor forwards queued request to running pod
6. **Warm State**: Subsequent requests route directly to pods
7. **Scale Down**: After 5 minutes of no traffic, KEDA scales 1 → 0

### Cold Start Times (Target)

| Runtime | Cold Start Target | Strategy |
|---------|-------------------|----------|
| Go      | <500ms            | Static binary, distroless image |
| Rust    | <500ms            | Static binary, musl libc |
| Node    | <2s               | Alpine image, tree-shaking |
| Python  | <3s               | Slim image, compiled bytecode |

## Troubleshooting

### Functions Not Scaling

1. Check HTTPScaledObject status:
```bash
kubectl get httpscaledobjects -A
kubectl describe httpscaledobject fn-hello -n fn-my-project
```

2. Check KEDA operator logs:
```bash
kubectl logs -n keda -l app=keda-operator -f
```

3. Check external scaler logs:
```bash
kubectl logs -n keda -l app.kubernetes.io/component=external-scaler -f
```

### Cold Start Too Slow

1. Check pod startup time:
```bash
kubectl get events -n fn-my-project --sort-by='.lastTimestamp'
```

2. Verify image pull policy:
```bash
kubectl get deploy fn-hello -n fn-my-project -o yaml | grep imagePullPolicy
# Should be: IfNotPresent (not Always)
```

3. Consider setting `minReplicas: 1` for latency-sensitive functions

### Interceptor Not Routing

1. Verify interceptor is running:
```bash
kubectl get pods -n keda -l app.kubernetes.io/component=interceptor
```

2. Check interceptor logs:
```bash
kubectl logs -n keda -l app.kubernetes.io/component=interceptor -f
```

3. Test internal routing:
```bash
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v http://keda-add-ons-http-interceptor-proxy.keda.svc.cluster.local:8080 \
  -H "Host: hello.fn.enclii.dev"
```

## Monitoring

### KEDA Metrics

KEDA exposes Prometheus metrics:

```bash
# Get metrics endpoint
kubectl get svc -n keda keda-operator-metrics-apiserver

# Scrape metrics
curl http://<metrics-svc>:8080/metrics
```

Key metrics:
- `keda_scaled_object_status`: Current replica count per ScaledObject
- `keda_trigger_totals`: Trigger activation count
- `keda_scaled_object_errors`: Scaling errors

### Function Invocation Metrics

The Enclii API tracks function metrics in the `function_invocations` table:
- Invocation count
- Duration (ms)
- Cold start detection
- Status codes

## Resource Usage

### KEDA Footprint

| Component | Memory | CPU |
|-----------|--------|-----|
| KEDA Operator | 128-256Mi | 50-200m |
| Metrics Server | 64-128Mi | 25-100m |
| HTTP Interceptor (x2) | 64-128Mi | 25-100m |
| External Scaler | 64-128Mi | 25-100m |
| **Total** | ~500Mi | ~300m |

### Per-Function Overhead

- HTTPScaledObject CR: ~1KB etcd storage
- No additional pods when scaled to zero
- Function pods only consume resources when active

## Upgrading KEDA

```bash
# Update helm repos
helm repo update

# Upgrade KEDA core
helm upgrade keda kedacore/keda --namespace keda

# Upgrade HTTP Add-on
helm upgrade http-add-on kedacore/keda-add-ons-http --namespace keda

# Verify upgrade
kubectl get pods -n keda
```

## Uninstalling

```bash
# Remove HTTP Add-on first
helm uninstall http-add-on --namespace keda

# Remove KEDA core
helm uninstall keda --namespace keda

# Optional: Remove CRDs
kubectl delete crd scaledobjects.keda.sh
kubectl delete crd httpscaledobjects.http.keda.sh
kubectl delete crd scaledjobs.keda.sh
kubectl delete crd triggerauthentications.keda.sh
kubectl delete crd clustertriggerauthentications.keda.sh

# Remove namespace
kubectl delete namespace keda
```

## References

- [KEDA Documentation](https://keda.sh/docs/)
- [KEDA HTTP Add-on](https://github.com/kedacore/http-add-on)
- [HTTPScaledObject Spec](https://github.com/kedacore/http-add-on#readme)
- [Enclii Functions Plan](/docs/architecture/FUNCTIONS_PLAN.md)
