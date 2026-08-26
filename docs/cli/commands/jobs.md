# enclii jobs

Manage cron and one-off scheduled jobs.

## Synopsis

```bash
enclii jobs <subcommand> [flags]
```

**Aliases:** `job`, `cron`

## Description

The `jobs` command manages cron jobs and one-off jobs for your services. Cron jobs run on a recurring schedule using standard cron expressions. One-off jobs run immediately (or at a specified time) and exit after a single execution. Jobs are reconciled to Kubernetes CronJob and Job resources by the Timetable reconciler (a background loop in the Switchyard API, ~30s interval).

## Execution Context

Jobs run **in the target service's runtime context**, not in a bare container. When the reconciler dispatches a job it resolves the service referenced by `--service-id` and copies from the service's current Kubernetes Deployment:

- the **container image** (the service's currently deployed release),
- the **environment variables** and **secret references** (`env` + `envFrom`),
- the **service account** and **image pull secrets**.

The job's command then runs as `/bin/sh -c "<command>"` inside that context, so commands like `rails db:migrate` see the same `DATABASE_URL` and secrets the service itself runs with.

Two deviations from the default:

- `--image <ref>` (on `run-once`, or `image` on the API) overrides the container image but **still inherits** the service's env/secrets/service account. Use this to run a sibling tool (e.g. `migrate/migrate:v4`) against the service's configuration.
- If the service's Deployment cannot be resolved (service never deployed, or deleted), the job falls back to a bare `busybox:latest` container with **no** environment. The reconciler logs a warning naming the reason. Commands that depend on the service's runtime will not work in this fallback -- deploy the service first.

Cron jobs get the same context resolution, re-evaluated on every reconcile pass: when the service deploys a new release, the CronJob's image and environment follow it.

> **Privilege note:** a job inherits the service's secrets, so creating a job is equivalent in privilege to deploying code to the service. Job creation therefore requires the same `developer` role as deploying.

## One-Off Job Lifecycle

```
pending ──(reconciler dispatches K8s Job)──> running ──> completed (exit code 0)
   │                                            └──────> failed    (exit code 1)
   └── waits until --run-at / run_at, if set
```

- **pending**: recorded in the database, not yet dispatched (or waiting for its `run_at` time).
- **running**: the Kubernetes Job was created in the project's namespace.
- **completed** / **failed**: synced back from the Kubernetes Job outcome, with `ended_at` set. The recorded exit code is the Job-level outcome (`0` = completed, `1` = failed); per-command output and exact process exit codes are visible in the logs.

`--timeout` maps to the Kubernetes Job's `activeDeadlineSeconds`: when exceeded, Kubernetes kills the job and it is recorded as **failed**. One-off jobs do not retry (`backoffLimit: 0`).

### Kubernetes Naming

| Resource | Name | Namespace |
|----------|------|-----------|
| One-off K8s Job | `job-<sanitized-name>-<first-8-of-job-uuid>` | project slug |
| Cron K8s CronJob | `cj-<sanitized-name>` (max 52 chars) | project slug |

One-off job pods carry the label `enclii.dev/one-off-job-id=<job-uuid>`, which is how `jobs logs` locates them. `jobs get <id>` prints the exact K8s Job name and namespace for correlation with `kubectl`.

## Subcommands

### `list`

List all cron and one-off jobs for a project.

**Aliases:** `ls`

```bash
enclii jobs list --project <slug>
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--project`, `-p` | string | | Project slug (required) |

**Output:** a cron job table (ID, NAME, SCHEDULE, SUSPENDED, LAST RUN, NEXT RUN) followed by a "One-off jobs" section (ID, NAME, STATUS, EXIT CODE, CREATED) listing the 50 most recent one-off jobs, newest first.

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
| `--timeout` | int | `3600` | Max execution time in seconds (`activeDeadlineSeconds`) |
| `--retries` | int | `0` | Max retry attempts on failure (`backoffLimit`) |
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

Get detailed information about a cron **or one-off** job.

```bash
enclii jobs get <job-id>
```

Takes the full job UUID as a positional argument. The ID is looked up first as a cron job, then as a one-off job. Cron jobs show schedule, command, timeout, retries, concurrency policy, suspension state and last/next run times. One-off jobs show status, exit code, the K8s Job name and namespace, and created/started/ended timestamps.

---

### `logs`

Fetch the pod logs of a one-off job execution.

```bash
enclii jobs logs <job-id>
```

Logs are read from the job's Kubernetes pod (located by the `enclii.dev/one-off-job-id` label; up to the last 1000 lines / 1 MiB). Two cases print an explanatory message with the job's status instead of logs:

- the job is still **pending** (no pods scheduled yet), or
- the pods were already **cleaned up** (Kubernetes Job TTL) -- the status and exit code in `jobs get` remain the durable record.

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
| `--image` | string | | Container image (default: the service's current deployment image + env) |
| `--timeout` | int | `3600` | Max execution time in seconds (`activeDeadlineSeconds`) |

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

One-off jobs:
ID        NAME        STATUS     EXIT CODE  CREATED
660e8400  db-migrate  completed  0          2 hours ago
771f9511  seed-data   failed     1          1 day ago
```

### Run a One-Off Database Migration

The job runs in the service's deployed image with its environment, so the migration sees the service's real `DATABASE_URL`:

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

The reconciler dispatches pending jobs on its next pass (within ~30s).

### Check the Result of a One-Off Job
```bash
enclii jobs get 660e8400-e29b-41d4-a716-446655440099
```

**Output:**
```
ID:          660e8400-e29b-41d4-a716-446655440099
Name:        db-migrate
Command:     rails db:migrate
Status:      completed
Exit Code:   0
Timeout:     3600s
Service ID:  550e8400-e29b-41d4-a716-446655440000
K8s Job:     job-db-migrate-660e8400
Namespace:   my-api
Created:     2026-03-19T02:00:00Z
Started:     2026-03-19T02:00:21Z
Ended:       2026-03-19T02:01:53Z
```

### Read a One-Off Job's Logs
```bash
enclii jobs logs 660e8400-e29b-41d4-a716-446655440099
```

**Output:**
```
Pod: job-db-migrate-660e8400-x7k2p (status: completed)
== 20260319020000 AddIndexToUsers: migrating ==
-- add_index(:users, :email)
== 20260319020000 AddIndexToUsers: migrated (0.0812s) ==
```

If the pods were already cleaned up:
```
Status: completed
logs no longer available: the job's pods were cleaned up
```

### Run a Tool Image Against the Service's Environment

`--image` overrides the container image while keeping the service's env/secrets:

```bash
enclii jobs run-once \
  --name schema-check \
  --command "migrate -database \"$DATABASE_URL\" -path /migrations status" \
  --image migrate/migrate:v4 \
  --service-id 550e8400-e29b-41d4-a716-446655440000 \
  --project my-api
```

### Create a Nightly Database Backup
```bash
enclii jobs create \
  --name nightly-backup \
  --schedule "0 2 * * *" \
  --command "pg_dump -Fc \"$DATABASE_URL\" > /tmp/db.dump && ./upload-backup.sh /tmp/db.dump" \
  --service-id 550e8400-e29b-41d4-a716-446655440000 \
  --project my-api
```

The cron job runs in the service's image and environment, re-resolved on every reconcile pass -- deploying a new release updates the CronJob to match.

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
