"""Tests for :mod:`enclii_sdk.resources.logs`.

REST query path is tested with :class:`httpx.MockTransport`. The WebSocket
tail helper is exercised via focused unit tests of URL building and
frame decoding — a full round-trip lives in the integration suite
(pytest -m integration) where a live API is available.
"""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime

import httpx
import pytest

from enclii_sdk import AsyncEncliiClient
from enclii_sdk.models.logs import LogEntry, LogLevel
from enclii_sdk.resources.logs import LogsResource, _decode_frame


async def test_query_builds_query_string(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_log_entry: dict,
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["path"] = request.url.path
        captured["params"] = dict(request.url.params.multi_items())
        captured["level_values"] = request.url.params.get_list("level")
        return httpx.Response(
            200,
            json={
                "entries": [sample_log_entry],
                "next_cursor": "cursor_abc",
                "reached_live_tail": False,
            },
        )

    client = make_client(handler)
    resp = await client.logs.query(
        "svc_123",
        level=[LogLevel.ERROR, LogLevel.WARN],
        search="oh no",
        limit=100,
    )
    assert captured["path"] == "/v1/services/svc_123/logs"
    assert sorted(captured["level_values"]) == ["error", "warn"]
    assert captured["params"]["search"] == "oh no"
    assert captured["params"]["limit"] == "100"
    assert len(resp.entries) == 1
    assert resp.entries[0].level == LogLevel.ERROR
    assert resp.next_cursor == "cursor_abc"


async def test_query_single_level(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["level_values"] = request.url.params.get_list("level")
        return httpx.Response(200, json={"entries": [], "reached_live_tail": True})

    client = make_client(handler)
    await client.logs.query("svc_123", level=LogLevel.ERROR)
    assert captured["level_values"] == ["error"]


async def test_query_with_datetime(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["since"] = request.url.params.get("since")
        return httpx.Response(200, json={"entries": [], "reached_live_tail": True})

    client = make_client(handler)
    now = datetime(2026, 4, 17, 12, 0, tzinfo=UTC)
    await client.logs.query("svc_123", since=now)
    assert captured["since"] == "2026-04-17T12:00:00+00:00"


async def test_query_duration_string(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["since"] = request.url.params.get("since")
        return httpx.Response(200, json={"entries": [], "reached_live_tail": True})

    client = make_client(handler)
    await client.logs.query("svc_123", since="2h")
    assert captured["since"] == "2h"


async def test_query_with_cursor(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["cursor"] = request.url.params.get("cursor")
        return httpx.Response(200, json={"entries": [], "reached_live_tail": False})

    client = make_client(handler)
    await client.logs.query("svc_123", cursor="abc")
    assert captured["cursor"] == "abc"


# ---------------------------------------------------------------------------
# WebSocket tail helpers (unit tests only — integration path runs live)
# ---------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("base_url", "expected_prefix"),
    [
        ("https://api.enclii.dev", "wss://api.enclii.dev"),
        ("http://localhost:4200", "ws://localhost:4200"),
    ],
)
async def test_ws_url_scheme_conversion(base_url: str, expected_prefix: str) -> None:
    async with AsyncEncliiClient(base_url=base_url, token="x") as client:
        res = LogsResource(client)
        url = res._build_ws_url("/v1/services/svc/logs/tail", level=None, search=None)
        assert url.startswith(expected_prefix)
        assert url.endswith("/v1/services/svc/logs/tail")


async def test_ws_url_adds_level_and_search() -> None:
    async with AsyncEncliiClient(base_url="https://api.enclii.dev", token="x") as client:
        res = LogsResource(client)
        url = res._build_ws_url(
            "/v1/services/svc/logs/tail",
            level=[LogLevel.ERROR, LogLevel.WARN],
            search="db",
        )
        assert "level=error" in url
        assert "level=warn" in url
        assert "search=db" in url


def test_decode_frame_entry() -> None:
    frame = _decode_frame('{"type":"entry","entry":{"message":"hi"}}')
    assert frame is not None
    assert frame["type"] == "entry"


def test_decode_frame_bytes() -> None:
    frame = _decode_frame(b'{"type":"ping"}')
    assert frame is not None
    assert frame["type"] == "ping"


def test_decode_frame_malformed_returns_none() -> None:
    assert _decode_frame("not json") is None
    assert _decode_frame("[]") is None  # not a dict
    assert _decode_frame(b"\xff\xfe") is None  # bad utf-8


def test_log_entry_parses_labels() -> None:
    entry = LogEntry.model_validate(
        {
            "timestamp": "2026-04-17T12:00:00Z",
            "level": "error",
            "message": "db exploded",
            "pod": "api-7f6b8-abc",
            "container": "api",
            "labels": {"namespace": "prod", "app": "api"},
        }
    )
    assert entry.level == LogLevel.ERROR
    assert entry.labels["namespace"] == "prod"
