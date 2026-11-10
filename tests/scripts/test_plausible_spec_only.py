"""
Guard: the Plausible CE manifests in infra/k8s/production/plausible/ are a
SPEC. Nothing deploys them, and the spec itself stays deployable when it is
wired.

Run with:
    pytest tests/scripts/test_plausible_spec_only.py -v

WHAT THIS PINS
==============
1. Not wired. No Argo Application, ApplicationSet project config or parent
   kustomization points at the directory, and no Argo source would pick it up
   by directory recursion. Wiring is a deliberate one-file change
   (infra/argocd/projects/plausible/config.json); when that PR lands, it
   updates this guard in the same change.
2. Deployable as written:
   - every image is pinned by digest, and Plausible is >= v3.2.1
     (CVE-2026-8467, the /storybook RCE in 3.0.0-3.2.0);
   - every container has CPU/memory requests and CPU/memory limits;
   - default-deny NetworkPolicy plus per-workload allows, never the combined
     namespaceSelector+podSelector peer, never ingress from 0.0.0.0/0;
   - every secret comes from an ExternalSecret with explicit `data`
     remoteRefs (no `dataFrom`, whose missing keys render as «<no value>»
     and still report SecretSynced), and every secret reference in a
     workload resolves to one of them;
   - persistent volumes are Longhorn, in the high-churn snapshot group;
   - no Secret, Ingress or Argo object lives in the spec.
3. Privacy defaults: the public host, invite-only registration, and
   country-level geolocation only (no MaxMind key, no Google integration).
4. The one known admission gap. Kyverno's restrict-image-registries does not
   admit ghcr.io/plausible/*. The test asserts that the gap is exactly that
   image, so a mirror or an allowlist change shows up here instead of as a
   pod that never starts.

Each check is a pure function over parsed documents; the negative tests prove
each one catches its break on a mutated copy.
"""
from __future__ import annotations

import copy
import fnmatch
import json
import re
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[2]
SPEC_REL = "infra/k8s/production/plausible"
SPEC_DIR = REPO_ROOT / SPEC_REL
ARGO_DIR = REPO_ROOT / "infra" / "argocd"
INFRA_DIR = REPO_ROOT / "infra"
IMAGE_POLICY = REPO_ROOT / "infra" / "k8s" / "base" / "kyverno" / "policies" / "image-policies.yaml"
RUNBOOK = SPEC_DIR / "RUNBOOK.md"

DIGEST_RE = re.compile(r"^[^@\s]+:[^@\s]+@sha256:[0-9a-f]{64}$")
PLAUSIBLE_REPO = "ghcr.io/plausible/community-edition"
PLAUSIBLE_MIN = (3, 2, 1)
PUBLIC_HOST = "https://plausible.madfam.io"
WORKLOAD_KINDS = {"Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob"}
FORBIDDEN_KINDS = {"Secret", "Ingress", "IngressRoute", "HTTPRoute", "Application", "ApplicationSet", "AppProject"}
VAULT_PREFIX = "secret/plausible"
BACKUP_SOURCE = ("secret/enclii", {"addon_backup_access_key_id", "addon_backup_secret_access_key"})


# --------------------------------------------------------------------------
# loading
# --------------------------------------------------------------------------
def spec_files() -> list[Path]:
    return sorted(p for p in SPEC_DIR.glob("*.yaml") if p.name != "kustomization.yaml")


def load_spec_docs() -> list[dict]:
    docs: list[dict] = []
    for path in spec_files():
        docs.extend(d for d in yaml.safe_load_all(path.read_text()) if d)
    return docs


def _this_repo(repo_url: str | None) -> bool:
    # Sources from other repositories cannot deploy files from this one.
    return not repo_url or repo_url.rstrip("/").removesuffix(".git").endswith("madfam-org/enclii")


