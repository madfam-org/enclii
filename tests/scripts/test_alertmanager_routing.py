"""Live receiver/route policy contracts, separate from config parser validation."""
from pathlib import Path

import pytest

REPO_ROOT = Path(__file__).resolve().parents[2]
LIVE_CONFIG = REPO_ROOT / "infra" / "k8s" / "production" / "monitoring"

def test_live_config_has_no_telegram_configs():
    """Telegram is deliberately absent until BOTH prerequisites land.

    Re-adding a telegram_configs block with a real chat_id is fine and this
    test should be updated in that same commit. Re-adding one WITHOUT a real
    chat_id is the outage; the checker above blocks that independently.

    Asserts on the PARSED config, not the file text: the surrounding YAML
    comments deliberately say the words "telegram_configs" and "chat_id" to
    explain why they are absent, and a text grep would trip over its own
    documentation.
    """
    import yaml

    docs = yaml.safe_load_all((LIVE_CONFIG / "alertmanager.yaml").read_text())
    cms = [
        d
        for d in docs
        if isinstance(d, dict)
        and d.get("kind") == "ConfigMap"
        and (d.get("metadata") or {}).get("name") == "alertmanager-config"
    ]
    assert len(cms) == 1, "expected exactly one alertmanager-config ConfigMap"
    cfg = yaml.safe_load(cms[0]["data"]["alertmanager.yml"])
    for recv in cfg.get("receivers") or []:
        assert "telegram_configs" not in recv, (
            f"receiver {recv.get('name')!r} has telegram_configs; if this is "
            "intentional it must carry a real nonzero chat_id and a populated "
            "Vault telegram_bot_token — update this test in the same commit"
        )


def _live_alertmanager_config() -> dict:
    """The parsed `alertmanager.yml` out of the live ConfigMap.

    Asserting on the PARSED config rather than the file text, for the same
    reason the telegram test above does: the surrounding YAML comments
    deliberately say "slack_configs" and "Slack" to explain why they are gone,
    and a text grep would trip over its own documentation.
    """
    import yaml

    docs = yaml.safe_load_all((LIVE_CONFIG / "alertmanager.yaml").read_text())
    cms = [
        d
        for d in docs
        if isinstance(d, dict)
        and d.get("kind") == "ConfigMap"
        and (d.get("metadata") or {}).get("name") == "alertmanager-config"
    ]
    assert len(cms) == 1, "expected exactly one alertmanager-config ConfigMap"
    return yaml.safe_load(cms[0]["data"]["alertmanager.yml"])


COURIER_WEBHOOK_URL = (
    "http://angelia-api.angelia.svc.cluster.local/v1/courier/alertmanager"
)
COURIER_CREDENTIALS_FILE = "/etc/alertmanager/courier/courier-alertmanager-secret"


def test_live_config_has_no_slack_configs():
    """Third-party messaging goes through Angelia Courier and nothing else.

    internal-devops decisions/2026-09-05-third-party-messaging-via-angelia-courier.md
    § 2: the `slack_configs` receivers are non-compliant and are REMOVED, not
    left inert. A re-added block is a message that leaves the estate with no
    ledger row, which is a policy violation and not merely a duplicate page.
    """
    cfg = _live_alertmanager_config()
    for recv in cfg.get("receivers") or []:
        assert "slack_configs" not in recv, (
            f"receiver {recv.get('name')!r} has slack_configs; third-party "
            "messaging must go through Angelia Courier's webhook receiver"
        )


def test_live_config_pages_through_courier():
    """Both severity receivers POST to the Courier receiver, authenticated.

    A missing `credentials_file` would not fail config load — Alertmanager
    would send an unauthenticated webhook, Courier would answer 401, and the
    page would be lost with the config looking correct. That is exactly the
    "configured, healthy-looking, and dead" shape, so it is pinned here.
    """
    cfg = _live_alertmanager_config()
    by_name = {r.get("name"): r for r in (cfg.get("receivers") or [])}
    for name in ("critical-receiver", "warning-receiver"):
        recv = by_name.get(name)
        assert recv is not None, f"receiver {name!r} is missing"
        hooks = recv.get("webhook_configs") or []
        assert len(hooks) == 1, f"receiver {name!r} should have one webhook_config"
        hook = hooks[0]
        assert hook["url"] == COURIER_WEBHOOK_URL
        auth = hook["http_config"]["authorization"]
        assert auth["type"] == "Bearer"
        assert auth["credentials_file"] == COURIER_CREDENTIALS_FILE
        # The email fallback is what carries alerts when angelia-api is down.
        assert recv.get("email_configs"), (
            f"receiver {name!r} lost its email_configs; email is the fallback "
            "that does not depend on the messaging path"
        )


