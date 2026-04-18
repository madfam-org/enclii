"""Outbound lifecycle webhook subscriptions (P2.3).

Distinct from the notification-webhook Slack/Discord integrations —
these are HMAC-signed, at-least-once delivered with retries + DLQ.
"""

from __future__ import annotations

from enclii_sdk.models.webhooks import (
    OutboundWebhookDelivery,
    OutboundWebhookEventType,
    OutboundWebhookSubscription,
    OutboundWebhookSubscriptionCreateRequest,
    OutboundWebhookSubscriptionCreateResponse,
    OutboundWebhookSubscriptionUpdateRequest,
)
from enclii_sdk.resources._base import Resource


class WebhooksResource(Resource):
    """CRUD for outbound webhook subscriptions + delivery management."""

    async def list_event_types(self) -> list[str]:
        """List subscribable event types.

        API: ``GET /v1/lifecycle-webhooks/event-types``.
        """
        data = await self._client.get("/v1/lifecycle-webhooks/event-types")
        return data if isinstance(data, list) else list(data.get("event_types", []))

    async def list(self, project_slug: str) -> list[OutboundWebhookSubscription]:
        """List subscriptions for a project.

        API: ``GET /v1/projects/{slug}/lifecycle-webhooks``.
        """
        data = await self._client.get(f"/v1/projects/{project_slug}/lifecycle-webhooks")
        items = data if isinstance(data, list) else data.get("subscriptions", [])
        return [OutboundWebhookSubscription.model_validate(s) for s in items]

    async def create(
        self,
        project_slug: str,
        *,
        name: str,
        url: str,
        events: list[str] | list[OutboundWebhookEventType],
    ) -> OutboundWebhookSubscriptionCreateResponse:
        """Create a webhook subscription.

        The response includes a plaintext ``signing_secret`` that is
        returned **exactly once** — persist it immediately.

        API: ``POST /v1/projects/{slug}/lifecycle-webhooks``. Requires
        ``developer``.
        """
        event_types = [
            e if isinstance(e, OutboundWebhookEventType) else OutboundWebhookEventType(e)
            for e in events
        ]
        request = OutboundWebhookSubscriptionCreateRequest(
            name=name,
            url=url,
            event_types=event_types,
        )
        data = await self._client.post(
            f"/v1/projects/{project_slug}/lifecycle-webhooks",
            json_body=request.model_dump(mode="json"),
        )
        return OutboundWebhookSubscriptionCreateResponse.model_validate(data)

    async def get(self, subscription_id: str) -> OutboundWebhookSubscription:
        """Fetch a subscription. API: ``GET /v1/lifecycle-webhooks/{sub_id}``."""
        data = await self._client.get(f"/v1/lifecycle-webhooks/{subscription_id}")
        return OutboundWebhookSubscription.model_validate(data)

    async def update(
        self,
        subscription_id: str,
        *,
        name: str | None = None,
        url: str | None = None,
        events: list[str] | list[OutboundWebhookEventType] | None = None,
        active: bool | None = None,
    ) -> OutboundWebhookSubscription:
        """Partially update a subscription. Only non-None fields apply.

        API: ``PATCH /v1/lifecycle-webhooks/{sub_id}``. Requires ``developer``.
        """
        event_types: list[OutboundWebhookEventType] | None = None
        if events is not None:
            event_types = [
                e if isinstance(e, OutboundWebhookEventType) else OutboundWebhookEventType(e)
                for e in events
            ]
        request = OutboundWebhookSubscriptionUpdateRequest(
            name=name,
            url=url,
            event_types=event_types,
            active=active,
        )
        data = await self._client.patch(
            f"/v1/lifecycle-webhooks/{subscription_id}",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return OutboundWebhookSubscription.model_validate(data)

    async def delete(self, subscription_id: str) -> None:
        """Delete a subscription.

        API: ``DELETE /v1/lifecycle-webhooks/{sub_id}``. Requires ``admin``.
        """
        await self._client.delete(f"/v1/lifecycle-webhooks/{subscription_id}")

    async def rotate_secret(
        self, subscription_id: str
    ) -> OutboundWebhookSubscriptionCreateResponse:
        """Rotate a subscription's signing secret.

        The new plaintext secret is returned exactly once. Immediately
        propagate it to the subscriber's verification logic.

        API: ``POST /v1/lifecycle-webhooks/{sub_id}/rotate-secret``.
        Requires ``developer``.
        """
        data = await self._client.post(f"/v1/lifecycle-webhooks/{subscription_id}/rotate-secret")
        return OutboundWebhookSubscriptionCreateResponse.model_validate(data)

    async def test(self, subscription_id: str) -> OutboundWebhookDelivery:
        """Send a ``test.ping`` to the subscription.

        API: ``POST /v1/lifecycle-webhooks/{sub_id}/test``. Requires
        ``developer``.
        """
        data = await self._client.post(f"/v1/lifecycle-webhooks/{subscription_id}/test")
        # Handlers return `{"delivery": {...}}` envelope.
        if isinstance(data, dict) and "delivery" in data:
            return OutboundWebhookDelivery.model_validate(data["delivery"])
        return OutboundWebhookDelivery.model_validate(data)

    async def list_deliveries(
        self,
        subscription_id: str,
        *,
        limit: int = 50,
        cursor: str | None = None,
    ) -> list[OutboundWebhookDelivery]:
        """List recent delivery attempts.

        API: ``GET /v1/lifecycle-webhooks/{sub_id}/deliveries``.
        """
        params: dict[str, int | str] = {"limit": limit}
        if cursor:
            params["cursor"] = cursor
        data = await self._client.get(
            f"/v1/lifecycle-webhooks/{subscription_id}/deliveries",
            params=params,
        )
        items = data if isinstance(data, list) else data.get("deliveries", [])
        return [OutboundWebhookDelivery.model_validate(d) for d in items]

    async def redeliver(self, subscription_id: str, delivery_id: str) -> OutboundWebhookDelivery:
        """Re-enqueue a historical delivery. Useful for DLQ recovery.

        API: ``POST /v1/lifecycle-webhooks/{sub_id}/deliveries/{delivery_id}/redeliver``.
        Requires ``developer``.
        """
        data = await self._client.post(
            f"/v1/lifecycle-webhooks/{subscription_id}/deliveries/{delivery_id}/redeliver"
        )
        if isinstance(data, dict) and "delivery" in data:
            return OutboundWebhookDelivery.model_validate(data["delivery"])
        return OutboundWebhookDelivery.model_validate(data)