def argo_sources(argo_dir: Path) -> list[tuple[str, str, bool]]:
    """(origin, path, recurse) for every Argo source path in this repo."""
    out: list[tuple[str, str, bool]] = []
    for cfg in sorted((argo_dir / "projects").glob("*/config.json")):
        data = json.loads(cfg.read_text())
        if _this_repo(data.get("repoURL")):
            out.append((str(cfg.relative_to(REPO_ROOT)), data.get("manifestPath", ""), False))
    for path in sorted(argo_dir.rglob("*.yaml")):
        for doc in yaml.safe_load_all(path.read_text()):
            if not isinstance(doc, dict) or doc.get("kind") not in {"Application", "ApplicationSet"}:
                continue
            spec = doc.get("spec") or {}
            if doc["kind"] == "ApplicationSet":
                spec = (spec.get("template") or {}).get("spec") or {}
            sources = [spec.get("source")] if spec.get("source") else []
            sources += spec.get("sources") or []
            for src in sources:
                if src and src.get("path") and _this_repo(src.get("repoURL")):
                    recurse = bool((src.get("directory") or {}).get("recurse"))
                    out.append((str(path.relative_to(REPO_ROOT)), src["path"], recurse))
    return out


def kustomization_refs(infra_dir: Path) -> list[tuple[str, str]]:
    """(kustomization file, resolved repo-relative ref) for every local ref."""
    out: list[tuple[str, str]] = []
    for kfile in sorted(infra_dir.rglob("kustomization.y*ml")):
        doc = yaml.safe_load(kfile.read_text()) or {}
        refs: list[str] = []
        for key in ("resources", "bases", "components", "patchesStrategicMerge"):
            refs += [r for r in (doc.get(key) or []) if isinstance(r, str)]
        for ref in refs:
            if "://" in ref or ref.startswith("github.com/"):
                continue
            resolved = (kfile.parent / ref).resolve()
            try:
                rel = str(resolved.relative_to(REPO_ROOT))
            except ValueError:
                continue
            out.append((str(kfile.relative_to(REPO_ROOT)), rel))
    return out


def kyverno_registry_globs(policy_path: Path) -> list[str]:
    for doc in yaml.safe_load_all(policy_path.read_text()):
        if doc and doc.get("metadata", {}).get("name") == "restrict-image-registries":
            pattern = doc["spec"]["rules"][0]["validate"]["pattern"]["spec"]["containers"][0]["image"]
            return [g.strip() for g in pattern.split("|")]
    raise AssertionError("restrict-image-registries not found")


# --------------------------------------------------------------------------
# helpers
# --------------------------------------------------------------------------
def pod_spec(doc: dict) -> dict | None:
    kind = doc.get("kind")
    if kind == "CronJob":
        return doc["spec"]["jobTemplate"]["spec"]["template"]["spec"]
    if kind in WORKLOAD_KINDS:
        return doc["spec"]["template"]["spec"]
    return None


def containers(doc: dict) -> list[dict]:
    spec = pod_spec(doc)
    if spec is None:
        return []
    return list(spec.get("initContainers") or []) + list(spec.get("containers") or [])


def images(docs: list[dict]) -> list[str]:
    out = [c["image"] for d in docs for c in containers(d)]
    out += [d["spec"]["imageName"] for d in docs if d.get("kind") == "Cluster"]
    return out


def image_repo(image: str) -> str:
    name = image.split("@", 1)[0]
    return name.rsplit(":", 1)[0] if ":" in name.split("/")[-1] else name


def es_outputs(docs: list[dict]) -> dict[str, set[str]]:
    """target Secret name -> keys it will hold."""
    out: dict[str, set[str]] = {}
    for d in docs:
        if d.get("kind") != "ExternalSecret":
            continue
        target = d["spec"]["target"]
        keys = {e["secretKey"] for e in d["spec"].get("data") or []}
        template = target.get("template") or {}
        if template.get("data"):
            keys = set(template["data"])
        out[target["name"]] = keys
    return out


