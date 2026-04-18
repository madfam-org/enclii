"""Authentication helpers for the Enclii SDK.

Bearer tokens are the supported mechanism. Static tokens and async token
providers are both accepted — the latter is useful for OIDC flows where
tokens must be refreshed periodically (e.g. via Janua's refresh endpoint).
"""

from __future__ import annotations

import os
from collections.abc import Awaitable, Callable

TokenProvider = Callable[[], Awaitable[str]]
"""Async callable returning a fresh bearer token on each call.

Implementations should handle their own caching / refresh logic — the
SDK invokes the provider on every request, so a lazy cache is usually
what you want.
"""

TokenInput = str | TokenProvider | None
"""Accepted shapes for the ``token`` parameter to :class:`AsyncEncliiClient`."""


def resolve_token_source(token: TokenInput) -> TokenProvider:
    """Normalize a token input into an async provider.

    Precedence: explicit argument → ``ENCLII_TOKEN`` env var → raise.

    Args:
        token: Either a literal string token, an async callable returning
            a token, or ``None`` to fall back to the environment variable.

    Returns:
        An async callable that produces a bearer token.

    Raises:
        ValueError: If no token is available from any source.
    """
    if callable(token):
        return token

    if isinstance(token, str) and token:
        literal = token

        async def _static() -> str:
            return literal

        return _static

    env_token = os.environ.get("ENCLII_TOKEN")
    if env_token:

        async def _env() -> str:
            # Re-read on every call so rotation during long-running
            # processes takes effect without a client restart.
            return os.environ.get("ENCLII_TOKEN", env_token)

        return _env

    raise ValueError(
        "No Enclii API token provided. Pass `token=...`, `token_provider=...`, "
        "or set the ENCLII_TOKEN environment variable."
    )


__all__ = ["TokenInput", "TokenProvider", "resolve_token_source"]
