"""
Guard: the Redpanda brokers request a fraction of a core, in the live
Application and in its mirror.

Run with:
    pytest tests/scripts/test_redpanda_cpu_request.py -v

WHY
===
The Redpanda chart renders a CPU limit and no request, so Kubernetes sets the
request equal to the 1-core limit. #413 (2026-08-17) tried to lower it with
`overprovisioned: true` in `infra/helm/redpanda/values.yaml`, but that flag
only adds `--overprovisioned`, and the mirror is not what Argo renders: the
live Application (`infra/argocd/apps/redpanda.yaml`) kept `false`. On
2026-11-01 the three brokers still reserved 3000m of the two workload nodes
against a 7-day max of 80m each.

The request is now set through `statefulset.podTemplate.spec.containers`,
which the chart merges into the PodSpec by container name. This test fails if
that block disappears from either file, if the two disagree, or if the request
climbs back to the limit.
"""
from __future__ import annotations

from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
APPLICATION = REPO_ROOT / "infra" / "argocd" / "apps" / "redpanda.yaml"
MIRROR = REPO_ROOT / "infra" / "helm" / "redpanda" / "values.yaml"


def _millicores(value: str | int | float) -> int:
    text = str(value)
    if text.endswith("m"):
        return int(text[:-1])
    return int(float(text) * 1000)


def _application_values() -> dict:
    app = yaml.safe_load(APPLICATION.read_text())
    return yaml.safe_load(app["spec"]["source"]["helm"]["values"])


def _mirror_values() -> dict:
    return yaml.safe_load(MIRROR.read_text())


def _broker_cpu_request(values: dict) -> str:
    containers = values["statefulset"]["podTemplate"]["spec"]["containers"]
    brokers = [c for c in containers if c.get("name") == "redpanda"]
    assert len(brokers) == 1, "podTemplate must patch exactly one `redpanda` container"
    return brokers[0]["resources"]["requests"]["cpu"]


def test_application_sets_a_fractional_cpu_request() -> None:
    values = _application_values()
    request = _millicores(_broker_cpu_request(values))
    limit = _millicores(values["resources"]["cpu"]["cores"])
    assert 0 < request < limit, f"request {request}m must be below the {limit}m limit"


def test_mirror_matches_the_application() -> None:
    live, mirror = _application_values(), _mirror_values()
    assert _broker_cpu_request(mirror) == _broker_cpu_request(live)
    assert mirror["resources"]["cpu"] == live["resources"]["cpu"]
