# Read-only OAuth client evidence

`enclii --profile admin secrets provision oidc inspect --platform crea-sim --json`
uses the existing managed Enclii admin session to GET the Janua client inventory
and the selected internal UUID's detail. The output contains only public client
metadata. It does not register, reconcile, resolve or rotate credentials, write
Vault intake, switch organization, create membership, or send business mail.
The internal-key registration path is deliberately unavailable to inspection.

A missing/ambiguous client, secret-bearing provider response, malformed JSON,
provider denial or transport failure returns a redacted error and no metadata.
An inactive client or drift is reported as evidence, never repaired automatically.
Only an embedded registry platform is selectable; mutation/registry override
flags are not accepted by the inspect child command.

The UI `crea-sim` profile alone declares `madfam:silent_auth`. Janua requires
this explicit first-party trust opt-in for this client name to reuse its own
browser session with `prompt=none` and preconsent. The `crea-sim-mcp` profile,
client names, audiences, organizations, callbacks, grants and consumer roles do
not change. The flag does not grant a human membership or application authority.

Declaring the registry does not reconcile a live client. Before any separately
reviewed change, capture fresh metadata and require an exact fingerprint match
for the actual ID, name, audience, organization, callback, scopes, grants,
confidentiality and active status. Any approved provider request must PATCH
only `allowed_scopes`, adding `madfam:silent_auth` to that same UI client. Re-read
the metadata afterwards: every other allowlisted field must remain equal. Do
not run general provisioning merely to apply this trust change: that flow can
also resolve/rotate credentials and submit intake.

Deploy the qualified SIM bootstrap with its activation flag off first, then
activate only after the live trust opt-in is verified. On withdrawal, disable
the SIM flag first and verify the same client before removing only this scope.
A real invited browser walkthrough remains separate from local synthetic tests.

## Scope-only native plan and guarded apply

`enclii --profile admin secrets provision oidc sim-sso` is read-only by default.
It reports the actual public metadata, its SHA256 fingerprint, and the sole
`allowed_scopes` patch. It is hardcoded to the embedded `crea-sim` UI profile;
there is no platform override, registration, resolver, rotator or intake path.
It refuses an inactive/public/unbound client, a different audience/callback or
grants, or scopes outside the four ordinary scopes plus this one trust opt-in.

Only a separately reviewed operator action may add `--apply`,
`--expect-metadata-sha256 <reviewed-plan-hash>` and a bounded `--reason`.
The command re-reads the client immediately, verifies that exact fingerprint,
PATCHes only `allowed_scopes` once, and re-reads the pinned client. A successful
receipt requires every other allowlisted fact to remain exactly equal. Provider
authority remains Janua's platform-admin check; the command grants no privilege.

There is no atomic metadata If-Match in this Janua endpoint. Serialize other
edits to this one client during the bounded operation; the fingerprint is a
fresh-read precondition, not a server-side compare-and-swap claim. Any denied,
uncertain, credential-bearing or drifted outcome is HELD without automatic
retry or rollback. Inspect the actual state before deciding the next step.

For withdrawal, disable the SIM app flag first. `sim-sso --disable` only plans
removal of `madfam:silent_auth`; review its fresh fingerprint before using the
same explicit apply/reason controls. All other scopes and identity fields stay
unchanged. Merely invoking either default plan performs only GET requests.
