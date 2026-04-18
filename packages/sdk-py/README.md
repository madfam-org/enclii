# Enclii Python SDK

Official Python SDK for the [Enclii](https://enclii.dev) Platform API.

Enclii is MADFAM's open-source DevOps platform. This SDK is the
framework-agnostic way to talk to it from Python — Django, FastAPI,
Celery workers, CI/CD scripts, whatever.

```bash
pip install enclii-sdk
```

## Requirements

* Python 3.11 or newer
* An Enclii API token (get one from the dashboard or via `enclii tokens create`)

## Quickstart

```python
import asyncio
import os
from enclii_sdk import AsyncEncliiClient

async def main():
    async with AsyncEncliiClient(
        base_url="https://api.enclii.dev",
        token=os.environ["ENCLII_TOKEN"],
    ) as enclii:
        # List projects
        projects = await enclii.projects.list()
        for p in projects:
            print(p.slug, p.name)

        # Deploy a release
        deploy = await enclii.services.deploy(
            "svc_abc123",
            release_id="rel_xyz",
            environment_name="production",
        )
        # Wait for it to roll out
        deploy = await enclii.deployments.wait_for_running(str(deploy.id))
        print("deployed", deploy.version_label)

asyncio.run(main())
```

### Sync wrapper for scripts

For one-shot scripts (CI hooks, cron jobs) where async overhead isn't
worth it, use `EncliiClient`:

```python
from enclii_sdk import EncliiClient

client = EncliiClient(token=os.environ["ENCLII_TOKEN"])
projects = client._run("projects.list")  # runs one-shot in asyncio.run
```

Long-running services should use `AsyncEncliiClient` directly — it reuses
a single HTTP connection pool.

## Authentication

Three options, in precedence order:

1. Explicit `token="enclii_..."` argument.
2. Async `token_provider=` callable for refresh-aware flows:

   ```python
   from janua_sdk import refresh_token  # example

   async def provider() -> str:
       return await refresh_token(...)  # returns fresh JWT

   client = AsyncEncliiClient(token_provider=provider)
   ```

3. `ENCLII_TOKEN` environment variable.

## Resources

| Namespace          | Operations |
|--------------------|------------|
| `enclii.projects`  | `list`, `get`, `create`, `delete` |
| `enclii.services`  | `list`, `get`, `create`, `update`, `delete`, `build`, `deploy`, `list_releases` |
| `enclii.deployments` | `list`, `list_all`, `get` (by id or v-number), `latest`, `wait_for_running` |
| `enclii.rollback`  | `rollback_deployment`, `instant_rollback` |
| `enclii.canary`    | `start`, `get`, `list`, `promote`, `rollback` |
| `enclii.logs`      | `query` (REST), `tail` (WebSocket async iterator) |
| `enclii.audit`     | `list` (consolidated), `legacy_activity` |
| `enclii.webhooks`  | `create`, `get`, `list`, `update`, `delete`, `rotate_secret`, `test`, `list_deliveries`, `redeliver`, `list_event_types` |
| `enclii.secrets`   | `list`, `get`, `create`, `update`, `delete`, `reveal` |
| `enclii.jobs`      | `create_cron`, `list_cron`, `get_cron`, `delete_cron`, `list_cron_runs`, `create_one_off` |

## Examples

### Deploy and wait

```python
release = await enclii.services.build("svc_123", git_sha="abc")
deployment = await enclii.services.deploy(
    "svc_123",
    release_id=str(release.id),
    environment_name="production",
)
final = await enclii.deployments.wait_for_running(str(deployment.id))
assert final.version_label  # e.g. "v42"
```

### Tail logs

```python
from enclii_sdk import LogLevel

async for entry in enclii.logs.tail("svc_123", level=LogLevel.ERROR):
    print(entry.timestamp.isoformat(), entry.pod, entry.message)
```

### Canary rollout

```python
rollout = await enclii.canary.start(
    "svc_123",
    digest="sha256:abc123",
    percentage=20,
    validation_window_minutes=10,
    change_ticket_url="https://github.com/madfam-org/api/pull/42",
)
while rollout.state.is_active():
    await asyncio.sleep(15)
    rollout = await enclii.canary.get("svc_123", str(rollout.id))
    print(rollout.state)
```

### Webhook subscription

```python
resp = await enclii.webhooks.create(
    project_slug="demo",
    name="Slack #deploys",
    url="https://hooks.slack.com/services/T00/B00/XXX",
    events=["deploy.succeeded", "deploy.failed", "rollback.succeeded"],
)
# resp.signing_secret is shown exactly once — store it now.
os.environ["SLACK_WEBHOOK_SECRET"] = resp.signing_secret
```

### Verify incoming webhook payloads (FastAPI example)

```python
from fastapi import FastAPI, Request, Response
from enclii_sdk.webhook_verify import verify, WebhookSignatureError

app = FastAPI()

@app.post("/webhooks/enclii")
async def receive(request: Request) -> Response:
    body = await request.body()
    sig = request.headers.get("X-Enclii-Signature", "")
    try:
        verify(body, sig, secret=os.environ["ENCLII_WEBHOOK_SECRET"])
    except WebhookSignatureError:
        return Response(status_code=401)
    # body is trusted — parse and dispatch
    ...
```

## Error handling

All API failures raise a subclass of `EncliiError`:

```python
from enclii_sdk import (
    AuthError, ConflictError, NotFoundError, PermissionError,
    RateLimitError, ServerError, ValidationError, EncliiError,
)

try:
    await enclii.projects.get("does-not-exist")
except NotFoundError as e:
    print(e.message, e.request_id)
except RateLimitError as e:
    await asyncio.sleep(e.retry_after_seconds or 5)
except EncliiError as e:
    print(e.status_code, e.details, e.hint)
```

5xx and 429 responses and transport-level errors are automatically
retried with exponential backoff (up to `max_retries=3` by default).
Tune or disable via the `max_retries` kwarg.

## Configuration

```python
AsyncEncliiClient(
    base_url="https://api.enclii.dev",  # or ENCLII_API_URL
    token="enclii_...",                  # or ENCLII_TOKEN
    timeout=30.0,                        # seconds
    max_retries=3,                       # retries on 429/5xx/network errors
    user_agent="my-app/1.0",
    http_client=httpx.AsyncClient(...),  # bring your own for custom pool
)
```

## Development

We use [uv](https://docs.astral.sh/uv/) for environment + build management and
a small Makefile for the common targets.

```bash
# Install package + dev deps into a local venv
make install          # ≡ uv sync --extra dev

# Run the test suite (100+ tests, no network, MockTransport-based)
make test

# Lint + format
make lint
make format
make typecheck

# Regenerate pydantic models from docs/api/openapi.yaml
make models           # writes src/enclii_sdk/models/generated.py
make verify-models    # CI: fails if generated.py has drifted from the spec

# Build sdist + wheel into ./dist
make build

# Publish to PyPI (operator step, requires PYPI_TOKEN env)
PYPI_TOKEN=pypi-... make publish
```

### Models: hand-written vs generated

The public model surface (`Project`, `Deployment`, `CanaryRollout`, `LogEntry`,
etc.) is **hand-maintained** in `src/enclii_sdk/models/` to track the Go SDK's
`pkg/types` package. Resource classes use these hand-written models.

An OpenAPI-generated mirror lives at `src/enclii_sdk/models/generated.py`,
produced from `docs/api/openapi.yaml` via
[`datamodel-code-generator`](https://docs.pydantic.dev/latest/integrations/datamodel_code_generator/).
It's checked in as a reference for consumers who want to work directly against
the raw spec shape, and CI enforces it stays in sync via
`scripts/verify_models.sh` (invoked by `make verify-models`).

## Publishing (operator)

The package isn't on PyPI yet. An operator with a PyPI API token can ship it with:

```bash
cd packages/sdk-py
uv build                                 # produces dist/enclii_sdk-0.1.0-*.whl + .tar.gz
uv publish --token "$PYPI_TOKEN"         # one-shot upload to PyPI

# or, using the Makefile:
PYPI_TOKEN=pypi-... make publish
```

For test uploads first:

```bash
uv publish --publish-url https://test.pypi.org/legacy/ --token "$TEST_PYPI_TOKEN"
```

## Versioning

This SDK follows semver. The major version tracks the Enclii API
contract — pin conservatively:

```
enclii-sdk>=0.1.0,<0.2.0
```

## License

Apache 2.0 — see [LICENSE](../../LICENSE).
