"""Tests for :mod:`enclii_sdk.resources.projects`."""

from __future__ import annotations

from collections.abc import Callable

import httpx

from enclii_sdk import AsyncEncliiClient


async def test_list_projects(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_project_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "GET"
        assert request.url.path == "/v1/projects"
        return httpx.Response(200, json={"projects": [sample_project_payload]})

    client = make_client(handler)
    projects = await client.projects.list()
    assert len(projects) == 1
    assert projects[0].slug == "demo"


async def test_get_project(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_project_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/projects/demo"
        return httpx.Response(200, json=sample_project_payload)

    client = make_client(handler)
    project = await client.projects.get("demo")
    assert project.name == "demo"


async def test_create_project(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_project_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url.path == "/v1/projects"
        body = request.read().decode()
        assert '"name":"demo"' in body.replace(" ", "")
        return httpx.Response(201, json=sample_project_payload)

    client = make_client(handler)
    created = await client.projects.create(name="demo", slug="demo")
    assert created.slug == "demo"


async def test_delete_project(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "DELETE"
        assert request.url.path == "/v1/projects/demo"
        return httpx.Response(204)

    client = make_client(handler)
    await client.projects.delete("demo")
