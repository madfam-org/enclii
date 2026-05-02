# enclii webhooks

Manage outbound lifecycle webhook subscriptions.

## Synopsis

```bash
enclii webhooks <subcommand> [flags]
```

**Aliases:** `webhook`

## Description

The `webhooks` subtree manages customer-configurable HTTPS subscriptions that receive **signed** lifecycle events (deploy succeeded/failed, rollback, scale, and similar).

Subscribers verify each request using the `X-Enclii-Signature` header, which follows the **Stripe-compatible** format:

```
X-Enclii-Signature: t=<unix_ts>,v1=<hmac_sha256_hex>
```

Compute `v1` as `HMAC_SHA256(secret, t + "." + raw_body)`. **Signing secrets are shown exactly once** — at create or rotate time — and are never returned by `show` or `list`. Store them in your subscriber's secret manager immediately.

## Subcommands

### `list`

List outbound webhook subscriptions for a project.

```bash
enclii webhooks list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |

### `create`

Create a new webhook subscription.

```bash
enclii webhooks create [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug |
| `--url`, `-u` | string | | Webhook URL (must be `https://`) |
| `--events`, `-e` | string | (all) | Comma-separated event types |
| `--name`, `-n` | string | URL host | Friendly name |

The signing secret is printed to stdout exactly once on success.

### `show`

Show details of a webhook subscription (does **not** include the signing secret).

```bash
enclii webhooks show <sub_id>
```

### `rotate`

Generate a new signing secret. The new secret is printed once and immediately becomes the authoritative secret for all subsequent deliveries.

```bash
enclii webhooks rotate <sub_id>
```

### `delete`

Delete a webhook subscription (soft-delete).

```bash
enclii webhooks delete <sub_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

### `test`

Enqueue a synthetic `test.ping` event so you can verify signature validation end-to-end. The response includes the delivery id.

```bash
enclii webhooks test <sub_id>
```

### `deliveries`

List recent deliveries for a subscription.

```bash
enclii webhooks deliveries <sub_id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit` | int | `20` | Max deliveries to return |

## Examples

### List subscriptions for a project

```bash
enclii webhooks list --project my-api
```

### Create a subscription for deploy events

```bash
enclii webhooks create \
  --project my-api \
  --url https://hooks.example.com/enclii \
  --events deploy.succeeded,deploy.failed \
  --name "Slack via hooks.example.com"
```

**Output:**
```
Created webhook sub_a1b2c3d4
Signing secret (shown once): whsec_•••••
Update your receiver to verify X-Enclii-Signature with this secret.
```

### Send a synthetic test event

```bash
enclii webhooks test sub_a1b2c3d4
```

### Inspect recent deliveries

```bash
enclii webhooks deliveries sub_a1b2c3d4 --limit 50
```

### Rotate the signing secret

```bash
enclii webhooks rotate sub_a1b2c3d4
```

**Important**: the new secret is authoritative the moment this returns. Update your receiver to accept **both** old and new secrets briefly, switch traffic, then drop the old secret. Otherwise your receiver will reject the next delivery.

### Delete a subscription

```bash
enclii webhooks delete sub_a1b2c3d4 --force
```

## Notes

- URLs must be `https://`. Plain HTTP subscriptions are rejected.
- Soft-deleted subscriptions stop receiving events immediately; their delivery history is retained for audit purposes.
- The signing secret is HMAC-SHA256. There is no way to recover a lost secret — rotate to issue a new one.
- The `t` timestamp in `X-Enclii-Signature` is Unix seconds. Reject events with a `t` more than 5 minutes off your clock to prevent replay.

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing URL, invalid events list, non-https URL) |
| `50` | Authentication error |

## See Also

- [`enclii activity`](./activity.md) - The lifecycle event stream that drives webhook deliveries
- [`enclii integrations`](./integrations.md) - Built-in third-party integrations (GitHub)
- [`enclii audit`](./audit.md) - Audit log of subscription create/rotate/delete
