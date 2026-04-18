"""Build + deploy + wait for RUNNING state.

Usage::

    python examples/deploy_and_wait.py <service_id> <git_sha> [environment]

Environment variables:
    ENCLII_TOKEN     Required. API token.
    ENCLII_API_URL   Optional. Defaults to https://api.enclii.dev.
"""

from __future__ import annotations

import asyncio
import os
import sys

from enclii_sdk import AsyncEncliiClient


async def main(service_id: str, git_sha: str, environment: str = "production") -> int:
    async with AsyncEncliiClient(
        base_url=os.environ.get("ENCLII_API_URL", "https://api.enclii.dev"),
        token=os.environ["ENCLII_TOKEN"],
    ) as enclii:
        print(f"Triggering build for {service_id} @ {git_sha}…")
        release = await enclii.services.build(service_id, git_sha=git_sha)
        print(f"  release_id={release.id} status={release.status}")

        # Poll the release until it's READY or FAILED. The deploy below
        # will 400 if we try to use a not-yet-built release.
        while release.status.value == "building":
            await asyncio.sleep(5)
            releases = await enclii.services.list_releases(service_id)
            for r in releases:
                if r.id == release.id:
                    release = r
                    break
            print(f"  build status: {release.status}")

        if release.status.value != "ready":
            print(f"Build failed: {release.error_message}")
            return 1

        print(f"Deploying {release.id} to {environment}…")
        deployment = await enclii.services.deploy(
            service_id,
            release_id=str(release.id),
            environment_name=environment,
        )
        print(f"  deployment_id={deployment.id}")

        print("Waiting for RUNNING state…")
        final = await enclii.deployments.wait_for_running(
            str(deployment.id), timeout=600.0
        )
        print(f"Deployed {final.version_label} ({final.ready_replicas} replicas)")
        return 0


if __name__ == "__main__":
    if len(sys.argv) < 3:
        print(__doc__)
        sys.exit(2)
    sys.exit(
        asyncio.run(
            main(sys.argv[1], sys.argv[2], sys.argv[3] if len(sys.argv) > 3 else "production")
        )
    )
