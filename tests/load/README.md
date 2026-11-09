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
| `health.js` (default) | Two-minute public liveness smoke, up to 5 VUs | No |
| `health.js` (explicit diagnostic) | Deep dependency health load, 16 minutes / up to 100 VUs | No |
| `api.js` | Authenticated API load test | Yes |
| `stress.js` | High-load stress test (1000 RPS) | Optional |

## Usage

### Quick Smoke Test
```bash
# Dependency-free /health/public; create the summary artifact directory first
mkdir -p results
k6 run tests/load/health.js

# With custom settings
k6 run --env ENCLII_BASE_URL=http://localhost:8080 tests/load/health.js
```

The smoke checks HTTP 200 and the actual public body (`ok: true`,
`service: switchyard-api`). It proves anonymous liveness at low load. It does
not prove database/cache/Kubernetes readiness, authenticated API performance,
or sustained platform capacity. HTTP/body failures and latency failures are
reported separately. `health_slo_breaches` retains the original combined
failure-rate gate: fewer than 0.1% of requests may fail availability or the
per-request 200 ms limit. Overlapping failures count once. The existing
duration thresholds remain enforced.

### Deep Health Diagnostic (Explicit Approval)

`/health` checks live database, cache and Kubernetes dependencies on every
request. Its former 16-minute/100-VU smoke workload is retained as a named
diagnostic. Run it only after reviewing the target and load window:

```bash
mkdir -p results
k6 run --env HEALTH_PROFILE=deep-diagnostic \
  --env ENCLII_ALLOW_DEEP_HEALTH_DIAGNOSTIC=true \
  --env ENCLII_BASE_URL=http://localhost:8080 tests/load/health.js
```

In the Load Tests workflow, select `deep-health` and explicitly check
`allow_deep_health_diagnostic`. The job refuses without that acknowledgement;
the diagnostic never runs from a schedule or pull request. This opt-in does
not change the intentional scheduled authenticated API load test or the
existing manual stress selection.

### Full Load Test
```bash
# Authenticated API test
k6 run --env ENCLII_TOKEN=$ENCLII_TOKEN tests/load/api.js
```

`api_errors` reports availability alone. `api_slo_breaches` preserves the
original combined gate: fewer than 1% of project-list requests may fail
availability or the per-request 500 ms limit. Disjoint availability and latency
failures contribute to the same rate; a request that fails both counts once.
The p95/p99 duration gates also remain enforced.

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

```

The scripts use explicit scenario stages. `DURATION` and `TARGET_RPS` do not
override those stages; review the selected profile before a live run.

## Test Results

Run from the repository root. Results are saved to `results/`, the directory
uploaded by the workflow:
- `health-summary.json` - Health test metrics
- `api-summary.json` - API test metrics
- `stress-summary.json` - Stress test metrics

Create the results directory:
```bash
mkdir -p results
```

All scripts explicitly request p99 summary statistics. Empty or partial runs
report missing observations as `null` and retain the machine summary; a
formatting error must not hide the original k6 threshold failure. Reported
throughput is the generator's observed rate, not a capacity certification.

The scheduled run at source `2a6a7586` sent 83,377 deep-health requests: every
response was HTTP 200 with healthy JSON, while latency increased under load
(p95 about 1.04 seconds). That finding remains a separate capacity investigation.
This harness correction does not establish that the latency bottleneck is fixed.

## Offline Contract Checks

```bash
node --experimental-vm-modules --test tests/load/harness.test.cjs
```

These fixtures evaluate the real scripts with synthetic k6 adapters and no
network. They cover the public/deep contracts, explicit diagnostic opt-in,
slow healthy versus unavailable responses, the original combined SLO rate
(including mixed disjoint failures and overlap), unchanged duration thresholds,
missing p99/empty/failed summaries and workflow event selection. The workflow
runs these checks for pull requests touching the harness; live test jobs stay
disabled for pull requests. Actual k6 execution, hosted smoke acceptance and
controlled load telemetry remain separate runtime checks.

## SLO Targets

| Metric | Target | Test Type |
|--------|--------|-----------|
| P95 Response Time | < 500ms | Load |
| P99 Response Time | < 1000ms | Load |
| Error Rate | < 1% | Load |
| Availability-or-latency breach rate | < 1% (500 ms per request) | Load |
| Availability-or-latency breach rate | < 0.1% (200 ms per request) | Health |
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
