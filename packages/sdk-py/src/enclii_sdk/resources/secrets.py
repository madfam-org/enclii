"""Environment variable / secret management."""

from __future__ import annotations

from enclii_sdk.models.secrets import (
    CreateEnvVarRequest,
    EnvVarResponse,
    UpdateEnvVarRequest,
)
from enclii_sdk.resources._base import Resource


class SecretsResource(Resource):
    """CRUD for environment variables (both plaintext and secret)."""

    async def list(self, service_id: str) -> list[EnvVarResponse]:
        """List env vars for a service.

        API: ``GET /v1/services/{id}/env-vars``. Secret values are masked.
        """
        data = await self._client.get(f"/v1/services/{service_id}/env-vars")
        items = data if isinstance(data, list) else data.get("env_vars", [])
        return [EnvVarResponse.model_validate(v) for v in items]

    async def get(self, service_id: str, var_id: str) -> EnvVarResponse:
        """Fetch a single env var.

        API: ``GET /v1/services/{id}/env-vars/{var_id}``.
        """
        data = await self._client.get(f"/v1/services/{service_id}/env-vars/{var_id}")
        return EnvVarResponse.model_validate(data)

    async def create(
        self,
        service_id: str,
        *,
        key: str,
        value: str,
        is_secret: bool = False,
        environment_id: str | None = None,
    ) -> EnvVarResponse:
        """Create an env var. API: ``POST /v1/services/{id}/env-vars``."""
        request = CreateEnvVarRequest(
            key=key,
            value=value,
            is_secret=is_secret,
            environment_id=environment_id,
        )
        data = await self._client.post(
            f"/v1/services/{service_id}/env-vars",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return EnvVarResponse.model_validate(data)

    async def update(
        self,
        service_id: str,
        var_id: str,
        *,
        value: str | None = None,
        is_secret: bool | None = None,
    ) -> EnvVarResponse:
        """Update an env var. API: ``PUT /v1/services/{id}/env-vars/{var_id}``."""
        request = UpdateEnvVarRequest(value=value, is_secret=is_secret)
        data = await self._client.put(
            f"/v1/services/{service_id}/env-vars/{var_id}",
            json_body=request.model_dump(mode="json", exclude_none=True),
        )
        return EnvVarResponse.model_validate(data)

    async def delete(self, service_id: str, var_id: str) -> None:
        """Delete an env var. API: ``DELETE /v1/services/{id}/env-vars/{var_id}``."""
        await self._client.delete(f"/v1/services/{service_id}/env-vars/{var_id}")

    async def reveal(self, service_id: str, var_id: str) -> EnvVarResponse:
        """Reveal a secret's value (audit-logged).

        API: ``POST /v1/services/{id}/env-vars/{var_id}/reveal``. Requires
        ``developer``. Every call produces an audit entry.
        """
        data = await self._client.post(f"/v1/services/{service_id}/env-vars/{var_id}/reveal")
        return EnvVarResponse.model_validate(data)
