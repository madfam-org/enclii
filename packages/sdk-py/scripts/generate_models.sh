#!/usr/bin/env bash
# Regenerate pydantic models from the Enclii OpenAPI spec.
#
# Source of truth: docs/api/openapi.yaml (at the repo root, two dirs up).
#
# The generated file (`src/enclii_sdk/models/generated.py`) is checked in and
# intended as a companion/reference to the hand-written models in the sibling
# modules (`core.py`, `webhooks.py`, `canary.py`, etc.). The hand-written
# models track Go SDK semantics (pkg/types) and are the consumer-facing API;
# the generated file exists so consumers who want to work directly against
# the raw spec shape can do so, and so CI can detect drift.
#
# Usage:
#   scripts/generate_models.sh          # regenerate in-place
#   scripts/generate_models.sh --check  # fail if regeneration would change file (CI drift)
set -euo pipefail

SDK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$SDK_DIR/../.." && pwd)"
SPEC="$REPO_ROOT/docs/api/openapi.yaml"
OUT="$SDK_DIR/src/enclii_sdk/models/generated.py"

if [ ! -f "$SPEC" ]; then
    echo "error: openapi spec not found at $SPEC" >&2
    exit 1
fi

CHECK=${1:-}

generate() {
    local target="$1"
    (cd "$SDK_DIR" && uv run --extra dev datamodel-codegen \
        --input "$SPEC" \
        --input-file-type openapi \
        --output-model-type pydantic_v2.BaseModel \
        --output "$target" \
        --target-python-version 3.11 \
        --use-standard-collections \
        --use-union-operator \
        --field-constraints \
        --snake-case-field \
        --use-schema-description 2>&1 | grep -v "FutureWarning\|formatters=\[\|return CodeFormatter" || true)
    # Apply ruff formatting so the file matches the rest of the SDK's style
    # (datamodel-code-generator emits black-style single quotes today, ruff
    # prefers double — running ruff here keeps verify_models.sh stable).
    (cd "$SDK_DIR" && uv run ruff format "$target" >/dev/null 2>&1 || true)
}

if [ "$CHECK" = "--check" ]; then
    TMP=$(mktemp)
    trap 'rm -f "$TMP"' EXIT
    generate "$TMP"
    # Strip the generator timestamp line before comparing — it changes every run.
    if ! diff -u \
        <(grep -v '^#   timestamp:' "$OUT") \
        <(grep -v '^#   timestamp:' "$TMP"); then
        echo ""
        echo "error: generated models have drifted from the OpenAPI spec." >&2
        echo "run: scripts/generate_models.sh" >&2
        exit 1
    fi
    echo "ok: generated models match the OpenAPI spec"
else
    generate "$OUT"
    echo "ok: regenerated $OUT"
fi
