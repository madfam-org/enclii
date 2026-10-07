# Skipped tests and known flakes

The inventory of every test that can skip, and why. Last reviewed
2026-10-02, against `main` after #667.

A skip belongs here only if it is **conditional on the environment** (a
database, a cluster, credentials, a platform) or carries a reason that a
reviewer accepted. A skip with no working reason is fixed or deleted instead.
When you add one, add its line here in the same PR.

Find them with:

```bash
grep -rn 't\.Skip' --include='*_test.go' apps packages
grep -rnE '\b(it|test|describe)\.(skip|only)\(|\bxit\(|\bxdescribe\(' \
  --include='*.ts' --include='*.tsx' --include='*.js' apps packages tests | grep -v node_modules
grep -rn 'pytest.mark.skip\|pytest.mark.xfail\|pytest.skip(' --include='*.py' . | grep -v node_modules
```

No `.only`, `xit`, `xdescribe`, `@pytest.mark.skip` or `@pytest.mark.xfail`
exists in the tree.

## Go

| Test | Skips when | Runs in CI |
|---|---|---|
| `internal/jobholds/store_integration_test.go` (`-tags integration`) | `JOB_HOLD_TEST_DATABASE_URL` is unset; execution also requires `LOCAL_DB=yes` and a loopback host | Yes: the `smoke` job runs migration/serialization checks against its disposable Postgres |
| `internal/db/*_integration_test.go` (`-tags integration`) | `TEST_DATABASE_URL` is unset | Yes: the `smoke` job of `ci.yml` runs `go test -tags integration ./internal/db/...` against its Postgres |
| `internal/provisioning/generated_roles_integration_test.go` (`-tags integration`) | `TEST_POSTGRES_ADMIN_URL` is unset | No. Gap: no workflow runs it; run it locally against a disposable Postgres (superuser URL, TCP) |
| `internal/api/handlers_integration_test.go`, `internal/services/auth_integration_test.go` (`-tags integration`) | `TEST_DATABASE_URL` is unset | No. Gap: no workflow runs them; run them locally against a disposable Postgres |
| `internal/reconciler/service_integration_test.go`, `canary_integration_test.go` | `INTEGRATION_USE_REAL_CLUSTER` / `KUBECONFIG` are unset; the service variant always skips without a real cluster, because `k8s.Client` cannot wrap a fake clientset there | No. Manual, against a disposable cluster |
| `internal/api/xc2_tenant_filter_handlers_test.go` `GetService` case | Always | No. `GetService` needs the full project-service wiring; the rule it would check is covered by `TestEnforceActingTeamForProject_*` |
| `internal/logstream/handler_test.go` `TestPumpTail_NoDropsOnHealthyClient` | `TailSendBuffer < 10` (a structural guard; never true today) | Yes |
| `packages/cli/internal/cmd/login_test.go` (two `ENCLII_BROWSER` cases) | On Windows, where `ENCLII_BROWSER` is not used | Yes, on Linux |

Removed in the 2026-10-02 close-out:
- `TestInitiateSignup_EndToEndHappyPath_InMemoryStub` was an empty, always-skipped test. Its path is covered by `internal/signup/service_test.go` (`TestInitiate_Success`).
- `TestExecService_ServiceNotFound` was always skipped ("requires sqlmock setup"). It now runs over sqlmock.

## Playwright

| Spec | Skips when |
|---|---|
| `apps/switchyard-ui/e2e/**` authenticated cases (`sso-login`, `dashboard`, `responsive`, `instant-rollback`) | `TEST_USER_PASSWORD` is unset |
| `apps/switchyard-ui/e2e/auth/sso-login.spec.ts` "Protected Routes" and `e2e/dashboard/dashboard.spec.ts` first block | Always: the redirect needs full client-side hydration, which is too slow for these specs. The redirect logic is unit-tested. Gap: an E2E proof of the unauthenticated redirect |
| `tests/e2e-ecosystem/tests/enclii-*-smoke.spec.ts`, `*-lifecycle.spec.ts` | Their `*_E2E_TOKEN` / `*_E2E_SERVICE_ID` variables are unset (manual proofs against a live environment) |
| `tests/e2e-ecosystem/tests/enclii-signup-smoke.spec.ts` | `SIGNUP_E2E_RUN` is not `1` |
| `tests/e2e-ecosystem/tests/enclii-paywall.spec.ts` pricing case | The deployed landing page has no pricing section |

## Python

| Test | Skips when |
|---|---|
| `tests/scripts/test_arc_pool_health.py`, `test_stuck_runner_watchdog.py` | A required CLI tool is missing, or `date` is BSD rather than GNU (macOS) |
| `infra/synthetic-flow-probe/tests/test_probe.py` manifest case | The journey manifests are not in the checkout |
| `tests/scripts/test_client_slo_rules.py` promtool case | `promtool` is not on PATH and `ENCLII_PROMTOOL` is unset, or the evaluator cannot run (no Docker daemon, image pull failure). The `client-slo-rules-test` job in `ci.yml` sets `ENCLII_PROMTOOL` to the pinned `prom/prometheus` image, so it runs in CI; the structural cases never skip |

`tests/scripts/test_digest_pin_push_retry.py` does not skip on macOS: it shims
`sed` so the GNU `sed -E -i` form the workflows use runs under BSD `sed`.

## Known flakes

None are known. In the last 100 `main` runs (as of 2026-10-02), every failure
was a Trivy image-scan finding or an image build. None was a test that passed
on retry.

The two timing-sensitive Go tests are written to be deterministic:
- `internal/reconciler/pgbouncer_drift_checker_test.go` bounds its wait so it terminates promptly under load;
- `internal/logstream` tail tests are structural.

If a test does flake, name it here with the run id and open an issue; never
retry it silently.
