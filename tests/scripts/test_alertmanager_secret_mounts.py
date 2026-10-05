"""
Structural guard: every credential file Alertmanager's config reads reaches the
running peers through a chain that can update without a restart.

Run with:
    pytest tests/scripts/test_alertmanager_secret_mounts.py -v

THE REGRESSION THIS PINS (2026-10-05)
=====================================
`smtp_auth_password_file` pointed at a file the StatefulSet mounted with
`subPath` from a hand-made Secret. A subPath mount never receives Secret
updates, so a rotated password reached no peer until a restart, and the Secret
had no writer anyone could find. Email failed 942 of 948 sends on one peer.
The fix (owner decision 2026-10-05) projects the password from Vault through
the `alertmanager-smtp` ExternalSecret into a DIRECTORY mount. This test fails
if any link of that chain, for any `*_file` the config reads, breaks again:

  config `*_file` path
    -> a volumeMount of the alertmanager container that CONTAINS the path as a
       direct child and has NO subPath (and no mount AT the path itself)
    -> a Secret volume with `optional: true` (a missing credential must never
       leave the peers Pending)
    -> an ExternalSecret, listed in kustomization.yaml, whose target is that
       Secret and whose `secretKey` is the file's basename

and, for the SMTP password, the ExternalSecret reads exactly the Vault path and
property the intake target `monitoring/alertmanager-smtp` writes, and is the
ExternalSecret that target force-syncs.

Only the StatefulSet is checked. The legacy Deployment in alertmanager.yaml is
held at `replicas: 0` (the HA cutover is complete) and still carries the
pre-2026-10-05 subPath mounts; it runs nothing.

The problem finder is a pure function so the negative tests below can prove it
catches each broken link on a mutated copy of the live manifests.
"""
from __future__ import annotations

import copy
import posixpath
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
REGISTRY = (
    REPO_ROOT / "apps" / "switchyard-api" / "internal" / "secretsintake" / "registry.yaml"
)

SMTP_TARGET_ID = "monitoring/alertmanager-smtp"
SMTP_FILE_KEY = "smtp_auth_password_file"


# --- loading ----------------------------------------------------------------


def kustomization_files() -> list[Path]:
    kust = yaml.safe_load((MONITORING / "kustomization.yaml").read_text())
    out = []
    for res in kust.get("resources", []):
        p = MONITORING / res
        if p.is_file():
            out.append(p)
    return out


def load_docs() -> list[dict]:
    docs = []
    for path in kustomization_files():
        for doc in yaml.safe_load_all(path.read_text()):
            if isinstance(doc, dict):
                docs.append(doc)
    return docs


def find(docs: list[dict], kind: str, name: str) -> dict:
    hits = [
        d for d in docs
        if d.get("kind") == kind and (d.get("metadata") or {}).get("name") == name
    ]
    assert len(hits) == 1, f"expected exactly one {kind}/{name}, found {len(hits)}"
    return hits[0]


def live_objects() -> tuple[dict, dict, list[dict]]:
    docs = load_docs()
    cm = find(docs, "ConfigMap", "alertmanager-config")
    config = yaml.safe_load(cm["data"]["alertmanager.yml"])
    sts = find(docs, "StatefulSet", "alertmanager")
    externalsecrets = [d for d in docs if d.get("kind") == "ExternalSecret"]
    return config, sts, externalsecrets


# --- the guard --------------------------------------------------------------


def config_files(node, where: str = "") -> list[tuple[str, str]]:
    """Every (yaml-path, file) pair for keys ending in `_file` in the config."""
    out: list[tuple[str, str]] = []
    if isinstance(node, dict):
        for key, val in node.items():
            here = f"{where}.{key}" if where else str(key)
            if str(key).endswith("_file") and isinstance(val, str) and val:
                out.append((here, val))
            else:
                out.extend(config_files(val, here))
    elif isinstance(node, list):
        for i, val in enumerate(node):
            out.extend(config_files(val, f"{where}[{i}]"))
    return out


def es_target_name(es: dict) -> str:
    target = (es.get("spec") or {}).get("target") or {}
    return target.get("name") or es["metadata"]["name"]


