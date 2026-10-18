"""
Structural guard: Grafana's admin password reaches Grafana's database from the
intake target `monitoring/grafana-admin`, and the chain cannot break Grafana
before the first submit.

Run with:
    pytest tests/scripts/test_grafana_admin_secret.py -v

THE CHAIN THIS PINS (2026-10-17)
================================
  intake target monitoring/grafana-admin
    -> secret/monitoring:grafana_admin_password
    -> ExternalSecret grafana-admin, listed in kustomization.yaml, the one the
       target force-syncs, writing Secret grafana-admin key admin-password
    -> a Secret volume in the Grafana Deployment, `optional: true` (absent
       before the first submit; a required volume would leave Grafana Pending)
    -> mounted as a DIRECTORY (no subPath) in the admin-password-sync init
       container, at the directory its script reads
    -> grafana-cli reset-admin-password reads it on stdin, never in argv.

Grafana reads GF_SECURITY_ADMIN_PASSWORD only on first run (the database is on
a PVC), which is why an init container, not the env var, applies the value.

The problem finder is a pure function so the negative tests can prove it
catches each broken link on a mutated copy of the live manifests.
"""
from __future__ import annotations

import copy
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
REGISTRY = (
    REPO_ROOT / "apps" / "switchyard-api" / "internal" / "secretsintake" / "registry.yaml"
)

TARGET_ID = "monitoring/grafana-admin"
INIT_NAME = "admin-password-sync"
SECRET_DIR = "/etc/grafana-admin"
SECRET_KEY = "admin-password"  # pragma: allowlist secret


def load_docs() -> list[dict]:
    kust = yaml.safe_load((MONITORING / "kustomization.yaml").read_text())
    docs: list[dict] = []
    for res in kust.get("resources", []):
        p = MONITORING / res
        if p.is_file():
            docs.extend(d for d in yaml.safe_load_all(p.read_text()) if isinstance(d, dict))
    return docs


def live_objects() -> tuple[dict, list[dict], dict]:
    docs = load_docs()
    deps = [
        d for d in docs
        if d.get("kind") == "Deployment" and d["metadata"]["name"] == "grafana"
    ]
    assert len(deps) == 1, f"expected one Deployment/grafana, found {len(deps)}"
    externalsecrets = [d for d in docs if d.get("kind") == "ExternalSecret"]
    target = yaml.safe_load(REGISTRY.read_text())["targets"][TARGET_ID]
    return deps[0], externalsecrets, target


def es_target_name(es: dict) -> str:
    target = (es.get("spec") or {}).get("target") or {}
    return target.get("name") or es["metadata"]["name"]


def find_problems(dep: dict, externalsecrets: list[dict], target: dict) -> list[str]:
    problems: list[str] = []

    # registry -> ExternalSecret
    if target.get("vault_path") != "secret/monitoring":
        problems.append(f"target vault_path is {target.get('vault_path')}, not secret/monitoring")
    if target.get("keys") != ["grafana_admin_password"]:
        problems.append(f"target keys are {target.get('keys')}, not [grafana_admin_password]")
    es_name = target.get("external_secret")
    matches = [e for e in externalsecrets if e["metadata"]["name"] == es_name]
    if len(matches) != 1:
        problems.append(
            f"target force-syncs ExternalSecret {es_name}, which is not in kustomization.yaml"
        )
        return problems
    es = matches[0]
    secret_name = es_target_name(es)
    if secret_name == "grafana-credentials":  # pragma: allowlist secret
        problems.append("ExternalSecret writes grafana-credentials: it would adopt the hand-made Secret")
    tgt = es["spec"].get("target") or {}
    if tgt.get("creationPolicy", "Owner") != "Owner":
        problems.append("ExternalSecret creationPolicy is not Owner: nothing would create the Secret")
    if tgt.get("deletionPolicy") != "Retain":
        problems.append("ExternalSecret deletionPolicy is not Retain")
    data = es["spec"].get("data") or []
    if [d.get("secretKey") for d in data] != [SECRET_KEY]:
        problems.append(f"ExternalSecret writes {[d.get('secretKey') for d in data]}, not [{SECRET_KEY}]")
    else:
        ref = data[0]["remoteRef"]
        if ref.get("key") != target.get("vault_path") or ref.get("property") not in (target.get("keys") or []):
            problems.append(
                f"ExternalSecret reads {ref.get('key')}:{ref.get('property')}, "
                "not what the intake target writes"
            )

    # ExternalSecret -> Deployment volume -> init container mount
    spec = dep["spec"]["template"]["spec"]
    volumes = [v for v in spec.get("volumes") or [] if (v.get("secret") or {}).get("secretName") == secret_name]
    if len(volumes) != 1:
        problems.append(f"Deployment has {len(volumes)} Secret volumes for {secret_name}, expected 1")
        return problems
    volume = volumes[0]
    if volume["secret"].get("optional") is not True:
        problems.append(
            f"Secret volume {volume['name']} is not `optional: true`; before the first "
            "submit Grafana would stay Pending"
        )
    inits = [c for c in spec.get("initContainers") or [] if c["name"] == INIT_NAME]
    if len(inits) != 1:
        problems.append(f"Deployment has no {INIT_NAME} init container")
        return problems
    init = inits[0]
    mounts = [m for m in init.get("volumeMounts") or [] if m["name"] == volume["name"]]
    if len(mounts) != 1:
        problems.append(f"{INIT_NAME} does not mount volume {volume['name']}")
    else:
        if mounts[0].get("subPath"):
            problems.append(f"{INIT_NAME} mounts {volume['name']} with subPath")
        if mounts[0]["mountPath"].rstrip("/") != SECRET_DIR:
            problems.append(f"{INIT_NAME} mounts {volume['name']} at {mounts[0]['mountPath']}, not {SECRET_DIR}")
    data_mounts = [m for m in init.get("volumeMounts") or [] if m["mountPath"] == "/var/lib/grafana"]
    if not data_mounts:
        problems.append(f"{INIT_NAME} does not mount Grafana's data directory")
    script = "\n".join(init.get("command") or []) + "\n".join(init.get("args") or [])
    if f"{SECRET_DIR}/{SECRET_KEY}" not in script:
        problems.append(f"{INIT_NAME} does not read {SECRET_DIR}/{SECRET_KEY}")
    if "--password-from-stdin" not in script:
        problems.append(f"{INIT_NAME} does not pass the password on stdin")
    if "exit 0" not in script:
        problems.append(f"{INIT_NAME} has no clean exit when the Secret is absent")
    images = {c["image"] for c in spec.get("containers") or [] if c["name"] == "grafana"}
    if init.get("image") not in images:
        problems.append(f"{INIT_NAME} image differs from the grafana container's image")
    return problems


