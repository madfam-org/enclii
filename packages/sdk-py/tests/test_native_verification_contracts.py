"""Exercise the generated closed contracts with accepted and refused wire data."""

from copy import deepcopy

import pytest
from pydantic import ValidationError

from enclii_sdk.models.generated import (
    JanuaStagingAdmissionReceipt,
    PortalGenerationReadReceipt,
    PortalGenerationReadRequest,
)

SERVICE = "11000000-0000-4000-8000-000000000001"
RESERVATION = "11000000-0000-4000-8000-000000000002"
OPERATION = "11000000-0000-4000-8000-000000000003"


def read_request() -> dict:
    return {
        "profile": "portal-sim-generation-read",
        "service_id": SERVICE,
        "pedido": {
            "version": "crea-sim-generation-read-v1",
            "reservaId": RESERVATION,
            "nucleoId": "crea-sim-map01",
        },
        "dry_run": True,
        "idempotency_key": OPERATION,
    }


def test_generated_read_request_accepts_dry_run_and_explicit_apply() -> None:
    dry = read_request()
    PortalGenerationReadRequest.model_validate(dry)
    apply = {**dry, "dry_run": False, "expect_plan_sha256": "a" * 64, "reason": "observe"}
    PortalGenerationReadRequest.model_validate(apply)


@pytest.mark.parametrize(
    "change",
    [
        {"dry_run": False},
        {"dry_run": True, "reason": "cannot authorize in dry run"},
        {"command": ["unreviewed"]},
        {"image": "unreviewed"},
        {"service_id": "00000000-0000-0000-0000-000000000000"},
        {"idempotency_key": "not-a-uuid"},
    ],
)
def test_generated_read_request_refuses_missing_confirmation_or_extra_authority(
    change: dict,
) -> None:
    with pytest.raises(ValidationError):
        PortalGenerationReadRequest.model_validate({**read_request(), **change})


@pytest.mark.parametrize("reason", [" ", "\n", "observe\x00", "x" * 513])
def test_generated_apply_request_refuses_invalid_reason(reason: str) -> None:
    with pytest.raises(ValidationError):
        PortalGenerationReadRequest.model_validate(
            {**read_request(), "dry_run": False, "expect_plan_sha256": "a" * 64, "reason": reason}
        )


def generation_receipt() -> dict:
    schema_enum = PortalGenerationReadReceipt.model_fields["schema_sha256"].annotation
    return {
        "fuente": "portal-sim-retained-generation",
        "version": "crea-sim-generation-read-v1",
        "reservaId": RESERVATION,
        "nucleoId": "crea-sim-map01",
        "baseEsperada": "crea_sim_kernel01_sim",
        "propietarioSha256": "a" * 64,
        "integranteSha256": "b" * 64,
        "rol": "DIRECTOR",
        "escenario": "director-primer-turno",
        "faseObservada": "ACTIVA",
        "revisionVacia": "9007199254740993",
        "epocaVacia": "0",
        "manifiestoSha256": "c" * 64,
        "huellaSemilla": "d" * 64,
        "codigoSha256": "e" * 64,
        "fixtureSha256": "f" * 64,
        "ahoraInicial": "2026-01-01T00:00:00.000Z",
        "schemaSha256": next(iter(schema_enum)).value,
        "databaseIdentitySha256": "a" * 64,
        "adoptionAuthorized": False,
    }


def test_generated_historical_receipt_preserves_exact_counters_and_refuses_authority() -> None:
    wire = generation_receipt()
    receipt = PortalGenerationReadReceipt.model_validate(wire)
    assert receipt.revision_vacia == "9007199254740993"
    assert receipt.ahora_inicial == "2026-01-01T00:00:00.000Z"
    for change in [
        {"adoptionAuthorized": True},
        {"owner_uri": "unreviewed"},
        {"faseObservada": "READY"},
        {"revisionVacia": "00"},
    ]:
        with pytest.raises(ValidationError):
            PortalGenerationReadReceipt.model_validate({**wire, **change})
    del wire["adoptionAuthorized"]
    with pytest.raises(ValidationError):
        PortalGenerationReadReceipt.model_validate(wire)


def test_generated_staging_receipt_cannot_turn_dry_admission_into_apply() -> None:
    image_enum = JanuaStagingAdmissionReceipt.model_fields["reviewed_image"].annotation
    gates_enum = JanuaStagingAdmissionReceipt.model_fields["unresolved_gates"].annotation.__args__[
        0
    ]
    wire = {
        "classification": "ADMITTED_DRY_RUN_ONLY",
        "dry_run_only": True,
        "apply_supported": False,
        "profile": "janua-api-staging-v1",
        "reviewed_image": next(iter(image_enum)).value,
        "project_slug": "janua",
        "environment_name": "staging",
        "project_id": RESERVATION,
        "environment_id": OPERATION,
        "service_id": SERVICE,
        "catalog_plan": "standard-0",
        "objects_dry_admitted": 4,
        "exact_api_destination_pairs": 2,
        "observation_fingerprint": "sha256:" + "a" * 64,
        "observed_at": "2026-01-01T00:00:00Z",
        "unresolved_gates": [gate.value for gate in gates_enum],
    }
    JanuaStagingAdmissionReceipt.model_validate(wire)
    for change in [
        {"apply_supported": True},
        {"dry_run_only": False},
        {"objects_dry_admitted": 3},
        {"credential": "unreviewed"},
        {"unresolved_gates": wire["unresolved_gates"][:-1]},
        {"unresolved_gates": ["UNREVIEWED"] * 7},
    ]:
        with pytest.raises(ValidationError):
            JanuaStagingAdmissionReceipt.model_validate({**deepcopy(wire), **change})
