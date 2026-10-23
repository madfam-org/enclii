# ARC (Actions Runner Controller) manifests

What the cluster actually reads for the self-hosted GitHub Actions fleet.
The Helm values under [`../../../helm/arc/`](../../../helm/arc/) are the
inputs; the `rendered.yaml` files here are what ArgoCD syncs. **Keep both in
agreement** — the one time they drifted, a `maxRunners` raise lived only in
the rendered manifest and the next Helm-path deploy would have silently
halved capacity.

## What is here

| Path | What it is | Synced by |
| --- | --- | --- |
| `controller/rendered.yaml` | the ARC controller (chart 0.14.0) | `arc-runners` |
| `runner-blue/rendered.yaml` | `madfam-runners-blue` — the whole org's CI pool, `maxRunners: 20` | `arc-runners-blue` |
| `runner-deploy/rendered.yaml` | `madfam-runners-deploy` — the ADR-010a deploy pool, `maxRunners: 2` | `arc-runners-deploy` |
| `pool-health-alert.yaml` | the detector that pages when the pool stops serving jobs | `arc-watchdog` |
| `stuck-runner-watchdog.yaml` | purges EphemeralRunners stuck against completed runs | `arc-watchdog` |
| `priority-classes.yaml` | non-preempting deploy priority; other classes declared | `arc-watchdog` |
| `monitoring.yaml` | ServiceMonitor + PrometheusRule. **Not deployed and not coverage** — see its own header | nothing |

`madfam-runners-green` exists only as Helm values: a dormant standby for
blue/green switches, with no rendered manifest and no digest pin.

The `arc-watchdog` Application uses an explicit `directory.include`
allowlist. **A new file in this directory is not deployed until it is named
there.** That is why `monitoring.yaml` sat looking healthy for 82 days
without ever being applied.

## Capacity recovery without cancelling jobs

Controller pods require non-builder nodes and retain two replicas, leader
election and a disruption budget. Values and rendered manifests explicitly
select `updateStrategy: eventual`: a runner-template change waits for assigned
jobs to finish instead of stopping active runners. ARC configuration validation
uses the existing dedicated deploy pool, with the same Helm and manifest checks,
so a blue scheduling backlog cannot block its own repair.