# --------------------------------------------------------------------------
# checks (pure)
# --------------------------------------------------------------------------
def wiring_problems(sources: list[tuple[str, str, bool]], refs: list[tuple[str, str]], spec_rel: str) -> list[str]:
    problems: list[str] = []
    spec = spec_rel.strip("/")
    for origin, path, recurse in sources:
        p = path.strip("/")
        if p == spec or p.startswith(spec + "/"):
            problems.append(f"{origin} deploys {path}")
        elif spec.startswith(p + "/") and recurse and not (REPO_ROOT / p / "kustomization.yaml").exists():
            problems.append(f"{origin} recurses into {path} without a kustomization")
    for kfile, rel in refs:
        if kfile.startswith(spec + "/"):
            continue
        if rel == spec or rel.startswith(spec + "/"):
            problems.append(f"{kfile} references {rel}")
    return problems


def kustomization_problems(kust: dict, files: list[str]) -> list[str]:
    listed = sorted(kust.get("resources") or [])
    problems = []
    if listed != sorted(files):
        problems.append(f"kustomization resources {listed} != files {sorted(files)}")
    if kust.get("namespace") != "plausible":
        problems.append("kustomization namespace is not plausible")
    return problems


def image_problems(docs: list[dict]) -> list[str]:
    problems = [f"not digest-pinned: {i}" for i in images(docs) if not DIGEST_RE.match(i)]
    plausible = [i for i in images(docs) if image_repo(i) == PLAUSIBLE_REPO]
    if not plausible:
        problems.append("no Plausible image")
    for image in plausible:
        m = re.search(r":v(\d+)\.(\d+)\.(\d+)@", image)
        if not m or tuple(int(x) for x in m.groups()) < PLAUSIBLE_MIN:
            problems.append(f"Plausible below v3.2.1 (CVE-2026-8467): {image}")
    return problems


def resource_problems(docs: list[dict]) -> list[str]:
    problems = []
    for d in docs:
        targets = [(f"{d['kind']}/{d['metadata']['name']}/{c['name']}", c.get("resources") or {}) for c in containers(d)]
        if d.get("kind") == "Cluster":
            targets.append((f"Cluster/{d['metadata']['name']}", d["spec"].get("resources") or {}))
        for name, res in targets:
            for section in ("requests", "limits"):
                for key in ("cpu", "memory"):
                    if not (res.get(section) or {}).get(key):
                        problems.append(f"{name} has no {section}.{key}")
    return problems


def netpol_problems(docs: list[dict]) -> list[str]:
    pols = [d for d in docs if d.get("kind") == "NetworkPolicy"]
    problems = []
    deny = [
        p for p in pols
        if p["spec"].get("podSelector") == {}
        and set(p["spec"].get("policyTypes") or []) == {"Ingress", "Egress"}
        and not p["spec"].get("ingress") and not p["spec"].get("egress")
    ]
    if not deny:
        problems.append("no default-deny-all policy")
    for p in pols:
        for direction, key in (("ingress", "from"), ("egress", "to")):
            for rule in p["spec"].get(direction) or []:
                for peer in rule.get(key) or []:
                    if "namespaceSelector" in peer and "podSelector" in peer:
                        problems.append(f"{p['metadata']['name']}: combined namespace+pod peer")
                    if direction == "ingress" and (peer.get("ipBlock") or {}).get("cidr") == "0.0.0.0/0":
                        problems.append(f"{p['metadata']['name']}: ingress from 0.0.0.0/0")
    allows = [p for p in pols if p not in deny and "Ingress" in (p["spec"].get("policyTypes") or [])]
    labelled = [(f"{d['kind']}/{d['metadata']['name']}", d["spec"]["template"]["metadata"]["labels"]) for d in docs if pod_spec(d) is not None]
    labelled += [(f"Cluster/{d['metadata']['name']}", {"cnpg.io/cluster": d["metadata"]["name"]}) for d in docs if d.get("kind") == "Cluster"]
    for name, labels in labelled:
        if not any(
            (p["spec"].get("podSelector") or {}).get("matchLabels")
            and all(labels.get(k) == v for k, v in p["spec"]["podSelector"]["matchLabels"].items())
            for p in allows
        ):
            problems.append(f"{name} has no ingress allow policy")
    return problems


