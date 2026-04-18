# @madfam/enclii-sdk — Examples

Four runnable examples that cover the common consumer patterns. Each is a
standalone `tsx` script — install `tsx` or use `ts-node` to run.

| File | What it shows |
|------|---------------|
| `deploy-and-wait.ts` | Trigger a deployment and block until it's running |
| `tail-logs.ts` | Stream live logs with reconnect-on-disconnect (Node-only subpath) |
| `canary-rollout.ts` | Start a canary, poll the rollout state, react to terminal outcomes |
| `webhook-subscription.ts` | Create a lifecycle webhook, handle the one-shot signing secret, verify a signature |

## Running locally

```bash
pnpm add -D tsx
# Point at your control plane (or use the default production URL):
export ENCLII_BASE_URL=https://api.enclii.dev/v1
export ENCLII_TOKEN=<api-token-from-user/tokens>

# Deploy-and-wait
export ENCLII_SERVICE_ID=<uuid>
export ENCLII_RELEASE_ID=<uuid>
tsx examples/deploy-and-wait.ts

# Tail logs
export ENCLII_SERVICE_ID=<uuid>
tsx examples/tail-logs.ts

# Canary rollout
export ENCLII_SERVICE_ID=<uuid>
export ENCLII_DIGEST=sha256:<hex>
tsx examples/canary-rollout.ts

# Webhook subscription
export ENCLII_PROJECT=<slug>
export ENCLII_WEBHOOK_URL=https://hooks.example.com/enclii
tsx examples/webhook-subscription.ts
```

Tokens come from `/user/tokens` (Personal Access Tokens) or an OIDC session.