def find_problems(config: dict, sts: dict, externalsecrets: list[dict]) -> list[str]:
    spec = sts["spec"]["template"]["spec"]
    container = next(c for c in spec["containers"] if c["name"] == "alertmanager")
    mounts = container.get("volumeMounts") or []
    volumes = {v["name"]: v for v in spec.get("volumes") or []}
    by_target = {es_target_name(es): es for es in externalsecrets}

    problems: list[str] = []
    files = config_files(config)
    if not files:
        problems.append("config reads no *_file at all: the parse is broken, not the data")
    for where, path in files:
        at_path = [m for m in mounts if m["mountPath"] == path]
        if at_path:
            problems.append(
                f"{where}: {path} is itself a mount point"
                f"{' with subPath' if at_path[0].get('subPath') else ''}; "
                "a file mount never receives Secret updates, mount the directory"
            )
            continue
        parent = posixpath.dirname(path)
        owning = [m for m in mounts if m["mountPath"].rstrip("/") == parent]
        if not owning:
            problems.append(
                f"{where}: no volumeMount of the alertmanager container is the "
                f"directory {parent}, so {path} does not exist in the peers"
            )
            continue
        mount = owning[0]
        if mount.get("subPath"):
            problems.append(f"{where}: mount {mount['name']} at {parent} uses subPath")
            continue
        volume = volumes.get(mount["name"])
        if volume is None:
            problems.append(f"{where}: mount {mount['name']} has no volume")
            continue
        if "configMap" in volume:
            continue  # not a credential; nothing to project
        secret = volume.get("secret")
        if secret is None:
            problems.append(f"{where}: volume {mount['name']} is neither Secret nor ConfigMap")
            continue
        if secret.get("optional") is not True:
            problems.append(
                f"{where}: Secret volume {mount['name']} is not `optional: true`; "
                "a missing credential would leave every peer Pending"
            )
        name = secret.get("secretName")
        es = by_target.get(name)
        if es is None:
            problems.append(
                f"{where}: Secret {name} has no ExternalSecret in kustomization.yaml "
                "(a hand-made Secret has no writer and no rotation path)"
            )
            continue
        keys = [d.get("secretKey") for d in (es["spec"].get("data") or [])]
        basename = posixpath.basename(path)
        if basename not in keys:
            problems.append(
                f"{where}: ExternalSecret {es['metadata']['name']} writes keys {keys}, "
                f"not {basename}; the file would be absent"
            )
    return problems


# --- live manifests ---------------------------------------------------------


def test_live_manifests_have_no_problems():
    config, sts, externalsecrets = live_objects()
    assert find_problems(config, sts, externalsecrets) == []


def test_smtp_password_file_is_in_the_smtp_directory_mount():
    config, sts, _ = live_objects()
    path = config["global"][SMTP_FILE_KEY]
    assert path == "/etc/alertmanager/smtp/smtp-password"
    spec = sts["spec"]["template"]["spec"]
    container = next(c for c in spec["containers"] if c["name"] == "alertmanager")
    mount = next(m for m in container["volumeMounts"] if m["name"] == "smtp-secret")
    assert mount["mountPath"] == "/etc/alertmanager/smtp"
    assert "subPath" not in mount
    volume = next(v for v in spec["volumes"] if v["name"] == "smtp-secret")
    assert volume["secret"]["secretName"] == "alertmanager-smtp"  # pragma: allowlist secret
    assert volume["secret"]["optional"] is True


def test_smtp_externalsecret_matches_the_intake_target():
    _, _, externalsecrets = live_objects()
    es = next(e for e in externalsecrets if e["metadata"]["name"] == "alertmanager-smtp")
    target = yaml.safe_load(REGISTRY.read_text())["targets"][SMTP_TARGET_ID]

    assert es["metadata"]["namespace"] == target["namespace"] == "monitoring"
    assert target["external_secret"] == es["metadata"]["name"], (
        "intake force-syncs the target's external_secret; it must be the one "
        "Alertmanager mounts"
    )
    assert es_target_name(es) == "alertmanager-smtp"
    assert es_target_name(es) != "alertmanager-smtp-secret", (
        "must not adopt the retired hand-made Secret"
    )
    assert es["spec"]["target"]["creationPolicy"] == "Owner"
    assert es["spec"]["target"]["deletionPolicy"] == "Retain"
    (entry,) = es["spec"]["data"]
    assert entry["secretKey"] == "smtp-password"  # pragma: allowlist secret
    assert entry["remoteRef"]["key"] == target["vault_path"] == "secret/monitoring"
    assert entry["remoteRef"]["property"] in target["keys"]
    assert target["keys"] == ["alertmanager_smtp_password"]
    assert "generate" not in target, "a Gmail app password is minted by Google, never generated"


