"""Enclii Platform API clients: async primary, sync wrapper."""

from __future__ import annotations

import asyncio
import json
import logging
from collections.abc import Mapping
from types import TracebackType
from typing import TYPE_CHECKING, Any, Self

import httpx
from tenacity import (
    AsyncRetrying,
    RetryError,
    retry_if_exception,
    stop_after_attempt,
    wait_exponential,
)

from enclii_sdk.auth import TokenInput, TokenProvider, resolve_token_source
from enclii_sdk.errors import (
    AuthError,
    ConflictError,
    EncliiError,
    NetworkError,
    NotFoundError,
    PermissionError,
    RateLimitError,
    ServerError,
    ValidationError,
)

if TYPE_CHECKING:
    # Resource modules import from this module at runtime; forward declare
    # for type checkers to avoid circular-import drama.
    from enclii_sdk.resources.audit import AuditResource
    from enclii_sdk.resources.canary import CanaryResource
    from enclii_sdk.resources.deployments import DeploymentsResource
    from enclii_sdk.resources.jobs import JobsResource
    from enclii_sdk.resources.logs import LogsResource
    from enclii_sdk.resources.projects import ProjectsResource
    from enclii_sdk.resources.rollback import RollbackResource
    from enclii_sdk.resources.secrets import SecretsResource
    from enclii_sdk.resources.services import ServicesResource
    from enclii_sdk.resources.webhooks import WebhooksResource

__version__ = "0.1.0"
DEFAULT_BASE_URL = "https://api.enclii.dev"
DEFAULT_TIMEOUT = 30.0
DEFAULT_USER_AGENT = f"enclii-sdk-py/{__version__}"
DEFAULT_MAX_RETRIES = 3

logger = logging.getLogger("enclii_sdk")


