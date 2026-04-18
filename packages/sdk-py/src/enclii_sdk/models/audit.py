"""Audit log models backing ``GET /v1/audit`` and ``GET /v1/activity``."""

from __future__ import annotations

from datetime import datetime
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field


class AuditLog(BaseModel):
    """A single audit event.

    Consolidated audit surface (P1.5) aggregates Janua sessions,
    Switchyard lifecycle/audit, and 4 Selva RFC ledgers via nexus-api.
    """

    model_config = ConfigDict(extra="allow")

    id: UUID
    timestamp: datetime
    actor_id: UUID | None = None
    actor_email: str = ""
    actor_role: str = ""
    action: str
    resource_type: str
    resource_id: str
    resource_name: str = ""
    project_id: UUID | None = None
    environment_id: UUID | None = None
    ip_address: str = ""
    user_agent: str = ""
    outcome: str = "success"
    context: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)


class AuditLogPage(BaseModel):
    """Paged response for ``GET /v1/audit``."""

    items: list[AuditLog] = Field(default_factory=list)
    next_cursor: str | None = None
    total: int | None = None


__all__ = ["AuditLog", "AuditLogPage"]
