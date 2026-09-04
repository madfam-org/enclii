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
| `priority-classes.yaml` | runner PriorityClasses — declared, **not yet referenced** | `arc-watchdog` |
| `monitoring.yaml` | ServiceMonitor + PrometheusRule. **Not deployed and not coverage** — see its own header | nothing |

`madfam-runners-green` exists only as Helm values: a dormant standby for
blue/green switches, with no rendered manifest and no digest pin.

The `arc-watchdog` Application uses an explicit `directory.include`
allowlist. **A new file in this directory is not deployed until it is named
there.** That is why `monitoring.yaml` sat looking healthy for 82 days
without ever being applied.

## The digest pin lives in two places now

Both `runner-blue/rendered.yaml` and `runner-deploy/rendered.yaml` pin the
overlay image **by digest** (twice each: the `runner` container and the
`init-dind-externals` initContainer), because Kyverno's
`require-image-digest` rejects a floating tag and `imagePullPolicy: Always`
does not rescue one.

So a base-image bump is **three** steps, not two:

1. bump `BASE_TAG` + `BASE_TAG_DATE` in
   [`../../../docker/arc-runner/Dockerfile`](../../../docker/arc-runner/Dockerfile);
2. merge, and take the digest from the `arc-runner-image.yml` run;
3. repin **all four** image references — both files, two each.

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

It is small on purpose (`minRunners: 1`, `maxRunners: 2`) and lands on the
same builder nodes as blue, which already sit at 94–95% CPU. Capacity is not
the point; not queueing is.

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

**Nothing sets `priorityClassName` yet**, in this repo or anywhere else, so
they currently have no runtime effect. They are declared now so the ordering
is a reviewed artifact instead of a number invented during the first capacity
incident. All four use `preemptionPolicy: Never`: they change queue order,
never evicting a running job — a CI job killed mid-run presents as a lost
runner pod, which is the hardest failure on this fleet to diagnose. Enabling
preemption for `enclii-deploy` is a separate decision with its own evidence.
