# Project Card Truth Runbook

_Last updated: 2026-05-18_

## Source of Truth

The main dashboard (`/`) and the projects dashboard (`/projects`) consume the
same backend aggregate:

```text
GET /v1/projects/cards
```

This endpoint keeps `/v1/projects` stable for SDKs and raw project listings,
while projecting dashboard card facts from Switchyard's backend sources:

- projects from `projects`
- service health, status, replicas, and namespace from `services`
- latest deployment and commit facts from `deployments` + `releases`
- backend-detected framework from the latest non-empty `releases.framework_slug`
- rollout state from live Kubernetes ReplicaSet inspection

The UI must not infer product/framework truth from MADFAM repo names, project
slugs, or domain maps. If the backend omits `framework`, the cards render no
framework rather than guessing.

## Contract

The aggregate response has this shape:

```json
{
  "generated_at": "2026-05-18T20:30:00Z",
  "count": 1,
  "projects": [
    {
      "id": "uuid",
      "name": "Example",
      "slug": "example",
      "updated_at": "2026-05-18T20:00:00Z",
      "aggregate_status": "healthy",
      "service_count": 1,
      "healthy_count": 1,
      "framework": "nextjs",
      "git_repo": "https://github.com/example/example",
      "deploy_resolution": "deployed",
      "last_deployment": {
        "timestamp": "2026-05-18T19:30:00Z",
        "status": "success",
        "branch": "main",
        "commit_message": "feat: ship"
      },
      "services": [
        {
          "id": "uuid",
          "name": "api",
          "status": "running",
          "health": "healthy",
          "replicas": "2/2",
          "environment": "production",
          "current_image_uri": "ghcr.io/example/api@sha256:...",
          "rollout_state": "ok"
        }
      ]
    }
  ]
}
```

## Acceptance Gates

Before calling the cards truthful:

1. `/` and `/projects` both fetch `/v1/projects/cards`.
2. No project-card code path imports client-specific repo/domain maps.
3. `aggregate_status` is `failing` for blocked rollout state or failed service
   status, even if an old pod keeps serving traffic.
4. `deploy_resolution` distinguishes `deployed`, `no-deploys`, and `unknown`;
   the UI must not claim "No deployments yet" when service data was unresolved.
5. `generated_at` drives the visible last-sync timestamp.
6. Production `switchyard-api` and `switchyard-ui` images are built from the
   commit containing this endpoint and UI wiring.
7. `services.json` dependency paths point at real repo directories. In
   particular, `switchyard-ui` must depend on `packages/shared-lib` and
   `packages/ui-components`, otherwise shared card UI changes can pass CI
   without producing a fresh UI image.
8. CI keeps non-container jobs on GitHub-hosted runners and reserves the
   `madfam-runners-blue` ARC pool for Docker image builds. If the project-card
   fix is merged but the production image does not move, check for queued
   `docker-build` jobs rather than lint/UI/build jobs occupying ARC capacity.

## Verification

```bash
curl -sS https://api.enclii.dev/v1/projects/cards \
  -H "Authorization: Bearer $ENCLII_TOKEN" | jq '.count, .generated_at'
```

Then verify the live deployment image metadata:

```bash
kubectl -n enclii get deploy switchyard-api switchyard-ui \
  -o custom-columns=NAME:.metadata.name,IMAGE:.spec.template.spec.containers[0].image
```

If `/projects` renders stale card facts after the API is correct, check whether
the UI deployment is still on an older image. For signed-image provenance, run
`cosign verify` on each deployment digest and compare `githubWorkflowSha` with
the commit that introduced the card aggregate or follow-up UI fix.

To rebuild only the project-card services after a CI-only remediation, dispatch
the CI workflow with:

```bash
gh workflow run ci.yml -R madfam-org/enclii \
  -f services=switchyard-api,switchyard-ui
```
