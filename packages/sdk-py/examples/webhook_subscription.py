"""Register a webhook subscription and print the signing secret.

The signing secret is returned by the API exactly once — persist it
immediately (e.g. to a secret manager). Running this example twice
creates two subscriptions.

Usage::

    python examples/webhook_subscription.py <project_slug> <name> <url> [events…]

Example::

    python examples/webhook_subscription.py demo "Slack #deploys" \\
        https://hooks.slack.com/services/T/B/X \\
        deploy.succeeded deploy.failed rollback.succeeded

Environment variables:
    ENCLII_TOKEN     Required. API token.
    ENCLII_API_URL   Optional. Defaults to https://api.enclii.dev.
"""

from __future__ import annotations

import asyncio
import os
import sys

from enclii_sdk import AsyncEncliiClient


async def main(project_slug: str, name: str, url: str, events: list[str]) -> int:
    if not events:
        # Sensible default for most callers.
        events = ["deploy.succeeded", "deploy.failed", "rollback.succeeded"]

    async with AsyncEncliiClient(
        base_url=os.environ.get("ENCLII_API_URL", "https://api.enclii.dev"),
        token=os.environ["ENCLII_TOKEN"],
    ) as enclii:
        resp = await enclii.webhooks.create(
            project_slug, name=name, url=url, events=events
        )
        sub = resp.subscription
        print(f"Created subscription {sub.id}")
        print(f"  name:   {sub.name}")
        print(f"  url:    {sub.url}")
        print(f"  events: {', '.join(e.value for e in sub.event_types)}")
        print()
        print("SIGNING SECRET (shown once — save this now):")
        print(f"  {resp.signing_secret}")
        if resp.note:
            print()
            print(resp.note)
    return 0


if __name__ == "__main__":
    if len(sys.argv) < 4:
        print(__doc__)
        sys.exit(2)
    sys.exit(
        asyncio.run(main(sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4:]))
    )
