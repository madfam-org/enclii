#!/usr/bin/env python3
"""
money-engine-status.py — one screen: is every money path working right now?

WHAT IT READS
=============
The revenue probes' own definitions, from this checkout:

  * the `blackbox-revenue` scrape job's targets and labels
    (infra/k8s/production/monitoring/prometheus.yaml, prometheus-config), and
  * the blackbox modules' pass conditions
    (infra/k8s/production/monitoring/blackbox-exporter.yaml).

It then makes the same public GET each probe makes (same headers, no
redirects followed, TLS required) and judges the answer with the same rules:
status code, body regexes, header regexes. So an agent or the owner can read
the money engine's state with one command, without a Grafana sign-in and
without a Prometheus read path (Enclii has none today).

With --cluster it also reads what the in-cluster prober saw, through Enclii's
audited read layer: `enclii ops pods logs` of the blackbox-exporter pod,
which logs every failed probe ("Probe failed ... target=..."). Outside and
inside can differ (the cluster's egress, Cloudflare's view of it).

WHAT IT DOES NOT READ
=====================
  * Prometheus's alert state (no Enclii read path to Alertmanager either).
  * The retainer billing cycle (CFDI stamped/sent, payment recorded): those
    are Prometheus gauges, readable in Grafana's "Money engine" dashboard
    once they exist; there is no agent read for them yet.
  * Anything behind a sign-in. Every request is an anonymous GET.

USAGE
=====
    python3 scripts/money-engine-status.py            # outside view, one screen
    python3 scripts/money-engine-status.py --cluster  # + the prober's failures (enclii)
    python3 scripts/money-engine-status.py --json     # machine-readable

Exit code: 0 when every money path is OK, 1 when any money path is broken,
2 when the definitions cannot be read. Warning-only targets never change the
exit code.
"""
from __future__ import annotations

import argparse
import json
import re
import shutil
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass, field
from pathlib import Path

import yaml

REPO_ROOT = Path(__file__).resolve().parents[1]
MONITORING = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"
JOB = "blackbox-revenue"


@dataclass
class Target:
    url: str
    labels: dict

    @property
    def paths(self) -> list[str]:
        return [p for p in self.labels.get("money_path", "").split(",") if p]

    @property
    def warning_only(self) -> bool:
        return self.labels.get("paging") == "warning"


@dataclass
class Result:
    url: str
    product: str
    flow: str
    module: str
    paths: list[str]
    warning_only: bool
    status: int
    ok: bool
    reasons: list[str] = field(default_factory=list)
    seconds: float = 0.0


def _configmap_docs(path: Path) -> list[dict]:
    with path.open("r", encoding="utf-8") as fh:
        return [d for d in yaml.safe_load_all(fh) if isinstance(d, dict) and d.get("kind") == "ConfigMap"]


def load_targets(prometheus: Path = MONITORING / "prometheus.yaml") -> list[Target]:
    for doc in _configmap_docs(prometheus):
        if doc["metadata"]["name"] != "prometheus-config":
            continue
        cfg = yaml.safe_load(doc["data"]["prometheus.yml"])
        for job in cfg["scrape_configs"]:
            if job.get("job_name") == JOB:
                return [
                    Target(url, dict(sc.get("labels") or {}))
                    for sc in job["static_configs"]
                    for url in sc["targets"]
                ]
    raise SystemExit(2)


def load_modules(blackbox: Path = MONITORING / "blackbox-exporter.yaml") -> dict:
    for doc in _configmap_docs(blackbox):
        if doc["metadata"]["name"] == "blackbox-exporter-config":
            return yaml.safe_load(doc["data"]["config.yml"])["modules"]
    raise SystemExit(2)


def judge(module: dict, status: int, body: str, headers: list[tuple[str, str]]) -> list[str]:
    """The blackbox http prober's pass rules this estate uses. [] means pass."""
    http = module["http"]
    reasons = []
    valid = http.get("valid_status_codes")
    if not ((status in valid) if valid else 200 <= status < 300):
        reasons.append(f"status {status}")
    for pattern in http.get("fail_if_body_not_matches_regexp") or []:
        if not re.search(pattern, body):
            reasons.append(f"body lacks /{pattern}/")
    for pattern in http.get("fail_if_body_matches_regexp") or []:
        if re.search(pattern, body):
            reasons.append(f"body has /{pattern}/")
    for rule in http.get("fail_if_header_not_matches") or []:
        values = [v for k, v in headers if k.lower() == rule["header"].lower()]
        if not values and rule.get("allow_missing"):
            continue
        if not any(re.search(rule["regexp"], v) for v in values):
            reasons.append(f"header {rule['header']} !~ /{rule['regexp']}/")
    return reasons


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):  # noqa: D401 - urllib API
        return None


def fetch(url: str, module: dict) -> tuple[int, str, list[tuple[str, str]]]:
    http = module["http"]
    if http.get("fail_if_not_ssl") and not url.startswith("https://"):
        return 0, "", []
    req = urllib.request.Request(url, method=http.get("method", "GET"), headers=dict(http.get("headers") or {}))
    opener = urllib.request.build_opener(
        _NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context())
    )
    timeout = float(str(module.get("timeout", "10s")).rstrip("s"))
    try:
        with opener.open(req, timeout=timeout) as resp:
            return resp.status, resp.read().decode("utf-8", "replace"), list(resp.headers.items())
    except urllib.error.HTTPError as exc:  # 3xx (not followed), 4xx, 5xx
        return exc.code, exc.read().decode("utf-8", "replace"), list(exc.headers.items())
    except Exception:  # timeout, DNS, TLS: the prober scores these as failures
        return 0, "", []


