# Zero-Touch Onboarding Audit - 2026-05-18

This audit checks whether Enclii currently respects the intended zero-touch
policy for onboarding MADFAM ecosystem apps and services.

Policy definition used for this audit:

- Core repos define the platform.
- Client repos define themselves.
- Onboarding a new app or service must not require a commit to the Enclii
  codebase.
- Enclii may read a client repo, persist runtime state in the database, create
  Kubernetes/Cloudflare/Argo/Janua/Vault resources, and reconcile those
  resources idempotently.
- Enclii must not become a central hand-edited catalog of client repos,
  routes, secrets, status checks, CORS origins, namespaces, or dashboard
  metadata.

## Executive Status

Enclii is not fully zero-touch today.

Progress since this audit was opened:

- Boundary guardrails now reject new app-specific Enclii catalog entries in CI
  and pre-commit.
- ArgoCD registration has an opt-in runtime `Application` reconciler behind
  `ENCLII_ARGOCD_REGISTRATION_MODE=runtime`.
- Onboarding now persists `status.entries[]` in the onboarding DB snapshot, and
  status regeneration has an opt-in runtime ConfigMap projector behind
  `ENCLII_STATUS_PROJECTION_MODE=runtime`.
- Project-card framework rendering now uses backend service facts from release
  `framework_slug`; the UI no longer carries a MADFAM repo/framework map.

The repo has important zero-touch foundations:

- `apps/switchyard-api/internal/manifest/enclii_yaml.go` reads `enclii.yaml`
  directly from client repositories.
- Onboarding validates a client `manifest_path`, creates project/service DB
  rows, creates namespaces, applies network policies derived from
  `enclii.yaml`, and can provision domains at runtime.
- Push webhooks re-read `enclii.yaml` from the pushed commit and use it for
  domains and headers.
- Build callbacks commit image digest updates to the target/client repo, not
  to Enclii.
- A richer `EcosystemApp` desired-state contract exists in SDK/API packages.

But the active production path still violates the policy in critical places:

- App onboarding and ensure flows auto-commit per-client ArgoCD config files
  into `infra/argocd/projects/<name>/config.json` in the Enclii repo.
- Status registration and status regeneration edit Enclii-owned ConfigMaps.
- Enclii contains large static app-specific inventories for ArgoCD projects,
  status pages, tunnel expectations, probes, deploy monitoring, CORS origins,
  ExternalSecrets, and ops scripts.
- Dashboard framework metadata still contains hardcoded MADFAM repo mappings,
  causing the main dashboard and `/projects` page to disagree when backend
  framework data is missing.

The platform is therefore close in shape, but not close in enforcement. The
desired-state reader exists, while the registry and observability surfaces are
still partly Enclii-as-catalog.

## P0 Violations

### 1. ArgoCD app registration writes client desired state into Enclii

Original evidence:

- `apps/switchyard-api/internal/api/onboarding_handlers.go:214` generates a
  project `config.json` for the ApplicationSet.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:227` treats the
  ArgoCD config commit as a critical onboarding step.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:231` writes to
  `infra/argocd/projects/<project>/config.json`.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:234` commits that
  file to `h.config.EncliiRepoOwner/h.config.EncliiRepoName`.
- `apps/switchyard-api/internal/api/onboarding_ensure_handlers.go:193` repeats
  the same behavior for repair/ensure.
- `infra/argocd/apps/project-appset.yaml:25` uses a Git generator pointed at
  `https://github.com/madfam-org/enclii.git`.
- `infra/argocd/apps/project-appset.yaml:29` scans
  `infra/argocd/projects/*/config.json`.

Current repo state:

- There are 35 checked-in `infra/argocd/projects/*/config.json` files.
- 6 point at the Enclii repo and are plausibly platform/core.
- 29 point at non-Enclii MADFAM/product repos and are app onboarding state
  stored in Enclii.

Impact:

- A new app cannot be considered zero-touch because the authoritative Argo
  registration is an Enclii repo file.
- The onboarding API's critical path depends on a GitHub token with write
  access to Enclii.
- Argo desired state is split between client repo manifests and Enclii's
  `config.json` catalog.

Required target:

- Onboarding must create or reconcile ArgoCD `Application` state from the
  client repo declaration without committing to Enclii.
- The Enclii repo should contain only the platform controller/template code and
  an allowlisted core bootstrap set.

### 2. Status registration and regeneration write app monitors to Enclii

