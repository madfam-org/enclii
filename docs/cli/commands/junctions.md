# enclii junctions

Manage routing rules and ingress configuration.

## Synopsis

```bash
enclii junctions <subcommand> [flags]
```

**Aliases:** `junction`, `routes`

## Description

The `junctions` command manages routing rules, custom domains, and ingress configuration for your services. Junctions define how external traffic reaches your services through domain mappings, path-based routing, TLS termination, and protocol selection.

When you add a junction, the control plane provisions a Cloudflare tunnel route and, if applicable, a cert-manager TLS certificate for the domain. Deleting a junction removes the route and associated certificates.

## Subcommands

### `list`

List all routing rules for a project.

**Aliases:** `ls`

```bash
enclii junctions list --project <project-slug>
enclii junctions list --project <project-slug> --json
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug (required) |
| `--json` | bool | `false` | Emit full machine-readable junction records |

### `add`

Add a routing rule that maps a domain to a service.

```bash
enclii junctions add <domain> --service-id <uuid> --project <project-slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug (required) |
| `--service-id` | string | | Target service UUID (required) |
| `--path` | string | `/` | Path prefix for routing |
| `--protocol` | string | `https` | Protocol: `http`, `https`, `grpc` |

### `get`

Get detailed information about a junction, including TLS configuration.

```bash
enclii junctions get <junction-id>
```

No additional flags.

### `delete`

Delete a junction. This removes the domain routing and any associated TLS certificates.

**Aliases:** `rm`, `remove`

```bash
enclii junctions delete <junction-id> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

## Examples

### List All Junctions for a Project

```bash
enclii junctions list --project my-api
```

**Output:**
```
ID                                    DOMAIN              PATH   PROTOCOL  TLS                CREATED
00000000-0000-0000-0000-000000000001  api.example.com     /      https     letsencrypt-prod   2026-03-15
00000000-0000-0000-0000-000000000002  app.example.com     /app   https     letsencrypt-prod   2026-03-16
00000000-0000-0000-0000-000000000003  grpc.example.com    /      grpc      letsencrypt-prod   2026-03-17
```

Use the full `ID` value with `junctions get` and `junctions delete`; truncated
UUID prefixes are not accepted by the API.

### Add a Custom Domain Route

```bash
enclii junctions add api.example.com \
  --service-id 8f14e45f-ceea-367f-a27f-c3b9a1a2e3d4 \
  --project my-api
```

**Output:**
```
Junction created:
  ID:       9a1b2c3d-4e5f-6789-abcd-ef0123456789
  Domain:   api.example.com
  Path:     /
  Protocol: https
  TLS:      letsencrypt-prod
```

### Add a Path-Based Route

```bash
enclii junctions add app.example.com \
  --service-id 8f14e45f-ceea-367f-a27f-c3b9a1a2e3d4 \
  --project my-api \
  --path /api/v2 \
  --protocol https
```

### Add a gRPC Route

```bash
enclii junctions add grpc.example.com \
  --service-id 2b3c4d5e-6f78-9012-3456-789abcdef012 \
  --project my-api \
  --protocol grpc
```

### Get Junction Details

```bash
enclii junctions get 9a1b2c3d-4e5f-6789-abcd-ef0123456789
```

**Output:**
```
ID:        9a1b2c3d-4e5f-6789-abcd-ef0123456789
Domain:    api.example.com
Path:      /
Protocol:  https
Service:   8f14e45f-ceea-367f-a27f-c3b9a1a2e3d4
Project:   c7d8e9f0-1234-5678-9abc-def012345678
Created:   2026-03-15T10:30:00Z
Updated:   2026-03-15T10:30:00Z

TLS Configuration:
  Enabled:        true
  Issuer:         letsencrypt-prod
  Min Version:    1.2
  Force Redirect: true
```

### Delete a Junction

```bash
enclii junctions delete 9a1b2c3d-4e5f-6789-abcd-ef0123456789
```

**Output:**
```
Are you sure you want to delete junction '9a1b2c3d-4e5f-6789-abcd-ef0123456789'? This removes domain routing. [y/N]: y
Junction '9a1b2c3d-4e5f-6789-abcd-ef0123456789' deleted.
```

### Delete Without Confirmation

```bash
enclii junctions delete 9a1b2c3d-4e5f-6789-abcd-ef0123456789 --force
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (missing required flags, invalid domain) |
| `30` | API request failed (junction not found, permission denied) |
| `50` | Authentication error (missing or invalid token) |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy a service to an environment
- [`enclii domains`](./domains.md) - Manage custom domains
- [`enclii ps`](./ps.md) - Check service status
- [Service Spec Reference](../../reference/service-spec.md) - Domain and routing configuration in `enclii.yaml`
