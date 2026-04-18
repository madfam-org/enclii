"""Stream error-level logs from a service.

Usage::

    python examples/tail_logs.py <service_id> [level]

Environment variables:
    ENCLII_TOKEN     Required. API token.
    ENCLII_API_URL   Optional. Defaults to https://api.enclii.dev.
"""

from __future__ import annotations

import asyncio
import os
import sys

from enclii_sdk import AsyncEncliiClient
from enclii_sdk.models.logs import LogLevel


async def main(service_id: str, level_name: str = "error") -> int:
    level = LogLevel(level_name)
    async with AsyncEncliiClient(
        base_url=os.environ.get("ENCLII_API_URL", "https://api.enclii.dev"),
        token=os.environ["ENCLII_TOKEN"],
    ) as enclii:
        print(f"Tailing {service_id} (level={level_name})… Ctrl+C to stop")
        try:
            async for entry in enclii.logs.tail(service_id, level=level):
                print(
                    f"[{entry.timestamp.isoformat()}] {entry.level} "
                    f"{entry.pod or '-'}: {entry.message}"
                )
        except KeyboardInterrupt:
            print("\nInterrupted — exiting.")
    return 0


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(2)
    sys.exit(
        asyncio.run(
            main(sys.argv[1], sys.argv[2] if len(sys.argv) > 2 else "error")
        )
    )