def forbidden_kind_problems(docs: list[dict]) -> list[str]:
    return [f"{d['kind']}/{d['metadata']['name']} must not be in the spec" for d in docs if d.get("kind") in FORBIDDEN_KINDS]


def externalsecret_problems(docs: list[dict]) -> list[str]:
    problems = []
    for d in docs:
        if d.get("kind") != "ExternalSecret":
            continue
        name = d["metadata"]["name"]
        spec = d["spec"]
        if spec.get("secretStoreRef") != {"name": "vault-store", "kind": "ClusterSecretStore"}:
            problems.append(f"{name}: not on the vault-store ClusterSecretStore")
        if spec.get("dataFrom"):
            problems.append(f"{name}: dataFrom renders missing keys as <no value>; use data")
        if not spec.get("data"):
            problems.append(f"{name}: no data entries")
        for entry in spec.get("data") or []:
            ref = entry.get("remoteRef") or {}
            if not ref.get("key") or not ref.get("property"):
                problems.append(f"{name}/{entry.get('secretKey')}: remoteRef needs key and property")
                continue
            ok = ref["key"] == VAULT_PREFIX or (ref["key"] == BACKUP_SOURCE[0] and ref["property"] in BACKUP_SOURCE[1])
            if not ok:
                problems.append(f"{name}/{entry['secretKey']}: unexpected Vault source {ref['key']}:{ref['property']}")
    return problems


def secret_ref_problems(docs: list[dict]) -> list[str]:
    produced = es_outputs(docs)
    problems: list[str] = []
    refs: list[tuple[str, str, str]] = []
    for d in docs:
        for c in containers(d):
            owner = f"{d['metadata']['name']}/{c['name']}"
            for env in c.get("env") or []:
                ref = (env.get("valueFrom") or {}).get("secretKeyRef")
                if ref:
                    refs.append((owner, ref["name"], ref["key"]))
                    if ref.get("optional") is not False:
                        problems.append(f"{owner}: secretKeyRef {ref['name']}/{ref['key']} must be optional: false")
            for src in c.get("envFrom") or []:
                if src.get("secretRef"):
                    refs.append((f"{d['metadata']['name']}/{c['name']}", src["secretRef"]["name"], "*"))
        if d.get("kind") == "Cluster":
            spec = d["spec"]
            init_secret = ((spec.get("bootstrap") or {}).get("initdb") or {}).get("secret") or {}
            if init_secret:
                refs.append((d["metadata"]["name"], init_secret["name"], "password"))
            creds = ((spec.get("backup") or {}).get("barmanObjectStore") or {}).get("s3Credentials") or {}
            for item in creds.values():
                refs.append((d["metadata"]["name"], item["name"], item["key"]))
    for owner, name, key in refs:
        if name not in produced:
            problems.append(f"{owner}: Secret {name} is not an ExternalSecret target")
        elif key != "*" and key not in produced[name]:
            problems.append(f"{owner}: Secret {name} has no key {key}")
    return problems


def storage_problems(docs: list[dict]) -> list[str]:
    problems = []
    for d in docs:
        claims = []
        if d.get("kind") == "StatefulSet":
            claims = [(c["metadata"]["name"], c["spec"].get("storageClassName"), c["metadata"].get("labels") or {}) for c in d["spec"].get("volumeClaimTemplates") or []]
        if d.get("kind") == "Cluster":
            claims = [("storage", d["spec"]["storage"].get("storageClass"), (d["spec"].get("inheritedMetadata") or {}).get("labels") or {})]
        for name, sc, labels in claims:
            where = f"{d['kind']}/{d['metadata']['name']}/{name}"
            if sc != "longhorn":
                problems.append(f"{where}: storage class {sc!r}, expected longhorn")
            if labels.get("recurring-job-group.longhorn.io/high-churn") != "enabled" or labels.get("recurring-job.longhorn.io/source") != "enabled":
                problems.append(f"{where}: not in the Longhorn high-churn group")
    return problems


