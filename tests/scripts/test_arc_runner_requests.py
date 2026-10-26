"""ARC runner pods must request what they actually use.

The scheduler packs builder nodes by REQUEST. At 400m / 2Gi it fit 15 runners
on an 8 vCPU / 31Gi builder, whose real use then ran the node out of memory
and NotReady (2026-10-24). The requests below are the p90 per-pod peak of the
following week; see the comment on the runner container in
infra/k8s/production/arc/runner-blue/rendered.yaml for the evidence.

These tests keep the three places that carry the runner template in agreement
(the Helm values and both rendered pools) and stop a later edit from quietly
cutting the requests back below measured use.
"""

from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
POOLS = ("blue", "deploy")

# Floors, not exact values: raising a request is a capacity decision anyone
# may make with data; lowering one below measured use is the defect.
RUNNER_MIN_CPU_MILLI = 1000
RUNNER_MIN_MEMORY_MI = 4096
DIND_MIN_CPU_MILLI = 100
DIND_MIN_MEMORY_MI = 512


def cpu_milli(value):
    text = str(value)
    if text.endswith("m"):
        return int(text[:-1])
    return int(float(text) * 1000)


def memory_mi(value):
    text = str(value)
    units = {"Ki": 1 / 1024, "Mi": 1, "Gi": 1024, "Ti": 1024 * 1024}
    for suffix, factor in units.items():
        if text.endswith(suffix):
            return float(text[: -len(suffix)]) * factor
    raise AssertionError(f"unexpected memory quantity {value!r}")


def documents(path):
    return [d for d in yaml.safe_load_all((ROOT / path).read_text()) if d]


def pod_spec(pool):
    scale_set = next(
        d
        for d in documents(f"infra/k8s/production/arc/runner-{pool}/rendered.yaml")
        if d["kind"] == "AutoscalingRunnerSet"
    )
    return scale_set["spec"]["template"]["spec"]


def container(spec, name, key="containers"):
    return next(c for c in spec[key] if c["name"] == name)


def test_rendered_runner_requests_cover_measured_use():
    for pool in POOLS:
        requests = container(pod_spec(pool), "runner")["resources"]["requests"]
        assert cpu_milli(requests["cpu"]) >= RUNNER_MIN_CPU_MILLI, pool
        assert memory_mi(requests["memory"]) >= RUNNER_MIN_MEMORY_MI, pool
        # The disk request stays load-bearing (2026-08-07 evictions).
        assert "ephemeral-storage" in requests, pool


def test_rendered_runner_requests_stay_below_limits():
    for pool in POOLS:
        resources = container(pod_spec(pool), "runner")["resources"]
        assert cpu_milli(resources["requests"]["cpu"]) <= cpu_milli(
            resources["limits"]["cpu"]
        ), pool
        assert memory_mi(resources["requests"]["memory"]) <= memory_mi(
            resources["limits"]["memory"]
        ), pool


def test_rendered_dind_sidecar_is_accounted_for():
    for pool in POOLS:
        dind = container(pod_spec(pool), "dind", key="initContainers")
        requests = dind["resources"]["requests"]
        assert cpu_milli(requests["cpu"]) >= DIND_MIN_CPU_MILLI, pool
        assert memory_mi(requests["memory"]) >= DIND_MIN_MEMORY_MI, pool
        # No cpu/memory limit on dind: it would OOM-kill a job's service
        # containers or image build. Only the disk is capped.
        assert set(dind["resources"].get("limits", {})) == {"ephemeral-storage"}, pool


def test_pools_and_helm_values_request_the_same():
    values = documents("infra/helm/arc/values-runner-set.yaml")[0]
    values_requests = container(values["template"]["spec"], "runner")["resources"][
        "requests"
    ]
    for pool in POOLS:
        rendered = container(pod_spec(pool), "runner")["resources"]["requests"]
        assert cpu_milli(rendered["cpu"]) == cpu_milli(values_requests["cpu"]), pool
        assert memory_mi(rendered["memory"]) == memory_mi(
            values_requests["memory"]
        ), pool
    blue, deploy = (pod_spec(pool) for pool in POOLS)
    for name, key in (("runner", "containers"), ("dind", "initContainers")):
        assert (
            container(blue, name, key)["resources"]
            == container(deploy, name, key)["resources"]
        ), name
