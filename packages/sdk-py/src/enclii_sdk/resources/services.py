"""Service CRUD, build, and deploy."""

from __future__ import annotations

from typing import Any

from enclii_sdk.models.core import (
    CreateServiceRequest,
    Deployment,
    DeployRequest,
    Release,
    Service,
    UpdateServiceRequest,
)
from enclii_sdk.resources._base import Resource


class ServicesResource(Resource):
    """Service CRUD and build/deploy orchestration."""

    async def list(self, project_slug: str) -> list[Service]:
        """List services in a project.

        API: ``GET /v1/projects/{slug}/services``.
        """
        data = await self._client.get(f"/v1/projects/{project_slug}/services")
        return [Service.model_validate(s) for s in data.get("services", [])]

    async def get(self, service_id: str) -> Service:
        """Fetch a single service by id.

        API: ``GET /v1/services/{id}``.
        """
        data = await self._client.get(f"/v1/services/{service_id}")
        return Service.model_validate(data)

    async def create(
        self,
        project_slug: str,
        *,
        name: str,
        git_repo: str,
        build_config: dict[str, Any] | None = None,
        app_path: str | None = None,
        watch_paths: list[str] | None = None,
    ) -> Service:
        """Create a new service under a project.

        API: ``POST /v1/projects/{slug}/services``. Requires ``developer``.
        """
        request = CreateServiceRequest(
            name=name,
            git_repo=git_repo,
            build_config=build_config,
            app_path=app_path,
            watch_paths=watch_paths,
        )
        data = await self._client.post(
            f"/v1/projects/{project_slug}/services",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return Service.model_validate(data)

    async def update(
        self,
        service_id: str,
        *,
        name: str | None = None,
        app_path: str | None = None,
        watch_paths: list[str] | None = None,
        auto_deploy: bool | None = None,
        auto_deploy_branch: str | None = None,
        auto_deploy_env: str | None = None,
        headers: dict[str, str] | None = None,
    ) -> Service:
        """Partially update a service. Only non-None fields are applied.

        API: ``PATCH /v1/services/{id}``. Requires ``developer``.
        """
        request = UpdateServiceRequest(
            name=name,
            app_path=app_path,
            watch_paths=watch_paths,
            auto_deploy=auto_deploy,
            auto_deploy_branch=auto_deploy_branch,
            auto_deploy_env=auto_deploy_env,
            headers=headers,
        )
        data = await self._client.patch(
            f"/v1/services/{service_id}",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return Service.model_validate(data)

    async def delete(self, service_id: str) -> None:
        """Delete a service. Requires ``admin``."""
        await self._client.delete(f"/v1/services/{service_id}")

    # ------------------------------------------------------------------
    # Build / deploy helpers
    # ------------------------------------------------------------------

    async def build(self, service_id: str, *, git_sha: str) -> Release:
        """Trigger a build for a specific commit.

        API: ``POST /v1/services/{id}/build``. Requires ``developer``.
        """
        data = await self._client.post(
            f"/v1/services/{service_id}/build",
            json_body={"git_sha": git_sha},
        )
        return Release.model_validate(data)

    async def list_releases(self, service_id: str) -> list[Release]:
        """List all releases for a service.

        API: ``GET /v1/services/{id}/releases``.
        """
        data = await self._client.get(f"/v1/services/{service_id}/releases")
        return [Release.model_validate(r) for r in data.get("releases", [])]

    async def deploy(
        self,
        service_id: str,
        *,
        release_id: str,
        environment_name: str,
        replicas: int | None = None,
        environment: dict[str, str] | None = None,
    ) -> Deployment:
        """Deploy a release to an environment.

        API: ``POST /v1/services/{id}/deploy``. Requires ``developer``.
        """
        request = DeployRequest(
            release_id=release_id,
            environment_name=environment_name,
            replicas=replicas,
            environment=environment,
        )
        data = await self._client.post(
            f"/v1/services/{service_id}/deploy",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return Deployment.model_validate(data)
