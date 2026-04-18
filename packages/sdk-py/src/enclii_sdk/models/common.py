"""Shared primitives: enums and pagination types."""

from __future__ import annotations

from enum import StrEnum as _StdStrEnum
from typing import Generic, TypeVar

from pydantic import BaseModel, ConfigDict, Field

T = TypeVar("T")


class StrEnum(_StdStrEnum):
    """String enum base re-exported as the SDK's canonical type.

    All API enums in :mod:`enclii_sdk.models` inherit from this so we
    can extend with SDK-wide helpers (e.g. future classifiers) without
    breaking callers. Behaviorally identical to :class:`enum.StrEnum`
    from Python 3.11+.
    """


class HealthStatus(StrEnum):
    """Health reconciliation state for a service/deployment."""

    UNKNOWN = "unknown"
    HEALTHY = "healthy"
    UNHEALTHY = "unhealthy"


class DeploymentStatus(StrEnum):
    """Deployment reconciliation state.

    Transitions: ``pending → deploying → running`` on success;
    ``failed`` or ``cancelled`` on error.
    """

    PENDING = "pending"
    DEPLOYING = "deploying"
    RUNNING = "running"
    FAILED = "failed"
    CANCELLED = "cancelled"


class ReleaseStatus(StrEnum):
    """Build/release state for a container image."""

    BUILDING = "building"
    READY = "ready"
    FAILED = "failed"


class CIRunnerMode(StrEnum):
    """Which GitHub Actions runner pool a project uses."""

    GITHUB = "github"
    SELF_HOSTED = "self-hosted"


class Page(BaseModel, Generic[T]):
    """Generic pagination envelope used across list endpoints.

    The Enclii API currently returns lists under a resource-named key
    (``projects``, ``services``, …) rather than a generic wrapper. This
    model is the SDK's normalized representation; resource methods
    translate the server response into a :class:`Page`.
    """

    model_config = ConfigDict(arbitrary_types_allowed=True)

    items: list[T] = Field(default_factory=list)
    next_cursor: str | None = None
    total: int | None = None

    def has_next(self) -> bool:
        """Return ``True`` if there is a cursor for the next page."""
        return self.next_cursor is not None


__all__ = [
    "CIRunnerMode",
    "DeploymentStatus",
    "HealthStatus",
    "Page",
    "ReleaseStatus",
    "StrEnum",
]
