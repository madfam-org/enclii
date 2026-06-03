# Enclii Load Testing

Performance and stress testing for the Enclii PaaS API using [k6](https://k6.io).

## Prerequisites

1. Install k6:
   ```bash
   # macOS
   brew install k6

   # Linux
   sudo gpg -k
   sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
   echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
   sudo apt-get update && sudo apt-get install k6
   ```

2. Get an API token (for authenticated tests):
   ```bash
   enclii login
   export ENCLII_TOKEN=$(cat ~/.enclii/token)
   ```

## Test Scripts

| Script | Description | Auth Required |
|--------|-------------|---------------|
| `health.js` | Health endpoint baseline | No |
| `api.js` | Authenticated API load test | Yes |
| `stress.js` | High-load stress test (1000 RPS) | Optional |

## Usage

### Quick Smoke Test
```bash
# Test health endpoint
k6 run tests/load/health.js

# With custom settings
k6 run --env ENCLII_BASE_URL=http://localhost:8080 tests/load/health.js
```

### Full Load Test
```bash
# Authenticated API test
k6 run --env ENCLII_TOKEN=$ENCLII_TOKEN tests/load/api.js
```

### Stress Test (1000 RPS target)
```bash
# Run stress test - WARNING: High load!
k6 run --env ENCLII_TOKEN=$ENCLII_TOKEN tests/load/stress.js

# Unauthenticated (health only)
k6 run tests/load/stress.js
```

### Custom Configuration
```bash
# Override base URL
k6 run --env ENCLII_BASE_URL=https://staging-api.enclii.dev tests/load/api.js

# Custom duration
k6 run --env DURATION=5m tests/load/health.js
```

## Test Results

Results are saved to `tests/load/results/`:
- `health-summary.json` - Health test metrics
- `api-summary.json` - API test metrics
- `stress-summary.json` - Stress test metrics

Create the results directory:
```bash
mkdir -p tests/load/results
```

## SLO Targets

| Metric | Target | Test Type |
|--------|--------|-----------|
| P95 Response Time | < 500ms | Load |
| P99 Response Time | < 1000ms | Load |
| Error Rate | < 1% | Load |
| P95 Response Time | < 1000ms | Stress |
| Error Rate | < 5% | Stress |
| Throughput | 1000 RPS | Stress |

## Interpreting Results

### Load Test (api.js)
- **PASS**: System handles normal load within SLO targets
- **FAIL**: Consider optimizing slow endpoints or scaling resources

### Stress Test (stress.js)
- **Breaking Point**: When P99 > 2s or error rate > 5%
- **Capacity Planning**: Actual RPS achieved vs target (1000 RPS)

## CI/CD Integration

Add to GitHub Actions:
```yaml
- name: Run Load Tests
  run: |
    k6 run --env ENCLII_TOKEN=${{ secrets.ENCLII_TOKEN }} tests/load/api.js
```

## Troubleshooting

### "Missing authentication token"
```bash
export ENCLII_TOKEN=$(cat ~/.enclii/token)
# or
enclii login  # Re-authenticate if token expired
```

### Connection refused
```bash
# Check API is running
curl https://api.enclii.dev/health
```

### Rate limiting
The API has rate limiting. For high-load tests, coordinate with the platform team.
