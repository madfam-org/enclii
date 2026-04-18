"""Log query + WebSocket tail."""

from __future__ import annotations

import asyncio
import json
from collections.abc import AsyncIterator
from datetime import datetime
from typing import Any

import httpx
import websockets
from websockets.exceptions import ConnectionClosed, InvalidStatus

from enclii_sdk.errors import AuthError, EncliiError, NetworkError, NotFoundError
from enclii_sdk.models.logs import LogEntry, LogLevel, LogQueryResponse
from enclii_sdk.resources._base import Resource


class LogsResource(Resource):
    """Log query and streaming.

    The Enclii API exposes two complementary surfaces:

    * REST ``GET /v1/services/{id}/logs`` — windowed, cursor-paginated
      query backed by Loki (P2.1).
    * WebSocket ``GET /v1/services/{id}/logs/tail`` — live tail.

    The SDK prefers the P2.1 surfaces when available and falls back to
    the legacy stream-by-deployment endpoints if they are.
    """

    async def query(
        self,
        service_id: str,
        *,
        since: str | datetime | None = None,
        until: str | datetime | None = None,
        level: LogLevel | list[LogLevel] | None = None,
        search: str | None = None,
        limit: int = 500,
        cursor: str | None = None,
    ) -> LogQueryResponse:
        """Query historical logs.

        API: ``GET /v1/services/{id}/logs``.

        Args:
            service_id: Service UUID.
            since: RFC3339 timestamp or duration string (``"1h"``, ``"24h"``).
                Defaults to 1 hour ago on the server.
            until: RFC3339 timestamp. Defaults to ``now``.
            level: One or more levels to include. ``None`` = all.
            search: Substring match applied server-side.
            limit: 1 to 2000, default 500.
            cursor: Opaque value from a previous response to page forward.

        Returns:
            :class:`LogQueryResponse` with entries and ``next_cursor``.
        """
        params: dict[str, Any] = {"limit": limit}
        if since is not None:
            params["since"] = _serialize_time(since)
        if until is not None:
            params["until"] = _serialize_time(until)
        if level is not None:
            params["level"] = (
                [level.value] if isinstance(level, LogLevel) else [lvl.value for lvl in level]
            )
        if search:
            params["search"] = search
        if cursor:
            params["cursor"] = cursor

        data = await self._client.get(f"/v1/services/{service_id}/logs", params=params)
        return LogQueryResponse.model_validate(data)

    async def tail(
        self,
        service_id: str,
        *,
        level: LogLevel | list[LogLevel] | None = None,
        search: str | None = None,
        reconnect: bool = True,
        max_reconnect_attempts: int = 5,
    ) -> AsyncIterator[LogEntry]:
        """Stream logs live via WebSocket.

        API: ``GET /v1/services/{id}/logs/tail``.

        This is an async generator — iterate with ``async for`` directly::

            async for entry in enclii.logs.tail("svc_123", level=LogLevel.ERROR):
                print(entry.timestamp, entry.message)

        Yields :class:`LogEntry` objects as they arrive. Frames with
        ``type != "entry"`` (keepalives, dropped-counters) are silently
        handled — callers only see actual log entries.

        If ``reconnect`` is ``True`` (default) and the connection is
        closed unexpectedly, the iterator transparently reconnects up
        to ``max_reconnect_attempts`` times with exponential backoff.
        """
        ws_url = self._build_ws_url(
            f"/v1/services/{service_id}/logs/tail",
            level=level,
            search=search,
        )
        token = await self._client._get_token()
        headers = [("Authorization", f"Bearer {token}")]

        attempts = 0
        while True:
            try:
                async with websockets.connect(
                    ws_url,
                    additional_headers=headers,
                    ping_interval=20,
                    ping_timeout=20,
                ) as ws:
                    attempts = 0  # reset on successful connect
                    async for message in ws:
                        frame = _decode_frame(message)
                        if frame is None:
                            continue
                        ftype = frame.get("type", "entry")
                        if ftype == "entry" and "entry" in frame:
                            yield LogEntry.model_validate(frame["entry"])
                        elif ftype == "error":
                            raise EncliiError(f"server-side tail error: {frame.get('error')}")
                        # "dropped", "ping", "bye" frames are logged-only.
            except InvalidStatus as exc:
                status = exc.response.status_code
                if status == 401:
                    raise AuthError("WebSocket authentication failed", status_code=401) from exc
                if status == 404:
                    raise NotFoundError(f"service {service_id} not found", status_code=404) from exc
                raise NetworkError(f"WebSocket handshake failed (status={status})") from exc
            except (ConnectionClosed, OSError) as exc:
                if not reconnect:
                    raise NetworkError(f"log tail disconnected: {exc}") from exc
                attempts += 1
                if attempts > max_reconnect_attempts:
                    raise NetworkError(
                        f"log tail disconnected after {attempts} attempts: {exc}"
                    ) from exc
                # Exponential backoff capped at 30s.
                await asyncio.sleep(min(2**attempts, 30))
                continue

    # ------------------------------------------------------------------
    # Helpers
    # ------------------------------------------------------------------

    def _build_ws_url(
        self,
        path: str,
        *,
        level: LogLevel | list[LogLevel] | None,
        search: str | None,
    ) -> str:
        """Derive the ws(s):// URL from the configured base_url."""
        base = self._client.base_url
        if base.startswith("https://"):
            ws_base = "wss://" + base[len("https://") :]
        elif base.startswith("http://"):
            ws_base = "ws://" + base[len("http://") :]
        else:
            ws_base = base
        params = httpx.QueryParams()
        if level is not None:
            levels = [level] if isinstance(level, LogLevel) else list(level)
            for lvl in levels:
                params = params.add("level", lvl.value)
        if search:
            params = params.add("search", search)
        query = str(params)
        return f"{ws_base}{path}" + (f"?{query}" if query else "")


def _decode_frame(message: str | bytes) -> dict[str, Any] | None:
    """Decode a WS frame; return ``None`` if it is not a JSON object."""
    if isinstance(message, bytes):
        try:
            message = message.decode("utf-8")
        except UnicodeDecodeError:
            return None
    try:
        obj = json.loads(message)
    except json.JSONDecodeError:
        return None
    return obj if isinstance(obj, dict) else None


def _serialize_time(value: str | datetime) -> str:
    """Normalize a time argument to a string for the query string."""
    if isinstance(value, datetime):
        return value.isoformat()
    return value
