"""The descheduler may only move pods that are cheap to move, a few at a time.

infra/k8s/production/descheduler/policy.yaml is what the CronJob reads. These
checks pin the guarantees its header promises (owner-approved 2026-11-04), so
a later edit cannot quietly widen what gets evicted.

The policy was also run once through descheduler v0.33.0's own code (policy
loader and validator, then one RunDeschedulerStrategies pass over a fake
cluster). With the control-plane taint it evicted exactly 5 plain app pods,
one per namespace, and none of: emptyDir, hostPath, PVC, DaemonSet, young,
control-plane-pinned or taint-tolerating pods, pods in excluded namespaces, or
anything on a builder. Without the taint, RemoveDuplicates moved one replica
and stopped at the per-namespace cap, and LowNodeUtilization drained the hot
node until it fell below target.
"""

import re
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
DIR = ROOT / "infra/k8s/production/descheduler"

# The owner's hard exclusions. More may be added (only stricter); none removed.
REQUIRED_EXCLUSIONS = {
    "kube-system",
    "arc-system",
    "arc-runners",
    "longhorn-system",
    "data",
    "monitoring",
    "logging",
    "argocd",
    "cnpg-system",
    "external-secrets",
}
TAINT_KEY = "node-role.madfam.io/control-plane"
IMAGE = re.compile(
    r"^registry\.k8s\.io/descheduler/descheduler:v(\d+)\.(\d+)\.(\d+)@sha256:[0-9a-f]{64}$"
)


def load(name):
    return [d for d in yaml.safe_load_all((DIR / name).read_text()) if d]


def policy():
    return load("policy.yaml")[0]


def plugin_args(name):
    (profile,) = policy()["profiles"]
    configs = {c["name"]: c.get("args") or {} for c in profile["pluginConfig"]}
    return configs[name]


def by_kind(docs, kind):
    return [d for d in docs if d["kind"] == kind]


def test_policy_shape_and_eviction_caps():
    p = policy()
    assert p["apiVersion"] == "descheduler/v1alpha2"
    assert p["kind"] == "DeschedulerPolicy"
    assert p["maxNoOfPodsToEvictTotal"] == 5
    assert p["maxNoOfPodsToEvictPerNamespace"] == 1
    # Builders are never processed, and nodeFit only counts non-builders.
    assert p["nodeSelector"] == "role!=builder"
    assert plugin_args("DefaultEvictor")["nodeSelector"] == "role!=builder"
    assert "metricsProviders" not in p and "metricsCollector" not in p


def test_only_the_three_approved_plugins_evict():
    (profile,) = policy()["profiles"]
    plugins = profile["plugins"]
    assert plugins["deschedule"]["enabled"] == ["RemovePodsViolatingNodeTaints"]
    assert sorted(plugins["balance"]["enabled"]) == [
        "LowNodeUtilization",
        "RemoveDuplicates",
    ]
    assert set(plugins) == {"deschedule", "balance"}
    configured = {c["name"] for c in profile["pluginConfig"]}
    assert configured == {
        "DefaultEvictor",
        "RemovePodsViolatingNodeTaints",
        "LowNodeUtilization",
        "RemoveDuplicates",
    }


def test_default_evictor_protects_what_must_not_move():
    args = plugin_args("DefaultEvictor")
    assert args["evictLocalStoragePods"] is False
    assert args["evictDaemonSetPods"] is False
    assert args["evictSystemCriticalPods"] is False
    assert args["evictFailedBarePods"] is False
    assert args["ignorePvcPods"] is True
    assert args["nodeFit"] is True
    assert args["minPodAge"] == "1h"
    assert "priorityThreshold" not in args
    assert "ignorePodsWithoutPDB" not in args


def test_every_plugin_excludes_the_same_namespaces():
    lists = {
        "RemovePodsViolatingNodeTaints": plugin_args("RemovePodsViolatingNodeTaints")[
            "namespaces"
        ],
        "LowNodeUtilization": plugin_args("LowNodeUtilization")["evictableNamespaces"],
        "RemoveDuplicates": plugin_args("RemoveDuplicates")["namespaces"],
    }
    excluded = None
    for name, namespaces in lists.items():
        assert set(namespaces) == {"exclude"}, name  # no include list
        current = namespaces["exclude"]
        assert len(current) == len(set(current)), name
        assert REQUIRED_EXCLUSIONS <= set(current), name
        assert "descheduler" in current, name
        if excluded is None:
            excluded = set(current)
        assert set(current) == excluded, name