def test_courier_receiver_down_is_routed_to_email_first():
    """The failure of the messaging path must not use the messaging path.

    CourierReceiverDown is `severity: critical`. If its route were below the
    severity routes, or carried `continue: true`, the alert about the dead
    pager would be delivered through the dead pager.
    """
    cfg = _live_alertmanager_config()
    routes = (cfg.get("route") or {}).get("routes") or []
    assert routes, "root route has no child routes"
    index, first = _route_for(cfg, "CourierReceiverDown")
    # Explicit MAP no-email holds precede the global fallback. They cannot
    # intercept a non-MAP pager failure and must never carry SMTP themselves.
    map_labels = {"namespace", "service", "application", "product", "name", "dest_namespace"}
    receivers = {r["name"]: r for r in cfg["receivers"]}
    for prefix in routes[:index]:
        assert prefix.get("matchers") in [[f'{label}="crea-map"'] for label in map_labels]
        assert prefix.get("continue") is False
        assert not receivers[prefix["receiver"]].get("email_configs")
    assert index < _first_severity_route_index(cfg)
    assert first.get("receiver") == "default-receiver"
    assert first.get("continue") is False
    default = next(
        r for r in cfg["receivers"] if r.get("name") == "default-receiver"
    )
    assert default.get("email_configs"), "default-receiver must be email"
    assert "webhook_configs" not in default, (
        "default-receiver carries the CourierReceiverDown alert and must not "
        "depend on the receiver that alert reports as broken"
    )


@pytest.mark.parametrize("label", ["namespace", "service", "application", "product", "name", "dest_namespace"])
def test_map_identity_stops_before_every_email_fallback(label):
    cfg = _live_alertmanager_config()
    routes = cfg["route"]["routes"]
    index, scoped = next((i, r) for i, r in enumerate(routes) if r.get("matchers") == [f'{label}="crea-map"'])
    pager_index, _ = _route_for(cfg, "CourierReceiverDown")
    assert index < pager_index
    assert scoped.get("continue") is False
    receiver = next(r for r in cfg["receivers"] if r["name"] == scoped["receiver"])
    assert not receiver.get("email_configs")
    assert receiver.get("webhook_configs"), "operational visibility must retain a non-email path"


def _route_for(cfg: dict, alertname: str) -> tuple[int, dict]:
    routes = (cfg.get("route") or {}).get("routes") or []
    for i, route in enumerate(routes):
        if any(f'alertname="{alertname}"' in str(m) for m in route.get("matchers") or []):
            return i, route
    raise AssertionError(f"no child route matches alertname={alertname}")


def _first_severity_route_index(cfg: dict) -> int:
    routes = (cfg.get("route") or {}).get("routes") or []
    for i, route in enumerate(routes):
        sev = (route.get("match") or {}).get("severity")
        if sev in ("critical", "warning") or any(
            str(m).startswith(("severity=\"critical", "severity=\"warning"))
            for m in route.get("matchers") or []
        ):
            return i
    return len(routes)


