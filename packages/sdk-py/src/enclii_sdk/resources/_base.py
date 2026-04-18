"""Shared base class for resource namespaces."""

from __future__ import annotations

from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from enclii_sdk.client import AsyncEncliiClient


class Resource:
    """Common state holder. All resource classes subclass this."""

    def __init__(self, client: AsyncEncliiClient) -> None:
        self._client = client
