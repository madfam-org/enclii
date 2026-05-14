# Timetable and provider remediation

Last updated: 2026-05-14

## Why this exists

ForgeSight market ingestion and Phynd DNS recovery must be handled through
Enclii, not direct production container access. The 2026-05-14 audit found two
platform blockers:

- `enclii jobs list --project forgesight` returned HTTP 500.
- `enclii jobs run-once --project forgesight ...` returned HTTP 500.
- `enclii providers porkbun domains phynd.app --json` returned
  `adapter_unconfigured`.
- `enclii providers cloudflare dns phynd.app --json` found no Cloudflare zone.

## Root cause fixed in code

The Timetable repositories and API handlers use normalized tables:

- `cron_jobs`
- `cron_job_runs`
- `one_off_jobs`

Migration 020 only added `services.jobs`; it did not create those tables. This
causes production `jobs list` and `jobs run-once` to fail once the project lookup
succeeds.

Migration 027 now creates the missing normalized tables idempotently.

## Deployment order

1. Deploy Switchyard API with migration 027.
2. Confirm `enclii jobs list --project forgesight` returns an empty list or job
   rows, not HTTP 500.
3. Trigger the ForgeSight one-off ingestion job through Enclii.
4. Confirm `FDM - PLA / CDMX / 30d` benchmarks become non-empty before allowing
   client-ready Tablaco quote generation.
5. Configure the Porkbun provider adapter or transfer `phynd.app` DNS authority
   into Cloudflare/Enclii.
6. Only after `phynd.app` is live should `crm.madfam.io` be treated as the
   MADFAM tenant slice.

## Acceptance checks

```bash
enclii jobs list --project forgesight
enclii jobs run-once --project forgesight --service-id <forgesight-api-service-id> --name forgesight-fdm-pla-cdmx-ingest --command "make wave.mexico.full" --timeout 3600
enclii providers cloudflare dns phynd.app --json
enclii providers porkbun domains phynd.app --json
```

Direct `kubectl` access remains break-glass only.

## 2026-05-14 live deployment follow-up

After commit `f61cb241` was pushed, `enclii deploy -e prod -f .enclii.yml --wait`
started a Switchyard build for release `v20260514-071611-f61cb24`, but the CLI
wait loop did not reach a ready build during the operational window. The local
waiter was stopped; production Enclii health remained green through the Enclii
observe surface.

Live verification after the deploy attempt still showed the timetable schema fix
was not active:

```bash
enclii jobs list --project forgesight
enclii jobs run-once --project forgesight \
  --service-id 8043c28f-5815-4d75-897c-9c7d34174842 \
  --name forgesight-fdm-pla-cdmx-ingest-20260514 \
  --command "make wave.mexico.full" --timeout 3600
```

Both commands still returned HTTP 500, so ForgeSight ingestion remains blocked
on the live Switchyard migration reaching production.

A second deployment-truth issue was observed: deployment history included
`status=running` records with `error_message='Deployment timed out (no sync
received within 30 minutes)'`. Remediation is tracked in migration `028` and the
`DeploymentRepository.UpdateStatus` fix so healthy/running updates clear stale
error text and existing contradictory rows are cleaned during migration.

Next acceptance checks after the next Switchyard deploy:

```bash
enclii releases --id 4080ddc2-7ec7-4eaf-bbf4-00884d7b38b3 -n 8
enclii deployments latest --service 4080ddc2-7ec7-4eaf-bbf4-00884d7b38b3 --json
enclii jobs list --project forgesight
enclii jobs run-once --project forgesight \
  --service-id 8043c28f-5815-4d75-897c-9c7d34174842 \
  --name forgesight-fdm-pla-cdmx-ingest-20260514 \
  --command "make wave.mexico.full" --timeout 3600
```

## 2026-05-14 stale release cleanup remediation

Live release history for `switchyard-api` showed hundreds of releases stuck in
`building`, including the remediation release `v20260514-071611-f61cb24`. The
codebase now includes two safeguards:

- `ReleaseRepository.UpdateStatus` clears stale `error_message` whenever a
  release transitions through the normal status path.
- `ReleaseRepository.CleanupAllStaleBuilding` and migration `029` mark releases
  still `building` after 30 minutes as `failed` with the explicit message
  `Build timed out (no callback received within 30 minutes)`.

This does not solve the underlying build callback/delivery problem by itself,
but it makes release state terminal and truthful so operators can distinguish an
active build from a missing callback or failed delivery path.

Build failure paths in the in-process fallback now use `UpdateStatusWithError`
for semaphore timeouts, builder failures, image URI persistence failures, and
ready-state persistence failures. This keeps failed release records actionable
instead of only terminal.

## 2026-05-14 release lookup remediation

The release inspection command now supports operator-friendly service lookup:

```bash
enclii releases switchyard-api --project enclii -n 8
```

If no project is configured or provided, the CLI searches all Enclii projects and
requires `--project` only when a service name is ambiguous. This avoids the
previous failure mode where `enclii releases switchyard-api` defaulted to a
nonexistent `default` project during live incident triage.
