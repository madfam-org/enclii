"""Canary rollout models (P2.7).

Replica-proportion traffic splitting with auto-promote after a
validation window. See ``apps/switchyard-api/internal/reconciler/canary.go``
for the state machine authority.
"""

from __future__ import annotations

from datetime import datetime
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

from enclii_sdk.models.common import StrEnum


class CanaryRolloutState(StrEnum):
    """State machine for a canary rollout.

    Transitions::

        pending → running → validating → promoting → succeeded
                          ↘ auto_rolled_back
                          ↘ manual_rolled_back
            ↘ failed (from any non-terminal on reconciler error)
    """

    PENDING = "pending"
    RUNNING = "running"
    VALIDATING = "validating"
    PROMOTING = "promoting"
    SUCCEEDED = "succeeded"
    AUTO_ROLLED_BACK = "auto_rolled_back"
    MANUAL_ROLLED_BACK = "manual_rolled_back"
    FAILED = "failed"

    def is_terminal(self) -> bool:
        """Return ``True`` if the rollout is in an end state."""
        return self in (
            CanaryRolloutState.SUCCEEDED,
            CanaryRolloutState.AUTO_ROLLED_BACK,
            CanaryRolloutState.MANUAL_ROLLED_BACK,
            CanaryRolloutState.FAILED,
        )

    def is_active(self) -> bool:
        """Return ``True`` if the rollout is still in flight."""
        return not self.is_terminal()


class StartCanaryRequest(BaseModel):
    """Payload for ``POST /v1/services/{id}/canary``.

    Validation bounds (enforced server-side by the reconciler):
      - ``percentage``: 5-50
      - ``validation_window_minutes``: 1-60
      - ``error_rate_threshold``: 0.0-0.5 (default 0.05)
    """

    digest: str = Field(description="Image digest or Release UUID of the candidate")
    percentage: int = Field(ge=5, le=50)
    validation_window_minutes: int = Field(default=10, ge=1, le=60)
    smoke_endpoint: str = ""
    error_rate_threshold: float = Field(default=0.05, ge=0.0, le=0.5)
    environment_name: str = "production"
    change_ticket_url: str = ""
    total_replicas: int = 0


class CanaryRollout(BaseModel):
    """A single in-flight or historical canary rollout."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    service_id: UUID
    environment_id: UUID
    stable_deployment_id: UUID
    canary_deployment_id: UUID
    new_stable_deployment_id: UUID | None = None
    canary_digest: str
    canary_percentage: int
    total_replicas: int
    canary_replicas: int
    stable_replicas: int
    validation_window_seconds: int
    smoke_endpoint: str = ""
    error_rate_threshold: float = 0.05
    state: CanaryRolloutState
    started_at: datetime | None = None
    validating_started_at: datetime | None = None
    promoting_started_at: datetime | None = None
    terminal_at: datetime | None = None
    initiated_by: UUID | None = None
    change_ticket_url: str = ""
    last_error: str = ""
    rollback_reason: str = ""
    created_at: datetime
    updated_at: datetime


class CanaryRolloutResponse(CanaryRollout):
    """Canary rollout with effective traffic share after replica rounding."""

    actual_percentage: float


__all__ = [
    "CanaryRollout",
    "CanaryRolloutResponse",
    "CanaryRolloutState",
    "StartCanaryRequest",
]
