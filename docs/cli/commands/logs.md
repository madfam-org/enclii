# enclii logs

Show or stream service logs.

## Synopsis

```bash
enclii logs [service] [flags]
```

## Description

The `logs` command retrieves logs for a service in an environment. Without `--follow` it prints the most recent lines once (preceded by the service's latest deployment id and version, when available). With `--follow` it opens a WebSocket stream and prints lines as they arrive, prefixed with a per-pod colour-coded `pod/container` label.

If `service` is omitted, the CLI reads the service name (and project) from `service.yaml` in the current directory, or from the file passed with `--file`.

Without `--follow`, the API returns the recent lines as plain text with no per-line timestamps, so `--timestamps` is rejected in that mode instead of being silently ignored.

Without `--follow`, `--since` is applied by the server: the CLI resolves the duration to a start time and sends it as `since` to `GET /v1/services/{service_id}/logs/history`, which returns only lines newer than that, still capped at `--lines`. See [Server version for `--since`](#server-version-for---since).

The output is plain text. There is no `--output`/`-o` flag and no JSON output mode, and there are no `--level`, `--instance`, `--until`, `--tail`, or `--no-color` flags; pipe through `grep` to filter.

## Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `service` | No | Service name. Defaults to `metadata.name` from the service spec file. |

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--env`, `-e` | string | `dev` | Environment to show logs for |
| `--follow`, `-f` | bool | `false` | Stream logs in real time over a WebSocket |
| `--lines`, `-n` | int | `100` | Number of recent lines to show |
| `--since` | string | | Show logs since a duration ago. Go duration syntax: `5m`, `1h`, `24h` (no `d` unit; use `168h` for 7 days). The server caps the window at 30 days (`720h`). |
| `--timestamps` | bool | `false` | Prefix each streamed line with its `HH:MM:SS` timestamp. Requires `--follow`; without it the command fails with exit code `10`. |
| `--file`, `-F` | string | `service.yaml` | Path to the service spec file used when `service` is omitted |

## Examples

### Recent logs

```bash
enclii logs api
```

### Stream logs in real time

```bash
enclii logs api -f
# Press Ctrl+C to stop
```

### Errors from the last 24 hours

```bash
enclii logs api --since 24h -n 1000 | grep -i error
```

### Production logs, streamed with timestamps

```bash
enclii logs api --env prod -f --timestamps
```

### Staging logs for the service in the current directory

```bash
enclii logs --env staging
```

## Server version for `--since`

The one-shot history endpoint only honours `since` on a switchyard-api that includes [#622](https://github.com/madfam-org/enclii/pull/622). An older server ignores the parameter and returns the last `--lines` lines whatever the window, with no error. Against such a server, `--since 24h` without `--follow` can show lines older than 24 hours.

The server accepts `since` as an RFC3339 timestamp, which is what the CLI sends, or as a positive Go duration such as `24h` for direct API callers. It rejects anything else, and timestamps in the future, with HTTP `400`. The response shape does not change.

## Streaming (WebSocket)

With `--follow`, the CLI connects to `/v1/services/{service_id}/logs/stream` on the configured API endpoint, using `wss://` for an `https://` endpoint and `ws://` for `http://`. The stream does not reconnect on its own: if it drops, the CLI prints "Log stream ended" and exits, and you re-run the command. If the stream cannot be opened, the CLI suggests re-running without `--follow`.

## See Also

- [`enclii ps`](./ps.md) - Check service status
- [`enclii deploy`](./deploy.md) - Deploy a service
- [`enclii functions logs`](./functions.md) - Logs for a serverless function
- [`enclii local logs`](./local.md) - Logs for the local development environment
- [Troubleshooting](../../troubleshooting/index.md)
