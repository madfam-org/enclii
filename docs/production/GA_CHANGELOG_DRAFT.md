# Enclii Commercial GA — changelog (draft)

> **Status:** Internal draft — publish after Stability GA window and legal sign-off.  
> **Retire externally:** “95% ready” / RC-only positioning.

---

## Highlights

Enclii is **generally available** for production workloads with three shipped product bets:

1. **Preview environments** — PR webhooks, preview URLs, wake/close lifecycle  
2. **Custom domains + TLS** — DNS verify, cert-manager, Junction routing  
3. **Persistent volumes** — service volume specs, PVC reconciliation, settings UI  

Plus **security and tenancy** hardening, **budget enforcement** on deploy/build, and **OpenAPI / sdk-ts** contract checks in CI.

---

## Security & multi-tenancy

- AuthZ matrix and handler audit (tenant isolation by project UUID)  
- Scoped `GET /v1/deployments` for non-admin users  
- Service-scoped log routes  
- Dashboard stats require authentication  
- Roundhouse callback and `git_repo` lookup fail-closed in production  

## Platform & API

- Canonical OpenAPI path; sdk-ts drift check in CI  
- `rollout_blocked_reason` on services (migration 030)  
- Structured budget proxy (`/v1/projects/:slug/billing/*`)  
- **Budget hard throttle** blocks non-production deploy/build at 100% spend  

## Product — Preview environments (bet A)

- `/v1/previews` API + PR webhook lifecycle  
- Previews tab in switchyard-ui  
- `enclii previews` CLI  
- E2E smoke + opt-in staging lifecycle (`PREVIEW_E2E_*`)  

## Product — Custom domains (bet B)

- Domain API, TLS, DNS verification  
- `enclii domains` CLI  
- E2E smoke + opt-in staging lifecycle (`DOMAIN_E2E_*`)  

## Product — Persistent volumes (bet C)

- Reconciler PVC generation and mounts  
- `ServiceVolumesEditor` in settings  
- `enclii volumes` CLI  
- E2E smoke + opt-in staging proof (`STORAGE_E2E_*`)  

## Developer experience

- Ecosystem E2E blocking on `main` (health, auth gates, preview/domains/storage smokes)  
- Commercial GA staging proof workflow (manual Actions)  
- Phase 0 ops runbook for deploy + cluster P0  

## Known limitations (honest scope)

- Preview environments: best-effort SLA; verify in your cluster before customer promises  
- Multi-region / edge: post-GA  
- Managed DB marketplace: deferred (bet D)  
- Full sdk-ts adoption in UI: incremental (preview + volume types shipped)  

## Upgrade notes (operators)

1. Deploy `main` per [PHASE0_OPS_RUNBOOK.md](./PHASE0_OPS_RUNBOOK.md)  
2. Apply migration **030**  
3. Complete [SECURITY_RELEASE_PR.md](./SECURITY_RELEASE_PR.md)  
4. Run staging proofs per [COMMERCIAL_GA_STAGING_PROOF.md](./COMMERCIAL_GA_STAGING_PROOF.md)  

---

## Related

- [SLA_DRAFT.md](./SLA_DRAFT.md)  
- [SUPPORT_TIERS_DRAFT.md](./SUPPORT_TIERS_DRAFT.md)  
- [COMMERCIAL_GA_TRACKER.md](./COMMERCIAL_GA_TRACKER.md)
