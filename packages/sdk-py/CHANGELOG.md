# Changelog

All notable changes to the `enclii-sdk` package. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project
adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed

- Regenerated `enclii_sdk.models.generated` from the current OpenAPI spec
  (it had drifted since the spec changes of 2026-09-22 and 2026-09-24). The
  hand-written consumer-facing models exported from `enclii_sdk` are
  unchanged. In the generated reference module:
  - added `ObservationReason`, `LogHistory`, `TemplateListResponse`,
    `OperatorOperationRequest`, `OperatorOperationResponse` and `Step`;
  - `ServiceHealthResponse` gains `healthy_count`, `degraded_count`,
    `unhealthy_count` and `timestamp`, and its per-service item (`Service2`)
    now mirrors the API: `service_id`, `service_name`, `project_slug`,
    `status`, `last_checked`, `deployment_name`, `observation_reason`,
    `pod_count`, `ready_pods`, `uptime`, `response_time_ms` and `error_rate`
    replace `id`, `name`, `health` and `last_check`;
  - the positionally named enums shift: the new service-health `Status5`
    takes that name, so the former `Status5`, `Status6` and `Status7` are now
    `Status6`, `Status7` and `Status8`. Import generated enums by name only
    with this in mind.
- Pinned the `datamodel-code-generator` dev dependency to 0.83.0 so the
  drift check is deterministic.

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
