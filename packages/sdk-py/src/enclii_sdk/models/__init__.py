"""Pydantic v2 models for the Enclii Platform API.

Models are hand-maintained to track the Go SDK's ``pkg/types`` package
and the OpenAPI spec at ``docs/api/openapi.yaml``. Regeneration from
OpenAPI is supported via ``scripts/generate_models.py`` for sections of
the spec that are complete — canary rollouts and outbound webhooks are
currently hand-written because they predate their OpenAPI entries.
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
