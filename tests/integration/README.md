# Enclii Integration Tests

Automated integration tests for the Enclii platform, validating critical features against a real Kubernetes cluster.

## Test Suites

### 1. PVC Persistence Tests (`pvc_persistence_test.go`)
Tests data persistence for PostgreSQL and Redis using PersistentVolumeClaims:
- PostgreSQL data survives pod restarts
- Redis data persists across restarts
- PVCs remain bound during rolling updates

### 2. Service Volume Tests (`service_volumes_test.go`)
Tests service deployment with persistent volumes:
- Single volume attachment
- Multiple volume support
- Data persistence verification
- Storage class configuration
- PVC cleanup on service deletion

### 3. Custom Domain Tests (`custom_domain_test.go`)
Tests custom domain management and TLS:
- Ingress creation with custom domains
- cert-manager TLS certificate issuance
- DNS TXT record verification
- Ingress updates on domain changes
- Ingress deletion when domains removed
- HTTPS redirect configuration

### 4. Route Tests (`routes_test.go`)
Tests HTTP path-based routing:
- Single and multiple route creation
- Path type configuration (Prefix, Exact, ImplementationSpecific)
- Route updates reflected in Ingress
- Route deletion handling
- Path priority ordering
- Multiple domains pointing to same service
- Custom port routing

## Prerequisites

### Required
- **Kubernetes cluster**: Kind, Minikube, or cloud cluster
- **kubectl**: Configured with cluster access
- **Go 1.26+**: For running tests
- **cert-manager**: For TLS tests (optional but recommended)
- **nginx-ingress-controller**: For routing tests (optional but recommended)

### Installation

#### Kind Cluster Setup
```bash
# Create Kind cluster with ingress support
cat <<EOF | kind create cluster --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: enclii-test
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: InitConfiguration
        nodeRegistration:
          kubeletExtraArgs:
            node-labels: "ingress-ready=true"
    extraPortMappings:
      - containerPort: 80
        hostPort: 80
        protocol: TCP
      - containerPort: 443
        hostPort: 443
        protocol: TCP
EOF
```

#### Install cert-manager
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.2/cert-manager.yaml

# Wait for cert-manager to be ready
kubectl wait --for=condition=Available --timeout=300s \
  deployment/cert-manager \
  deployment/cert-manager-webhook \
  deployment/cert-manager-cainjector \
  -n cert-manager

# Install ClusterIssuers
kubectl apply -f ../../infra/k8s/base/cert-manager.yaml
```

#### Install nginx-ingress-controller
```bash
# For Kind
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/main/deploy/static/provider/kind/deploy.yaml

# Wait for ingress-nginx to be ready
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=300s
```

#### Install PostgreSQL and Redis
```bash
kubectl apply -f ../../infra/k8s/base/postgres.yaml
kubectl apply -f ../../infra/k8s/base/redis.yaml

# Wait for databases to be ready
kubectl wait --for=condition=ready pod -l app=postgres --timeout=300s
kubectl wait --for=condition=ready pod -l app=redis --timeout=300s
```

## Running Tests

### Using the Test Runner Script

The test runner script provides a convenient way to run all or specific test suites:

```bash
# Run all tests
./run-tests.sh

# Run specific test suite
./run-tests.sh --suite-domains --no-cleanup

# Run with custom timeout
TEST_TIMEOUT=1h ./run-tests.sh

# Show help
./run-tests.sh --help
```

**Environment Variables:**
- `KUBECONFIG`: Path to kubeconfig file (default: `~/.kube/config`)
- `TEST_TIMEOUT`: Test timeout duration (default: `30m`)
- `CLEANUP`: Clean up test namespaces before running (default: `true`)

**Test Suite Flags:**
- `--suite-pvc`: Run PVC persistence tests
- `--suite-volumes`: Run service volume tests
- `--suite-domains`: Run custom domain tests
- `--suite-routes`: Run route tests

### Using Go Test Directly

```bash
# Run all tests
go test -v -timeout 30m ./...

# Run specific test suite
go test -v -timeout 30m -run "^Test.*Persistence" ./pvc_persistence_test.go ./helpers.go

# Run specific test
go test -v -timeout 30m -run "TestPostgreSQLPersistence" ./pvc_persistence_test.go ./helpers.go

