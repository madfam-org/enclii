"""
Structural guard: every link managed-Postgres addon backups depend on is in
git, and the links agree with each other and with the Go code.

Run with:
    pytest tests/scripts/test_addon_backup_wiring.py -v

THE CHAIN THIS PINS (2026-10-26)
================================
No addon had ever been backed up: AddonBackupNeverCompleted fired for every
live addon cluster. Four links were missing, each enough on its own:

  intake target enclii/addon-db-backups
    -> secret/enclii:addon_backup_access_key_id / addon_backup_secret_access_key
    -> ExternalSecret enclii-db-backup-credentials (listed in the production
       kustomization; extracts secret/enclii and templates exactly two keys),
       writing Secret enclii/enclii-db-backup-credentials with ACCESS_KEY_ID
       and SECRET_ACCESS_KEY, the names the provisioner puts in s3Credentials
    -> switchyard-api env ENCLII_ADDON_BACKUP_DESTINATION_BASE (store on)
    -> switchyard-api ClusterRole may create/get/patch/delete scheduledbackups
       (patch: a hibernated cluster's schedule is suspended, then resumed)
       and list/delete backups (2026-11-01: a Backup stuck in
       walArchivingFailing wedges its schedule until it is deleted)
    -> per-cluster backup-egress NetworkPolicy, spec.backup, ScheduledBackup
       (Go: internal/addons/postgres_backup.go, tested there).

Each link fails silently, which is why the problem finder is a pure function
and the negative tests prove it catches each break on a mutated copy.
"""
from __future__ import annotations

import copy
import re
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
PROD = REPO_ROOT / "infra" / "k8s" / "production"
RBAC = REPO_ROOT / "infra" / "k8s" / "base" / "rbac.yaml"
ADDONS = REPO_ROOT / "apps" / "switchyard-api" / "internal" / "addons"
REGISTRY = REPO_ROOT / "apps" / "switchyard-api" / "internal" / "secretsintake" / "registry.yaml"

TARGET_ID = "enclii/addon-db-backups"
ES_NAME = "enclii-db-backup-credentials"


def go_constant(name: str) -> str:
    text = (ADDONS / "types.go").read_text()
    m = re.search(rf'^\s*{name}\s*=\s*"([^"]+)"', text, re.M)
    assert m, f"{name} not found in types.go"
    return m.group(1)


def go_credential_keys() -> list[str]:
    text = (ADDONS / "postgres_backup.go").read_text()
    m = re.search(r"backupCredentialKeys = \[\]string\{([^}]*)\}", text)
    assert m, "backupCredentialKeys not found in postgres_backup.go"
    return re.findall(r'"([^"]+)"', m.group(1))


def live() -> dict:
    kust = yaml.safe_load((PROD / "kustomization.yaml").read_text())
    es_docs = []
    for res in kust.get("resources", []):
        p = PROD / res
        if p.is_file() and p.name.endswith(".externalsecret.yaml"):
            es_docs.extend(d for d in yaml.safe_load_all(p.read_text()) if isinstance(d, dict))
    env_docs = [d for d in yaml.safe_load_all((PROD / "environment-patch.yaml").read_text()) if isinstance(d, dict)]
    rbac_docs = [d for d in yaml.safe_load_all(RBAC.read_text()) if isinstance(d, dict)]
    return {
        "externalsecrets": es_docs,
        "env": env_docs,
        "rbac": rbac_docs,
        "registry": yaml.safe_load(REGISTRY.read_text())["targets"],
    }


