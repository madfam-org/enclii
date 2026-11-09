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

With --cluster it also reads, through Enclii's audited read layer
(`enclii ops pods diagnose|logs`, no kubectl):

  * what the in-cluster prober saw: the blackbox-exporter pod logs every
    failed probe ("Probe failed ... target=..."). Outside and inside can
    differ (the cluster's egress, Cloudflare's view of it);
  * the vCTO retainer billing cycle: the nauta worker logs one
    `retainer-cycle-status` line per cycle every hour (nauta#351), with the
    corte, the CFDI deadline, stamped, sent, the payment due date and the
    payment record. The same values are the `nauta_retainer_cycle_*` gauges
    the RetainerCfdiOverdue rule reads.

WHAT IT DOES NOT READ
=====================
  * Prometheus's alert state (no Enclii read path to Alertmanager either).
  * Anything behind a sign-in. Every request is an anonymous GET.
  * Which client a retainer workspace is: the log names a workspace id only
    (map it in the Nauta cockpit).

USAGE
=====
    python3 scripts/money-engine-status.py            # outside view, one screen
    python3 scripts/money-engine-status.py --cluster  # + the prober's failures (enclii)
    python3 scripts/money-engine-status.py --json     # machine-readable

Exit code: 0 when every money path is OK, 1 when any money path is broken
or (with --cluster) a retainer cycle's CFDI is past its deadline unstamped,
2 when the definitions cannot be read. Warning-only targets never change the
exit code.
"""
from __future__ import annotations

import argparse
import calendar
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


def _iso(value: str) -> float | None:
    """The nauta line's timestamps: ISO-8601 `...Z`, or `-` for not yet."""
    if not value or value == "-":
        return None
    try:
        return float(calendar.timegm(time.strptime(value, "%Y-%m-%dT%H:%M:%SZ")))
    except ValueError:
        return None


def parse_retainer_lines(text: str) -> dict[tuple[str, str], dict]:
    """The latest `retainer-cycle-status` line per (workspace, cycle)."""
    out: dict[tuple[str, str], dict] = {}
    for raw in text.splitlines():
        if '"retainer-cycle-status"' not in raw:
            continue
        try:
            line = json.loads(raw)["line"]
        except (ValueError, KeyError, TypeError):
            continue
        fields = dict(tok.split("=", 1) for tok in str(line).split() if "=" in tok)
        if "ws" in fields and "cycle" in fields:
            out[(fields["ws"], fields["cycle"])] = fields
    return out


def judge_retainer(fields: dict, now: float) -> dict:
    """CFDI: stamped / OVERDUE (past cfdi_deadline, the RetainerCfdiOverdue
    condition) / due. Payment: recorded / OVERDUE (a day past payment_due) / due."""
    stamped, sent = _iso(fields.get("stamped", "-")), _iso(fields.get("sent", "-"))
    deadline = _iso(fields.get("cfdi_deadline", "-"))
    due, paid = _iso(fields.get("payment_due", "-")), _iso(fields.get("payment_recorded", "-"))
    if stamped:
        cfdi = "stamped" + ("" if sent else ", NOT SENT")
    elif deadline and deadline < now:
        cfdi = "OVERDUE"
    else:
        cfdi = "due"
    if paid:
        payment = "recorded" + ("" if fields.get("paid_in_full") == "yes" else " (partial)")
    elif due and due + 86400 < now:
        payment = "OVERDUE"
    else:
        payment = "due"
    return {"cfdi": cfdi, "payment": payment}


def read_retainer_cycles(tail: int) -> tuple[dict | None, str]:
    """The retainer cycles from the nauta worker's log, through `enclii ops pods` reads."""
    if not shutil.which("enclii"):
        return None, "enclii CLI not on PATH"
    try:
        diag = subprocess.run(
            ["enclii", "ops", "pods", "diagnose", "-n", "nauta", "--json"],
            capture_output=True, text=True, timeout=90, stdin=subprocess.DEVNULL,
        )
        pods = json.loads(diag.stdout)["data"]["pods"]
        name = next(p["name"] for p in pods if p["name"].startswith("nauta-worker-") and p.get("phase") == "Running")
        logs = subprocess.run(
            ["enclii", "ops", "pods", "logs", name, "-n", "nauta", "--tail", str(tail), "--json"],
            capture_output=True, text=True, timeout=90, stdin=subprocess.DEVNULL,
        )
        text = json.loads(logs.stdout)["data"]["logs"]
    except (subprocess.SubprocessError, ValueError, KeyError, StopIteration) as exc:
        return None, f"could not read the nauta worker: {type(exc).__name__}"
    return parse_retainer_lines(text), f"pod {name}, last {tail} lines"


def render(results: list[Result], cluster: dict[str, dict] | None, cluster_note: str,
           retainer: dict | None = None, retainer_note: str = "", now: float | None = None) -> str:
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
    if retainer is not None or retainer_note:
        lines.append("")
        lines.append(f"RETAINER CYCLES ({retainer_note})")
        if retainer is None:
            lines.append("  not read")
        elif not retainer:
            lines.append("  no retainer-cycle-status line in the log (nauta#351 not deployed, or the worker just restarted)")
        else:
            for (ws, cycle), f in sorted(retainer.items(), key=lambda kv: (kv[0][0], int(kv[0][1]) if kv[0][1].isdigit() else 0)):
                j = judge_retainer(f, now if now is not None else time.time())
                lines.append(
                    f"  ws {ws[:8]} cycle {cycle}: CFDI {j['cfdi']} (deadline {f.get('cfdi_deadline', '-')}, stamped {f.get('stamped', '-')},"
                    f" sent {f.get('sent', '-')}); payment {j['payment']} (due {f.get('payment_due', '-')})"
                )
    lines.append("")
    lines.append(
        f"read-proof: listed={len(results)} checked={len(results)} failing={sum(not r.ok for r in results)}"
        f" (money-path steps failing={sum(1 for r in results if r.paths and not r.ok)})"
    )
    lines.append("not read here: alert state, signed-in flows" + ("" if retainer_note else ", the retainer cycle (use --cluster)"))
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
    retainer, rnote = (read_retainer_cycles(args.tail) if args.cluster else (None, ""))
    now = time.time()
    retainer_states = {f"{ws}/{c}": judge_retainer(f, now) for (ws, c), f in (retainer or {}).items()}
    if args.json:
        print(json.dumps({
            "results": [asdict(r) for r in results],
            "money_paths": {k: all(s.ok for s in v) for k, v in money_paths(results).items()},
            "cluster": cluster,
            "cluster_note": note,
            "retainer": retainer_states,
            "retainer_note": rnote,
        }, indent=2))
    else:
        print(render(results, cluster, note, retainer, rnote, now))
    cfdi_overdue = any(v["cfdi"] == "OVERDUE" for v in retainer_states.values())
    return 1 if cfdi_overdue or any(not r.ok for r in results if r.paths) else 0


if __name__ == "__main__":
    sys.exit(main())
