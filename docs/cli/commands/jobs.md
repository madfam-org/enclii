# enclii jobs

Manage cron and one-off scheduled jobs.

## Synopsis

```bash
enclii jobs <subcommand> [flags]
```

**Aliases:** `job`, `cron`

## Description

The `jobs` command manages cron jobs and one-off jobs for your services. Cron jobs run on a recurring schedule using standard cron expressions. One-off jobs run immediately (or at a specified time) and exit after a single execution. Jobs are reconciled to Kubernetes CronJob and Job resources by the Timetable reconciler.

## Subcommands

### `list`

List all cron jobs for a project.

**Aliases:** `ls`

```bash
enclii jobs list --project <slug>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug (required) |

**Output columns:** ID (truncated), NAME, SCHEDULE, SUSPENDED, LAST RUN (relative), NEXT RUN (date).

---

### `create`

Create a new cron job with a schedule expression.

```bash
enclii jobs create --name <name> --schedule "<cron-expr>" --command "<cmd>" \
  --service-id <uuid> --project <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug (required) |
| `--name`, `-n` | string | | Job name (required) |
| `--schedule`, `-s` | string | | Cron schedule expression (required) |
| `--command`, `-c` | string | | Command to execute (required) |
| `--service-id` | string | | Service ID (required) |
| `--timeout` | int | `3600` | Max execution time in seconds |
| `--retries` | int | `0` | Max retry attempts on failure |
| `--concurrency` | string | `forbid` | Concurrency policy: `allow`, `forbid`, `replace` |

The schedule uses standard 5-field cron syntax:

```
 +-------- minute (0-59)
 | +------ hour (0-23)
 | | +---- day of month (1-31)
 | | | +-- month (1-12)
 | | | | + day of week (0-6, Sun=0)
 * * * * *
```

---

### `get`

Get detailed information about a cron job.

```bash
enclii jobs get <job-id>
```

Takes the full job UUID as a positional argument. Displays all job fields including schedule, command, timeout, retries, concurrency policy, suspension state, and last/next run times.

---

### `delete`

Delete a cron job and all its run history.

**Aliases:** `rm`, `remove`

```bash
enclii jobs delete <job-id> [--force]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--force` | bool | `false` | Skip confirmation prompt |

Without `--force`, the command prompts for confirmation before deleting.

---

### `runs`

List execution history for a cron job.

```bash
enclii jobs runs <job-id>
```

**Output columns:** ID (truncated), STATUS, EXIT CODE, STARTED (timestamp), DURATION.

---

### `run-once`

Run a one-off job that executes immediately and exits.

```bash
enclii jobs run-once --name <name> --command "<cmd>" \
  --service-id <uuid> --project <slug> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug (required) |
| `--name`, `-n` | string | | Job name (required) |
| `--command`, `-c` | string | | Command to execute (required) |
| `--service-id` | string | | Service ID (required) |
| `--timeout` | int | `3600` | Max execution time in seconds |

## Examples

### List All Jobs for a Project
```bash
enclii jobs list --project my-api
```

**Output:**
```
ID        NAME              SCHEDULE      SUSPENDED  LAST RUN       NEXT RUN
a1b2c3d4  nightly-backup    0 2 * * *                3 hours ago    2026-03-20 02:00
e5f6g7h8  hourly-sync       0 * * * *                12 minutes ago 2026-03-19 16:00
i9j0k1l2  weekly-report     0 9 * * 1     yes        3 days ago     -
```

### Create a Nightly Database Backup
```bash
enclii jobs create \
  --name nightly-backup \
  --schedule "0 2 * * *" \
  --command "pg_dump -Fc mydb > /backups/db.dump" \
  --service-id 550e8400-e29b-41d4-a716-446655440000 \
  --project my-api
```

**Output:**
```
Cron job created:
  ID:       550e8400-e29b-41d4-a716-446655440001
  Name:     nightly-backup
  Schedule: 0 2 * * *
  Command:  pg_dump -Fc mydb > /backups/db.dump
  Next Run: 2026-03-20T02:00:00Z
```

### Create a Job with Retry and Concurrency Control
```bash
enclii jobs create \
  --name hourly-sync \
  --schedule "0 * * * *" \
  --command "./sync.sh" \
  --service-id 550e8400-e29b-41d4-a716-446655440000 \
  --project my-api \
  --timeout 300 \
  --retries 2 \
  --concurrency forbid
```

### View Job Details
```bash
enclii jobs get 550e8400-e29b-41d4-a716-446655440001
```

**Output:**
```
ID:          550e8400-e29b-41d4-a716-446655440001
Name:        nightly-backup
Schedule:    0 2 * * *
Command:     pg_dump -Fc mydb > /backups/db.dump
Timeout:     3600s
Retries:     0
Concurrency: forbid
Suspended:   false
Service ID:  550e8400-e29b-41d4-a716-446655440000
Created:     2026-03-15T10:30:00Z
Updated:     2026-03-19T02:00:12Z
Last Run:    2026-03-19T02:00:00Z
Next Run:    2026-03-20T02:00:00Z
```

### View Run History
```bash
enclii jobs runs 550e8400-e29b-41d4-a716-446655440001
```

**Output:**
```
ID        STATUS     EXIT CODE  STARTED              DURATION
f1e2d3c4  completed  0          2026-03-19 02:00:05  1m32s
b5a6c7d8  completed  0          2026-03-18 02:00:03  1m28s
e9f0a1b2  failed     1          2026-03-17 02:00:04  45s
```

### Run a One-Off Database Migration
```bash
enclii jobs run-once \
  --name db-migrate \
  --command "rails db:migrate" \
  --service-id 550e8400-e29b-41d4-a716-446655440000 \
  --project my-api
```

**Output:**
```
One-off job created:
  ID:      660e8400-e29b-41d4-a716-446655440099
  Name:    db-migrate
  Command: rails db:migrate
  Status:  pending
```

### Run a One-Off Job with Custom Timeout
```bash
enclii jobs run-once \
  --name seed-data \
  --command "./seed.sh" \
  --service-id 550e8400-e29b-41d4-a716-446655440000 \
  --project my-api \
  --timeout 600
```

### Delete a Job
```bash
# With confirmation prompt
enclii jobs delete 550e8400-e29b-41d4-a716-446655440001

# Skip confirmation
enclii jobs delete 550e8400-e29b-41d4-a716-446655440001 --force
```

## Concurrency Policies

### forbid (Default)
Skips the new run if the previous run is still active. Prevents overlapping executions.

```bash
enclii jobs create --concurrency forbid ...
```

### allow
Permits concurrent runs of the same job. Use when jobs are idempotent and can safely overlap.

```bash
enclii jobs create --concurrency allow ...
```

### replace
Cancels the currently running instance and starts a new one. Use when only the latest run matters.

```bash
enclii jobs create --concurrency replace ...
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Operation successful |
| `10` | Validation error (invalid schedule, missing flags) |
| `30` | API request failed |
| `50` | Authentication error |

## See Also

- [`enclii deploy`](./deploy.md) - Deploy a service
- [`enclii ps`](./ps.md) - Check service status
- [`enclii logs`](./logs.md) - View service logs
- [Service Spec Reference](../../reference/service-spec.md) - Job configuration in `enclii.yaml`
