#!/usr/bin/env python3
"""
check-argocd-ignore-differences.py — CI lint preventing ArgoCD ignore rules that
silently drop writes to CRD lists.

THE BUG THIS PREVENTS
=====================
With ``RespectIgnoreDifferences=true`` in ``syncOptions``, ArgoCD copies the LIVE
values at every ignored path into the DESIRED object before applying it
(``controller/sync.go`` -> ``normalizeTargetResources``). How it copies depends
on the kind:

* built-in kinds resolve in the Kubernetes scheme and take a strategic merge
  patch, which merges lists element-wise by their patch key — safe;
* CRDs do not resolve, so ArgoCD falls back to an RFC 7386 JSON merge patch, in
  which **arrays are atomic**: an ignored path *inside a list* makes the whole
  live list overwrite the desired one.

The ApplicationSet ``project-applications`` carried

    - group: external-secrets.io
      kind: ExternalSecret
      jqPathExpressions:
        - .spec.data[]?.remoteRef.conversionStrategy

so every entry ADDED to an ExternalSecret's ``spec.data`` was discarded on sync,
while the sync still reported ``serverside-applied`` / ``Succeeded`` and the app
sat ``OutOfSync`` forever (nauta-services 2026-09-07; symbiosis-hcm-services
2026-09-23 dropped symbiosis-hcm#145 five times). The runtime reconciler was
fixed first; this lint keeps every YAML under ``infra/argocd/`` in line with it.
See docs/infrastructure/ARGOCD_KNOWN_ISSUES.md.

WHAT IT CHECKS
==============
For every ``Application`` and ``ApplicationSet`` (its ``spec.template``) found in
the given roots:

1. ERROR — ``RespectIgnoreDifferences=true`` is set AND an ``ignoreDifferences``
   rule on a kind that is not built-in (any group outside the Kubernetes scheme,
   or a ``*`` wildcard) has a jq path or JSON pointer that selects inside a list
   (``[]``, ``[]?``, ``[0]`` in jq; a numeric or ``-`` segment in a pointer).
2. ERROR — ``ServerSideDiff`` appears in ``syncOptions`` (ArgoCD v3.2.5 ignores it
   there; it is read only from the compare-options annotation or a controller
   env var, so its presence there is always a mistake).
3. ERROR — ``ServerSideDiff=true`` appears in the
   ``argocd.argoproj.io/compare-options`` annotation. Mirrors the runtime
   reconciler test: SSD's dry-run goes through admission and Kyverno's
   verify-image-signatures denies it on every digest bump of a signature-
   verified Deployment, wedging the whole app on a ComparisonError.

HOW TO FIX A FINDING
====================
* List-selecting rule on a CRD: remove it. If it only hid apiserver-applied CRD
  defaults, spell those defaults out in the manifest in git instead (for ESO:
  ``conversionStrategy: Default``, ``decodingStrategy: None``,
  ``metadataPolicy: None`` on every ``remoteRef``).
* ServerSideDiff: remove it; see the known-issues doc for why.
"""
from __future__ import annotations

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path

import yaml

# API groups registered in the scheme ArgoCD uses for normalisation
# (k8s.io/client-go/kubernetes/scheme). Kinds in these groups take the
# strategic-merge path. Anything else — including "*" — is treated as a CRD.
BUILTIN_GROUPS = frozenset({
    "", "core", "apps", "batch", "policy", "autoscaling", "extensions",
    "networking.k8s.io", "rbac.authorization.k8s.io", "storage.k8s.io",
    "admissionregistration.k8s.io", "coordination.k8s.io", "discovery.k8s.io",
    "scheduling.k8s.io", "node.k8s.io", "certificates.k8s.io", "events.k8s.io",
    "flowcontrol.apiserver.k8s.io", "authentication.k8s.io",
    "authorization.k8s.io", "apiregistration.k8s.io",
})

# jq: `[]`, `[]?`, `[0]`, `[-1]`, `[ ]` select list elements. `["key"]` does not.
JQ_LIST_SELECTOR = re.compile(r"\[\s*-?\d*\s*\]")
# JSON pointer: a segment that is an array index or the append marker `-`.
POINTER_LIST_SEGMENT = re.compile(r"^(?:\d+|-)$")

COMPARE_OPTIONS = "argocd.argoproj.io/compare-options"


@dataclass
class Finding:
    path: Path
    resource: str
    message: str

    def __str__(self) -> str:
        return f"  {self.path}: {self.resource}: {self.message}"


