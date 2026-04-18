"""Tests for :mod:`enclii_sdk.resources.deployments`."""

from __future__ import annotations

from collections.abc import Callable

import httpx
import pytest

from enclii_sdk import AsyncEncliiClient, EncliiError


async def test_get_by_id(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/deployments/dep_123"
        return httpx.Response(200, json=sample_deployment_payload)

    client = make_client(handler)
    dep = await client.deployments.get("dep_123")
    assert dep.version_label == "v42"


async def test_get_by_version_label(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    """Heroku-style v-number lookup: /v1/services/{id}/versions/{v}."""
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        return httpx.Response(200, json=sample_deployment_payload)

    client = make_client(handler)
    await client.deployments.get("svc_123", version="v42")
    assert captured["path"] == "/v1/services/svc_123/versions/v42"

    await client.deployments.get("svc_123", version=42)
    assert captured["path"] == "/v1/services/svc_123/versions/42"


async def test_latest(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/services/svc_123/deployments/latest"
        return httpx.Response(200, json=sample_deployment_payload)

    client = make_client(handler)
    dep = await client.deployments.latest("svc_123")
    assert dep.replicas == 2


async def test_list_service_deployments(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"deployments": [sample_deployment_payload]})

    client = make_client(handler)
    deps = await client.deployments.list("svc_123")
    assert len(deps) == 1


async def test_wait_for_running_succeeds(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    """Poll sees pending → deploying → running."""
    state = {"i": 0}
    statuses = ["pending", "deploying", "running"]

    def handler(_: httpx.Request) -> httpx.Response:
        payload = dict(sample_deployment_payload)
        payload["status"] = statuses[min(state["i"], len(statuses) - 1)]
        state["i"] += 1
        return httpx.Response(200, json=payload)

    client = make_client(handler)
    dep = await client.deployments.wait_for_running(
        "dep_123", timeout=5.0, poll_interval=0.01
    )
    assert dep.status.value == "running"
    assert state["i"] >= 3


async def test_wait_for_running_raises_on_failure(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_deployment_payload: dict,
) -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        payload = dict(sample_deployment_payload)
        payload["status"] = "failed"
        payload["error_message"] = "image pull backoff"
        return httpx.Response(200, json=payload)

    client = make_client(handler)
    with pytest.raises(EncliiError, match="image pull backoff"):
        await client.deployments.wait_for_running("dep_123", poll_interval=0.01)
