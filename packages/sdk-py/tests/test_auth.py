"""Auth helper: token resolution precedence."""

from __future__ import annotations

import pytest

from enclii_sdk.auth import resolve_token_source


async def test_literal_token_returned_as_provider() -> None:
    provider = resolve_token_source("abc")
    assert await provider() == "abc"


async def test_callable_passthrough() -> None:
    async def custom() -> str:
        return "dynamic"

    provider = resolve_token_source(custom)
    assert await provider() == "dynamic"
    assert provider is custom


async def test_env_fallback(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ENCLII_TOKEN", "from_env")
    provider = resolve_token_source(None)
    assert await provider() == "from_env"


async def test_empty_string_falls_back_to_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ENCLII_TOKEN", "from_env")
    # Empty string is truthy-False → falls through.
    provider = resolve_token_source("")
    assert await provider() == "from_env"


async def test_missing_everywhere_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("ENCLII_TOKEN", raising=False)
    with pytest.raises(ValueError, match="No Enclii API token"):
        resolve_token_source(None)


async def test_env_re_read_on_each_call(monkeypatch: pytest.MonkeyPatch) -> None:
    """Long-running processes can rotate the env var and see the change."""
    monkeypatch.setenv("ENCLII_TOKEN", "first")
    provider = resolve_token_source(None)
    assert await provider() == "first"
    monkeypatch.setenv("ENCLII_TOKEN", "second")
    assert await provider() == "second"
