# Kyverno Policy Engine

Kubernetes-native policy management for Enclii cluster security.

## Installation

### 1. Add Helm Repository

```bash
helm repo add kyverno https://kyverno.github.io/kyverno/
helm repo update
```

### 2. Install Kyverno

```bash
kubectl create namespace kyverno

# Production (HA with 3 replicas)
helm upgrade --install kyverno kyverno/kyverno \
  -n kyverno \
  --set replicaCount=3 \
  --set admissionController.replicas=3 \
  --set backgroundController.replicas=1 \
  --set cleanupController.replicas=1 \
  --set reportsController.replicas=1 \
  --wait --timeout 5m

# Single-node (development/staging)
helm upgrade --install kyverno kyverno/kyverno \
  -n kyverno \
  --set replicaCount=1 \
  --set admissionController.replicas=1 \
  --wait --timeout 5m
```

### 3. Verify Installation

```bash
kubectl get pods -n kyverno
kubectl get clusterpolicies
```

### 4. Apply Policies

```bash
# Apply all Enclii policies
kubectl apply -f policies/
```

## Policies

### Security Policies

| Policy | Mode | Description |
|--------|------|-------------|
| `disallow-privileged-containers` | Enforce | Blocks privileged containers |
| `require-run-as-nonroot` | Enforce | Blocks containers that run as root |
| `disallow-host-namespaces` | Enforce | Blocks hostNetwork, hostPID, hostIPC |
| `restrict-capabilities` | Enforce | Blocks dangerous capabilities |

### Best Practice Policies

| Policy | Mode | Description |
|--------|------|-------------|
| `require-resource-limits` | Audit | Warns if CPU/memory limits missing |
| `require-labels` | Audit | Warns if required labels missing |
| `require-probes` | Audit | Warns if health probes missing |
| `disallow-latest-tag` | Audit | Warns on `:latest` image tags |

### Image Security Policies

| Policy | Mode | Description |
|--------|------|-------------|
| `require-image-signature` | Audit | Warns on unsigned images |
| `restrict-image-registries` | Audit | Warns on non-approved registries |

## Policy Modes

- **Enforce**: Blocks non-compliant resources from being created
- **Audit**: Allows resources but generates policy reports

## Troubleshooting

### View Policy Reports

```bash
# Cluster-wide reports
kubectl get clusterpolicyreport

# Namespace reports
kubectl get policyreport -A

# Detailed report
kubectl describe clusterpolicyreport
```

### Check Admission Controller Logs

```bash
kubectl logs -n kyverno -l app.kubernetes.io/component=admission-controller -f
```

### Policy Not Applying

```bash
# Check policy status
kubectl get clusterpolicy <name> -o yaml

# Look for validation errors in status
```

## Exemptions

To exempt specific resources from policies, add these annotations:

```yaml
metadata:
  annotations:
    policies.kyverno.io/exclude: "true"
```

Or create a PolicyException resource (Kyverno 1.9+).