Current status:

- Fixed: onboarding/ensure no longer edit `apps/status/k8s/madfam/configmap.yaml`;
  they persist `status.entries[]` into `config_snapshot.status_entries`.
- Fixed: `POST /v1/admin/status/regenerate` now supports runtime Kubernetes
  ConfigMap projection with `ENCLII_STATUS_PROJECTION_MODE=runtime`.
- Remaining: production must be switched from legacy `gitops` projection after
  historical MADFAM status entries have DB provenance and shrink-guard parity.

Original evidence:

- `apps/switchyard-api/internal/api/onboarding_handlers.go:264` registers
  status entries during onboarding.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:559` says
  `registerStatusEntries` reads the current ConfigMap from GitHub and commits
  an update.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:594` targets
  `apps/status/k8s/madfam/configmap.yaml`.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:630` commits the
  updated ConfigMap back to Enclii.
- `apps/switchyard-api/internal/api/status_handlers.go:208` implements
  `RegenerateStatusConfig`.
- `apps/switchyard-api/internal/api/status_handlers.go:230` reads checked-in
  status ConfigMaps from Enclii.
- `apps/switchyard-api/internal/api/status_handlers.go:278` commits generated
  ConfigMaps back to Enclii.

Data-integrity gap:

- `apps/switchyard-api/internal/api/status_handlers.go:169` expects
  `reg.ConfigSnapshot["status_entries"]`.
- `apps/switchyard-api/internal/api/onboarding_handlers.go:341` stores
  `manifest_path`, `namespace`, `branch`, `services`, and `domains`, but not
  `status_entries`.
- `apps/switchyard-api/internal/api/onboarding_ensure_handlers.go:310` stores
  desired state and ensure metadata, but not status entries.

Impact:

- The status page is still a repo-mutating projection.
- Historical app status entries are not recoverable from onboarding DB state.
- A status regenerate can be unsafe without shrink guards because the source of
  truth is incomplete.

Required target:

- Status declarations from `enclii.yaml` or `EcosystemApp` must be persisted in
  onboarding/runtime state.
- Status app config must be served from the API or reconciled into Kubernetes
  directly, not committed to Enclii.

## P1 Violations

### 3. Static tunnel, probe, and deploy-monitor inventories live in Enclii

Evidence:

- `infra/k8s/production/expected-tunnel-config.json` contains 48 hostnames,
  including product routes such as Dhanam, Tezca, Yantra4D, ForgeSight,
  Karafiel, MADFAM Site, Pravara, and others.
- `infra/k8s/production/cloudflared-probe.yaml:43` embeds static probe targets.
- `infra/k8s/production/cloudflared-probe.yaml:63` through line 84 hardcodes
  Karafiel, Tezca, and Dhanam probes.
- `infra/k8s/production/synthetic-flow-probe.yaml:70` hardcodes the Karafiel
  SSO journey.
- `infra/k8s/production/synthetic-flow-probe.yaml:103` hardcodes the Dhanam
  SSO journey.
- `infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml:35` says a
  repo should be added by appending to the checked-in list.
- `infra/k8s/production/monitoring/deploy-pipeline-monitor.yaml:40` through
  line 53 hardcodes 14 MADFAM repos.

Nuance:

- Runtime Cloudflare provisioning is mostly on the right path:
  `apps/switchyard-api/internal/api/domain_provisioner.go:29` provisions
  domains from `enclii.yaml` into DB, Cloudflare tunnel routes, and DNS.
- The static tunnel expectation file and monitoring ConfigMaps are the
  zero-touch breach, not the runtime Cloudflare API path itself.

Required target:

- Tunnel expectations should be generated from core baseline plus DB/client
  declarations, or replaced with a reconciler comparing live Cloudflare routes
  to onboarding state.
- Probe targets and deploy monitor repos should be sourced from onboarding DB,
  Argo Applications, or explicit client observability declarations.
- Synthetic journeys should live in client repos or be marked as core-only
  exceptions with an allowlist.

### 4. Janua CORS/CSRF contains product origins in Enclii

Evidence:

- `infra/k8s/production/janua-env-config.yaml:53` says non-core app origins are
  derived from OAuth redirect URIs.
- `infra/k8s/production/janua-env-config.yaml:54` then records a ForgeSight
  emergency addition.
- `infra/k8s/production/janua-env-config.yaml:57` includes ForgeSight origins
  in `CORS_ORIGINS`.
- `infra/k8s/production/janua-env-config.yaml:60` includes ForgeSight origins
  in `CSRF_TRUSTED_ORIGINS`.

Impact:

- Adding app auth domains can still require Enclii repo edits.
- This directly violates the contract item forbidding core repo CORS edits for
  app onboarding.

Required target:

- Janua must derive allowed origins from OAuth client redirect URIs and/or
  client-owned identity declarations.
- Enclii should keep only core platform origins in checked-in Janua config.

### 5. Product ExternalSecrets and service-auth bridges are checked into Enclii

Evidence:

- `infra/k8s/base/external-secrets/vault-secrets/` contains per-app
  ExternalSecrets such as `selva`, `dhanam`, `forgesight`, `karafiel`,
  `madfam-site`, `pravara-mes`, `tezca`, and `yantra4d`.
- `infra/k8s/base/external-secrets/vault-secrets/dhanam-secrets.yaml:4`
  defines a Dhanam secret target in the Dhanam namespace.
- `infra/k8s/base/external-secrets/vault-secrets/dhanam-secrets.yaml:18`
  through line 89 maps product keys from `secret/dhanam`.
- `infra/k8s/base/external-secrets/ecosystem-service-auth-external-secrets.yaml:2`
  defines an ecosystem service-auth bridge.
- `infra/k8s/base/external-secrets/ecosystem-service-auth-external-secrets.yaml:8`
  says the bridge is required for Dhanam <-> Cotiza.
- `infra/k8s/base/external-secrets/ecosystem-service-auth-external-secrets.yaml:56`
  defines `digifab-quoting-dhanam-auth`.

Vault reboot/recovery relevance:

- After Vault/ESO recovery work, keeping per-product secret materialization
  manifests in Enclii is a recovery and rotation risk.
- The safe source of truth should be a manifest-declared secret contract plus
  Vault custody metadata, not an expanding set of product YAML files in the
  platform repo.

Required target:

- Client desired state should declare needed secret keys, source paths, rotation
  cadence, and ownership.
- Enclii should create/update ExternalSecret resources dynamically or through a
  controller from stored desired state.
- Cross-service grants should move to Janua Lockbox/Vault service grants, not
  product-specific bridge manifests in Enclii.

## P2 Violations And Smells

### 6. Ops scripts still encode app/namespace lists

Examples:

- `scripts/cluster-ops-deploy.sh:154` names Yantra4D, Pravara, Karafiel,
  Tezca, ForgeSight, and MADFAM Site for policy application.
- `scripts/cluster-ops-deploy.sh:419` loops across a static namespace list.
- `scripts/vault-secret-migration.sh:61` defines `ALL_NAMESPACES`.
- `scripts/rotate-ghcr-credentials.sh:24` starts a hardcoded namespace list.

Impact:

- Recovery and rotation workflows are not reliably complete for newly
  onboarded apps.
- Operators must remember to update scripts outside the onboarding path.

Required target:

- Runtime scripts should derive namespaces from Kubernetes labels, onboarding
  DB records, Argo Applications, or client manifests.
- Static lists should be core-only and explicitly allowlisted.

### 7. Historical DB migrations contain app slugs

Evidence:

- `apps/switchyard-api/internal/db/migrations/024_reparent_projects_to_teams.up.sql`
  contains a one-time backfill map of existing MADFAM projects.

Classification:

- Acceptable as a historical data repair if it is not repeated as an onboarding
  mechanism.

Guardrail:

- Future migrations must not add product slugs as a normal onboarding path.
- Any product-specific data migration must be marked as historical repair and
  tied to an incident/audit.

### 8. Ecosystem docs/templates act as a central product catalog

Evidence:

- `docs/templates/ecosystem/README.md` instructs contributors to update
  central metadata files in Enclii to add a repo.
- `docs/templates/ecosystem/generator.py` carries a central MADFAM ecosystem
  map.

Impact:

- Not a runtime provisioning blocker, but it normalizes Enclii as the registry
  for products.

Required target:

- Repo-facing ecosystem docs should be generated from client repo manifests or
  from a separate catalog API, not maintained as product inventory in Enclii.

### 9. Dashboard framework fallback hardcodes product knowledge

Current status:

- Fixed: project services now expose backend-detected `framework` from the
  latest non-empty release `framework_slug`.
- Fixed: both the main dashboard and `/projects` use the shared project-card
  transform and no longer apply a MADFAM-specific repo/framework fallback.
- Remaining: add backend freshness/provenance fields and a single aggregate
  project-card endpoint so both pages avoid per-project fan-out.

Original evidence:

- `apps/switchyard-ui/components/dashboard/framework-icon.tsx:186` defines
  `KNOWN_REPO_FRAMEWORKS`.
- The map includes Janua, Pravara, Tezca, Dhanam, ForgeSight, Karafiel,
  Yantra4D, Enclii, MADFAM Site, and Selva.
- `apps/switchyard-ui/app/(protected)/page.tsx:92` uses the hardcoded fallback
  when service framework data is absent.
- `apps/switchyard-ui/app/(protected)/projects/page.tsx:138` does not use the
  same fallback.

Impact:

- Main dashboard and `/projects` can render different framework metadata for
  the same project.
- UI truth depends on client-specific frontend knowledge rather than backend
  service facts.

Required target:

- Backend project/service records must carry a truthful `framework_slug` from
  repo analysis, build detection, or client desired state.
- Dashboard and `/projects` should use the same aggregate endpoint or shared
  transform with no MADFAM-specific fallback.

## What Already Works With The Policy

These paths are compatible with zero-touch and should be kept:

- `manifest.FetchAndParse` reads `enclii.yaml` from the client repo.
- Onboarding validates `manifest_path` in the client repo before provisioning.
- NetworkPolicy resources are generated from the client `network:` section and
  applied through the Kubernetes API.
- Domain records, Cloudflare tunnel routes, and DNS records can be created from
  client `domains:` declarations without an Enclii repo commit.
- Push webhooks re-read the pushed client repo commit for domains and headers.
- Build callbacks write digest changes to the target/client repo's
  `kustomization.yaml`.
- `EcosystemApp` and the SDK type model already describe the fuller contract
  needed for identity, runtime, deployment, observability, and secret custody.

## Remediation Plan

### Phase 0 - Freeze and guard the boundary

Goal: stop making the problem larger while replacement controllers are built.

Implementation:

- Mark checked-in non-core `infra/argocd/projects/*/config.json` files as
  legacy adopted state.
- Add a core allowlist for platform projects that may remain in Enclii during
  bootstrap.
- Add a CI boundary check that fails on new non-core app entries in:
  - `infra/argocd/projects/*/config.json`
  - `apps/status/k8s/*/configmap.yaml`
  - product routes in `expected-tunnel-config.json`
  - product CORS/CSRF origins in Enclii-owned Janua config
  - new product ExternalSecret manifests under Enclii
  - new UI hardcoded repo/framework mappings
- Update docs and CLI help so they no longer describe Enclii auto-commits as
  zero-touch.

Tests:

- CI fixture that adds a fake non-core Argo config and expects the boundary
  check to fail.
- CI fixture that adds a fake product CORS origin and expects failure.

### Phase 1 - Replace Enclii-git Argo registration

Goal: make app deployment registration runtime-reconciled from client desired
state.

Implementation:

- Add an Argo application reconciler in Switchyard API or a controller:
  - input: repo URL, branch, manifest path, namespace, app name, desired hash
  - output: ArgoCD `Application` CR or Argo API application
  - ownership label: `app.kubernetes.io/managed-by=enclii-platform`
  - idempotency key: repo + environment + desired-state hash
- Store Argo desired state in onboarding DB, not in Enclii git.
- Make the existing Enclii auto-commit mode a legacy feature flag.
- Default new onboarding to runtime Argo reconciliation.
- Migrate the 29 non-Enclii project configs:
  - read legacy `config.json`
  - upsert onboarding records where missing
  - create matching Argo Applications
  - compare live sync/health
  - disable or remove legacy ApplicationSet ownership for those apps

Tests:

- Unit test that onboarding in runtime mode never calls Enclii GitHub file
  writer.
- Integration test that onboarding creates/updates an Argo Application from a
  client fixture.
- Migration test for legacy `config.json` to DB/Argo desired state.

### Phase 2 - Make status a projection from runtime desired state

Goal: status entries trace back to client manifests or a core allowlist.

Implementation:

- Persist `status.entries` from `enclii.yaml`/`EcosystemApp` in onboarding DB.
- Change `registerStatusEntries` to upsert DB desired state only.
- Replace GitHub ConfigMap commits with one of:
  - status app reads the Switchyard API directly; or
  - a reconciler updates the Kubernetes ConfigMap directly in-cluster.
- Keep core Enclii status entries in an explicit core allowlist.
- Add provenance to every status entry: `core` or `repo=<owner/name> path=...`.

Tests:

- Onboarding stores status entries in `config_snapshot`.
- Status projection includes client entries after onboarding.
- Regenerate does not call Enclii GitHub file writer.
- Shrink guard remains until all historical entries have provenance.

### Phase 3 - Derive domains, probes, and monitoring from desired state

Goal: observability and routing become data-driven.

Implementation:

- Reconcile Cloudflare live tunnel routes back into `custom_domains` so
  out-of-band existing routes are adopted.
- Split `expected-tunnel-config.json` into core-only baseline or remove it in
  favor of a live route audit.
- Generate cloudflared probe targets from:
  - core platform targets
  - client service health checks in `enclii.yaml`/`EcosystemApp`
  - live service records
- Generate deploy-pipeline monitor repos from onboarding DB or Argo Apps.
- Move synthetic journeys into client repos under an agreed path such as
  `observability/synthetic-flows/*.yaml`.

Tests:

- Route audit reports no route-only domains.
- Every generated probe target has provenance.
- Every deploy-monitor repo has an onboarding record or core allowlist entry.

### Phase 4 - Move auth and secrets to client-declared contracts

Goal: app auth and secret materialization no longer require Enclii YAML edits.

Implementation:

- Extend `enclii.yaml` or switch onboarding to `EcosystemApp` for:
  - OAuth clients
  - redirect URIs
  - allowed origins
  - audiences/scopes/roles
  - secret keys and Vault paths
  - rotation cadence
  - external provider purpose labels
- Janua derives CORS/CSRF from registered OAuth clients.
- Enclii creates ExternalSecret resources from client desired state or through a
  controller.
- Cross-service credentials move to Vault/Janua grants instead of
  product-specific bridge manifests in Enclii.

Tests:

- New client fixture gets OAuth client, CORS origin, ExternalSecret, and secret
  provenance without an Enclii repo diff.
- Vault/ESO recovery check enumerates namespaces from runtime labels/DB, not a
  static script list.

### Phase 5 - Make project cards truthful and fresh

Goal: the main dashboard and `/projects` render the same truthful data.

Implementation:

- Add a backend project-card aggregate endpoint that returns:
  - project identity
  - services
  - aggregate health/rollout state
  - deploy resolution
  - framework slug
  - repo metadata freshness timestamp
  - provenance for inferred fields
- Populate `framework_slug` from repo analyzer/build detector/client desired
  state.
- Remove `KNOWN_REPO_FRAMEWORKS` for non-core product repos.
- Make both dashboard pages use the same aggregate endpoint or the exact same
  shared transform.

Tests:

- Main dashboard and `/projects` render identical framework/deploy/health data
  for the same API fixture.
- Unknown framework remains unknown unless the backend emits a slug.
- Repo metadata cache behavior is explicit in the UI freshness indicator.

## Acceptance Criteria For "100% Zero-Touch"

A new MADFAM-slice service is compliant only when this is true:

1. The client repo contains the desired state.
2. `POST /v1/admin/onboard` or `/v1/admin/onboard/ensure` reads that desired
   state.
3. No file in the Enclii repo changes.
4. The platform creates/reconciles DB records, namespace, NetworkPolicies,
   ExternalSecrets, Argo Application, domains, DNS, tunnel routes, status
   entries, probes, auth clients, and dashboard data from runtime state.
5. Every user-facing project card field has a source and freshness timestamp.
6. Every generated resource has provenance back to either:
   - the client repo manifest and commit SHA; or
   - an explicit core platform allowlist.
7. CI rejects newly introduced app-specific state in Enclii.

## Priority Recommendation

Do not start by deleting legacy files. Start by stopping new writes.

The safe order is:

1. Add guardrails and legacy allowlists.
2. Implement runtime Argo Application reconciliation.
3. Persist status entries and stop status GitHub commits.
4. Adopt existing Cloudflare/status/Argo state into DB with provenance.
5. Move secrets/auth declarations to client desired state.
6. Remove or quarantine legacy app catalogs after live parity is proven.

That sequence keeps production stable while moving the source of truth out of
Enclii and back to the client repos where the zero-touch policy says it belongs.
