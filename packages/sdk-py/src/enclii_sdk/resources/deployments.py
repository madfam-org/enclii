"""Deployment queries (``/v1/deployments`` + ``/v1/services/{id}/deployments``)."""

from __future__ import annotations

import asyncio
from datetime import UTC, datetime

from enclii_sdk.errors import EncliiError
from enclii_sdk.models.common import DeploymentStatus
from enclii_sdk.models.core import Deployment, DeploymentEnriched
from enclii_sdk.resources._base import Resource


class DeploymentsResource(Resource):
    """Read-only deployment access.

    Mutation lives on :class:`enclii_sdk.resources.services.ServicesResource`
    (``deploy``) and :class:`enclii_sdk.resources.rollback.RollbackResource`.
    """

    async def list(self, service_id: str) -> list[Deployment]:
        """List deployments for a service (newest first).

        API: ``GET /v1/services/{id}/deployments``.
        """
        data = await self._client.get(f"/v1/services/{service_id}/deployments")
        return [Deployment.model_validate(d) for d in data.get("deployments", [])]

    async def list_all(self) -> list[DeploymentEnriched]:
        """List all deployments across services the caller can see.

        API: ``GET /v1/deployments``.
        """
        data = await self._client.get("/v1/deployments")
        return [DeploymentEnriched.model_validate(d) for d in data.get("deployments", [])]

    async def latest(self, service_id: str) -> Deployment:
        """Fetch the most recent deployment for a service.

        API: ``GET /v1/services/{id}/deployments/latest``.
        """
        data = await self._client.get(f"/v1/services/{service_id}/deployments/latest")
        return Deployment.model_validate(data)

    async def get(
        self,
        service_id_or_deployment_id: str,
        version: str | int | None = None,
    ) -> Deployment:
        """Fetch a deployment by id, or by service id + Heroku-style v-number.

        API:
            - ``GET /v1/deployments/{id}`` (when ``version`` is None)
            - ``GET /v1/services/{id}/versions/{v}`` (when ``version`` is set)

        The ``version`` argument accepts either a bare integer (``42``)
        or the prefixed form (``"v42"``) — the server normalizes.
        """
        if version is None:
            data = await self._client.get(f"/v1/deployments/{service_id_or_deployment_id}")
        else:
            data = await self._client.get(
                f"/v1/services/{service_id_or_deployment_id}/versions/{version}"
            )
        return Deployment.model_validate(data)

    async def wait_for_running(
        self,
        deployment_id: str,
        *,
        timeout: float = 600.0,
        poll_interval: float = 5.0,
    ) -> Deployment:
        """Poll until a deployment reaches a terminal state.

        Returns when the deployment is ``running``, ``failed``, or
        ``cancelled``. Raises :class:`TimeoutError` on timeout and
        :class:`EncliiError` on ``failed``/``cancelled`` so callers don't
        need to re-check the status.
        """
        deadline = asyncio.get_event_loop().time() + timeout
        while True:
            dep = await self.get(deployment_id)
            if dep.status == DeploymentStatus.RUNNING:
                return dep
            if dep.status in (DeploymentStatus.FAILED, DeploymentStatus.CANCELLED):
                raise EncliiError(
                    f"deployment {deployment_id} reached terminal state "
                    f"{dep.status}: {dep.error_message or 'no error message'}",
                )
            if asyncio.get_event_loop().time() >= deadline:
                raise TimeoutError(
                    f"deployment {deployment_id} did not reach RUNNING in {timeout}s "
                    f"(last status={dep.status}, checked at {datetime.now(UTC).isoformat()})"
                )
            await asyncio.sleep(poll_interval)
