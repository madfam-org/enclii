"""Tests for :mod:`enclii_sdk.resources.webhooks`."""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from uuid import uuid4

import httpx

from enclii_sdk import AsyncEncliiClient
from enclii_sdk.models.webhooks import OutboundWebhookEventType


async def test_list_event_types(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/lifecycle-webhooks/event-types"
        return httpx.Response(
            200,
            json=["deploy.started", "deploy.succeeded", "deploy.failed"],
        )

    client = make_client(handler)
    events = await client.webhooks.list_event_types()
    assert "deploy.succeeded" in events


async def test_create_subscription_returns_signing_secret(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_webhook_subscription: dict,
) -> None:
    response_body = {
        "subscription": sample_webhook_subscription,
        "signing_secret": "whsec_real_plaintext_abcdef123456",
        "note": "Save this now — we won't show it again.",
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/projects/demo/lifecycle-webhooks"
        body = request.read().decode()
        assert "deploy.succeeded" in body
        return httpx.Response(201, json=response_body)

    client = make_client(handler)
    resp = await client.webhooks.create(
        "demo",
        name="Slack #deploys",
        url="https://hooks.slack.com/services/example",
        events=[
            OutboundWebhookEventType.DEPLOY_SUCCEEDED,
            OutboundWebhookEventType.DEPLOY_FAILED,
        ],
    )
    assert resp.signing_secret.startswith("whsec_")
    assert resp.subscription.name == "Slack #deploys"


async def test_create_accepts_string_event_types(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_webhook_subscription: dict,
) -> None:
    response_body = {
        "subscription": sample_webhook_subscription,
        "signing_secret": "whsec_x",
        "note": "",
    }

    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(201, json=response_body)

    client = make_client(handler)
    resp = await client.webhooks.create(
        "demo",
        name="Slack",
        url="https://example.com/hooks",
        events=["deploy.succeeded"],
    )
    assert resp.signing_secret == "whsec_x"


async def test_get_subscription(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_webhook_subscription: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == f"/v1/lifecycle-webhooks/{sample_webhook_subscription['id']}"
        return httpx.Response(200, json=sample_webhook_subscription)

    client = make_client(handler)
    sub = await client.webhooks.get(sample_webhook_subscription["id"])
    assert sub.active is True


async def test_update_subscription_patches(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_webhook_subscription: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode()
        # Only active should be in the body — unset fields stripped.
        assert '"active"' in body
        assert '"name"' not in body
        return httpx.Response(200, json={**sample_webhook_subscription, "active": False})

    client = make_client(handler)
    sub = await client.webhooks.update(sample_webhook_subscription["id"], active=False)
    assert sub.active is False


async def test_delete_subscription(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_webhook_subscription: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "DELETE"
        return httpx.Response(204)

    client = make_client(handler)
    await client.webhooks.delete(sample_webhook_subscription["id"])


async def test_rotate_secret(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_webhook_subscription: dict,
) -> None:
    body = {
        "subscription": sample_webhook_subscription,
        "signing_secret": "whsec_rotated",
        "note": "",
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path.endswith("/rotate-secret")
        return httpx.Response(200, json=body)

    client = make_client(handler)
    resp = await client.webhooks.rotate_secret(sample_webhook_subscription["id"])
    assert resp.signing_secret == "whsec_rotated"


async def test_test_ping(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    delivery = {
        "id": str(uuid4()),
        "subscription_id": str(uuid4()),
        "event_id": "evt_ping",
        "event_type": "deploy.started",
        "payload_sha256": "abc",
        "attempt_number": 1,
        "status": "delivered",
        "created_at": datetime.now(UTC).isoformat(),
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path.endswith("/test")
        return httpx.Response(200, json={"delivery": delivery})

    client = make_client(handler)
    result = await client.webhooks.test("sub_123")
    assert result.status.value == "delivered"


async def test_list_deliveries(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    delivery = {
        "id": str(uuid4()),
        "subscription_id": str(uuid4()),
        "event_id": "evt_1",
        "event_type": "deploy.succeeded",
        "payload_sha256": "sha",
        "attempt_number": 1,
        "status": "delivered",
        "created_at": datetime.now(UTC).isoformat(),
    }

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path.endswith("/deliveries")
        return httpx.Response(200, json={"deliveries": [delivery]})

    client = make_client(handler)
    deliveries = await client.webhooks.list_deliveries("sub_123")
    assert len(deliveries) == 1
