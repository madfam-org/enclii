"""Canary rollout operations (P2.7)."""

from __future__ import annotations

from enclii_sdk.models.canary import (
    CanaryRollout,
    CanaryRolloutResponse,
    StartCanaryRequest,
)
from enclii_sdk.resources._base import Resource


class CanaryResource(Resource):
    """Canary rollout lifecycle management.

    Rollouts are started asynchronously — :meth:`start` returns as soon
    as the rollout record is persisted. Callers should :meth:`get` or
    :meth:`list` to observe state transitions.
    """

    async def start(
        self,
        service_id: str,
        *,
        digest: str,
        percentage: int,
        validation_window_minutes: int = 10,
        smoke_endpoint: str = "",
        error_rate_threshold: float = 0.05,
        environment_name: str = "production",
        change_ticket_url: str = "",
        total_replicas: int = 0,
    ) -> CanaryRolloutResponse:
        """Start a canary rollout.

        API: ``POST /v1/services/{id}/canary``. Requires ``developer``.

        Args:
            digest: Image digest or Release UUID of the candidate (the
                handler disambiguates).
            percentage: Traffic share for the canary (5-50).
            validation_window_minutes: How long the canary must stay
                healthy before auto-promotion triggers (1-60).
            smoke_endpoint: Optional path on the canary Service that must
                return 200 during the validation window.
            error_rate_threshold: Max fraction of 5xx responses tolerated
                during validation (default 0.05, max 0.5).
            environment_name: Target environment (default ``"production"``).
            change_ticket_url: Required for production environments.
            total_replicas: Override for the current service replica count.

        Returns the persisted rollout with its effective traffic share.

        Raises:
            ConflictError: If the service already has an active rollout.
            PermissionError: If ``change_ticket_url`` is missing for prod.
        """
        request = StartCanaryRequest(
            digest=digest,
            percentage=percentage,
            validation_window_minutes=validation_window_minutes,
            smoke_endpoint=smoke_endpoint,
            error_rate_threshold=error_rate_threshold,
            environment_name=environment_name,
            change_ticket_url=change_ticket_url,
            total_replicas=total_replicas,
        )
        data = await self._client.post(
            f"/v1/services/{service_id}/canary",
            json_body=request.model_dump(mode="json"),
        )
        return CanaryRolloutResponse.model_validate(data)

    async def list(self, service_id: str) -> list[CanaryRollout]:
        """List all canary rollouts for a service (newest first).

        API: ``GET /v1/services/{id}/canary``.
        """
        data = await self._client.get(f"/v1/services/{service_id}/canary")
        # Server returns either a list directly or a {"rollouts": [...]}
        # envelope depending on build — handle both.
        rollouts = data if isinstance(data, list) else data.get("rollouts", [])
        return [CanaryRollout.model_validate(r) for r in rollouts]

    async def get(self, service_id: str, rollout_id: str) -> CanaryRolloutResponse:
        """Fetch a rollout's current state.

        API: ``GET /v1/services/{id}/canary/{rollout_id}``.
        """
        data = await self._client.get(f"/v1/services/{service_id}/canary/{rollout_id}")
        return CanaryRolloutResponse.model_validate(data)

    async def promote(self, service_id: str, rollout_id: str) -> CanaryRollout:
        """Manually promote a canary before the validation window closes.

        API: ``POST /v1/services/{id}/canary/{rollout_id}/promote``.
        Requires ``developer``.
        """
        data = await self._client.post(f"/v1/services/{service_id}/canary/{rollout_id}/promote")
        return CanaryRollout.model_validate(data)

    async def rollback(
        self,
        service_id: str,
        rollout_id: str,
        *,
        reason: str = "",
    ) -> CanaryRollout:
        """Manually roll back a canary.

        API: ``POST /v1/services/{id}/canary/{rollout_id}/rollback``.
        Requires ``developer``.
        """
        data = await self._client.post(
            f"/v1/services/{service_id}/canary/{rollout_id}/rollback",
            json_body={"reason": reason} if reason else None,
        )
        return CanaryRollout.model_validate(data)
