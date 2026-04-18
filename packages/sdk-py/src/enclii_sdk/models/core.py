"""Core resource models: project, service, release, deployment, env vars."""

from __future__ import annotations

from datetime import datetime
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

from enclii_sdk.models.common import (
    CIRunnerMode,
    DeploymentStatus,
    HealthStatus,
    ReleaseStatus,
    StrEnum,
)


class BuildType(StrEnum):
    """How a service's container image is built."""

    AUTO = "auto"
    DOCKERFILE = "dockerfile"
    BUILDPACK = "buildpack"


class BuildConfig(BaseModel):
    """Build-time configuration for a service.

    Maps to ``types.BuildConfig`` in the Go SDK.
    """

    model_config = ConfigDict(extra="allow")

    type: BuildType = BuildType.AUTO
    dockerfile: str | None = None
    buildpack: str | None = None
    context: str | None = None
    build_args: dict[str, str] | None = None
    target: str | None = None


class Project(BaseModel):
    """A collection of services sharing environments and access control."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    name: str
    slug: str
    ci_runner_mode: CIRunnerMode = CIRunnerMode.GITHUB
    created_at: datetime
    updated_at: datetime


class Environment(BaseModel):
    """A deployment target within a project (dev/staging/prod/preview-*)."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    project_id: UUID
    name: str
    kube_namespace: str
    created_at: datetime
    updated_at: datetime


class Service(BaseModel):
    """A deployable application inside a project."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    project_id: UUID
    name: str
    git_repo: str
    app_path: str = ""
    watch_paths: list[str] = Field(default_factory=list)
    build_config: BuildConfig
    headers: dict[str, str] | None = None
    auto_deploy: bool = False
    auto_deploy_branch: str = ""
    auto_deploy_env: str = ""
    health: HealthStatus = HealthStatus.UNKNOWN
    status: str = "unknown"
    desired_replicas: int = 0
    ready_replicas: int = 0
    last_health_check: datetime | None = None
    last_deployment: datetime | None = None
    last_commit_message: str = ""
    last_commit_branch: str = ""
    created_at: datetime
    updated_at: datetime


class Release(BaseModel):
    """An immutable, built version of a service."""

    model_config = ConfigDict(extra="allow")

    id: UUID
    service_id: UUID
    version: str
    image_uri: str
    git_sha: str
    git_branch: str = ""
    commit_message: str = ""
    commit_author_name: str = ""
    commit_author_email: str = ""
    pr_number: int | None = None
    pr_title: str = ""
    pr_url: str = ""
    repo_url: str = ""
    status: ReleaseStatus
    error_message: str | None = None
    sbom: str = ""
    sbom_format: str = ""
    image_signature: str = ""
    signature_verified_at: datetime | None = None
    created_at: datetime
    updated_at: datetime


class Deployment(BaseModel):
    """A running instance of a :class:`Release` in an :class:`Environment`.

    The Heroku-style ``version_number`` is populated by the reconciler
    at deploy-start and never reused, even across rollbacks.
    """

    model_config = ConfigDict(extra="allow")

    id: UUID
    release_id: UUID
    environment_id: UUID
    group_id: UUID | None = None
    deploy_order: int = 0
    replicas: int = 0
    status: DeploymentStatus
    health: HealthStatus = HealthStatus.UNKNOWN
    error_message: str | None = None
    service_id: UUID | None = None
    version_number: int | None = None
    created_at: datetime
    updated_at: datetime

    @property
    def version_label(self) -> str:
        """Return the Heroku-style label (``"v42"``) or empty if unallocated."""
        if self.version_number is None:
            return ""
        return f"v{self.version_number}"


class DeploymentEnriched(Deployment):
    """Deployment with joined release + service metadata for UI surfaces."""

    model_config = ConfigDict(extra="allow")

    service_name: str = ""
    git_sha: str = ""
    git_branch: str = ""
    commit_message: str = ""
    commit_author: str = ""
    commit_author_email: str = ""
    pr_number: int | None = None
    pr_title: str = ""
    pr_url: str = ""
    repo_url: str = ""


class EnvironmentVariable(BaseModel):
    """An environment variable attached to a service + environment.

    Secrets are masked with bullet characters in API responses.
    """

    model_config = ConfigDict(extra="allow")

    id: UUID
    service_id: UUID
    environment_id: UUID | None = None
    key: str
    value: str = ""
    is_secret: bool = False
    created_at: datetime
    updated_at: datetime


# ---------------------------------------------------------------------------
# Request payloads
# ---------------------------------------------------------------------------


class CreateProjectRequest(BaseModel):
    """Payload for ``POST /v1/projects``."""

    name: str
    slug: str


class CreateServiceRequest(BaseModel):
    """Payload for ``POST /v1/projects/{slug}/services``."""

    name: str
    git_repo: str
    build_config: dict | None = None
    app_path: str | None = None
    watch_paths: list[str] | None = None


class UpdateServiceRequest(BaseModel):
    """Payload for ``PATCH /v1/services/{id}``. Only non-None fields apply."""

    model_config = ConfigDict(extra="allow")

    name: str | None = None
    app_path: str | None = None
    watch_paths: list[str] | None = None
    auto_deploy: bool | None = None
    auto_deploy_branch: str | None = None
    auto_deploy_env: str | None = None
    headers: dict[str, str] | None = None


class DeployRequest(BaseModel):
    """Payload for ``POST /v1/services/{id}/deploy``."""

    release_id: str
    environment_name: str
    environment: dict[str, str] | None = None
    replicas: int | None = None


class RollbackRequest(BaseModel):
    """Payload for ``POST /v1/deployments/{id}/rollback``."""

    to_release: str | None = None
    change_ticket_url: str | None = None


__all__ = [
    "BuildConfig",
    "BuildType",
    "CreateProjectRequest",
    "CreateServiceRequest",
    "DeployRequest",
    "Deployment",
    "DeploymentEnriched",
    "Environment",
    "EnvironmentVariable",
    "Project",
    "Release",
    "RollbackRequest",
    "Service",
    "UpdateServiceRequest",
]
