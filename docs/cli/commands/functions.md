# enclii functions

Manage serverless functions with scale-to-zero capabilities.

## Synopsis

```bash
enclii functions <subcommand> [flags]
```

**Aliases:** `fn`, `func`

## Description

The `functions` command manages serverless functions that automatically scale based on demand, including scaling to zero when idle. Functions are lightweight, event-driven compute units deployed from a local `functions/` directory.

Supported runtimes: Go, Python, Node.js, Rust.

## Subcommands

### `list`

List all functions for a project or all accessible functions.

**Aliases:** `ls`

```bash
enclii functions list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Filter by project slug |

**Output columns:** NAME, RUNTIME, STATUS, INVOCATIONS, AVG MS, LAST INVOKED

---

### `deploy`

Deploy a serverless function from the `functions/` directory in the current working directory.

```bash
enclii functions deploy [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | **(required)** | Project slug |
| `--name`, `-n` | string | directory name | Function name |
| `--runtime`, `-r` | string | auto-detected | Runtime: `go`, `python`, `node`, `rust` |

The runtime is auto-detected based on files in the `functions/` directory:

| File | Detected Runtime |
|------|------------------|
| `go.mod` or `main.go` | Go |
| `requirements.txt` or `handler.py` | Python |
| `package.json` or `handler.js` | Node.js |
| `Cargo.toml` | Rust |

Default handler entry points per runtime:

| Runtime | Default Handler |
|---------|-----------------|
| Go | `main.Handler` |
| Python | `handler.main` |
| Node.js | `handler.main` |
| Rust | `handler` |

---

### `logs`

View logs for a function.

```bash
enclii functions logs <function-name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--follow`, `-f` | bool | `false` | Follow log output |
| `--lines`, `-n` | int | `50` | Number of lines to show |

---

### `invoke`

Invoke a function with optional JSON data.

```bash
enclii functions invoke <function-name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--data`, `-d` | string | | JSON data to send to the function |
| `--async` | bool | `false` | Invoke asynchronously (do not wait for response) |

The response includes the HTTP status code, round-trip duration, whether the invocation triggered a cold start, and the response body (pretty-printed when valid JSON).

---

### `info`

Show detailed information about a function, including configuration, metrics, and timestamps.

```bash
enclii functions info <function-name>
```

No additional flags. Displays: name, ID, status, runtime, handler, memory, timeout, replica bounds, endpoint, invocation count, average duration, active replicas, and creation/update/deploy timestamps.

---

### `delete`

Delete a function and all its resources.

**Aliases:** `rm`, `remove`

```bash
enclii functions delete <function-name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

Without `--force`, the command prompts for confirmation before deleting.

## Examples

### List All Functions

```bash
enclii functions list
```

**Output:**
```
NAME            RUNTIME  STATUS  INVOCATIONS  AVG MS  LAST INVOKED
process-image   python   Ready   4,821        312ms   3 minutes ago
validate-input  node     Ready   12,093       18ms    just now
resize          go       Ready   892          45ms    1 hour ago
```

### List Functions for a Project

```bash
enclii functions list --project acme-api
```

### Deploy a Function

```bash
enclii functions deploy --project acme-api
```

**Output:**
```
Deploying function 'acme_api' (node runtime) to project 'acme-api'...
Function created: fn_8a2b3c4d
Status: Pending
Endpoint: https://acme_api.fn.enclii.dev (pending deployment)
```

### Deploy with Explicit Name and Runtime

```bash
enclii functions deploy --project acme-api --name hello --runtime go
```

### Invoke a Function

```bash
enclii functions invoke hello --data '{"name":"world"}'
```

**Output:**
```
Status: 200
Duration: 47ms
Response:
{
  "greeting": "Hello, world!"
}
```

### Invoke with Cold Start

```bash
enclii functions invoke resize --data '{"url":"https://example.com/image.png","width":800}'
```

**Output:**
```
Status: 200
Duration: 1.2s
Cold Start: yes
Response:
{
  "resized_url": "https://cdn.acme.com/image-800.png"
}
```

### Invoke Asynchronously

```bash
enclii functions invoke batch-process --data '{"items":[1,2,3]}' --async
```

### View Function Logs

```bash
enclii functions logs hello --lines 20
```

### Follow Function Logs

```bash
enclii functions logs hello --follow
```

### Show Function Details

```bash
enclii functions info hello
```

**Output:**
```
Name:          hello
ID:            fn_8a2b3c4d
Status:        Ready
Runtime:       go
Handler:       main.Handler
Memory:        128Mi
Timeout:       30s
Min Replicas:  0
Max Replicas:  10
Endpoint:      https://hello.fn.enclii.dev

Metrics:
  Invocations: 892
  Avg Duration: 45.30ms
  Active Replicas: 0
  Last Invoked: 2026-03-19T10:42:00Z

Timestamps:
  Created:  2026-03-15T08:00:00Z
  Updated:  2026-03-19T10:42:00Z
  Deployed: 2026-03-15T08:01:12Z
```

### Delete a Function

```bash
enclii functions delete hello
```

**Output:**
```
Are you sure you want to delete function 'hello'? [y/N]: y
Function 'hello' deleted.
```

### Force Delete (Skip Confirmation)

```bash
enclii functions delete hello --force
```

## Function Lifecycle

1. **Deploy** -- CLI detects runtime, creates function via the Switchyard API
2. **Build** -- Container image built with the detected runtime base image
3. **Ready** -- Function is deployed and accepting invocations
4. **Scale-to-zero** -- After the cooldown period (default 5 minutes) with no traffic, replicas scale to 0
5. **Cold start** -- First invocation after scale-to-zero triggers a new replica

### Function Status Values

| Status | Description |
|--------|-------------|
| `Pending` | Function created, build not yet started |
| `Building` | Container image is being built |
| `Deploying` | Image built, deploying to the cluster |
| `Ready` | Running and accepting invocations |
| `Failed` | Build or deployment failed |
| `Deleting` | Deletion in progress |

## Configuration Defaults

| Setting | Default |
|---------|---------|
| Memory | 128Mi |
| CPU | 100m |
| Timeout | 30s |
| Min Replicas | 0 (scale-to-zero) |
| Max Replicas | 10 |
| Cooldown Period | 300s (5 minutes) |
| Concurrency Target | 100 requests/replica |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Command successful |
| `10` | Validation error (missing `functions/` directory, undetected runtime, invalid config) |
| `30` | API operation failed (create, invoke, delete) |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy a full service
- [`enclii logs`](./logs.md) - View service logs
- [`enclii ps`](./ps.md) - Check service status
- [Service Spec Reference](../../reference/service-spec.md) - `functions:` section in `enclii.yaml`
