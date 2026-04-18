"""Project CRUD (``/v1/projects`` endpoints)."""

from __future__ import annotations

from enclii_sdk.models.core import CreateProjectRequest, Project
from enclii_sdk.resources._base import Resource


class ProjectsResource(Resource):
    """Project management.

    Projects are the top-level grouping for services, environments and
    access grants. Slugs are globally unique within a tenant.
    """

    async def list(self) -> list[Project]:
        """Return all projects visible to the authenticated user.

        API: ``GET /v1/projects``.
        """
        data = await self._client.get("/v1/projects")
        return [Project.model_validate(p) for p in data.get("projects", [])]

    async def get(self, slug: str) -> Project:
        """Fetch a single project by slug.

        API: ``GET /v1/projects/{slug}``.
        """
        data = await self._client.get(f"/v1/projects/{slug}")
        return Project.model_validate(data)

    async def create(self, *, name: str, slug: str) -> Project:
        """Create a new project.

        API: ``POST /v1/projects``. Requires ``admin`` role.
        """
        request = CreateProjectRequest(name=name, slug=slug)
        data = await self._client.post("/v1/projects", json_body=request.model_dump(mode="json"))
        return Project.model_validate(data)

    async def delete(self, slug: str) -> None:
        """Delete a project and all its services.

        API: ``DELETE /v1/projects/{slug}``. Requires ``admin`` role.
        This is irreversible — the caller is responsible for any
        confirmation prompt.
        """
        await self._client.delete(f"/v1/projects/{slug}")
