"""Tests for :mod:`enclii_sdk.resources.audit`."""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from uuid import uuid4

import httpx

from enclii_sdk import AsyncEncliiClient


def _audit_row() -> dict:
    return {
        "id": str(uuid4()),
        "timestamp": datetime.now(UTC).isoformat(),
        "actor_email": "ci@madfam.io",
        "actor_role": "system",
        "action": "deploy",
        "resource_type": "service",
        "resource_id": str(uuid4()),
        "resource_name": "api",
        "outcome": "success",
        "ip_address": "10.0.0.1",
        "user_agent": "enclii-sdk-py/0.1.0",
        "context": {"pr_url": "https://github.com/x/y/pull/1"},
    }


async def test_list_audit_paged(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/audit"
        return httpx.Response(
            200,
            json={
                "items": [_audit_row(), _audit_row()],
                "next_cursor": "c_next",
                "total": 100,
            },
        )

    client = make_client(handler)
    page = await client.audit.list(limit=2)
    assert len(page.items) == 2
    assert page.next_cursor == "c_next"
    assert page.total == 100


async def test_list_audit_forwards_filters(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["params"] = dict(request.url.params)
        return httpx.Response(200, json={"items": []})

    client = make_client(handler)
    await client.audit.list(actor="ci@madfam.io", action="deploy", resource_type="service")
    assert captured["params"]["actor"] == "ci@madfam.io"
    assert captured["params"]["action"] == "deploy"
    assert captured["params"]["resource_type"] == "service"


async def test_legacy_activity(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/activity"
        return httpx.Response(200, json={"activity": [_audit_row()]})

    client = make_client(handler)
    rows = await client.audit.legacy_activity()
    assert len(rows) == 1