def probe(target: Target, modules: dict, fetcher=fetch) -> Result:
    module = modules[target.labels["module"]]
    start = time.monotonic()
    status, body, headers = fetcher(target.url, module)
    reasons = judge(module, status, body, headers) if status else ["no answer (timeout, DNS or TLS)"]
    return Result(
        url=target.url,
        product=target.labels["product"],
        flow=target.labels["flow"],
        module=target.labels["module"],
        paths=target.paths,
        warning_only=target.warning_only,
        status=status,
        ok=not reasons,
        reasons=reasons,
        seconds=round(time.monotonic() - start, 2),
    )


def money_paths(results: list[Result]) -> dict[str, list[Result]]:
    out: dict[str, list[Result]] = {}
    for r in results:
        for p in r.paths:
            out.setdefault(p, []).append(r)
    return dict(sorted(out.items()))


PROBE_FAILED = re.compile(
    r'^time=(?P<time>\S+) level=ERROR .*msg="Probe failed" module=(?P<module>\S+) target=(?P<target>\S+)'
)


def parse_prober_log(text: str) -> dict[str, dict]:
    """Per target: how many failed probes the blackbox-exporter logged, first and last."""
    out: dict[str, dict] = {}
    for line in text.splitlines():
        m = PROBE_FAILED.match(line)
        if not m:
            continue
        entry = out.setdefault(m["target"], {"failures": 0, "first": m["time"], "last": m["time"]})
        entry["failures"] += 1
        entry["last"] = m["time"]
    return out


def read_cluster_prober(tail: int) -> tuple[dict[str, dict] | None, str]:
    """The in-cluster prober's failures, through `enclii ops pods` reads."""
    if not shutil.which("enclii"):
        return None, "enclii CLI not on PATH"
    try:
        diag = subprocess.run(
            ["enclii", "ops", "pods", "diagnose", "-n", "monitoring", "--json"],
            capture_output=True, text=True, timeout=90,
        )
        pods = json.loads(diag.stdout)["data"]["pods"]
        name = next(p["name"] for p in pods if p["name"].startswith("blackbox-exporter-") and p.get("phase") == "Running")
        logs = subprocess.run(
            ["enclii", "ops", "pods", "logs", name, "-n", "monitoring", "--tail", str(tail), "--json"],
            capture_output=True, text=True, timeout=90,
        )
        data = json.loads(logs.stdout)["data"]
    except (subprocess.SubprocessError, ValueError, KeyError, StopIteration) as exc:
        return None, f"could not read the prober: {type(exc).__name__}"
    started = next((l for l in data["logs"].splitlines() if "Starting blackbox_exporter" in l), "")
    return parse_prober_log(data["logs"]), f"pod {name}, last {tail} lines" + (
        f", started {started.split()[0].removeprefix('time=')}" if started else ""
    )


def render(results: list[Result], cluster: dict[str, dict] | None, cluster_note: str) -> str:
    lines = [f"Money engine status  {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}  (public GETs, probe rules)"]
    paths = money_paths(results)
    lines.append("")
    lines.append("MONEY PATHS (a broken path pages: MoneyPathDown)")
    for name, steps in paths.items():
        bad = [s for s in steps if not s.ok]
        lines.append(f"  {'BROKEN' if bad else 'OK    '}  {name:<18} {len(steps) - len(bad)}/{len(steps)} steps pass")
        for s in bad:
            lines.append(f"            x {s.product} {s.flow}: {s.url} [{s.status}] {'; '.join(s.reasons)}")
    others = [r for r in results if not r.paths]
    lines.append("")
    lines.append("OTHER REVENUE PROBES")
    for label, sel in (("pages (critical)", [r for r in others if not r.warning_only]),
                       ("digest (warning)", [r for r in others if r.warning_only])):
        bad = [r for r in sel if not r.ok]
        lines.append(f"  {label}: {len(sel) - len(bad)}/{len(sel)} pass")
        for r in bad:
            lines.append(f"    x {r.product} {r.flow}: {r.url} [{r.status}] {'; '.join(r.reasons)}")
    if cluster is not None or cluster_note:
        lines.append("")
        lines.append(f"IN-CLUSTER PROBER ({cluster_note})")
        if cluster is None:
            lines.append("  not read")
        elif not cluster:
            lines.append("  no failed probe logged")
        else:
            for target, e in sorted(cluster.items(), key=lambda kv: kv[1]["last"], reverse=True):
                lines.append(f"  {e['failures']:>4} failed  last {e['last']}  {target}")
    lines.append("")
    lines.append(
        f"read-proof: listed={len(results)} checked={len(results)} failing={sum(not r.ok for r in results)}"
        f" (money-path steps failing={sum(1 for r in results if r.paths and not r.ok)})"
    )
    lines.append("not read here: alert state, the retainer billing cycle (Grafana 'Money engine'), signed-in flows")
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[1])
    ap.add_argument("--cluster", action="store_true", help="also read the in-cluster prober's failures via enclii")
    ap.add_argument("--tail", type=int, default=2000, help="prober log lines to read with --cluster")
    ap.add_argument("--json", action="store_true")
    args = ap.parse_args(argv)
    try:
        targets, modules = load_targets(), load_modules()
    except (OSError, yaml.YAMLError):
        return 2
    results = [probe(t, modules) for t in targets]
    cluster, note = (read_cluster_prober(args.tail) if args.cluster else (None, ""))
    if args.json:
        print(json.dumps({
            "results": [asdict(r) for r in results],
            "money_paths": {k: all(s.ok for s in v) for k, v in money_paths(results).items()},
            "cluster": cluster,
            "cluster_note": note,
        }, indent=2))
    else:
        print(render(results, cluster, note))
    return 1 if any(not r.ok for r in results if r.paths) else 0


if __name__ == "__main__":
    sys.exit(main())