def problems(state: dict) -> list[str]:
    out: list[str] = []
    secret_name = go_constant("BackupCredentialsSecretName")
    source_ns = go_constant("BackupCredentialsSourceNS")
    keys = go_credential_keys()

    target = state["registry"].get(TARGET_ID)
    if target is None:
        return [f"intake target {TARGET_ID} missing"]
    if target.get("namespace") != source_ns:
        out.append(f"intake target namespace {target.get('namespace')} != {source_ns}")
    if (target.get("generate") or {}).get("disabled") is not True:
        out.append("intake target must not be generatable: an R2 token is minted by Cloudflare")

    es = [d for d in state["externalsecrets"] if d.get("kind") == "ExternalSecret" and d["metadata"]["name"] == ES_NAME]
    if len(es) != 1:
        out.append(f"expected one ExternalSecret {ES_NAME} in the production kustomization, found {len(es)}")
    else:
        spec = es[0]["spec"]
        tgt = spec["target"]
        if tgt["name"] != secret_name:
            out.append(f"ExternalSecret writes {tgt['name']}, provisioner replicates {secret_name}")
        if tgt.get("creationPolicy") != "Owner":
            out.append("sole writer must be creationPolicy: Owner (Merge never creates the Secret)")
        template = tgt.get("template") or {}
        if template.get("mergePolicy", "Replace") != "Replace":
            out.append("template must replace the data: the Secret is copied into every addon namespace")
        tdata = template.get("data") or {}
        if sorted(tdata) != sorted(keys):
            out.append(f"Secret keys {sorted(tdata)} != s3Credentials keys {keys}")
        for key, value in tdata.items():
            m = re.fullmatch(r"\{\{\s*\.([a-z0-9_]+)\s*\}\}", str(value))
            if not m or m.group(1) not in target["keys"]:
                out.append(f"{key} renders {value!r}, which is not one property the intake target writes")
        extracts = [d.get("extract", {}).get("key") for d in spec.get("dataFrom", [])]
        if extracts != [target["vault_path"]]:
            out.append(f"ExternalSecret extracts {extracts}, intake writes {target['vault_path']}")
        if spec.get("data"):
            out.append("ExternalSecret must not also map data entries")

    env = {}
    for doc in state["env"]:
        if doc.get("kind") == "Deployment" and doc["metadata"]["name"] == "switchyard-api":
            for c in doc["spec"]["template"]["spec"]["containers"]:
                env.update({e["name"]: e.get("value") for e in c.get("env", [])})
    base = env.get("ENCLII_ADDON_BACKUP_DESTINATION_BASE") or ""
    if not base.startswith("s3://"):
        out.append("switchyard-api has no ENCLII_ADDON_BACKUP_DESTINATION_BASE: backups stay off")
    if base.rstrip("/").endswith("enclii-backups"):
        out.append("addon backups must not share the platform enclii-backups bucket")
    if not (env.get("ENCLII_ADDON_BACKUP_ENDPOINT_URL") or "").startswith("https://"):
        out.append("switchyard-api has no ENCLII_ADDON_BACKUP_ENDPOINT_URL")

    required = {
        "scheduledbackups": ("create", "get", "patch", "delete"),
        "backups": ("list", "delete"),
    }
    verbs: dict[str, set[str]] = {resource: set() for resource in required}
    for doc in state["rbac"]:
        if doc.get("kind") == "ClusterRole" and doc["metadata"]["name"] == "switchyard-api":
            for rule in doc.get("rules", []):
                if "postgresql.cnpg.io" not in rule.get("apiGroups", []):
                    continue
                for resource in required:
                    if resource in rule.get("resources", []):
                        verbs[resource].update(rule.get("verbs", []))
    for resource, needed in required.items():
        for verb in needed:
            if verb not in verbs[resource]:
                out.append(f"switchyard-api ClusterRole cannot {verb} {resource}")
    return out


def test_live_chain_is_complete():
    assert problems(live()) == []


def test_finder_catches_each_broken_link():
    base = live()

    def mutated(fn):
        state = copy.deepcopy(base)
        fn(state)
        return problems(state)

    def es(state):
        return next(d for d in state["externalsecrets"] if d["metadata"]["name"] == ES_NAME)

    def env(state):
        doc = next(d for d in state["env"] if d.get("kind") == "Deployment")
        return doc["spec"]["template"]["spec"]["containers"][0]["env"]

    def cluster_role(state):
        return next(d for d in state["rbac"] if d.get("kind") == "ClusterRole" and d["metadata"]["name"] == "switchyard-api")

    cases = {
        "no intake target": lambda s: s["registry"].pop(TARGET_ID),
        "ExternalSecret dropped from kustomization": lambda s: s.__setitem__(
            "externalsecrets", [d for d in s["externalsecrets"] if d["metadata"]["name"] != ES_NAME]),
        "wrong target Secret": lambda s: es(s)["spec"]["target"].__setitem__("name", "backup-creds"),
        "renamed key": lambda s: es(s)["spec"]["target"]["template"]["data"].__setitem__(
            "access-key-id", es(s)["spec"]["target"]["template"]["data"].pop("ACCESS_KEY_ID")),
        "property nobody writes": lambda s: es(s)["spec"]["target"]["template"]["data"].__setitem__(
            "SECRET_ACCESS_KEY", "{{ .secret_key }}"),
        "merged template": lambda s: es(s)["spec"]["target"]["template"].__setitem__("mergePolicy", "Merge"),
        "wrong Vault path": lambda s: es(s)["spec"]["dataFrom"][0]["extract"].__setitem__("key", "secret/data"),
        "Merge policy": lambda s: es(s)["spec"]["target"].__setitem__("creationPolicy", "Merge"),
        "store off": lambda s: env(s).__setitem__(slice(None), [e for e in env(s) if e["name"] != "ENCLII_ADDON_BACKUP_DESTINATION_BASE"]),
        "shared platform bucket": lambda s: next(e for e in env(s) if e["name"] == "ENCLII_ADDON_BACKUP_DESTINATION_BASE").__setitem__("value", "s3://enclii-backups"),
        "no scheduledbackups RBAC": lambda s: cluster_role(s).__setitem__(
            "rules", [r for r in cluster_role(s)["rules"] if "scheduledbackups" not in r.get("resources", [])]),
        "wedged Backup cannot be deleted": lambda s: next(
            r for r in cluster_role(s)["rules"] if r.get("resources") == ["backups"]
        )["verbs"].remove("delete"),
        "generatable token": lambda s: s["registry"][TARGET_ID].pop("generate"),
    }
    for name, fn in cases.items():
        assert mutated(fn), f"finder missed: {name}"
