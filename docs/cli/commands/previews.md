# enclii previews

Manage PR-based preview environments for a service.

## Synopsis

```bash
enclii previews <subcommand> [flags]
```

**Aliases:** `preview`

## Description

Preview environments are ephemeral deployments created for pull requests. Each open PR typically receives a unique URL (for example `https://pr-42-my-api.preview.enclii.app`) after the GitHub webhook triggers a build and deploy.

The UI shows previews on the service **Previews** tab. These CLI commands mirror the same API for scripting and CI.

Service context comes from `service.yaml` in the current directory unless you pass `--service`.

## Subcommands

### `previews list`

List preview environments for a service.

**Aliases:** `ls`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name (uses `service.yaml` if not specified) |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |
| `--pr` | int | `0` | Filter to a single PR number |
| `--all`, `-a` | bool | `false` | Include branch and commit columns |

### `previews get`

Get details for one preview by ID, or by PR number with service context.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--service`, `-s` | string | | Service name |
| `--file`, `-f` | string | `service.yaml` | Path to service spec file |
| `--pr` | int | `0` | Look up preview by PR number instead of ID |

### `previews close`

Close a preview (stop resources; mark as closed). Usually automatic when the PR is merged or closed.

**Aliases:** `stop`

### `previews wake`

Wake a sleeping preview (scale deployment back up after auto-sleep).

### `previews delete`

Permanently delete a closed or failed preview record.

**Aliases:** `rm`, `remove`

## Examples

```bash
# List previews for the service in service.yaml
enclii previews list

# List previews for a named service
enclii previews list --service my-api

# Inspect PR #42
enclii previews get --pr 42 --service my-api

# Get by preview UUID
enclii previews get 00000000-0000-4000-8000-000000000001

# Wake a sleeping preview
enclii previews wake 00000000-0000-4000-8000-000000000001

# Close an active preview manually
enclii previews close 00000000-0000-4000-8000-000000000001
```

## Automatic creation (GitHub)

Previews are created when:

1. The service is linked to a GitHub repository (`git_repo` on the service record).
2. A repository webhook delivers `pull_request` events to Enclii (see [`webhooks`](./webhooks.md) for outbound lifecycle hooks; inbound GitHub app/webhook is configured in the Enclii UI under project integrations).

Manual creation is available via `POST /v1/previews` (API) for staging tests — see [COMMERCIAL_GA_STAGING_PROOF.md](../../production/COMMERCIAL_GA_STAGING_PROOF.md).

## Related

- Service UI: **Services → Previews** tab
- API: `GET /v1/services/:id/previews`, `POST /v1/previews/:id/close`, `POST /v1/previews/:id/wake`
- Staging proof: [`COMMERCIAL_GA_STAGING_PROOF.md`](../../production/COMMERCIAL_GA_STAGING_PROOF.md)
