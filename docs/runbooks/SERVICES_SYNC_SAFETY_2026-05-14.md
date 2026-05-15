# Services Sync Safety Remediation - 2026-05-14

## Context

`enclii services-sync --dir . --project enclii --dry-run --ignore-project-mismatch`
was used as an Enclii-first way to inspect whether the Enclii repo could safely
sync its service definitions.

The dry run exposed two unsafe behaviors:

- The repository-level `.enclii.yml` used legacy service identities
  `enclii-api` and `enclii-ui` under `madfam-platform`, while the live Enclii
  project uses `switchyard-api` and `switchyard-ui` under `enclii`.
- The CLI treated Kubernetes `kind: Service` manifests as Enclii service specs
  because it only checked `kind: Service` and `metadata.name`.

## Remediation implemented

- `.enclii.yml` now targets `switchyard-api` and `switchyard-ui` in the live
  `enclii` project.
- `services-sync` now only accepts service specs with `apiVersion:
  enclii.dev/v1` or `apiVersion: enclii.dev/v1alpha`.
- `services-sync` now reads multi-document YAML files, so a canonical
  `.enclii.yml` can define both API and UI services without silently ignoring
  later documents.
- `services-sync --reconcile-existing` can now repair existing service metadata
  drift from checked-in specs, including `git_repo`, `app_path`, auto-deploy
  fields, and `build_config`. The behavior is opt-in so broad syncs do not
  surprise-update live service records.
- Regression tests cover Kubernetes-service exclusion and multi-document Enclii
  service parsing, source metadata preservation, and reconcile drift detection.

## Operational rule

Do not run broad repository-root service syncs with an older installed Enclii
CLI. Use a CLI build containing this fix, then run a dry run first:

```bash
enclii services-sync --dir . --project enclii --dry-run
```

Only run the non-dry-run sync after the dry run shows existing intended
services and no accidental service creations.

For existing-service drift, use the explicit reconcile flag:

```bash
enclii services-sync --dir apps/app --project forgesight --dry-run --reconcile-existing
enclii services-sync --dir apps/app --project forgesight --reconcile-existing
```

## Deployment note

`enclii deploy` builds from the current Git commit SHA. Local uncommitted
changes will not be included in a production deployment. Commit and push the
Switchyard migration and CLI safety fixes before deploying through Enclii.

## 2026-05-14 deploy-path finding

The corrected `.enclii.yml` now resolves to the live `switchyard-api` service,
but a production deploy attempt exposed a separate delivery-path risk: Enclii
created/used the requested `prod` environment and started a build, while the
live jobs API remained on the previous schema. Treat service sync safety and
actual production delivery as separate acceptance gates.

Required operator sequence:

1. Run `enclii services-sync --dir . --project enclii --dry-run` and confirm it
   only reports intended Enclii service specs.
2. Deploy Switchyard through the approved Enclii/GitOps path.
3. Verify the deployed release git SHA matches the pushed remediation commit.
4. Verify live timetable jobs no longer return HTTP 500.
