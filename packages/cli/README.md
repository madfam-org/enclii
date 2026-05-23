# Enclii CLI

Command-line interface for the Enclii platform.

## Overview

The `enclii` CLI enables developers to:
- Deploy and manage services
- Stream logs and debug issues
- Configure domains and environments
- Manage teams and access
- Operate MADFAM infrastructure and providers through audited `ops` and
  `providers` replacement layers instead of raw `kubectl`, `gh`, Cloudflare,
  Porkbun, or Hetzner tooling

## Installation

### macOS (Homebrew)

```bash
brew install enclii/tap/enclii
```

### Linux

```bash
curl -sSL https://get.enclii.dev | bash
```

### Windows

```powershell
# Using scoop
scoop bucket add enclii https://github.com/madfam-org/scoop-enclii
scoop install enclii
```

### From Source

```bash
git clone https://github.com/madfam-org/enclii.git
cd enclii
make install-cli           # builds + installs to /usr/local/bin/enclii
# or
make build-cli && ./bin/enclii version
```

`install-cli` writes a stripped, version-injected binary (`-ldflags "-s -w -X ..."`) to `CLI_INSTALL_DIR` (default `/usr/local/bin`). Override the destination:

```bash
make install-cli CLI_INSTALL_DIR=$HOME/.local/bin
```

## Quick Start

```bash
# Authenticate
enclii login

# Initialize a service
cd my-app
enclii init

# Deploy
enclii deploy

# View logs
enclii logs my-app -f
```

## Project Structure

```
packages/cli/
├── cmd/enclii/                 # main package (binary entry point)
└── internal/
    ├── client/                 # typed HTTP client for the Switchyard API
    │   ├── api.go              # core endpoints (projects, services, releases…)
    │   ├── api_admin.go        # admin + functions endpoints
    │   ├── api_canary.go       # canary rollout endpoints
    │   └── api_streaming.go    # log/event streaming over WebSocket
    ├── cmd/                    # one file per top-level Cobra command
    │   ├── root.go             # AddCommand wiring + Version/Commit/BuildDate vars
    │   ├── apirequest.go       # shared apiRequest, emitJSON, queryString helpers
    │   ├── httpclient.go       # timeout-bearing http.Client factories
    │   ├── login.go            # OAuth/PKCE flow against Janua
    │   ├── teams.go projects.go tokens.go
    │   ├── audit.go activity.go observe.go integrations.go deployments.go
    │   ├── ops.go providers.go # audited operator/provider replacement layer
    │   ├── admin.go admin_*.go # platform operator subtree (mirrors admin-console)
    │   └── …                   # one file per top-level group
    ├── config/                 # ~/.enclii config + credentials, auto-refresh
    ├── exitcodes/              # typed exit codes for non-zero returns
    ├── helpers/                # cross-cutting helpers
    └── spec/                   # service.yaml / enclii.yaml parser
```

## Commands

See the [CLI Reference](../../docs/cli/README.md) for the canonical, grouped index. The CLI ships ~38 top-level command groups; below is a non-exhaustive map of the most common.

| Command | Description |
|---------|-------------|
| `login` / `logout` / `whoami` | OAuth/PKCE auth via Janua SSO |
| `tokens` | Personal API tokens (CI/CD authentication) |
| `init` / `projects` / `services-sync` / `services-delete` | Project + service lifecycle |
| `deploy` / `rollback` / `releases` / `deployments` / `ps` / `logs` | Deployment operations |
| `secrets` / `domains` / `functions` / `jobs` / `junctions` | Service configuration |
| `teams` / `integrations` | Team management + GitHub integration |
| `observe` / `activity` / `audit` | Metrics, lifecycle feed, audit log (CSV export) |
| `ops` | Audited Kubernetes/Argo/Longhorn/Kyverno/ARC operator workflows |
| `quote-flow` | Enclii-first doctor for Selva -> Yantra4D -> Cotiza -> ForgeSight quote readiness |
| `providers` | Audited GitHub/Cloudflare/Porkbun/Hetzner provider workflows |
| `admin` | Platform operator subtree: `fleet`, `topology`, `clusters`, `drift`, `propagation`, `governance`, `costs`, `vclusters` |
| `vault` | Cluster Vault status (read-only) |
| `local` | Local development environment |
| `version` | Build version + commit (`--json` available) |

