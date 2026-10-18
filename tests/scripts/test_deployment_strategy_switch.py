"""
Guard: no Deployment is switched to `strategy.type: Recreate` by a manifest
edit, because under Argo's server-side apply that switch never lands.

Run with:
    pytest tests/scripts/test_deployment_strategy_switch.py -v

WHY (2026-10-18, #799 then #804)
================================
#799 changed the Grafana Deployment from the default RollingUpdate to
`type: Recreate`, so the old pod would release its ReadWriteOnce volume first.
The monitoring Argo app applies server-side (`ServerSideApply=true`, like most
apps in infra/argocd/apps). The live object kept the `rollingUpdate` block the
API server had defaulted, and the API refuses Recreate next to it:

    spec.strategy.rollingUpdate: Forbidden: may not be specified when
    strategy `type` is 'Recreate'

The sync retried and never landed; the monitoring app sat Degraded until #804
kept RollingUpdate with `maxSurge: 0` / `maxUnavailable: 1`. With one replica
that is the same terminate-first rollout, and the API accepts it.

WHAT THIS CHECKS
================
- Every Deployment under infra/k8s/ and apps/*/k8s/ whose strategy is
  Recreate must be listed in KNOWN_RECREATE. The listed ones were already
  Recreate when this guard was added; the trap is the SWITCH, not the type.
- A new entry is fine for a Deployment that has never existed in the cluster
  as RollingUpdate (it is created as Recreate, so no defaulted block exists).
  For an existing Deployment use RollingUpdate with maxSurge 0 and
  maxUnavailable 1 instead, as grafana.yaml does.
- The allow-list cannot go stale: every entry must still exist and still be
  Recreate.
- Grafana keeps the #804 shape.

The problem finder is a pure function, so the negative test can prove it
catches the #799 edit on a mutated copy of the live manifest.
"""
from __future__ import annotations

import copy
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]

# (repo-relative path, Deployment name) -> why it may be Recreate.
KNOWN_RECREATE = {
    ("infra/k8s/base/postgres.yaml", "postgres"): "single writer on one volume",
    ("infra/k8s/platform-infra/postgres.yaml", "postgres"): "single writer on one volume",
    ("infra/k8s/base/redis.yaml", "redis"): "single instance on one volume",
    ("infra/k8s/base/verdaccio/deployment.yaml", "verdaccio"): "storage volume is ReadWriteOnce",
    ("infra/k8s/dr-test/ephemeral-postgres.yaml", "dr-postgres"): "single-pod ephemeral drill target",
    ("infra/k8s/production/monitoring/alertmanager.yaml", "alertmanager"): "data volume is ReadWriteOnce",
    ("infra/k8s/production/monitoring/prometheus.yaml", "prometheus"): "TSDB volume lock",
    ("infra/k8s/production/monitoring/weighbridge.yaml", "weighbridge"): "two controllers must not overlap",
}

# A scan that reads nothing must not pass. 60 Deployments on 2026-10-18.
MIN_DEPLOYMENTS = 40


def manifest_files() -> list[Path]:
    files = list((REPO_ROOT / "infra" / "k8s").rglob("*.yaml"))
    files += list((REPO_ROOT / "infra" / "k8s").rglob("*.yml"))
    files += list(REPO_ROOT.glob("apps/*/k8s/**/*.yaml"))
    files += list(REPO_ROOT.glob("apps/*/k8s/**/*.yml"))
    return sorted(set(files))


def load_deployments() -> list[tuple[str, dict]]:
    out: list[tuple[str, dict]] = []
    for path in manifest_files():
        rel = path.relative_to(REPO_ROOT).as_posix()
        for doc in yaml.safe_load_all(path.read_text(encoding="utf-8")):
            if isinstance(doc, dict) and doc.get("kind") == "Deployment":
                out.append((rel, doc))
    return out


def strategy_type(dep: dict) -> str | None:
    return ((dep.get("spec") or {}).get("strategy") or {}).get("type")


def find_problems(deployments: list[tuple[str, dict]], known: dict) -> list[str]:
    problems: list[str] = []
    seen_recreate = set()
    for rel, dep in deployments:
        if strategy_type(dep) != "Recreate":
            continue
        key = (rel, (dep.get("metadata") or {}).get("name"))
        seen_recreate.add(key)
        if key not in known:
            problems.append(
                f"{key[0]}: Deployment {key[1]} uses strategy.type Recreate and is not in "
                "KNOWN_RECREATE. Switching an existing Deployment to Recreate never syncs "
                "under ServerSideApply (#799); use RollingUpdate with maxSurge 0 and "
                "maxUnavailable 1 (#804). If it is brand new, add it to KNOWN_RECREATE."
            )
    for key in known:
        if key not in seen_recreate:
            problems.append(
                f"KNOWN_RECREATE lists {key[0]} Deployment {key[1]}, which is no longer a "
                "Recreate Deployment there: remove the entry"
            )
    return problems


def grafana(deployments: list[tuple[str, dict]]) -> dict:
    matches = [
        d for rel, d in deployments
        if rel == "infra/k8s/production/monitoring/grafana.yaml"
        and d["metadata"]["name"] == "grafana"
    ]
    assert len(matches) == 1, f"expected one Deployment/grafana, found {len(matches)}"
    return matches[0]


# --- live manifests ---------------------------------------------------------


def test_scan_reads_the_manifests():
    deployments = load_deployments()
    assert len(deployments) >= MIN_DEPLOYMENTS, (
        f"read {len(deployments)} Deployments, expected at least {MIN_DEPLOYMENTS}"
    )


def test_no_unlisted_recreate_deployment():
    assert find_problems(load_deployments(), KNOWN_RECREATE) == []


def test_grafana_rolls_terminate_first_without_recreate():
    strategy = grafana(load_deployments())["spec"]["strategy"]
    assert strategy["type"] == "RollingUpdate"
    assert strategy["rollingUpdate"] == {"maxSurge": 0, "maxUnavailable": 1}


# --- negative tests ---------------------------------------------------------


def test_catches_the_799_edit():
    deployments = copy.deepcopy(load_deployments())
    dep = grafana(deployments)
    dep["spec"]["strategy"] = {"type": "Recreate"}
    problems = find_problems(deployments, KNOWN_RECREATE)
    assert any("grafana" in p and "#804" in p for p in problems), problems


def test_catches_a_stale_allow_list_entry():
    known = dict(KNOWN_RECREATE)
    known[("infra/k8s/production/monitoring/grafana.yaml", "grafana")] = "test"
    problems = find_problems(load_deployments(), known)
    assert any("no longer a Recreate" in p and "grafana" in p for p in problems), problems
