"""Shared pytest fixtures for the Enclii SDK test suite."""

from __future__ import annotations

from collections.abc import AsyncIterator, Callable
from datetime import UTC, datetime
from typing import Any
from uuid import uuid4

import httpx
import pytest
import pytest_asyncio

from enclii_sdk import AsyncEncliiClient


def _iso(dt: datetime | None = None) -> str:
    return (dt or datetime.now(UTC)).isoformat()


@pytest.fixture
def fake_uuid() -> str:
    """Stable UUID factory for test fixtures."""
    return str(uuid4())


@pytest.fixture
def sample_project_payload(fake_uuid: str) -> dict[str, Any]:
    return {
        "id": fake_uuid,
        "name": "demo",
        "slug": "demo",
        "ci_runner_mode": "github",
        "created_at": _iso(),
        "updated_at": _iso(),
    }


@pytest.fixture
def sample_service_payload(fake_uuid: str) -> dict[str, Any]:
    return {
        "id": fake_uuid,
        "project_id": str(uuid4()),
        "name": "api",
        "git_repo": "https://github.com/madfam-org/api",
        "app_path": "",
        "watch_paths": [],
        "build_config": {"type": "auto"},
        "auto_deploy": False,
        "auto_deploy_branch": "",
        "auto_deploy_env": "",
        "health": "healthy",
        "status": "running",
        "desired_replicas": 2,
        "ready_replicas": 2,
        "created_at": _iso(),
        "updated_at": _iso(),
    }


@pytest.fixture
def sample_deployment_payload(fake_uuid: str) -> dict[str, Any]:
    return {
        "id": fake_uuid,
        "release_id": str(uuid4()),
        "environment_id": str(uuid4()),
        "service_id": str(uuid4()),
        "version_number": 42,
        "replicas": 2,
        "status": "running",
        "health": "healthy",
        "deploy_order": 0,
        "created_at": _iso(),
        "updated_at": _iso(),
    }


@pytest.fixture
def sample_release_payload(fake_uuid: str) -> dict[str, Any]:
    return {
        "id": fake_uuid,
        "service_id": str(uuid4()),
        "version": "v1",
        "image_uri": "ghcr.io/madfam-org/api:abc",
        "git_sha": "abcdef0123",
        "status": "ready",
        "created_at": _iso(),
        "updated_at": _iso(),
    }


@pytest.fixture
def sample_canary_payload(fake_uuid: str) -> dict[str, Any]:
    return {
        "id": fake_uuid,
        "service_id": str(uuid4()),
        "environment_id": str(uuid4()),
        "stable_deployment_id": str(uuid4()),
        "canary_deployment_id": str(uuid4()),
        "canary_digest": "sha256:deadbeef",
        "canary_percentage": 20,
        "total_replicas": 5,
        "canary_replicas": 1,
        "stable_replicas": 4,
        "validation_window_seconds": 600,
        "error_rate_threshold": 0.05,
        "state": "running",
        "actual_percentage": 20.0,
        "created_at": _iso(),
        "updated_at": _iso(),
    }


@pytest.fixture
def sample_webhook_subscription(fake_uuid: str) -> dict[str, Any]:
    return {
        "id": fake_uuid,
        "project_id": str(uuid4()),
        "name": "Slack #deploys",
        "url": "https://hooks.slack.com/services/example",
        "secret_sha256_prefix": "a1b2c3d4",
        "event_types": ["deploy.succeeded", "deploy.failed"],
        "active": True,
        "created_by": "ci@madfam.io",
        "consecutive_failures": 0,
        "created_at": _iso(),
        "updated_at": _iso(),
    }


@pytest.fixture
def sample_log_entry() -> dict[str, Any]:
    return {
        "timestamp": _iso(),
        "level": "error",
        "pod": "api-7f6b8-abc",
        "container": "api",
        "message": "oh no",
        "labels": {"app": "api"},
    }


HandlerFn = Callable[[httpx.Request], httpx.Response]


@pytest.fixture
def mock_transport_factory() -> Callable[[HandlerFn], httpx.MockTransport]:
    """Build :class:`httpx.MockTransport` instances from a handler function."""

    def _factory(handler: HandlerFn) -> httpx.MockTransport:
        return httpx.MockTransport(handler)

    return _factory


@pytest_asyncio.fixture
async def make_client(
    mock_transport_factory: Callable[[HandlerFn], httpx.MockTransport],
) -> AsyncIterator[Callable[[HandlerFn], AsyncEncliiClient]]:
    """Build an :class:`AsyncEncliiClient` with a mocked httpx transport.

    Closes the client in teardown — tests only need to call the factory
    once and trust cleanup to the fixture.
    """
    created: list[AsyncEncliiClient] = []

    def _make(handler: HandlerFn) -> AsyncEncliiClient:
        transport = mock_transport_factory(handler)
        http_client = httpx.AsyncClient(
            base_url="https://api.enclii.test",
            transport=transport,
            timeout=5.0,
        )
        client = AsyncEncliiClient(
            base_url="https://api.enclii.test",
            token="enclii_test",
            http_client=http_client,
            max_retries=1,
        )
        # SDK client doesn't own externally-supplied http_client.
        # Track both for cleanup.
        created.append(client)
        created.append(http_client)  # type: ignore[arg-type]
        return client

    yield _make

    for obj in created:
        if hasattr(obj, "aclose"):
            await obj.aclose()
