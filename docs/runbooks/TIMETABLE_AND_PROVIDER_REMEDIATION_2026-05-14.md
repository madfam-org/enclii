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