def test_smtp_property_is_the_one_monitoring_secrets_already_reads():
    """One copy of the credential: the new reader uses the existing property."""
    path = (
        REPO_ROOT / "infra" / "k8s" / "base" / "external-secrets" / "vault-secrets"
        / "monitoring-secrets.yaml"
    )
    es = yaml.safe_load(path.read_text())
    refs = {(d["remoteRef"]["key"], d["remoteRef"]["property"]) for d in es["spec"]["data"]}
    assert ("secret/monitoring", "alertmanager_smtp_password") in refs


def test_retired_template_is_gone():
    assert not (MONITORING / "alertmanager-smtp-secret.yaml.template").exists()


# --- the guard catches each broken link ------------------------------------


def _container(sts: dict) -> dict:
    spec = sts["spec"]["template"]["spec"]
    return next(c for c in spec["containers"] if c["name"] == "alertmanager")


def _volume(sts: dict, name: str) -> dict:
    return next(v for v in sts["spec"]["template"]["spec"]["volumes"] if v["name"] == name)


def test_catches_a_subpath_file_mount():
    config, sts, ess = live_objects()
    sts = copy.deepcopy(sts)
    mounts = _container(sts)["volumeMounts"]
    for m in mounts:
        if m["name"] == "smtp-secret":
            m["mountPath"] = "/etc/alertmanager/smtp/smtp-password"
            m["subPath"] = "smtp-password"
    problems = find_problems(config, sts, ess)
    assert any("smtp-password is itself a mount point with subPath" in p for p in problems)


def test_catches_a_subpath_directory_mount():
    config, sts, ess = live_objects()
    sts = copy.deepcopy(sts)
    for m in _container(sts)["volumeMounts"]:
        if m["name"] == "smtp-secret":
            m["subPath"] = "smtp"
    assert any("uses subPath" in p for p in find_problems(config, sts, ess))


def test_catches_a_config_path_outside_every_mount():
    config, sts, ess = live_objects()
    config = copy.deepcopy(config)
    config["global"][SMTP_FILE_KEY] = "/etc/alertmanager/smtp-password"
    assert any("does not exist in the peers" in p for p in find_problems(config, sts, ess))


def test_catches_a_required_secret_volume():
    config, sts, ess = live_objects()
    sts = copy.deepcopy(sts)
    del _volume(sts, "smtp-secret")["secret"]["optional"]
    assert any("not `optional: true`" in p for p in find_problems(config, sts, ess))


def test_catches_a_hand_made_secret():
    config, sts, ess = live_objects()
    sts = copy.deepcopy(sts)
    _volume(sts, "smtp-secret")["secret"]["secretName"] = "alertmanager-smtp-secret"  # pragma: allowlist secret
    assert any("has no ExternalSecret" in p for p in find_problems(config, sts, ess))


def test_catches_a_renamed_secret_key():
    config, sts, ess = live_objects()
    ess = copy.deepcopy(ess)
    es = next(e for e in ess if e["metadata"]["name"] == "alertmanager-smtp")
    es["spec"]["data"][0]["secretKey"] = "password"  # pragma: allowlist secret
    assert any("the file would be absent" in p for p in find_problems(config, sts, ess))


def test_courier_credential_passes_through_the_same_guard():
    """The guard is general: the Courier credentials_file is checked too."""
    config, _, _ = live_objects()
    files = dict(config_files(config))
    assert SMTP_FILE_KEY in " ".join(files)
    assert "/etc/alertmanager/courier/courier-alertmanager-secret" in files.values()