Most read subcommands accept `--json`; mutations require `--force` to skip confirmation prompts.
The `ops` and `providers` replacement-layer commands are plan-first: mutating
operations are dry-run by default and require `--apply --reason "..."` once the
server-side adapter is wired. `providers cloudflare dns-apply` is wired for
Cloudflare zones owned by the configured Enclii account: dry-runs load the live
zone/record state, and applies create, update, or no-op DNS records through the
Switchyard API audit path. It intentionally blocks when the apex zone is not
visible to Enclii; registrar nameserver changes still require the Porkbun
provider adapter or another approved Enclii-controlled domain authority path.

## Development

### Prerequisites

- Go 1.25+

### Building

```bash
# Build for the current platform (stripped + version-injected via Makefile)
make build-cli

# Manual build (no version injection)
cd packages/cli && go build -o ../../bin/enclii ./cmd/enclii

# Manual build with version metadata
go build -ldflags "\
  -s -w \
  -X github.com/madfam-org/enclii/packages/cli/internal/cmd.Version=$(git describe --always --dirty) \
  -X github.com/madfam-org/enclii/packages/cli/internal/cmd.Commit=$(git rev-parse --short HEAD) \
  -X github.com/madfam-org/enclii/packages/cli/internal/cmd.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o bin/enclii ./cmd/enclii
```

### Testing

```bash
# Run tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...

# Integration tests
go test -tags=integration ./...
```

### Linting

```bash
# Run golangci-lint
golangci-lint run
```

## Configuration

OAuth credentials are stored at `~/.enclii/credentials.json` (mode `0600`):

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "refresh_token": "rt_...",
  "token_type": "Bearer",
  "expires_at": "2026-08-01T00:00:00Z",
  "issuer": "https://auth.madfam.io"
}
```

Tokens auto-refresh on the next CLI invocation when within 60 seconds of expiry, provided a refresh token is present (see `internal/config/config.go`).

Other defaults come from environment variables (`ENCLII_API_ENDPOINT`, `ENCLII_API_TOKEN`, `ENCLII_OIDC_ISSUER`, `ENCLII_OIDC_CLIENT_ID`, `ENCLII_LOG_LEVEL`, `ENCLII_PROJECT`) or the global flags (`--api-endpoint`, `--api-token`, `--log-level`).

When `ENCLII_ENVIRONMENT=development` and `ENCLII_API_ENDPOINT` is unset, the CLI targets `http://localhost:4200` (aligned with `switchyard-ui`). See `docs/contracts/DEV_ENV_ALIGNMENT.md`.

Timetable (`enclii jobs`) and Junction (`enclii junctions`) commands use the same `apiRequest` / `apiRequestResponse` helpers as billing and admin commands.

## Authentication Flow

The CLI uses OAuth 2.0 with PKCE:

```
1. CLI generates code_verifier and code_challenge
2. Opens browser to auth.madfam.io/authorize
3. User authenticates with Janua SSO
4. Janua redirects to localhost callback
5. CLI exchanges code for tokens
6. Tokens stored in config file
```

## API Client

`internal/client/api.go` (and `api_admin.go`, `api_canary.go`, `api_streaming.go`) hold the typed `APIClient` covering projects, services, releases, deployments, env-vars, functions, and admin/onboarding endpoints. Commands without a typed method use the shared `apiRequest` helper in `internal/cmd/apirequest.go`, which wraps `httpClient()` (30s timeout) and adds auth + standard headers. Legacy per-command HTTP wrappers (`billingRequest`, `jobsRequest`, `junctionsRequest`) delegate to these helpers.

## Output Formatting

The CLI prefers domain-appropriate human output (tables via `text/tabwriter`) and exposes `--json` on most read subcommands for stable machine-readable output. There is no global `-o` flag — `--json` is opt-in per command. Shared JSON emission goes through `emitJSON` in `internal/cmd/apirequest.go`.

## WebSocket Log Streaming

`enclii logs -f` uses the streaming endpoints in `internal/client/api_streaming.go`. The implementation is built on `gorilla/websocket` and shares the same auth conventions as the typed `APIClient`.

## Release Process

1. Update version in `version.go`
2. Create git tag: `git tag v0.5.0`
3. Push tag: `git push origin v0.5.0`
4. GitHub Actions builds and releases

## Related Components

- **[Switchyard API](../../apps/switchyard-api/)** - Backend API
- **[Switchyard UI](../../apps/switchyard-ui/)** - Web dashboard
- **[SDK Go](../sdk-go/)** - Go SDK

## License

Apache 2.0 - See [LICENSE](../../LICENSE)