def yaml_files(roots: list[str]) -> list[Path]:
    out: list[Path] = []
    for root in roots:
        p = Path(root)
        if p.is_file():
            out.append(p)
        elif p.is_dir():
            out.extend(sorted(q for q in p.rglob("*") if q.suffix in (".yaml", ".yml")))
    return out


def list_selecting_paths(rule: dict) -> list[str]:
    hits: list[str] = []
    for expr in rule.get("jqPathExpressions") or []:
        if isinstance(expr, str) and JQ_LIST_SELECTOR.search(expr):
            hits.append(f"jq {expr!r}")
    for ptr in rule.get("jsonPointers") or []:
        if isinstance(ptr, str) and any(POINTER_LIST_SEGMENT.match(s) for s in ptr.split("/")[1:]):
            hits.append(f"pointer {ptr!r}")
    return hits


def is_builtin(rule: dict) -> bool:
    group = rule.get("group", "")
    kind = rule.get("kind", "")
    return isinstance(group, str) and group in BUILTIN_GROUPS and kind != "*"


def check_app(path: Path, resource: str, metadata: dict, spec: dict) -> list[Finding]:
    findings: list[Finding] = []
    sync_options = [str(o) for o in ((spec.get("syncPolicy") or {}).get("syncOptions") or [])]
    respect = any(o.replace(" ", "") == "RespectIgnoreDifferences=true" for o in sync_options)

    if any(o.startswith("ServerSideDiff") for o in sync_options):
        findings.append(Finding(path, resource,
            "ServerSideDiff in syncOptions is silently ignored by ArgoCD v3.2.5 — remove it"))

    compare = str(((metadata or {}).get("annotations") or {}).get(COMPARE_OPTIONS, ""))
    if "ServerSideDiff=true" in compare.replace(" ", "").split(","):
        findings.append(Finding(path, resource,
            f"{COMPARE_OPTIONS} carries ServerSideDiff=true — its admission dry-run is denied by "
            "Kyverno on signature-verified Deployments (ComparisonError wedges the app)"))

    if not respect:
        return findings
    for rule in spec.get("ignoreDifferences") or []:
        if not isinstance(rule, dict) or is_builtin(rule):
            continue
        hits = list_selecting_paths(rule)
        if hits:
            findings.append(Finding(path, resource,
                f"ignoreDifferences on {rule.get('group', '')!r}/{rule.get('kind', '')!r} selects inside a "
                f"list ({', '.join(hits)}) with RespectIgnoreDifferences=true — RFC 7386 merge patch "
                "replaces the whole desired list with the live one and drops every added item"))
    return findings


def scan(path: Path) -> tuple[int, list[Finding]]:
    try:
        docs = list(yaml.safe_load_all(path.read_text(encoding="utf-8")))
    except yaml.YAMLError as exc:
        return 0, [Finding(path, "-", f"YAML parse error: {exc}")]
    findings: list[Finding] = []
    apps = 0
    for doc in docs:
        if not isinstance(doc, dict):
            continue
        kind = doc.get("kind")
        name = (doc.get("metadata") or {}).get("name", "?")
        if kind == "Application":
            apps += 1
            findings.extend(check_app(path, f"Application/{name}", doc.get("metadata") or {}, doc.get("spec") or {}))
        elif kind == "ApplicationSet":
            apps += 1
            tmpl = (doc.get("spec") or {}).get("template") or {}
            findings.extend(check_app(path, f"ApplicationSet/{name} (template)",
                                      tmpl.get("metadata") or {}, tmpl.get("spec") or {}))
    return apps, findings


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("roots", nargs="*", default=["infra/argocd"],
                    help="YAML files or directories (default: infra/argocd)")
    args = ap.parse_args()

    files = yaml_files(args.roots or ["infra/argocd"])
    findings: list[Finding] = []
    apps = 0
    for f in files:
        n, fs = scan(f)
        apps += n
        findings.extend(fs)

    # Read-proof: a lint that scanned nothing must never read as one that found nothing.
    print(f"argocd-ignore-differences: scanned {len(files)} file(s), "
          f"{apps} Application/ApplicationSet object(s); {len(findings)} finding(s)")
    if apps == 0:
        print("ERROR: no Application or ApplicationSet found — wrong root?")
        return 1
    if findings:
        for f in findings:
            print(f)
        print("\nSee the module docstring and docs/infrastructure/ARGOCD_KNOWN_ISSUES.md.")
        return 1
    print("OK — no list-selecting ignore rule on a CRD under RespectIgnoreDifferences, and no ServerSideDiff.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
