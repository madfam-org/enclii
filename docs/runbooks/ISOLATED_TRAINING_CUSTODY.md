# Isolated training and staging custody

> Boundary checkpoint (2026-10-13, platform preparation): public-safe logical
> target names, namespaces, key names and callbacks only. Actual credentials,
> human/organization identifiers and operational receipts stay in the private
> operator workspace and `internal-devops`. Policy:
> [Public repository boundary](../PUBLIC_REPO_BOUNDARY.md).

Source declarations establish allowed targets, not deployed credentials or a
working training environment. Verify the live target catalog before creating a
client whose secret is returned only once. Use the governed Enclii intake and
OIDC provisioner; never print secrets, copy production material into staging or
write provider/cluster secrets outside those adapters.

## Human training clients

The two embedded registry copies declare confidential authorization-code/PKCE
clients with rotating refresh:

| Logical name/audience | Exact callback | Intake target |
| --- | --- | --- |
| `crea-sim` | `https://sim.creatumundo.mx/auth/callback` | `crea-sim/ui-oidc` |
| `crea-sim-mcp` | `https://sim.creatumundo.mx/auth/mcp/callback` | `crea-sim/mcp-oidc` |

Registration produces the actual `jnc_*` identifier. A logical name is never a
substitute for that identifier. Both declarations initially request only
`openid`, `profile`, `email`, and `offline_access`, which the existing deployed
registration contract accepts. The manual UI pilot keeps MCP/chat disabled.
Reconcile the same MCP client with training-only resource scopes through a
separate reviewed policy release after actual isolated Janua staging and normal
promotion; this declaration grants neither production MAP scopes nor training
write scopes.

An invited human must prove fresh signed active membership in the operator's
exact configured CTM organization. No email match, service account or client
registration creates human membership or a production clinical role. A future
organization-bound Selva producer uses independent machine credentials and its
sole approved inference scope; the human UI/MCP credentials do not travel to
the inference service.

## Closed intake targets

| Targets | Vault path | ExternalSecret |
| --- | --- | --- |
| `crea-sim/ui-oidc`, `crea-sim/mcp-oidc`, `crea-sim/gateway-runtime` | `secret/crea-sim-gateway` | `crea-sim-gateway` |
| `crea-sim/gateway-ca` | `secret/crea-sim-gateway` | `crea-sim-gateway-ca` |
| `crea-sim/capture-runtime` | `secret/crea-sim-captura` | `crea-sim-captura` |
| `crea-sim/capture-ca` | `secret/crea-sim-captura` | `crea-sim-captura-ca` |
| `crea-sim/kernel01-runtime` | `secret/crea-sim-map01` | `crea-sim-map01` |
| `crea-sim/kernel01-ca` | `secret/crea-sim-map01` | `crea-sim-map01-ca` |
| `crea-sim/selva-oidc` | `secret/crea-sim-selva` | `crea-sim-selva` |
| `janua-staging/runtime-crypto`, `janua-staging/database` | `secret/janua-staging` | `janua-staging-secrets` |
| `janua-staging/database-ca` | `secret/janua-staging` | `janua-staging-ca` |
| `janua-staging/redis` | `secret/janua-staging` | `janua-staging-redis` |

The registry specifies each target's exact key allowlist and namespace. CA
targets accept the verified public certificate bundle, never its private key.
Runtime database targets carry only the managed nonowner URI for the dedicated
synthetic database. Migrator credentials stay in the separately owned CNPG
Secret and never enter an application intake target.

Generate fresh material in the approved private custody flow with the exact
consumer format. The generic intake generator emits unpadded base64url; it is
not a 64-character hex sealing/HMAC key, an RSA PEM pair or a padded Fernet key.
Do not use that default generator for incompatible fields. The context,
control, mail and session keys are distinct by purpose; only the intended two
ends receive each shared key. Registration, invitation and message delivery
are separate operations; this runbook does not send invitations or mail.

## Bounded staging namespace bootstrap

`POST /v1/admin/projects/:slug/environments/staging/bootstrap` defaults to
`dry_run: true`. It uses the existing platform-rank and project/acting-tenant
guards, preserving the documented tenant-scope rollback policy. It requires a
persisted staging environment, the exact `<slug>-staging` namespace and a
completed same-project repository onboarding registration. Apply requires a
reason.

The helper prepares only the namespace and centrally owned DNS-only
default-deny. Existing namespaces and policies need the exact project identity;
untagged or foreign objects are refused before writes. Dry runs do not create
or label objects. Fresh creation reuses the UID-checked onboarding rollback if
default-deny fails. It does not retarget production Argo, create workloads,
register routes, grant identities or transfer credentials.

The existing ApplicationSet reads `infra/argocd/projects/*/config.json` from
reviewed main. A separate staging declaration must be reviewed only after
custody, addon binding, resource/storage capacity, image admission, managed
migration and exact network rules are proven. There is no staging declaration
or live activation in this change. A pin timestamp cannot prove a healthy soak.

See [Secret intake](SECRET_INTAKE.md),
[namespace and onboarding](../guides/ONBOARDING_GUIDE.md), and the managed
database isolation contract. Fresh seed/migration needs a separately reviewed
fixed-profile owner-only adapter; arbitrary one-off command or SQL execution
does not satisfy that contract.
