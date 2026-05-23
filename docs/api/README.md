# API specification (canonical path)

The OpenAPI source of truth lives at [`openapi.yaml`](./openapi.yaml), a symlink to
[`../api-reference/openapi.yaml`](../api-reference/openapi.yaml).

Tooling (`packages/sdk-ts`, `packages/sdk-py`, CI drift checks) reads **`docs/api/openapi.yaml`**.
Edit the spec in `docs/api-reference/openapi.yaml` (or update both paths together if the symlink
is replaced).

Regenerate TypeScript types:

```bash
pnpm -F @madfam/enclii-sdk run generate-types
pnpm -F @madfam/enclii-sdk run verify-types
```
