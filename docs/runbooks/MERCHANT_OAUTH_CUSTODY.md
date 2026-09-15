# Merchant OAuth custody

Nauta and MAP read Dhanam's native monthly payment projection through separate
Janua confidential clients. Each has only `client_credentials`, audience
`dhanam-api`, scope `dhanam:merchant-read`, and a reviewed organization binding.
Dhanam's native `PaymentProjectionGrant` independently binds that client and
organization to one merchant Space. Neither client can create charges through
this projection.

| Intake target | Vault path | Namespace / isolated ExternalSecret |
| --- | --- | --- |
| `nauta/dhanam-merchant-oauth` | `secret/nauta` | `nauta` / `nauta-merchant-oauth` |
| `crea-map/dhanam-merchant-oauth` | `secret/crea-map` | `crea-map` / `crea-map-merchant-oauth` |

Both targets require the same five lowercase properties:
`dhanam_merchant_client_id`, `dhanam_merchant_client_secret`,
`dhanam_merchant_workspace_id`, `dhanam_merchant_organization_id`, and
`dhanam_merchant_space_id`. The existing Vault writer policy already covers both
consumer paths; no additional privilege or cross-consumer path is introduced.
Intake merges these properties with existing application secrets.

## Protected configuration and provisioning

Keep the OIDC override outside Git. Give each consumer a distinct stable client
name. Its `intake_key_map` maps the first two properties to `client_id` and
`client_secret`, and the remaining three to the exact source-backed identifiers.
The client `organization_id` must match the mapped organization. `client_key`
is Janua's audience alias, so it is `dhanam-api`, not a consumer identity. Once
created, pin the returned public client ID in the protected override.

Inspect the native client by exact name/pin before applying. A client with the
same audience is not the same consumer. The corrected CLI refuses identity and
privilege drift rather than rotating an unrelated client. Do not use `intake
--generate`: random strings are not Janua OAuth credentials or source bindings.

After the server revision containing both targets is ready:

```sh
enclii secrets intake targets --json
enclii secrets provision oidc --registry <protected-registry.json> \
  --platform <consumer-platform-id> --reason "Review merchant read custody" \
  --dry-run --rotate-secret=false --json
```

The dry-run is local and performs no Janua/Vault mutation. Review its target and
five keys, plus the protected source identifiers and native client presence.
For a reviewed new client, apply by omitting `--dry-run`:

```sh
enclii secrets provision oidc --registry <protected-registry.json> \
  --platform <consumer-platform-id> --reason "Provision reviewed merchant read custody" \
  --rotate-secret=false --json
enclii secrets intake status <intake-id>
```

Run each consumer separately. Do not use `--all` or automatic secret rotation.
A pre-existing client with no retrievable secret will refuse this command;
inspect the existing custody before any explicitly reviewed rotation. If intake
fails after client creation, inspect that exact client and failed intake before
retrying. Never create a second client to conceal a partial result.

## Native grant and activation

1. Use Dhanam's source-backed native grant command for the exact newly issued
   client, organization, Space, source workspace and actual operator identity.
   Keep its config and receipt protected. No payment materialization follows.
2. Verify masked custody presence and successful intake. Only then enable each
   consumer's isolated ExternalSecret in its own deployment manifests. A missing
   property must not abort unrelated database/auth secret projection.
3. Nauta resolves its own reserved merchant ExternalRef and current organization;
   MAP resolves its own reviewed workspace binding. Both compare the native
   response's Space, organization and requested period before displaying data.
4. Verify a real authorized monthly read, a genuine empty month, and refusal for
   an unauthorized actor. A missing grant, failed fetch or missing source remains
   an explicit warning/error, never a fabricated zero balance.

No credential value belongs in a CLI argument, PR, log or browser payload.
Presence of these intake targets alone is not evidence of live client creation,
ESO synchronization, native grant application or payment completeness.

## Validation

```sh
(cd apps/switchyard-api && go test ./internal/secretsintake)
bash scripts/check-intake-policy-parity.sh
```
