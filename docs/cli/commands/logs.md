# enclii logs

Stream or fetch service logs.

## Synopsis

```bash
enclii logs <service> [flags]
```

## Description

The `logs` command retrieves logs from a running service. Supports real-time streaming, historical log retrieval, and filtering by level, time range, and instance.

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `service` | Yes | Service name |

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `production` | Target environment |
| `--follow`, `-f` | bool | `false` | Stream logs in real-time |
| `--since` | duration | `1h` | Show logs since duration (e.g., `5m`, `2h`, `7d`) |
| `--until` | string | | Show logs until timestamp |
| `--tail`, `-n` | int | `100` | Number of recent lines to show |
| `--level`, `-l` | string | all | Filter by level: `debug`, `info`, `warn`, `error` |
| `--instance` | string | all | Filter by specific instance ID |
| `--output`, `-o` | string | `text` | Output format: `text`, `json` |
| `--timestamps`, `-t` | bool | `true` | Show timestamps |
| `--no-color` | bool | `false` | Disable colored output |

## Examples

### Basic Log Retrieval
```bash
enclii logs api
```

**Output:**
```
2025-01-11T10:30:15Z [INFO]  Server started on port 8080
2025-01-11T10:30:16Z [INFO]  Connected to database
2025-01-11T10:31:02Z [INFO]  GET /api/v1/users 200 45ms
2025-01-11T10:31:15Z [WARN]  Rate limit approaching for client 192.168.1.1
2025-01-11T10:32:00Z [ERROR] Failed to process webhook: timeout
```

### Stream Logs in Real-Time
```bash
enclii logs api -f
# Press Ctrl+C to stop
```

### Filter by Log Level
```bash
enclii logs api --level error --since 24h
```

### View Logs from Specific Instance
```bash
enclii logs api --instance api-7d9f8c-abc12
```

### JSON Output for Processing
```bash
enclii logs api -o json | jq '.level == "error"'
```

**JSON Output Format:**
```json
{
  "timestamp": "2025-01-11T10:32:00Z",
  "level": "ERROR",
  "message": "Failed to process webhook: timeout",
  "service": "api",
  "instance": "api-7d9f8c-abc12",
  "trace_id": "abc123def456"
}
```

### Staging Environment Logs
```bash
enclii logs api --env staging -f
```

### Last 500 Lines
```bash
enclii logs api --tail 500 --since 7d
```

## Log Levels

| Level | Description |
|-------|-------------|
| `debug` | Detailed debugging information |
| `info` | General operational messages |
| `warn` | Warning conditions |
| `error` | Error conditions |

## Streaming (WebSocket)

When using `--follow`, the CLI establishes a WebSocket connection to stream logs in real-time:

```
wss://api.enclii.dev/api/v1/services/{service}/logs/stream
```

The connection automatically reconnects on network interruptions.

## See Also

- [`enclii ps`](./ps.md) - Check service status
- [`enclii deploy`](./deploy.md) - Deploy a service
- [Troubleshooting](../../troubleshooting/index.md)
