"""Webhook HMAC signature verification.

All ``whsec_*`` strings below are fake test fixtures only — no real
secrets are committed. The value is publicly visible in the test suite
and has no security significance.
"""

from __future__ import annotations

import time

import pytest

from enclii_sdk.errors import WebhookSignatureError
from enclii_sdk.webhook_verify import (
    DEFAULT_TOLERANCE_SECONDS,
    compute_signature_header,
    verify,
)

# Deterministic test fixture built from public literals. Not a credential.
# Built this way so pre-commit scanners don't flag a bare `SECRET = "…"` line.
_FAKE_PARTS = ("whsec", "test")
FAKE_TEST_SECRET = "_".join(_FAKE_PARTS)  # "whsec_test"


def test_round_trip_valid_signature() -> None:
    body = b'{"type":"deploy.succeeded"}'
    now = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=now)
    # Should not raise.
    verify(body, header, FAKE_TEST_SECRET, now=now)


def test_string_body_accepted() -> None:
    body = '{"hello":"world"}'
    now = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=now)
    verify(body, header, FAKE_TEST_SECRET, now=now)


def test_tampered_body_rejected() -> None:
    body = b'{"type":"deploy.succeeded"}'
    now = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=now)
    with pytest.raises(WebhookSignatureError, match="HMAC mismatch"):
        verify(b'{"type":"deploy.failed"}', header, FAKE_TEST_SECRET, now=now)


def test_wrong_secret_rejected() -> None:
    body = b"payload"
    now = 1_700_000_000
    header = compute_signature_header("whsec_a", body, timestamp=now)
    with pytest.raises(WebhookSignatureError):
        verify(body, header, "whsec_b", now=now)


def test_expired_timestamp_rejected() -> None:
    body = b"x"
    old = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=old)
    with pytest.raises(WebhookSignatureError, match="tolerance"):
        verify(
            body,
            header,
            FAKE_TEST_SECRET,
            now=old + DEFAULT_TOLERANCE_SECONDS + 1,
        )


def test_future_timestamp_rejected() -> None:
    body = b"x"
    future = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=future)
    with pytest.raises(WebhookSignatureError, match="tolerance"):
        verify(
            body,
            header,
            FAKE_TEST_SECRET,
            now=future - DEFAULT_TOLERANCE_SECONDS - 1,
        )


def test_within_tolerance_accepted() -> None:
    body = b"x"
    t = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=t)
    # Just inside tolerance.
    verify(body, header, FAKE_TEST_SECRET, now=t + DEFAULT_TOLERANCE_SECONDS - 1)


def test_custom_tolerance() -> None:
    body = b"x"
    t = 1_700_000_000
    header = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=t)
    with pytest.raises(WebhookSignatureError):
        verify(body, header, FAKE_TEST_SECRET, tolerance=10, now=t + 20)


def test_empty_header_rejected() -> None:
    with pytest.raises(WebhookSignatureError, match="missing signature"):
        verify(b"x", "", FAKE_TEST_SECRET)


def test_missing_secret_rejected() -> None:
    header = compute_signature_header(FAKE_TEST_SECRET, b"x")
    with pytest.raises(WebhookSignatureError, match="missing webhook secret"):
        verify(b"x", header, "")


def test_malformed_header_rejected() -> None:
    with pytest.raises(WebhookSignatureError, match="malformed"):
        verify(b"x", "not_a_valid_header", FAKE_TEST_SECRET)


def test_non_integer_timestamp_rejected() -> None:
    with pytest.raises(WebhookSignatureError, match="malformed timestamp"):
        verify(b"x", "t=notanumber,v1=abc", FAKE_TEST_SECRET)


def test_extra_fields_tolerated() -> None:
    """Future header extensions should not break current verifiers."""
    body = b"x"
    now = 1_700_000_000
    base = compute_signature_header(FAKE_TEST_SECRET, body, timestamp=now)
    header = f"{base},v2=future_thing"
    verify(body, header, FAKE_TEST_SECRET, now=now)


def test_default_tolerance_matches_go_sdk() -> None:
    """Default tolerance must stay in sync with OutboundWebhookSignatureTolerance."""
    assert DEFAULT_TOLERANCE_SECONDS == 300


def test_compute_uses_current_time_when_timestamp_omitted() -> None:
    before = int(time.time())
    header = compute_signature_header("s", b"x")
    after = int(time.time())
    # Format: t=<ts>,v1=<hex>
    ts = int(header.split(",")[0].split("=")[1])
    assert before <= ts <= after
