"""Tests for :mod:`enclii_sdk.resources.services`."""

from __future__ import annotations

from collections.abc import Callable

import httpx

from enclii_sdk import AsyncEncliiClient


async def test_list_services(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_service_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/projects/demo/services"
        return httpx.Response(200, json={"services": [sample_service_payload]})

    client = make_client(handler)
    services = await client.services.list("demo")
    assert len(services) == 1
    assert services[0].name == "api"


async def test_get_service_parses_health(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_service_payload: dict,
) -> None:
    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=sample_service_payload)

    client = make_client(handler)
    svc = await client.services.get(sample_service_payload["id"])
    assert svc.health.value == "healthy"
    assert svc.ready_replicas == 2


async def test_create_service_strips_nones(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_service_payload: dict,
) -> None:
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = request.read()
        return httpx.Response(201, json=sample_service_payload)

    client = make_client(handler)
    await client.services.create(
        "demo",
        name="api",
        git_repo="https://github.com/x/y",
    )
    body = captured["body"].decode()
    assert '"app_path"' not in body  # None stripped
    assert '"git_repo"' in body


async def test_update_service_patches(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_service_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "PATCH"
        body = request.read().decode()
        assert '"auto_deploy":true' in body.replace(" ", "")
        return httpx.Response(200, json=sample_service_payload)

    client = make_client(handler)
    await client.services.update(sample_service_payload["id"], auto_deploy=True)


async def test_build_and_deploy(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_release_payload: dict,
    sample_deployment_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.url.path.endswith("/build"):
            return httpx.Response(202, json=sample_release_payload)
        if request.url.path.endswith("/deploy"):
            return httpx.Response(201, json=sample_deployment_payload)
        raise AssertionError(f"unexpected path {request.url.path}")

    client = make_client(handler)
    release = await client.services.build("svc_123", git_sha="abc")
    assert release.status.value == "ready"

    deployment = await client.services.deploy(
        "svc_123",
        release_id=str(release.id),
        environment_name="production",
        replicas=3,
    )
    assert deployment.status.value == "running"
    assert deployment.version_label == "v42"


async def test_list_releases(
    make_client: Callable[[Callable[[httpx.Request], httpx.Response]], AsyncEncliiClient],
    sample_release_payload: dict,
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/v1/services/svc_123/releases"
        return httpx.Response(200, json={"releases": [sample_release_payload]})

    client = make_client(handler)
    releases = await client.services.list_releases("svc_123")
    assert len(releases) == 1
