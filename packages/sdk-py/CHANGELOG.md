# Changelog

All notable changes to the `enclii-sdk` package. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/).

## [0.1.0] - 2026-04-17

### Added

- Initial public release.
- `AsyncEncliiClient` async client with typed resource namespaces:
  `projects`, `services`, `deployments`, `rollback`, `canary`, `logs`,
  `audit`, `webhooks`, `secrets`, `jobs`.
- `EncliiClient` sync wrapper for one-shot scripts.
- Pydantic v2 models tracking the Go SDK's `pkg/types` surface plus
  canary rollouts (P2.7) and outbound lifecycle webhooks (P2.3).
- `enclii_sdk.webhook_verify.verify` — Stripe-compatible HMAC-SHA256
  webhook signature verification with configurable clock tolerance.
- Retries on 429/5xx/transport failures via `tenacity` (exponential
  backoff, configurable ceiling).
- WebSocket log tail via `websockets` with auto-reconnect.
- Deployment lookup by Heroku-style v-number (`deployments.get(svc,
  version="v42")`).
- 100+ test suite covering every resource, auth, retries, error mapping,
  webhook signature verification, and generated-model imports.
- OpenAPI-driven pydantic models at `enclii_sdk.models.generated`,
  regenerated via `make models` / `scripts/generate_models.sh` and
  drift-checked in CI via `scripts/verify_models.sh`.
- `packages/sdk-py/Makefile` with install/test/lint/format/typecheck/
  models/verify-models/build/publish targets.
- `.github/workflows/sdk-py.yml` CI matrix (Python 3.11/3.12/3.13):
  lint + format + tests + OpenAPI drift check + build artefact upload.