def test_taint_plugin_acts_only_on_the_soft_control_plane_taint():
    args = plugin_args("RemovePodsViolatingNodeTaints")
    assert args["includePreferNoSchedule"] is True
    assert args["includedTaints"] == [TAINT_KEY]
    assert "labelSelector" not in args


def test_low_node_utilization_works_on_requests_with_ordered_bands():
    args = plugin_args("LowNodeUtilization")
    low, high = args["thresholds"], args["targetThresholds"]
    assert {"cpu", "memory"} <= set(low) and set(low) == set(high)
    for resource in low:
        assert 0 < low[resource] < high[resource] <= 100, resource
    assert not args.get("useDeviationThresholds")
    assert "metricsUtilization" not in args


def test_cronjob_runs_the_pinned_image_against_the_policy():
    docs = load("cronjob.yaml")
    (cronjob,) = by_kind(docs, "CronJob")
    spec = cronjob["spec"]
    assert spec["concurrencyPolicy"] == "Forbid"
    job = spec["jobTemplate"]["spec"]
    assert job["backoffLimit"] == 0
    assert job["activeDeadlineSeconds"] <= 900
    pod = job["template"]["spec"]
    assert pod["serviceAccountName"] == "descheduler"
    assert pod["securityContext"]["runAsNonRoot"] is True
    (container,) = pod["containers"]
    assert IMAGE.match(container["image"]), container["image"]
    major, minor = (int(x) for x in IMAGE.match(container["image"]).groups()[:2])
    # v0.33 is the release for the cluster's Kubernetes minor (1.33). A newer
    # release may rename DefaultEvictor's booleans; re-check the policy then.
    assert (major, minor) == (0, 33)
    assert "--policy-config-file=/policy-dir/policy.yaml" in container["args"]
    assert not any(a.startswith("--dry-run") for a in container["args"])
    assert "limits" in container["resources"]
    ctx = container["securityContext"]
    assert ctx["allowPrivilegeEscalation"] is False
    assert ctx["readOnlyRootFilesystem"] is True
    assert ctx["capabilities"]["drop"] == ["ALL"]
    (volume,) = pod["volumes"]
    assert volume["configMap"]["name"] == "descheduler-policy"
    (mount,) = container["volumeMounts"]
    assert mount["name"] == volume["name"] and mount["mountPath"] == "/policy-dir"


def test_kustomization_ships_policy_file_under_the_mounted_name():
    kustomization = load("kustomization.yaml")[0]
    (generator,) = kustomization["configMapGenerator"]
    assert generator["name"] == "descheduler-policy"
    assert generator["files"] == ["policy.yaml"]
    assert generator["options"]["disableNameSuffixHash"] is True
    assert set(kustomization["resources"]) == {
        "namespace.yaml",
        "rbac.yaml",
        "cronjob.yaml",
    }


def test_rbac_evicts_through_the_eviction_api_only():
    (role,) = by_kind(load("rbac.yaml"), "ClusterRole")
    for rule in role["rules"]:
        if "pods" in rule["resources"]:
            assert set(rule["verbs"]) <= {"get", "list", "watch"}, rule
        if "pods/eviction" in rule["resources"]:
            assert rule["verbs"] == ["create"], rule
        assert "*" not in rule["verbs"] and "*" not in rule["resources"], rule
        assert "secrets" not in rule["resources"], rule
    assert any("pods/eviction" in r["resources"] for r in role["rules"])


def test_namespace_and_argo_application_agree():
    (namespace,) = load("namespace.yaml")
    assert namespace["metadata"]["name"] == "descheduler"
    assert namespace["metadata"]["labels"]["enclii.dev/type"] == "infrastructure"
    app = [
        d
        for d in yaml.safe_load_all(
            (ROOT / "infra/argocd/apps/descheduler.yaml").read_text()
        )
        if d
    ][0]
    assert app["spec"]["source"]["path"] == "infra/k8s/production/descheduler"
    assert app["spec"]["destination"]["namespace"] == "descheduler"
    assert app["spec"]["syncPolicy"]["automated"] == {"prune": True, "selfHeal": True}
