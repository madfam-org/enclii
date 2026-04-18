"""Tests for :mod:`enclii_sdk.resources.jobs`."""

from __future__ import annotations

from collections.abc import Callable
from datetime import UTC, datetime
from uuid import uuid4

import httpx

from enclii_sdk import AsyncEncliiClient


def _cron_payload() -> dict:
    return {
        "id": str(uuid4()),
        "project_id": str(uuid4()),
        "environment_id": str(uuid4()),
        "name": "nightly",
        "schedule": "0 2 * * *",
        "command": ["python", "manage.py", "migrate"],
        "image": "ghcr.io/madfam-org/karafiel:v42",
        "created_at": datetime.now(UTC).isoformat(),
        "updated_at": datetime.now(UTC).isoformat(),
    }


async def test_create_cron(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/projects/demo/cron-jobs"
        body = request.read().decode()
        assert "nightly" in body
        assert "0 2 * * *" in body
        return httpx.Response(201, json=_cron_payload())

    client = make_client(handler)
    job = await client.jobs.create_cron(
        "demo",
        name="nightly",
        schedule="0 2 * * *",
        command=["python", "manage.py", "migrate"],
        image="ghcr.io/madfam-org/karafiel:v42",
    )
    assert job.schedule == "0 2 * * *"


async def test_list_cron(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json={"cron_jobs": [_cron_payload()]})

    client = make_client(handler)
    jobs = await client.jobs.list_cron("demo")
    assert len(jobs) == 1


async def test_delete_cron(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "DELETE"
        return httpx.Response(204)

    client = make_client(handler)
    await client.jobs.delete_cron("cj_1")


async def test_create_one_off(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/projects/demo/one-off-jobs"
        return httpx.Response(201, json=_cron_payload())

    client = make_client(handler)
    job = await client.jobs.create_one_off(
        "demo",
        name="migrate",
        command=["python", "manage.py", "migrate"],
        image="ghcr.io/madfam-org/karafiel:v42",
    )
    assert job.name == "nightly"  # stub payload returns cron fixture
