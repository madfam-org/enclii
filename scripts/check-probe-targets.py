#!/usr/bin/env python3
"""
check-probe-targets.py — CI lint for synthetic-probe target ConfigMaps.

WHAT THIS CATCHES, AND WHAT IT HONESTLY DOES NOT
================================================
On 2026-08-06 the in-cluster synthetic probe dialed `dhanam-api:3000` and
`janua-api:8080` — container ports those Services do NOT publish (both
publish 80). Dialing a Service port that does not exist times out, and the
probe reported `probe_blocked` with the hint "check NetworkPolicy / CNI": a
wrong diagnosis that masked the real one and made a genuine outage
indistinguishable from standing noise.

**This static check would NOT have caught that on its own.** Those entries
were internally consistent: `"port": 3000` matched `...svc.cluster.local:3000`
in the URL. The Service definitions live in each product repo, not here, so
nothing in this repository could be compared against. The control that
actually catches Fault 2 is the runtime one in `infra/cloudflared-probe/
probe.py`, which reads the live Service's published ports and reports a
distinct `probe_misconfigured` event. See docs/infrastructure/
EXTERNAL_SECRETS.md and the probe README for the split.

What this check DOES enforce is the internal consistency the runtime check
assumes — the class of typo that makes the runtime diagnosis itself lie:

  1. `targets.json` is well-formed JSON with the expected envelope.
  2. Every target has all required keys (name, url, namespace, service, port).
  3. The URL's port equals the declared `port` field. If they diverge, the
     probe dials one port and every metric, alert annotation and log line
     blames another.
  4. The URL host is `<service>.<namespace>.svc.cluster.local` — matching the
     declared `service` and `namespace`. If they diverge, the alert names a
     Service that was never probed.
  5. The URL scheme is http/https and a path is present.
  6. Target names are unique (duplicates silently overwrite each other in
     per-name gauges).

USAGE
=====
    python3 scripts/check-probe-targets.py infra/k8s/

Exit codes:
  0 — all probe target ConfigMaps are internally consistent
  1 — at least one inconsistency
  2 — could not parse manifests (YAML error, bad JSON, missing dir)
"""
from __future__ import annotations

import json
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator
from urllib.parse import urlparse

try:
    import yaml
except ImportError:  # pragma: no cover
    print("error: PyYAML is required. Install with: pip install pyyaml", file=sys.stderr)
    sys.exit(2)


# ConfigMap data keys that hold a probe target list. Add new probes here.
TARGET_KEYS = ("targets.json",)

REQUIRED_TARGET_FIELDS = ("name", "url", "namespace", "service", "port")

CLUSTER_DOMAIN_SUFFIX = ".svc.cluster.local"

DEFAULT_PORT_FOR_SCHEME = {"http": 80, "https": 443}


@dataclass
class Finding:
    severity: str  # "fail" | "warn"
    file: str
    message: str

    def render(self) -> str:
        prefix = "FAIL" if self.severity == "fail" else "WARN"
        return f"[{prefix}] {self.file}: {self.message}"


def iter_yaml_docs(root: Path) -> Iterator[tuple[Path, dict]]:
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        if path.suffix not in {".yaml", ".yml"}:
            continue
        try:
            with path.open("r", encoding="utf-8") as fh:
                for doc in yaml.safe_load_all(fh):
                    if isinstance(doc, dict):
                        yield path, doc
        except yaml.YAMLError as exc:
            print(f"error: cannot parse {path}: {exc}", file=sys.stderr)
            raise


