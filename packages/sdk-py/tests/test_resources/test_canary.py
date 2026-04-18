"""Tests for :mod:`enclii_sdk.resources.canary`."""

from __future__ import annotations

from collections.abc import Callable

import httpx
import pytest

from enclii_sdk import AsyncEncliiClient
from enclii_sdk.models.canary import CanaryRolloutState


async def test_start_canary(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_canary_payload: dict,
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["body"] = request.read().decode()
        return httpx.Response(202, json=sample_canary_payload)

    client = make_client(handler)
    rollout = await client.canary.start(
        "svc_123",
        digest="sha256:deadbeef",
        percentage=20,
        validation_window_minutes=10,
        change_ticket_url="JIRA-42",
    )
    assert captured["path"] == "/v1/services/svc_123/canary"
    assert '"percentage":20' in captured["body"].replace(" ", "")
    assert "sha256:deadbeef" in captured["body"]
    assert rollout.state == CanaryRolloutState.RUNNING
    assert rollout.actual_percentage == 20.0


async def test_start_canary_validates_percentage() -> None:
    from pydantic import ValidationError as PydanticValidationError

    from enclii_sdk.models.canary import StartCanaryRequest

    with pytest.raises(PydanticValidationError):
        StartCanaryRequest(digest="x", percentage=99)


async def test_get_canary(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_canary_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/services/svc_123/canary/ro_1"
        return httpx.Response(200, json=sample_canary_payload)

    client = make_client(handler)
    rollout = await client.canary.get("svc_123", "ro_1")
    assert rollout.canary_percentage == 20


async def test_list_canary_accepts_both_shapes(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_canary_payload: dict,
) -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=[sample_canary_payload])

    client = make_client(handler)
    rollouts = await client.canary.list("svc_123")
    assert len(rollouts) == 1


async def test_promote_canary(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_canary_payload: dict,
) -> None:
    payload = {**sample_canary_payload, "state": "promoting"}

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/services/svc_123/canary/ro_1/promote"
        return httpx.Response(200, json=payload)

    client = make_client(handler)
    rollout = await client.canary.promote("svc_123", "ro_1")
    assert rollout.state == CanaryRolloutState.PROMOTING


async def test_rollback_canary(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_canary_payload: dict,
) -> None:
    payload = {**sample_canary_payload, "state": "manual_rolled_back"}

    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/services/svc_123/canary/ro_1/rollback"
        body = request.read().decode()
        assert "regression" in body
        return httpx.Response(200, json=payload)

    client = make_client(handler)
    rollout = await client.canary.rollback("svc_123", "ro_1", reason="regression")
    assert rollout.state.is_terminal()


def test_canary_state_terminal_classification() -> None:
    assert CanaryRolloutState.SUCCEEDED.is_terminal()
    assert CanaryRolloutState.AUTO_ROLLED_BACK.is_terminal()
    assert CanaryRolloutState.FAILED.is_terminal()
    assert not CanaryRolloutState.RUNNING.is_terminal()
    assert CanaryRolloutState.VALIDATING.is_active()
