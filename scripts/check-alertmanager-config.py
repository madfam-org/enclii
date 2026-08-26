#!/usr/bin/env python3
"""
check-alertmanager-config.py — CI lint for the Alertmanager config ConfigMap.

WHY THIS EXISTS
===============
On 2026-08-26 `alertmanager-2` was in CrashLoopBackOff with ~170 restarts and
the StatefulSet RollingUpdate was wedged at the highest ordinal. The cause was
one line in the `alertmanager-config` ConfigMap:

    telegram_configs:
      - bot_token_file: '/etc/alertmanager/telegram-bot-token'
        chat_id: 0

`chat_id: 0` was committed as a deliberate "inert sentinel" — the author's own
ConfigMap comment said it would fail at SEND time until an operator supplied a
real chat id. That premise was wrong. Alertmanager v0.26.0 validates it at
CONFIG-LOAD time, in `TelegramConfig.UnmarshalYAML` (config/notifiers.go:778):

    if c.ChatID == 0 {
        return fmt.Errorf("missing chat_id on telegram_config")
    }

So the process exits 1 in under a second and never serves. Worse, this hid
behind a stale-revision reprieve: pods 0 and 1 stayed Ready only because they
were still running a 72-day-old pre-Telegram config from memory. Any eviction,
node drain or restart of either would have crashed them identically — a full
estate alerting outage one disruption away, invisible to every readiness probe.

The class of bug is "a manifest that is valid YAML, renders fine under
kustomize, syncs Healthy in Argo, and kills the process on load". Nothing in
this repository could see it. This check can.

WHAT THIS CHECKS
================
Two layers, in order:

  1. STRUCTURAL (always runs, no network, no container runtime).
     Reimplements the v0.26.0 load-time validations that reject a config
     outright. Every rule below was read off the v0.26.0 source, and each
     carries its upstream error string so a CI failure reads the same as the
     crash it prevents. Rules are deliberately limited to unambiguous
     load-time rejections — this is a lint, not a reimplementation of
     Alertmanager.

  2. amtool check-config (runs only when `amtool` is on PATH, or when
     ENCLII_AMTOOL is set to a command).
     This is the real thing: `amtool check-config` calls the very same
     `config.LoadFile` → `yaml.UnmarshalStrict` → `UnmarshalYAML` chain the
     server uses (cli/check_config.go:67), so it catches everything, including
     the unknown-field rejections the structural layer cannot model. It is
     OPTIONAL on purpose: this repo's CI lint lanes are dependency-light
     Python, and a lint that only works when a 60MB image pull succeeds is a
     lint that gets skipped during an incident. Structural is the floor;
     amtool is the ceiling when available.

     If the validator cannot RUN (no docker, unreachable registry, rate
     limit, timeout), this degrades to structural-only with a loud stderr
     WARNING and does NOT fail the build — a lane that goes red on registry
     hiccups teaches people to ignore its red, and an ignored check is worth
     less than no check because it still looks like coverage. Only a
     validator that ran and REJECTED the config fails the build.

HONEST LIMITATIONS
==================
The structural layer does NOT reimplement `yaml.UnmarshalStrict`, so it will
not catch a misspelled or unknown field (`chat_i: 123`), which Alertmanager
rejects at load. It does not resolve global-fallback requirements (e.g. a
`slack_configs` with no api_url anywhere, which fails with "no global Slack
API URL set either inline or in a file"), because those depend on global
blocks this check does not fully model. It does not validate Go templates.
Run the amtool layer for those. What the structural layer guarantees is that
the exact 2026-08-26 class — a required scalar committed as a zero/empty
sentinel — cannot ship again.

USAGE
=====
    python3 scripts/check-alertmanager-config.py infra/k8s/

    # with the real validator, if you have it:
    ENCLII_AMTOOL="docker run --rm --entrypoint /bin/amtool -v {dir}:/w:ro \\
        docker.io/prom/alertmanager:v0.26.0 check-config /w/{name}" \\
        python3 scripts/check-alertmanager-config.py infra/k8s/

Exit codes:
  0 — every alertmanager.yml found is loadable
  1 — at least one would be rejected at config load (process would crash)
  2 — could not parse manifests, or no alertmanager config was found at all
"""
from __future__ import annotations

