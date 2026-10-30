"""
Tests for scripts/check-alertmanager-config.py.

Run with:
    pytest tests/scripts/test_check_alertmanager_config.py -v

Read `test_the_2026_08_26_crashloop_config_fails` first: it is the regression
this whole check exists for — a `chat_id: 0` sentinel that Alertmanager
v0.26.0 rejects at CONFIG LOAD, not at send time. Then read
`test_does_not_catch_unknown_fields` and `test_amtool_layer_is_optional`,
which pin the honest limits so nobody later mistakes the structural layer for
a full reimplementation of `amtool check-config`.
"""
from __future__ import annotations

import subprocess
import sys
import textwrap
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
SCRIPT = REPO_ROOT / "scripts" / "check-alertmanager-config.py"
LIVE_CONFIG = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"

from test_alertmanager_routing import (
    COURIER_CREDENTIALS_FILE, COURIER_WEBHOOK_URL, REVENUE_MATCHER,
    _first_severity_route_index, _live_alertmanager_config, _route_for,
)


def run_script(*roots: Path, env: dict | None = None) -> tuple[int, str, str]:
    import os

    merged = dict(os.environ)
    # The live PATH may carry a real amtool; the structural-layer tests must
    # not silently depend on whether it does.
    merged.pop("ENCLII_AMTOOL", None)
    if env:
        merged.update(env)
    proc = subprocess.run(
        [sys.executable, str(SCRIPT), *[str(r) for r in roots]],
        capture_output=True,
        text=True,
        env=merged,
    )
    return proc.returncode, proc.stdout, proc.stderr


def configmap(am_yml: str, name: str = "alertmanager-config") -> str:
    indented = textwrap.indent(textwrap.dedent(am_yml).strip("\n"), "    ")
    return (
        "apiVersion: v1\n"
        "kind: ConfigMap\n"
        "metadata:\n"
        f"  name: {name}\n"
        "  namespace: monitoring\n"
        "data:\n"
        "  alertmanager.yml: |\n" + indented + "\n"
    )


MINIMAL = """
    route:
      receiver: 'default-receiver'
    receivers:
      - name: 'default-receiver'
        email_configs:
          - to: 'admin@madfam.io'
"""


def write(dir_: Path, body: str, name: str = "alertmanager.yaml") -> Path:
    p = dir_ / name
    p.write_text(body)
    return p


# ---------------------------------------------------------------------------
# The regression this check exists for
# ---------------------------------------------------------------------------


def test_the_2026_08_26_crashloop_config_fails(tmp_path):
    """chat_id: 0 — the exact config that crashlooped alertmanager-2."""
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'default-receiver'
              routes:
                - match:
                    severity: critical
                  receiver: 'critical-receiver'
            receivers:
              - name: 'default-receiver'
                email_configs:
                  - to: 'admin@madfam.io'
              - name: 'critical-receiver'
                telegram_configs:
                  - bot_token_file: '/etc/alertmanager/telegram-bot-token'
                    chat_id: 0
                    send_resolved: true
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "missing chat_id on telegram_config" in out


def test_omitted_and_null_chat_id_fail_identically(tmp_path):
    """ChatID is a plain int64: omitted, null and 0 are the same to Go."""
    for literal in ("", "chat_id:", "chat_id: null", "chat_id: 0"):
        d = tmp_path / f"case-{abs(hash(literal))}"
        d.mkdir()
        write(
            d,
            configmap(
                f"""
                route:
                  receiver: 'r'
                receivers:
                  - name: 'r'
                    telegram_configs:
                      - bot_token: 'abc'
                        {literal}
                """
            ),
        )
        code, out, _ = run_script(d)
        assert code == 1, f"{literal!r} should be rejected"
        assert "missing chat_id on telegram_config" in out


