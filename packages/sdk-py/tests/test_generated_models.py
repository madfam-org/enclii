"""Smoke test for the OpenAPI-generated pydantic models.

The generated file (``enclii_sdk.models.generated``) is checked in and
supposed to be kept in sync with ``docs/api/openapi.yaml`` via
``scripts/verify_models.sh`` in CI. These tests guard the import surface
so a broken generator run (e.g. missing optional dep) fails fast in unit
tests, not at consumer install time.
"""

from __future__ import annotations

import pytest


def test_generated_module_imports() -> None:
    """The generated module must import without pulling missing deps."""
    from enclii_sdk.models import generated  # noqa: F401


def test_generated_health_response_round_trip() -> None:
    """A representative generated model serialises and parses cleanly."""
    from enclii_sdk.models.generated import HealthResponse

    model = HealthResponse(status="ok", version="1.0.0", uptime=42)
    dumped = model.model_dump()
    assert dumped["status"] == "ok"
    assert HealthResponse.model_validate(dumped).uptime == 42


def test_generated_is_not_reexported_from_top_level() -> None:
    """Generated models are deliberately not on the public top-level API.

    Consumers use the hand-written models (Project, Deployment, etc.).
    The generated module is a reference/drift-detection artefact.
    """
    import enclii_sdk

    assert not hasattr(enclii_sdk, "HealthResponse"), (
        "generated.HealthResponse leaked into the top-level namespace"
    )


def test_generated_login_request_validates_email() -> None:
    """EmailStr constraint from the spec is enforced."""
    from pydantic import ValidationError

    from enclii_sdk.models.generated import LoginRequest

    # Dummy test value (>=8 chars per spec), not a credential.
    fake_pwd = "x" * 10
    LoginRequest(email="ok@example.com", password=fake_pwd)
    with pytest.raises(ValidationError):
        LoginRequest(email="not-an-email", password=fake_pwd)