# --- live manifests ---------------------------------------------------------


def test_live_manifests_have_no_problems():
    assert find_problems(*live_objects()) == []


def test_property_is_the_one_monitoring_secrets_already_names():
    """One copy of the credential: the new reader uses the existing property."""
    path = (
        REPO_ROOT / "infra" / "k8s" / "base" / "external-secrets" / "vault-secrets"
        / "monitoring-secrets.yaml"
    )
    es = yaml.safe_load(path.read_text())
    refs = {(d["remoteRef"]["key"], d["remoteRef"]["property"]) for d in es["spec"]["data"]}
    assert ("secret/monitoring", "grafana_admin_password") in refs


# --- negative tests: each broken link is caught ------------------------------


def _mutated(fn):
    dep, ess, target = copy.deepcopy(live_objects())
    fn(dep, ess, target)
    return find_problems(dep, ess, target)


def _init(dep):
    return next(c for c in dep["spec"]["template"]["spec"]["initContainers"] if c["name"] == INIT_NAME)


def _volume(dep):
    return next(v for v in dep["spec"]["template"]["spec"]["volumes"] if v["name"] == "grafana-admin")


def test_catches_required_volume():
    assert any("optional" in p for p in _mutated(lambda d, e, t: _volume(d)["secret"].update(optional=False)))


def test_catches_subpath_mount():
    def mutate(d, e, t):
        m = next(m for m in _init(d)["volumeMounts"] if m["name"] == "grafana-admin")
        m["subPath"] = SECRET_KEY
    assert any("subPath" in p for p in _mutated(mutate))


def test_catches_wrong_externalsecret_name_in_registry():
    assert any("not in kustomization" in p for p in _mutated(lambda d, e, t: t.update({"external_secret": "monitoring-secrets"})))


def test_catches_adopting_the_hand_made_secret():
    def mutate(d, e, t):
        es = next(x for x in e if x["metadata"]["name"] == "grafana-admin")
        es["spec"]["target"]["name"] = "grafana-credentials"
        _volume(d)["secret"]["secretName"] = "grafana-credentials"  # pragma: allowlist secret
    assert any("adopt" in p for p in _mutated(mutate))


def test_catches_wrong_property():
    def mutate(d, e, t):
        es = next(x for x in e if x["metadata"]["name"] == "grafana-admin")
        es["spec"]["data"][0]["remoteRef"]["property"] = "grafana_smtp_password"
    assert any("not what the intake target writes" in p for p in _mutated(mutate))


def test_catches_password_in_argv():
    def mutate(d, e, t):
        init = _init(d)
        init["command"] = [c.replace("--password-from-stdin", '"$(cat $f)"') for c in init["command"]]
    assert any("stdin" in p for p in _mutated(mutate))


def test_catches_missing_init_container():
    assert any("init container" in p for p in _mutated(lambda d, e, t: d["spec"]["template"]["spec"].update(initContainers=[])))