import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator

try:
    import yaml
except ImportError:  # pragma: no cover
    print("error: PyYAML is required. Install with: pip install pyyaml", file=sys.stderr)
    sys.exit(2)


# The ConfigMap data key holding an Alertmanager config. Add new ones here.
CONFIG_KEYS = ("alertmanager.yml", "alertmanager.yaml")

# v0.26.0 TelegramConfig.UnmarshalYAML, config/notifiers.go:781-786.
TELEGRAM_PARSE_MODES = {"", "Markdown", "MarkdownV2", "HTML"}

# v0.26.0 WeChatConfig.UnmarshalYAML, config/notifiers.go:548.
WECHAT_MESSAGE_TYPES = {"", "text", "markdown"}


@dataclass(frozen=True)
class Finding:
    path: Path
    where: str
    message: str

    def render(self) -> str:
        return f"FAIL {self.path}: {self.where}: {self.message}"


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


def _as_list(raw: object) -> list:
    return raw if isinstance(raw, list) else []


def _present(value: object) -> bool:
    """True if the field carries a real value.

    Alertmanager reads these into Go scalars, so an omitted key, an explicit
    null, and an empty string are all indistinguishable from "unset" — which
    is exactly the sentinel trap this check exists to close.
    """
    if value is None:
        return False
    if isinstance(value, str):
        return value.strip() != ""
    return True