class AsyncEncliiClient:
    """Async client for the Enclii Platform API.

    The client owns an :class:`httpx.AsyncClient`. Prefer using it as an
    async context manager so the underlying connection pool is closed
    deterministically::

        async with AsyncEncliiClient(token="enclii_...") as enclii:
            projects = await enclii.projects.list()

    Args:
        base_url: API root. Defaults to ``https://api.enclii.dev`` and
            respects the ``ENCLII_API_URL`` environment variable if set.
        token: Bearer token string. Mutually exclusive with ``token_provider``.
        token_provider: Async callable returning a bearer token. Called
            on every request so OIDC flows can refresh lazily.
        timeout: Request timeout in seconds. Defaults to 30.
        max_retries: Max retries for retriable failures (429, 5xx,
            transport errors). Defaults to 3.
        user_agent: Optional User-Agent override.
        http_client: Optional pre-built :class:`httpx.AsyncClient`. If
            provided, the client's lifecycle is **not** managed by the SDK —
            callers must close it themselves.
    """

    def __init__(
        self,
        *,
        base_url: str = DEFAULT_BASE_URL,
        token: TokenInput = None,
        token_provider: TokenProvider | None = None,
        timeout: float = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        user_agent: str = DEFAULT_USER_AGENT,
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if token is not None and token_provider is not None:
            raise ValueError("Pass either token= or token_provider=, not both")
        self._token_source = resolve_token_source(token_provider or token)

        self._base_url = base_url.rstrip("/")
        self._timeout = timeout
        self._max_retries = max_retries
        self._user_agent = user_agent

        self._external_http = http_client is not None
        self._http = http_client or httpx.AsyncClient(
            base_url=self._base_url,
            timeout=timeout,
            headers={"User-Agent": user_agent},
        )

        # Resources are lazily initialized on first access to keep
        # `AsyncEncliiClient()` construction cheap and import-safe.
        self._projects: ProjectsResource | None = None
        self._services: ServicesResource | None = None
        self._deployments: DeploymentsResource | None = None
        self._rollback: RollbackResource | None = None
        self._canary: CanaryResource | None = None
        self._logs: LogsResource | None = None
        self._audit: AuditResource | None = None
        self._webhooks: WebhooksResource | None = None
        self._secrets: SecretsResource | None = None
        self._jobs: JobsResource | None = None

    # ------------------------------------------------------------------
    # Context-manager plumbing
    # ------------------------------------------------------------------

    async def __aenter__(self) -> Self:
        return self

    async def __aexit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        await self.aclose()

    async def aclose(self) -> None:
        """Close the underlying HTTP transport.

        Idempotent. No-op if an externally-owned :class:`httpx.AsyncClient`
        was supplied at construction — the caller is responsible for that.
        """
        if not self._external_http:
            await self._http.aclose()

    # ------------------------------------------------------------------
    # Public accessors
    # ------------------------------------------------------------------

    @property
    def base_url(self) -> str:
        """Configured API root without trailing slash."""
        return self._base_url

    @property
    def http(self) -> httpx.AsyncClient:
        """Underlying :class:`httpx.AsyncClient`. Mostly for tests."""
        return self._http

    @property
    def projects(self) -> ProjectsResource:
        """Project CRUD operations."""
        if self._projects is None:
            from enclii_sdk.resources.projects import ProjectsResource

            self._projects = ProjectsResource(self)
        return self._projects

    @property
    def services(self) -> ServicesResource:
        """Service CRUD + build operations."""
        if self._services is None:
            from enclii_sdk.resources.services import ServicesResource

            self._services = ServicesResource(self)
        return self._services

    @property
    def deployments(self) -> DeploymentsResource:
        """Deployment queries (list, get, get by v-number)."""
        if self._deployments is None:
            from enclii_sdk.resources.deployments import DeploymentsResource

            self._deployments = DeploymentsResource(self)
        return self._deployments

    @property
    def rollback(self) -> RollbackResource:
        """Deployment rollback operations."""
        if self._rollback is None:
            from enclii_sdk.resources.rollback import RollbackResource

            self._rollback = RollbackResource(self)
        return self._rollback

    @property
    def canary(self) -> CanaryResource:
        """Canary rollout lifecycle (P2.7)."""
        if self._canary is None:
            from enclii_sdk.resources.canary import CanaryResource

            self._canary = CanaryResource(self)
        return self._canary

    @property
    def logs(self) -> LogsResource:
        """Log query and WebSocket tail."""
        if self._logs is None:
            from enclii_sdk.resources.logs import LogsResource

            self._logs = LogsResource(self)
        return self._logs

    @property
    def audit(self) -> AuditResource:
        """Audit log queries."""
        if self._audit is None:
            from enclii_sdk.resources.audit import AuditResource

            self._audit = AuditResource(self)
        return self._audit

    @property
    def webhooks(self) -> WebhooksResource:
        """Outbound lifecycle webhook subscription management (P2.3)."""
        if self._webhooks is None:
            from enclii_sdk.resources.webhooks import WebhooksResource

            self._webhooks = WebhooksResource(self)
        return self._webhooks

    @property
    def secrets(self) -> SecretsResource:
        """Environment variables and secrets management."""
        if self._secrets is None:
            from enclii_sdk.resources.secrets import SecretsResource

            self._secrets = SecretsResource(self)
        return self._secrets

    @property
    def jobs(self) -> JobsResource:
        """Cron jobs and one-off jobs (timetable)."""
        if self._jobs is None:
            from enclii_sdk.resources.jobs import JobsResource

            self._jobs = JobsResource(self)
        return self._jobs

    # ------------------------------------------------------------------
    # Low-level HTTP
    # ------------------------------------------------------------------

    async def request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        params: Mapping[str, Any] | None = None,
        headers: Mapping[str, str] | None = None,
    ) -> Any:
        """Send an authenticated request and return the decoded body.

        Applies retries for 429/5xx/transport failures and raises the
        appropriate :class:`EncliiError` subclass on terminal failures.
        """
        token = await self._token_source()

        async def _attempt() -> Any:
            request_headers = {
                "Authorization": f"Bearer {token}",
                "Accept": "application/json",
                "User-Agent": self._user_agent,
            }
            if json_body is not None:
                request_headers["Content-Type"] = "application/json"
            if headers:
                request_headers.update(headers)

            try:
                response = await self._http.request(
                    method,
                    path,
                    json=json_body,
                    params=_coerce_params(params),
                    headers=request_headers,
                )
            except httpx.TransportError as exc:
                raise NetworkError(f"transport error: {exc}") from exc

            return self._parse_response(response)

        try:
            async for attempt in AsyncRetrying(
                stop=stop_after_attempt(self._max_retries + 1),
                wait=wait_exponential(multiplier=0.5, min=0.5, max=8.0),
                retry=retry_if_exception(_is_retriable),
                reraise=True,
            ):
                with attempt:
                    return await _attempt()
        except RetryError as exc:  # pragma: no cover — reraise=True covers it
            raise exc.last_attempt.exception() from exc

        # Should be unreachable since reraise=True.
        raise RuntimeError("retry loop exited without result")  # pragma: no cover

    def _parse_response(self, response: httpx.Response) -> Any:
        """Translate an :class:`httpx.Response` into a Python value or error.

        Error responses are mapped to the appropriate :class:`EncliiError`
        subclass based on status code. The API error envelope is
        ``{"error": str, "details"?: str, "hint"?: str}`` per
        ``components/schemas/Error`` in the OpenAPI spec.
        """
        request_id = response.headers.get("X-Request-Id")

        if response.is_success:
            if response.status_code == 204 or not response.content:
                return None
            ctype = response.headers.get("Content-Type", "")
            if "application/json" in ctype:
                return response.json()
            return response.text

        payload: dict[str, Any]
        try:
            payload = response.json() if response.content else {}
            if not isinstance(payload, dict):
                payload = {"error": str(payload)}
        except (json.JSONDecodeError, ValueError):
            payload = {"error": response.text or response.reason_phrase}

        message = str(payload.get("error") or payload.get("message") or "unknown error")
        details = payload.get("details")
        hint = payload.get("hint")
        status = response.status_code

        common_kwargs: dict[str, Any] = {
            "status_code": status,
            "details": details if isinstance(details, str) else None,
            "hint": hint if isinstance(hint, str) else None,
            "request_id": request_id,
            "response_body": payload,
        }

        if status == 401:
            raise AuthError(message, **common_kwargs)
        if status == 403:
            raise PermissionError(message, **common_kwargs)
        if status == 404:
            raise NotFoundError(message, **common_kwargs)
        if status == 409:
            raise ConflictError(message, **common_kwargs)
        if status in (400, 422):
            raise ValidationError(message, **common_kwargs)
        if status == 429:
            retry_after = _parse_retry_after(response.headers.get("Retry-After"))
            raise RateLimitError(message, retry_after_seconds=retry_after, **common_kwargs)
        if status >= 500:
            raise ServerError(message, **common_kwargs)
        raise EncliiError(message, **common_kwargs)

    # ------------------------------------------------------------------
    # Convenience helpers used by resource modules
    # ------------------------------------------------------------------

    async def get(
        self,
        path: str,
        *,
        params: Mapping[str, Any] | None = None,
    ) -> Any:
        """Shortcut for ``GET path``."""
        return await self.request("GET", path, params=params)

    async def post(
        self,
        path: str,
        *,
        json_body: Any = None,
        params: Mapping[str, Any] | None = None,
    ) -> Any:
        """Shortcut for ``POST path``."""
        return await self.request("POST", path, json_body=json_body, params=params)

    async def patch(
        self,
        path: str,
        *,
        json_body: Any = None,
    ) -> Any:
        """Shortcut for ``PATCH path``."""
        return await self.request("PATCH", path, json_body=json_body)

    async def put(
        self,
        path: str,
        *,
        json_body: Any = None,
    ) -> Any:
        """Shortcut for ``PUT path``."""
        return await self.request("PUT", path, json_body=json_body)

    async def delete(self, path: str) -> None:
        """Shortcut for ``DELETE path``; returns ``None`` on success."""
        await self.request("DELETE", path)

    async def _get_token(self) -> str:
        """Internal helper for resources that need the bearer token
        outside the main request flow (e.g. WebSocket connections)."""
        return await self._token_source()


