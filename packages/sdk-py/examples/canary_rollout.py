"""Start a canary rollout and poll until it reaches a terminal state.

Usage::

    python examples/canary_rollout.py <service_id> <digest> <percentage> <change_ticket_url>

Environment variables:
    ENCLII_TOKEN     Required. API token.
    ENCLII_API_URL   Optional. Defaults to https://api.enclii.dev.
"""

from __future__ import annotations

import asyncio
import os
import sys

from enclii_sdk import AsyncEncliiClient


async def main(
    service_id: str, digest: str, percentage: int, change_ticket_url: str
) -> int:
    async with AsyncEncliiClient(
        base_url=os.environ.get("ENCLII_API_URL", "https://api.enclii.dev"),
        token=os.environ["ENCLII_TOKEN"],
    ) as enclii:
        print(f"Starting canary: {digest} @ {percentage}%…")
        rollout = await enclii.canary.start(
            service_id,
            digest=digest,
            percentage=percentage,
            validation_window_minutes=10,
            change_ticket_url=change_ticket_url,
        )
        print(f"  rollout_id={rollout.id}")
        print(f"  initial_state={rollout.state}")
        print(f"  actual_traffic={rollout.actual_percentage}%")

        while rollout.state.is_active():
            await asyncio.sleep(15)
            rollout = await enclii.canary.get(service_id, str(rollout.id))
            print(f"  state={rollout.state}")

        print(f"\nTerminal state: {rollout.state}")
        if rollout.last_error:
            print(f"  error: {rollout.last_error}")
        if rollout.rollback_reason:
            print(f"  rollback_reason: {rollout.rollback_reason}")

        return 0 if rollout.state.value == "succeeded" else 1


if __name__ == "__main__":
    if len(sys.argv) < 5:
        print(__doc__)
        sys.exit(2)
    sys.exit(
        asyncio.run(
            main(sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4])
        )
    )
