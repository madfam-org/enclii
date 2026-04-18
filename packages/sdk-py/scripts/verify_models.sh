#!/usr/bin/env bash
# CI drift check: fail if `src/enclii_sdk/models/generated.py` is out of sync
# with the OpenAPI spec. Thin wrapper around generate_models.sh --check.
set -euo pipefail
exec "$(dirname "${BASH_SOURCE[0]}")/generate_models.sh" --check
