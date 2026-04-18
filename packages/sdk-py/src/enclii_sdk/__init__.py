"""Enclii Platform Python SDK.

Top-level exports for the most common use cases::

    from enclii_sdk import AsyncEncliiClient, EncliiClient

    async with AsyncEncliiClient(token="enclii_...") as enclii:
        projects = await enclii.projects.list()

For webhook signature verification::

    from enclii_sdk.webhook_verify import verify
"""

from __future__ import annotations

from enclii_sdk.client import (
    DEFAULT_BASE_URL,
    DEFAULT_MAX_RETRIES,
    DEFAULT_TIMEOUT,
    AsyncEncliiClient,
    EncliiClient,
    __version__,
)
from enclii_sdk.errors import (
    AuthError,
    ConflictError,
    EncliiError,
    NetworkError,
    NotFoundError,
    PermissionError,
    RateLimitError,
    ServerError,
    ValidationError,
    WebhookSignatureError,
)
from enclii_sdk.models import (
    AuditLog,
    CanaryRollout,
    CanaryRolloutResponse,
    CanaryRolloutState,
    Deployment,
    DeploymentEnriched,
    DeploymentStatus,
    Environment,
    HealthStatus,
    LogEntry,
    LogLevel,
    OutboundWebhookDelivery,
    OutboundWebhookEventType,
    OutboundWebhookSubscription,
    OutboundWebhookSubscriptionCreateResponse,
    Project,
    Release,
    ReleaseStatus,
    Service,
)

__all__ = [
    "DEFAULT_BASE_URL",
    "DEFAULT_MAX_RETRIES",
    "DEFAULT_TIMEOUT",
    "AsyncEncliiClient",
    "AuditLog",
    "AuthError",
    "CanaryRollout",
    "CanaryRolloutResponse",
    "CanaryRolloutState",
    "ConflictError",
    "Deployment",
    "DeploymentEnriched",
    "DeploymentStatus",
    "EncliiClient",
    "EncliiError",
    "Environment",
    "HealthStatus",
    "LogEntry",
    "LogLevel",
    "NetworkError",
    "NotFoundError",
    "OutboundWebhookDelivery",
    "OutboundWebhookEventType",
    "OutboundWebhookSubscription",
    "OutboundWebhookSubscriptionCreateResponse",
    "PermissionError",
    "Project",
    "RateLimitError",
    "Release",
    "ReleaseStatus",
    "ServerError",
    "Service",
    "ValidationError",
    "WebhookSignatureError",
    "__version__",
]