Do not change an active scale set's listener template merely to move its listener.
Upstream reports a warm-runner listener recreation deadlock for values-only
changes on the deployed controller version
([upstream report](https://github.com/actions/actions-runner-controller/issues/4432)).
The deploy-pool capacity change must wait until the eventual-strategy
controller rollout is verified through Enclii. Observe pool assignments before
and after the change; no active runner deletion or workflow cancellation is part
of this recovery. The deploy pool now starts on demand (`minRunners: 0`, `maxRunners: 2`) and
uses the existing non-preempting deploy PriorityClass. Its listener requires a
non-builder node. It must preserve the blue
worker template and verify actual pickup of previously queued jobs.

## Controller ownership

The controller excludes `app.kubernetes.io/instance` when copying labels to
dynamic children. Without this ARC-supported filter, listener Roles and
RoleBindings inherit Argo's tracking label and Argo repeatedly prunes them as
resources absent from Git. Keep `flags.excludeLabelPropagationPrefixes` in the
controller values and its rendered command argument in agreement. Other labels,
RBAC rules, and Argo pruning remain unchanged.

Verify the controller rollout through Enclii:

```bash
enclii ops apps status arc-runners -n argocd --json
enclii ops pods diagnose -n arc-system --json
```

Then check both runner Applications for recurring prune operations. Existing dynamic
resources acquire filtered labels when ARC recreates them; controller readiness
alone is not evidence that the reconciliation loop has stopped.

## The digest pin lives in two places now

Both `runner-blue/rendered.yaml` and `runner-deploy/rendered.yaml` pin the
overlay image **by digest** (twice each: the `runner` container and the
`init-dind-externals` initContainer), because Kyverno's
`require-image-digest` rejects a floating tag and `imagePullPolicy: Always`
does not rescue one.

So a base-image bump is **three** steps, not two:

1. bump `BASE_TAG` + `BASE_TAG_DATE` in
   [`../../../docker/arc-runner/Dockerfile`](../../../docker/arc-runner/Dockerfile);
2. merge, and take the digest from the `arc-runner-image.yml` run on `main`:
   the job summary's "Digest (smoke-tested, signed)" line, which is the build
   step's `digest` output. That is the signed single-platform manifest, and
   the digest `:stable` is promoted to once the run's smoke checks and
   signature pass (see
   [`../../../docker/arc-runner/README.md`](../../../docker/arc-runner/README.md#stable-is-promoted-after-verification)
   and the header of
   [`../../../../.github/workflows/arc-runner-image.yml`](../../../../.github/workflows/arc-runner-image.yml)).
   A run that did not promote `:stable` is not a candidate for a pin;
3. repin **all four** image references — both files, two each.

Both pools currently run
`ghcr.io/madfam-org/enclii/arc-runner:stable@sha256:d12ef5e0f5314a878970d079b3e64e85ee9837f23ea8ad4538f5db9ac38e2cb3`
(#771, actions-runner 2.338.0, the image built and promoted on `main` on
2026-10-17 in run 39072135012; it is the single-platform manifest digest).
Rollback digest:
`sha256:2f5876c2b29d4fb42640fae93579f488b11aa7ee17935ddd2dc1091c89784230`,
the pin this one replaced (#656, actions-runner 2.337.0, built on 2026-09-29).
Before that: `sha256:c35966ed277acedf16953c1e8075a2c9d92509be488b4de48b9487605da5a162`
(#521, the image built from #520 — the render-library parity fix that added
`libcomerr2` and `libgpg-error0`). That one is a
**manifest list** digest: pins before #656 took the list digest from the build
log's `pushing manifest for …` line. Both are valid digest pins for the
pools' `linux/amd64` nodes. Read both from `git log -p` on the two
`rendered.yaml` files rather than from this paragraph if they ever disagree.

### How a merged pin reaches the pools

Through Argo, not through `arc-self-deploy.yml`. The Applications
`arc-runners-blue` and `arc-runners-deploy`
([`infra/argocd/apps/arc-runners.yaml`](../../../argocd/apps/arc-runners.yaml),
[`infra/argocd/apps/arc-runners-deploy.yaml`](../../../argocd/apps/arc-runners-deploy.yaml))
auto-sync `runner-blue/` and `runner-deploy/` from `main` (automated, prune,
self-heal). The controller comes the same way from `controller/` through
`arc-runners`.

`arc-self-deploy.yml` ("ARC Configuration and Bootstrap") is not that path.
On a push to `main` it runs only its Validate job (client-side dry runs). Its
Deploy job runs only on a non-dry-run `workflow_dispatch`, for bootstrap or
recovery, and even then it applies `infra/k8s/base/arc/` and the
**top-level** `infra/k8s/production/arc/*.yaml` files, never
`runner-*/rendered.yaml` or `controller/rendered.yaml`, before the Helm
blue/green switch. A green run of it after a pin merge is not evidence that
the pools moved.

Observed with #780 (2026-10-18, times UTC): merged 04:11; `arc-runners-deploy`
synced the new revision at 04:15 and `arc-runners-blue` right after; jobs
already running finished on their old pods, new jobs got 2.338.0. Check the
synced revision and the runner counts through Enclii:

```bash
enclii ops apps status arc-runners-blue -n argocd --json
enclii ops apps status arc-runners-deploy -n argocd --json
enclii ops runners arc -n arc-runners --json   # counts per scale set, no image
```

`ops runners arc` does not report the runner image (adapter gap, checked
2026-10-18), so the image pin itself is read with the break-glass command
below.

The pools never follow the `:stable` tag; moving or rolling back `:stable`
changes no running runner. The `:stable` rollback command lives in
[`../../../docker/arc-runner/README.md`](../../../docker/arc-runner/README.md#rolling-stable-back).

Verify what the pools actually run, read-only:

```bash
kubectl get autoscalingrunnerset -n arc-runners -o jsonpath=\
'{range .items[*]}{.metadata.name}{"\t"}{.spec.template.spec.containers[?(@.name=="runner")].image}{"\n"}{end}'
```

Repinning blue and forgetting deploy leaves a small pool quietly running an
old agent that starts failing registration ~60 days later while blue looks
perfectly healthy. The Image Age Ratchet watches the tag, not the digests;
this list is the control for those.

## The deploy pool (ADR-010a)

`madfam-runners-deploy` exists so a deploy does not queue behind a ten-shard
browser matrix — the observed case was ~30 minutes for one deploy job, then
~an hour end to end once the serial concurrency group backed up. It is a
**priority rule, not a partition**: nothing reserves blue for CI, and
nothing stops other workflows being pointed at this label later.

It is small and starts on demand (`minRunners: 0`, `maxRunners: 2`). Workers
land on builder nodes; the listener stays off builders. This reclaims an idle
worker reservation when no deploy is waiting. Non-preempting priority lets a
waiting deploy use the next available slot without evicting a running job.

**Provisioning order matters, and it is the reason this is a separate PR
from any workflow change.** The scale set must exist and be observed
registering *before* `vars.DEPLOY_RUNNER_LABEL` is set on the consuming
repository. Setting the variable first points every deploy job at a label no
runner offers, and those jobs queue **indefinitely** rather than failing
fast — the one way this change can make shipping worse than it is today.
With the variable unset, deploy behaviour is exactly what it is today, so
the two steps are independently revertible.

## Priority classes

`priority-classes.yaml` declares, highest first:

`enclii-deploy` > `fragua-paid` > `fragua-trial` > `enclii-internal-ci`

The deploy pool sets `priorityClassName: enclii-deploy`; blue CI retains its
existing default priority. They are declared now so the ordering
is a reviewed artifact instead of a number invented during the first capacity
incident. All four use `preemptionPolicy: Never`: they change queue order,
never evicting a running job — a CI job killed mid-run presents as a lost
runner pod, which is the hardest failure on this fleet to diagnose. Enabling
preemption for `enclii-deploy` is a separate decision with its own evidence.
