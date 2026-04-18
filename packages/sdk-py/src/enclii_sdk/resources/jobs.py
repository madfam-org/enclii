"""Timetable jobs: cron jobs and one-off jobs.

Note: the switchyard-api cron-job endpoints currently return 501 (stub).
The SDK is shipped anyway so consumers can start wiring call sites —
the shape will match when the handlers are implemented.
"""

from __future__ import annotations

from enclii_sdk.models.jobs import (
    CreateCronJobRequest,
    CreateOneOffJobRequest,
    CronJob,
    CronJobRun,
)
from enclii_sdk.resources._base import Resource


class JobsResource(Resource):
    """Cron jobs and one-off jobs (``timetable`` tag)."""

    async def create_cron(
        self,
        project_slug: str,
        *,
        name: str,
        schedule: str,
        command: list[str],
        image: str,
        environment_name: str = "production",
    ) -> CronJob:
        """Create a cron job.

        API: ``POST /v1/projects/{slug}/cron-jobs``. Requires ``developer``.
        """
        request = CreateCronJobRequest(
            name=name,
            schedule=schedule,
            command=command,
            image=image,
            environment_name=environment_name,
        )
        data = await self._client.post(
            f"/v1/projects/{project_slug}/cron-jobs",
            json_body=request.model_dump(mode="json"),
        )
        return CronJob.model_validate(data)

    async def list_cron(self, project_slug: str) -> list[CronJob]:
        """List cron jobs for a project."""
        data = await self._client.get(f"/v1/projects/{project_slug}/cron-jobs")
        items = data if isinstance(data, list) else data.get("cron_jobs", [])
        return [CronJob.model_validate(j) for j in items]

    async def get_cron(self, job_id: str) -> CronJob:
        """Fetch a cron job by id."""
        data = await self._client.get(f"/v1/cron-jobs/{job_id}")
        return CronJob.model_validate(data)

    async def delete_cron(self, job_id: str) -> None:
        """Delete a cron job. Requires ``admin``."""
        await self._client.delete(f"/v1/cron-jobs/{job_id}")

    async def list_cron_runs(self, job_id: str) -> list[CronJobRun]:
        """List historical runs of a cron job."""
        data = await self._client.get(f"/v1/cron-jobs/{job_id}/runs")
        items = data if isinstance(data, list) else data.get("runs", [])
        return [CronJobRun.model_validate(r) for r in items]

    async def create_one_off(
        self,
        project_slug: str,
        *,
        name: str,
        command: list[str],
        image: str,
        environment_name: str = "production",
    ) -> CronJob:
        """Create a one-off job (runs once and exits).

        API: ``POST /v1/projects/{slug}/one-off-jobs``. Requires ``developer``.
        """
        request = CreateOneOffJobRequest(
            name=name,
            command=command,
            image=image,
            environment_name=environment_name,
        )
        data = await self._client.post(
            f"/v1/projects/{project_slug}/one-off-jobs",
            json_body=request.model_dump(mode="json"),
        )
        return CronJob.model_validate(data)
