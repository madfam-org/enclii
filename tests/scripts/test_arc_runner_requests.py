"""ARC runner pods: requests sized to a typical job, limits to the largest.

The scheduler packs builder nodes by REQUEST. At 400m / 2Gi, with no limit
sized to real peaks, it over-packed a builder that then ran out of memory and
went NotReady (2026-10-24). From 2026-10-26 the request was the p90 peak
(1 CPU / 4Gi), which reserved a heavy job's peak for every job.

Since 2026-11-04 (owner decision, from 2 days / 3,436 runner pods: median
0.23 CPU / 0.3GiB, p95 1.37 CPU / 4.75GiB, max 6GiB) the runner requests
500m / 2.5Gi with a 2 CPU / 7Gi limit; see the comment on the runner container
in infra/k8s/production/arc/runner-blue/rendered.yaml.

These tests keep the places that carry the runner template in agreement (the
Helm values, both rendered pools, and the blue-green switch script's
maxRunners) and stop a later edit from quietly cutting a request below the
approved floor or a limit below the measured peak.
"""

import subprocess
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
POOLS = ("blue", "deploy")

# Floors, not exact values: raising a request is a capacity decision anyone
# may make with data; lowering one below the approved sizing is the defect.
RUNNER_MIN_CPU_MILLI = 500  # above the measured median peak (0.23)
RUNNER_MIN_MEMORY_MI = 2560  # 2.5Gi; median peak 0.3GiB
DIND_MIN_CPU_MILLI = 100
DIND_MIN_MEMORY_MI = 512
# Limits must stay above the measured peaks (2026-11-04): p95 CPU 1.37 cores,
# max memory 6GiB. A memory limit at the max is a cgroup OOM for the largest
# job, the 2026-07-21 dhanam failure (no logs, reaped as a dead worker).
RUNNER_MIN_CPU_LIMIT_MILLI = 1370
RUNNER_MIN_MEMORY_LIMIT_MI = 6 * 1024 + 512


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


def test_rendered_runner_limits_cover_measured_peaks():
    for pool in POOLS:
        limits = container(pod_spec(pool), "runner")["resources"]["limits"]
        assert cpu_milli(limits["cpu"]) >= RUNNER_MIN_CPU_LIMIT_MILLI, pool
        assert memory_mi(limits["memory"]) >= RUNNER_MIN_MEMORY_LIMIT_MI, pool
        assert "ephemeral-storage" in limits, pool


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
    values_resources = container(values["template"]["spec"], "runner")["resources"]
    for pool in POOLS:
        rendered = container(pod_spec(pool), "runner")["resources"]
        for kind in ("requests", "limits"):
            assert cpu_milli(rendered[kind]["cpu"]) == cpu_milli(
                values_resources[kind]["cpu"]
            ), (pool, kind)
            assert memory_mi(rendered[kind]["memory"]) == memory_mi(
                values_resources[kind]["memory"]
            ), (pool, kind)
    blue, deploy = (pod_spec(pool) for pool in POOLS)
    for name, key in (("runner", "containers"), ("dind", "initContainers")):
        assert (
            container(blue, name, key)["resources"]
            == container(deploy, name, key)["resources"]
        ), name


def test_blue_cap_agrees_between_values_and_rendered():
    # The one time these drifted (2026-08), a raise lived only in the rendered
    # manifest and the next Helm-path deploy would have halved capacity.
    values = documents("infra/helm/arc/values-runner-set-blue.yaml")[0]
    scale_set = next(
        d
        for d in documents("infra/k8s/production/arc/runner-blue/rendered.yaml")
        if d["kind"] == "AutoscalingRunnerSet"
    )
    assert scale_set["spec"]["maxRunners"] == values["maxRunners"]
    assert scale_set["spec"]["minRunners"] == values["minRunners"]


def switch_script_max_runners(color):
    # Run the script's own function, not a copy of it.
    script = ROOT / "infra/scripts/arc/blue-green-switch.sh"
    text = script.read_text()
    start = text.index("max_runners_for_color() {")
    end = text.index("\n}\n", start) + 3
    result = subprocess.run(
        [
            "bash",
            "-c",
            'log_error() { echo "$1" >&2; }\n'
            + text[start:end]
            + '\nmax_runners_for_color "$1"',
            "bash",
            color,
        ],
        env={"REPO_ROOT": str(ROOT), "PATH": "/usr/bin:/bin"},
        capture_output=True,
        text=True,
        check=True,
    )
    return int(result.stdout.strip())


def test_blue_green_switch_uses_the_values_file_cap():
    # It hardcoded blue=12 while blue ran 20: the break-glass switch would
    # have clamped the pool it was restoring.
    for color in ("blue", "green"):
        values = documents(f"infra/helm/arc/values-runner-set-{color}.yaml")[0]
        assert switch_script_max_runners(color) == values["maxRunners"], color
