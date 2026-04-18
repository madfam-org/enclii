"""Audit log queries."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from enclii_sdk.models.audit import AuditLog, AuditLogPage
from enclii_sdk.resources._base import Resource


class AuditResource(Resource):
    """Audit log access.

    Two surfaces:

    * ``/v1/activity`` — legacy, Switchyard-only.
    * ``/v1/audit`` — consolidated (P1.5) surface aggregating Janua,
      Switchyard, and Selva ledgers via nexus-api.

    The SDK exposes both; most callers should prefer ``list`` (the
    consolidated surface).
    """

    async def list(
        self,
        *,
        since: datetime | str | None = None,
        until: datetime | str | None = None,
        actor: str | None = None,
        resource_type: str | None = None,
        action: str | None = None,
        project_id: str | None = None,
        limit: int = 100,
        cursor: str | None = None,
    ) -> AuditLogPage:
        """Query the consolidated audit log.

        API: ``GET /v1/audit``.
        """
        params: dict[str, Any] = {"limit": limit}
        if since is not None:
            params["since"] = since.isoformat() if isinstance(since, datetime) else since
        if until is not None:
            params["until"] = until.isoformat() if isinstance(until, datetime) else until
        if actor:
            params["actor"] = actor
        if resource_type:
            params["resource_type"] = resource_type
        if action:
            params["action"] = action
        if project_id:
            params["project_id"] = project_id
        if cursor:
            params["cursor"] = cursor

        data = await self._client.get("/v1/audit", params=params)
        items_raw = (
            data if isinstance(data, list) else data.get("items") or data.get("logs", [])
        )
        next_cursor = data.get("next_cursor") if isinstance(data, dict) else None
        total = data.get("total") if isinstance(data, dict) else None
        return AuditLogPage(
            items=[AuditLog.model_validate(it) for it in items_raw],
            next_cursor=next_cursor,
            total=total,
        )

    async def legacy_activity(
        self,
        *,
        limit: int = 100,
    ) -> list[AuditLog]:
        """Legacy single-source activity feed.

        API: ``GET /v1/activity``. Prefer :meth:`list` for new code.
        """
        data = await self._client.get("/v1/activity", params={"limit": limit})
        items = (
            data if isinstance(data, list) else data.get("items") or data.get("activity", [])
        )
        return [AuditLog.model_validate(it) for it in items]