def privacy_problems(docs: list[dict]) -> list[str]:
    problems = []
    config = next((d for d in docs if d.get("kind") == "ConfigMap" and d["metadata"]["name"] == "plausible-config"), None)
    if config is None:
        return ["plausible-config ConfigMap missing"]
    data = config.get("data") or {}
    if data.get("BASE_URL") != PUBLIC_HOST:
        problems.append(f"BASE_URL is {data.get('BASE_URL')!r}, expected {PUBLIC_HOST}")
    if data.get("DISABLE_REGISTRATION") != "invite_only":
        problems.append("registration is not invite_only")
    names = set(data)
    for d in docs:
        for c in containers(d):
            names |= {e["name"] for e in c.get("env") or []}
    for banned in ("MAXMIND_LICENSE_KEY", "IP_GEOLOCATION_DB", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"):
        if banned in names:
            problems.append(f"{banned} is set (country-level geolocation only, no Google integration)")
    return problems


def registry_gap(docs: list[dict], globs: list[str]) -> set[str]:
    return {image_repo(i) for i in images(docs) if not any(fnmatch.fnmatch(i, g) for g in globs)}


# --------------------------------------------------------------------------
# tests on the real tree
# --------------------------------------------------------------------------
DOCS = load_spec_docs()


def test_spec_is_not_wired_anywhere():
    assert wiring_problems(argo_sources(ARGO_DIR), kustomization_refs(INFRA_DIR), SPEC_REL) == []


def test_no_plausible_project_config_exists():
    assert not (ARGO_DIR / "projects" / "plausible").exists()


def test_kustomization_lists_exactly_the_spec_files():
    kust = yaml.safe_load((SPEC_DIR / "kustomization.yaml").read_text())
    assert kustomization_problems(kust, [p.name for p in spec_files()]) == []


def test_readme_and_every_manifest_say_spec_only():
    assert "SPEC ONLY" in (SPEC_DIR / "README.md").read_text()
    for path in spec_files() + [SPEC_DIR / "kustomization.yaml"]:
        assert "SPEC ONLY" in path.read_text(), path.name


def test_images_pinned_and_plausible_patched():
    assert image_problems(DOCS) == []


def test_every_container_has_requests_and_limits():
    assert resource_problems(DOCS) == []


def test_network_policies_default_deny():
    assert netpol_problems(DOCS) == []


def test_no_secret_ingress_or_argo_objects():
    assert forbidden_kind_problems(DOCS) == []


def test_externalsecrets_use_explicit_vault_refs():
    assert externalsecret_problems(DOCS) == []


def test_every_secret_reference_resolves():
    assert secret_ref_problems(DOCS) == []


def test_volumes_are_longhorn_high_churn():
    assert storage_problems(DOCS) == []


def test_privacy_defaults():
    assert privacy_problems(DOCS) == []


def test_admission_gap_is_exactly_the_plausible_image():
    gap = registry_gap(DOCS, kyverno_registry_globs(IMAGE_POLICY))
    assert gap == {PLAUSIBLE_REPO}, (
        "The Kyverno registry gap changed. If ghcr.io/plausible is now admitted or mirrored, "
        "update README.md 'Before wiring' and this assertion in the same PR."
    )
    assert "restrict-image-registries" in (SPEC_DIR / "README.md").read_text()


def test_runbook_exists_and_links_back():
    text = RUNBOOK.read_text()
    assert SPEC_REL in text
    for step in ("Intake", "DNS", "Wire", "Smoke", "snippet", "Weekly report"):
        assert step.lower() in text.lower(), step


def test_files_stay_under_800_lines():
    for path in list(SPEC_DIR.iterdir()) + [Path(__file__)]:
        assert len(path.read_text().splitlines()) <= 800, path.name


# --------------------------------------------------------------------------
# negative tests: each check catches its break
# --------------------------------------------------------------------------
def _doc(docs: list[dict], kind: str, name: str) -> dict:
    return next(d for d in docs if d.get("kind") == kind and d["metadata"]["name"] == name)


def test_wiring_catches_project_config_app_path_parent_ref_and_recursion():
    assert wiring_problems([("projects/plausible/config.json", SPEC_REL, False)], [], SPEC_REL)
    assert wiring_problems([("apps/x.yaml", SPEC_REL + "/", False)], [], SPEC_REL)
    assert wiring_problems([], [("infra/k8s/production/kustomization.yaml", SPEC_REL)], SPEC_REL)
    assert wiring_problems([("apps/y.yaml", "infra/k8s/production/plausible-x", False)], [], SPEC_REL) == []
    # A recursing directory source above the spec, with no kustomization there.
    assert wiring_problems([("apps/z.yaml", "infra/k8s", True)], [], SPEC_REL)
    # The spec's own kustomization referencing its files is fine.
    assert wiring_problems([], [(SPEC_REL + "/kustomization.yaml", SPEC_REL + "/plausible.yaml")], SPEC_REL) == []


def test_kustomization_check_catches_orphan_file():
    kust = yaml.safe_load((SPEC_DIR / "kustomization.yaml").read_text())
    assert kustomization_problems(kust, [p.name for p in spec_files()] + ["extra.yaml"])
    kust["namespace"] = "default"
    assert kustomization_problems(kust, [p.name for p in spec_files()])


def test_image_check_catches_tag_only_and_vulnerable_version():
    docs = copy.deepcopy(DOCS)
    _doc(docs, "StatefulSet", "plausible-events-db")["spec"]["template"]["spec"]["containers"][0]["image"] = "docker.io/clickhouse/clickhouse-server:24.12-alpine"
    assert any("not digest-pinned" in p for p in image_problems(docs))
    docs = copy.deepcopy(DOCS)
    c = _doc(docs, "Deployment", "plausible")["spec"]["template"]["spec"]["containers"][0]
    c["image"] = c["image"].replace(":v3.2.1@", ":v3.2.0@")
    assert any("CVE-2026-8467" in p for p in image_problems(docs))
    docs = copy.deepcopy(DOCS)
    _doc(docs, "Cluster", "plausible-db")["spec"]["imageName"] = "ghcr.io/cloudnative-pg/postgresql:16"
    assert image_problems(docs)


def test_resource_check_catches_missing_limit():
    docs = copy.deepcopy(DOCS)
    del _doc(docs, "Deployment", "plausible")["spec"]["template"]["spec"]["initContainers"][1]["resources"]["limits"]["memory"]
    assert resource_problems(docs) == ["Deployment/plausible/migrate has no limits.memory"]
    docs = copy.deepcopy(DOCS)
    del _doc(docs, "Cluster", "plausible-db")["spec"]["resources"]["requests"]["cpu"]
    assert resource_problems(docs) == ["Cluster/plausible-db has no requests.cpu"]


def test_netpol_check_catches_missing_deny_combined_peer_open_ingress_and_uncovered_pod():
    docs = [d for d in copy.deepcopy(DOCS) if not (d.get("kind") == "NetworkPolicy" and d["metadata"]["name"] == "default-deny-all")]
    assert "no default-deny-all policy" in netpol_problems(docs)
    docs = copy.deepcopy(DOCS)
    peer = _doc(docs, "NetworkPolicy", "plausible-web")["spec"]["ingress"][0]["from"][0]
    peer["podSelector"] = {"matchLabels": {"app": "cloudflared"}}
    assert any("combined" in p for p in netpol_problems(docs))
    docs = copy.deepcopy(DOCS)
    _doc(docs, "NetworkPolicy", "plausible-web")["spec"]["ingress"].append({"from": [{"ipBlock": {"cidr": "0.0.0.0/0"}}]})
    assert any("0.0.0.0/0" in p for p in netpol_problems(docs))
    docs = [d for d in copy.deepcopy(DOCS) if not (d.get("kind") == "NetworkPolicy" and d["metadata"]["name"] == "plausible-events-db")]
    assert "StatefulSet/plausible-events-db has no ingress allow policy" in netpol_problems(docs)


def test_forbidden_kinds_caught():
    docs = copy.deepcopy(DOCS) + [{"kind": "Ingress", "metadata": {"name": "plausible"}}, {"kind": "Secret", "metadata": {"name": "x"}}]
    assert len(forbidden_kind_problems(docs)) == 2


def test_externalsecret_check_catches_datafrom_wrong_store_and_foreign_path():
    docs = copy.deepcopy(DOCS)
    es = _doc(docs, "ExternalSecret", "plausible-app-secrets")
    es["spec"]["dataFrom"] = [{"extract": {"key": "secret/plausible"}}]
    es["spec"]["secretStoreRef"]["name"] = "other-store"
    es["spec"]["data"][0]["remoteRef"]["key"] = "secret/dhanam"
    problems = externalsecret_problems(docs)
    assert any("dataFrom" in p for p in problems)
    assert any("vault-store" in p for p in problems)
    assert any("unexpected Vault source secret/dhanam" in p for p in problems)
    docs = copy.deepcopy(DOCS)
    _doc(docs, "ExternalSecret", "plausible-backup-credentials")["spec"]["data"][0]["remoteRef"]["property"] = "r2_root_key"
    assert any("unexpected Vault source" in p for p in externalsecret_problems(docs))


def test_secret_ref_check_catches_unknown_secret_key_and_optional():
    docs = copy.deepcopy(DOCS)
    env = _doc(docs, "Deployment", "plausible")["spec"]["template"]["spec"]["containers"][0]["env"]
    env[0]["valueFrom"]["secretKeyRef"]["key"] = "NOPE"
    assert any("has no key NOPE" in p for p in secret_ref_problems(docs))
    docs = copy.deepcopy(DOCS)
    env = _doc(docs, "StatefulSet", "plausible-events-db")["spec"]["template"]["spec"]["containers"][0]["env"]
    env[1]["valueFrom"]["secretKeyRef"]["optional"] = True
    assert any("optional: false" in p for p in secret_ref_problems(docs))
    docs = copy.deepcopy(DOCS)
    _doc(docs, "Cluster", "plausible-db")["spec"]["bootstrap"]["initdb"]["secret"]["name"] = "hand-made"
    assert any("hand-made is not an ExternalSecret target" in p for p in secret_ref_problems(docs))


def test_storage_check_catches_wrong_class_and_missing_group():
    docs = copy.deepcopy(DOCS)
    claim = _doc(docs, "StatefulSet", "plausible-events-db")["spec"]["volumeClaimTemplates"][0]
    claim["spec"]["storageClassName"] = "local-path"
    del claim["metadata"]["labels"]["recurring-job-group.longhorn.io/high-churn"]
    assert len(storage_problems(docs)) == 2
    docs = copy.deepcopy(DOCS)
    del _doc(docs, "Cluster", "plausible-db")["spec"]["inheritedMetadata"]
    assert storage_problems(docs) == ["Cluster/plausible-db/storage: not in the Longhorn high-churn group"]


def test_privacy_check_catches_open_registration_city_geo_and_wrong_host():
    docs = copy.deepcopy(DOCS)
    data = _doc(docs, "ConfigMap", "plausible-config")["data"]
    data["DISABLE_REGISTRATION"] = "false"
    data["BASE_URL"] = "https://analytics.madfam.io"
    data["MAXMIND_LICENSE_KEY"] = "placeholder"
    assert len(privacy_problems(docs)) == 3


def test_registry_gap_detects_unapproved_registry_and_clears_when_mirrored():
    globs = kyverno_registry_globs(IMAGE_POLICY)
    docs = copy.deepcopy(DOCS)
    for c in containers(_doc(docs, "Deployment", "plausible")):
        c["image"] = c["image"].replace(PLAUSIBLE_REPO, "ghcr.io/madfam-org/enclii/plausible-community-edition")
    assert registry_gap(docs, globs) == set()
    docs = copy.deepcopy(DOCS)
    _doc(docs, "StatefulSet", "plausible-events-db")["spec"]["template"]["spec"]["containers"][0]["image"] = "registry.example.com/ch:1@sha256:" + "0" * 64
    assert "registry.example.com/ch" in registry_gap(docs, globs)