# Run with short mode (skips integration tests)
go test -v -short ./...
```

### CI/CD

Integration tests run automatically on:
- **Pull requests** to `main` or `develop` branches
- **Pushes** to `main` or `develop` branches
- **Manual workflow dispatch** via GitHub Actions UI

See `.github/workflows/integration-tests.yml` for CI configuration.

## Test Structure

Each test follows this pattern:

```go
func TestFeatureName(t *testing.T) {
    // Skip in short mode
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    // Create test namespace
    ctx := context.Background()
    namespace := "enclii-test-feature"
    helper, err := NewTestHelper(namespace)
    require.NoError(t, err)

    // Setup namespace
    err = helper.CreateNamespace(ctx)
    require.NoError(t, err)
    defer func() {
        _ = helper.DeleteNamespace(ctx)
    }()

    // Test implementation
    // ...

    // Assertions
    assert.Equal(t, expected, actual)
}
```

## Manual Verification Steps

Some tests require manual steps (marked with `⚠️  Manual step:` in test output):

### Custom Domain Tests
1. Add custom domain via API:
   ```bash
   curl -X POST http://localhost:8080/v1/services/{service_id}/domains \
     -H "Authorization: Bearer $TOKEN" \
     -d '{
       "domain": "api.example.com",
       "environment": "production",
       "tls_enabled": true,
       "tls_issuer": "letsencrypt-staging"
     }'
   ```

2. Verify DNS TXT record:
   ```bash
   dig TXT api.example.com
   ```

3. Verify TLS certificate:
   ```bash
   kubectl get certificate -n enclii-test-custom-domain
   kubectl describe certificate api-gateway-api-example-com-tls
   ```

### Route Tests
1. Add route via API:
   ```bash
   curl -X POST http://localhost:8080/v1/services/{service_id}/routes \
     -H "Authorization: Bearer $TOKEN" \
     -d '{
       "path": "/api/v1",
       "path_type": "Prefix",
       "port": 8080,
       "environment": "production"
     }'
   ```

2. Verify Ingress:
   ```bash
   kubectl get ingress -n enclii-test-routes
   kubectl describe ingress web-app
   ```

## Troubleshooting

### Tests Timing Out

If tests are timing out:
1. Increase timeout: `TEST_TIMEOUT=1h ./run-tests.sh`
2. Check cluster resources: `kubectl top nodes`
3. Review pod logs: `kubectl logs -l test=integration --all-namespaces`

### PVC Not Binding

If PVCs are not binding:
1. Check storage class: `kubectl get storageclass`
2. Check PVC status: `kubectl describe pvc <pvc-name>`
3. Ensure cluster has dynamic provisioner (Kind requires `rancher.io/local-path`)

### Ingress Not Created

If Ingress resources are not created:
1. Verify nginx-ingress is running: `kubectl get pods -n ingress-nginx`
2. Check Ingress logs: `kubectl logs -n ingress-nginx -l app.kubernetes.io/component=controller`
3. Verify custom domain/route was added via API

### TLS Certificate Not Issued

If cert-manager is not issuing certificates:
1. Check cert-manager is running: `kubectl get pods -n cert-manager`
2. Check ClusterIssuer: `kubectl get clusterissuer`
3. Check Certificate resource: `kubectl describe certificate <cert-name>`
4. Review cert-manager logs: `kubectl logs -n cert-manager -l app=cert-manager`

### Test Namespace Cleanup

If test namespaces are not cleaning up:
```bash
# Manual cleanup
kubectl delete namespaces -l test=integration

# Or cleanup individual namespace
kubectl delete namespace enclii-test-custom-domain
```

## Development

### Adding New Tests

1. Create test file: `tests/integration/new_feature_test.go`
2. Use `TestHelper` for Kubernetes operations
3. Follow existing test patterns
4. Add manual verification steps where kubectl exec is required
5. Update this README with new test suite description
6. Update `run-tests.sh` to include new suite

### Test Helpers

The `TestHelper` provides utilities for common Kubernetes operations:

```go
// Wait for resources
helper.WaitForPodReady(ctx, labelSelector, timeout)
helper.WaitForDeploymentReady(ctx, name, timeout)
helper.WaitForPVCBound(ctx, name, timeout)
helper.WaitForIngressCreated(ctx, name, timeout)

// Get resources
helper.GetPod(ctx, name)
helper.GetDeployment(ctx, name)
helper.GetService(ctx, name)
helper.GetIngress(ctx, name)
helper.GetPVC(ctx, name)

// List resources
helper.ListPods(ctx, labelSelector)

// Cleanup
helper.Cleanup(ctx)
helper.DeleteNamespace(ctx)
```

## CI/CD Integration

### GitHub Actions

The `.github/workflows/integration-tests.yml` workflow:
- Creates Kind cluster with ingress support
- Installs cert-manager and nginx-ingress
- Runs all test suites
- Collects logs on failure
- Uploads test artifacts

### Local CI Simulation

To simulate CI locally:
```bash
# Use Kind cluster
kind create cluster --name enclii-test

# Install dependencies
make infra-dev

# Run tests
./run-tests.sh
```

## Test Coverage

Current integration test coverage:

| Feature                    | Coverage | Test File                    |
|----------------------------|----------|------------------------------|
| PostgreSQL Persistence     | ✅        | pvc_persistence_test.go      |
| Redis Persistence          | ✅        | pvc_persistence_test.go      |
| Service Volumes            | ✅        | service_volumes_test.go      |
| Custom Domains             | ✅        | custom_domain_test.go        |
| TLS Certificates           | ✅        | custom_domain_test.go        |
| DNS Verification           | ✅        | custom_domain_test.go        |
| Path-Based Routing         | ✅        | routes_test.go               |
| Multiple Routes            | ✅        | routes_test.go               |
| Route Path Types           | ✅        | routes_test.go               |
| Multiple Domains           | ✅        | routes_test.go               |

## Contributing

When adding new features to Enclii:
1. Write integration tests for new functionality
2. Update existing tests if behavior changes
3. Run tests locally before submitting PR
4. Ensure CI passes before merging

## Resources

- [Kubernetes Testing Best Practices](https://kubernetes.io/docs/concepts/cluster-administration/manage-deployment/)
- [cert-manager Documentation](https://cert-manager.io/docs/)
- [nginx-ingress Documentation](https://kubernetes.github.io/ingress-nginx/)
- [Kind Documentation](https://kind.sigs.k8s.io/)
