"""Typed errors for the Enclii SDK.

All HTTP failures are raised as :class:`EncliiError` or one of its
subclasses. The hierarchy mirrors the Go SDK's error taxonomy so that
code sharing the same error-handling semantics (e.g. workflow retry
policies) maps one-to-one.
"""

from __future__ import annotations

from typing import Any


class EncliiError(Exception):
    """Base class for all Enclii SDK errors.

    Attributes:
        status_code: HTTP status code from the API (0 for local errors).
        message: Human-readable error message from the API ``error`` field.
        details: Optional details from the API ``details`` field.
        hint: Optional troubleshooting hint from the API ``hint`` field.
        request_id: Value of the ``X-Request-Id`` response header if present.
        response_body: Raw response body for debugging.
    """

    def __init__(
        self,
        message: str,
        *,
        status_code: int = 0,
        details: str | None = None,
        hint: str | None = None,
        request_id: str | None = None,
        response_body: Any = None,
    ) -> None:
        super().__init__(message)
        self.message = message
        self.status_code = status_code
        self.details = details
        self.hint = hint
        self.request_id = request_id
        self.response_body = response_body

    def __str__(self) -> str:
        parts = [f"enclii API error {self.status_code}: {self.message}"]
        if self.details:
            parts.append(f"({self.details})")
        if self.hint:
            parts.append(f"hint: {self.hint}")
        if self.request_id:
            parts.append(f"request_id={self.request_id}")
        return " ".join(parts)


class AuthError(EncliiError):
    """401 Unauthorized — invalid or missing API token."""


class PermissionError(EncliiError):
    """403 Forbidden — authenticated but not authorized."""


class NotFoundError(EncliiError):
    """404 Not Found — resource does not exist or is not visible."""

    def __init__(
        self,
        message: str,
        *,
        resource_type: str | None = None,
        resource_id: str | None = None,
        **kwargs: Any,
    ) -> None:
        super().__init__(message, **kwargs)
        self.resource_type = resource_type
        self.resource_id = resource_id


class ConflictError(EncliiError):
    """409 Conflict — request conflicts with current resource state."""


class ValidationError(EncliiError):
    """400/422 — payload failed validation."""


class RateLimitError(EncliiError):
    """429 Too Many Requests — rate limit exceeded.

    Attributes:
        retry_after_seconds: Seconds to wait before retrying, from the
            ``Retry-After`` header if present.
    """

    def __init__(
        self,
        message: str,
        *,
        retry_after_seconds: float | None = None,
        **kwargs: Any,
    ) -> None:
        super().__init__(message, **kwargs)
        self.retry_after_seconds = retry_after_seconds


class ServerError(EncliiError):
    """5xx — server-side error. Retriable per SDK default policy."""


class NetworkError(EncliiError):
    """Transport-level failure (connection reset, DNS, TLS, etc.)."""


class WebhookSignatureError(EncliiError):
    """Webhook HMAC signature verification failed.

    Raised by :func:`enclii_sdk.webhook_verify.verify` when the signature
    header is missing, malformed, the HMAC does not match, or the
    timestamp skew exceeds the tolerance.
    """

    def __init__(self, message: str) -> None:
        super().__init__(message, status_code=0)


__all__ = [
    "AuthError",
    "ConflictError",
    "EncliiError",
    "NetworkError",
    "NotFoundError",
    "PermissionError",
    "RateLimitError",
    "ServerError",
    "ValidationError",
    "WebhookSignatureError",
]
