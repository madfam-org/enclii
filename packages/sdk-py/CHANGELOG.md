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
- 35+ test suite covering every resource, auth, retries, error mapping,
  and webhook signature verification.
