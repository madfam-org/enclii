"""Closed public contract for held intent, without claiming runtime authority."""

from pathlib import Path

import pytest
import yaml
from pydantic import ValidationError

from enclii_sdk.models.generated import (
    JanuaStagingPreregistrationGate,
    JanuaStagingPreregistrationIntent,
    JanuaStagingPreregistrationPlanRequest,
    JanuaStagingPreregistrationRecordRequest,
)

OPERATION = "11111111-1111-4111-8111-111111111111"


def intent() -> dict[str, object]:
    return {
        "id": OPERATION,
        "project_id": "22222222-2222-4222-8222-222222222222",
        "environment_id": "33333333-3333-4333-8333-333333333333",
        "repository_registration_id": "44444444-4444-4444-8444-444444444444",
        "repository_registration_scope": "REPOSITORY_OWNERSHIP_ONLY",
        "state": "HELD_PREREGISTRATION_ONLY",
        "profile": "janua-staging-preregistration-v1",
        "proposed_service_name": "janua-api-staging",
        "proposed_deployment_name": "janua-api-staging",
        "proposed_repository": "https://github.com/madfam-org/janua.git",
        "proposed_app_path": "apps/api",
        "proposed_namespace": "janua-staging",
        "proposed_manifest_path": "k8s/overlays/staging",
        "proposed_ref": "main",
        "activation_held": True,
        "service_materialized": False,
        "native_binding_proven": False,
        "materialization_supported": False,
        "recorded": False,
        "plan_sha256": "a" * 64,
        "unresolved_gates": [g.value for g in JanuaStagingPreregistrationGate],
    }


def test_generated_closed_plan_record_and_receipt_round_trip() -> None:
    plan = {"operation_id": OPERATION, "record": False}
    assert (
        JanuaStagingPreregistrationPlanRequest.model_validate(plan).operation_id.root == OPERATION
    )
    record = {**plan, "record": True, "expect_plan_sha256": "a" * 64, "reason": "bounded"}
    assert (
        JanuaStagingPreregistrationRecordRequest.model_validate(record).expect_plan_sha256
        == "a" * 64
    )
    for receipt in [
        intent(),
        {**intent(), "recorded": True, "recorded_at": "2026-10-20T12:00:00Z"},
    ]:
        value = JanuaStagingPreregistrationIntent.model_validate(receipt)
        assert value.model_dump(mode="json")["activation_held"] is True
        assert value.repository_registration_scope.value == "REPOSITORY_OWNERSHIP_ONLY"
        assert "SOURCE_STAGE_REGISTRATION_NOT_PROVEN" in [g.value for g in value.unresolved_gates]


@pytest.mark.parametrize(
    "mutation",
    [
        {"activation_held": False},
        {"service_materialized": True},
        {"native_binding_proven": True},
        {"materialization_supported": True},
        {"repository_registration_scope": "STAGING_SOURCE_VERIFIED"},
        {"proposed_namespace": "production"},
        {"unresolved_gates": ["QUALIFIED"] * 6},
        {"unresolved_gates": ["SOURCE_STAGE_REGISTRATION_NOT_PROVEN"]},
        {"plan_sha256": "unverified"},
        {"recorded_at": "2026-10-20T12:00:00+00:00"},
        {"private_reason": "not a public field"},
    ],
)
def test_generated_receipt_refuses_activation_or_unclosed_fields(
    mutation: dict[str, object],
) -> None:
    with pytest.raises(ValidationError):
        JanuaStagingPreregistrationIntent.model_validate({**intent(), **mutation})


def test_generated_requests_refuse_arbitrary_selectors_and_missing_comparison() -> None:
    for payload in [
        {"operation_id": OPERATION, "record": False, "namespace": "arbitrary"},
        {"operation_id": OPERATION, "record": True},
        {"operation_id": OPERATION},
    ]:
        with pytest.raises(ValidationError):
            JanuaStagingPreregistrationPlanRequest.model_validate(payload)
    with pytest.raises(ValidationError):
        JanuaStagingPreregistrationRecordRequest.model_validate(
            {"operation_id": OPERATION, "record": True, "reason": "bounded"}
        )


def test_canonical_routes_keep_closed_modes_and_all_unresolved_gates() -> None:
    spec = yaml.safe_load((Path(__file__).parents[3] / "docs/api/openapi.yaml").read_text())
    path = "/admin/projects/{slug}/environments/staging/preregistration-intents"
    post = spec["paths"][path]["post"]
    read = spec["paths"][path + "/{operation_id}"]["get"]
    assert post["operationId"] == "planOrRecordJanuaStagingPreregistrationIntent"
    assert read["operationId"] == "readJanuaStagingPreregistrationIntent"
    assert len(post["requestBody"]["content"]["application/json"]["schema"]["oneOf"]) == 2
    assert "requestBody" not in read
    schemas = spec["components"]["schemas"]
    for name in ["PlanRequest", "RecordRequest", "Intent"]:
        assert schemas["JanuaStagingPreregistration" + name]["additionalProperties"] is False
    receipt = schemas["JanuaStagingPreregistrationIntent"]
    gates = receipt["properties"]["unresolved_gates"]
    assert gates["minItems"] == gates["maxItems"] == 6 and gates["uniqueItems"] is True
    assert set(schemas["JanuaStagingPreregistrationGate"]["enum"]) == {
        g.value for g in JanuaStagingPreregistrationGate
    }
    assert "SOURCE_STAGE_REGISTRATION_NOT_PROVEN" in post["description"]
