# Enclii Python SDK examples

Runnable examples. Each script reads `ENCLII_TOKEN` from the environment
and defaults to the production API (`https://api.enclii.dev`). Set
`ENCLII_API_URL` to override for staging / local development.

```bash
export ENCLII_TOKEN="enclii_..."
python examples/deploy_and_wait.py svc_123 abc123def
```

| Script                     | Purpose                                            |
|----------------------------|----------------------------------------------------|
| `deploy_and_wait.py`       | Trigger a build + deploy, wait for RUNNING state   |
| `tail_logs.py`             | Stream error-level logs from a service             |
| `canary_rollout.py`        | Start a canary rollout and poll until terminal     |
| `webhook_subscription.py`  | Register a webhook + print the signing secret      |