def test_email_failure_is_routed_to_courier_only():
    """The mirror of CourierReceiverDown: dead email must not report by email.

    AlertmanagerEmailDeliveryFailing is `severity: critical`. Routed by
    severity it would reach critical-receiver, whose email_configs are the
    thing it reports as broken. On 2026-09-24 email had failed for weeks with
    nothing saying so.
    """
    cfg = _live_alertmanager_config()
    idx, route = _route_for(cfg, "AlertmanagerEmailDeliveryFailing")
    assert idx < _first_severity_route_index(cfg), (
        "the AlertmanagerEmailDeliveryFailing route must sit above the "
        "severity routes, or the critical route catches it first"
    )
    assert route.get("continue") is False
    by_name = {r.get("name"): r for r in (cfg.get("receivers") or [])}
    recv = by_name.get(route.get("receiver"))
    assert recv is not None, f"undefined receiver {route.get('receiver')!r}"
    assert "email_configs" not in recv, (
        "the receiver for AlertmanagerEmailDeliveryFailing must not depend on "
        "the email path that alert reports as broken"
    )
    hooks = recv.get("webhook_configs") or []
    assert len(hooks) == 1 and hooks[0]["url"] == COURIER_WEBHOOK_URL
    auth = hooks[0]["http_config"]["authorization"]
    assert auth["credentials_file"] == COURIER_CREDENTIALS_FILE


# Courier's receiver is Fastify with its default bodyLimit (angelia
# apps/api/src/courier/routes.ts sets none). The per-alert budget is the
# pessimistic size the alertmanager.yaml note derives (largest rule in this
# repo about 2.7 KB serialised, budgeted at 4 KiB).
COURIER_BODY_LIMIT_BYTES = 1024 * 1024
ALERT_BUDGET_BYTES = 4 * 1024
ENVELOPE_BUDGET_BYTES = 8 * 1024


def test_every_live_courier_webhook_is_batch_capped_with_2x_headroom():
    """No Courier batch can outgrow the receiver's body limit.

    On 2026-10-05 the uncapped warning digest (3128-3572 alerts) was refused
    whole with `413 FST_ERR_CTP_BODY_TOO_LARGE` every 3h, and each refusal
    fired CourierReceiverDown, a false page. Every Courier webhook is capped,
    critical ones too: a critical storm past the limit is refused the same
    way and takes the page with it.
    """
    cfg = _live_alertmanager_config()
    hooks = [
        (recv.get("name"), hook)
        for recv in cfg.get("receivers") or []
        for hook in recv.get("webhook_configs") or []
        if hook.get("url") == COURIER_WEBHOOK_URL
    ]
    names = sorted(name for name, _ in hooks)
    assert names == ["courier-only-receiver", "critical-receiver", "warning-receiver"]
    for name, hook in hooks:
        cap = hook.get("max_alerts")
        assert isinstance(cap, int) and cap > 0, f"{name}: max_alerts={cap!r}"
        worst = cap * ALERT_BUDGET_BYTES + ENVELOPE_BUDGET_BYTES
        assert worst * 2 <= COURIER_BODY_LIMIT_BYTES, (
            f"{name}: max_alerts={cap} gives a worst-case batch of {worst} B, "
            f"less than 2x headroom under Courier's {COURIER_BODY_LIMIT_BYTES} B"
        )


def test_critical_route_keeps_paging_within_the_email_budget():
    """Every critical still pages, and the timings stay inside Gmail's cap.

    The route timings were set by replaying real ALERTS against the ~500/day
    consumer cap (see the note above `route:`). Loosening them silently puts
    the fallback back over the cap; dropping the critical route stops paging.
    """
    cfg = _live_alertmanager_config()
    routes = (cfg.get("route") or {}).get("routes") or []
    crit = [r for r in routes if (r.get("match") or {}).get("severity") == "critical"]
    assert len(crit) == 1, "exactly one severity=critical route expected"
    crit = crit[0]
    assert crit["receiver"] == "critical-receiver"
    assert crit.get("group_by") == ["alertname"]
    assert crit.get("repeat_interval") == "4h"
    assert crit.get("group_interval") == "15m"
    warn = [r for r in routes if (r.get("match") or {}).get("severity") == "warning"]
    assert len(warn) == 1 and warn[0].get("group_by") == ["severity"], (
        "warnings are one digest group; per-alertname warning groups churn"
    )


REVENUE_MATCHER = 'category="revenue-probe"'


