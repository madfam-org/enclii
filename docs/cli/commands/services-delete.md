# enclii services-delete

Delete a service from a project.

## Synopsis

```bash
enclii services-delete [flags]
```

## Description

The `services-delete` command permanently removes a service and all of its associated resources from a project. This includes deployments, releases, environment variables, custom domains, and routes.

This operation is irreversible. By default, the command requires you to type `DELETE` at an interactive confirmation prompt before proceeding. Use the `--force` flag to bypass the prompt in CI/CD pipelines or automated scripts.

You can identify the target service in one of two ways:

- **By project and name** -- provide both `--project` and `--name`.
- **By UUID** -- provide `--id` directly if you already know the service identifier.

When using `--project` and `--name`, the CLI looks up the service via the Switchyard API and resolves the UUID automatically.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project` | string | | Project slug containing the service |
| `--name` | string | | Name of the service to delete |
| `--id` | string | | UUID of the service to delete (alternative to `--project` + `--name`) |
| `--force` | bool | `false` | Skip the interactive confirmation prompt |

Either `--id` or both `--project` and `--name` must be provided.

## Examples

### Delete a Service by Name

```bash
enclii services-delete --project enclii --name janua-api
```

**Output:**
```
Connecting to API...
Connected to switchyard-api (version 0.1.0)

Looking up service 'janua-api' in project 'enclii'...
Found service: janua-api (ID: 12345678-1234-1234-1234-123456789abc)

WARNING: This will permanently delete the service and all associated resources:
  - All deployments
  - All releases
  - All environment variables
  - All custom domains
  - All routes

Service ID: 12345678-1234-1234-1234-123456789abc
Service Name: janua-api

Type 'DELETE' to confirm: DELETE

Deleting service 12345678-1234-1234-1234-123456789abc...
Service deleted successfully.
```

### Delete a Service by UUID

```bash
enclii services-delete --id 12345678-1234-1234-1234-123456789abc
```

### Force Delete Without Confirmation

```bash
enclii services-delete --project enclii --name janua-api --force
```

**Output:**
```
Connecting to API...
Connected to switchyard-api (version 0.1.0)

Looking up service 'janua-api' in project 'enclii'...
Found service: janua-api (ID: 12345678-1234-1234-1234-123456789abc)

Deleting service 12345678-1234-1234-1234-123456789abc...
Service deleted successfully.
```

### Cancelled Deletion

If you type anything other than `DELETE` at the confirmation prompt, the operation is safely aborted:

```bash
enclii services-delete --project enclii --name janua-api
# ...
# Type 'DELETE' to confirm: no
# Deletion cancelled.
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Service deleted successfully, or deletion cancelled by user |
| `10` | Validation error (missing required flags) |
| `30` | Deletion failed (API error, service not found) |
| `50` | Authentication error (invalid or missing API token) |

## See Also

- [`enclii ps`](./ps.md) - List services and their status
- [`enclii deploy`](./deploy.md) - Deploy a service
- [`enclii rollback`](./rollback.md) - Revert a deployment
- [`enclii onboard`](./onboard.md) - Onboard a new project and its services
