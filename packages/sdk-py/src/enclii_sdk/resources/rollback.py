"""Rollback operations.

Two endpoints exist in switchyard-api:

* ``POST /v1/deployments/{id}/rollback`` — legacy, ArgoCD-backed path.
* ``POST /v1/services/{id}/rollback`` — instant rollback (P0.5), traffic
  flips via Service-selector in <30s.

The SDK exposes both under :class:`RollbackResource` so callers pick
intent explicitly.
"""

from __future__ import annotations

from enclii_sdk.models.core import Deployment, RollbackRequest
from enclii_sdk.resources._base import Resource


class RollbackResource(Resource):
    """Rollback-specific operations."""

    async def rollback_deployment(
        self,
        deployment_id: str,
        *,
        to_release: str | None = None,
        change_ticket_url: str | None = None,
    ) -> None:
        """Roll back a specific deployment.

        API: ``POST /v1/deployments/{id}/rollback``. Requires ``developer``.
        If ``to_release`` is not supplied the previous release is chosen.
        """
        request = RollbackRequest(
            to_release=to_release, change_ticket_url=change_ticket_url
        )
        await self._client.post(
            f"/v1/deployments/{deployment_id}/rollback",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )

    async def instant_rollback(
        self,
        service_id: str,
        *,
        to_release: str | None = None,
        change_ticket_url: str | None = None,
    ) -> Deployment:
        """Instantly flip traffic back to a previous release (P0.5).

        API: ``POST /v1/services/{id}/rollback``. Requires ``developer``.
        Production environments require ``change_ticket_url``.

        Returns the deployment that is now live.
        """
        request = RollbackRequest(
            to_release=to_release, change_ticket_url=change_ticket_url
        )
        data = await self._client.post(
            f"/v1/services/{service_id}/rollback",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return Deployment.model_validate(data)
