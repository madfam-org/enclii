"""Tests for :mod:`enclii_sdk.resources.secrets`."""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from uuid import uuid4

import httpx

from enclii_sdk import AsyncEncliiClient


def _env_var(
    is_secret: bool = False,
    value: str | None = None,
) -> dict:
    # Default value follows server behavior: masked when secret, plain otherwise.
    # Callers can override `value` explicitly (e.g. for reveal responses).
    if value is None:
        value = "••••••" if is_secret else "postgres://..."
    return {
        "id": str(uuid4()),
        "service_id": str(uuid4()),
        "environment_id": str(uuid4()),
        "key": "DATABASE_URL",
        "value": value,
        "is_secret": is_secret,
        "created_at": datetime.now(UTC).isoformat(),
        "updated_at": datetime.now(UTC).isoformat(),
    }


async def test_list(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"env_vars": [_env_var(), _env_var(True)]})

    client = make_client(handler)
    vars_ = await client.secrets.list("svc_123")
    assert len(vars_) == 2
    assert vars_[1].is_secret is True
    assert vars_[1].value == "••••••"


async def test_create_masked(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = request.read().decode()
        return httpx.Response(201, json=_env_var(is_secret=True))

    client = make_client(handler)
    v = await client.secrets.create(
        "svc_123",
        key="DB_PASSWORD",
        value="super_secret",
        is_secret=True,
    )
    assert '"is_secret":true' in captured["body"].replace(" ", "")
    assert '"value":"super_secret"' in captured["body"].replace(" ", "")
    assert v.is_secret is True


async def test_update(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "PUT"
        return httpx.Response(200, json=_env_var())

    client = make_client(handler)
    await client.secrets.update("svc_123", "var_1", value="new")


async def test_delete(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "DELETE"
        return httpx.Response(204)

    client = make_client(handler)
    await client.secrets.delete("svc_123", "var_1")


async def test_reveal(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path.endswith("/reveal")
        return httpx.Response(200, json=_env_var(is_secret=True, value="revealed_plain"))

    client = make_client(handler)
    v = await client.secrets.reveal("svc_123", "var_1")
    # Server controls masking — we just propagate it.
    assert v.value == "revealed_plain"
