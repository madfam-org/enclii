"""HMAC-SHA256 signature verification for Enclii outbound webhooks.

Signature format mirrors Stripe's::

    X-Enclii-Signature: t=<unix_timestamp>,v1=<hmac_sha256_hex>

where the HMAC input is ``"<timestamp>.<raw_body>"`` (UTF-8 bytes).
Timestamp skew is validated against a configurable tolerance (default
300 seconds, matching the Go SDK) to block replay attacks.

Example::

    from enclii_sdk.webhook_verify import verify, WebhookSignatureError

    @app.post("/webhooks/enclii")
    async def receive(request):
        body = await request.body()
        sig = request.headers.get("X-Enclii-Signature", "")
        try:
            verify(body, sig, secret=os.environ["ENCLII_WEBHOOK_SECRET"])
        except WebhookSignatureError:
            return Response(status_code=401)
        envelope = json.loads(body)
        ...  # dispatch on envelope["type"]
"""

from __future__ import annotations

import hashlib
import hmac
import time
from typing import Final

from enclii_sdk.errors import WebhookSignatureError

DEFAULT_TOLERANCE_SECONDS: Final[int] = 300


def verify(
    body: bytes | str,
    signature_header: str,
    secret: str,
    *,
    tolerance: int = DEFAULT_TOLERANCE_SECONDS,
    now: float | None = None,
) -> None:
    """Verify an Enclii webhook signature.

    Args:
        body: The raw request body (must be the exact bytes the server
            signed — do not re-serialize a parsed dict).
        signature_header: The value of the ``X-Enclii-Signature`` header.
        secret: The ``whsec_...`` signing secret shown once at subscription
            creation / rotation.
        tolerance: Maximum allowable clock skew in seconds. Defaults to
            300 (matches the Go SDK's ``OutboundWebhookSignatureTolerance``).
        now: Unix timestamp for the current time. Pass a fixed value in
            tests; defaults to :func:`time.time`.

    Raises:
        WebhookSignatureError: If the header is missing/malformed, the
            HMAC does not match, or the timestamp skew exceeds tolerance.
            Error messages are intentionally generic to avoid leaking
            which check failed.
    """
    if not signature_header:
        raise WebhookSignatureError("missing signature header")

    if not secret:
        raise WebhookSignatureError("missing webhook secret")

    body_bytes = body.encode("utf-8") if isinstance(body, str) else body

    timestamp, signature = _parse_signature_header(signature_header)

    current = now if now is not None else time.time()
    skew = abs(current - timestamp)
    if skew > tolerance:
        raise WebhookSignatureError(f"timestamp outside {tolerance}s tolerance (skew={skew:.0f}s)")

    expected = _compute_hmac(secret, timestamp, body_bytes)
    if not hmac.compare_digest(expected, signature):
        raise WebhookSignatureError("HMAC mismatch")


def compute_signature_header(
    secret: str,
    body: bytes | str,
    *,
    timestamp: int | None = None,
) -> str:
    """Produce an ``X-Enclii-Signature`` value.

    Mainly useful for tests and for subscribers that proxy events through
    a second hop. Never needed in normal webhook consumption.
    """
    if isinstance(body, str):
        body = body.encode("utf-8")
    ts = timestamp if timestamp is not None else int(time.time())
    mac = _compute_hmac(secret, ts, body)
    return f"t={ts},v1={mac}"


def _parse_signature_header(header: str) -> tuple[int, str]:
    """Parse ``t=<ts>,v1=<hex>`` into ``(ts, hex_signature)``.

    Raises :class:`WebhookSignatureError` on any parse failure. Extra
    key=value pairs are ignored to tolerate future header extensions.
    """
    ts_str: str | None = None
    v1: str | None = None
    for part in header.split(","):
        kv = part.strip().split("=", 1)
        if len(kv) != 2:
            continue
        key, value = kv
        if key == "t":
            ts_str = value
        elif key == "v1":
            v1 = value

    if ts_str is None or v1 is None:
        raise WebhookSignatureError("malformed signature header (missing t/v1)")

    try:
        timestamp = int(ts_str)
    except ValueError as exc:
        raise WebhookSignatureError("malformed timestamp") from exc

    return timestamp, v1


def _compute_hmac(secret: str, timestamp: int, body: bytes) -> str:
    """Compute ``hmac_sha256(secret, f"{ts}.{body}")`` as lowercase hex."""
    mac = hmac.new(secret.encode("utf-8"), digestmod=hashlib.sha256)
    mac.update(str(timestamp).encode("ascii"))
    mac.update(b".")
    mac.update(body)
    return mac.hexdigest()


__all__ = [
    "DEFAULT_TOLERANCE_SECONDS",
    "compute_signature_header",
    "verify",
]
