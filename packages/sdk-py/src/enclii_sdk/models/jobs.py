"""Timetable models: cron jobs and one-off jobs.

Note: the cron-jobs API surface is implemented as stubs returning 501 in
switchyard-api at the time of writing (see handlers.go:623). The models
are shipped anyway so consumers can start wiring integrations now.
"""

from __future__ import annotations

from datetime import datetime
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field


class CreateCronJobRequest(BaseModel):
    """Payload for ``POST /v1/projects/{slug}/cron-jobs``."""

    name: str
    schedule: str = Field(description="Cron expression (e.g. ``0 */6 * * *``)")
    command: list[str]
    image: str
    environment_name: str = "production"


class CreateOneOffJobRequest(BaseModel):
    """Payload for ``POST /v1/projects/{slug}/one-off-jobs``."""

    name: str
    command: list[str]
    image: str
    environment_name: str = "production"


class CronJob(BaseModel):
    """A scheduled job."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    project_id: UUID
    name: str
    schedule: str
    command: list[str] = Field(default_factory=list)
    image: str
    environment_id: UUID
    created_at: datetime
    updated_at: datetime


class CronJobRun(BaseModel):
    """Historical run of a cron job."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    cron_job_id: UUID
    status: str
    started_at: datetime | None = None
    finished_at: datetime | None = None
    exit_code: int | None = None
    created_at: datetime


__all__ = ["CreateCronJobRequest", "CreateOneOffJobRequest", "CronJob", "CronJobRun"]
