"""Tests for :mod:`enclii_sdk.resources.rollback`."""

from __future__ import annotations

from collections.abc import Callable

import httpx

from enclii_sdk import AsyncEncliiClient


async def test_rollback_deployment(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["body"] = request.read().decode()
        return httpx.Response(200, json={})

    client = make_client(handler)
    await client.rollback.rollback_deployment(
        "dep_123", to_release="rel_prev", change_ticket_url="JIRA-42"
    )
    assert captured["path"] == "/v1/deployments/dep_123/rollback"
    assert "rel_prev" in captured["body"]
    assert "JIRA-42" in captured["body"]


async def test_instant_rollback(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/services/svc_123/rollback"
        return httpx.Response(200, json=sample_deployment_payload)

    client = make_client(handler)
    dep = await client.rollback.instant_rollback(
        "svc_123", change_ticket_url="JIRA-42"
    )
    assert dep.version_number == 42
