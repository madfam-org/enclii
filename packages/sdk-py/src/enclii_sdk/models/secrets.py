"""Secret/env-var request models.

Separate from :mod:`enclii_sdk.models.core` because the create/update
payloads have secret-aware fields (``is_secret``, ``value``) that do not
round-trip through the read response shape.
"""

from __future__ import annotations

from datetime import datetime
from uuid import UUID

from pydantic import BaseModel, ConfigDict


class CreateEnvVarRequest(BaseModel):
    """Payload for ``POST /v1/services/{id}/env-vars``.

    Setting ``is_secret=True`` masks the value in subsequent responses
    until an explicit ``reveal`` request is made.
    """

    key: str
    value: str
    is_secret: bool = False
    environment_id: UUID | None = None


class UpdateEnvVarRequest(BaseModel):
    """Payload for ``PUT /v1/services/{id}/env-vars/{var_id}``."""

    value: str | None = None
    is_secret: bool | None = None


class EnvVarResponse(BaseModel):
    """Read-side view of an env var. Secrets are masked by the server."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    service_id: UUID
    environment_id: UUID | None = None
    key: str
    value: str  # Masked as bullets when is_secret=True.
    is_secret: bool
    created_at: datetime
    updated_at: datetime


__all__ = ["CreateEnvVarRequest", "EnvVarResponse", "UpdateEnvVarRequest"]
