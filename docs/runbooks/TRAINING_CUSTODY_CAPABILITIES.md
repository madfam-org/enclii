# Training custody capability proof

Last Updated: 2026-10-13

> **Boundary checkpoint (2026-10-13, platform engineering):** Public-safe
> mechanism and capability contracts only. Private custody identities, values
> and operational receipts remain in internal-devops. Policy:
> [PUBLIC_REPO_BOUNDARY.md](../PUBLIC_REPO_BOUNDARY.md).

GET `/v1/admin/secrets/training-custody-capabilities` requires the real
server-resolved platform operator rank, including when the tenant-scope
rollback flag is false. It accepts no query or body. The optional Vault probe
uses the existing scoped writer identity once, with a ten-second maximum and
no redirect or automatic retry. Existing KV writer implementations remain
compatible; an implementation without the probe returns unavailable.

All thirteen reviewed intake IDs must retain their exact path and namespace;
missing IDs, new training-prefix IDs and reroutes within the same destination
set fail closed. The five registry destinations are `crea-sim-gateway`, `crea-sim-captura`,
`crea-sim-map01`, `crea-sim-selva`, and `janua-staging`. The probe verifies the
reviewed registry paths/namespaces and queries only their five `secret/data/`
and five `secret/metadata/` paths. Data requires create/update/patch/read;
metadata requires read/list. Known additional authority, including root/sudo,
is returned as outside the expected scoped contract and cannot produce an
all-required-within-scope result. An unknown/malformed/incomplete response,
403, timeout, network error or oversized body is unverified, never readiness.

The response exposes timestamp, closed status, path capability sets, missing
requirements, unexpected authority and unknown flags only. It exports no
credential, address, namespace configuration, raw error/body, KV value or HCL.
HTTP errors are 503 for an unavailable adapter and 502 for an unverified probe;
complete missing/elevated permission evidence returns 200 with its explicit
status and false `all_required_within_expected_scope`.

This proves only path capabilities of the current writer, including its
effective identity/group policies. It does not prove that values were supplied,
that parameter constraints permit a write, ownership, admission, namespace or
service readiness, or permission to mint credentials. It does not apply any
Vault policy, bootstrap an admin identity or rotate a token.

Vault1.18's [capabilities-self contract](https://developer.hashicorp.com/vault/api-docs/v1.18.x/system/capabilities-self)
documents the path map. The [versioned HTTP routing source](https://github.com/hashicorp/vault/blob/v1.18.3/http/handler.go)
also injects the map at top level alongside `data`. Complete bare maps and
normal/injected envelopes are accepted; injected copies must agree. Duplicate
JSON keys, partial/extra target maps and authentication/wrapping/lease payloads
fail closed. No untrusted response field is reflected into a diagnostic.

## Separate human delegation package — not implemented or executed

The initial permission check needs no administrative elevation. If it proves
missing permission, a separately reviewed fixed-policy adapter remains needed.
Prepare the canonical policy artifact/hash, closed apply schema, previous/source
hash guard, exact readback and capability receipt, conflict/timeout uncertainty,
and unconditional operator gate before asking for any missing delegation.
Vault's ACL-policy endpoint does not provide an atomic compare-and-set here;
an expected hash is a precondition, not a substitute for an exclusive governed
administrative lane.

The authority dependency is the HUMAN Tier0 custodian, not the KV writer.
Private canonical contracts are
`internal-devops/runbooks/orchestrator-vault-credentials-guide.md`,
`internal-devops/runbooks/bitwarden-orchestrator-custody.md`
and `internal-devops/runbooks/vault-bootstrap.md`.
They keep admin/root material outside the cluster; agents cannot open its
custody. The operator console hands human-only bootstrap steps off; its MCP
client cannot execute them. Those dated records do not prove current authority.

The following is a SOURCE REVIEW contract for a future limited delegation,
not a command sequence to execute in this release. A human may establish it
only under the documented platform-bootstrap exception with an approved
artifact, audit and existing administrative custody. No root-token argument,
credential-file path or credential retrieval is specified here.

The separately reviewed `enclii-writer-policy-applier.hcl` must contain only:

```hcl
path "sys/policies/acl/switchyard-secret-writer" {
  capabilities = ["read", "update"]
}
path "sys/capabilities-self" {
  capabilities = ["update"]
}
path "auth/token/lookup-self" {
  capabilities = ["read"]
}
```

After the fixed central ServiceAccount and Vault TokenReview/audience custody
are separately reviewed and proven, the HUMAN-only delegation command shapes
are:

```sh
vault policy write enclii-writer-policy-applier reviewed/enclii-writer-policy-applier.hcl
vault write auth/kubernetes/role/enclii-writer-policy-applier \
  bound_service_account_names=enclii-vault-policy-applier \
  bound_service_account_namespaces=enclii audience=vault \
  token_policies=enclii-writer-policy-applier token_no_default_policy=true \
  token_ttl=5m token_max_ttl=10m
```

The prospective adapter must require the existing fixed target policy before
writing. It gets no other policy name, list/delete/sudo, KV access, token mint,
auth/mount administration or caller HCL. Its short-lived delegated token is
distinct from the current writer. Root material must never be mounted into the
API/Job, routed through ordinary KV/intake, or received by an agent. No current
writer may create or read its own policy-admin delegation.

Boundary: public mechanism and capability names only. Custody identities,
values and operational evidence remain in private internal-devops.
