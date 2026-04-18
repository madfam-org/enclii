"""Pydantic v2 models for the Enclii Platform API.

Two parallel model sets live here, by design:

* **Hand-written modules** (``core.py``, ``webhooks.py``, ``canary.py``, ``jobs.py``,
  ``logs.py``, ``secrets.py``, ``audit.py``, ``common.py``) — the consumer-facing API
  surface, tracking the Go SDK's ``pkg/types`` package. These are what
  ``from enclii_sdk import ...`` exposes and what the resource classes use.
* **Generated module** (``generated.py``) — produced from ``docs/api/openapi.yaml``
  via ``datamodel-code-generator``. Checked in as a reference for consumers
  who want to work against the raw spec shape, and as a CI drift canary.

Regenerate with ``make models`` (or ``scripts/generate_models.sh``).
CI enforces the two stay in sync with the spec via ``make verify-models``.
Outbound-webhook and canary models predate their OpenAPI entries, so the
hand-written variants remain authoritative there.
"""

from enclii_sdk.models.audit import AuditLog, AuditLogPage
from enclii_sdk.models.canary import (
    CanaryRollout,
    CanaryRolloutResponse,
    CanaryRolloutState,
    StartCanaryRequest,
)
from enclii_sdk.models.common import (
    CIRunnerMode,
    DeploymentStatus,
    HealthStatus,
    Page,
    ReleaseStatus,
)
from enclii_sdk.models.core import (
    BuildConfig,
    BuildType,
    CreateProjectRequest,
    CreateServiceRequest,
    Deployment,
    DeploymentEnriched,
    DeployRequest,
    Environment,
    EnvironmentVariable,
    Project,
    Release,
    RollbackRequest,
    Service,
    UpdateServiceRequest,
)
from enclii_sdk.models.jobs import (
    CreateCronJobRequest,
    CreateOneOffJobRequest,
    CronJob,
    CronJobRun,
)
from enclii_sdk.models.logs import LogEntry, LogLevel, LogQueryResponse
from enclii_sdk.models.secrets import (
    CreateEnvVarRequest,
    EnvVarResponse,
    UpdateEnvVarRequest,
)
from enclii_sdk.models.webhooks import (
    OutboundWebhookDelivery,
    OutboundWebhookDeliveryStatus,
    OutboundWebhookEnvelope,
    OutboundWebhookEventType,
    OutboundWebhookSubscription,
    OutboundWebhookSubscriptionCreateRequest,
    OutboundWebhookSubscriptionCreateResponse,
    OutboundWebhookSubscriptionUpdateRequest,
)

__all__ = [
    "AuditLog",
    "AuditLogPage",
    "BuildConfig",
    "BuildType",
    "CIRunnerMode",
    "CanaryRollout",
    "CanaryRolloutResponse",
    "CanaryRolloutState",
    "CreateCronJobRequest",
    "CreateEnvVarRequest",
    "CreateOneOffJobRequest",
    "CreateProjectRequest",
    "CreateServiceRequest",
    "CronJob",
    "CronJobRun",
    "DeployRequest",
    "Deployment",
    "DeploymentEnriched",
    "DeploymentStatus",
    "EnvVarResponse",
    "Environment",
    "EnvironmentVariable",
    "HealthStatus",
    "LogEntry",
    "LogLevel",
    "LogQueryResponse",
    "OutboundWebhookDelivery",
    "OutboundWebhookDeliveryStatus",
    "OutboundWebhookEnvelope",
    "OutboundWebhookEventType",
    "OutboundWebhookSubscription",
    "OutboundWebhookSubscriptionCreateRequest",
    "OutboundWebhookSubscriptionCreateResponse",
    "OutboundWebhookSubscriptionUpdateRequest",
    "Page",
    "Project",
    "Release",
    "ReleaseStatus",
    "RollbackRequest",
    "Service",
    "StartCanaryRequest",
    "UpdateEnvVarRequest",
    "UpdateServiceRequest",
]