def test_real_nonzero_chat_id_passes(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                telegram_configs:
                  - bot_token_file: '/etc/alertmanager/telegram-bot-token'
                    chat_id: -1001234567890
                    parse_mode: ''
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 0, out


def test_telegram_requires_a_token(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                telegram_configs:
                  - chat_id: 42
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "missing bot_token or bot_token_file on telegram_config" in out


def test_telegram_rejects_both_token_forms(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                telegram_configs:
                  - bot_token: 'abc'
                    bot_token_file: '/etc/alertmanager/telegram-bot-token'
                    chat_id: 42
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "at most one of bot_token & bot_token_file must be configured" in out


def test_telegram_rejects_bad_parse_mode(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                telegram_configs:
                  - bot_token: 'abc'
                    chat_id: 42
                    parse_mode: 'markdown'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "unknown parse_mode on telegram_config" in out


# ---------------------------------------------------------------------------
# The live repo config must stay loadable
# ---------------------------------------------------------------------------


def test_live_monitoring_config_would_load():
    """The committed production config must pass. This is the ratchet."""
    code, out, err = run_script(LIVE_CONFIG)
    assert code == 0, (
        f"live alertmanager config would crash on load or breaks a policy "
        f"rule:\n{out}\n{err}"
    )


def test_undefined_receiver_in_route_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'default-receiver'
              routes:
                - match:
                    severity: critical
                  receiver: 'nope'
            receivers:
              - name: 'default-receiver'
                email_configs:
                  - to: 'admin@madfam.io'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert 'undefined receiver "nope" used in route' in out


def test_duplicate_receiver_name_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                email_configs:
                  - to: 'a@madfam.io'
              - name: 'r'
                email_configs:
                  - to: 'b@madfam.io'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert 'notification config name "r" is not unique' in out


def test_root_route_with_matchers_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
              match:
                severity: critical
            receivers:
              - name: 'r'
                email_configs:
                  - to: 'a@madfam.io'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "root route must not have any matchers" in out


def test_email_without_to_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                email_configs:
                  - send_resolved: true
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "missing to address in email config" in out


def test_webhook_without_url_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                webhook_configs:
                  - send_resolved: true
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "one of url or url_file must be configured" in out


def _courier_hook_config(extra: str) -> str:
    return configmap(
        f"""
        route:
          receiver: 'r'
        receivers:
          - name: 'r'
            webhook_configs:
              - url: '{COURIER_WEBHOOK_URL}'
                send_resolved: true
                {extra}
        """
    )


def test_courier_webhook_without_max_alerts_fails_as_policy(tmp_path):
    """The 2026-10-05 class: an uncapped batch is refused whole with 413.

    The config LOADS, so this must read as a policy failure and never as the
    crash message, which would send a reader looking for the wrong bug.
    """
    write(tmp_path, _courier_hook_config(""))
    code, out, err = run_script(tmp_path)
    assert code == 1
    assert "FAIL (policy)" in out
    assert "no positive max_alerts" in out
    assert "breaks an alerting policy rule" in err
    assert "REJECTED AT LOAD" not in err


def test_courier_webhook_with_zero_max_alerts_fails(tmp_path):
    """`max_alerts: 0` is Alertmanager's "no limit", the bug itself."""
    write(tmp_path, _courier_hook_config("max_alerts: 0"))
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "no positive max_alerts (got 0)" in out


def test_courier_webhook_with_max_alerts_passes(tmp_path):
    write(tmp_path, _courier_hook_config("max_alerts: 100"))
    code, out, err = run_script(tmp_path)
    assert code == 0, f"{out}\n{err}"


def test_courier_rule_matches_on_path_not_host(tmp_path):
    """A host or scheme change must not quietly exempt a Courier webhook."""
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                webhook_configs:
                  - url: 'https://courier.example.invalid/v1/courier/alertmanager'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "no positive max_alerts" in out


def test_non_courier_webhook_needs_no_max_alerts(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                webhook_configs:
                  - url: 'http://heartbeat.example.invalid/ping'
            """
        ),
    )
    code, out, err = run_script(tmp_path)
    assert code == 0, f"{out}\n{err}"


def test_negative_max_alerts_is_a_load_rejection(tmp_path):
    """MaxAlerts is a uint64; a negative value fails to unmarshal at load."""
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                webhook_configs:
                  - url: 'http://heartbeat.example.invalid/ping'
                    max_alerts: -1
            """
        ),
    )
    code, out, err = run_script(tmp_path)
    assert code == 1
    assert "cannot unmarshal max_alerts -1 into uint64" in out
    assert "REJECTED AT LOAD" in err


def test_slack_both_api_url_forms_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                slack_configs:
                  - api_url: 'https://hooks.slack.com/x'
                    api_url_file: '/etc/alertmanager/slack-webhook-url'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "at most one of api_url & api_url_file must be configured" in out


def test_slack_with_only_api_url_file_passes(tmp_path):
    """The shape this repo actually ships. Must not be a false positive."""
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                slack_configs:
                  - api_url_file: '/etc/alertmanager/slack-webhook-url'
                    channel: '#alerts-critical'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 0, out


def test_wildcard_group_by_with_other_labels_fails(tmp_path):
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
              group_by: ['...', 'alertname']
            receivers:
              - name: 'r'
                email_configs:
                  - to: 'a@madfam.io'
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "wildcard group_by" in out


# ---------------------------------------------------------------------------
# Harness behavior and honest limits
# ---------------------------------------------------------------------------


def test_missing_config_is_exit_2_not_silent_pass(tmp_path):
    """A lint that finds nothing must fail loudly, not report success.

    The 2026-08-26 crashloop shipped through green CI. A check that quietly
    passes when it read zero configs would reproduce exactly that failure
    mode, so 'found nothing' is exit 2, not exit 0.
    """
    (tmp_path / "unrelated.yaml").write_text("apiVersion: v1\nkind: Service\n")
    code, _, err = run_script(tmp_path)
    assert code == 2
    assert "found no Alertmanager config ConfigMap" in err


def test_nonexistent_path_is_exit_2(tmp_path):
    code, _, err = run_script(tmp_path / "does-not-exist")
    assert code == 2
    assert "no such path" in err


def test_does_not_catch_unknown_fields(tmp_path):
    """HONEST LIMIT: Alertmanager uses yaml.UnmarshalStrict, this does not.

    `chat_i: 123` is rejected by the real loader as an unknown field. The
    structural layer cannot see that — it would instead flag the MISSING
    chat_id, which happens to catch this typo for the right outcome but the
    wrong reason. Run the amtool layer for true strict-field coverage.
    """
    write(
        tmp_path,
        configmap(
            """
            route:
              receiver: 'r'
            receivers:
              - name: 'r'
                telegram_configs:
                  - bot_token: 'abc'
                    chat_i: 123
            """
        ),
    )
    code, out, _ = run_script(tmp_path)
    assert code == 1
    assert "missing chat_id" in out
    assert "unknown field" not in out


def test_amtool_layer_is_optional_and_reports_when_absent(tmp_path):
    write(tmp_path, configmap(MINIMAL))
    code, out, _ = run_script(tmp_path)
    assert code == 0
    assert "structural checks only" in out


def test_infra_failure_degrades_instead_of_failing_the_build(tmp_path):
    """A registry/daemon failure must NOT be reported as a bad config.

    This is the distinction that matters most for trust in the lane: if a
    Docker Hub hiccup turns this check red, people learn to ignore its red —
    and an ignored check is worth less than no check, because it looks like
    coverage. Only a validator that actually RAN and rejected the config may
    fail the build.
    """
    write(tmp_path, configmap(MINIMAL))
    for stderr_text in (
        "docker: command not found",
        "Cannot connect to the Docker daemon at unix:///var/run/docker.sock",
        "failed to resolve reference: dial tcp: i/o timeout",
        "toomanyrequests: You have reached your pull rate limit",
    ):
        code, out, err = run_script(
            tmp_path,
            env={"ENCLII_AMTOOL": f"sh -c 'echo \"{stderr_text}\" >&2; exit 125'"},
        )
        assert code == 0, f"{stderr_text!r} must not fail the build:\n{out}\n{err}"
        assert "UNAVAILABLE" in out
        assert "WARNING" in err, "degraded coverage must be loud on stderr"


def test_config_is_readable_by_an_unprivileged_container_user(tmp_path):
    """Regression: CI failed with 'stat /w/alertmanager.yml: permission denied'.

    tempfile makes its directory 0700 owned by the invoking user, but the
    alertmanager image runs as nobody (65534), so the bind-mounted config was
    unreadable and amtool could not stat it. The script now widens the
    permissions; this asserts the temp file the validator is handed is
    world-readable inside a world-traversable directory.
    """
    write(tmp_path, configmap(MINIMAL))
    probe = tmp_path / "modes.txt"
    # Read the modes in Python, not with stat(1): `stat -f` means "format" on
    # BSD/macOS but "filesystem info" on GNU/Linux, where it SUCCEEDS and
    # silently defeats a `||` fallback. That portability trap failed this very
    # test on the CI runner while the code under test was already correct.
    reader = (
        "import os,stat,sys;"
        "d=sys.argv[1];f=os.path.join(d,sys.argv[2]);"
        "open(sys.argv[3],'w').write("
        "'%o %o' % (stat.S_IMODE(os.stat(d).st_mode),"
        "stat.S_IMODE(os.stat(f).st_mode)))"
    )
    code, _, _ = run_script(
        tmp_path,
        env={
            "ENCLII_AMTOOL": (
                f"{sys.executable} -c \"{reader}\" {{dir}} {{name}} {probe}"
            )
        },
    )
    assert code == 0
    modes = probe.read_text().split()
    assert len(modes) == 2, f"expected dir and file modes, got {modes!r}"
    dir_mode, file_mode = modes
    assert int(dir_mode, 8) & 0o001, f"temp dir {dir_mode} lacks o+x (not traversable)"
    assert int(file_mode, 8) & 0o004, f"config file {file_mode} lacks o+r"


def test_permission_denied_is_not_reported_as_a_bad_config(tmp_path):
    """Defense in depth for the CI regression above."""
    write(tmp_path, configmap(MINIMAL))
    code, out, err = run_script(
        tmp_path,
        env={
            "ENCLII_AMTOOL": (
                "sh -c 'echo \"amtool: error: stat /w/alertmanager.yml: "
                "permission denied\" >&2; exit 1'"
            )
        },
    )
    assert code == 0, "a file the validator cannot read is not a config verdict"
    assert "UNAVAILABLE" in out
    assert "WARNING" in err


def test_silent_nonzero_exit_is_treated_as_unavailable(tmp_path):
    """A real rejection always prints. Silence + nonzero is the runtime dying."""
    write(tmp_path, configmap(MINIMAL))
    code, out, _ = run_script(tmp_path, env={"ENCLII_AMTOOL": "false"})
    assert code == 0
    assert "UNAVAILABLE" in out


def test_amtool_timeout_degrades(tmp_path):
    write(tmp_path, configmap(MINIMAL))
    code, out, err = run_script(
        tmp_path,
        env={"ENCLII_AMTOOL": "sleep 30", "ENCLII_AMTOOL_TIMEOUT": "1"},
    )
    assert code == 0
    assert "UNAVAILABLE" in out
    assert "did not finish" in err


def test_genuine_amtool_rejection_still_fails(tmp_path):
    """The degradation path must not swallow a REAL verdict."""
    write(tmp_path, configmap(MINIMAL))
    code, out, _ = run_script(
        tmp_path,
        env={
            "ENCLII_AMTOOL": (
                "sh -c 'echo \"Checking /w/alertmanager.yml  FAILED: "
                "missing chat_id on telegram_config\"; exit 1'"
            )
        },
    )
    assert code == 1
    assert "amtool check-config rejected this config" in out
    assert "missing chat_id on telegram_config" in out


def test_amtool_layer_runs_when_configured(tmp_path):
    """ENCLII_AMTOOL is honored, and its failure fails the check."""
    write(tmp_path, configmap(MINIMAL))
    code, out, _ = run_script(tmp_path, env={"ENCLII_AMTOOL": "true"})
    assert code == 0
    assert "amtool check-config: ran." in out

    code, out, _ = run_script(
        tmp_path,
        env={
            "ENCLII_AMTOOL": (
                "sh -c 'echo \"Checking /w/x.yml  FAILED: bad config\"; exit 1'"
            )
        },
    )
    assert code == 1
    assert "amtool check-config rejected this config" in out
    assert "bad config" in out


# ---------------------------------------------------------------------------
# Staging never pages (infra-stability audit 2026-10-17, pager noise plan)
# ---------------------------------------------------------------------------

STAGING_LABELS = ("namespace", "application", "name", "dest_namespace")


def _matcher_holds(labels: dict, matcher: str) -> bool:
    import re

    m = re.fullmatch(r'(\w+)(=~|!~|!=|=)"(.*)"', matcher)
    assert m, f"unparsed matcher {matcher!r}"
    key, op, value = m.groups()
    have = labels.get(key, "")
    if op == "=":
        return have == value
    if op == "!=":
        return have != value
    if op == "=~":
        return re.fullmatch(value, have) is not None
    return re.fullmatch(value, have) is None


def _first_route(cfg: dict, labels: dict) -> dict:
    """The first child route Alertmanager would pick (every route here stops)."""
    for route in (cfg.get("route") or {}).get("routes") or []:
        matchers = [str(m) for m in route.get("matchers") or []]
        matchers += [f'{k}="{v}"' for k, v in (route.get("match") or {}).items()]
        if all(_matcher_holds(labels, m) for m in matchers):
            return route
    return cfg["route"]


@pytest.mark.parametrize("label", STAGING_LABELS)
@pytest.mark.parametrize("value", ["selva-office-staging", "enclii-karafiel-staging", "voxa-staging-services"])
def test_staging_criticals_reach_email_only(label, value):
    cfg = _live_alertmanager_config()
    route = _first_route(cfg, {"alertname": "AnyCritical", "severity": "critical", label: value})
    assert route["receiver"] == "staging-receiver"
    by_name = {r.get("name"): r for r in (cfg.get("receivers") or [])}
    recv = by_name["staging-receiver"]
    assert not recv.get("webhook_configs"), "staging-receiver must not reach Courier"
    assert recv.get("email_configs"), "staging criticals must still be visible by email"


@pytest.mark.parametrize("value", ["enclii", "voxa", "dhanam", "staging", "crea-map-ensayo"])
def test_production_criticals_still_page(value):
    cfg = _live_alertmanager_config()
    route = _first_route(cfg, {"alertname": "AnyCritical", "severity": "critical", "namespace": value})
    assert route["receiver"] == "critical-receiver"


def test_a_product_labelled_staging_alert_still_pages():
    cfg = _live_alertmanager_config()
    labels = {
        "alertname": "RevenueEndpointDown",
        "severity": "critical",
        "category": "revenue-probe",
        "product": "example",
        "namespace": "example-staging",
    }
    assert _first_route(cfg, labels)["receiver"] == "critical-receiver"
    labels.pop("category")
    assert _first_route(cfg, labels)["receiver"] == "critical-receiver"


def test_staging_routes_sit_between_the_revenue_and_critical_routes():
    cfg = _live_alertmanager_config()
    routes = (cfg.get("route") or {}).get("routes") or []
    staging = [i for i, r in enumerate(routes) if r.get("receiver") == "staging-receiver"]
    assert len(staging) == len(STAGING_LABELS)
    crit_idx = next(i for i, r in enumerate(routes) if (r.get("match") or {}).get("severity") == "critical")
    assert all(_first_severity_route_index(cfg) < i < crit_idx for i in staging)