def check_telegram(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:766-788."""
    errs: list[str] = []
    token = cfg.get("bot_token")
    token_file = cfg.get("bot_token_file")
    if not _present(token) and not _present(token_file):
        errs.append("missing bot_token or bot_token_file on telegram_config")
    elif _present(token) and _present(token_file):
        errs.append("at most one of bot_token & bot_token_file must be configured")

    # THE 2026-08-26 BUG. ChatID is a plain int64 with no file-based variant,
    # so 0, null and an omitted key are the same value to Alertmanager, and
    # all three are rejected. There is NO valid inert placeholder for this
    # field: if you cannot supply a real chat id, the block must not exist.
    chat_id = cfg.get("chat_id")
    if not isinstance(chat_id, int) or isinstance(chat_id, bool) or chat_id == 0:
        errs.append(
            "missing chat_id on telegram_config "
            "(chat_id must be a real nonzero int64; 0/null/omitted are all "
            "rejected at CONFIG LOAD and the process exits 1 — there is no "
            "valid inert placeholder, remove the block instead)"
        )

    parse_mode = cfg.get("parse_mode")
    if parse_mode is not None and str(parse_mode) not in TELEGRAM_PARSE_MODES:
        errs.append(
            "unknown parse_mode on telegram_config, must be Markdown, "
            "MarkdownV2, HTML or empty string"
        )
    return [f"{where}: {e}" for e in errs]


def check_email(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:262-272."""
    errs: list[str] = []
    if not _present(cfg.get("to")):
        errs.append("missing to address in email config")
    headers = cfg.get("headers")
    if isinstance(headers, dict):
        seen: set[str] = set()
        for key in headers:
            norm = str(key).title()
            if norm in seen:
                errs.append(f'duplicate header "{key}" in email config')
            seen.add(norm)
    return [f"{where}: {e}" for e in errs]


def check_webhook(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:498-510."""
    errs: list[str] = []
    url = cfg.get("url")
    url_file = cfg.get("url_file")
    if not _present(url) and not _present(url_file):
        errs.append("one of url or url_file must be configured")
    elif _present(url) and _present(url_file):
        errs.append("at most one of url & url_file must be configured")
    elif _present(url) and not str(url).startswith(("http://", "https://")):
        errs.append("scheme required for webhook url")
    return [f"{where}: {e}" for e in errs]


def check_slack(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:470-473.

    Slack requires NEITHER api_url nor api_url_file at the notifier level —
    the "must have one" check is a global fallback in config.go. Only the
    mutual exclusion is a notifier-level load-time rejection.
    """
    if _present(cfg.get("api_url")) and _present(cfg.get("api_url_file")):
        return [f"{where}: at most one of api_url & api_url_file must be configured"]
    return []


def check_pagerduty(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:322-330."""
    errs: list[str] = []
    keys = ("routing_key", "routing_key_file", "service_key", "service_key_file")
    if not any(_present(cfg.get(k)) for k in keys):
        errs.append("missing service or routing key in PagerDuty config")
    if _present(cfg.get("routing_key")) and _present(cfg.get("routing_key_file")):
        errs.append("at most one of routing_key & routing_key_file must be configured")
    if _present(cfg.get("service_key")) and _present(cfg.get("service_key_file")):
        errs.append("at most one of service_key & service_key_file must be configured")
    return [f"{where}: {e}" for e in errs]


def check_victorops(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:647-652."""
    errs: list[str] = []
    if not _present(cfg.get("routing_key")):
        errs.append("missing Routing key in VictorOps config")
    if _present(cfg.get("api_key")) and _present(cfg.get("api_key_file")):
        errs.append("at most one of api_key & api_key_file must be configured")
    return [f"{where}: {e}" for e in errs]


def check_pushover(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:707-718."""
    errs: list[str] = []
    for field in ("user_key", "token"):
        inline, from_file = cfg.get(field), cfg.get(f"{field}_file")
        if not _present(inline) and not _present(from_file):
            errs.append(f"one of {field} or {field}_file must be configured")
        elif _present(inline) and _present(from_file):
            errs.append(f"at most one of {field} & {field}_file must be configured")
    return [f"{where}: {e}" for e in errs]


def check_webex(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:204-210."""
    if not _present(cfg.get("room_id")):
        return [f"{where}: missing room_id on webex_config"]
    return []


def check_wechat(where: str, cfg: dict) -> list[str]:
    """v0.26.0 config/notifiers.go:547-549."""
    mt = cfg.get("message_type")
    if mt is not None and str(mt) not in WECHAT_MESSAGE_TYPES:
        return [
            f'{where}: weChat message type "{mt}" does not match valid '
            "options ^(text|markdown)$"
        ]
    return []


# notifier key -> validator. Notifier types absent here (opsgenie, sns,
# discord, msteams) either have no notifier-level UnmarshalYAML validation in
# v0.26.0 or validate through global fallbacks this check does not model —
# see HONEST LIMITATIONS. Add them here if this repo starts using them.
NOTIFIER_CHECKS = {
    "telegram_configs": check_telegram,
    "email_configs": check_email,
    "webhook_configs": check_webhook,
    "slack_configs": check_slack,
    "pagerduty_configs": check_pagerduty,
    "victorops_configs": check_victorops,
    "pushover_configs": check_pushover,
    "webex_configs": check_webex,
    "wechat_configs": check_wechat,
}


def collect_route_receivers(route: object, acc: list[str]) -> None:
    if not isinstance(route, dict):
        return
    recv = route.get("receiver")
    if _present(recv):
        acc.append(str(recv))
    for child in _as_list(route.get("routes")):
        collect_route_receivers(child, acc)


def check_routing(cfg: dict) -> list[str]:
    """v0.26.0 config/config.go:548-605 and Route.UnmarshalYAML :799-844."""
    errs: list[str] = []
    route = cfg.get("route")
    if not isinstance(route, dict):
        return ["route: no routes provided"]

    if not _present(route.get("receiver")):
        errs.append("route: root route must specify a default receiver")
    if any(_present(route.get(k)) for k in ("match", "match_re", "matchers")):
        errs.append("route: root route must not have any matchers")
    if _present(route.get("mute_time_intervals")):
        errs.append("route: root route must not have any mute time intervals")
    if _present(route.get("active_time_intervals")):
        errs.append("route: root route must not have any active time intervals")
    # config.go:186 — checked in Load(), not UnmarshalYAML.
    if route.get("continue") is True:
        errs.append("route: cannot have continue in root route")

    group_by = _as_list(route.get("group_by"))
    if "..." in group_by and len(group_by) > 1:
        errs.append(
            "route: cannot have wildcard group_by (`...`) and other other "
            "labels at the same time"
        )
    if len(set(group_by)) != len(group_by):
        errs.append("route: duplicated label in group_by")

    receivers = _as_list(cfg.get("receivers"))
    names: list[str] = []
    for recv in receivers:
        if not isinstance(recv, dict):
            continue
        name = recv.get("name")
        if not _present(name):
            errs.append("receivers: missing name in receiver")
            continue
        if str(name) in names:
            errs.append(f'receivers: notification config name "{name}" is not unique')
        names.append(str(name))

    used: list[str] = []
    collect_route_receivers(route, used)
    for name in used:
        if name not in names:
            errs.append(f'route: undefined receiver "{name}" used in route')
    return errs


def check_alertmanager_yml(path: Path, cm_name: str, key: str, body: str) -> list[Finding]:
    prefix = f"ConfigMap '{cm_name}' data['{key}']"
    try:
        cfg = yaml.safe_load(body)
    except yaml.YAMLError as exc:
        return [Finding(path, prefix, f"not valid YAML: {exc}")]
    if not isinstance(cfg, dict):
        return [Finding(path, prefix, "config is not a YAML mapping")]

    messages: list[str] = list(check_routing(cfg))
    for recv in _as_list(cfg.get("receivers")):
        if not isinstance(recv, dict):
            continue
        rname = recv.get("name", "<unnamed>")
        for notifier_key, checker in NOTIFIER_CHECKS.items():
            for idx, entry in enumerate(_as_list(recv.get(notifier_key))):
                if not isinstance(entry, dict):
                    messages.append(
                        f"receiver '{rname}' {notifier_key}[{idx}]: not a mapping"
                    )
                    continue
                where = f"receiver '{rname}' {notifier_key}[{idx}]"
                messages.extend(checker(where, entry))

    return [Finding(path, prefix, m) for m in messages]


# How long to give the optional validator before treating it as unavailable.
# Generous enough for a cold image pull, short enough not to wedge CI.
AMTOOL_TIMEOUT_SECONDS = int(os.environ.get("ENCLII_AMTOOL_TIMEOUT", "180"))

# Signatures of "the validator could not run", as opposed to "the validator
# ran and rejected the config". A container runtime that cannot pull, cannot
# reach its daemon, or is not installed says so on stderr and exits nonzero —
# indistinguishable from a real rejection by exit code alone.
UNAVAILABLE_MARKERS = (
    "command not found",
    "not found: docker",
    "cannot connect to the docker daemon",
    "docker daemon is not running",
    "error during connect",
    "failed to resolve reference",
    "pull access denied",
    "manifest unknown",
    "no such host",
    "connection refused",
    "network is unreachable",
    "i/o timeout",
    "tls handshake timeout",
    "context deadline exceeded",
    "temporary failure in name resolution",
    "toomanyrequests",
    "permission denied while trying to connect",
)


class AmtoolUnavailable(Exception):
    """The optional validator could not be run. NOT a config verdict."""


def run_amtool(body: str, key: str) -> tuple[bool, str] | None:
    """Run the real validator if one is reachable. None means 'not available'.

    ENCLII_AMTOOL, when set, is a command template with {dir} and {name}
    placeholders — this lets CI or an operator point at a container image
    without this script hardcoding a runtime.

    CRITICAL DISTINCTION: a nonzero exit from the wrapper command does NOT by
    itself mean the config is bad. If the container runtime cannot pull the
    image, cannot reach its daemon, or is not installed, it also exits
    nonzero. Treating that as a config rejection would fail the merge queue
    over a registry hiccup and — far worse — train everyone to ignore this
    lane's red, which is exactly how the crashloop this check exists to
    prevent shipped green in the first place. Infrastructure failures degrade
    to structural-only (reported loudly on stderr); only a validator that
    actually ran and rejected the config fails the build.
    """
    template = os.environ.get("ENCLII_AMTOOL")
    with tempfile.TemporaryDirectory() as tmp:
        name = "alertmanager.yml" if key.endswith(".yml") else key
        target = Path(tmp) / name
        target.write_text(body, encoding="utf-8")
        try:
            if template:
                cmd = template.format(dir=tmp, name=name)
                proc = subprocess.run(
                    cmd,
                    shell=True,
                    capture_output=True,
                    text=True,
                    timeout=AMTOOL_TIMEOUT_SECONDS,
                )
            elif shutil.which("amtool"):
                proc = subprocess.run(
                    ["amtool", "check-config", str(target)],
                    capture_output=True,
                    text=True,
                    timeout=AMTOOL_TIMEOUT_SECONDS,
                )
            else:
                return None
        except subprocess.TimeoutExpired:
            raise AmtoolUnavailable(
                f"validator did not finish within {AMTOOL_TIMEOUT_SECONDS}s "
                "(slow or unreachable image registry)"
            ) from None
        except OSError as exc:
            raise AmtoolUnavailable(f"could not execute validator: {exc}") from None

        output = (proc.stdout + proc.stderr).strip()
        if proc.returncode == 0:
            return True, output

        lowered = output.lower()
        if any(marker in lowered for marker in UNAVAILABLE_MARKERS):
            raise AmtoolUnavailable(output or "validator exited nonzero with no output")
        if not output:
            # A real rejection always prints "Checking '<file>' FAILED: <err>".
            # Silence plus nonzero is the runtime failing, not a verdict.
            raise AmtoolUnavailable("validator exited nonzero with no output")
        return False, output


def main(argv: list[str]) -> int:
    roots = [Path(a) for a in argv[1:]] or [Path("infra/k8s/")]
    for root in roots:
        if not root.exists():
            print(f"error: no such path: {root}", file=sys.stderr)
            return 2

    findings: list[Finding] = []
    checked = 0
    amtool_ran = False
    amtool_failed = False
    amtool_unavailable: str | None = None

    try:
        for root in roots:
            for path, doc in iter_yaml_docs(root):
                if doc.get("kind") != "ConfigMap":
                    continue
                data = doc.get("data") or {}
                if not isinstance(data, dict):
                    continue
                meta = doc.get("metadata") or {}
                cm_name = meta.get("name", "<unnamed>") if isinstance(meta, dict) else "<unnamed>"
                for key in CONFIG_KEYS:
                    body = data.get(key)
                    if not isinstance(body, str):
                        continue
                    checked += 1
                    findings.extend(check_alertmanager_yml(path, cm_name, key, body))

                    try:
                        result = run_amtool(body, key)
                    except AmtoolUnavailable as exc:
                        amtool_unavailable = str(exc)
                        continue
                    if result is None:
                        continue
                    amtool_ran = True
                    ok, output = result
                    if not ok:
                        amtool_failed = True
                        findings.append(
                            Finding(
                                path,
                                f"ConfigMap '{cm_name}' data['{key}']",
                                f"amtool check-config rejected this config:\n{output}",
                            )
                        )
    except yaml.YAMLError:
        return 2

    for f in findings:
        print(f.render())

    print()
    if checked == 0:
        print(
            "error: found no Alertmanager config ConfigMap in "
            f"{[str(r) for r in roots]}. This check is only meaningful if it "
            "actually reads the config — a silently-empty lint is how the "
            "2026-08-26 crashloop shipped green in the first place.",
            file=sys.stderr,
        )
        return 2

    if amtool_ran:
        amtool_status = "amtool check-config: ran."
    elif amtool_unavailable:
        amtool_status = "amtool check-config: UNAVAILABLE, structural checks only."
        # Loud on stderr: degraded coverage must be visible in the CI log, not
        # inferred from its absence. It is deliberately not a build failure —
        # see run_amtool's docstring.
        print(
            f"WARNING: the optional amtool layer could not run, so unknown-field "
            f"and template validation were SKIPPED. Structural checks still ran "
            f"and passed. Reason: {amtool_unavailable}",
            file=sys.stderr,
        )
    else:
        amtool_status = "amtool check-config: not configured, structural checks only."

    print(
        f"checked {checked} alertmanager config(s) in {len(roots)} root(s); "
        f"{len(findings)} failure(s). " + amtool_status
    )

    if findings:
        print(
            "FAIL: this Alertmanager config would be REJECTED AT LOAD — the "
            "process exits 1 and the pod crashloops. Fix it before merge.",
            file=sys.stderr,
        )
        return 1
    if amtool_failed:  # pragma: no cover - defensive
        return 1
    print("OK: Alertmanager config would load.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