def _is_retriable(exc: BaseException) -> bool:
    """Return True if the exception class should trigger a retry."""
    return isinstance(exc, (RateLimitError, ServerError, NetworkError))


def _parse_retry_after(raw: str | None) -> float | None:
    """Parse an HTTP ``Retry-After`` header value into a float (seconds).

    Only the numeric-seconds form is supported; HTTP-date form is
    uncommon on Enclii and can be handled by a caller if needed.
    """
    if not raw:
        return None
    try:
        return float(raw)
    except ValueError:
        return None


def _coerce_params(
    params: Mapping[str, Any] | None,
) -> list[tuple[str, str]] | None:
    """Flatten list values to repeated key=value pairs.

    Required for query params like ``level=error&level=warn`` that the
    log endpoints expect.
    """
    if params is None:
        return None
    flat: list[tuple[str, str]] = []
    for key, value in params.items():
        if value is None:
            continue
        if isinstance(value, (list, tuple)):
            for item in value:
                flat.append((key, str(item)))
        elif isinstance(value, bool):
            flat.append((key, "true" if value else "false"))
        else:
            flat.append((key, str(value)))
    return flat


# ---------------------------------------------------------------------------
# Sync wrapper
# ---------------------------------------------------------------------------


class EncliiClient:
    """Synchronous wrapper around :class:`AsyncEncliiClient`.

    Each sync method call runs its async counterpart in a fresh event
    loop via :func:`asyncio.run`. This is appropriate for **one-shot
    scripts** — CI/CD hooks, cron jobs, CLI tools — and not for
    long-running services. Services should use :class:`AsyncEncliiClient`
    directly to reuse a single connection pool.

    All arguments are forwarded to :class:`AsyncEncliiClient`.
    """

    def __init__(
        self,
        *,
        base_url: str = DEFAULT_BASE_URL,
        token: TokenInput = None,
        token_provider: TokenProvider | None = None,
        timeout: float = DEFAULT_TIMEOUT,
        max_retries: int = DEFAULT_MAX_RETRIES,
        user_agent: str = DEFAULT_USER_AGENT,
    ) -> None:
        self._kwargs: dict[str, Any] = {
            "base_url": base_url,
            "token": token,
            "token_provider": token_provider,
            "timeout": timeout,
            "max_retries": max_retries,
            "user_agent": user_agent,
        }

    def _run(self, method: str, *args: Any, **kwargs: Any) -> Any:
        """Run ``method(*args, **kwargs)`` on a fresh AsyncEncliiClient."""

        async def _do() -> Any:
            async with AsyncEncliiClient(**self._kwargs) as client:
                attr: Any = client
                for part in method.split("."):
                    attr = getattr(attr, part)
                if callable(attr):
                    return await attr(*args, **kwargs)
                return attr

        return asyncio.run(_do())


__all__ = [
    "DEFAULT_BASE_URL",
    "DEFAULT_MAX_RETRIES",
    "DEFAULT_TIMEOUT",
    "AsyncEncliiClient",
    "EncliiClient",
    "__version__",
]