def test_revenue_probes_page_per_product_above_the_critical_route():
    """A second product's revenue outage must open its own page.

    Under the generic critical route (group_by alertname) a RevenueEndpointDown
    for a new product joined the group already paging for another, went out
    only at that group's next group_interval tick and read as the same page;
    a standing page on one product masked the next one. The revenue route
    groups by product, sits above the critical route (which would otherwise
    match first), and pages through the same receiver. The rules side of the
    contract (every revenue rule carries category=revenue-probe, every target
    a product) is pinned in tests/scripts/test_revenue_probe_rules.py.
    """
    cfg = _live_alertmanager_config()
    routes = (cfg.get("route") or {}).get("routes") or []
    revenue = [
        (i, r) for i, r in enumerate(routes)
        if REVENUE_MATCHER in [str(m) for m in r.get("matchers") or []]
    ]
    assert len(revenue) == 1, f"expected one {REVENUE_MATCHER} route, got {revenue!r}"
    idx, route = revenue[0]
    crit_idx = next(
        i for i, r in enumerate(routes)
        if (r.get("match") or {}).get("severity") == "critical"
    )
    assert idx < crit_idx, "the revenue route must sit above the generic critical route"
    assert idx == _first_severity_route_index(cfg), (
        "the alertname and info routes above must keep catching their alerts first"
    )
    assert [str(m) for m in route["matchers"]] == [REVENUE_MATCHER, 'severity="critical"']
    assert route["receiver"] == "critical-receiver"
    assert route.get("continue") is False
    assert route.get("group_by") == ["alertname", "product"]
    assert route.get("group_wait") == "10s"
    assert route.get("group_interval") == "5m"
    assert route.get("repeat_interval") == "1h"


def test_informational_severities_go_to_a_sink():
    cfg = _live_alertmanager_config()
    routes = (cfg.get("route") or {}).get("routes") or []
    idx = next(
        i for i, r in enumerate(routes)
        if any('severity=~"info|none"' in str(m) for m in r.get("matchers") or [])
    )
    assert idx < _first_severity_route_index(cfg)
    by_name = {r.get("name"): r for r in (cfg.get("receivers") or [])}
    recv = by_name[routes[idx]["receiver"]]
    assert [k for k in recv if k.endswith("_configs")] == []


def test_courier_receiver_down_mutes_watchdog_so_the_heartbeat_pages():
    """CourierReceiverDown's non-email backstop (money-machine audit M2 F7).

    CourierReceiverDown is delivered by email only. While it fires, the
    always-firing Watchdog must be muted, so the beats to the outside
    heartbeat Worker stop and the Worker pages Telegram directly. The rule
    must name exactly those two alerts and carry no `equal`: Watchdog and
    CourierReceiverDown share no label that would make `equal` match, so an
    `equal` would silently disable the backstop.
    """
    cfg = _live_alertmanager_config()
    rules = [
        r for r in cfg.get("inhibit_rules") or []
        if 'alertname="CourierReceiverDown"' in [str(m) for m in r.get("source_matchers") or []]
    ]
    assert len(rules) == 1, f"expected one CourierReceiverDown inhibit rule, got {rules!r}"
    (rule,) = rules
    assert [str(m) for m in rule.get("target_matchers") or []] == ['alertname="Watchdog"']
    assert not rule.get("equal"), "an `equal` list would never match Watchdog"
    # The backstop only reaches a person through the heartbeat; it must never
    # mute anything that pages.
    assert not rule.get("target_match") and not rule.get("target_match_re")


def test_watchdog_is_routed_to_a_sink_above_the_severity_routes():
    """Watchdog always fires; it must be visible in Alertmanager, not delivered.

    Unrouted, it falls to default-receiver and emails every repeat_interval
    forever. Routed to a receiver with an integration, it pages forever.
    """
    cfg = _live_alertmanager_config()
    idx, route = _route_for(cfg, "Watchdog")
    assert idx < _first_severity_route_index(cfg)
    assert route.get("continue") is False
    by_name = {r.get("name"): r for r in (cfg.get("receivers") or [])}
    recv = by_name.get(route.get("receiver"))
    assert recv is not None, f"undefined receiver {route.get('receiver')!r}"
    integrations = [k for k in recv if k.endswith("_configs")]
    assert integrations == [], (
        f"Watchdog's receiver must have no integrations until an outside "
        f"heartbeat is sanctioned; found {integrations}"
    )


# ---------------------------------------------------------------------------
# Other load-time rejections
# ---------------------------------------------------------------------------