def check_target(path: Path, cm_name: str, index: int, raw: object) -> list[Finding]:
    findings: list[Finding] = []
    where = f"ConfigMap '{cm_name}' target[{index}]"

    if not isinstance(raw, dict):
        return [
            Finding("fail", str(path), f"{where} is {type(raw).__name__}, expected object")
        ]

    # Comment-only entries (`{"//": "..."}`) are a JSON idiom used in the
    # existing ConfigMap; they carry no target and are skipped.
    if set(raw.keys()) <= {"//"}:
        return []

    name = raw.get("name", f"<index {index}>")
    where = f"ConfigMap '{cm_name}' target '{name}'"

    missing = [f for f in REQUIRED_TARGET_FIELDS if f not in raw]
    if missing:
        findings.append(
            Finding("fail", str(path), f"{where} is missing required field(s): {missing}")
        )
        return findings

    url = str(raw["url"])
    namespace = str(raw["namespace"])
    service = str(raw["service"])

    try:
        declared_port = int(raw["port"])
    except (TypeError, ValueError):
        return findings + [
            Finding("fail", str(path), f"{where} has non-integer port {raw['port']!r}")
        ]

    parsed = urlparse(url)

    if parsed.scheme not in DEFAULT_PORT_FOR_SCHEME:
        findings.append(
            Finding(
                "fail",
                str(path),
                f"{where} url scheme {parsed.scheme!r} is not http/https: {url}",
            )
        )
        return findings

    if not parsed.path or parsed.path == "/":
        findings.append(
            Finding(
                "warn",
                str(path),
                f"{where} url has no health path ({url}) — probing '/' rarely "
                "exercises the same handler as a real health endpoint.",
            )
        )

    url_port = parsed.port
    if url_port is None:
        url_port = DEFAULT_PORT_FOR_SCHEME[parsed.scheme]

    if url_port != declared_port:
        findings.append(
            Finding(
                "fail",
                str(path),
                f"{where} dials port {url_port} in its url but declares "
                f"\"port\": {declared_port}. The probe dials the URL; every "
                "metric label, alert annotation and log line uses the declared "
                "port — so the alert would blame a port nobody dialed. Make "
                "them equal.",
            )
        )

    host = parsed.hostname or ""
    expected_host = f"{service}.{namespace}{CLUSTER_DOMAIN_SUFFIX}"
    if host != expected_host:
        findings.append(
            Finding(
                "fail",
                str(path),
                f"{where} url host is {host!r} but namespace/service fields say "
                f"{expected_host!r}. The probe resolves the URL; alerts are "
                "labelled from the fields — they must describe the same Service.",
            )
        )

    return findings


def check_configmap(path: Path, doc: dict) -> list[Finding]:
    findings: list[Finding] = []
    meta = doc.get("metadata") or {}
    cm_name = meta.get("name", "<unnamed>")
    data = doc.get("data") or {}
    if not isinstance(data, dict):
        return findings

    for key in TARGET_KEYS:
        if key not in data:
            continue
        try:
            payload = json.loads(data[key])
        except (TypeError, ValueError) as exc:
            findings.append(
                Finding(
                    "fail",
                    str(path),
                    f"ConfigMap '{cm_name}' key '{key}' is not valid JSON: {exc}",
                )
            )
            continue

        if isinstance(payload, dict) and "targets" in payload:
            targets = payload["targets"]
        else:
            targets = payload

        if not isinstance(targets, list):
            findings.append(
                Finding(
                    "fail",
                    str(path),
                    f"ConfigMap '{cm_name}' key '{key}' must be a list or "
                    "{'targets': [...]}",
                )
            )
            continue

        seen: dict[str, int] = {}
        for index, raw in enumerate(targets):
            findings.extend(check_target(path, cm_name, index, raw))
            if isinstance(raw, dict) and isinstance(raw.get("name"), str):
                name = raw["name"]
                if name in seen:
                    findings.append(
                        Finding(
                            "fail",
                            str(path),
                            f"ConfigMap '{cm_name}' has duplicate target name "
                            f"'{name}' (indices {seen[name]} and {index}) — "
                            "per-name gauges would silently overwrite.",
                        )
                    )
                seen[name] = index

    return findings


def main(argv: list[str]) -> int:
    if len(argv) < 2:
        print("usage: check-probe-targets.py <root> [<root> ...]", file=sys.stderr)
        return 2

    roots = [Path(p) for p in argv[1:]]
    for root in roots:
        if not root.exists():
            print(f"error: root does not exist: {root}", file=sys.stderr)
            return 2

    all_findings: list[Finding] = []
    configmaps = 0
    try:
        for root in roots:
            for path, doc in iter_yaml_docs(root):
                if doc.get("kind") != "ConfigMap":
                    continue
                data = doc.get("data") or {}
                if not isinstance(data, dict):
                    continue
                if not any(k in data for k in TARGET_KEYS):
                    continue
                configmaps += 1
                all_findings.extend(check_configmap(path, doc))
    except yaml.YAMLError:
        return 2

    fails = [f for f in all_findings if f.severity == "fail"]
    warns = [f for f in all_findings if f.severity == "warn"]

    for f in all_findings:
        print(f.render())

    print()
    print(
        f"checked {configmaps} probe-target ConfigMap(s) in {len(roots)} root(s); "
        f"{len(fails)} failure(s), {len(warns)} warning(s)."
    )

    if fails:
        print(
            "FAIL: probe target consistency check failed. See messages above.",
            file=sys.stderr,
        )
        return 1
    print("OK: probe target consistency check passed.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
