"""Outbound lifecycle webhook models (P2.3).

Customer-configured HTTPS endpoints that receive HMAC-signed JSON payloads
on deploy / rollback / scale / secret.rotated events. Mirrors
``types/outbound_webhooks.go`` in the Go SDK verbatim.
"""

from __future__ import annotations

from datetime import datetime
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, HttpUrl

from enclii_sdk.models.common import StrEnum

# ---------------------------------------------------------------------------
# Constants mirroring the Go SDK
# ---------------------------------------------------------------------------

OUTBOUND_WEBHOOK_API_VERSION = "2026-04-01"
"""Webhook envelope API version. Bumped only on breaking payload changes."""

OUTBOUND_WEBHOOK_MAX_ATTEMPTS = 5
"""Delivery retry ceiling before a delivery transitions to ``dlq``."""

OUTBOUND_WEBHOOK_MAX_PAYLOAD_BYTES = 64 * 1024
"""Envelope size cap (64 KiB). Larger payloads are rejected at enqueue."""

OUTBOUND_WEBHOOK_SIGNATURE_TOLERANCE_SECONDS = 300
"""Default maximum clock skew (seconds) for signature verification."""

OUTBOUND_WEBHOOK_SIGNATURE_HEADER = "X-Enclii-Signature"
"""HTTP header carrying the ``t=<ts>,v1=<hex>`` tuple."""

OUTBOUND_WEBHOOK_EVENT_HEADER = "X-Enclii-Event"
"""HTTP header surfacing the event type for subscriber-side routing."""

OUTBOUND_WEBHOOK_DELIVERY_ID_HEADER = "X-Enclii-Delivery-Id"
"""HTTP header providing an idempotency key for deduplicating retries."""


class OutboundWebhookEventType(StrEnum):
    """Subscribable lifecycle events.

    ``test.ping`` is control-plane-only and emitted exclusively by the
    ``enclii webhooks test`` CLI command. It cannot be subscribed to.
    """

    DEPLOY_STARTED = "deploy.started"
    DEPLOY_SUCCEEDED = "deploy.succeeded"
    DEPLOY_FAILED = "deploy.failed"
    ROLLBACK_SUCCEEDED = "rollback.succeeded"
    SECRET_ROTATED = "secret.rotated"
    SERVICE_SCALED = "service.scaled"


class OutboundWebhookDeliveryStatus(StrEnum):
    """State of a single delivery attempt row."""

    PENDING = "pending"
    DELIVERING = "delivering"
    DELIVERED = "delivered"
    FAILED = "failed"
    DLQ = "dlq"


class OutboundWebhookSubscription(BaseModel):
    """A customer-configured webhook endpoint.

    The raw signing secret is never returned after creation/rotation —
    it is shown exactly once in :class:`OutboundWebhookSubscriptionCreateResponse`.
    """

    model_config = ConfigDict(extra="allow")

    id: UUID
    project_id: UUID
    name: str
    url: str
    secret_sha256_prefix: str
    event_types: list[OutboundWebhookEventType]
    active: bool = True
    created_by: str = ""
    created_at: datetime
    updated_at: datetime
    last_success_at: datetime | None = None
    last_failure_at: datetime | None = None
    consecutive_failures: int = 0
    auto_disabled_at: datetime | None = None


class OutboundWebhookSubscriptionCreateRequest(BaseModel):
    """Payload for ``POST /v1/projects/{slug}/lifecycle-webhooks``."""

    name: str = Field(min_length=1)
    url: HttpUrl
    event_types: list[OutboundWebhookEventType] = Field(default_factory=list)


class OutboundWebhookSubscriptionUpdateRequest(BaseModel):
    """Payload for ``PATCH /v1/lifecycle-webhooks/{sub_id}``.

    Only non-None fields are applied — allows toggling ``active`` without
    clobbering url/events.
    """

    name: str | None = None
    url: HttpUrl | None = None
    event_types: list[OutboundWebhookEventType] | None = None
    active: bool | None = None


class OutboundWebhookSubscriptionCreateResponse(BaseModel):
    """Response from create / rotate-secret endpoints.

    ``signing_secret`` is plaintext and must be persisted by the caller —
    the server will never return it again.
    """

    subscription: OutboundWebhookSubscription
    signing_secret: str
    note: str = ""


class OutboundWebhookDelivery(BaseModel):
    """A single delivery attempt row.

    Retries produce additional rows with the same ``event_id`` but
    incremented ``attempt_number``.
    """

    model_config = ConfigDict(extra="allow")

    id: UUID
    subscription_id: UUID
    lifecycle_event_id: UUID | None = None
    event_id: str
    event_type: OutboundWebhookEventType
    payload: dict[str, Any] | None = None
    payload_sha256: str
    attempt_number: int
    status: OutboundWebhookDeliveryStatus
    http_status: int | None = None
    response_snippet: str = ""
    error_message: str = ""
    attempted_at: datetime | None = None
    delivered_at: datetime | None = None
    duration_ms: int | None = None
    next_retry_at: datetime | None = None
    created_at: datetime


class OutboundWebhookEnvelope(BaseModel):
    """The canonical JSON body posted to subscribers.

    All events share this shape; event-specific fields live under ``data``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    type: OutboundWebhookEventType
    created_at: datetime
    api_version: str = OUTBOUND_WEBHOOK_API_VERSION
    data: dict[str, Any] = Field(default_factory=dict)


__all__ = [
    "OUTBOUND_WEBHOOK_API_VERSION",
    "OUTBOUND_WEBHOOK_DELIVERY_ID_HEADER",
    "OUTBOUND_WEBHOOK_EVENT_HEADER",
    "OUTBOUND_WEBHOOK_MAX_ATTEMPTS",
    "OUTBOUND_WEBHOOK_MAX_PAYLOAD_BYTES",
    "OUTBOUND_WEBHOOK_SIGNATURE_HEADER",
    "OUTBOUND_WEBHOOK_SIGNATURE_TOLERANCE_SECONDS",
    "OutboundWebhookDelivery",
    "OutboundWebhookDeliveryStatus",
    "OutboundWebhookEnvelope",
    "OutboundWebhookEventType",
    "OutboundWebhookSubscription",
    "OutboundWebhookSubscriptionCreateRequest",
    "OutboundWebhookSubscriptionCreateResponse",
    "OutboundWebhookSubscriptionUpdateRequest",
]
