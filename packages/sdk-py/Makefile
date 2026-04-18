# Enclii Python SDK — developer targets.
# Requires uv (https://docs.astral.sh/uv/).

.PHONY: help install test lint format typecheck models verify-models build publish clean

help:
	@echo "Targets:"
	@echo "  install         install package + dev deps via uv"
	@echo "  test            run pytest"
	@echo "  lint            ruff check"
	@echo "  format          ruff format"
	@echo "  typecheck       mypy"
	@echo "  models          regenerate models/generated.py from OpenAPI"
	@echo "  verify-models   fail if generated models drift from OpenAPI (CI)"
	@echo "  build           build sdist + wheel via uv build"
	@echo "  publish         uv publish (requires PYPI_TOKEN env)"
	@echo "  clean           remove build artefacts and caches"

install:
	uv sync --extra dev

test:
	uv run pytest

lint:
	uv run ruff check src tests

format:
	uv run ruff format src tests

typecheck:
	uv run mypy src

models:
	./scripts/generate_models.sh

verify-models:
	./scripts/verify_models.sh

build:
	uv build

publish: build
	@test -n "$$PYPI_TOKEN" || { echo "error: PYPI_TOKEN not set"; exit 1; }
	uv publish --token "$$PYPI_TOKEN"

clean:
	rm -rf dist build *.egg-info .pytest_cache .mypy_cache .ruff_cache
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
